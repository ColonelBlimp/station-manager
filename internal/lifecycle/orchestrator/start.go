package orchestrator

import (
	"context"
	"fmt"
)

// Start brings the container up: it validates the plan and adapter coverage, claims initialization
// ownership, latches each node's Active() once, then drives Initialize → Start for every active node
// in the plan's topological start order, recording each node's Milestone. On ANY failure it rolls
// back every advanced node from its recorded milestone (reverse order) and returns the error — the
// daemon then exits without a shutdown traversal, so shutdown only ever meets Running or Inactive
// nodes. Idempotent after success; terminal after a failed start (no re-Start, which would
// double-initialize a partially-started graph).
func (o *Orchestrator) Start(ctx context.Context) error {
	o.transitionMu.Lock()
	defer o.transitionMu.Unlock()
	if o.started {
		return nil
	}
	if o.startAttempted {
		return fmt.Errorf("orchestrator: Start already attempted (terminal after a failed start)")
	}

	// 1. Plan validation — freezes registration and validates topology (cycles, refs, one fence).
	plan, perr := o.c.Plan()
	if perr != nil {
		return fmt.Errorf("orchestrator: invalid lifecycle plan: %w", perr)
	}
	nodes := plan.Nodes()

	// 2. Construction validation — every plan node has exactly one adapter, and every adapter binds a
	//    plan node. A missing adapter (or a stray one) is a wiring bug that must fail fast, before any
	//    ownership claim or initializer runs.
	planNames := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		planNames[n.Name] = true
		if _, ok := o.adapters[n.Name]; !ok {
			return fmt.Errorf("orchestrator: plan node %q has no adapter", n.Name)
		}
	}
	for id := range o.adapters {
		if !planNames[id] {
			return fmt.Errorf("orchestrator: adapter %q is not a plan node", id)
		}
	}

	// 3. Claim initialization ownership (single-init-owner) — AFTER validation, BEFORE any Active() or
	//    initializer. The claim is durable: even if Start fails and rolls back below, Build() stays
	//    forbidden.
	if cerr := o.c.BeginOrchestratorInit(); cerr != nil {
		return fmt.Errorf("orchestrator: cannot claim initialization: %w", cerr)
	}
	o.plan = plan
	o.startAttempted = true

	// 4. Latch every node's Active() exactly once; prune the inactive ones (Result = Inactive).
	for _, n := range nodes {
		a := o.adapters[n.Name]
		act := a.Active == nil || a.Active() // Active() called OUTSIDE mu (may query orchestrator state)
		o.active[n.Name] = act
		if !act {
			o.setResult(n.Name, Inactive)
		}
	}

	// 5. Initialize → Start each active node in topological start order, recording milestones. On any
	//    failure, unwind every advanced node (reverse) and return the wrapped error.
	var advanced []string
	for _, name := range plan.StartOrder() {
		if !o.active[name] {
			continue // pruned
		}
		a := o.adapters[name]
		if a.Initialize != nil {
			if ierr := a.Initialize(); ierr != nil {
				// The failing node never reached Initialized, so it is NOT in `advanced`: Initialize
				// owns its own partial-state cleanup (matching Supervisor.Start's acquire discipline).
				o.rollback(ctx, advanced)
				return fmt.Errorf("orchestrator: initialize %q: %w", name, ierr)
			}
		}
		o.setMilestone(name, MilestoneInitialized)
		advanced = append(advanced, name) // now rollback-eligible via its own Rollback

		if a.Start != nil {
			if serr := a.Start(ctx); serr != nil {
				o.rollback(ctx, advanced)
				return fmt.Errorf("orchestrator: start %q: %w", name, serr)
			}
		}
		o.setMilestone(name, MilestoneRunning) // a real Start, or an auto-promoted nil-Start node
	}

	o.started = true
	return nil
}

// rollback unwinds the advanced nodes in REVERSE order from their recorded milestone:
// MilestoneRunning → Stop, MilestoneInitialized → Rollback(MilestoneInitialized). Each teardown is
// bounded by the caller's ctx (runBounded), so a hook that ignores its context cannot wedge Start
// past ctx's deadline — the abandoned teardown runs on against the exiting process (codex P1 on
// 19192efa). It runs on the already-failing start path, so hook errors are best-effort: the
// actionable error is the start failure Start returns. Callbacks run WITHOUT o.mu held (only the
// short milestone read/write take it). Caller holds transitionMu.
func (o *Orchestrator) rollback(ctx context.Context, advanced []string) {
	for i := len(advanced) - 1; i >= 0; i-- {
		name := advanced[i]
		a := o.adapters[name]
		switch o.getMilestone(name) {
		case MilestoneRunning:
			if a.Stop != nil {
				_, _ = runBounded(ctx, a.Stop)
			}
		case MilestoneInitialized:
			if a.Rollback != nil {
				_, _ = runBounded(ctx, func(context.Context) error { return a.Rollback(MilestoneInitialized) })
			}
		}
		o.setMilestone(name, MilestoneNone)
	}
}
