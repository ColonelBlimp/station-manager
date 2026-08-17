package evidence

// LC-3 (docs/reviews/internal-lifecycle-concurrency-audit.md) — evidence's Stop was not an
// atomic producer cutoff, not a concurrent completion barrier, and not terminal. The split
// closed/started flags let a slot admitted past the lock-free closed check land in the buffer
// AFTER the writer's final drain check (silently lost, pending leaked); a second concurrent
// Stop caller returned before the first had drained and closed the archive; and a Start after
// Stop re-opened the archive that the latched Stop would then never tear down. These tests pin
// the fix — one mutex-guarded lifecycle (idle→running→stopped), an in-flight producer group,
// and stopOnce/stopDone (the internal/bridge + internal/ft8 pattern).
//
// Acceptance criteria (operator-observable; drafted before the mechanism, operator rulings
// 2026-08-17):
//
//   AC-1  A slot offered as Stop runs is EITHER fully captured (its rows land in evidence.db)
//         OR cleanly refused (no rows, not counted) — never admitted-then-abandoned. Observable:
//         pending == 0 after Stop, and an admitted slot is actually written. Confusable state
//         broken: a slot enqueued after the drain's empty check, so the stop summary reports a
//         clean drain while the slot has vanished and pending is leaked.
//   AC-2  Concurrent Stop callers each return only AFTER the archive is closed (the single
//         "evidence: stopped" summary is logged). Confusable state broken: a second caller
//         returning early — treating the service as stopped — while the first caller's
//         drain/close is still in flight.
//   AC-3  Stop before Start is terminal: no evidence.db file, no worker goroutine, and a later
//         Start is refused (silent nil no-op, opens nothing, Status disabled). Confusable state
//         broken: a Stop-then-Start that re-opens the archive + spawns workers the latched Stop
//         never tears down (file + goroutine leak).
//   AC-4  Many producers racing a Stop: no data race, no send-on-closed panic, pending == 0.
//         The timing-dependent interleaving AC-1 pins at one point, swept across many. (The
//         finding notes LC-3 uses race-free primitives, so -race alone need not flag it; the
//         teeth here are no-panic + no-deadlock + pending == 0.)
//
// Reversion proofs (each reverts ONE mechanism, keeping the seams, and must go RED for its own
// reason — verified 2026-08-17):
//   AC-1: delete teardown's `s.producers.Wait()` → Stop completes while the admitted slot is
//         still mid-enqueue; the window observes the early return.
//   AC-2: replace the stopOnce/stopDone barrier with the pre-LC-3 `closed.Swap(true)` early
//         return → non-owner callers return before the owner logs the summary; the window sees
//         an early return.
//   AC-3: relax Start's guard from `s.life != evIdle` back to `s.life == evRunning` → Start
//         after Stop re-opens the archive; the file appears.

import (
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// AC-1: the producer cutoff is atomic — Stop must not COMPLETE while a slot it admitted is
// still mid-enqueue; it waits for that send to land in the buffer before draining. (The fix
// makes the loss interleaving — a send landing after the writer's final empty check —
// UNREACHABLE, so it cannot be forced by seam-synchronization; the distinguishing observable
// is instead that Stop blocks until the in-flight producer's send lands. The producer is held
// OBSERVABLY, via <-enqEntered, before the window starts, so scheduler delay cannot false-pass.)
func TestCaptureSlot_AdmittedSlotSurvivesConcurrentStop(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	enqEntered := make(chan struct{})
	enqRelease := make(chan struct{})
	captureEnqueueStallForTest = func() { close(enqEntered); <-enqRelease }
	defer func() { captureEnqueueStallForTest = nil }()

	// A slot the writer WOULD commit (fresh archive: capacity available, queue not full).
	go s.CaptureSlot(richSlot(slotAt(0)))
	<-enqEntered // admitted (in-flight producer group), stalled just before the send

	stopReturned := make(chan struct{})
	go func() { s.Stop(); close(stopReturned) }()

	// While the admitted slot is still mid-enqueue, Stop must not return: a non-atomic cutoff
	// would drain and close now, and the held send would then land in a dead queue.
	select {
	case <-stopReturned:
		t.Fatal("Stop returned while an admitted slot was still mid-enqueue — its send lands in a dead queue")
	case <-time.After(200 * time.Millisecond): // same window class as AC-2 (operator-ruled 2026-08-17)
	}

	close(enqRelease) // let the admitted slot's send land
	<-stopReturned    // Stop now drains it and returns

	if p := s.pending.Load(); p != 0 {
		t.Fatalf("pending = %d after Stop, want 0 — an admitted slot was abandoned in a dead queue", p)
	}
	raw := openRaw(t, cfg.Path)
	if n := countRows(t, raw, `SELECT COUNT(*) FROM coverage`); n != 1 {
		t.Fatalf("coverage rows = %d, want 1 — the admitted slot was not captured", n)
	}
}

// AC-1 (refusal half, operator ruling): a slot offered AFTER Stop has sealed admission is
// silently ignored — not written, not counted. Characterization of the "uncounted" ruling
// (passes old and new code alike; it pins the decision, not the race).
func TestCaptureSlot_OfferedAfterStopIsCleanlyRefused(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)
	s.Stop() // seals evStopped, drains, closes

	before := s.pending.Load()
	s.CaptureSlot(richSlot(slotAt(0))) // must be a no-op
	if p := s.pending.Load(); p != before {
		t.Fatalf("pending moved %d → %d — a slot offered after Stop was admitted", before, p)
	}
	if st := s.Status(); st.DroppedSlots != 0 {
		t.Errorf("dropped_slots = %d, want 0 — a post-Stop slot must not be counted", st.DroppedSlots)
	}
}

