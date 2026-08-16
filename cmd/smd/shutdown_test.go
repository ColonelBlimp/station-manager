package main

// LC-2 — the graceful-shutdown deadline must bound the WHOLE teardown, not just the
// tail. Before this change the shutdown budget was constructed AFTER the synchronous
// bridge/ft8/psk/evidence Stop() calls, so a single wedged Stop() blocked forever and
// the daemon never reached HTTP shutdown, the worker/QSO drains, or a closing record.
//
// Acceptance criteria (operator-observable; stated before the mechanism):
//
//   AC-1  When a subsystem Stop() blocks past the budget, the daemon emits ONE
//         structured record NAMING that stage, and still returns (it does not hang) —
//         distinct from a clean shutdown, which emits no such record.
//   AC-2  Total graceful teardown is bounded close to the configured budget, not
//         unbounded — distinct from the pre-LC-2 behaviour, which hung indefinitely.
//   AC-3  The bridge RF-unkey is ATTEMPTED even when a later stage blocks — distinct
//         from a timeout that fires before the unkey is launched.
//   AC-4  A clean shutdown runs every stage and emits NO exceeded/skip record —
//         distinct from the blocked cases, so a spurious warning can't hide here.
//   AC-5  packaging/smd.service declares a TimeoutStopSec backstop (absolute systemd
//         cap) greater than the app budget.
//
// Dependency preservation (operator ruling): after the budget expires, independent
// cleanup (HTTP shutdown, psk) is still attempted, but a dependent stage whose
// prerequisite did not stop is SKIPPED, not attempted — closing the evidence archive
// under a live ft8 producer, Wait()ing a WaitGroup ft8 may still Add() to, or closing
// hub channels a live publisher may still send on, are races/panics. The nearest
// confusable state is "skipped" vs "ran and failed"; the skip is recorded naming the
// unmet prerequisite.
//
// Reversion proof: replacing gracefulShutdown's body with the pre-LC-2 sequence
// (sequential unbounded Stops, budget built afterwards, unconditional hub.Close) makes
// the blocking tests hang → their bounded-return assertion fails, and the named-stage
// assertions find no record. See the reversion note in the session log.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const (
	msgShutdownExceeded = "graceful shutdown exceeded budget; remaining dependent teardown abandoned"
	msgShutdownSkipped  = "graceful shutdown stage skipped; prerequisite did not stop"
)

// syncBuffer is a concurrency-safe log sink: stage error logs (were any emitted) run
// on the stage goroutines while the deadline/skip records are written on the main
// teardown goroutine, so a plain bytes.Buffer would race under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// stageProbe records whether a teardown seam was invoked and can block on demand to
// model a wedged Stop().
type stageProbe struct {
	mu     sync.Mutex
	called bool
	block  chan struct{} // non-nil ⇒ the seam blocks until the channel is closed
}

func (p *stageProbe) fire() {
	p.mu.Lock()
	p.called = true
	blk := p.block
	p.mu.Unlock()
	if blk != nil {
		<-blk
	}
}

func (p *stageProbe) wasCalled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.called
}

type shutdownHarness struct {
	t         *testing.T
	buf       *syncBuffer
	budget    time.Duration
	bridge    *stageProbe
	ft8       *stageProbe
	psk       *stageProbe
	evidence  *stageProbe
	httpP     *stageProbe
	hub       *stageProbe
	workerWG  sync.WaitGroup
	qsoLogWG  sync.WaitGroup
	toRelease []chan struct{}
}

func newShutdownHarness(t *testing.T, budget time.Duration) *shutdownHarness {
	t.Helper()
	h := &shutdownHarness{
		t:        t,
		buf:      &syncBuffer{},
		budget:   budget,
		bridge:   &stageProbe{},
		ft8:      &stageProbe{},
		psk:      &stageProbe{},
		evidence: &stageProbe{},
		httpP:    &stageProbe{},
		hub:      &stageProbe{},
	}
	t.Cleanup(func() {
		for _, ch := range h.toRelease {
			close(ch) // let any blocked seam goroutine finish and exit
		}
	})
	return h
}

