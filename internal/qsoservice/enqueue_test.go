package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
	"github.com/stretchr/testify/require"
)

// Register the "qrz" type's ADIF stamp prefix for these tests (the real qrz
// package isn't imported here). EnqueueUploads resolves a forwarder's type → its
// stamp prefix to key the skip-check; without it, type "qrz" would report "no
// stamp" and never skip.
func init() {
	forwarding.RegisterAdifPrefix("qrz", "QRZCOM")
	// Mirror the real clublog package's registration (not imported here):
	// ClubLog forbids catch-up batches on realtime.php, so EnqueueUploads must
	// refuse it as a manual-backfill destination.
	forwarding.RegisterNoBulkBackfill("clublog")
}

// enabledQRZ is a minimal enabled forwarder config for the backfill tests.
// EnqueueUploads only reads name/type/enabled/action_filter — it never builds
// the forwarder — so the type string need not be a registered implementation.
func enabledQRZ() types.ForwarderConfig {
	return types.ForwarderConfig{
		Name:         "qrz",
		Type:         "qrz",
		Enabled:      true,
		ActionFilter: []string{"insert", "update", "delete"},
	}
}

// seedStoredQso stores one QSO with no upload rows (SubmitImport with empty
// forwardTo queues nothing) and returns its UUID + local id.
func seedStoredQso(t *testing.T, s *Service, lbID int64, call, timeOn string) (string, int64) {
	t.Helper()
	rec := adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"}, // import relaxes the callsign match
	}
	res, err := s.SubmitImport(context.Background(), lbID, rec, false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
	return res.UUID, res.ID
}

// hasPendingInsertRow reports whether the QSO has an insert-upload row to
// forwarderName (any status).
func hasInsertRow(t *testing.T, s *Service, qsoID int64, forwarderName string) bool {
	t.Helper()
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.ForwarderName == forwarderName && r.Action == "insert" {
			return true
		}
	}
	return false
}

// enabledClublog is a minimal enabled retry-only forwarder config (the type
// registered NoBulkBackfill in init above).
func enabledClublog() types.ForwarderConfig {
	return types.ForwarderConfig{
		Name:         "clublog",
		Type:         "clublog",
		Enabled:      true,
		ActionFilter: []string{"insert", "delete"},
	}
}

// TestEnqueueUploads_NoBulkBackfill_RefusesNoHistoryRows: for a NoBulkBackfill
// destination (ClubLog — realtime.php must never carry catch-up batches; the
// 2026-07-19 API-key grant condition), a QSO with NO queue history for that
// forwarder is backfill: refused per-row into skipped_no_history, and nothing
// reaches the queue.
func TestEnqueueUploads_NoBulkBackfill_RefusesNoHistoryRows(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	res, err := s.EnqueueUploads(context.Background(), "clublog", []string{u1}, false, origin.Manual)
	require.NoError(t, err)
	require.Zero(t, res.Enqueued)
	require.Equal(t, []string{u1}, res.SkippedNoHistory)

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), id1)
	require.NoError(t, err)
	require.Empty(t, rows, "no queue row may be written for a refused backfill row")
}

// TestEnqueueUploads_NoBulkBackfill_RejectsForce: force means "re-send even
// though uploaded" — for a retry-only destination that is by definition the
// prohibited catch-up batch (review round 2 #1), so the whole request is
// refused with a typed error before any row is touched.
func TestEnqueueUploads_NoBulkBackfill_RejectsForce(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	_, err := s.EnqueueUploads(context.Background(), "clublog", []string{u1}, true, origin.Manual)
	se := IsSubmitError(err)
	require.NotNil(t, se, "want a SubmitError, got %v", err)
	require.Equal(t, "force_unsupported", se.Code)

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), id1)
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestEnqueueUploads_NoBulkBackfill_UploadedRowIsNotProvenance: an UPLOADED
// queue row is a delivered QSO, not retry provenance — it must classify as
// no-history, closing the second half of the force bypass (review round 2 #1:
// even without force, a stampless-but-uploaded row must never re-arm).
func TestEnqueueUploads_NoBulkBackfill_UploadedRowIsNotProvenance(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()
	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	// Live enqueue driven to UPLOADED via the plain (stampless) success path,
	// so the stamp skip-check cannot mask the history classification.
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	require.NoError(t, err)
	require.NoError(t, s.DB.InsertQsoUploadTx(ctx, tx, id1, action.Insert, "clublog", "clublog", origin.Live))
	require.NoError(t, tx.Commit())
	cancel()
	claimed, err := s.DB.ClaimPendingUploadsWithContext(ctx, "clublog", 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, s.DB.MarkUploadSuccessWithContext(ctx, claimed[0].ID, "cl-1"))

	res, err := s.EnqueueUploads(ctx, "clublog", []string{u1}, false, origin.Manual)
	require.NoError(t, err)
	require.Zero(t, res.Enqueued)
	require.Equal(t, []string{u1}, res.SkippedNoHistory)

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, id1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "uploaded", rows[0].Status, "the delivered row must stay uploaded, not re-armed")
}

