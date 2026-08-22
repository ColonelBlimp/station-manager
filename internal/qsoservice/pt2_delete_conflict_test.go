package qsoservice

import (
	"context"
	"encoding/json"
	stderr "errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// newConcurrentDeleteTestService uses a file-backed SQLite database and a real
// connection pool. The package's ordinary :memory: harness pins MaxOpenConns to
// one, which serialises callers before they can exercise simultaneous PATCH and
// DELETE transactions.
func newConcurrentDeleteTestService(t *testing.T, forwarders ...types.ForwarderConfig) *Service {
	t.Helper()
	workDir := t.TempDir()
	dbPath := filepath.Join(workDir, "pt2-delete-race.db")

	cfg := config.DefaultConfig(workDir)
	cfg.Datastore.Path = dbPath
	cfg.Forwarders = forwarders
	cfgSvc := config.New(cfg)
	require.NoError(t, cfgSvc.Initialize())

	logSvc := &logging.Service{ConfigService: cfgSvc}
	logSvc.WorkingDir = cfgSvc.WorkingDir()
	require.NoError(t, logSvc.Initialize())

	dbSvc := &sqlite.Service{ConfigService: cfgSvc, LoggerService: logSvc}
	require.NoError(t, dbSvc.Initialize())
	dbSvc.DatabaseConfig = &types.DatastoreConfig{
		Driver:                    "sqlite",
		Path:                      dbPath,
		MaxOpenConns:              4,
		MaxIdleConns:              4,
		ContextTimeout:            10,
		TransactionContextTimeout: 10,
	}
	require.NoError(t, dbSvc.Open())
	require.NoError(t, dbSvc.Migrate())

	hub := events.NewHub()
	t.Cleanup(func() {
		hub.Close()
		_ = dbSvc.Close()
		_ = logSvc.Close()
	})
	return &Service{DB: dbSvc, Logger: logSvc, Config: cfgSvc, Hub: hub}
}

// PT-2 — a stale-snapshot delete racing a concurrent edit must refuse with
// delete_conflict, leave the edited QSO live, write no delete upload or history
// row, and — after a fresh re-fetch — delete successfully with the LATEST
// pre-delete image in history (internal-persistence-transaction-audit PT-2).
//
// Without the revision guard the stale delete silently wins over the concurrent
// edit AND the append-only delete history records the pre-edit (revision-N)
// content, even though the last live state was N+1 — an unrepairable false
// sequence in an append-only chain.
func TestDelete_StaleSnapshotRefusesWithDeleteConflict(t *testing.T) {
	s := newTestService(t, enabledCloud())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	first, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)

	// Fetch revision N twice — a delete snapshot and an edit snapshot.
	delSnap, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	editSnap, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)

	// Update through the edit snapshot → revision N+1.
	_, err = s.Update(ctx, editSnap, []byte(`{"rst_sent":"57"}`), source.API)
	require.NoError(t, err)

	// Delete through the STALE snapshot (still revision N).
	err = s.Delete(ctx, delSnap, source.API)
	require.Error(t, err, "a stale-snapshot delete must refuse, not remove the concurrent edit")
	se := IsSubmitError(err)
	require.NotNil(t, se, "the refusal is caller-facing (409), not a daemon fault")
	require.Equal(t, "delete_conflict", se.Code)

	// The edited QSO remains LIVE, carrying edit A's field.
	got, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err, "the QSO is still live — the stale delete did not remove it")
	require.Equal(t, "57", got.QsoDetails.RstSent, "the concurrent edit survives")

	// The refused delete wrote NO delete upload row and NO delete history row
	// (the whole transaction rolled back).
	require.False(t, hasDeleteRow(t, s, delSnap.ID, "cloud"), "the refused delete queued no delete upload")
	hist, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	for _, h := range hist {
		require.NotEqual(t, "delete", h.Op, "the refused delete appended no history row")
	}

	// Refetch N+1, delete successfully, and prove the delete history's before_image
	// is the LATEST live state (rst_sent 57), not the stale revision-N snapshot.
	fresh, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, fresh, source.API))

	hist2, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	var del *types.QsoHistory
	for i := range hist2 {
		if hist2[i].Op == "delete" {
			del = &hist2[i]
		}
	}
	require.NotNil(t, del, "the successful delete appended a history row")
	var before types.Qso
	require.NoError(t, json.Unmarshal(del.BeforeImage, &before))
	require.Equal(t, "57", before.QsoDetails.RstSent,
		"the delete history's before_image is the last live state (N+1), not the stale N snapshot")
}

// PT-2's concurrent boundary: PATCH and DELETE start from the same revision and
// race as independent transactions on a real SQLite pool. Exactly one serial
// ordering may win on every round:
//
//   - PATCH first: PATCH commits, DELETE returns delete_conflict, edited row live;
//   - DELETE first: DELETE commits, PATCH returns ErrNotFound, row tombstoned.
//
// Both success, both failure, SQLITE_BUSY, a lost edit, or a delete upload/history
// row on the refused path are all invalid third outcomes. Repeated under -race,
// this covers the actual simultaneous-writer boundary the deterministic stale
// test above cannot create with its single-connection harness.
func TestDelete_PatchRaceHasOneSerialOrdering(t *testing.T) {
	s := newConcurrentDeleteTestService(t, enabledCloud())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	const rounds = 24
	for i := range rounds {
		rec := ssbRec("M0ABC", "K1ABC")
		rec.QsoDetails.TimeOn = fmt.Sprintf("12%02d", i)
		first, err := s.Submit(ctx, lbID, rec, false)
		require.NoError(t, err, "round %d: submit", i)

		updateSnap, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
		require.NoError(t, err, "round %d: update snapshot", i)
		deleteSnap, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
		require.NoError(t, err, "round %d: delete snapshot", i)

		start := make(chan struct{})
		var updateErr, deleteErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, updateErr = s.Update(ctx, updateSnap, []byte(`{"rst_sent":"57"}`), source.API)
		}()
		go func() {
			defer wg.Done()
			<-start
			deleteErr = s.Delete(ctx, deleteSnap, source.API)
		}()
		close(start)
		wg.Wait()

		switch {
		case updateErr == nil && deleteErr != nil:
			se := IsSubmitError(deleteErr)
			require.NotNil(t, se, "round %d: PATCH-first delete error = %v", i, deleteErr)
			require.Equal(t, "delete_conflict", se.Code, "round %d", i)

			live, ferr := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
			require.NoError(t, ferr, "round %d: PATCH-first row remains live", i)
			require.Equal(t, "57", live.QsoDetails.RstSent, "round %d: PATCH survives", i)
			require.False(t, hasDeleteRow(t, s, live.ID, "cloud"),
				"round %d: refused delete queued no upload", i)
			hist, herr := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, first.UUID)
			require.NoError(t, herr, "round %d: PATCH-first history", i)
			for _, h := range hist {
				require.NotEqual(t, "delete", h.Op, "round %d: refused delete wrote no history", i)
			}

		case deleteErr == nil && updateErr != nil:
			require.True(t, stderr.Is(updateErr, errors.ErrNotFound),
				"round %d: DELETE-first PATCH error = %v, want ErrNotFound", i, updateErr)
			_, ferr := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
			require.True(t, stderr.Is(ferr, errors.ErrNotFound),
				"round %d: DELETE-first row remains tombstoned: %v", i, ferr)
			require.True(t, hasDeleteRow(t, s, deleteSnap.ID, "cloud"),
				"round %d: successful delete queued its upload", i)

		default:
			t.Fatalf("round %d: non-serial outcome: update err=%v, delete err=%v", i, updateErr, deleteErr)
		}
	}
}
