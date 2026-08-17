package iocdi

// ADR 0070 phase 1 — the iocdi lifecycle-node registry + immutable plan
// (docs/v2-design/lifecycle.md §3.1). Observable criteria:
//
//   AC-1  RegisterNode rejects an empty or duplicate ID (case-insensitive) and preserves
//         registration order (the deterministic shutdown tiebreak).
//   AC-2  Plan rejects a StartAfter/DrainAfter reference to an unknown node.
//   AC-3  Plan rejects a cycle in the start graph.
//   AC-4  Plan rejects a cycle in the drain graph INDEPENDENTLY of the start graph.
//   AC-5  Plan rejects more than one RFCritical node.
//   AC-6  StartOrder is topological over the merged start graph (explicit StartAfter UNION the
//         DI-derived edges), ties broken by registration order.
//   AC-7  Plan freezes the registry (RegisterNode after Plan fails), and Nodes()/StartOrder()
//         return defensive deep copies.

import (
	"errors"
	"reflect"
	"testing"
)

// Beans for the DI-merge test: diB di.inject-depends on diA.
type diA struct{}
type diB struct {
	A *diA `di.inject:"a"`
}

func nodeNames(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// AC-1: empty + duplicate (case-insensitive) rejected; order preserved.
func TestRegisterNode_RejectsEmptyAndDuplicate_PreservesOrder(t *testing.T) {
	c := New()
	if err := c.RegisterNode(Node{Name: "   "}); !errors.Is(err, ErrEmptyNodeID) {
		t.Errorf("empty-ID RegisterNode err = %v, want ErrEmptyNodeID", err)
	}
	for _, n := range []string{"first", "second", "third"} {
		if err := c.RegisterNode(Node{Name: n}); err != nil {
			t.Fatalf("RegisterNode(%q): %v", n, err)
		}
	}
	if err := c.RegisterNode(Node{Name: "Second"}); !errors.Is(err, ErrDuplicateNodeID) {
		t.Errorf("duplicate RegisterNode err = %v, want ErrDuplicateNodeID (case-insensitive)", err)
	}
	p, err := c.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got, want := nodeNames(p.Nodes()), []string{"first", "second", "third"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Nodes() order = %v, want registration order %v", got, want)
	}
}

// AC-2: an unknown StartAfter/DrainAfter reference is rejected.
func TestPlan_RejectsUnknownReference(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a", DrainAfter: []string{"ghost"}})
	if _, err := c.Plan(); !errors.Is(err, ErrUnknownNodeDep) {
		t.Errorf("Plan err = %v, want ErrUnknownNodeDep", err)
	}
}

// AC-3: a start-graph cycle is rejected.
func TestPlan_RejectsStartCycle(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a", StartAfter: []string{"b"}})
	_ = c.RegisterNode(Node{Name: "b", StartAfter: []string{"a"}})
	if _, err := c.Plan(); !errors.Is(err, ErrStartCycle) {
		t.Errorf("Plan err = %v, want ErrStartCycle", err)
	}
}

// AC-4: a drain-graph cycle is rejected INDEPENDENTLY — the start graph here is acyclic (empty),
// so only the drain cycle can be the cause.
func TestPlan_RejectsDrainCycleIndependently(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a", DrainAfter: []string{"b"}})
	_ = c.RegisterNode(Node{Name: "b", DrainAfter: []string{"a"}})
	if _, err := c.Plan(); !errors.Is(err, ErrDrainCycle) {
		t.Errorf("Plan err = %v, want ErrDrainCycle (start graph is acyclic)", err)
	}
}

// AC-5: more than one RFCritical node is rejected.
func TestPlan_RejectsMultipleRFCritical(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a", StopPriority: RFCritical})
	_ = c.RegisterNode(Node{Name: "b", StopPriority: RFCritical})
	if _, err := c.Plan(); !errors.Is(err, ErrMultipleRFCritical) {
		t.Errorf("Plan err = %v, want ErrMultipleRFCritical", err)
	}
}

// AC-6: StartOrder is topological with a registration-order tiebreak. Registered [z, y, x] with
// z StartAfter x: x must precede z, and among the initially-eligible {y, x} the earlier-registered
// y goes first → [y, x, z], which differs from registration order (proving both properties).
func TestPlan_StartOrderTopologicalWithRegistrationTiebreak(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "z", StartAfter: []string{"x"}})
	_ = c.RegisterNode(Node{Name: "y"})
	_ = c.RegisterNode(Node{Name: "x"})
	p, err := c.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got, want := p.StartOrder(), []string{"y", "x", "z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("StartOrder = %v, want %v (topological + registration tiebreak)", got, want)
	}
}

