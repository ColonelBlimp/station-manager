// Package lifecycle provides the Supervisor — the owned per-service lifecycle primitive of
// ADR 0070 (docs/decisions/0070-daemon-lifecycle-graph.md, design in
// docs/v2-design/lifecycle.md). A terminal-single-use service owns one Supervisor and delegates
// its state machine, admission cutoff, in-flight tracking, service context, and completion
// barrier to it, keeping only its own drain policy in a teardown closure.
//
// It replaces the hand-rolled stopOnce/stopDone/stopped/life/cancel/admission-WaitGroup that
// bridge, ft8, and evidence each carry, and makes the LC-2/LC-3/LC-4 invariants structural:
//
//   - admission is open ONLY while Running (refused before Start and once sealed at Stop);
//   - Start is one transition w.r.t. Stop AND producers — resources acquired, workers registered,
//     admission opened, and Start completed all commit atomically;
//   - Stop seals every lane, cancels the service context (cancellable work rolls back), waits every
//     lane's admitted work, then runs teardown once; every caller returns the same error;
//   - the package is independent of logging — a teardown panic folds (with its stack) into the
//     shared error and the orchestrator logs it.
package lifecycle

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// Phase is the service's own lifecycle state, owned by its Supervisor.
type Phase uint8

const (
	Idle     Phase = iota // constructed; Start may run (also the state after a cleaned-up start failure)
	Running               // Start's acquire+launch committed; admission open
	Stopping              // Stop sealed admission; teardown in progress
	Stopped               // teardown completed (cleanly or with a folded error)
)

func (p Phase) String() string {
	switch p {
	case Idle:
		return "idle"
	case Running:
		return "running"
	case Stopping:
		return "stopping"
	case Stopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Disposition fixes how a lane's admitted work relates to Stop.
type Disposition uint8

const (
	// MustDrain work is sealed at Stop and awaited, never interrupted — the writer drain.
	MustDrain Disposition = iota
	// Cancellable work binds to Context() and is interrupted at Stop (then still awaited) — the
	// sync loop. Cancel-before-wait lets it release locks must-drain work may need.
	Cancellable
)

// Supervisor is the reusable lifecycle core a terminal-single-use service owns. Construct with New.
type Supervisor struct {
	// transitionMu serializes Start against Stop so the whole start transition (and the whole
	// teardown) is atomic with respect to the other. It is SEPARATE from mu so a long acquire held
	// under transitionMu never blocks Admit (which takes only mu).
	transitionMu sync.Mutex

	mu     sync.Mutex // guards phase, ctx/cancel, lanes; the admission cutoff shares it with the seal
	phase  Phase
	ctx    context.Context
	cancel context.CancelFunc
	lanes  []*Lane

	stopOnce sync.Once
	stopDone chan struct{}
	stopErr  error // read by every caller after <-stopDone
}

// New constructs an Idle Supervisor.
func New() *Supervisor {
	return &Supervisor{stopDone: make(chan struct{})}
}

// RegisterLane declares a named work lane. Call it at construction, BEFORE Start — the returned
// handle is held by the service and used directly (no per-call string lookup on the hot path).
func (s *Supervisor) RegisterLane(name string, d Disposition) *Lane {
	l := &Lane{sup: s, name: name, disposition: d}
	s.mu.Lock()
	s.lanes = append(s.lanes, l)
	s.mu.Unlock()
	return l
}

// Start owns the whole start transition under transitionMu. In order: derive the service context
// from parent; run acquire (fallible) while public admission is still closed; run launch
// (infallible) which uses the StartScope to pre-register long-lived workers while admission remains
// closed; then the COMMIT POINT — publish Running so public Admit opens. No public Admit succeeds
// before the commit, so status never reports Running with workers not yet registered. On acquire
// failure it cancels the context and stays Idle (retryable); acquire must clean up its own partial
// resources. No-op if already Running; terminal refusal (no-op) if Stopped/Stopping.
func (s *Supervisor) Start(
	parent context.Context,
	acquire func(ctx context.Context) error,
	launch func(ctx context.Context, sc *StartScope),
) error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	s.mu.Lock()
	if s.phase != Idle { // already Running, or terminal (Stopping/Stopped)
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	s.ctx, s.cancel = ctx, cancel
	s.mu.Unlock()

	if acquire != nil {
		if err := acquire(ctx); err != nil {
			cancel() // stays Idle (retryable); acquire owns its own partial-resource cleanup
			return err
		}
	}
	if launch != nil {
		sc := &StartScope{sup: s, valid: true}
		launch(ctx, sc)
		sc.mu.Lock()
		sc.valid = false // the scope is only usable synchronously within launch
		sc.mu.Unlock()
	}

	s.mu.Lock()
	s.phase = Running // COMMIT: admission opens
	s.mu.Unlock()
	return nil
}

// Context returns the service context, cancelled at Stop. Cancellable-lane work binds to it. It is
// non-nil once Start has begun; before any Start it returns context.Background().
func (s *Supervisor) Context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Phase reports the current lifecycle phase.
func (s *Supervisor) Phase() Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase
}

// Stop runs the teardown exactly once and returns its error to every caller (sequential or
// concurrent). The sequence: seal every lane (Running→Stopping) under the same mutex Admit reads;
// cancel the context (cancellable work interrupts and releases its locks); wait every lane's
// admitted work; run teardown; → Stopped. Stop-before-Start is terminal and runs no teardown. A
// teardown panic is recovered, folded with its stack into the returned error, and never
// re-panicked; close(stopDone) always runs, so no caller is stranded.
func (s *Supervisor) Stop(teardown func() error) error {
	s.stopOnce.Do(func() {
		defer close(s.stopDone) // registered first ⇒ runs last ⇒ always releases callers

		s.transitionMu.Lock()
		defer s.transitionMu.Unlock()

		s.mu.Lock()
		wasRunning := s.phase == Running
		s.phase = Stopping // seal: Admit now refuses on every lane
		cancel := s.cancel
		lanes := append([]*Lane(nil), s.lanes...)
		s.mu.Unlock()

		if !wasRunning {
			// Stop-before-Start (or already terminal): nothing was opened to tear down.
			s.setPhase(Stopped)
			return
		}

		if cancel != nil {
			cancel() // cancellable-lane work interrupts BEFORE we wait, releasing its locks
		}
		for _, l := range lanes {
			l.wg.Wait() // both kinds: all admitted work finishes
		}
		s.stopErr = runTeardown(teardown)
		s.setPhase(Stopped)
	})
	<-s.stopDone
	return s.stopErr
}

func (s *Supervisor) setPhase(p Phase) {
	s.mu.Lock()
	s.phase = p
	s.mu.Unlock()
}

// runTeardown recovers a teardown panic and folds it (with the stack) into an error, so a panicking
// teardown becomes a Failed result instead of stranding callers or crashing every other service's
// teardown mid-shutdown. The package stays independent of logging.
func runTeardown(teardown func() error) (err error) {
	if teardown == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("lifecycle: teardown panicked: %v\n%s", r, debug.Stack())
		}
	}()
	return teardown()
}

