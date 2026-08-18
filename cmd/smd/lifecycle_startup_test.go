package main

// ADR 0070 phase 3a — characterization of the ORCHESTRATED daemon startup (operator-required):
//
//   C1  Both enrichment consumers (ft8's completed-QSO logger and http) receive the SAME instance —
//       enrichment is constructed once, never twice.
//   C2  A config-disabled FT8 does not prevent HTTP (or enrichment) from reaching serving state.
//   C3  An infra (enrichment) Initialize failure prevents the dependent fleet Starts and is
//       attributed to the infra node.
//   C4  No consumer is hand-started or initialized through a hidden closure — the orchestrator, and
//       only the orchestrator, constructs them.
//
// These drive the REAL graph over a temp working dir + file databases, with bridge and ft8 disabled
// so no serial/audio hardware is touched (ft8 stays pruned; bridge is inactive).

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newOrchestratedDaemon builds a real daemon over a temp working dir + file databases — the exact
// shape run() assembles (Wire → resolve beans → construct the pre-orchestrator fleet → register the
// lifecycle graph) — and returns it ready for orch.Start. Bridge and ft8 are disabled, so orch.Start
// touches no hardware.
func newOrchestratedDaemon(t *testing.T, mut func(*config.Config)) (*daemon, *orchestrator.Orchestrator) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("SM_WORKING_DIR", tmp)

	cfg := config.DefaultConfig(tmp)
	cfg.Datastore.Path = filepath.Join(tmp, "log.db")
	cfg.SocketPath = filepath.Join(tmp, "smd.sock")
	cfg.UserAgent = "station-manager-test/1.0"
	if mut != nil {
		mut(&cfg)
	}

	cfgSvc := config.New(cfg)
	cfgPath := filepath.Join(tmp, "config.json")
	if err := config.WriteJSON(cfgPath, cfg); err != nil {
		t.Fatalf("seed config.json: %v", err)
	}
	cfgSvc.SetPath(cfgPath)

	container := iocdi.New()
	hub := events.NewHub()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	must(container.RegisterInstance(types.ConfigServiceName, cfgSvc))
	must(container.RegisterInstance(events.ServiceName, hub))
	must(container.Register(types.LoggingServiceName, reflect.TypeFor[*logging.Service]()))
	must(container.Register(types.SqliteServiceName, reflect.TypeFor[*sqlite.Service]()))
	must(container.Register(types.ReferenceDBServiceName, reflect.TypeFor[*sqlite.Service]()))
	must(container.Register(qsoservice.ServiceName, reflect.TypeFor[*qsoservice.Service]()))
	iocdi.SetLiteralProvider(func(id string, tt reflect.Type) (any, bool, error) {
		if id == "workingdir" && tt.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})

	if err := container.Wire(); err != nil {
		t.Fatalf("wire: %v", err)
	}
	logger, err := iocdi.ResolveAs[*logging.Service](container, types.LoggingServiceName)
	if err != nil {
		t.Fatalf("resolve logging: %v", err)
	}
	hub.SetLogger(logger)
	db, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		t.Fatalf("resolve sqlite: %v", err)
	}
	refDB, err := iocdi.ResolveAs[*sqlite.Service](container, types.ReferenceDBServiceName)
	if err != nil {
		t.Fatalf("resolve referencedb: %v", err)
	}
	qso, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		t.Fatalf("resolve qso: %v", err)
	}

	workerCtx, workerCancel := context.WithCancel(context.Background())
	d := &daemon{
		cfg:          cfg,
		cfgSvc:       cfgSvc,
		cfgPath:      cfgPath,
		logger:       logger,
		hub:          hub,
		db:           db,
		refDB:        refDB,
		qso:          qso,
		workerCtx:    workerCtx,
		workerCancel: workerCancel,
		errCh:        make(chan error, 1),
		restartCh:    make(chan struct{}),
	}
	d.bridge = bridge.New(cfg.ActiveBridge(), logger)
	d.ft8 = ft8.NewService(cfg.ActiveFt8(), logger, cfgSvc.WorkingDir())

	orch, err := d.registerLifecycle(container)
	if err != nil {
		t.Fatalf("registerLifecycle: %v", err)
	}
	t.Cleanup(func() {
		workerCancel()
		orch.Shutdown(2*time.Second, nil) // clean teardown when started; a no-op before a successful Start
		_ = db.Close()
		_ = refDB.Close()
		_ = logger.Close()
	})
	return d, orch
}

// C1: enrichment is built once and the HTTP server composes THAT instance (not a second one).
func TestLifecycle_EnrichmentIsSingleInstanceSharedByHttp(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.Lookup.Hamnut.Enabled = true // a working provider ⇒ a real enrichment runtime is built
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	if d.enrich == nil {
		t.Fatal("enrichment runtime was not built though a provider is enabled")
	}
	if got := orch.Milestone(nodeEnrichment); got != orchestrator.MilestoneRunning {
		t.Fatalf("enrichment milestone = %v, want Running", got)
	}
	if d.server == nil {
		t.Fatal("http server was not constructed")
	}
	// If http (or any consumer) built its own enrichment, this pointer would differ — the "construct
	// enrichment twice" hazard. ft8's completed-QSO logger reads the same d.enrich holder.
	if d.server.Enrichment() != d.enrich {
		t.Errorf("http composes a DIFFERENT enrichment instance (%p) than the graph built (%p); "+
			"both consumers must share the one instance", d.server.Enrichment(), d.enrich)
	}
}

