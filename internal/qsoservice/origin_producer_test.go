package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// Producer-mapping proofs for Diff B (docs/reviews/forwarding-logging-gaps.md F1).
//
// The wire and schema contracts are pinned elsewhere (internal/api,
// internal/database/sqlite). What those cannot catch is a producer wired to the
// WRONG constant: an implementation that threaded a single hard-coded origin
// through every call site would satisfy "the field is present and mirrors the
// row" everywhere while recording provenance that is simply false.
//
// `live` and `edit` are pinned at the API layer, where they are reachable through
// stable HTTP handlers. This file covers the producers that are not:
//
//   - import, INCLUDING the record-by-record batch fallback — a distinct code
//     path (importBatchFallback re-runs through submit with isImport=true), and
//     the one most likely to be missed because it only runs when a batch write
//     has already failed.
//   - stamp_sync, whose enqueue is a different method entirely.
//
// `reconcile` is pinned in internal/forwarding/smcloud, where the reconciler can
// actually be driven. It shares EnqueueUploads with `manual`, which is exactly why
// both ends need their own assertion; EnqueueDeleteUploads is reconcile-only.
//
// These read the stored row rather than a log line: origin is queue state, and
// the log record merely reports it.

// originOf returns the origin recorded on the QSO's upload row for a forwarder.
func originOf(t *testing.T, s *Service, qsoID int64, forwarderName, action string) string {
	t.Helper()
	rows, err := s.DB.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.ForwarderName == forwarderName && r.Action == action {
			return r.Origin
		}
	}
	t.Fatalf("no %s row for forwarder %q on qso %d", action, forwarderName, qsoID)
	return ""
}

// qsoIDByCall resolves a stored QSO's row id by callsign within a logbook.
func qsoIDByCall(t *testing.T, s *Service, lbID int64, call string) int64 {
	t.Helper()
	rows, err := s.DB.FetchQsoSliceByLogbookIdWithContext(context.Background(), lbID)
	require.NoError(t, err)
	for _, r := range rows {
		if r.Call == call {
			return r.ID
		}
	}
	t.Fatalf("no stored QSO for call %q in logbook %d", call, lbID)
	return 0
}

func importRecord(call, timeOn string) adif.Record {
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails: types.QsoDetails{
			Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn,
		},
		LoggingStation: types.LoggingStation{StationCallsign: "G0XYZ"},
	}
}

// A bulk import enqueues with origin `import`, not `live`. Both run through
// submit(); only the isImport flag separates them, which is what makes this the
// easiest pair in the daemon to get backwards.
func TestOrigin_ImportBatchEnqueuesAsImport(t *testing.T) {
	// qrz, not clublog: the import gate refuses no-bulk-backfill destinations
	// outright (review 2026-08-07 #1), and this test's rule is ORIGIN attribution,
	// not destination policy.
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")

	res, err := s.SubmitImportBatch(context.Background(), lbID,
		[]adif.Record{importRecord("M0AAA", "0901")}, []string{"qrz"}, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stored)

	qsoID := qsoIDByCall(t, s, lbID, "M0AAA")
	require.Equal(t, "import", originOf(t, s, qsoID, "qrz", "insert"),
		"a bulk import must record origin=import, not the live-logging value")
}

// The batch fallback path — importBatchFallback re-runs a failed batch
// record-by-record through submit(..., isImport=true), which is a SECOND route to
// the same enqueue and only executes once something has already gone wrong.
//
// FIXTURE NOTE (two earlier attempts were wrong, 2026-08-01; the reversion proof
// is what exposed them, because reverting submit.go's derivation broke nothing):
//
//   - An over-long MODE is rejected by validation in phase 1, so the batch commits
//     the good record alone via the BATCH producer.
//   - Two records sharing a dedupe key are caught by the in-batch `batchKeys` map
//     (submit_batch.go:150) before the DB is consulted.
//
// Both left the test exercising the batch path under a fallback name. The failure
// has to happen INSIDE the phase-2 transaction, and a UUID collision does exactly
// that: it collides on UNIQUE(uuid), which the dedupe pre-read cannot see because
// the dedupe FIELDS differ. Fixture shape borrowed from the proven
// TestSubmitImportBatch_UUIDCollisionReportedNotFatal.
//
// That the batch transaction rolled back is what makes this decisive: the good
// record's upload row can only have been written by the fallback.
func TestOrigin_ImportBatchFallbackStillEnqueuesAsImport(t *testing.T) {
	// qrz for the same reason as the batch test above (review 2026-08-07 #1).
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")
	ctx := context.Background()

	const uuid = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	rec := func(uid, call, timeOn string) adif.Record {
		r := importRecord(call, timeOn)
		r.AppSmQsoID = uid
		return r
	}

	// Pre-store the UUID so a later record carrying it collides on UNIQUE(uuid).
	pre, err := s.SubmitImport(ctx, lbID, rec(uuid, "K1AAA", "1200"), false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", pre.Status)

	// One good record, then the collision — same UUID, DIFFERENT dedupe fields, so
	// phase-1 dedupe passes it through and phase 2 aborts on the unique index.
	res, err := s.SubmitImportBatch(ctx, lbID,
		[]adif.Record{rec("", "M0BBB", "0902"), rec(uuid, "K3CCC", "1400")},
		[]string{"qrz"}, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stored, "the good record must survive via the fallback")
	require.Len(t, res.Errors, 1, "the collision must be reported per-record")
	require.Contains(t, res.Errors[0].Reason, "uuid_conflict",
		"proves phase 2 aborted and importBatchFallback ran — without it the batch "+
			"would have committed and this would be testing the batch producer")

	qsoID := qsoIDByCall(t, s, lbID, "M0BBB")
	require.Equal(t, "import", originOf(t, s, qsoID, "qrz", "insert"),
		"the record-by-record fallback must record the same origin as the batch path")
}

// The stamp-sync mirror re-enqueue is its own producer with its own method, and
// its rows are the ones that made smcloud look 12.8x busier than QRZ in the F1
// measurement — the case the whole field exists to explain.
func TestOrigin_StampSyncEnqueuesAsStampSync(t *testing.T) {
	// A ROW MIRROR forwarder, not clublog: EnqueueStampSync deliberately targets
	// mirror types only (forwarding.RegisterRowMirror), so a clublog fixture would
	// re-enqueue nothing and the assertion below would never run.
	s := newTestService(t, enabledMirror())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")
	_, qsoID := seedStoredQso(t, s, lbID, "M0DDD", "0904")

	n, err := s.EnqueueStampSync(context.Background(), []int64{qsoID})
	require.NoError(t, err)
	require.Positive(t, n, "fixture: nothing was re-enqueued, so this proves nothing")

	require.Equal(t, "stamp_sync", originOf(t, s, qsoID, "smcloud", "update"),
		"a stamp-sync mirror re-enqueue must be distinguishable from live traffic")
}
