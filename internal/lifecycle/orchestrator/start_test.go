package orchestrator

// ADR 0070 phase 1 — the orchestrator's Start half (docs/v2-design/lifecycle.md §4.1–4.2), exercised
// with SYNTHETIC nodes + adapters. Criteria (observable via recorded hook calls, the returned error,
// and Result/Milestone):
//
//   AC-S1  Start drives Initialize then Start per active node in topological start order; each node's
//          Initialize runs exactly once, a prerequisite before its dependent.
//   AC-S2  Active() is evaluated once and latched; a latched-inactive node is pruned (never
//          initialized/started) and its Result is Inactive.
//   AC-S3  A nil-Start node auto-promotes to MilestoneRunning once Initialize succeeds.
//   AC-S4  On a Start failure, Start returns the error and unwinds every ADVANCED node in reverse
//          (Running→Stop, Initialized→Rollback); an Initialize failure does NOT roll back the failing
//          node (it cleans up its own partial state).
//   AC-S5  Start claims init ownership before any initializer: on a container already Built it refuses
//          and initializes nothing; after a successful Start, Build() is refused.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/iocdi"
)

// hookRec records the global order of adapter-hook calls across a Start/Shutdown.
type hookRec struct {
	mu  sync.Mutex
	seq []string
}

func (r *hookRec) add(s string) {
	r.mu.Lock()
	r.seq = append(r.seq, s)
	r.mu.Unlock()
}

func (r *hookRec) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seq...)
}

// probe is a synthetic subsystem: a configurable Adapter that records every hook call.
type probe struct {
	name     string
	rec      *hookRec
	active   bool  // Active() returns this
	nilStart bool  // omit the Start hook (construction-only node)
	initErr  error // Initialize returns this
	startErr error // Start returns this

	activePolls         int
	inits               int
	starts              int
	prepares            int
	stops               int
	rollbacks           int
	lastRollbackReached Milestone
}

func newProbe(rec *hookRec, name string) *probe {
	return &probe{name: name, rec: rec, active: true}
}

func (p *probe) adapter() Adapter {
	a := Adapter{
		NodeID: p.name,
		Active: func() bool { p.activePolls++; return p.active },
		Initialize: func() error {
			p.inits++
			p.rec.add("init:" + p.name)
			return p.initErr
		},
		PrepareStop: func() {
			p.prepares++
			p.rec.add("prepare:" + p.name)
		},
		Stop: func(context.Context) error {
			p.stops++
			p.rec.add("stop:" + p.name)
			return nil
		},
		Rollback: func(reached Milestone) error {
			p.rollbacks++
			p.lastRollbackReached = reached
			p.rec.add("rollback:" + p.name)
			return nil
		},
	}
	if !p.nilStart {
		a.Start = func(context.Context) error {
			p.starts++
			p.rec.add("start:" + p.name)
			return p.startErr
		}
	}
	return a
}

