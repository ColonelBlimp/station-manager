package orchestrator

// ADR 0070 phase 1 — the orchestrator's Shutdown half (docs/v2-design/lifecycle.md §4.3), exercised
// with SYNTHETIC nodes + adapters. Criteria (observable via the returned ShutdownReport, Result(node),
// and recorded hook-call order):
//
//   AC-D1  Every active node's PrepareStop runs before ANY Stop and before the budget; nil is a no-op.
//   AC-D2  The active RFCritical node's Stop is the SOLE Stop attempted until it returns (or the
//          deadline fires); no other Stop begins before then. An inactive fence is simply pruned.
//   AC-D3  Every NON-FENCE node's Stop runs only after all its DrainAfter prereqs settled as
//          Drained/Inactive (drain-graph topological order, registration tiebreak).
//   AC-D4  Stop nil→Drained, error→Failed (Err carried), past-budget→TimedOut (traversal not blocked).
//   AC-D5  A node with any prereq result ∉ {Drained,Inactive} is Skipped (BlockedBy = the failed
//          prereqs in declared DrainAfter order); its Skipped propagates transitively.
//   AC-D6  An Inactive prereq satisfies a drain edge — the dependent drains normally.
//   AC-D7  ShutdownReport.Outcomes is in traversal order (fence first, then the drain); Inactive nodes
//          are absent (Result==Inactive still). FirstTimedOut names the FIRST timed-out node.
//   AC-D8  budget<=0 ⇒ immediate expiry: the first eligible Stop is TimedOut, dependents Skipped.
//   AC-D9  Idempotent: a second or concurrent Shutdown reruns no hook and returns the same report.
//   AC-D10 Shutdown before a successful Start (never started OR Start failed+rolled back) is an empty
//          no-op: no hooks, empty report, existing results unchanged.

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/iocdi"
)

// startedOrch builds a container (nodes only) + orchestrator, registers the adapters, and Starts.
func startedOrch(t *testing.T, nodes []iocdi.Node, adapters ...Adapter) *Orchestrator {
	t.Helper()
	c := iocdi.New()
	for _, n := range nodes {
		if err := c.RegisterNode(n); err != nil {
			t.Fatalf("RegisterNode(%q): %v", n.Name, err)
		}
	}
	o := New(c)
	for _, a := range adapters {
		if err := o.Register(a); err != nil {
			t.Fatalf("Register(%q): %v", a.NodeID, err)
		}
	}
	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return o
}

// outcomeFor returns the report's outcome for a node (and whether it is present).
func outcomeFor(r ShutdownReport, node string) (NodeOutcome, bool) {
	for _, o := range r.Outcomes {
		if o.Node == node {
			return o, true
		}
	}
	return NodeOutcome{}, false
}

func stopOrder(seq []string) []string {
	var out []string
	for _, s := range seq {
		if len(s) > 5 && s[:5] == "stop:" {
			out = append(out, s[5:])
		}
	}
	return out
}

// AC-D1: PrepareStop for every active node before any Stop and before the budget.
func TestShutdown_PrepareStopBeforeAnyStop(t *testing.T) {
	rec := &hookRec{}
	a, b := newProbe(rec, "a"), newProbe(rec, "b")
	o := startedOrch(t, []iocdi.Node{{Name: "a"}, {Name: "b"}}, a.adapter(), b.adapter())
	o.Shutdown(time.Second)

	seq := rec.snapshot()
	firstStop := len(seq)
	for i, s := range seq {
		if len(s) >= 5 && s[:5] == "stop:" {
			firstStop = i
			break
		}
	}
	// Every prepare must appear before the first stop.
	for i, s := range seq {
		if len(s) >= 8 && s[:8] == "prepare:" && i > firstStop {
			t.Errorf("PrepareStop ran after a Stop: %v", seq)
		}
	}
	if a.prepares != 1 || b.prepares != 1 {
		t.Errorf("prepares: a=%d b=%d, want 1 and 1", a.prepares, b.prepares)
	}
}

