package qsoservice

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

/*
	Review round 2026-08-07 (commit 8d9ab622) — five findings, all verified
	against the code before these rules were written; specified before the
	implementations. Each rule names its confusable pair.

	Finding 1 (High) — import forwarding bypassed the no-bulk-backfill policy:
	`import --forward clublog` would queue an entire historical log for
	ClubLog's realtime endpoint, the exact catch-up batch SM promised ClubLog
	in writing (2026-07-19) it would never send, enforced at the manual
	backfill (enqueue.go) but not at either import path.
	  IG1 — SubmitImportBatch naming a no-bulk-backfill destination refuses UP
	        FRONT ("backfill_unsupported"), storing nothing — refusal, not
	        silent narrowing (the round-2 force_unsupported posture), and I can
	        tell it from an import that ran without forwarding: re-running
	        WITHOUT the name stores every record.
	  IG2 — SubmitImport refuses identically, matching the name
	        case-insensitively (the CLI passes operator-typed case).
	  IG3 — the LIVE submit path still enqueues to clublog: as-you-log
	        realtime IS the sanctioned use; an over-broad gate here would
	        silently stop live forwarding (the nearest wrong fix).

	Finding 2 (High) — concurrent PATCHes both fetch revision N; the second
	commit silently overwrites the first's unrelated fields and writes a
	second revision-N audit before-image (the chain branches). The fix is
	optimistic concurrency on the trigger-maintained revision counter
	(ADR 0050): the row UPDATE matches only at the snapshot's revision.
	  CAS1 — an Update from a STALE snapshot (the row changed since that
	         fetch) refuses with "edit_conflict", leaves the first edit
	         intact, and writes NO second audit row — distinguishable from
	         the lost-update state, where both "succeed" and one edit
	         silently vanishes. (This closes the two-requests window; a
	         client editing over a stale FORM is out of scope — revision is
	         json:"-", deliberately not client-supplied.)
	  CAS2 — an Update from a FRESH fetch after another edit succeeds: CAS
	         refuses stale snapshots, not sequential edits.

	Finding 3 (Medium) — prepareQso/Update enforce no maximum lengths for
	RST_SENT / RST_RCVD (schema CHECK ≤ 10, migration 0002/0006) or COUNTRY
	(trimmed ≤ 50), so an overlong value passed validation and died as a DB
	CHECK failure: a 500 on the public path, and on the batch import path a
	non-SubmitError in the per-record fallback that ABORTED the whole import.
	  LV1 — an import record with an 11-char RST is a PER-RECORD error and
	        the rest of the import completes — distinguishable from the old
	        state (one bad record sinks every later good one).
	  LV2 — public Submit with an overlong RST refuses invalid_field_value
	        (a 400, not a server fault).
	  LV3 — COUNTRY > 50 chars likewise.
	  LV4 — PATCH rst_sent > 10 likewise on the update path.

	Finding 4 (Medium) — time coherence only fired when TIME_ON > TIME_OFF at
	minute precision, so (a) with non-decreasing clock times QSO_DATE_OFF
	could be ANY date — years before QSO_DATE — and (b) a both-HHMMSS pair in
	the same minute stored a negative interval (120059 → 120000), seconds
	being preserved in storage but truncated in the check.
	  TC1 — non-decreasing times + QSO_DATE_OFF BEFORE QSO_DATE → refused.
	  TC2 — non-decreasing times + QSO_DATE_OFF AFTER QSO_DATE → refused (a
	        same-direction pair spans a full day only via the overnight rule,
	        which requires TIME_ON after TIME_OFF).
	  TC3 — both times HHMMSS, same minute, ON later than OFF, same day →
	        refused: the times are compared at their finest COMMON precision.
	  TC4 — a mixed HHMM/HHMMSS pair in the same minute stays ACCEPTED in
	        both directions: HHMM names the minute, not second :00 — refusing
	        it would reject QRZ-style minute logs of seconds-bearing QSOs
	        (the precision-honest reading, mirroring preserveSeconds).
	  TC5 — the PATCH path enforces the same-day rule identically.
	  (The genuine overnight case — ON 2350, OFF 0010, DATE_OFF the next
	  day — stays accepted; asserted inside TC1's test as its guard.)

	Finding 5 (Medium) — Restore's existence probe treated EVERY error as
	"not found" and fell through to the insert, and a check-to-insert race
	surfaced a raw unique-constraint error instead of the documented
	idempotent skipped_existing.
	  RS1 — a failed existence probe surfaces the probe fault, attributed to
	        the existence check — not blind fall-through whose insert error
	        misattributes the fault ("insert restored qso" was the old text).
	        Fixture: closed DB — under the old code the error names the
	        INSERT, under the rule it names the check.
	  RS2 — classifyRestoreInsertErr translates a REAL uuid unique-collision
	        (row present on refetch) into skipped_existing — the concurrent
	        restore's documented outcome — and passes every other insert
	        fault through unchanged. Tested at the classifier because the
	        collision branch is reachable only in the check-to-insert window,
	        which a sequential fixture cannot enter (the probe would see the
	        row); the wiring is the one call site in Restore.
*/

