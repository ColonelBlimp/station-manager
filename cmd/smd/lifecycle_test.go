package main

// ADR 0070 phase 3a — the daemon's lifecycle graph (bean nodes + the declared service fleet,
// with enrichment and mailer promoted to first-class nodes) must produce a VALID immutable plan
// whose derived start order honours every real dependency, with bridge as the sole RF fence.
//
// Acceptance criteria (operator-observable; stated before the mechanism):
//
//   AC-G1  The plan BUILDS: every node reference resolves, the start graph and the (independent)
//          drain graph are both acyclic. A missing edge or a stray reference fails Plan().
//   AC-G2  Bridge is the ONE RF-critical fence (Plan rejects a second).
//   AC-G3  The derived start order places each consumer AFTER the infra/services it needs at
//          Initialize/Start: the bean spine (config → logging → {log-db, ref-db} → qso), log-db
//          before ref-db (bootstrap re-keys both before either opens), enrichment + mailer + bridge
//          + evidence before their fleet consumers, and http after ALL of enrichment, mailer,
//          bridge, ft8, evidence.
//   AC-G4  The drain graph carries only real producer/drain-safety edges (hub after its publishers,
//          evidence + qso-log after ft8) — NOT a mechanical mirror of the start edges.
//
// Reversion proof: dropping bridge's StopPriority=RFCritical makes AC-G2 find no fence; removing
// enrichment from ft8's/http's StartAfter makes AC-G3 place a consumer before its infra. Both flip
// the corresponding assertion.

import (
	"reflect"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// registerDaemonBeans mirrors run()'s bean registration so the plan's DI-derived start edges (a
// bean node's di.inject deps that are also nodes) are present exactly as in production.
func registerDaemonBeans(t *testing.T, c *iocdi.Container) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("register bean: %v", err)
		}
	}
	must(c.RegisterInstance(types.ConfigServiceName, config.New(config.Config{})))
	must(c.RegisterInstance(events.ServiceName, events.NewHub()))
	must(c.Register(types.LoggingServiceName, reflect.TypeFor[*logging.Service]()))
	must(c.Register(types.SqliteServiceName, reflect.TypeFor[*sqlite.Service]()))
	must(c.Register(types.ReferenceDBServiceName, reflect.TypeFor[*sqlite.Service]()))
	must(c.Register(qsoservice.ServiceName, reflect.TypeFor[*qsoservice.Service]()))
}

// planFor registers the beans + the lifecycle nodes and builds the immutable plan.
func planFor(t *testing.T) *iocdi.Plan {
	t.Helper()
	c := iocdi.New()
	registerDaemonBeans(t, c)
	if err := registerLifecycleNodes(c); err != nil {
		t.Fatalf("registerLifecycleNodes: %v", err)
	}
	p, err := c.Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err) // AC-G1
	}
	return p
}

// orderIndex maps each node id to its position in the derived start order.
func orderIndex(t *testing.T, p *iocdi.Plan) map[string]int {
	t.Helper()
	idx := make(map[string]int)
	for i, name := range p.StartOrder() {
		idx[name] = i
	}
	return idx
}

// AC-G1: the plan builds and covers exactly the declared node set.
func TestLifecycleGraph_PlanBuilds(t *testing.T) {
	p := planFor(t)
	got := len(p.Nodes())
	if want := len(lifecycleNodes()); got != want {
		t.Fatalf("plan has %d nodes, want %d", got, want)
	}
}

// AC-G2: bridge is the sole RF fence; a second RFCritical is rejected by Plan.
func TestLifecycleGraph_BridgeIsSoleRFFence(t *testing.T) {
	fences := 0
	for _, n := range lifecycleNodes() {
		if n.StopPriority == iocdi.RFCritical {
			fences++
			if n.Name != nodeBridge {
				t.Errorf("RFCritical fence is %q, want %q", n.Name, nodeBridge)
			}
		}
	}
	if fences != 1 {
		t.Fatalf("want exactly 1 RFCritical fence, got %d", fences)
	}

	// A second fence must be rejected at Plan() (independent guard).
	c := iocdi.New()
	registerDaemonBeans(t, c)
	if err := registerLifecycleNodes(c); err != nil {
		t.Fatalf("registerLifecycleNodes: %v", err)
	}
	if err := c.RegisterNode(iocdi.Node{Name: "second-fence", StopPriority: iocdi.RFCritical}); err != nil {
		t.Fatalf("register second fence node: %v", err)
	}
	if _, err := c.Plan(); err == nil {
		t.Error("Plan() accepted a second RFCritical fence; want rejection")
	}
}

// AC-G3: the derived start order honours every real start dependency.
func TestLifecycleGraph_StartOrderHonoursDependencies(t *testing.T) {
	p := planFor(t)
	idx := orderIndex(t, p)

	before := func(a, b string) {
		t.Helper()
		ia, oka := idx[a]
		ib, okb := idx[b]
		if !oka || !okb {
			t.Fatalf("missing node in start order: %q=%v %q=%v", a, oka, b, okb)
		}
		if ia >= ib {
			t.Errorf("start order: %q (%d) must come before %q (%d)", a, ia, b, ib)
		}
	}

	// Bean spine (DI-derived).
	before(nodeConfig, nodeLogging)
	before(nodeLogging, nodeLogDB)
	before(nodeLogging, nodeRefDB)
	before(nodeLogDB, nodeRefDB) // bootstrap re-keys both before either opens
	before(nodeLogDB, nodeQso)
	before(nodeRefDB, nodeQso)

	// Promoted infra before its fleet consumers.
	before(nodeEnrichment, nodeFt8)
	before(nodeEnrichment, nodeHTTP)
	before(nodeMailer, nodeHTTP)
	before(nodeBridge, nodeFt8)
	before(nodeEvidence, nodeFt8)

	// http is the front door — after every service it composes.
	for _, dep := range []string{nodeEnrichment, nodeMailer, nodeBridge, nodeFt8, nodeEvidence, nodeQso} {
		before(dep, nodeHTTP)
	}
}

