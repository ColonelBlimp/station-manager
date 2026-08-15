package worker

// L11-C3 — Periodic queue-depth summary.
// (docs/reviews/internal-codebase-logging-gaps.md; criteria in queue_context_test.go.)
//
//	When uploads are backed up, the log periodically shows pending depth, oldest-row age and
//	the durable failed total — so a growing backlog is visible before it drains. "draining
//	normally" is tellable from "growing / oldest row aging" across consecutive summaries, and
//	a genuinely-empty queue from a summary that simply did not run.
//
// Rulings 2026-08-15: fixed 60-second interval; emit while backed up, when the failed total
// changes, and ONCE on the transition to empty; suppress steady idle. The failed total is a
// durable DB count (queue_summary uses UploadQueueDepth, tested in the sqlite package).
//
// The emit DECISION TABLE is unit-tested directly against queueSummaryLog: each step's fixture
// differs from the suppressed baseline in exactly the field that should trigger it, so a
// decision that collapsed two cases would be caught. summarizeOnce + the Run ticker wiring are
// covered separately with a real DB.

import (
	"bytes"
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const msgQueueSummary = "forwarding: queue summary"

func depth(pending, failed int64, oldest time.Time) sqlite.UploadQueueDepth {
	return sqlite.UploadQueueDepth{Pending: pending, OldestQueued: oldest, Failed: failed}
}

// C3 decision table: the sequence walks the queue through backed-up, drained, steady-idle and
// a late failure. After each emit the cumulative summary-record count is asserted, so the
// suppress/emit decision for every distinct state is pinned.
func TestQueueSummary_EmitDecisionTable(t *testing.T) {
	buf := &bytes.Buffer{}
	t0 := time.Unix(3_000_000, 0).UTC()
	q := &queueSummaryLog{log: logging.NewForWriter(buf), name: "qrz", now: func() time.Time { return t0 }}

	count := func() int { return len(withMessage(t, buf, msgQueueSummary)) }
	step := func(name string, d sqlite.UploadQueueDepth, want int) {
		q.emit(d)
		if got := count(); got != want {
			t.Fatalf("%s: cumulative summary records = %d, want %d\n%s", name, got, want, buf.String())
		}
	}

	old := t0.Add(-90 * time.Second)
	step("first tick, empty, no failures → suppressed", depth(0, 0, time.Time{}), 0)
	step("backed up (3) → emit", depth(3, 0, old), 1)
	step("still backed up (2) → emit", depth(2, 0, old), 2)
	step("drained to empty → one emit on transition", depth(0, 0, time.Time{}), 3)
	step("steady idle, failed unchanged → suppressed", depth(0, 0, time.Time{}), 3)
	step("a row failed while idle (0→2) → emit", depth(0, 2, time.Time{}), 4)
	step("steady idle, failed unchanged (2) → suppressed", depth(0, 2, time.Time{}), 4)
}

// C3 content: while backed up, the summary carries pending, the durable failed total, and the
// oldest-row age. Age uses the injected clock so it is exact.
func TestQueueSummary_BackedUpRecordCarriesDepthAgeFailed(t *testing.T) {
	buf := &bytes.Buffer{}
	now := time.Unix(3_000_000, 0).UTC()
	q := &queueSummaryLog{log: logging.NewForWriter(buf), name: "qrz", now: func() time.Time { return now }}

	q.emit(depth(5, 2, now.Add(-120*time.Second)))

	recs := withMessage(t, buf, msgQueueSummary)
	if len(recs) != 1 {
		t.Fatalf("summary records = %d, want 1\n%s", len(recs), buf.String())
	}
	rec := recs[0]
	if rec["level"] != "info" {
		t.Errorf("level = %v, want info", rec["level"])
	}
	if p, _ := rec["pending"].(float64); int64(p) != 5 {
		t.Errorf("pending = %v, want 5", rec["pending"])
	}
	if f, _ := rec["failed"].(float64); int64(f) != 2 {
		t.Errorf("failed = %v, want 2", rec["failed"])
	}
	if a, _ := rec["oldest_age_seconds"].(float64); int64(a) != 120 {
		t.Errorf("oldest_age_seconds = %v, want 120", rec["oldest_age_seconds"])
	}
}

// C3 defence-in-depth: a backed-up depth whose OldestQueued is somehow the zero time (the
// store's atomic snapshot makes this impossible, but a future regression could) must NOT log
// an age computed from year 0001 — it omits oldest_age_seconds rather than logging ~60e9.
func TestQueueSummary_ZeroOldestOmitsAge(t *testing.T) {
	buf := &bytes.Buffer{}
	now := time.Unix(3_000_000, 0).UTC()
	q := &queueSummaryLog{log: logging.NewForWriter(buf), name: "qrz", now: func() time.Time { return now }}

	q.emit(depth(3, 0, time.Time{})) // backed up, but zero oldest

	recs := withMessage(t, buf, msgQueueSummary)
	if len(recs) != 1 {
		t.Fatalf("summary records = %d, want 1\n%s", len(recs), buf.String())
	}
	if _, ok := recs[0]["oldest_age_seconds"]; ok {
		t.Errorf("oldest_age_seconds present with a zero OldestQueued — would be a nonsensical age")
	}
}

// C3 wiring: summarizeOnce reads the real store and emits. Two pending rows for this forwarder
// → one summary record with pending=2. Proves store→emit are actually connected.
func TestQueueSummary_SummarizeOnceReadsStoreAndEmits(t *testing.T) {
	h, buf := captureHarness(t)
	w := newReachWorker(t, h)

	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Update) // 2 pending rows, one forwarder

	w.summarizeOnce(context.Background())

	recs := withMessage(t, buf, msgQueueSummary)
	if len(recs) != 1 {
		t.Fatalf("summary records = %d, want 1\n%s", len(recs), buf.String())
	}
	if p, _ := recs[0]["pending"].(float64); int64(p) != 2 {
		t.Errorf("pending = %v, want 2", recs[0]["pending"])
	}
	if _, ok := recs[0]["oldest_age_seconds"]; !ok {
		t.Errorf("backed-up summary carries no oldest_age_seconds\n%s", buf.String())
	}
}