// ---- Finding 1: import refuses no-bulk-backfill destinations -----------------

func importRec(call, timeOn string) adif.Record {
	return adif.Record{
		ContactedStation: types.ContactedStation{Call: call},
		QsoDetails:       types.QsoDetails{Band: "20m", Mode: "SSB", Freq: "14.074", QsoDate: "20260101", TimeOn: timeOn},
		LoggingStation:   types.LoggingStation{StationCallsign: "G0XYZ"},
	}
}

func TestImport_RefusesNoBulkBackfillForwarder_Batch(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	recs := []adif.Record{importRec("K1AAA", "1200"), importRec("K2BBB", "1201")}

	res, err := s.SubmitImportBatch(ctx, lbID, recs, []string{"clublog"}, 0, nil)
	require.Error(t, err, "IG1: naming clublog must refuse — queueing a historical log "+
		"to realtime.php is the catch-up batch SM promised ClubLog it would never send")
	se := IsSubmitError(err)
	require.NotNil(t, se, "the refusal is caller-facing, not a daemon fault")
	require.Equal(t, "backfill_unsupported", se.Code)
	require.Zero(t, res.Stored, "refusal happens UP FRONT — nothing stored")

	// The refusal stored nothing: the same records import cleanly without the name.
	res, err = s.SubmitImportBatch(ctx, lbID, recs, nil, 0, nil)
	require.NoError(t, err)
	require.Equal(t, 2, res.Stored, "both records still importable — the refusal wrote nothing")
}

func TestImport_RefusesNoBulkBackfillForwarder_PerRecord(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	// Mixed case: the CLI passes the operator's typing; the gate must match the
	// config name the way forwarderNamed does.
	_, err := s.SubmitImport(context.Background(), lbID, importRec("K1AAA", "1200"), false, []string{"ClubLog"})
	require.Error(t, err, "IG2: the per-record import path shares the policy")
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "backfill_unsupported", se.Code)
}

func TestSubmit_LiveClublogEnqueueStillWorks(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err, "IG3: live logging must be untouched by the import gate")

	q, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)
	ups, err := s.DB.FetchUploadsByQsoIDWithContext(ctx, q.ID)
	require.NoError(t, err)
	found := false
	for _, u := range ups {
		if strings.EqualFold(u.ForwarderName, "clublog") {
			found = true
		}
	}
	require.True(t, found, "as-you-log realtime IS the sanctioned ClubLog use — "+
		"an over-broad gate that stops live forwarding is the nearest wrong fix")
}

// ---- Finding 2: revision CAS on the update path ------------------------------

func TestUpdate_StaleSnapshotRefusesWithEditConflict(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	first, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	stale, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)

	// Edit A commits from this snapshot; the snapshot is now stale.
	_, err = s.Update(ctx, stale, []byte(`{"rst_sent":"57"}`), source.API)
	require.NoError(t, err)

	// Edit B replays the SAME snapshot — the two-concurrent-requests shape,
	// sequenced deterministically. Without CAS it "succeeds" and silently
	// reverts edit A's field; the audit chain gains a second revision-N
	// before-image.
	_, err = s.Update(ctx, stale, []byte(`{"country":"Malawi"}`), source.API)
	require.Error(t, err, "CAS1: a stale snapshot must refuse, not overwrite")
	se := IsSubmitError(err)
	require.NotNil(t, se, "the refusal is caller-facing (409), not a daemon fault")
	require.Equal(t, "edit_conflict", se.Code)

	got, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	require.Equal(t, "57", got.QsoDetails.RstSent, "edit A survives — the lost update IS the finding")
	require.NotEqual(t, "Malawi", got.ContactedStation.Country, "edit B was refused, not applied")

	hist, err := s.DB.FetchQsoHistoryByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	require.Len(t, hist, 1, "exactly one audit row — the refused edit writes no branch "+
		"into the chain (two identical before-images was the corruption half)")
}

func TestUpdate_FreshFetchAfterEditSucceeds(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	first, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	snap, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	_, err = s.Update(ctx, snap, []byte(`{"rst_sent":"57"}`), source.API)
	require.NoError(t, err)

	fresh, err := s.DB.FetchQsoByUUIDWithContext(ctx, first.UUID)
	require.NoError(t, err)
	updated, err := s.Update(ctx, fresh, []byte(`{"country":"Malawi"}`), source.API)
	require.NoError(t, err, "CAS2: CAS refuses STALE snapshots, not sequential edits")
	require.Equal(t, "Malawi", updated.ContactedStation.Country)
	require.Equal(t, "57", updated.QsoDetails.RstSent, "edit A's field rides through untouched")
}