// blockByName wedges the named stage: its seam blocks until test cleanup.
func (h *shutdownHarness) blockByName(stage string) {
	h.t.Helper()
	p := map[string]*stageProbe{
		"bridge":      h.bridge,
		"ft8":         h.ft8,
		"pskreporter": h.psk,
		"evidence":    h.evidence,
	}[stage]
	if p == nil {
		h.t.Fatalf("blockByName: unknown stage %q", stage)
	}
	ch := make(chan struct{})
	p.mu.Lock()
	p.block = ch
	p.mu.Unlock()
	h.toRelease = append(h.toRelease, ch)
}

func (h *shutdownHarness) deps() teardownDeps {
	return teardownDeps{
		log:          logging.NewForWriter(h.buf),
		budget:       h.budget,
		stopBridge:   func() error { h.bridge.fire(); return nil },
		stopFt8:      func() error { h.ft8.fire(); return nil },
		stopPsk:      func() error { h.psk.fire(); return nil },
		stopEvidence: func() { h.evidence.fire() },
		shutdownHTTP: func(context.Context) error { h.httpP.fire(); return nil },
		workerWG:     &h.workerWG,
		qsoLogWG:     &h.qsoLogWG,
		closeHub:     func() { h.hub.fire() },
	}
}

// runBounded runs gracefulShutdown in a goroutine and fails if it does not return
// within within (the pre-LC-2 behaviour hangs; this is the AC-2 bound).
func (h *shutdownHarness) runBounded(within time.Duration) {
	h.t.Helper()
	done := make(chan struct{})
	go func() {
		gracefulShutdown(h.deps())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(within):
		h.t.Fatalf("gracefulShutdown did not return within %v; teardown is unbounded", within)
	}
}

func (h *shutdownHarness) records() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			h.t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		out = append(out, rec)
	}
	return out
}

func (h *shutdownHarness) exceededRecords() []map[string]any {
	h.t.Helper()
	var out []map[string]any
	for _, rec := range h.records() {
		if rec["message"] == msgShutdownExceeded {
			out = append(out, rec)
		}
	}
	return out
}

func (h *shutdownHarness) skipRecord(stage string) map[string]any {
	h.t.Helper()
	for _, rec := range h.records() {
		if rec["message"] == msgShutdownSkipped && rec["stage"] == stage {
			return rec
		}
	}
	return nil
}

