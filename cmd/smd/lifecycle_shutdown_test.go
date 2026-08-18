package main

// ADR 0070 phase 3b — the ORCHESTRATED shutdown of the REAL daemon graph derives the §5 safety
// partial order, and its logging node drains last / is skipped-when-unsafe. These register the actual
// lifecycleNodes() with fake recording adapters (no services, no hardware) and drive orch.Shutdown.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

type stopRec struct {
	mu    sync.Mutex
	order []string
}

func (r *stopRec) add(n string) { r.mu.Lock(); r.order = append(r.order, n); r.mu.Unlock() }
func (r *stopRec) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// realGraphOrch registers the REAL daemon lifecycle graph (lifecycleNodes) with fake recording
// adapters and Starts it. faults[node] (when present) is that node's Stop body, run after recording.
func realGraphOrch(t *testing.T, faults map[string]func(context.Context) error) (*stopRec, *orchestrator.Orchestrator) {
	t.Helper()
	c := iocdi.New()
	if err := registerLifecycleNodes(c); err != nil {
		t.Fatalf("registerLifecycleNodes: %v", err)
	}
	o := orchestrator.New(c)
	rec := &stopRec{}
	for _, n := range lifecycleNodes() {
		name := n.Name
		if err := o.Register(orchestrator.Adapter{
			NodeID: name,
			Active: func() bool { return true },
			Start:  func(context.Context) error { return nil },
			Stop: func(ctx context.Context) error {
				rec.add(name)
				if faults != nil {
					if f := faults[name]; f != nil {
						return f(ctx)
					}
				}
				return nil
			},
		}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}
	if err := o.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return rec, o
}

func outcomeOf(rep orchestrator.ShutdownReport, node string) orchestrator.NodeOutcome {
	for _, oc := range rep.Outcomes {
		if oc.Node == node {
			return oc
		}
	}
	return orchestrator.NodeOutcome{}
}

func indexIn(order []string, node string) int {
	for i, n := range order {
		if n == node {
			return i
		}
	}
	return -1
}

// Happy path: bridge is the first teardown (RF fence), evidence/qso-log drain after ft8, hub after
// its publishers, and logging drains LAST (the logger records the whole teardown) — all Drained.
func TestLifecycleShutdown_RealGraphHappyPartialOrder(t *testing.T) {
	rec, o := realGraphOrch(t, nil)
	rep := o.Shutdown(2*time.Second, nil)
	order := rec.snapshot()

	for _, n := range []string{
		nodeBridge, nodeFt8, nodeEvidence, nodeQsoLog, nodeHub, nodeHTTP,
		nodeWorkers, nodePsk, nodeEnrichment, nodeMailer, nodeLogDB, nodeRefDB, nodeLogging,
	} {
		if oc := outcomeOf(rep, n); oc.Result != orchestrator.Drained {
			t.Errorf("%s outcome = %+v, want Drained", n, oc)
		}
	}
	if len(order) == 0 || order[0] != nodeBridge {
		t.Errorf("stop order = %v, want bridge (RF fence) first", order)
	}
	before := func(a, b string) {
		t.Helper()
		if ia, ib := indexIn(order, a), indexIn(order, b); ia < 0 || ib < 0 || ia > ib {
			t.Errorf("stop order %v: want %s before %s", order, a, b)
		}
	}
	before(nodeFt8, nodeEvidence)
	before(nodeFt8, nodeQsoLog)
	before(nodeFt8, nodePsk) // psk flushes after ft8's decode loop stops
	before(nodeHTTP, nodeHub)
	before(nodeWorkers, nodeHub)
	before(nodeQsoLog, nodeHub)
	// The databases outlive every shutdown-time writer.
	for _, w := range []string{nodeFt8, nodeQsoLog, nodeHTTP, nodeWorkers, nodeHub, nodeEnrichment} {
		before(w, nodeLogDB)
		before(w, nodeRefDB)
	}
	if order[len(order)-1] != nodeLogging {
		t.Errorf("stop order = %v, want logging LAST (logger outlives every other Stop)", order)
	}
}

// A producer (ft8) that fails to drain ⇒ its consumers are Skipped[ft8], the skip propagates to the
// hub, and — the phase-3b guarantee — logging is Skipped too, so its Stop never runs: the logger is
// left open for process reclamation rather than closed beneath a possibly-live user.
func TestLifecycleShutdown_RealGraphFt8FailSkipsDependentsAndLogging(t *testing.T) {
	faults := map[string]func(context.Context) error{
		nodeFt8: func(context.Context) error { return errors.New("ft8 stop boom") },
	}
	rec, o := realGraphOrch(t, faults)
	rep := o.Shutdown(2*time.Second, nil)

	if oc := outcomeOf(rep, nodeFt8); oc.Result != orchestrator.Failed {
		t.Errorf("ft8 outcome = %+v, want Failed", oc)
	}
	for _, n := range []string{nodeEvidence, nodeQsoLog} {
		if oc := outcomeOf(rep, n); oc.Result != orchestrator.Skipped || indexIn(oc.BlockedBy, nodeFt8) < 0 {
			t.Errorf("%s outcome = %+v, want Skipped BlockedBy [ft8]", n, oc)
		}
	}
	if oc := outcomeOf(rep, nodeHub); oc.Result != orchestrator.Skipped {
		t.Errorf("hub outcome = %+v, want Skipped (transitive)", oc)
	}
	if oc := outcomeOf(rep, nodePsk); oc.Result != orchestrator.Skipped {
		t.Errorf("psk outcome = %+v, want Skipped (ft8 did not drain)", oc)
	}
	// The databases are Skipped and left OPEN — a consumer (qso-log) did not drain, so closing them
	// under a possibly-live writer is exactly the hazard the drain-skip avoids.
	for _, db := range []string{nodeLogDB, nodeRefDB} {
		if oc := outcomeOf(rep, db); oc.Result != orchestrator.Skipped {
			t.Errorf("%s outcome = %+v, want Skipped (left open — a writer did not drain)", db, oc)
		}
		if indexIn(rec.snapshot(), db) >= 0 {
			t.Errorf("%s Stop ran though it was Skipped; the DB must be left open under a live writer", db)
		}
	}
	if oc := outcomeOf(rep, nodeLogging); oc.Result != orchestrator.Skipped {
		t.Errorf("logging outcome = %+v, want Skipped (a prerequisite did not drain)", oc)
	}
	if indexIn(rec.snapshot(), nodeLogging) >= 0 {
		t.Error("logging Stop ran though logging was Skipped; the logger must be left open")
	}
}

// The observer emits ONLY the exceptional records — a Failed node's Stop error, exactly ONE
// first-timeout budget warning, each Skipped node naming its prerequisite — preserving today's
// gracefulShutdown records. A Drained node logs nothing, and the logging node is skipped by the
// observer (its own outcome is reported by reportLoggingOutcome).
func TestShutdownObserver_EmitsExceptionalRecordsOnly(t *testing.T) {
	buf := &syncBuffer{}
	d := &daemon{logger: logging.NewForWriter(buf)}
	d.cfg.Server.ShutdownTimeoutSec = 5
	obs := d.shutdownObserver()

	obs(orchestrator.NodeOutcome{Node: "ft8", Result: orchestrator.Failed, Err: errors.New("stop-boom")})
	obs(orchestrator.NodeOutcome{Node: "evidence", Result: orchestrator.Skipped, BlockedBy: []string{"ft8"}})
	obs(orchestrator.NodeOutcome{Node: "http", Result: orchestrator.TimedOut})
	obs(orchestrator.NodeOutcome{Node: "hub", Result: orchestrator.TimedOut})     // second timeout: NOT re-logged
	obs(orchestrator.NodeOutcome{Node: "psk", Result: orchestrator.Drained})      // Drained: no record
	obs(orchestrator.NodeOutcome{Node: nodeLogging, Result: orchestrator.Failed}) // logging: observer skips it

	s := buf.String()
	if !strings.Contains(s, "stop-boom") || !strings.Contains(s, `"stage":"ft8"`) {
		t.Errorf("missing ft8 Stop-error record: %s", s)
	}
	if !strings.Contains(s, msgShutdownSkippedRecord) || !strings.Contains(s, `"prerequisite":"ft8"`) {
		t.Errorf("missing evidence skip record: %s", s)
	}
	if n := strings.Count(s, msgShutdownExceededRecord); n != 1 {
		t.Errorf("exceeded-budget record count = %d, want 1 (first timeout only): %s", n, s)
	}
	if !strings.Contains(s, `"stage":"http"`) {
		t.Errorf("first-timeout record must name http: %s", s)
	}
	if strings.Contains(s, `"stage":"psk"`) {
		t.Errorf("a Drained node must emit no record: %s", s)
	}
	if strings.Contains(s, `"stage":"loggingservice"`) {
		t.Errorf("observer must skip the logging node (reportLoggingOutcome handles it): %s", s)
	}
}

// End-to-end clean shutdown of the REAL daemon (bridge + ft8 disabled): every active node drains, the
// databases are closed, and the logging node's Stop writes the final "smd stopped" line before Close.
func TestLifecycle_CleanShutdownDrainsAllAndClosesLogger(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, nil)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	d.cleanShutdown = true
	report := orch.Shutdown(5*time.Second, d.shutdownObserver())
	d.reportLoggingOutcome(report)

	for _, oc := range report.Outcomes {
		if oc.Result != orchestrator.Drained {
			t.Errorf("%s outcome = %v, want Drained on a clean shutdown", oc.Node, oc.Result)
		}
	}
	if err := d.db.Ping(); err == nil {
		t.Error("log DB still open after the orchestrated shutdown")
	}
	if err := d.refDB.Ping(); err == nil {
		t.Error("reference DB still open after the orchestrated shutdown")
	}
	logData, err := os.ReadFile(filepath.Join(d.cfgSvc.WorkingDir(), "log", "smd.log"))
	if err != nil {
		t.Fatalf("read smd.log: %v", err)
	}
	if !strings.Contains(string(logData), "smd stopped") {
		t.Errorf("smd.log missing the clean-run 'smd stopped' line")
	}
}