// ---- Finding 3: schema length limits are validation, not 500s ----------------

func TestImport_OverlongRstIsPerRecordError(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	bad := importRec("K2BBB", "1201")
	bad.QsoDetails.RstSent = "599+20dB40X" // 11 > the schema's 10 (migration 0002/0006)
	recs := []adif.Record{importRec("K1AAA", "1200"), bad, importRec("K3CCC", "1202")}

	res, err := s.SubmitImportBatch(ctx, lbID, recs, nil, 0, nil)
	require.NoError(t, err, "LV1: one overlong RST must NOT abort the import — "+
		"under the old code the CHECK failure sank the batch, then the fallback, "+
		"then every record after it")
	require.Equal(t, 2, res.Stored, "the good records around the offender store")
	require.Len(t, res.Errors, 1)
	require.Equal(t, 1, res.Errors[0].Index)
	require.Contains(t, res.Errors[0].Reason, "invalid_field_value")
}

func TestSubmit_OverlongRstRefused(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	rec := ssbRec("M0ABC", "K1ABC")
	rec.QsoDetails.RstRcvd = "59999999999" // 11 > the schema's 10 (migration 0002/0006)
	_, err := s.Submit(context.Background(), lbID, rec, false)
	require.Error(t, err, "LV2: an overlong RST is the caller's mistake, not a daemon fault")
	se := IsSubmitError(err)
	require.NotNil(t, se, "must be a 400-class SubmitError — the old path died on the DB CHECK as a 500")
	require.Equal(t, "invalid_field_value", se.Code)
	require.Contains(t, se.Message, "RST_RCVD")
}

func TestSubmit_OverlongCountryRefused(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")

	rec := ssbRec("M0ABC", "K1ABC")
	rec.ContactedStation.Country = strings.Repeat("C", 51) // 51 > the schema's trimmed 50
	_, err := s.Submit(context.Background(), lbID, rec, false)
	require.Error(t, err)
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_field_value", se.Code)
	require.Contains(t, se.Message, "COUNTRY")
}

func TestUpdate_OverlongRstRefused(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)

	_, err = s.Update(ctx, existing, []byte(`{"rst_sent":"59999999999"}`), source.API)
	require.Error(t, err, "LV4: the update path shares the limit")
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_field_value", se.Code)
}

// LV5 (codex bb04a661 P2): the caps mirror SQLite length(), which counts
// CHARACTERS, not bytes — so the validator must count runes. A 50-rune
// country of two-byte characters (100 bytes) satisfies the schema's
// length(trim(country)) <= 50 and must be ACCEPTED; the byte-counting first
// implementation rejected it, refusing values the database and every prior
// build accepted. The fixture stores through the REAL DB, so its acceptance
// also measures the external claim about length() semantics.
func TestSubmit_CountryLengthCountsCharactersNotBytes(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	rec := ssbRec("M0ABC", "K1ABC")
	rec.ContactedStation.Country = strings.Repeat("é", 50) // 50 chars, 100 bytes
	res, err := s.Submit(ctx, lbID, rec, false)
	require.NoError(t, err, "LV5: 50 characters satisfies the schema CHECK — a byte count "+
		"rejects what the database accepts")
	require.Equal(t, "stored", res.Status)

	rec = ssbRec("M0ABC", "K2DEF")
	rec.ContactedStation.Country = strings.Repeat("é", 51) // 51 chars — over the cap
	_, err = s.Submit(ctx, lbID, rec, false)
	require.Error(t, err, "51 characters exceeds the CHECK regardless of encoding")
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_field_value", se.Code)
}

// ---- Finding 4: time coherence covers the non-decreasing direction -----------

func TestSubmit_DateOffMustMatchQsoDateWhenTimesDoNotDecrease(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	mk := func(timeOn, timeOff, dateOff string) adif.Record {
		r := ssbRec("M0ABC", "K1ABC")
		r.QsoDetails.TimeOn = timeOn
		r.QsoDetails.TimeOff = timeOff
		r.QsoDetails.QsoDateOff = dateOff
		return r
	}

	// TC1: QSO_DATE_OFF years BEFORE QSO_DATE, times increasing — the old check
	// never looked at this direction at all.
	_, err := s.Submit(ctx, lbID, mk("1200", "1300", "20200101"), false)
	require.Error(t, err, "TC1: an end date before the start date is impossible")
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_time_range", se.Code)

	// TC2: QSO_DATE_OFF AFTER QSO_DATE with increasing times — a full-day span
	// only exists via the overnight rule, which requires TIME_ON after TIME_OFF.
	_, err = s.Submit(ctx, lbID, mk("1200", "1300", "20260102"), false)
	require.Error(t, err, "TC2: a 25-hour QSO is not a state, it's a typo")
	se = IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_time_range", se.Code)

	// Guard: the genuine overnight case stays accepted.
	res, err := s.Submit(ctx, lbID, mk("2350", "0010", "20260102"), false)
	require.NoError(t, err, "the overnight wrap is the legitimate case the rule must keep")
	require.Equal(t, "stored", res.Status)
}