// AC-D2: the RFCritical fence is the sole Stop until its Stop RETURNS. The fence is HELD BLOCKED under
// test control (its outcome stays un-recorded throughout); the non-fence Stop SIGNALS if it runs while
// the fence is not yet Drained, and the test rules out any overlap BEFORE releasing the fence. A
// correct shutdown's loop is blocked on the fence (no overlap); a concurrent regression signals and
// fails. Checking before release closes the release-vs-return window (five codex rounds); a bounded
// select on entry avoids a hang if the fence is skipped.
func TestShutdown_RFFenceIsSoleUntilItReturns(t *testing.T) {
	rec := &hookRec{}
	bridge, other := newProbe(rec, "bridge"), newProbe(rec, "other")
	var o *Orchestrator
	fenceEntered := make(chan struct{})
	release := make(chan struct{})
	overlap := make(chan struct{}, 1)
	ab := bridge.adapter()
	ab.Stop = func(context.Context) error {
		rec.add("stop:bridge")
		close(fenceEntered)
		<-release
		return nil
	}
	ao := other.adapter()
	ao.Stop = func(context.Context) error {
		if o.Result("bridge") != Drained { // ran while the fence had not yet returned/recorded
			overlap <- struct{}{}
		}
		rec.add("stop:other")
		return nil
	}
	// Register `other` FIRST so only the fence phase — not registration order — can make bridge drain
	// first (otherwise the test would pass by coincidence).
	o = startedOrch(t, []iocdi.Node{
		{Name: "other"},
		{Name: "bridge", StopPriority: iocdi.RFCritical},
	}, ao, ab)
	done := make(chan ShutdownReport, 1)
	go func() { done <- o.Shutdown(2 * time.Second) }()
	select {
	case <-fenceEntered:
	case <-done:
		t.Fatal("Shutdown completed without entering the RF fence")
	case <-time.After(2 * time.Second):
		t.Fatal("RF fence was never entered")
	}
	// Rule out overlap BEFORE releasing (see the acceptance test): a correct shutdown's loop is
	// blocked on the fence so `other` cannot run; a concurrent regression runs it immediately.
	select {
	case <-overlap:
		t.Error("other Stop overlapped the still-executing RF fence")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	rep := <-done
	if len(rep.Outcomes) == 0 || rep.Outcomes[0].Node != "bridge" {
		t.Errorf("Outcomes = %+v, want bridge first (fence drains first)", rep.Outcomes)
	}
}

// AC-D3: a dependent drains only after its prerequisite Drained (prerequisite-first).
func TestShutdown_DrainsPrerequisitesFirst(t *testing.T) {
	rec := &hookRec{}
	ft8, evidence := newProbe(rec, "ft8"), newProbe(rec, "evidence")
	o := startedOrch(t, []iocdi.Node{
		{Name: "evidence", DrainAfter: []string{"ft8"}},
		{Name: "ft8"},
	}, evidence.adapter(), ft8.adapter())
	rep := o.Shutdown(time.Second)

	order := stopOrder(rec.snapshot())
	if indexOf(order, "ft8") < 0 || indexOf(order, "evidence") < 0 || indexOf(order, "ft8") > indexOf(order, "evidence") {
		t.Errorf("stop order = %v, want ft8 before evidence (DrainAfter)", order)
	}
	if oc, _ := outcomeFor(rep, "evidence"); oc.Result != Drained {
		t.Errorf("evidence result = %d, want Drained", oc.Result)
	}
}

// AC-D4: classify Drained / Failed(+Err) / TimedOut. ok+boom drain within budget; hang outlasts it.
func TestShutdown_ClassifiesResults(t *testing.T) {
	rec := &hookRec{}
	ok, boom, hang := newProbe(rec, "ok"), newProbe(rec, "boom"), newProbe(rec, "hang")
	errBoom := errors.New("boom stop failed")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	aOk := ok.adapter()
	aBoom := boom.adapter()
	aBoom.Stop = func(context.Context) error { rec.add("stop:boom"); return errBoom }
	aHang := hang.adapter()
	aHang.Stop = func(context.Context) error { rec.add("stop:hang"); <-block; return nil }

	o := startedOrch(t, []iocdi.Node{{Name: "ok"}, {Name: "boom"}, {Name: "hang"}}, aOk, aBoom, aHang)
	rep := o.Shutdown(150 * time.Millisecond)

	if oc, _ := outcomeFor(rep, "ok"); oc.Result != Drained {
		t.Errorf("ok result = %d, want Drained", oc.Result)
	}
	if oc, _ := outcomeFor(rep, "boom"); oc.Result != Failed || !errors.Is(oc.Err, errBoom) {
		t.Errorf("boom outcome = %+v, want Failed carrying errBoom", oc)
	}
	if oc, _ := outcomeFor(rep, "hang"); oc.Result != TimedOut || oc.Err != nil {
		t.Errorf("hang outcome = %+v, want TimedOut with nil Err", oc)
	}
	if rep.FirstTimedOut != "hang" {
		t.Errorf("FirstTimedOut = %q, want hang", rep.FirstTimedOut)
	}
}

// AC-D5: transitive skip — a→b→c, a TimedOut ⇒ b Skipped(BlockedBy a) ⇒ c Skipped(BlockedBy b).
func TestShutdown_TransitiveSkip(t *testing.T) {
	rec := &hookRec{}
	a, b, cc := newProbe(rec, "a"), newProbe(rec, "b"), newProbe(rec, "c")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	aa := a.adapter()
	aa.Stop = func(context.Context) error { rec.add("stop:a"); <-block; return nil }

	o := startedOrch(t, []iocdi.Node{
		{Name: "a"},
		{Name: "b", DrainAfter: []string{"a"}},
		{Name: "c", DrainAfter: []string{"b"}},
	}, aa, b.adapter(), cc.adapter())
	rep := o.Shutdown(120 * time.Millisecond)

	ocB, _ := outcomeFor(rep, "b")
	if ocB.Result != Skipped || !reflect.DeepEqual(ocB.BlockedBy, []string{"a"}) {
		t.Errorf("b outcome = %+v, want Skipped BlockedBy [a]", ocB)
	}
	ocC, _ := outcomeFor(rep, "c")
	if ocC.Result != Skipped || !reflect.DeepEqual(ocC.BlockedBy, []string{"b"}) {
		t.Errorf("c outcome = %+v, want Skipped BlockedBy [b] (transitive)", ocC)
	}
	if b.stops != 0 || cc.stops != 0 {
		t.Errorf("a skipped node's Stop was attempted: b=%d c=%d", b.stops, cc.stops)
	}
}

// AC-D6: an Inactive prerequisite satisfies the drain edge — the dependent drains normally.
func TestShutdown_InactivePrerequisiteSatisfies(t *testing.T) {
	rec := &hookRec{}
	off, b := newProbe(rec, "off"), newProbe(rec, "b")
	off.active = false
	o := startedOrch(t, []iocdi.Node{
		{Name: "off"},
		{Name: "b", DrainAfter: []string{"off"}},
	}, off.adapter(), b.adapter())
	rep := o.Shutdown(time.Second)

	if oc, _ := outcomeFor(rep, "b"); oc.Result != Drained {
		t.Errorf("b result = %d, want Drained (Inactive prereq satisfies)", oc.Result)
	}
	if _, ok := outcomeFor(rep, "off"); ok {
		t.Error("Inactive node appeared in Outcomes; it should be pruned from traversal")
	}
	if got := o.Result("off"); got != Inactive {
		t.Errorf("Result(off) = %d, want Inactive", got)
	}
}

// AC-D5/D7 detail: BlockedBy lists failed prerequisites in DECLARED DrainAfter order (not reg order).
func TestShutdown_BlockedByInDeclaredOrder(t *testing.T) {
	rec := &hookRec{}
	p1, p2, dep := newProbe(rec, "p1"), newProbe(rec, "p2"), newProbe(rec, "dep")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	a1, a2 := p1.adapter(), p2.adapter()
	a1.Stop = func(context.Context) error { rec.add("stop:p1"); <-block; return nil }
	a2.Stop = func(context.Context) error { rec.add("stop:p2"); <-block; return nil }
	// Register nodes so reg order (p2 before p1) differs from dep's declared order [p1, p2].
	o := startedOrch(t, []iocdi.Node{
		{Name: "p2"},
		{Name: "p1"},
		{Name: "dep", DrainAfter: []string{"p1", "p2"}},
	}, a2, a1, dep.adapter())
	rep := o.Shutdown(120 * time.Millisecond)

	oc, _ := outcomeFor(rep, "dep")
	if oc.Result != Skipped || !reflect.DeepEqual(oc.BlockedBy, []string{"p1", "p2"}) {
		t.Errorf("dep BlockedBy = %v, want [p1 p2] (declared DrainAfter order)", oc.BlockedBy)
	}
}

// AC-D8: budget<=0 is immediate expiry — the hung node TimedOut, its dependent Skipped.
func TestShutdown_BudgetZeroImmediateExpiry(t *testing.T) {
	rec := &hookRec{}
	a, b := newProbe(rec, "a"), newProbe(rec, "b")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	aa := a.adapter()
	aa.Stop = func(context.Context) error { rec.add("stop:a"); <-block; return nil }
	o := startedOrch(t, []iocdi.Node{
		{Name: "a"},
		{Name: "b", DrainAfter: []string{"a"}},
	}, aa, b.adapter())
	rep := o.Shutdown(0)

	if oc, _ := outcomeFor(rep, "a"); oc.Result != TimedOut {
		t.Errorf("a result = %d, want TimedOut (budget<=0 immediate expiry)", oc.Result)
	}
	if oc, _ := outcomeFor(rep, "b"); oc.Result != Skipped {
		t.Errorf("b result = %d, want Skipped", oc.Result)
	}
}

// AC-D9: idempotent — a second Shutdown reruns no hook and returns the same report.
func TestShutdown_Idempotent(t *testing.T) {
	rec := &hookRec{}
	a, b := newProbe(rec, "a"), newProbe(rec, "b")
	o := startedOrch(t, []iocdi.Node{{Name: "a"}, {Name: "b"}}, a.adapter(), b.adapter())

	rep1 := o.Shutdown(time.Second)
	seqAfter1 := rec.snapshot()
	rep2 := o.Shutdown(time.Second)
	seqAfter2 := rec.snapshot()

	if !reflect.DeepEqual(seqAfter1, seqAfter2) {
		t.Errorf("second Shutdown reran hooks: %v then %v", seqAfter1, seqAfter2)
	}
	if !reflect.DeepEqual(rep1, rep2) {
		t.Errorf("second Shutdown returned a different report:\n %+v\n %+v", rep1, rep2)
	}
	if a.prepares != 1 || a.stops != 1 || b.prepares != 1 || b.stops != 1 {
		t.Errorf("hooks ran more than once: a(p=%d,s=%d) b(p=%d,s=%d)", a.prepares, a.stops, b.prepares, b.stops)
	}
}

// AC-D9 (concurrent): two concurrent Shutdowns run each hook exactly once and agree on the report.
func TestShutdown_ConcurrentCallsRunHooksOnce(t *testing.T) {
	rec := &hookRec{}
	a := newProbe(rec, "a")
	o := startedOrch(t, []iocdi.Node{{Name: "a"}}, a.adapter())

	var wg sync.WaitGroup
	reps := make([]ShutdownReport, 2)
	for i := range reps {
		wg.Add(1)
		go func(i int) { defer wg.Done(); reps[i] = o.Shutdown(time.Second) }(i)
	}
	wg.Wait()
	if !reflect.DeepEqual(reps[0], reps[1]) {
		t.Errorf("concurrent Shutdowns disagreed:\n %+v\n %+v", reps[0], reps[1])
	}
	prepares, stops := 0, 0
	for _, s := range rec.snapshot() {
		switch s {
		case "prepare:a":
			prepares++
		case "stop:a":
			stops++
		}
	}
	if prepares != 1 || stops != 1 {
		t.Errorf("concurrent Shutdown ran hooks %d prepares / %d stops, want 1 each", prepares, stops)
	}
}

// codex P1 (32ad6113): a Shutdown before Start must NOT latch shutdownDone — otherwise a
// shutdown-before-start (e.g. a startup/shutdown race) caches an empty report and every LATER
// Shutdown returns it, leaving a successfully-started daemon (RF-critical included) running. Reversion:
// latch shutdownDone before the !started check → the post-Start Shutdown returns the cached empty.
func TestShutdown_BeforeStartDoesNotDisableLaterShutdown(t *testing.T) {
	rec := &hookRec{}
	a := newProbe(rec, "a")
	c := iocdi.New()
	if err := c.RegisterNode(iocdi.Node{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	o := New(c)
	if err := o.Register(a.adapter()); err != nil {
		t.Fatal(err)
	}
	if rep := o.Shutdown(time.Second); len(rep.Outcomes) != 0 {
		t.Fatalf("pre-start Shutdown = %+v, want empty", rep)
	}
	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rep := o.Shutdown(time.Second)
	if len(rep.Outcomes) == 0 || a.stops != 1 {
		t.Errorf("Shutdown after a pre-start no-op did not tear down: outcomes=%+v a.stops=%d", rep.Outcomes, a.stops)
	}
}

// codex P2 (32ad6113): the settled report must be defensively copied, or a caller mutating a returned
// report corrupts the cache and every later idempotent return. Reversion: return o.report directly →
// mutating rep1 changes rep2.
func TestShutdown_ReportIsDefensivelyCopied(t *testing.T) {
	rec := &hookRec{}
	a, b := newProbe(rec, "a"), newProbe(rec, "b")
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	aa := a.adapter()
	aa.Stop = func(context.Context) error { rec.add("stop:a"); <-block; return nil }
	o := startedOrch(t, []iocdi.Node{
		{Name: "a"},
		{Name: "b", DrainAfter: []string{"a"}},
	}, aa, b.adapter())

	rep1 := o.Shutdown(0) // a TimedOut, b Skipped BlockedBy [a]
	for i := range rep1.Outcomes {
		rep1.Outcomes[i].Node = "MUTATED"
		rep1.Outcomes[i].Result = Drained
		for j := range rep1.Outcomes[i].BlockedBy {
			rep1.Outcomes[i].BlockedBy[j] = "MUTATED"
		}
	}
	rep2 := o.Shutdown(0) // idempotent — must be a fresh copy, unaffected by the mutation above
	ocB, ok := outcomeFor(rep2, "b")
	if !ok || ocB.Result != Skipped || !reflect.DeepEqual(ocB.BlockedBy, []string{"a"}) {
		t.Errorf("second report was corrupted by mutating the first: %+v", rep2.Outcomes)
	}
}

// AC-D10: Shutdown before a successful Start is an empty no-op.
func TestShutdown_BeforeStartIsEmptyNoOp(t *testing.T) {
	// Never started.
	rec := &hookRec{}
	a := newProbe(rec, "a")
	c := iocdi.New()
	if err := c.RegisterNode(iocdi.Node{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	o := New(c)
	if err := o.Register(a.adapter()); err != nil {
		t.Fatal(err)
	}
	rep := o.Shutdown(time.Second)
	if len(rep.Outcomes) != 0 || rep.FirstTimedOut != "" {
		t.Errorf("Shutdown before Start = %+v, want empty report", rep)
	}
	if a.prepares != 0 || a.stops != 0 {
		t.Errorf("Shutdown before Start ran hooks: prepares=%d stops=%d", a.prepares, a.stops)
	}

	// Start failed and rolled back → still an empty no-op.
	rec2 := &hookRec{}
	base, boom := newProbe(rec2, "base"), newProbe(rec2, "boom")
	boom.startErr = errors.New("boom fails")
	c2 := iocdi.New()
	for _, n := range []iocdi.Node{{Name: "base"}, {Name: "boom", StartAfter: []string{"base"}}} {
		if err := c2.RegisterNode(n); err != nil {
			t.Fatal(err)
		}
	}
	o2 := New(c2)
	if err := o2.Register(base.adapter()); err != nil {
		t.Fatal(err)
	}
	if err := o2.Register(boom.adapter()); err != nil {
		t.Fatal(err)
	}
	if err := o2.Start(context.Background()); err == nil {
		t.Fatal("Start should fail")
	}
	base.prepares, base.stops = 0, 0 // ignore rollback's Stop of base; count only Shutdown's hooks
	rep2 := o2.Shutdown(time.Second)
	if len(rep2.Outcomes) != 0 {
		t.Errorf("Shutdown after a failed Start = %+v, want empty report", rep2)
	}
	if base.prepares != 0 || base.stops != 0 {
		t.Errorf("Shutdown after a failed Start ran hooks: prepares=%d stops=%d", base.prepares, base.stops)
	}
}
