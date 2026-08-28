package qsoservice

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// TestSubmitImportBatch_StoresDedupsAndReports exercises the bulk-import path
// across multiple batches (batchSize=2): unique records store, in-run AND
// pre-existing duplicates are counted (not re-stored), a validation failure is
// isolated + reported by index (not fatal), and contacted_station is upserted
// once per unique call.
func TestSubmitImportBatch_StoresDedupsAndReports(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := func(call, timeOn string) adif.Record {
		return adif.Record{
			ContactedStation: types.ContactedStation{Call: call},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
			LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"}, // import relaxes the callsign match
		}
	}

	// Pre-store one QSO so the batch sees a PRE-EXISTING (committed) duplicate.
	pre, err := s.SubmitImport(ctx, lbID, rec("PRE9", "1300"), false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", pre.Status)

	recs := []adif.Record{
		rec("K1AAA", "1200"), // 0 stored
		{QsoDetails: types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"}, LoggingStation: types.LoggingStation{StationCallsign: "G0XYZ"}}, // 1 no CALL → error
		rec("K1AAA", "1200"), // 2 in-run dup of #0
		rec("K2BBB", "1201"), // 3 stored
		rec("K3CCC", "1202"), // 4 stored
		rec("K2BBB", "1201"), // 5 in-run dup of #3
		rec("PRE9", "1300"),  // 6 pre-existing dup
	}

	res, err := s.SubmitImportBatch(ctx, lbID, recs, nil, 2, nil)
	require.NoError(t, err)

	require.Equal(t, 3, res.Stored, "K1AAA, K2BBB, K3CCC")
	require.Equal(t, 3, res.Duplicate, "two in-run dups + one pre-existing")
	require.Len(t, res.Errors, 1)
	require.Equal(t, 1, res.Errors[0].Index, "the no-CALL record's input index")
	require.Contains(t, res.Errors[0].Reason, "missing_required_field")

	// Each unique stored call wrote exactly one QSO + one contacted_station.
	for _, call := range []string{"K1AAA", "K2BBB", "K3CCC"} {
		key := ComputeDedupeKey(call, "20m", "SSB", "14074", "20260101", map[string]string{"K1AAA": "1200", "K2BBB": "1201", "K3CCC": "1202"}[call])
		q, ferr := s.DB.FetchQsoByDedupeKeyWithContext(ctx, lbID, key)
		require.NoError(t, ferr, "stored QSO for %s", call)
		require.Equal(t, call, q.ContactedStation.Call)

		cs, cerr := s.DB.FetchContactedStationByCallsignWithContext(ctx, call)
		require.NoError(t, cerr, "contacted_station upserted for %s", call)
		require.Equal(t, call, cs.Call)
	}
}

// TestSubmitImportBatch_ForwardSelection confirms the batch path honours the
// same enqueue gate as per-record submit (ADR 0022): nothing is queued unless
// the forwarder is named in forwardTo.
func TestSubmitImportBatch_ForwardSelection(t *testing.T) {
	s := newTestService(t,
		types.ForwarderConfig{Name: "qrz", Type: "qrz", Enabled: true, ActionFilter: []string{"insert"}},
	)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()
	rec := func(call string) adif.Record {
		return adif.Record{
			ContactedStation: types.ContactedStation{Call: call},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: "1200"},
			LoggingStation:   types.LoggingStation{StationCallsign: "M0ABC"},
		}
	}
	uploadCount := func(call string) int {
		key := ComputeDedupeKey(call, "20m", "SSB", "14074", "20260101", "1200")
		q, ferr := s.DB.FetchQsoByDedupeKeyWithContext(ctx, lbID, key)
		require.NoError(t, ferr)
		rows, uerr := s.DB.FetchUploadsByQsoIDWithContext(ctx, q.ID)
		require.NoError(t, uerr)
		return len(rows)
	}

	// Named → enqueued.
	res, err := s.SubmitImportBatch(ctx, lbID, []adif.Record{rec("K1AAA")}, []string{"qrz"}, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stored)
	require.Equal(t, 1, uploadCount("K1AAA"), "named forwarder enqueues a row")

	// Not named → nothing queued (default import).
	res, err = s.SubmitImportBatch(ctx, lbID, []adif.Record{rec("K2BBB")}, nil, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stored)
	require.Equal(t, 0, uploadCount("K2BBB"), "unnamed → no upload rows")
}

// TestSubmitImportBatch_UUIDCollisionReportedNotFatal guards F3 (review
// 2026-07-02): restore-from-export (the SM Cloud P1 workflow) can re-import a QSO
// whose dedupe fields were edited after export — same UUID, different dedupe key.
// That collides on the UNIQUE(uuid) index, not the dedupe index, so the dedupe
// refetch misses and it must be classified as a per-record uuid_conflict that is
// reported and SKIPPED — not a raw error that aborts the whole import mid-run.
func TestSubmitImportBatch_UUIDCollisionReportedNotFatal(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	const uuid = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	rec := func(uid, call, timeOn string) adif.Record {
		return adif.Record{
			AppSmQsoID:       uid,
			ContactedStation: types.ContactedStation{Call: call},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
			LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
		}
	}

	// Pre-store a QSO with the known UUID (import preserves supplied UUIDs).
	pre, err := s.SubmitImport(ctx, lbID, rec(uuid, "K1AAA", "1200"), false, nil)
	require.NoError(t, err)
	require.Equal(t, "stored", pre.Status)

	// Batch (batchSize=2): a good record, then the UUID collision (different
	// dedupe fields), then another good record AFTER it — the last one storing
	// proves the run did not abort at the collision.
	recs := []adif.Record{
		rec("", "K2BBB", "1300"),   // 0 stored
		rec(uuid, "K3CCC", "1400"), // 1 UUID collision (dedupe key differs)
		rec("", "K4DDD", "1500"),   // 2 stored AFTER the collision
	}

	res, err := s.SubmitImportBatch(ctx, lbID, recs, nil, 2, nil)
	require.NoError(t, err, "a UUID collision must not abort the whole import")
	require.Equal(t, 2, res.Stored, "K2BBB + K4DDD stored around the collision")
	require.Len(t, res.Errors, 1)
	require.Equal(t, 1, res.Errors[0].Index, "the colliding record's input index")
	require.Contains(t, res.Errors[0].Reason, "uuid_conflict")
}

// Q8 — WHEN A BATCH WRITE FAILS AND THE IMPORT DROPS TO PER-RECORD, THE TRIGGERING
// ERROR IS LOGGED. The efficient batch path degrading to per-record inserts was
// invisible: a systematic cause (a constraint, schema drift) presented as "imports
// are slow" with no thread to pull. A UUID collision inside the batch tx forces the
// fallback here.
func TestSubmitImportBatch_FallbackLogsTriggeringError(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	const uuid = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	rec := func(uid, call, timeOn string) adif.Record {
		return adif.Record{
			AppSmQsoID:       uid,
			ContactedStation: types.ContactedStation{Call: call},
			QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
			LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
		}
	}

	_, err := s.SubmitImport(ctx, lbID, rec(uuid, "K1AAA", "1200"), false, nil)
	require.NoError(t, err)

	// Batch (size 2): a good record then a UUID collision → the batch INSERT fails
	// and the run drops to per-record.
	recs := []adif.Record{
		rec("", "K2BBB", "1300"),
		rec(uuid, "K3CCC", "1400"),
	}
	buf := logbuf(s)
	res, err := s.SubmitImportBatch(ctx, lbID, recs, nil, 2, nil)
	require.NoError(t, err, "a clean rollback enters the per-record recovery path, not an abort")
	require.Equal(t, 1, res.Stored, "K2BBB stored via the fallback")
	// The confirmed-rollback fallback must produce the correct per-record tally: the
	// UUID collision is reported as an error, nothing is mis-counted as a duplicate.
	require.Len(t, res.Errors, 1, "the colliding record is reported errored, not stored")
	require.Contains(t, res.Errors[0].Reason, "uuid_conflict")
	require.Equal(t, 0, res.Duplicate, "a uuid conflict is not a dedupe duplicate")

	line := logLineWith(t, buf.String(), "falling back to per-record")
	require.Contains(t, line, `"level":"warn"`, "the fallback trigger must be visible")
	require.Contains(t, line, `"base_index":0`, "with the batch's base index")
}

// Q10 — A BULK IMPORT LEAVES A DURABLE COMPLETION SUMMARY. SubmitImportBatch returned
// its totals to the CLI (stdout) without logging them, so smd.log could not tell a
// completed import from an interrupted one — and for a restore that summary is the
// record of what was recovered.
func TestSubmitImportBatch_LogsCompletionSummary(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)

	// Two unique + one duplicate (fanoutRec fixes band/mode/date/time, so a repeated
	// call is a dedupe-key collision).
	recs := []adif.Record{fanoutRec("K1AAA"), fanoutRec("K2BBB"), fanoutRec("K1AAA")}
	res, err := s.SubmitImportBatch(context.Background(), lbID, recs, nil, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 2, res.Stored)
	require.Equal(t, 1, res.Duplicate)

	line := logLineWith(t, buf.String(), "bulk import complete")
	require.Contains(t, line, `"level":"info"`)
	require.Contains(t, line, `"stored":2`)
	require.Contains(t, line, `"duplicate":1`)
	require.Contains(t, line, `"errored":0`)
}