// AC-G3 (edge-enforced): registering the nodes in REVERSE order removes the registration-order
// tiebreak as a source of correct ordering, so ONLY the declared StartAfter/DI edges can keep each
// consumer after its infra. This is what makes the ordering assertions bite when an edge is dropped
// (rather than passing by the coincidence that infra happens to be registered first).
func TestLifecycleGraph_StartOrderIsEdgeEnforced(t *testing.T) {
	c := iocdi.New()
	registerDaemonBeans(t, c)
	nodes := lifecycleNodes()
	for i := len(nodes) - 1; i >= 0; i-- { // reverse registration order
		if err := c.RegisterNode(nodes[i]); err != nil {
			t.Fatalf("register node %q: %v", nodes[i].Name, err)
		}
	}
	p, err := c.Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	idx := orderIndex(t, p)
	before := func(a, b string) {
		t.Helper()
		if idx[a] >= idx[b] {
			t.Errorf("edge not enforced under reverse registration: %q (%d) must precede %q (%d)", a, idx[a], b, idx[b])
		}
	}
	before(nodeEnrichment, nodeFt8)
	before(nodeEnrichment, nodeHTTP)
	before(nodeMailer, nodeHTTP)
	before(nodeBridge, nodeFt8)
	before(nodeEvidence, nodeFt8)
	before(nodeLogDB, nodeRefDB)
	before(nodeConfig, nodeLogging)
}

// AC-G4: the drain graph carries only real producer/drain-safety edges.
func TestLifecycleGraph_DrainEdgesAreSafetyOnly(t *testing.T) {
	drainAfter := make(map[string][]string)
	for _, n := range lifecycleNodes() {
		drainAfter[n.Name] = n.DrainAfter
	}

	has := func(node, prereq string) bool {
		for _, d := range drainAfter[node] {
			if d == prereq {
				return true
			}
		}
		return false
	}

	// Present: the real send-on-closed-channel / live-producer edges.
	if !has(nodeHub, nodeHTTP) || !has(nodeHub, nodeWorkers) || !has(nodeHub, nodeQsoLog) {
		t.Errorf("hub must DrainAfter its publishers {http, workers, qso-log}; got %v", drainAfter[nodeHub])
	}
	if !has(nodeEvidence, nodeFt8) {
		t.Errorf("evidence must DrainAfter ft8 (its sole producer); got %v", drainAfter[nodeEvidence])
	}
	if !has(nodeQsoLog, nodeFt8) {
		t.Errorf("qso-log must DrainAfter ft8 (ft8's decode loop launches it); got %v", drainAfter[nodeQsoLog])
	}
	// The databases outlive their shutdown-time writers (drained before the hub) + the enrichment
	// refresher, so they DrainAfter {hub, enrichment}.
	for _, db := range []string{nodeLogDB, nodeRefDB} {
		if !has(db, nodeHub) || !has(db, nodeEnrichment) {
			t.Errorf("%s must DrainAfter {hub, enrichment} (consumers write it during shutdown); got %v", db, drainAfter[db])
		}
	}
	// PSK drains after ft8 (ft8's decode loop feeds AddSpot until ft8 stops).
	if !has(nodePsk, nodeFt8) {
		t.Errorf("psk must DrainAfter ft8 (or last decodes drop); got %v", drainAfter[nodePsk])
	}

	// logging drains after EVERY other node — the logger-safety edge: it records the shutdown, so it
	// closes only once nothing else can log through it (a non-drained node ⇒ logging Skipped).
	others := 0
	for _, n := range lifecycleNodes() {
		if n.Name != nodeLogging {
			others++
		}
	}
	if got := len(drainAfter[nodeLogging]); got != others {
		t.Errorf("logging must DrainAfter every other node (%d edges), got %d: %v", others, got, drainAfter[nodeLogging])
	}

	// Absent: start deps must NOT be mechanically mirrored as drain edges.
	if len(drainAfter[nodeHTTP]) != 0 {
		t.Errorf("http must have NO DrainAfter (it is a publisher, not a drain prerequisite holder); got %v", drainAfter[nodeHTTP])
	}
	if len(drainAfter[nodeEnrichment]) != 0 {
		t.Errorf("enrichment must have NO DrainAfter (no producer/drain hazard); got %v", drainAfter[nodeEnrichment])
	}
	if len(drainAfter[nodeMailer]) != 0 {
		t.Errorf("mailer must have NO DrainAfter; got %v", drainAfter[nodeMailer])
	}
	if len(drainAfter[nodeFt8]) != 0 {
		t.Errorf("ft8 must have NO DrainAfter (it is the producer, not a consumer); got %v", drainAfter[nodeFt8])
	}
}
