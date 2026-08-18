package main

// ADR 0070 phase 3 — the daemon's declarative lifecycle graph.
//
// This file holds the SHAPE of the graph (the nodes, their start/drain edges, and the RF fence);
// the adapters that bind each node to real service transitions live alongside it. Registering the
// graph + driving it through internal/lifecycle/orchestrator replaces run()'s hand-wired
// Initialize/Start sequence and (phase 3b) cmd/smd/shutdown.go's hand-ordered gracefulShutdown.
//
// Node identity: bean nodes reuse their bean ID (case-insensitive), so the plan derives their start
// edges from the di.inject graph automatically (mergedStartPrereqLocked). Non-bean nodes — the
// service fleet plus the promoted enrichment/mailer runtimes — declare their start prerequisites
// explicitly via StartAfter. DrainAfter carries ONLY real producer/drain-safety edges, never a
// mechanical mirror of the start edges (ADR 0070: shutdown edges reflect send-on-closed-channel /
// live-producer hazards, not construction order).

import (
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Node IDs. Bean nodes MUST match their registered bean ID so the plan merges the di.inject-derived
// start edges; the rest are the non-bean fleet + promoted infra.
const (
	// Bean nodes (ids == bean ids).
	nodeConfig  = types.ConfigServiceName      // "configservice"
	nodeHub     = events.ServiceName           // "eventhub" — the daemon events hub
	nodeLogging = types.LoggingServiceName     // "loggingservice"
	nodeLogDB   = types.SqliteServiceName      // "sqliteservice" — the log database
	nodeRefDB   = types.ReferenceDBServiceName // "referencedb" — the enrichment-cache database
	nodeQso     = qsoservice.ServiceName       // "qsoservice"

	// Non-bean fleet + promoted infra nodes.
	nodeBridge     = "bridge"     // CAT/RF bridge — the RF-critical fence
	nodeEnrichment = "enrichment" // lookup.Orchestrator runtime (promoted: ft8 + http depend on it)
	nodeMailer     = "mailer"     // email.Service (promoted: http depends on it)
	nodeEvidence   = "evidence"   // FT8 evidence writer
	nodePsk        = "psk"        // PSK Reporter uploader
	nodeFt8        = "ft8"        // FT8 decode subsystem (sole producer for evidence/qso-log)
	nodeWorkers    = "workers"    // forwarder workers + the smcloud reconciler that rides them
	nodeQsoLog     = "qso-log"    // FT8 completed-QSO log goroutines (launched by ft8's decode loop)
	nodeHTTP       = "http"       // the HTTP API server (the front door)
)

// lifecycleNodes declares the daemon graph. Registration order is the deterministic shutdown
// tiebreak among otherwise-unconstrained nodes (ADR 0070) and is kept roughly start-dependency
// ordered; the topological StartOrder is what actually sequences startup.
//
// The logging node's DrainAfter is set to EVERY other node below (not hard-coded — derived, so it
// cannot drift as nodes change): the logger records the shutdown itself, so it must close only after
// nothing else can still log through it. If any node ends non-drained, logging is Skipped and the
// logger is left open for process reclamation rather than closed beneath a possibly-live user.
func lifecycleNodes() []iocdi.Node {
	nodes := []iocdi.Node{
		// Bean spine. Start edges come from di.inject (config → logging → {log-db, ref-db} → qso);
		// ref-db additionally waits on log-db so BootstrapReferenceSplit re-keys both before either
		// connection opens. The hub is a publisher sink: it closes only after its publishers drain.
		{Name: nodeConfig},
		{Name: nodeHub, DrainAfter: []string{nodeHTTP, nodeWorkers, nodeQsoLog}},
		{Name: nodeLogging},
		{Name: nodeLogDB},
		{Name: nodeRefDB, StartAfter: []string{nodeLogDB}},
		{Name: nodeQso},

		// The RF fence. Needs the logger + config; keys TX exclusively at shutdown.
		{Name: nodeBridge, StartAfter: []string{nodeLogging, nodeConfig}, StopPriority: iocdi.RFCritical},

		// Promoted infra. Enrichment is the shared lookup runtime consumed by ft8's completed-QSO
		// logger AND http; mailer is consumed by http. Both activate independently of ft8.
		{Name: nodeEnrichment, StartAfter: []string{nodeRefDB, nodeLogDB, nodeLogging}},
		{Name: nodeMailer, StartAfter: []string{nodeLogging, nodeConfig}},

		// Fleet. evidence is ft8's sink (started + constructed before ft8 wires it); its archive path
		// derives from the log-DB's resolved path, so it also waits on log-db. psk is independent.
		{Name: nodeEvidence, StartAfter: []string{nodeLogging, nodeConfig, nodeLogDB}, DrainAfter: []string{nodeFt8}},
		{Name: nodePsk, StartAfter: []string{nodeLogging}},
		{Name: nodeFt8, StartAfter: []string{nodeBridge, nodeEnrichment, nodeEvidence, nodePsk}},

		// Forwarder workers (need db + qso + hub). qso-log rides ft8's decode loop; it drains after ft8.
		{Name: nodeWorkers, StartAfter: []string{nodeLogDB, nodeQso, nodeHub, nodeLogging}},
		{Name: nodeQsoLog, StartAfter: []string{nodeFt8}, DrainAfter: []string{nodeFt8}},

		// The front door — composes every service, so it starts last.
		{Name: nodeHTTP, StartAfter: []string{
			nodeEnrichment, nodeMailer, nodeBridge, nodeFt8, nodeEvidence,
			nodeQso, nodeLogDB, nodeHub, nodeLogging,
		}},
	}

	// logging drains strictly after every other node (see the doc comment above).
	var others []string
	for _, n := range nodes {
		if n.Name != nodeLogging {
			others = append(others, n.Name)
		}
	}
	for i := range nodes {
		if nodes[i].Name == nodeLogging {
			nodes[i].DrainAfter = others
		}
	}
	return nodes
}

// registerLifecycleNodes registers every lifecycle node on the container, preserving declaration
// order (the shutdown tiebreak). Called after the beans are registered so the plan can merge the
// di.inject-derived start edges.
func registerLifecycleNodes(c *iocdi.Container) error {
	for _, n := range lifecycleNodes() {
		if err := c.RegisterNode(n); err != nil {
			return err
		}
	}
	return nil
}
