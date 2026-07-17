package smcloud

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// THE S5 GATE (sm-cloud-p1.md): back up → wipe local → restore → local ==
// original — UUID, HH:MM:SS seconds, and additional_data-carried fields
// intact — plus the stronger closing assertion: the RESTORED database
// reconciles IN SYNC with the cloud, which only holds if modified_at survived
// the whole cycle bit-for-bit. Real parts throughout (sqlite + qsoservice on
// both "machines", the real cloud server + Postgres in the middle); skips
// without the dev Postgres (task db:pg:up).
func TestRestore_FullCycle(t *testing.T) {
	cloud := newCloudStack(t)
	ctx := context.Background()

	// ---- Machine 1: the original database. Log two QSOs, delete one, push all.
	qsoSvc1, db1, _, fc := newLocalStack(t, cloud.URL)
	lb1, err := db1.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)
	fwd, err := New(fc)
	require.NoError(t, err)

	uKeep := importQso(t, qsoSvc1, lb1, "DL9UW", "142559")
	uDead := importQso(t, qsoSvc1, lb1, "9A4ZM", "143000")
	qDead, err := db1.FetchQsoByUUIDWithContext(ctx, uDead)
	require.NoError(t, err)
	require.NoError(t, qsoSvc1.Delete(ctx, qDead, source.Source("test")))

	drainTo(t, fwd, db1, uKeep, action.Insert)
	drainTo(t, fwd, db1, uDead, action.Delete)

	orig, err := db1.FetchQsoByUUIDWithContext(ctx, uKeep)
	require.NoError(t, err)

	// ---- Machine 2: the "wiped" replacement — a fresh, empty database.
	qsoSvc2, db2, logSvc2, _ := newLocalStack(t, cloud.URL)
	lb2, err := db2.InsertLogbook(types.Logbook{Name: "Main", Callsign: "7Q5MLV"})
	require.NoError(t, err)

	// ---- Restore: FetchExport → per-record qsoservice.Restore (exactly what
	// `smd restore` runs).
	export, err := FetchExport(ctx, fc)
	require.NoError(t, err)
	require.Len(t, export.Logbooks, 1)
	require.Equal(t, "main", export.Logbooks[0].Name)
	require.Len(t, export.Qsos, 2, "export is retentive — tombstone included")

	var stored, tombstones int
	for _, r := range export.Qsos {
		var qso types.Qso
		require.NoError(t, json.Unmarshal(r.Qso, &qso))
		qso.ModifiedAt = r.ModifiedAt
		if r.DeletedAt != nil {
			qso.DeletedAt = *r.DeletedAt
			tombstones++
		}
		status, err := qsoSvc2.Restore(ctx, lb2, qso)
		require.NoError(t, err)
		require.Equal(t, qsoservice.RestoreStored, status)
		stored++
	}
	require.Equal(t, 2, stored)
	require.Equal(t, 1, tombstones)

	// ---- The gate: the restored live QSO deep-equals the original.
	restored, err := db2.FetchQsoByUUIDWithContext(ctx, uKeep)
	require.NoError(t, err)
	require.Equal(t, orig.UUID, restored.UUID)
	require.Equal(t, "142559", restored.TimeOn, "seconds intact")
	require.True(t, restored.ModifiedAt.Equal(orig.ModifiedAt),
		"modified_at intact: orig %v restored %v", orig.ModifiedAt, restored.ModifiedAt)
	// Field-for-field equality modulo the local storage key (a fresh row id)
	// and LogbookID (machine 2's logbook).
	normOrig, normRest := orig, restored
	normOrig.ID, normRest.ID = 0, 0
	normOrig.LogbookID, normRest.LogbookID = 0, 0
	require.Equal(t, normOrig, normRest, "restored QSO deep-equals the original")

	// The tombstone came back deleted (invisible live, present with recency).
	_, err = db2.FetchQsoByUUIDWithContext(ctx, uDead)
	require.Error(t, err)
	deadRestored, err := db2.FetchQsoByUUIDIncludingDeletedWithContext(ctx, uDead)
	require.NoError(t, err)
	require.False(t, deadRestored.DeletedAt.IsZero())

	// ---- The closing proof: machine 2 reconciles IN SYNC with the cloud —
	// possible only if every modified_at survived backup → restore verbatim.
	rec, err := NewReconciler(fc, lb2, db2, qsoSvc2, logSvc2)
	require.NoError(t, err)
	sum, err := rec.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, sum.InSync, "restored DB must reconcile in sync: %+v", sum)
	require.Equal(t, 1, sum.LocalCount)
	require.Equal(t, 0, sum.EnqueuedUpserts)
	require.Equal(t, 0, sum.EnqueuedDeletes)

	// Idempotent re-run: everything skips.
	for _, r := range export.Qsos {
		var qso types.Qso
		require.NoError(t, json.Unmarshal(r.Qso, &qso))
		qso.ModifiedAt = r.ModifiedAt
		if r.DeletedAt != nil {
			qso.DeletedAt = *r.DeletedAt
		}
		status, err := qsoSvc2.Restore(ctx, lb2, qso)
		require.NoError(t, err)
		require.Equal(t, qsoservice.RestoreSkippedExisting, status)
	}
}