func TestSubmit_SecondsAwareWhenBothTimesCarrySeconds(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	// TC3: both HHMMSS, same minute, ON after OFF, same day — the old minute
	// truncation stored a negative 59-second interval.
	r := ssbRec("M0ABC", "K1ABC")
	r.QsoDetails.TimeOn = "120059"
	r.QsoDetails.TimeOff = "120000"
	_, err := s.Submit(ctx, lbID, r, false)
	require.Error(t, err, "TC3: seconds are preserved in storage, so the check must see them")
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_time_range", se.Code)

	// TC4: mixed precision in the same minute stays accepted in BOTH directions —
	// HHMM names the minute, not second :00 (the preserveSeconds reading).
	r = ssbRec("M0ABC", "K2DEF")
	r.QsoDetails.TimeOn = "120059"
	r.QsoDetails.TimeOff = "1200"
	res, err := s.Submit(ctx, lbID, r, false)
	require.NoError(t, err, "TC4: a minute-precision TIME_OFF against a seconds TIME_ON is a "+
		"QRZ-style minute log, not a negative interval")
	require.Equal(t, "stored", res.Status)

	r = ssbRec("M0ABC", "K3GHI")
	r.QsoDetails.TimeOn = "1201"
	r.QsoDetails.TimeOff = "120159"
	res, err = s.Submit(ctx, lbID, r, false)
	require.NoError(t, err)
	require.Equal(t, "stored", res.Status)
}

func TestUpdate_DateOffMustMatchQsoDateWhenTimesDoNotDecrease(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	res, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	existing, err := s.DB.FetchQsoByUUIDWithContext(ctx, res.UUID)
	require.NoError(t, err)

	_, err = s.Update(ctx, existing, []byte(`{"qso_date_off":"20200101"}`), source.API)
	require.Error(t, err, "TC5: the PATCH path shares the rule")
	se := IsSubmitError(err)
	require.NotNil(t, se)
	require.Equal(t, "invalid_time_range", se.Code)
}

// ---- Finding 5: restore's existence probe and insert-collision ---------------

func TestRestore_ProbeFaultIsAttributedToTheCheck(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")

	require.NoError(t, s.DB.Close(), "fixture: a real unreadable datastore, not a mock")

	_, err := s.Restore(context.Background(), lbID, restorableQso(utils.NewUUIDv7(),
		time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC)))
	require.Error(t, err, "RS1: a probe fault must surface, never classify as absence")
	require.ErrorContains(t, err, "existence check",
		"the fault is the PROBE's — the old fall-through attributed it to the insert "+
			"('insert restored qso'), sending diagnosis to the wrong operation")
}

func TestRestore_ClassifyInsertErr(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")
	ctx := context.Background()

	uuid := utils.NewUUIDv7()
	q := restorableQso(uuid, time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC))
	q.LogbookID = lbID
	// The direct-DB insert bypasses Restore's defaulting and dedupe-key
	// recompute; satisfy the schema CHECKs (time_off well-formed, 64-char key)
	// the way Restore itself would.
	q.TimeOff = q.TimeOn
	q.DedupeKey = ComputeDedupeKey(q.Call, q.Band, q.Mode, "14255", q.QsoDate, utils.TimeToHHMM(q.TimeOn))
	_, err := s.DB.InsertRestoredQsoWithContext(ctx, q)
	require.NoError(t, err, "fixture: the row commits — the racing winner")

	// The REAL error the check-to-insert race produces: a second insert of the
	// same UUID against the live unique index.
	_, collisionErr := s.DB.InsertRestoredQsoWithContext(ctx, q)
	require.Error(t, collisionErr, "fixture: the loser's insert must actually collide")

	st, err := s.classifyRestoreInsertErr(ctx, uuid, lbID, collisionErr)
	require.NoError(t, err, "RS2: the loser's outcome is the documented idempotent skip, "+
		"not a raw constraint error")
	require.Equal(t, RestoreSkippedExisting, st)

	// Any other fault passes through unchanged, attributed to the insert.
	_, err = s.classifyRestoreInsertErr(ctx, utils.NewUUIDv7(), lbID, fmt.Errorf("disk I/O error"))
	require.Error(t, err)
	require.ErrorContains(t, err, "insert restored qso")
}
