package orchestrator

// ADR 0070 phase-1 GATE — the §5 acceptance test (docs/v2-design/lifecycle.md §363). It declares the
// daemon's REAL lifecycle graph (the eight gracefulShutdown stages + a construction-only logging node)
// as iocdi nodes + adapters and asserts the orchestrator DERIVES the same safety partial order + skips
// that cmd/smd/shutdown.go hand-codes today — every drain constraint, the RF fence, and every skip —
// WITHOUT asserting the operational psk/http/workers tiebreak. The graph is declared inline here for
// phase 1 (moving it into cmd/smd is phase 3).
//
// Criteria (asserted through the ShutdownReport, Result(), AND hook coordination):
//
//   §5-1  bridge (RFCritical) is the SOLE teardown until it returns (fence) — proven by holding its
//         Stop and asserting no other Stop began before it returned.
//   §5-2/3/4  happy path: all Drained, ft8 before evidence, ft8 before qso-log, {http,workers,qso-log}
//         all before hub.
//   §5-5  a producer that did not drain (ft8 Failed) ⇒ evidence Skipped[ft8] and qso-log Skipped[ft8].
//   §5-6  ft8 Failed ⇒ hub Skipped[qso-log] transitively (http/workers drained). A hanging ft8 is NOT
//         used here: it consumes the shared budget before http/workers are reached (ft8 precedes them
//         in the real stage order), so those independent nodes would race an expired ctx and hub's
//         BlockedBy would widen to [http workers qso-log]. Failing fast isolates the transitive skip.
//   §5-5t ft8 TimedOut ⇒ evidence/qso-log Skipped[ft8] and FirstTimedOut==ft8 (the timeout path also
//         triggers the skips); the downstream budget cascade is not asserted (it is non-deterministic).
//   §5-6b ft8 Drained but http Failed under a GENEROUS budget ⇒ hub Skipped[http] (prerequisite-
//         qualified, not budget-driven), workers/qso-log Drained.
//   §5-7  ft8+qso-log config-disabled ⇒ hub Drained and evidence Drained (Inactive satisfies
//         vacuously); the two Inactive nodes are absent from Outcomes but Result()==Inactive.
//   §5-8  the mutual order of psk/http/workers is NOT asserted (operational tiebreak).
//   §5-9  a construction-only logging node (nil Start ⇒ auto-promoted) reaches Drained in the happy
//         path. It carries NO DrainAfter edges so it stays usable while cmd/smd formats the report;
//         the "drain strictly after the rest" sequencing is a phase-3 concern, not asserted here.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/iocdi"
)

// daemonGraph is the real hand-declared lifecycle graph, derived from cmd/smd/shutdown.go.
func daemonGraph() []iocdi.Node {
	return []iocdi.Node{
		{Name: "bridge", StopPriority: iocdi.RFCritical}, // RF unkey — the fence
		{Name: "ft8"}, // sole producer into evidence/qso-log/hub
		{Name: "psk"}, // independent flush
		{Name: "evidence", DrainAfter: []string{"ft8"}},
		{Name: "http"},
		{Name: "workers"},
		{Name: "qso-log", DrainAfter: []string{"ft8"}},
		{Name: "hub", DrainAfter: []string{"http", "workers", "qso-log"}},
		{Name: "logging"}, // construction-only; nil Start; NO DrainAfter (stays drainable)
	}
}

// daemonAdapters builds a default Adapter per node (Active, trivial Start except logging's nil Start,
// a Stop that records + returns nil). Tests mutate the returned map before starting.
func daemonAdapters(rec *hookRec) map[string]*Adapter {
	m := make(map[string]*Adapter)
	for _, node := range daemonGraph() {
		n := node.Name
		a := &Adapter{
			NodeID: n,
			Active: func() bool { return true },
			Stop:   func(context.Context) error { rec.add("stop:" + n); return nil },
		}
		if n != "logging" { // logging is construction-only — nil Start auto-promotes to Running
			a.Start = func(context.Context) error { return nil }
		}
		m[n] = a
	}
	return m
}