// TestEnqueueUploads_NoBulkBackfill_RetriesFailedLiveRow: a QSO WITH queue
// history for the destination was a live upload — re-arming it is legitimate
// realtime usage (2026-07-19 review #2: this endpoint is how a 403-era
// Terminal row is re-sent after credentials are fixed), so the enqueue
// re-arms it while a history-less row in the same request is still refused.
func TestEnqueueUploads_NoBulkBackfill_RetriesFailedLiveRow(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200") // had a live upload that failed
	u2, id2 := seedStoredQso(t, s, lbID, "K2BBB", "1201") // never queued (backfill)

	// Simulate the live enqueue + terminal failure (the 403-era shape): a
	// queue row exists for clublog, driven to failed via the worker lifecycle.
	tx, cancel, err := s.DB.BeginTxContext(ctx)
	require.NoError(t, err)
	require.NoError(t, s.DB.InsertQsoUploadTx(ctx, tx, id1, action.Insert, "clublog", "clublog", origin.Live))
	require.NoError(t, tx.Commit())
	cancel()
	claimed, err := s.DB.ClaimPendingUploadsWithContext(ctx, "clublog", 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, s.DB.MarkUploadFailedWithContext(ctx, claimed[0].ID, "auth rejected (403)"))

	res, err := s.EnqueueUploads(ctx, "clublog", []string{u1, u2}, false, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued, "the failed live row must be re-armed")
	require.Equal(t, []string{u2}, res.SkippedNoHistory, "the never-queued row stays refused")

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, id1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "pending", rows[0].Status, "re-arm returns the failed row to pending")

	rows2, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, id2)
	require.NoError(t, err)
	require.Empty(t, rows2)
}

func TestEnqueueUploads_EnqueuesSelectedGaps(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	u2, id2 := seedStoredQso(t, s, lbID, "K2BBB", "1201")

	res, err := s.EnqueueUploads(ctx, "qrz", []string{u1, u2}, false, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, 2, res.Enqueued)
	require.Equal(t, 0, res.SkippedUploaded)
	require.Empty(t, res.NotFound)
	require.Empty(t, res.SkippedDeleted)

	require.True(t, hasInsertRow(t, s, id1, "qrz"), "K1AAA queued")
	require.True(t, hasInsertRow(t, s, id2, "qrz"), "K2BBB queued")
}

func TestEnqueueUploads_SkipsAlreadyUploadedUnlessForce(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	u1, id1 := seedStoredQso(t, s, lbID, "K1AAA", "1200")

	// Drive the row to uploaded via the real worker lifecycle: enqueue → claim →
	// mark success WITH the ADIF stamp (the durable per-destination "done" signal
	// the skip-check reads — the stamp, not the queue row, so it survives import).
	_, err := s.EnqueueUploads(ctx, "qrz", []string{u1}, false, origin.Manual)
	require.NoError(t, err)
	claimed, err := s.DB.ClaimPendingUploadsWithContext(ctx, "qrz", 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, s.DB.MarkUploadSuccessWithAdifStampWithContext(ctx, claimed[0].ID, "upstream-1", id1, "QRZCOM"))

	// Default (no force): the uploaded QSO is skipped, nothing re-queued.
	res, err := s.EnqueueUploads(ctx, "qrz", []string{u1}, false, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, 0, res.Enqueued)
	require.Equal(t, 1, res.SkippedUploaded)

	// force=true re-arms the row to pending so the worker re-sends.
	res, err = s.EnqueueUploads(ctx, "qrz", []string{u1}, true, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued)
	require.Equal(t, 0, res.SkippedUploaded)

	rows, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, id1)
	require.NoError(t, err)
	require.Len(t, rows, 1, "still one insert row (re-armed, not duplicated)")
	require.Equal(t, "pending", rows[0].Status, "force re-armed to pending")
}

func TestEnqueueUploads_RejectsIneligibleForwarder(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		fwds []types.ForwarderConfig
		dest string
	}{
		{
			name: "disabled",
			fwds: []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: false, ActionFilter: []string{"insert"}}},
			dest: "qrz",
		},
		{
			name: "unknown",
			fwds: []types.ForwarderConfig{enabledQRZ()},
			dest: "clublog",
		},
		{
			name: "enabled but does not forward inserts",
			fwds: []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"delete"}}},
			dest: "qrz",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestService(t, c.fwds...)
			lbID := seedLogbook(t, s, "Main", "M0ABC")
			u1, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")

			_, err := s.EnqueueUploads(ctx, c.dest, []string{u1}, false, origin.Manual)
			require.Error(t, err)
			se := IsSubmitError(err)
			require.NotNil(t, se)
			require.Equal(t, "forwarder_unavailable", se.Code)
		})
	}
}

func TestEnqueueUploads_ClassifiesMissingAndDeleted(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	live, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	delUUID, _ := seedStoredQso(t, s, lbID, "K2BBB", "1201")

	// Soft-delete the second QSO.
	delQso, err := s.DB.FetchQsoByUUIDWithContext(ctx, delUUID)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, delQso, source.API))

	unknown := utils.NewUUIDv7() // valid shape, never stored
	const malformed = "not-a-uuid"

	res, err := s.EnqueueUploads(ctx, "qrz", []string{live, delUUID, unknown, malformed}, false, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued, "only the live QSO")
	require.ElementsMatch(t, []string{delUUID}, res.SkippedDeleted)
	require.ElementsMatch(t, []string{unknown, malformed}, res.NotFound)
}
