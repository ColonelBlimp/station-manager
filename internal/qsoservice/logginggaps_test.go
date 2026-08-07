package qsoservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/enums/source"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

/*
	qsoservice logging gaps Q1–Q3 (docs/reviews/qsoservice-logging-gaps.md,
	Tier 1) — specified before the implementation, 2026-08-08 session of
	2026-08-07. Each rule is stated as its finding's CONFUSABLE PAIR: the two
	states that produced identical log output, where telling them apart is what
	the record exists for. Per the audit's own instruction, every test asserts
	that the confusable states produce DISTINGUISHABLE output — "a line was
	emitted" is weaker than the rule.

	Q1 — Restore is the disaster-recovery path and logged NOTHING on either
	     outcome. Confusable: an idempotent re-run (every row skipped_existing)
	     vs a real recovery (every row stored) — identical silence, and telling
	     those apart is the entire question during a recovery. Per-call at
	     Debug (a restore is a bulk loop; the run summary is the cmd/smd loop's
	     Info line).
	Q2 — EnqueueUploads logged 2 of its 5 outcome counts, and the zero-enqueue
	     early return logged NOTHING — the pure ClubLog-compliance case (every
	     row refused skipped_no_history to honour the 2026-07-19 realtime.php
	     grant condition) left no record that SM refused on purpose.
	     Confusable: all-refused vs never-invoked (same silence), and
	     "nothing was refused" vs "300 were refused" (same line). The log must
	     fire on EVERY return path with the requested selection size and all
	     five counts (lengths only — the UUID lists stay out of Info).
	Q3 — A duplicate submit was silent while a stored submit logs, which is
	     backwards for diagnosis: the operator saw the rejection and is the one
	     asking. Confusable: a refused duplicate vs a submit never attempted.
	     The refusal logs at Info with the colliding row's identity.

	Q2 correction (found during the api A2 round, 2026-08-08 session of
	2026-08-07): EnqueueUploads has TWO callers — the manual-backfill handler
	(origin manual) and the SM Cloud reconciler's heal path (origin reconcile) —
	and the Q2 line hardcoded "manual upload backfill result" for both, so a
	reconcile heal logged as an operator press. The delete sibling hardcoded the
	opposite attribution ("(reconcile repair)") and is equally callable with
	origin manual. Confusable: an operator's press vs the reconciler healing
	divergence on its own — the exact who-asked question the org parameter
	exists to answer, discarded one call before the log. The rule: the outcome
	line CARRIES its origin, and the message asserts no attribution the field
	could contradict.
*/

// logbuf swaps the service's logger for a buffer-backed one, so assertions
// run against exactly what the package would have written to smd.log.
func logbuf(s *Service) *strings.Builder {
	var buf strings.Builder
	s.Logger = logging.NewForWriter(&buf)
	return &buf
}

// --- Q1: restore outcomes are distinguishable ---------------------------------

func TestRestore_OutcomesAreDistinguishableInTheLog(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "7Q5MLV")
	buf := logbuf(s)
	ctx := context.Background()

	uuid := utils.NewUUIDv7()
	q := restorableQso(uuid, time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC))

	st, err := s.Restore(ctx, lbID, q)
	require.NoError(t, err)
	require.Equal(t, RestoreStored, st, "fixture: first restore stores")

	st, err = s.Restore(ctx, lbID, q)
	require.NoError(t, err)
	require.Equal(t, RestoreSkippedExisting, st, "fixture: re-run skips")

	out := buf.String()
	require.Contains(t, out, `"outcome":"stored"`,
		"a stored row must be visible in the log — a recovery that leaves no record "+
			"is confusable with a restore that did nothing")
	require.Contains(t, out, `"outcome":"skipped_existing"`,
		"an idempotent skip must be visible AND distinguishable from a store — "+
			"identical silence was the finding")
	require.Contains(t, out, uuid, "the row's identity is the record's point")
	require.Contains(t, out, `"level":"debug"`,
		"per-call lines belong at Debug — a restore is a bulk loop")
}

// --- Q2: enqueue logs every return path, all five counts ----------------------

func TestEnqueueUploads_AllRefusedStillLogs(t *testing.T) {
	s := newTestService(t, enabledClublog())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	u1, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	buf := logbuf(s)

	res, err := s.EnqueueUploads(context.Background(), "clublog", []string{u1}, false, origin.Manual)
	require.NoError(t, err)
	require.Zero(t, res.Enqueued, "fixture: the pure ClubLog-compliance case — everything refused")
	require.Equal(t, []string{u1}, res.SkippedNoHistory)

	out := buf.String()
	require.Contains(t, out, `"requested":1`,
		"the selection size is what makes 'I selected N and nothing happened' answerable")
	require.Contains(t, out, `"skipped_no_history":1`,
		"SM refused these to honour a written commitment to ClubLog; the refusal "+
			"used to exist only in a browser response — all-refused and never-invoked "+
			"were the same silence")
}

