package iocdi

// ADR 0070 phase 1 — the Wire()/Build() split (docs/v2-design/lifecycle.md §4.0). Criteria:
//
//   AC-1  Wire() constructs + injects every bean but does NOT run Initialize.
//   AC-2  Build() wires AND runs Initialize (the compat/explicit path).
//   AC-3  Resolve is wire-only: it wires (never Builds/initializes) so a stray resolve after the
//         orchestrator takes over init cannot double-initialize.
//   AC-4  Single-init-owner guardrail: Build refuses (ErrAlreadyInitialized) if initialization was
//         already claimed elsewhere.
//   AC-5  Idempotent: Wire→Build→Build initializes exactly once.

import (
	"errors"
	"reflect"
	"testing"
)

// failFirstInit's Initialize fails on the first call and succeeds after — a transient failure.
type failFirstInit struct{ attempts int }

func (f *failFirstInit) Initialize() error {
	f.attempts++
	if f.attempts == 1 {
		return errors.New("transient init failure")
	}
	return nil
}

// initTracker is a bean with a di.inject dependency and an Initializer that counts its calls.
type initTracker struct {
	Dep       *diA `di.inject:"a"`
	initCount int
}

func (t *initTracker) Initialize() error { t.initCount++; return nil }

// The partial-init retry tests use a dependency chain base ← mid ← tail (each di.inject-depends on
// the previous), so the initializer order is deterministic (base, mid, tail). mid fails its first
// Initialize (transient), so a first Build initializes base then stops at mid and never reaches tail.
type baseInit struct{ count int }

func (b *baseInit) Initialize() error { b.count++; return nil }

type midInit struct {
	Base     *baseInit `di.inject:"base"`
	attempts int
}

func (m *midInit) Initialize() error {
	m.attempts++
	if m.attempts == 1 {
		return errors.New("transient mid failure")
	}
	return nil
}

type tailInit struct {
	Mid   *midInit `di.inject:"mid"`
	count int
}

func (t *tailInit) Initialize() error { t.count++; return nil }

func registerChain(t *testing.T) *Container {
	t.Helper()
	c := New()
	for _, id := range []string{"base", "mid", "tail"} {
		var typ reflect.Type
		switch id {
		case "base":
			typ = reflect.TypeOf(baseInit{})
		case "mid":
			typ = reflect.TypeOf(midInit{})
		case "tail":
			typ = reflect.TypeOf(tailInit{})
		}
		if err := c.Register(id, typ); err != nil {
			t.Fatalf("Register %q: %v", id, err)
		}
	}
	return c
}

func registerTracker(t *testing.T) *Container {
	t.Helper()
	c := New()
	if err := c.Register("a", reflect.TypeOf(diA{})); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := c.Register("tracker", reflect.TypeOf(initTracker{})); err != nil {
		t.Fatalf("Register tracker: %v", err)
	}
	return c
}

// AC-1: Wire constructs + injects, but does not initialize.
func TestWire_ConstructsAndInjectsButDoesNotInitialize(t *testing.T) {
	c := registerTracker(t)
	if err := c.Wire(); err != nil {
		t.Fatalf("Wire: %v", err)
	}
	tr, err := ResolveAs[*initTracker](c, "tracker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tr.Dep == nil {
		t.Error("Wire did not inject the dependency")
	}
	if tr.initCount != 0 {
		t.Errorf("Wire ran Initialize %d times; it must run none", tr.initCount)
	}
}

// AC-2: Build wires and initializes.
func TestBuild_WiresAndInitializes(t *testing.T) {
	c := registerTracker(t)
	if err := c.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	tr, err := ResolveAs[*initTracker](c, "tracker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tr.Dep == nil {
		t.Error("Build did not inject the dependency")
	}
	if tr.initCount != 1 {
		t.Errorf("Build ran Initialize %d times, want 1", tr.initCount)
	}
}

