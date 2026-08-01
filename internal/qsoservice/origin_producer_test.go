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
// actually be driven; it shares EnqueueUploads/EnqueueDeleteUploads with `manual`,
// which is exactly why both ends need their own assertion.
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
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")

	res, err := s.SubmitImportBatch(context.Background(), lbID,
		[]adif.Record{importRecord("M0AAA", "0901")}, []string{"clublog"}, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stored)

	qsoID := qsoIDByCall(t, s, lbID, "M0AAA")
	require.Equal(t, "import", originOf(t, s, qsoID, "clublog", "insert"),
		"a bulk import must record origin=import, not the live-logging value")
}

// The batch fallback path. importBatchFallback re-runs the batch record-by-record
// through submit() after a transactional write fails, so it is a SECOND route to
// the same enqueue — and it only executes when something has already gone wrong,
// which is precisely when nobody is watching the provenance.
//
// The fixture forces the fallback by putting a record that violates a DB
// constraint alongside a good one: the batch write aborts, the fallback stores
// the good record individually, and its origin must still be `import`.
func TestOrigin_ImportBatchFallbackStillEnqueuesAsImport(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "G0XYZ")

	good := importRecord("M0BBB", "0902")
	bad := importRecord("M0CCC", "0903")
	bad.QsoDetails.Mode = "THIS_MODE_IS_FAR_TOO_LONG_FOR_THE_CHECK" // >20 chars → CHECK violation

	res, err := s.SubmitImportBatch(context.Background(), lbID,
		[]adif.Record{good, bad}, []string{"clublog"}, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stored, "the good record must survive via the fallback")
	require.NotEmpty(t, res.Errors, "the bad record must be reported, proving the fallback ran")

	qsoID := qsoIDByCall(t, s, lbID, "M0BBB")
	require.Equal(t, "import", originOf(t, s, qsoID, "clublog", "insert"),
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