// Lane is a named work group. MustDrain work is awaited uninterrupted at Stop; Cancellable work is
// interrupted (via Context()) then awaited.
type Lane struct {
	sup         *Supervisor
	name        string
	disposition Disposition
	wg          sync.WaitGroup
}

// Name reports the lane's registered name.
func (l *Lane) Name() string { return l.name }

// Admit registers one unit of in-flight work. ok is true ONLY while the supervisor is Running —
// refused before Start (admission not open) and once sealed at Stop. A true return MUST be paired
// with exactly one done().
func (l *Lane) Admit() (done func(), ok bool) {
	l.sup.mu.Lock()
	if l.sup.phase != Running {
		l.sup.mu.Unlock()
		return nil, false
	}
	l.wg.Add(1) // under mu, so the seal (also under mu) can never race a fresh Add
	l.sup.mu.Unlock()
	return l.wg.Done, true
}

// StartScope is the private token launch uses to register long-lived workers before the commit
// point, while public admission is still closed. It is valid only synchronously within launch.
type StartScope struct {
	sup   *Supervisor
	mu    sync.Mutex
	valid bool
}

// Track pre-registers one long-lived worker on a lane and returns its done. Use it for CANCELLABLE
// workers; a teardown-signalled worker must NOT be Track'd (Stop waits lanes before teardown, so it
// would deadlock) — those stay on the service's own WaitGroup, waited inside teardown. Track panics
// if used outside launch.
func (sc *StartScope) Track(lane *Lane) (done func()) {
	sc.mu.Lock()
	valid := sc.valid
	sc.mu.Unlock()
	if !valid {
		panic("lifecycle: StartScope.Track called outside launch")
	}
	lane.wg.Add(1)
	return lane.wg.Done
}