// C3 wiring: RunSummary actually fires its ticker, INDEPENDENTLY of the claim loop. A row is
// enqueued (already pending) and only RunSummary is run — proving the summary is a peer
// goroutine that reports the backlog without Run needing to make progress (which is the whole
// point: it must fire even while a slow Submit blocks the claim loop). Guards against "forgot
// to wire the ticker".
func TestQueueSummary_RunSummaryTickerFires(t *testing.T) {
	h, buf := captureHarness(t)
	cfg := defaultCfg("stub")
	cfg.SummaryInterval = 20 * time.Millisecond
	w, err := New(cfg, buildStub(t, stub.ModeAlwaysSuccess, 0), h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert) // pending

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.RunSummary(ctx); close(done) }()
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	recs := withMessage(t, buf, msgQueueSummary)
	if len(recs) == 0 {
		t.Fatalf("no queue-summary records — RunSummary's ticker did not fire\n%s", buf.String())
	}
	if p, _ := recs[0]["pending"].(float64); int64(p) < 1 {
		t.Errorf("summary pending = %v, want >= 1", recs[0]["pending"])
	}
}

// blockingForwarder holds Submit until released, modelling a slow/hung upstream.
type blockingForwarder struct{ release chan struct{} }

func (blockingForwarder) Type() string       { return stub.Type }
func (blockingForwarder) AdifPrefix() string { return "" }
func (b blockingForwarder) Submit(ctx context.Context, _ types.Qso, _ forwarding.Action, _ string) forwarding.Result {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return forwarding.Result{Outcome: forwarding.OutcomeUnreachable, Err: stderrors.New("released")}
}

// C3 — the load-bearing rule (P2 fix): the summary must fire even WHILE the claim loop is
// blocked on a Submit, since that is exactly when the backlog needs surfacing. Batch=1 claims
// one row (whose Submit blocks), leaving a second row pending; both loops run; a summary must
// appear during the block. An implementation that put the summary on the claim loop's select
// would starve here (the loop cannot reach the summary tick while stuck in Submit).
func TestQueueSummary_FiresWhileClaimLoopBlocked(t *testing.T) {
	h, buf := captureHarness(t)
	cfg := defaultCfg("stub")
	cfg.Batch = 1 // claim one; the other row stays pending to be reported
	cfg.SummaryInterval = 20 * time.Millisecond
	fwd := blockingForwarder{release: make(chan struct{})}
	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Update) // 2 pending rows

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); w.Run(ctx) }()        // claims row 1, then BLOCKS in Submit
	go func() { defer wg.Done(); w.RunSummary(ctx) }() // must still fire
	time.Sleep(150 * time.Millisecond)                 // summaries accumulate WHILE Run is blocked

	// Join the goroutines before touching buf — the bytes.Buffer is not safe to read while the
	// logger writes it. Cancelling stops both loops (and unblocks Submit via ctx.Done); the
	// summaries emitted during the block above are already in buf. RunSummary exits on ctx
	// rather than firing again, so anything here fired during the block.
	close(fwd.release)
	cancel()
	wg.Wait()

	if got := len(withMessage(t, buf, msgQueueSummary)); got == 0 {
		t.Fatalf("no summary while the claim loop was blocked on Submit — the summary is coupled "+
			"to the blocking claim loop\n%s", buf.String())
	}
}