// AC-6 (DI-merge): a node that is also a bean inherits the bean's di.inject dependencies as start
// edges. diB depends on diA, so a must precede b even though the NODES were registered [b, a].
func TestPlan_MergesDIDerivedStartEdges(t *testing.T) {
	c := New()
	if err := c.Register("a", reflect.TypeOf(diA{})); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := c.Register("b", reflect.TypeOf(diB{})); err != nil {
		t.Fatalf("Register b: %v", err)
	}
	_ = c.RegisterNode(Node{Name: "b"}) // reverse of the DI order, no explicit StartAfter
	_ = c.RegisterNode(Node{Name: "a"})
	p, err := c.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	order := p.StartOrder()
	if indexOf(order, "a") >= indexOf(order, "b") {
		t.Errorf("StartOrder = %v; the DI-derived edge a→b must place a before b", order)
	}
}

// codex P1: Plan freezes BEAN registration too — the plan snapshots the DI graph, so a bean
// registered afterward would leave the immutable plan missing an edge. Reversion: drop the
// planFrozen check in Register → a post-Plan Register succeeds.
func TestPlan_FreezesBeanRegistrationToo(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a"})
	if _, err := c.Plan(); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := c.Register("late", reflect.TypeOf(diA{})); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("Register after Plan err = %v, want ErrRegistrationClosed", err)
	}
	if err := c.RegisterInstance("late2", &diA{}); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("RegisterInstance after Plan err = %v, want ErrRegistrationClosed", err)
	}
}

// codex P1 (recheck): the early planFrozen fast-path can lose a lock race to Plan(). The seam runs
// Plan() between Register's fast-path and its lock; the under-regMu recheck must still refuse, so no
// bean lands after the plan snapshotted. Reversion: drop the under-lock recheck → Register succeeds.
func TestRegister_RechecksFreezeUnderLock(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "x"})

	var planned bool
	beanRegisterPreLockForTest = func() {
		if !planned { // Plan takes regMu, freezes + snapshots, releases — all before Register locks.
			planned = true
			if _, err := c.Plan(); err != nil {
				t.Fatalf("Plan in seam: %v", err)
			}
		}
	}
	defer func() { beanRegisterPreLockForTest = nil }()

	// diB depends on "a" — a rejected registration must leave NO bean AND no phantom required-dep.
	if err := c.Register("late", reflect.TypeOf(diB{})); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("Register racing Plan err = %v, want ErrRegistrationClosed (recheck under lock)", err)
	}
	if _, ok := c.registeredBeans["late"]; ok {
		t.Error("a bean was inserted after Plan snapshotted the graph")
	}
	if _, ok := c.requiredDependency["a"]; ok {
		t.Error("a rejected registration left a phantom required dependency (checkForDependency ran before the freeze check)")
	}
}

// codex P2: an explicit self-dependency is a cycle, not a valid plan. Reversion: restore the
// `id != n.Name` self-edge drop → the self-loop vanishes and Plan wrongly succeeds.
func TestPlan_RejectsExplicitSelfDependency(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a", StartAfter: []string{"a"}})
	if _, err := c.Plan(); !errors.Is(err, ErrStartCycle) {
		t.Errorf("Plan err = %v, want ErrStartCycle (an explicit self-dependency is a cycle)", err)
	}
}

// AC-7: Plan freezes the registry and returns defensive deep copies.
func TestPlan_FreezesAndReturnsDeepCopies(t *testing.T) {
	c := New()
	_ = c.RegisterNode(Node{Name: "a"})
	_ = c.RegisterNode(Node{Name: "b", DrainAfter: []string{"a"}})
	p, err := c.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := c.RegisterNode(Node{Name: "c"}); !errors.Is(err, ErrPlanFrozen) {
		t.Errorf("RegisterNode after Plan err = %v, want ErrPlanFrozen", err)
	}

	// Mutate the returned copies; a fresh call must be unaffected.
	nodes := p.Nodes()
	for i := range nodes {
		nodes[i].Name = "MUTATED"
		for j := range nodes[i].DrainAfter {
			nodes[i].DrainAfter[j] = "MUTATED"
		}
	}
	if order := p.StartOrder(); len(order) > 0 {
		order[0] = "MUTATED"
	}
	for _, n := range p.Nodes() {
		if n.Name == "MUTATED" {
			t.Error("Nodes() is not a defensive copy — a caller mutated the plan's node names")
		}
		for _, d := range n.DrainAfter {
			if d == "MUTATED" {
				t.Error("Nodes() did not deep-copy the DrainAfter slice")
			}
		}
	}
	for _, o := range p.StartOrder() {
		if o == "MUTATED" {
			t.Error("StartOrder() is not a defensive copy")
		}
	}
}