func TestEnqueueUploads_LogCarriesAllFiveOutcomes(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	ctx := context.Background()

	u1, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200") // → enqueued
	u2, _ := seedStoredQso(t, s, lbID, "K2BBB", "1201") // → skipped_deleted
	del, err := s.DB.FetchQsoByUUIDWithContext(ctx, u2)
	require.NoError(t, err)
	require.NoError(t, s.Delete(ctx, del, source.Source("test")))
	buf := logbuf(s)

	res, err := s.EnqueueUploads(ctx, "qrz", []string{u1, u2, "not-a-uuid"}, false, origin.Manual)
	require.NoError(t, err)
	require.Equal(t, 1, res.Enqueued, "fixture: one row of each outcome class in play")
	require.Equal(t, []string{u2}, res.SkippedDeleted)
	require.Equal(t, []string{"not-a-uuid"}, res.NotFound)

	// All five counts on one line — {enqueued:12} and {enqueued:12,
	// skipped_no_history:300} were the same line before this.
	out := buf.String()
	require.Contains(t, out, `"requested":3`)
	require.Contains(t, out, `"enqueued":1`)
	require.Contains(t, out, `"skipped_uploaded":0`)
	require.Contains(t, out, `"skipped_deleted":1`)
	require.Contains(t, out, `"not_found":1`)
	require.Contains(t, out, `"skipped_no_history":0`)
	require.NotContains(t, out, u2,
		"UUID lists stay OUT of the Info line — lengths only (the finding's own bound)")
}

func TestEnqueueDeleteUploads_ZeroEnqueueStillLogs(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)

	res, err := s.EnqueueDeleteUploads(context.Background(), "qrz", []string{"not-a-uuid"}, origin.Manual)
	require.NoError(t, err)
	require.Zero(t, res.Enqueued, "fixture: nothing enqueueable")
	require.Equal(t, []string{"not-a-uuid"}, res.NotFound)

	out := buf.String()
	require.Contains(t, out, `"requested":1`,
		"the delete-repair path's zero-enqueue return was equally silent")
	require.Contains(t, out, `"not_found":1`,
		"NotFound was omitted from the delete-path line even when it did log")
}

// --- Q2 correction: the outcome line names its origin --------------------------

func TestEnqueueUploads_LogNamesItsOrigin(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	u1, _ := seedStoredQso(t, s, lbID, "K1AAA", "1200")
	u2, _ := seedStoredQso(t, s, lbID, "K2BBB", "1201")
	buf := logbuf(s)
	ctx := context.Background()

	_, err := s.EnqueueUploads(ctx, "qrz", []string{u1}, false, origin.Manual)
	require.NoError(t, err)
	_, err = s.EnqueueUploads(ctx, "qrz", []string{u2}, true, origin.Reconcile)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, `"origin":"manual"`,
		"the operator's press names itself")
	require.Contains(t, out, `"origin":"reconcile"`,
		"a reconciler heal is NOT an operator press — identical lines for the two "+
			"was the defect")
	require.NotContains(t, out, "manual upload backfill",
		"the message must assert no attribution the origin field could contradict — "+
			"a reconcile heal logged as 'manual upload backfill result'")
}

func TestEnqueueDeleteUploads_LogNamesItsOrigin(t *testing.T) {
	s := newTestService(t, enabledQRZ())
	seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)

	_, err := s.EnqueueDeleteUploads(context.Background(), "qrz", []string{"not-a-uuid"}, origin.Manual)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, `"origin":"manual"`,
		"the delete-repair line carries who asked")
	require.NotContains(t, out, "reconcile repair",
		"the sibling defect mirrored: a manual delete repair logged as the reconciler's")
}

// --- Q3: a refused duplicate leaves a record ----------------------------------

func TestSubmit_DuplicateRefusalIsLogged(t *testing.T) {
	s := newTestService(t)
	lbID := seedLogbook(t, s, "Main", "M0ABC")
	buf := logbuf(s)
	ctx := context.Background()

	first, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	require.Equal(t, "stored", first.Status, "fixture: the original stores")

	dup, err := s.Submit(ctx, lbID, ssbRec("M0ABC", "K1ABC"), false)
	require.NoError(t, err)
	require.Equal(t, "duplicate", dup.Status, "fixture: the repeat is refused")

	out := buf.String()
	require.Contains(t, out, "QSO duplicate refused",
		"the refusal must be visible — the operator saw the rejection and is the "+
			"one who will ask; today the written QSO logs and the refused one doesn't, "+
			"which is backwards for diagnosis")
	require.Contains(t, out, first.UUID,
		"the colliding row's identity is what answers 'which contact did it hit?'")
	require.Contains(t, out, "K1ABC", "the refused submission's call is named")
	require.Equal(t, 1, strings.Count(out, "QSO stored"),
		"exactly one store — the refusal is a DIFFERENT message, not a second store line")
}