// waitCalled polls until p.wasCalled() (a post-expiry independent stage is launched in
// a goroutine, so its flag is set slightly after gracefulShutdown returns).
func waitCalled(t *testing.T, p *stageProbe, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.wasCalled() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// AC-4: a clean shutdown runs every stage and emits no exceeded/skip record.
func TestGracefulShutdown_CleanRunCompletesAllStagesNoRecord(t *testing.T) {
	h := newShutdownHarness(t, 2*time.Second)
	h.runBounded(2 * time.Second)

	for name, p := range map[string]*stageProbe{
		"bridge": h.bridge, "ft8": h.ft8, "pskreporter": h.psk,
		"evidence": h.evidence, "http": h.httpP, "hub": h.hub,
	} {
		if !p.wasCalled() {
			t.Errorf("stage %q was not run on a clean shutdown", name)
		}
	}
	if n := len(h.exceededRecords()); n != 0 {
		t.Errorf("clean shutdown emitted %d exceeded-budget records, want 0", n)
	}
	for _, rec := range h.records() {
		if rec["message"] == msgShutdownSkipped {
			t.Errorf("clean shutdown emitted a skip record: %v", rec)
		}
	}
}

// AC-1: a blocked subsystem Stop() is named in exactly ONE exceeded-budget record,
// and the daemon still returns (bounded). Table over the four synchronous Stop seams.
func TestGracefulShutdown_BlockedStageNamedInSingleRecord(t *testing.T) {
	for _, stage := range []string{"bridge", "ft8", "pskreporter", "evidence"} {
		t.Run(stage, func(t *testing.T) {
			h := newShutdownHarness(t, 120*time.Millisecond)
			h.blockByName(stage)
			h.runBounded(2 * time.Second) // AC-2: bounded despite the wedge

			exc := h.exceededRecords()
			if len(exc) != 1 {
				t.Fatalf("want exactly 1 exceeded-budget record, got %d: %v", len(exc), h.records())
			}
			if exc[0]["stage"] != stage {
				t.Errorf("exceeded record names stage %v, want %q", exc[0]["stage"], stage)
			}
		})
	}
}

// AC-2: with a stage wedged forever, total teardown is bounded close to the budget.
func TestGracefulShutdown_TotalTeardownBoundedByBudget(t *testing.T) {
	h := newShutdownHarness(t, 150*time.Millisecond)
	h.blockByName("ft8")

	start := time.Now()
	h.runBounded(2 * time.Second)
	if elapsed := time.Since(start); elapsed > 800*time.Millisecond {
		t.Errorf("teardown took %v; must be bounded close to the %v budget", elapsed, h.budget)
	}
}

// AC-3: the bridge RF-unkey is attempted even when a later stage wedges the budget.
func TestGracefulShutdown_RFUnkeyAttemptedWhenLaterStageBlocks(t *testing.T) {
	h := newShutdownHarness(t, 120*time.Millisecond)
	h.blockByName("ft8") // ft8 is stage 2 — bridge (stage 1) must already have run
	h.runBounded(2 * time.Second)

	if !h.bridge.wasCalled() {
		t.Error("bridge Stop (RF unkey) was not attempted; a timeout must never skip it")
	}
}

// Dependency preservation: ft8 hangs → evidence, the FT8 QSO-log drain, and hub are
// SKIPPED (naming ft8), while the independent stages (HTTP, psk) are still attempted.
func TestGracefulShutdown_Ft8HangSkipsDependentsRunsIndependents(t *testing.T) {
	h := newShutdownHarness(t, 120*time.Millisecond)
	h.blockByName("ft8")
	h.runBounded(2 * time.Second)

	// Independent cleanup is still attempted (launched post-expiry, so poll).
	waitCalled(t, h.psk, "pskreporter (independent) must still be attempted when ft8 hangs")
	waitCalled(t, h.httpP, "HTTP shutdown (independent) must still be signalled when ft8 hangs")

	// Dependents must NOT run.
	if h.evidence.wasCalled() {
		t.Error("evidence must be SKIPPED when ft8 did not stop (it is the only producer)")
	}
	if h.hub.wasCalled() {
		t.Error("hub must NOT be closed when ft8 did not stop (live publisher ⇒ send-on-closed panic)")
	}

	// Each skip is recorded naming ft8 as the unmet prerequisite.
	for _, stage := range []string{"evidence", "ft8-qso-log", "hub"} {
		rec := h.skipRecord(stage)
		if rec == nil {
			t.Fatalf("no skip record for stage %q; records=%v", stage, h.records())
		}
		if rec["prerequisite"] != "ft8" {
			t.Errorf("stage %q skip names prerequisite %v, want ft8", stage, rec["prerequisite"])
		}
	}
}

// AC-5: the packaged unit declares an absolute systemd TimeoutStopSec backstop (20s
// per the LC-2 ruling) above the default 10s app budget.
func TestSmdService_DeclaresTimeoutStopSecBackstop(t *testing.T) {
	b, err := os.ReadFile("../../packaging/smd.service")
	if err != nil {
		t.Fatalf("read smd.service: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "TimeoutStopSec=20") {
		t.Errorf("smd.service must declare TimeoutStopSec=20 (absolute systemd cap); got:\n%s", s)
	}
}