// C2: a disabled FT8 must not block HTTP or enrichment from reaching serving state.
func TestLifecycle_DisabledFt8StillReachesServing(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, nil) // ft8 + bridge disabled by default; no providers
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	if got := orch.Result(nodeFt8); got != orchestrator.Inactive {
		t.Errorf("ft8 result = %v, want Inactive (disabled ⇒ pruned)", got)
	}
	if got := orch.Milestone(nodeEnrichment); got != orchestrator.MilestoneRunning {
		t.Errorf("enrichment milestone = %v, want Running — it must activate independent of ft8", got)
	}
	if got := orch.Milestone(nodeHTTP); got != orchestrator.MilestoneRunning {
		t.Errorf("http milestone = %v, want Running — HTTP must reach serving with ft8 disabled", got)
	}
}

// C3: an enrichment Initialize failure prevents the dependent fleet Starts and is attributed to the
// enrichment node.
func TestLifecycle_InfraInitFailurePreventsDependentsAndIsAttributed(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.Lookup.Hamnut.Enabled = true
		c.Lookup.Hamnut.Name = "no-such-provider" // buildEnrichment fails: no constructor registered
	})
	err := orch.Start(d.workerCtx)
	if err == nil {
		t.Fatal("orchestrated start succeeded though enrichment init must fail")
	}
	if !strings.Contains(err.Error(), nodeEnrichment) {
		t.Errorf("start error does not attribute the failure to the %q node: %v", nodeEnrichment, err)
	}
	// The dependent fleet nodes are ordered AFTER enrichment, so they must never have started.
	if got := orch.Milestone(nodeFt8); got != orchestrator.MilestoneNone {
		t.Errorf("ft8 milestone = %v, want None — a dependent must not start after infra init fails", got)
	}
	if got := orch.Milestone(nodeHTTP); got != orchestrator.MilestoneNone {
		t.Errorf("http milestone = %v, want None — a dependent must not start after infra init fails", got)
	}
}

// C5 (codex 3a850604 P2): when a node's OWN Start fails after Initialize (its milestone is stuck at
// Initialized, not Running), the orchestrator drives its Rollback — so a resource opened in
// Initialize or partway through Start is released, not leaked until process exit. A dirty
// migration-tracking row makes the log DB Open() cleanly then fail Migrate(); the connection must be
// closed by the log-DB node's Rollback.
func TestLifecycle_FailedStartRollsBackAndReleasesResources(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, nil)

	// Poison the log DB: a valid SQLite file whose golang-migrate tracking table is marked dirty, so
	// Open() succeeds (isOpen=true) but Migrate() returns ErrDirty — a Start failure AFTER the
	// connection opened.
	raw, err := sql.Open("sqlite", d.cfg.Datastore.Path)
	if err != nil {
		t.Fatalf("open poison db: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations_log (version uint64, dirty bool)`); err != nil {
		t.Fatalf("create tracking table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO schema_migrations_log (version, dirty) VALUES (1, 1)`); err != nil {
		t.Fatalf("seed dirty row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close poison db: %v", err)
	}

	if err := orch.Start(d.workerCtx); err == nil {
		t.Fatal("orchestrated start succeeded though the log DB migration must fail (dirty)")
	} else if !strings.Contains(err.Error(), nodeLogDB) {
		t.Errorf("start error does not attribute the failure to the %q node: %v", nodeLogDB, err)
	}
	if got := orch.Milestone(nodeLogDB); got != orchestrator.MilestoneNone {
		t.Errorf("log-db milestone = %v, want None (rolled back)", got)
	}
	// The connection Open() acquired must be released by the node's Rollback — Ping fails when closed.
	if err := d.db.Ping(); err == nil {
		t.Error("log DB connection is still open after a failed Start; Rollback did not release it")
	}
}

// C6 (codex 1fbbd41d P2): the workers node's Rollback must drain the forwarder workers WITHOUT
// cancelling the orchestrator's rollback context — otherwise runBounded sees rollback as timed out
// and halts the unwind, leaving the predecessor DBs (and logger) open. A bad forwarder type fails
// startWorkers after the DBs opened; rollback must still reach and close them.
func TestLifecycle_WorkerStartFailureStillRollsBackPredecessors(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		c.Forwarders = []types.ForwarderConfig{
			{Name: "bad", Type: "definitely-not-a-registered-forwarder", Enabled: true},
		}
	})
	if err := orch.Start(d.workerCtx); err == nil {
		t.Fatal("orchestrated start succeeded though the forwarder build must fail")
	} else if !strings.Contains(err.Error(), nodeWorkers) {
		t.Errorf("start error does not attribute the failure to the %q node: %v", nodeWorkers, err)
	}
	// The DB nodes are unwound after the workers node; their Stop must still run.
	if err := d.db.Ping(); err == nil {
		t.Error("log DB is still open after a worker-start failure; the worker Rollback halted the unwind")
	}
	if err := d.refDB.Ping(); err == nil {
		t.Error("reference DB is still open after a worker-start failure; the worker Rollback halted the unwind")
	}
}

// C4: the promoted-infra consumers exist only after the orchestrator ran — nothing hand-constructs
// or hand-starts them through a hidden closure before orch.Start.
func TestLifecycle_ConsumersInitializedOnlyByTheOrchestrator(t *testing.T) {
	d, orch := newOrchestratedDaemon(t, nil)
	if d.mailer != nil || d.server != nil || d.enrich != nil {
		t.Fatalf("a consumer was built before orchestrator.Start (hidden closure): mailer=%v server=%v enrich=%v",
			d.mailer != nil, d.server != nil, d.enrich != nil)
	}
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	if d.mailer == nil {
		t.Error("mailer was not constructed by the orchestrator")
	}
	if d.server == nil {
		t.Error("http server was not constructed by the orchestrator")
	}
}