// startDaemonOrch registers the graph + the (possibly customized) adapters in node order and Starts.
func startDaemonOrch(t *testing.T, m map[string]*Adapter) *Orchestrator {
	t.Helper()
	c := iocdi.New()
	for _, node := range daemonGraph() {
		if err := c.RegisterNode(node); err != nil {
			t.Fatalf("RegisterNode(%q): %v", node.Name, err)
		}
	}
	o := New(c)
	for _, node := range daemonGraph() {
		if err := o.Register(*m[node.Name]); err != nil {
			t.Fatalf("Register(%q): %v", node.Name, err)
		}
	}
	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return o
}

func resultsDrained(t *testing.T, rep ShutdownReport, nodes ...string) {
	t.Helper()
	for _, n := range nodes {
		oc, ok := outcomeFor(rep, n)
		if !ok || oc.Result != Drained {
			t.Errorf("%s outcome = %+v (present=%v), want Drained", n, oc, ok)
		}
	}
}

// mustPrecede asserts a drained before b in the recorded stop order.
func mustPrecede(t *testing.T, order []string, a, b string) {
	t.Helper()
	ia, ib := indexOf(order, a), indexOf(order, b)
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("stop order %v: want %s before %s", order, a, b)
	}
}

// §5-2/3/4/9: happy path — everything Drained in the safety partial order; logging drains too.
func TestAcceptance_HappyPathPartialOrder(t *testing.T) {
	rec := &hookRec{}
	o := startDaemonOrch(t, daemonAdapters(rec))
	rep := o.Shutdown(2 * time.Second)

	resultsDrained(t, rep, "bridge", "ft8", "psk", "evidence", "http", "workers", "qso-log", "hub", "logging")
	order := stopOrder(rec.snapshot())
	mustPrecede(t, order, "ft8", "evidence")
	mustPrecede(t, order, "ft8", "qso-log")
	mustPrecede(t, order, "http", "hub")
	mustPrecede(t, order, "workers", "hub")
	mustPrecede(t, order, "qso-log", "hub")
	// §5-1 lite: bridge is first. (Exclusivity has its own test below.)
	if len(order) == 0 || order[0] != "bridge" {
		t.Errorf("stop order = %v, want bridge first", order)
	}
	// §5-9: the construction-only logging node reached Drained via auto-promotion.
	if got := o.Milestone("logging"); got != MilestoneRunning {
		t.Errorf("logging Milestone = %d, want MilestoneRunning (nil Start auto-promote)", got)
	}
}

