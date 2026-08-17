// Package orchestrator drives the runtime lifecycle transitions of ADR 0070
// (docs/decisions/0070-daemon-lifecycle-graph.md, design in docs/v2-design/lifecycle.md §4). It
// consumes the immutable iocdi lifecycle plan (topology + drain edges + the RF fence) and an
// Adapter per node, then owns start (initialize → start, with rollback) and shutdown (a budgeted,
// dependency-ordered drain that derives LC-2's stages and skips from declarations).
//
// Like the sibling Supervisor, this package is independent of logging: Shutdown returns a
// structured ShutdownReport and the caller (cmd/smd) formats the per-failure, per-skip, and
// single-deadline log lines. Policy and formatting stay outside the lifecycle mechanism.
package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle"
)

// Milestone is the highest transition the orchestrator successfully drove for a node; rollback
// unwinds from here. It exists because Phase has no "initialized": a node past Initialize but not
// Start is Idle-phase yet Initialized-milestone (rollback-eligible).
type Milestone uint8

const (
	MilestoneNone        Milestone = iota // not yet initialized
	MilestoneInitialized                  // Initialize succeeded, Start not (yet) run
	MilestoneRunning                      // Start succeeded (or auto-promoted for a nil-Start node)
)

// Result is the orchestrator's settled verdict about the stop transition it drove for a node.
// It is NEVER a Phase (a TimedOut node is still Stopping; a Skipped node is still Running).
type Result uint8

const (
	Pending  Result = iota // no shutdown verdict yet
	Drained                // Stop returned cleanly within budget
	Failed                 // Stop returned an error
	TimedOut               // the budget fired while Stop was still running
	Skipped                // a drain prerequisite was not satisfied; Stop not attempted
	Inactive               // config-disabled: pruned, its drain edges vacuously satisfied
)

// Adapter binds one immutable plan node (by NodeID) to a subsystem's concrete lifecycle hooks. Every
// hook is optional; the nil behavior is defined per field so divergent service shapes adapt at this
// boundary by closures (no reflection, no generic framework — ADR 0070).
type Adapter struct {
	NodeID string // binds to ONE plan node (case-insensitive, matched to the node ID)

	// Active is evaluated ONCE at Start and latched; nil ⇒ always active. A latched-inactive node is
	// pruned from every traversal and its Result is Inactive.
	Active func() bool

	// Initialize advances the node to MilestoneInitialized; nil ⇒ trivially initialized.
	Initialize func() error

	// Start advances the node to MilestoneRunning (wraps Supervisor.Start); nil ⇒ AUTO-PROMOTE to
	// Running once Initialized (construction-only nodes, e.g. logging: Initialize opens, Stop closes).
	Start func(ctx context.Context) error

	// PrepareStop is optional and MUST be non-blocking; run before the budget/fence at shutdown.
	// nil ⇒ no-op.
	PrepareStop func()

	// Stop is the supervised, budget-bounded teardown; a returned error ⇒ Failed; nil ⇒ trivially
	// Drained.
	Stop func(ctx context.Context) error

	// Rollback cleans up a node that advanced during a FAILED Start; nil ⇒ no-op. reached is the
	// milestone it had reached.
	Rollback func(reached Milestone) error

	// Phase is the node's observable phase for status; nil ⇒ inferred from milestone/result.
	Phase func() lifecycle.Phase
}

// Orchestrator drives one iocdi container's lifecycle. Construct it with the container (it reads the
// plan and claims init ownership at Start); register one Adapter per plan node; then Start / Shutdown.
type Orchestrator struct {
	c *iocdi.Container

	mu             sync.Mutex
	adapters       map[string]Adapter
	plan           *iocdi.Plan
	active         map[string]bool      // latched once at Start
	milestone      map[string]Milestone // highest transition reached per node
	result         map[string]Result    // shutdown verdict per node
	startAttempted bool                 // true once Start has begun initializing (terminal — no re-Start)
	started        bool                 // true after a fully successful Start
}

// New constructs an orchestrator bound to a container. Register adapters before Start.
func New(c *iocdi.Container) *Orchestrator {
	return &Orchestrator{
		c:         c,
		adapters:  make(map[string]Adapter),
		active:    make(map[string]bool),
		milestone: make(map[string]Milestone),
		result:    make(map[string]Result),
	}
}

// Register records one Adapter, keyed by its normalized NodeID. It rejects an empty or duplicate
// NodeID, and any registration once Start has begun. The NodeID is validated against the plan at
// Start (the plan does not exist until then).
func (o *Orchestrator) Register(a Adapter) error {
	id := normalizeID(a.NodeID)
	if id == "" {
		return fmt.Errorf("orchestrator: adapter has an empty NodeID")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.startAttempted {
		return fmt.Errorf("orchestrator: Register(%q) after Start", id)
	}
	if _, dup := o.adapters[id]; dup {
		return fmt.Errorf("orchestrator: adapter %q registered twice", id)
	}
	a.NodeID = id
	o.adapters[id] = a
	return nil
}

// Result returns the node's shutdown verdict (Pending before shutdown, Inactive for a config-disabled
// node latched at Start). An unknown node reports Pending.
func (o *Orchestrator) Result(node string) Result {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.result[normalizeID(node)]
}

// Milestone returns the highest start transition the orchestrator drove for the node.
func (o *Orchestrator) Milestone(node string) Milestone {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.milestone[normalizeID(node)]
}

func normalizeID(id string) string { return strings.ToLower(strings.TrimSpace(id)) }