// harness wires a container (nodes only — no beans needed) + an orchestrator with the given probes.
func harness(t *testing.T, nodes []iocdi.Node, probes ...*probe) (*Orchestrator, *iocdi.Container) {
	t.Helper()
	c := iocdi.New()
	for _, n := range nodes {
		if err := c.RegisterNode(n); err != nil {
			t.Fatalf("RegisterNode(%q): %v", n.Name, err)
		}
	}
	o := New(c)
	for _, p := range probes {
		if err := o.Register(p.adapter()); err != nil {
			t.Fatalf("Register(%q): %v", p.name, err)
		}
	}
	return o, c
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// AC-S1: Initialize→Start per node, in topological order, each Initialize exactly once.
func TestStart_InitializesThenStartsInTopologicalOrder(t *testing.T) {
	rec := &hookRec{}
	base, mid := newProbe(rec, "base"), newProbe(rec, "mid")
	o, _ := harness(t, []iocdi.Node{
		{Name: "mid", StartAfter: []string{"base"}},
		{Name: "base"},
	}, base, mid)

	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if base.inits != 1 || mid.inits != 1 {
		t.Errorf("inits: base=%d mid=%d, want 1 and 1", base.inits, mid.inits)
	}
	seq := rec.snapshot()
	// base fully up before mid; per-node init before start.
	want := []string{"init:base", "start:base", "init:mid", "start:mid"}
	if len(seq) != len(want) {
		t.Fatalf("hook sequence = %v, want %v", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("hook sequence = %v, want %v", seq, want)
			break
		}
	}
}

// AC-S2: Active() latched once; inactive node pruned and Result==Inactive.
func TestStart_LatchesActiveOnce_PrunesInactive(t *testing.T) {
	rec := &hookRec{}
	on, off := newProbe(rec, "on"), newProbe(rec, "off")
	off.active = false
	o, _ := harness(t, []iocdi.Node{{Name: "on"}, {Name: "off"}}, on, off)

	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if off.activePolls != 1 {
		t.Errorf("inactive node Active() polled %d times, want 1 (latched once)", off.activePolls)
	}
	if off.inits != 0 || off.starts != 0 {
		t.Errorf("inactive node was driven: inits=%d starts=%d, want 0 and 0", off.inits, off.starts)
	}
	if got := o.Result("off"); got != Inactive {
		t.Errorf("Result(off) = %d, want Inactive", got)
	}
	if on.inits != 1 {
		t.Errorf("active node inits = %d, want 1", on.inits)
	}
}

// AC-S3: a nil-Start node auto-promotes to MilestoneRunning once Initialized.
func TestStart_AutoPromotesNilStartNode(t *testing.T) {
	rec := &hookRec{}
	ctor := newProbe(rec, "ctor")
	ctor.nilStart = true
	o, _ := harness(t, []iocdi.Node{{Name: "ctor"}}, ctor)

	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if ctor.inits != 1 || ctor.starts != 0 {
		t.Errorf("nil-Start node: inits=%d starts=%d, want 1 and 0", ctor.inits, ctor.starts)
	}
	if got := o.Milestone("ctor"); got != MilestoneRunning {
		t.Errorf("Milestone(ctor) = %d, want MilestoneRunning (auto-promote)", got)
	}
}

// AC-S4a: a Start failure rolls back advanced nodes in reverse — the Running predecessor via Stop,
// the failing (Initialized) node via Rollback; an un-reached successor is untouched.
func TestStart_RollsBackOnStartFailure(t *testing.T) {
	rec := &hookRec{}
	base, mid, tail := newProbe(rec, "base"), newProbe(rec, "mid"), newProbe(rec, "tail")
	mid.startErr = errors.New("mid start boom")
	o, _ := harness(t, []iocdi.Node{
		{Name: "base"},
		{Name: "mid", StartAfter: []string{"base"}},
		{Name: "tail", StartAfter: []string{"mid"}},
	}, base, mid, tail)

	err := o.Start(context.Background())
	if err == nil {
		t.Fatal("Start should fail (mid's Start errors)")
	}
	if !errors.Is(err, mid.startErr) {
		t.Errorf("Start err = %v, want it to wrap mid's start error", err)
	}
	// tail never reached.
	if tail.inits != 0 {
		t.Errorf("tail was initialized despite the failure upstream (inits=%d)", tail.inits)
	}
	// mid reached Initialized (Start failed) → Rollback(Initialized), NOT Stop.
	if mid.rollbacks != 1 || mid.stops != 0 {
		t.Errorf("mid rollback=%d stop=%d, want 1 and 0 (Initialized ⇒ Rollback)", mid.rollbacks, mid.stops)
	}
	if mid.lastRollbackReached != MilestoneInitialized {
		t.Errorf("mid Rollback reached = %d, want MilestoneInitialized", mid.lastRollbackReached)
	}
	// base reached Running → Stop, NOT Rollback.
	if base.stops != 1 || base.rollbacks != 0 {
		t.Errorf("base stop=%d rollback=%d, want 1 and 0 (Running ⇒ Stop)", base.stops, base.rollbacks)
	}
	// Reverse order: mid unwinds before base.
	seq := rec.snapshot()
	if i, j := indexOf(seq, "rollback:mid"), indexOf(seq, "stop:base"); i < 0 || j < 0 || i > j {
		t.Errorf("rollback order = %v; want rollback:mid before stop:base (reverse)", seq)
	}
}

// AC-S4b: an Initialize failure does NOT roll back the failing node (Initialize owns its own cleanup);
// the Running predecessor is still rolled back.
func TestStart_InitFailureDoesNotRollBackTheFailingNode(t *testing.T) {
	rec := &hookRec{}
	base, mid := newProbe(rec, "base"), newProbe(rec, "mid")
	mid.initErr = errors.New("mid init boom")
	o, _ := harness(t, []iocdi.Node{
		{Name: "base"},
		{Name: "mid", StartAfter: []string{"base"}},
	}, base, mid)

	if err := o.Start(context.Background()); err == nil {
		t.Fatal("Start should fail (mid's Initialize errors)")
	}
	if mid.rollbacks != 0 || mid.stops != 0 {
		t.Errorf("mid rollback=%d stop=%d, want 0 and 0 (Initialize failure cleans itself)", mid.rollbacks, mid.stops)
	}
	if base.stops != 1 {
		t.Errorf("base stop=%d, want 1 (Running predecessor rolled back)", base.stops)
	}
}

// codex P1 (19192efa): Start must NOT hold its state mutex across adapter callbacks, or a callback
// that queries Result/Milestone (e.g. to publish lifecycle status) deadlocks on the non-reentrant
// lock. Reversion: guard the traversal with o.mu held across callbacks → Start deadlocks and the
// test times out.
func TestStart_CallbackQueryingStateDoesNotDeadlock(t *testing.T) {
	rec := &hookRec{}
	p := newProbe(rec, "n1")
	c := iocdi.New()
	if err := c.RegisterNode(iocdi.Node{Name: "n1"}); err != nil {
		t.Fatal(err)
	}
	o := New(c)
	a := p.adapter()
	inner := a.Initialize
	a.Initialize = func() error {
		_ = o.Result("n1") // both must return while Start is mid-traversal, not deadlock
		_ = o.Milestone("n1")
		return inner()
	}
	if err := o.Register(a); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- o.Start(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start deadlocked: a callback querying Result/Milestone blocked on the state mutex")
	}
}

// codex P1 (19192efa): a stuck Stop in rollback (one that ignores its context) must not block Start
// forever — rollback bounds each teardown by the caller's ctx. base's Stop blocks; mid's Start fails,
// triggering rollback of base; with a deadline-bearing ctx, rollback abandons the stuck Stop and
// Start returns its error. Reversion: rollback calls Stop directly with context.Background() → Start
// hangs and the test times out.
func TestStart_RollbackStuckStopIsBounded(t *testing.T) {
	rec := &hookRec{}
	base, mid := newProbe(rec, "base"), newProbe(rec, "mid")
	mid.startErr = errors.New("mid start boom")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) }) // release the abandoned Stop goroutine at test end

	a := base.adapter()
	a.Stop = func(context.Context) error { <-block; return nil } // ignores ctx, blocks

	c := iocdi.New()
	for _, n := range []iocdi.Node{{Name: "base"}, {Name: "mid", StartAfter: []string{"base"}}} {
		if err := c.RegisterNode(n); err != nil {
			t.Fatal(err)
		}
	}
	o := New(c)
	if err := o.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := o.Register(mid.adapter()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- o.Start(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start should fail (mid's Start errors)")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start hung: rollback's stuck Stop was not bounded by the caller's ctx")
	}
}

// codex P1 (bf9488cd): when a rollback teardown times out (abandoned, still running), the unwind
// must HALT — launching a prerequisite's Stop while the dependent's abandoned Stop is still using its
// resources is a reverse-order violation (race / data loss). Chain base ← mid ← boom: boom's Start
// fails; mid (Running) is rolled back but its Stop hangs; once the budget is cancelled, base's Stop
// (mid's prerequisite) must NOT be LAUNCHED at all. base.Stop signals via a channel so the launch is
// observed deterministically (not a raced counter). Reversion: drop the break-on-timeout → base.Stop
// is launched under the still-running mid and baseStopLaunched closes.
func TestStart_RollbackHaltsOnTimeout_PreservesReverseOrder(t *testing.T) {
	rec := &hookRec{}
	base, mid, boom := newProbe(rec, "base"), newProbe(rec, "mid"), newProbe(rec, "boom")
	boom.startErr = errors.New("boom start fails")

	midStarted := make(chan struct{})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	am := mid.adapter()
	am.Stop = func(context.Context) error { close(midStarted); <-block; return nil } // hangs, ignores ctx

	baseStopLaunched := make(chan struct{})
	ab := base.adapter()
	ab.Stop = func(context.Context) error { close(baseStopLaunched); return nil }

	c := iocdi.New()
	for _, n := range []iocdi.Node{
		{Name: "base"},
		{Name: "mid", StartAfter: []string{"base"}},
		{Name: "boom", StartAfter: []string{"mid"}},
	} {
		if err := c.RegisterNode(n); err != nil {
			t.Fatal(err)
		}
	}
	o := New(c)
	for _, a := range []Adapter{ab, am, boom.adapter()} {
		if err := o.Register(a); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- o.Start(ctx) }()
	<-midStarted // mid's Stop is now in progress (hung)
	cancel()     // expire the budget while mid is mid-teardown
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start should fail (boom's Start errors)")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start hung: rollback did not halt after a timed-out teardown")
	}
	// The reverse-order safety invariant: base (mid's prerequisite) must not be torn down while mid's
	// abandoned Stop still runs. Under the bug base.Stop is launched (its goroutine closes the channel
	// within µs); the halt fix never launches it, so the channel stays open for the full window.
	select {
	case <-baseStopLaunched:
		t.Error("prerequisite base's Stop was launched after a dependent's Stop timed out (reverse-order violation)")
	case <-time.After(time.Second):
	}
}

// AC-S5: Start claims init ownership before any initializer — a Built container is refused with
// nothing initialized; and after Start, Build() is refused.
func TestStart_RefusesBuiltContainer_AndClaimsAfterwards(t *testing.T) {
	rec := &hookRec{}
	p := newProbe(rec, "n1")
	o, c := harness(t, []iocdi.Node{{Name: "n1"}}, p)
	if err := c.Build(); err != nil { // Build claims ownerBuild
		t.Fatalf("Build: %v", err)
	}
	if err := o.Start(context.Background()); !errors.Is(err, iocdi.ErrAlreadyInitialized) {
		t.Errorf("Start on a Built container err = %v, want ErrAlreadyInitialized", err)
	}
	if p.inits != 0 {
		t.Errorf("Start initialized %d nodes on a Built container, want 0", p.inits)
	}

	// The mirror: a fresh orchestrator's successful Start claims ownership, refusing a later Build.
	rec2 := &hookRec{}
	p2 := newProbe(rec2, "n1")
	o2, c2 := harness(t, []iocdi.Node{{Name: "n1"}}, p2)
	if err := o2.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c2.Build(); !errors.Is(err, iocdi.ErrAlreadyInitialized) {
		t.Errorf("Build after orchestrator Start err = %v, want ErrAlreadyInitialized", err)
	}
}