// §5-1: the RF fence is the SOLE teardown until its Stop RETURNS. The fence is HELD BLOCKED inside
// its Stop under test control (its outcome stays un-recorded — Result != Drained — throughout). Each
// non-fence Stop that runs while the fence is not yet Drained SIGNALS an overlap; the test rules out
// any overlap (observing that signal) BEFORE it releases the fence, so a correct sequential shutdown —
// whose loop is blocked on the fence, running no non-fence Stop — passes, while a concurrent
// regression signals and fails. Checking before release closes the release-vs-return window (five
// codex rounds); a bounded select on entry avoids a hang if the fence is skipped.
func TestAcceptance_RFFenceIsSole(t *testing.T) {
	rec := &hookRec{}
	m := daemonAdapters(rec)
	var o *Orchestrator
	fenceEntered := make(chan struct{})
	release := make(chan struct{})
	overlap := make(chan string, len(daemonGraph())) // a non-fence Stop that ran before the fence returned
	m["bridge"].Stop = func(context.Context) error {
		rec.add("stop:bridge")
		close(fenceEntered)
		<-release // held executing — bridge's result stays Pending until released
		return nil
	}
	for _, node := range daemonGraph() {
		if node.Name == "bridge" {
			continue
		}
		n := node.Name
		m[n].Stop = func(context.Context) error {
			if o.Result("bridge") != Drained { // ran while the fence had not yet returned/recorded
				overlap <- n
			}
			rec.add("stop:" + n)
			return nil
		}
	}
	o = startDaemonOrch(t, m)
	done := make(chan ShutdownReport, 1)
	go func() { done <- o.Shutdown(2 * time.Second) }()
	select {
	case <-fenceEntered:
	case <-done:
		t.Fatal("Shutdown completed without entering the RF fence")
	case <-time.After(2 * time.Second):
		t.Fatal("RF fence was never entered")
	}
	// Rule out any overlap BEFORE releasing: while the fence is held blocked, a correct sequential
	// shutdown's loop is blocked on it so no non-fence Stop runs, while a concurrent regression runs
	// one immediately and it signals here.
	select {
	case n := <-overlap:
		t.Errorf("%s Stop overlapped the still-executing RF fence", n)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	rep := <-done
	// Re-check overlap after Shutdown returned: every non-fence Stop has run by now (runBounded waits
	// for each), so any signal descheduled past the window above is buffered and caught here.
	//
	// The RF-fence-sole invariant is guaranteed by construction — the synchronous blocking drain(fence)
	// call (shutdown.go) — and this test catches the real regression (draining the fence in a
	// goroutine) deterministically: reversion go drain(fence) → 40 FAIL / 0 PASS across 20 runs. The
	// residual >2s goroutine-starvation window in overlap-signal delivery is pathological CI
	// contention, not a realistic miss; it is accepted rather than chased with further timing-window
	// hardening (7 codex rounds, 0 production defects; operator-accepted 2026-08-17).
	select {
	case n := <-overlap:
		t.Errorf("%s Stop overlapped the still-executing RF fence (late signal)", n)
	default:
	}
	if len(rep.Outcomes) == 0 || rep.Outcomes[0].Node != "bridge" {
		t.Errorf("Outcomes[0] = %+v, want bridge (fence drains first)", rep.Outcomes)
	}
	resultsDrained(t, rep, "bridge", "ft8", "hub", "logging")
}

// §5-5/§5-6: ft8 Failed (fast) ⇒ evidence/qso-log Skipped[ft8], hub Skipped[qso-log] (transitive),
// with http/workers Drained (budget intact). Deterministic — a failing producer does not consume the
// budget, so the independent publishers drain and hub's only blocker is qso-log.
func TestAcceptance_Ft8FailSkipsProducersAndHub(t *testing.T) {
	rec := &hookRec{}
	m := daemonAdapters(rec)
	m["ft8"].Stop = func(context.Context) error { rec.add("stop:ft8"); return errors.New("ft8 stop failed") }
	o := startDaemonOrch(t, m)
	rep := o.Shutdown(2 * time.Second) // generous — nothing times out

	if rep.FirstTimedOut != "" {
		t.Errorf("FirstTimedOut = %q, want empty (a fast failure, no timeout)", rep.FirstTimedOut)
	}
	if oc, _ := outcomeFor(rep, "ft8"); oc.Result != Failed {
		t.Errorf("ft8 result = %d, want Failed", oc.Result)
	}
	for _, n := range []string{"evidence", "qso-log"} {
		oc, _ := outcomeFor(rep, n)
		if oc.Result != Skipped || !reflect.DeepEqual(oc.BlockedBy, []string{"ft8"}) {
			t.Errorf("%s outcome = %+v, want Skipped BlockedBy [ft8]", n, oc)
		}
	}
	ocHub, _ := outcomeFor(rep, "hub")
	if ocHub.Result != Skipped || !reflect.DeepEqual(ocHub.BlockedBy, []string{"qso-log"}) {
		t.Errorf("hub outcome = %+v, want Skipped BlockedBy [qso-log] (transitive)", ocHub)
	}
	resultsDrained(t, rep, "http", "workers", "bridge", "psk", "logging")
}

// §5-5t: ft8 TimedOut also triggers the producer skips. The hanging ft8 consumes the budget, so the
// downstream cascade (http/workers, hub's exact BlockedBy) is non-deterministic and NOT asserted —
// only that the timeout path skips evidence/qso-log and names ft8 as the first timeout.
func TestAcceptance_Ft8TimeoutStillSkipsProducers(t *testing.T) {
	rec := &hookRec{}
	m := daemonAdapters(rec)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	m["ft8"].Stop = func(context.Context) error { rec.add("stop:ft8"); <-block; return nil }
	o := startDaemonOrch(t, m)
	rep := o.Shutdown(120 * time.Millisecond)

	if oc, _ := outcomeFor(rep, "ft8"); oc.Result != TimedOut {
		t.Errorf("ft8 result = %d, want TimedOut", oc.Result)
	}
	if rep.FirstTimedOut != "ft8" {
		t.Errorf("FirstTimedOut = %q, want ft8", rep.FirstTimedOut)
	}
	for _, n := range []string{"evidence", "qso-log"} {
		oc, _ := outcomeFor(rep, n)
		if oc.Result != Skipped || !reflect.DeepEqual(oc.BlockedBy, []string{"ft8"}) {
			t.Errorf("%s outcome = %+v, want Skipped BlockedBy [ft8]", n, oc)
		}
	}
}

// §5-6b: a DIRECT hub-prerequisite failure under a generous budget — http Failed ⇒ hub Skipped[http],
// proving the hub skip is prerequisite-qualified, not budget-driven.
func TestAcceptance_HttpFailSkipsHubWithoutTimeout(t *testing.T) {
	rec := &hookRec{}
	m := daemonAdapters(rec)
	errHTTP := errors.New("http shutdown failed")
	m["http"].Stop = func(context.Context) error { rec.add("stop:http"); return errHTTP }
	o := startDaemonOrch(t, m)
	rep := o.Shutdown(2 * time.Second) // generous — nothing times out

	if rep.FirstTimedOut != "" {
		t.Errorf("FirstTimedOut = %q, want empty (nothing timed out)", rep.FirstTimedOut)
	}
	if oc, _ := outcomeFor(rep, "http"); oc.Result != Failed || !errors.Is(oc.Err, errHTTP) {
		t.Errorf("http outcome = %+v, want Failed carrying errHTTP", oc)
	}
	resultsDrained(t, rep, "workers", "qso-log", "ft8")
	ocHub, _ := outcomeFor(rep, "hub")
	if ocHub.Result != Skipped || !reflect.DeepEqual(ocHub.BlockedBy, []string{"http"}) {
		t.Errorf("hub outcome = %+v, want Skipped BlockedBy [http] (prerequisite-qualified)", ocHub)
	}
}

// §5-7: ft8 + qso-log config-disabled ⇒ hub and evidence still Drain (Inactive satisfies vacuously);
// the Inactive nodes are absent from Outcomes but observable via Result().
func TestAcceptance_DisabledPublisherLetsHubClose(t *testing.T) {
	rec := &hookRec{}
	m := daemonAdapters(rec)
	m["ft8"].Active = func() bool { return false }
	m["qso-log"].Active = func() bool { return false }
	o := startDaemonOrch(t, m)
	rep := o.Shutdown(2 * time.Second)

	// hub closes and evidence drains despite the disabled FT8 producer/publisher.
	resultsDrained(t, rep, "hub", "evidence", "http", "workers", "bridge", "psk", "logging")
	// The disabled nodes: Inactive via Result(), absent from Outcomes.
	for _, n := range []string{"ft8", "qso-log"} {
		if got := o.Result(n); got != Inactive {
			t.Errorf("Result(%s) = %d, want Inactive", n, got)
		}
		if _, ok := outcomeFor(rep, n); ok {
			t.Errorf("%s (Inactive) appeared in Outcomes; it should be pruned", n)
		}
	}
}
