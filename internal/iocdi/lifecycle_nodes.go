package iocdi

import (
	"fmt"
	"strings"
)

// This file implements the ADR 0070 lifecycle-node registry and immutable plan
// (docs/v2-design/lifecycle.md §3.1). It is DECLARATIVE metadata only — no lifecycle behavior;
// the orchestrator (internal/lifecycle/orchestrator) consumes the plan and drives transitions.
// It lives beside bean registration but is independent of it: a lifecycle node need not be a
// bean, and a bean need not be a node.

// Priority marks a node's shutdown fence (ADR 0070). Exactly one RFCritical node is allowed; it is
// the sole teardown attempted until its transition returns or the deadline fires.
type Priority uint8

const (
	Normal     Priority = iota // ordinary drain node
	RFCritical                 // the RF-safety fence (bridge)
)

// Node is declarative lifecycle metadata for one daemon subsystem. Node IDs and dependency
// references are case-INSENSITIVE (lower-cased on registration), matching bean-ID semantics so a
// node that is also a bean can share the identifier.
type Node struct {
	Name         string
	StartAfter   []string // explicit start prerequisites (covers non-bean nodes; merged with DI edges)
	DrainAfter   []string // drain prerequisites: this node's teardown waits until each is Drained/Inactive
	StopPriority Priority
}

// RegisterNode adds a lifecycle node, preserving registration order (the shutdown tiebreak). It
// validates the ID (non-empty, unique) at the call, like Register does for beans; reference and
// graph validation happen in Plan. It fails once Plan has frozen the registry.
func (c *Container) RegisterNode(n Node) error {
	name := normalizeID(n.Name)
	if name == "" {
		return ErrEmptyNodeID
	}
	c.regMu.Lock()
	defer c.regMu.Unlock()
	if c.planFrozen.Load() {
		return ErrPlanFrozen
	}
	for _, e := range c.lifecycleNodes {
		if e.Name == name {
			return fmt.Errorf("%w: %q", ErrDuplicateNodeID, name)
		}
	}
	c.lifecycleNodes = append(c.lifecycleNodes, Node{
		Name:         name,
		StartAfter:   normalizeIDs(n.StartAfter),
		DrainAfter:   normalizeIDs(n.DrainAfter),
		StopPriority: n.StopPriority,
	})
	return nil
}

// Plan freezes the registry and builds + validates the immutable plan (the single source of truth
// for lifecycle ordering). It fails on: an empty/duplicate ID (also caught at RegisterNode), a
// StartAfter/DrainAfter reference to an unknown node, a cycle in the start graph OR (independently)
// the drain graph, and more than one RFCritical node.
func (c *Container) Plan() (*Plan, error) {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	c.planFrozen.Store(true) // closes node AND bean registration (P1): the DI snapshot is now final

	nodes := c.lifecycleNodes // already in registration order, IDs normalized
	names := make([]string, len(nodes))
	byName := make(map[string]Node, len(nodes))
	rfCount := 0
	for i, n := range nodes {
		names[i] = n.Name
		byName[n.Name] = n
		if n.StopPriority == RFCritical {
			rfCount++
		}
	}
	if rfCount > 1 {
		return nil, fmt.Errorf("%w: %d found", ErrMultipleRFCritical, rfCount)
	}

	// Reference validation: every StartAfter/DrainAfter names a registered node.
	for _, n := range nodes {
		for _, ref := range append(append([]string{}, n.StartAfter...), n.DrainAfter...) {
			if _, ok := byName[ref]; !ok {
				return nil, fmt.Errorf("%w: node %q references %q", ErrUnknownNodeDep, n.Name, ref)
			}
		}
	}

	// Start graph: explicit StartAfter UNION the DI-derived edges (a bean-node's dependencies that
	// are themselves lifecycle nodes), deduped. Its topological order (ties by registration order)
	// is StartOrder(); a cycle is fatal.
	startPrereq := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		startPrereq[n.Name] = c.mergedStartPrereqLocked(n, byName)
	}
	order, cyclic := topoOrder(names, startPrereq)
	if cyclic {
		return nil, ErrStartCycle
	}

	// Drain graph: DrainAfter only, validated for a cycle INDEPENDENTLY (a legal drain order need
	// not be the reverse of a legal start order).
	drainPrereq := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		drainPrereq[n.Name] = n.DrainAfter
	}
	if _, cyclic := topoOrder(names, drainPrereq); cyclic {
		return nil, ErrDrainCycle
	}

	return &Plan{nodes: cloneNodes(nodes), startOrder: append([]string(nil), order...)}, nil
}