// AC-3: Resolve is wire-only — it wires (dependency injected) but does not initialize.
func TestResolve_IsWireOnly(t *testing.T) {
	c := registerTracker(t)
	// No explicit Wire/Build: the resolve itself must ensure only wiring.
	tr, err := ResolveAs[*initTracker](c, "tracker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if tr.Dep == nil {
		t.Error("Resolve did not wire (dependency not injected)")
	}
	if tr.initCount != 0 {
		t.Errorf("Resolve ran Initialize %d times; resolution must be wire-only", tr.initCount)
	}
}

// AC-4: Build refuses if initialization was already claimed by the OTHER owner (the orchestrator).
func TestBuild_RefusesWhenInitializationAlreadyClaimed(t *testing.T) {
	c := registerTracker(t)
	c.initOwner.Store(int32(ownerOrchestrator)) // simulate the orchestrator having claimed init
	if err := c.Build(); !errors.Is(err, ErrAlreadyInitialized) {
		t.Errorf("Build err = %v, want ErrAlreadyInitialized (single-init-owner guardrail)", err)
	}
	if c.built.Load() {
		t.Error("Build marked the container built despite refusing initialization")
	}
}

// codex P1 (cc093d0e): a Build retry after a PARTIAL init must resume, not restart — an initializer
// that already succeeded is never re-run (Initializer is not required to be idempotent). With the
// chain base ← mid ← tail and mid failing once: the first Build initializes base then fails at mid;
// the retry must skip base (count stays 1), retry mid (succeeds), then run tail. Reversion: drop the
// initDone skip → the retry re-runs base and base.count reaches 2.
func TestBuild_RetryResumesWithoutReinit(t *testing.T) {
	c := registerChain(t)

	if err := c.Build(); err == nil {
		t.Fatal("first Build should fail (mid's initializer errors on the first call)")
	}
	base, _ := ResolveAs[*baseInit](c, "base")
	mid, _ := ResolveAs[*midInit](c, "mid")
	tail, _ := ResolveAs[*tailInit](c, "tail")
	// Partial init: base ran once, mid was attempted, tail never reached.
	if base.count != 1 {
		t.Errorf("after first Build base.count = %d, want 1", base.count)
	}
	if tail.count != 0 {
		t.Errorf("after first Build tail.count = %d, want 0 (tail must not run before mid succeeds)", tail.count)
	}
	if c.built.Load() {
		t.Error("a partial Build marked the container built")
	}

	if err := c.Build(); err != nil {
		t.Fatalf("retry Build err = %v, want nil (resume from the failed bean)", err)
	}
	if base.count != 1 {
		t.Errorf("retry re-ran a succeeded initializer: base.count = %d, want 1", base.count)
	}
	if mid.attempts != 2 {
		t.Errorf("mid.attempts = %d, want 2 (the failing bean is retried)", mid.attempts)
	}
	if tail.count != 1 {
		t.Errorf("tail.count = %d, want 1 (remaining beans run after the retry)", tail.count)
	}
	if !c.built.Load() {
		t.Error("retry Build did not complete")
	}
}

// codex P1 (cc093d0e): once a Build attempt begins initialization it owns it DURABLY — even a failed
// partial Build keeps ownerBuild, so the orchestrator can never re-initialize the beans Build already
// ran (ADR 0070 single-init-owner). Reversion: release ownership on failure (store ownerNone) → the
// orchestrator's claim succeeds.
func TestBuild_RefusesOrchestratorAfterPartialBuild(t *testing.T) {
	c := registerChain(t)

	if err := c.Build(); err == nil {
		t.Fatal("first Build should fail (mid's initializer errors on the first call)")
	}
	if got := initOwner(c.initOwner.Load()); got != ownerBuild {
		t.Errorf("initOwner = %d after a partial Build, want ownerBuild (ownership is durable)", got)
	}
	if c.claimInitOwner(ownerOrchestrator) {
		t.Error("orchestrator claimed init after a partial Build — single-init-owner violated")
	}
}

