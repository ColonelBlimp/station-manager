package orchestrator

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/iocdi"
)

// NodeOutcome is one node's settled shutdown verdict. Err is set only for a Failed Stop; BlockedBy
// lists the failed prerequisites (in the node's declared DrainAfter order) only for a Skipped node.
type NodeOutcome struct {
	Node      string
	Result    Result
	Err       error
	BlockedBy []string
}

// ShutdownReport is the ordered snapshot Shutdown returns. Outcomes are in traversal order (the RF
// fence first, then the topological drain); Inactive nodes are pruned from traversal and do NOT
// appear (they stay observable via Result(node) == Inactive). FirstTimedOut names the first node
// whose Stop the deadline interrupted (empty if none). The caller (cmd/smd) formats the per-failure,
// per-skip, and single-deadline log lines from this — the orchestrator stays logging-free.
type ShutdownReport struct {
	Outcomes      []NodeOutcome
	FirstTimedOut string
}

// Shutdown drives the budgeted, dependency-ordered teardown (design §4.3) and returns the settled
// ShutdownReport. It is idempotent: a second or concurrent call reruns no hook and returns the same
// report. Before a successful Start (never started, or Start failed and rolled back) it is an empty
// no-op that touches nothing.
func (o *Orchestrator) Shutdown(budget time.Duration) ShutdownReport {
	o.transitionMu.Lock()
	defer o.transitionMu.Unlock()
	if o.shutdownDone {
		return cloneReport(o.report) // idempotent — a defensive copy of the settled report, no re-run
	}
	if !o.started {
		// Never started, or Start failed+rolled back — an empty no-op. It must NOT latch shutdownDone
		// (codex P1): a Shutdown racing ahead of Start would otherwise cache this empty report and every
		// later Shutdown — after Start brought subsystems up — would return it, leaving them running.
		return ShutdownReport{}
	}
	o.shutdownDone = true

	nodes := o.plan.Nodes() // registration order
	byName := make(map[string]iocdi.Node, len(nodes))
	for _, n := range nodes {
		byName[n.Name] = n
	}

	// 1. PrepareStop — every active node, BEFORE the budget context exists (must be non-blocking).
	for _, n := range nodes {
		if o.active[n.Name] {
			if a := o.adapters[n.Name]; a.PrepareStop != nil {
				a.PrepareStop()
			}
		}
	}

	// 2. Budget — one deadline bounds every Stop below (budget<=0 ⇒ already expired: WithTimeout with
	//    a non-positive duration returns an already-cancelled context).
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	var outcomes []NodeOutcome
	firstTimedOut := ""
	record := func(name string, r Result, err error, blockedBy []string) {
		o.setResult(name, r)
		outcomes = append(outcomes, NodeOutcome{Node: name, Result: r, Err: err, BlockedBy: blockedBy})
		if r == TimedOut && firstTimedOut == "" {
			firstTimedOut = name
		}
	}
	drain := func(name string) {
		a := o.adapters[name]
		if a.Stop == nil {
			record(name, Drained, nil, nil) // nil Stop ⇒ trivially Drained
			return
		}
		err, timedOut := runBounded(ctx, a.Stop)
		switch {
		case timedOut:
			record(name, TimedOut, nil, nil)
		case err != nil:
			record(name, Failed, err, nil)
		default:
			record(name, Drained, nil, nil)
		}
	}

	// Inactive nodes are pre-settled: pruned from traversal, their Inactive result already vacuously
	// satisfies every drain edge (set at Start).
	settled := make(map[string]bool)
	for _, n := range nodes {
		if !o.active[n.Name] {
			settled[n.Name] = true
		}
	}

	// 3. Fence — the active RFCritical node drains FIRST and SOLE: runBounded blocks until its Stop
	//    returns or the deadline fires, so the sequential loop cannot start any other Stop before then.
	//    It is marked settled so the traversal below never runs it a second time. An inactive fence is
	//    simply pruned (no fence phase).
	fence := ""
	for _, n := range nodes {
		if n.StopPriority == iocdi.RFCritical && o.active[n.Name] {
			fence = n.Name
			break
		}
	}
	if fence != "" {
		drain(fence)
		settled[fence] = true
	}

	// 4. Sequential topological drain of the remaining active non-fence nodes. A node is PROCESSED
	//    once all its DrainAfter prereqs are settled (deterministic: earliest-registered eligible
	//    node). It drains iff every prereq result is Drained/Inactive; otherwise it is Skipped
	//    (BlockedBy = the failed prereqs in declared order), and its Skipped propagates to dependents.
	for {
		pick := ""
		for _, n := range nodes {
			if settled[n.Name] {
				continue
			}
			if o.prereqsSettled(byName[n.Name], settled) {
				pick = n.Name
				break
			}
		}
		if pick == "" {
			break // every active non-fence node settled (the drain graph is acyclic — Plan checks)
		}
		if blockers := o.drainBlockers(byName[pick]); len(blockers) > 0 {
			record(pick, Skipped, nil, blockers)
		} else {
			drain(pick)
		}
		settled[pick] = true
	}

	o.report = ShutdownReport{Outcomes: outcomes, FirstTimedOut: firstTimedOut}
	return cloneReport(o.report)
}

// cloneReport deep-copies a settled report (the Outcomes slice AND each nested BlockedBy) so a caller
// mutating a returned report cannot corrupt the cached report or any later idempotent return (codex
// P2). Matches the Plan's defensive-copy discipline.
func cloneReport(r ShutdownReport) ShutdownReport {
	out := ShutdownReport{FirstTimedOut: r.FirstTimedOut}
	if r.Outcomes != nil {
		out.Outcomes = make([]NodeOutcome, len(r.Outcomes))
		for i, oc := range r.Outcomes {
			out.Outcomes[i] = oc
			if oc.BlockedBy != nil {
				out.Outcomes[i].BlockedBy = append([]string(nil), oc.BlockedBy...)
			}
		}
	}
	return out
}

// prereqsSettled reports whether every DrainAfter prerequisite of n has a settled result.
func (o *Orchestrator) prereqsSettled(n iocdi.Node, settled map[string]bool) bool {
	for _, dep := range n.DrainAfter {
		if !settled[dep] {
			return false
		}
	}
	return true
}

// drainBlockers returns n's DrainAfter prerequisites whose result is NOT Drained/Inactive, in the
// node's DECLARED DrainAfter order — the send-on-closed-channel safety edges that were not satisfied.
func (o *Orchestrator) drainBlockers(n iocdi.Node) []string {
	var blockers []string
	for _, dep := range n.DrainAfter {
		switch o.getResult(dep) {
		case Drained, Inactive:
			// satisfied
		default:
			blockers = append(blockers, dep)
		}
	}
	return blockers
}