// mergedStartPrereq is the effective start-prerequisite set for a node: its explicit StartAfter
// UNION the beans it di.inject-depends on that are ALSO lifecycle nodes, deduped. Non-bean nodes
// contribute only their explicit StartAfter. Caller holds regMu.
func (c *Container) mergedStartPrereqLocked(n Node, byName map[string]Node) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(id string) {
		// A self-edge (id == n.Name) is NOT dropped (codex P2): an explicit self-dependency is a
		// cycle and must surface as ErrStartCycle, not a valid-looking plan. Beans never self-inject,
		// so the DI-merge produces no false self-edges.
		if !seen[id] {
			if _, ok := byName[id]; ok { // only edges between lifecycle nodes matter
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	for _, sa := range n.StartAfter {
		add(sa)
	}
	if bn, ok := c.registeredBeans[n.Name]; ok && bn.hasDependencies {
		for _, dep := range bn.dependencies {
			add(dep) // bean IDs are already normalized (lower-cased) at Register
		}
	}
	return out
}

// Plan is the immutable lifecycle graph. Nodes() and StartOrder() return defensive copies.
type Plan struct {
	nodes      []Node
	startOrder []string
}

// Nodes returns the lifecycle nodes in registration order as a deep copy (the StartAfter/DrainAfter
// slices are copied too), so no consumer can mutate the shared graph.
func (p *Plan) Nodes() []Node { return cloneNodes(p.nodes) }

// StartOrder returns the topological start order (ties by registration order) as a copy.
func (p *Plan) StartOrder() []string { return append([]string(nil), p.startOrder...) }

// topoOrder is Kahn's algorithm with a deterministic registration-order tiebreak: among nodes whose
// prerequisites are all emitted, it emits the earliest-registered. names is the registration order;
// prereq[name] lists the nodes that must precede name. Returns cyclic=true iff some node can never
// be emitted (a cycle). O(V^2) — V is tiny (~12 nodes).
func topoOrder(names []string, prereq map[string][]string) (order []string, cyclic bool) {
	index := make(map[string]int, len(names))
	for i, n := range names {
		index[n] = i
	}
	indeg := make(map[string]int, len(names))
	dependents := make(map[string][]string, len(names))
	for _, n := range names {
		for _, p := range prereq[n] {
			indeg[n]++
			dependents[p] = append(dependents[p], n)
		}
	}
	emitted := make(map[string]bool, len(names))
	order = make([]string, 0, len(names))
	for len(order) < len(names) {
		next, nextIdx := "", -1
		for _, n := range names {
			if !emitted[n] && indeg[n] == 0 && (nextIdx == -1 || index[n] < nextIdx) {
				next, nextIdx = n, index[n]
			}
		}
		if next == "" {
			return order, true // no eligible node ⇒ a cycle remains
		}
		emitted[next] = true
		order = append(order, next)
		for _, d := range dependents[next] {
			indeg[d]--
		}
	}
	return order, false
}

func normalizeID(id string) string { return strings.ToLower(strings.TrimSpace(id)) }
func normalizeIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = normalizeID(id)
	}
	return out
}

func cloneNodes(nodes []Node) []Node {
	out := make([]Node, len(nodes))
	for i, n := range nodes {
		out[i] = Node{
			Name:         n.Name,
			StartAfter:   append([]string(nil), n.StartAfter...),
			DrainAfter:   append([]string(nil), n.DrainAfter...),
			StopPriority: n.StopPriority,
		}
	}
	return out
}