// captureStderr redirects os.Stderr around fn and returns what was written. Not parallel-safe.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return string(out)
}

// reportLoggingOutcome handles the logging node's OWN outcome after Shutdown: a Skipped logging (an
// earlier node did not drain ⇒ logger left open) is noted through the still-open logger; a Failed or
// TimedOut logging close goes to stderr, because the logger is gone. A Drained logging is silent.
func TestReportLoggingOutcome_StderrOnFailure_LoggerNoteOnSkip(t *testing.T) {
	// Skipped → note through the open logger.
	buf := &syncBuffer{}
	d := &daemon{logger: logging.NewForWriter(buf)}
	d.reportLoggingOutcome(orchestrator.ShutdownReport{Outcomes: []orchestrator.NodeOutcome{
		{Node: nodeLogging, Result: orchestrator.Skipped, BlockedBy: []string{"ft8"}},
	}})
	if !strings.Contains(buf.String(), "left open") {
		t.Errorf("Skipped logging must note the left-open logger via the logger: %s", buf.String())
	}

	// Failed → stderr (the logger is closed/broken).
	stderr := captureStderr(t, func() {
		d2 := &daemon{logger: logging.NewForWriter(&syncBuffer{})}
		d2.reportLoggingOutcome(orchestrator.ShutdownReport{Outcomes: []orchestrator.NodeOutcome{
			{Node: nodeLogging, Result: orchestrator.Failed, Err: errors.New("close-boom")},
		}})
	})
	if !strings.Contains(stderr, "close-boom") {
		t.Errorf("Failed logging close must be reported to stderr: %q", stderr)
	}

	// Drained → silent (nothing to stderr, nothing to the logger).
	buf3 := &syncBuffer{}
	d3 := &daemon{logger: logging.NewForWriter(buf3)}
	stderr3 := captureStderr(t, func() {
		d3.reportLoggingOutcome(orchestrator.ShutdownReport{Outcomes: []orchestrator.NodeOutcome{
			{Node: nodeLogging, Result: orchestrator.Drained},
		}})
	})
	if stderr3 != "" || buf3.String() != "" {
		t.Errorf("Drained logging must be silent: stderr=%q log=%q", stderr3, buf3.String())
	}
}