// AC-2: three concurrent Stop callers all wait for the teardown owner. The owner is held
// OBSERVABLY inside teardown (via <-entered) before the absence window starts, so a scheduler
// delay cannot make a caller's early return escape the window (operator requirement 2026-08-17).
func TestStop_ConcurrentCallersAllWaitForTeardownOwner(t *testing.T) {
	sb := &syncBuf{}
	cfg := testConfig(t, true)
	s := newRunningSyncLogged(t, cfg, sb)

	entered := make(chan struct{})
	release := make(chan struct{})
	teardownStallForTest = func() { close(entered); <-release }
	defer func() { teardownStallForTest = nil }()

	const callers = 3
	// Each caller records whether the archive-close summary was already logged when it returned.
	summaryAtReturn := make(chan bool, callers)
	for i := 0; i < callers; i++ {
		go func() { s.Stop(); summaryAtReturn <- len(sbLines(sb, stopMsg)) == 1 }()
	}

	<-entered // the teardown OWNER is inside teardown, stalled — the window cannot false-pass.

	select {
	case <-summaryAtReturn:
		t.Fatal("a concurrent Stop returned while the owner was still in teardown (early return, not a barrier)")
	case <-time.After(200 * time.Millisecond): // operator-ruled window (2026-08-17)
	}

	close(release) // let the owner finish (logs the summary, closes the archive).
	for i := 0; i < callers; i++ {
		if !<-summaryAtReturn {
			t.Error("a Stop caller returned before the archive was closed / summary logged")
		}
	}
	if got := len(sbLines(sb, stopMsg)); got != 1 {
		t.Errorf("evidence-stopped summary logged %d times, want exactly 1", got)
	}
}

// AC-3: Stop before Start is terminal — opens no file, spawns no worker, and a later Start is a
// silent nil no-op that re-opens nothing. No file was ever opened ⇒ the writer/monitor/sync
// workers (spawned only after the archive opens) never started; the goroutine count cross-checks
// it (leaked workers block on quit/ch forever, so they persist rather than settle).
func TestStop_BeforeStartIsTerminal(t *testing.T) {
	cfg := testConfig(t, true)
	s := New(cfg, logging.Noop())
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	base := runtime.NumGoroutine()

	s.Stop() // before Start

	if _, err := os.Stat(cfg.Path); !os.IsNotExist(err) {
		t.Fatalf("evidence.db present after Stop-before-Start (stat err = %v); Stop must open nothing", err)
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start after Stop returned %v, want nil (silent terminal no-op)", err)
	}
	if _, err := os.Stat(cfg.Path); !os.IsNotExist(err) {
		t.Fatalf("Start after Stop created evidence.db; a terminal service must not re-open the archive")
	}
	if st := s.Status(); st.State != StateDisabled {
		t.Errorf("Status.State = %q after Stop-then-Start, want %q", st.State, StateDisabled)
	}
	waitFor(t, "goroutine count to stay at/below the pre-Stop baseline (no workers spawned)",
		func() bool { return runtime.NumGoroutine() <= base })
}

// LC-3 review (codex P2 on bcc5b7cf): the operator-visible capture state must flip WITH the
// admission cutoff. Once teardown has sealed admission (CaptureSlot refuses), Status must not
// still report "capturing" from the stale s.state that the draining writer holds until close.
// The seam holds teardown just past the seal, before the archive closes.
func TestStatus_ReportsDisabledOnceAdmissionIsSealed(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	entered := make(chan struct{})
	release := make(chan struct{})
	teardownStallForTest = func() { close(entered); <-release }
	defer func() { teardownStallForTest = nil }()

	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	<-entered // teardown has sealed admission and is stalled before the archive closes

	if st := s.Status(); st.State != StateDisabled {
		t.Errorf("Status.State = %q while admission is sealed, want %q — capture reported active after the cutoff", st.State, StateDisabled)
	}

	close(release)
	<-stopped
}

// AC-4: many producers racing Stop — race-free, panic-free, lossless. Run under -race (CI does).
// The trailing synchronous Stop is the completion barrier: it blocks until the teardown owner
// finished, so pending is read only after every accepted slot has drained.
func TestStop_ManyProducersRacingStopIsRaceFreeAndLossless(t *testing.T) {
	cfg := testConfig(t, true)
	s := newRunning(t, cfg)

	const producers, perProducer = 24, 20
	var wg sync.WaitGroup
	for i := 0; i < producers; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < perProducer; j++ {
				s.CaptureSlot(richSlot(slotAt(base*perProducer + j)))
			}
		}(i)
	}
	go s.Stop() // races the producers

	wg.Wait() // every producer finished: admitted-and-drained or cleanly refused.
	s.Stop()  // barrier — waits out the teardown owner (idempotent).

	if p := s.pending.Load(); p != 0 {
		t.Fatalf("pending = %d after Stop, want 0 — a racing slot was leaked", p)
	}
}