// codex P2: a transient initializer failure must stay retryable — Build rolls back its init claim
// on failure. Reversion: drop the rollback → the retry Build returns ErrAlreadyInitialized.
func TestBuild_RetryableAfterInitializerFailure(t *testing.T) {
	c := New()
	if err := c.Register("failing", reflect.TypeOf(failFirstInit{})); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := c.Build(); err == nil {
		t.Fatal("first Build should fail (its initializer errors)")
	}
	if c.built.Load() {
		t.Error("a failed Build marked the container built")
	}
	if err := c.Build(); err != nil {
		t.Errorf("retry Build err = %v, want nil (a transient init failure must stay retryable)", err)
	}
	if !c.built.Load() {
		t.Error("retry Build did not complete")
	}
}

// codex P1 (b161407): Wire() closes registration. A bean registered after Wire would never be
// constructed (Wire is a no-op once wired) and would surface as "not initialized" at resolve, far
// from the bad Register call. Register/RegisterInstance must refuse once wired, exactly as they do
// once built/planFrozen. Reversion: drop the wired check → the post-Wire Register succeeds.
func TestRegister_ClosedAfterWire(t *testing.T) {
	c := New()
	if err := c.Register("a", reflect.TypeOf(diA{})); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := c.Wire(); err != nil {
		t.Fatalf("Wire: %v", err)
	}
	if err := c.Register("late", reflect.TypeOf(diA{})); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("Register after Wire err = %v, want ErrRegistrationClosed", err)
	}
	if err := c.RegisterInstance("late2", &diA{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("RegisterInstance after Wire err = %v, want ErrRegistrationClosed", err)
	}
	if _, ok := c.registeredBeans["late"]; ok {
		t.Error("a bean was registered after Wire")
	}
}

// codex P1 (b161407, recheck): the early registration-closed fast-path can lose a lock race to
// Wire(). The seam wires the container between Register's fast-path and its lock; the under-regMu
// recheck must still refuse, so no bean lands after wiring — and (like the freeze recheck) it must
// run BEFORE checkForDependency, or a rejected registration leaves a phantom required dependency.
// Reversion: drop the wired check from the under-lock recheck → the racing Register succeeds.
func TestRegister_RechecksWiredUnderLock(t *testing.T) {
	c := New()

	var wiredOnce bool
	beanRegisterPreLockForTest = func() {
		if !wiredOnce { // Wire takes buildLock+regMu, wires, sets wired, releases — before Register locks.
			wiredOnce = true
			if err := c.Wire(); err != nil {
				t.Fatalf("Wire in seam: %v", err)
			}
		}
	}
	defer func() { beanRegisterPreLockForTest = nil }()

	// diB depends on "a" — a rejected registration must leave NO bean AND no phantom required-dep.
	if err := c.Register("late", reflect.TypeOf(diB{})); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("Register racing Wire err = %v, want ErrRegistrationClosed (recheck under lock)", err)
	}
	if _, ok := c.registeredBeans["late"]; ok {
		t.Error("a bean was inserted after Wire completed")
	}
	if _, ok := c.requiredDependency["a"]; ok {
		t.Error("a rejected registration left a phantom required dependency")
	}
}

// AC-5: Wire then Build then Build initializes exactly once.
func TestWireThenBuild_InitializesExactlyOnce(t *testing.T) {
	c := registerTracker(t)
	if err := c.Wire(); err != nil {
		t.Fatalf("Wire: %v", err)
	}
	if err := c.Build(); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := c.Build(); err != nil { // idempotent
		t.Fatalf("second Build: %v", err)
	}
	tr, _ := ResolveAs[*initTracker](c, "tracker")
	if tr.initCount != 1 {
		t.Errorf("initCount = %d, want 1 (Wire → Build → Build)", tr.initCount)
	}
}
