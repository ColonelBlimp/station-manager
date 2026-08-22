package main

import (
	"context"
	"encoding/json"
	stderr "errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/clublog" // registers "clublog" forwarder + default retry via init(); main also sets clublog.UserAgent below
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrz"     // registers "qrz" forwarder + default retry via init(); main also sets qrz.UserAgent below
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrzcq"   // registers "qrzcq" forwarder + 90-second pacing via init(); main also sets qrzcq.UserAgent below
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud" // registers "smcloud" forwarder (ADR 0040 backup client) via init(); main also sets smcloud.UserAgent below
	// The test-only "stub" forwarder is registered ONLY in dev builds (-tags dev,
	// see forwarder_stub_dev.go) — never in a release binary, so a production
	// config can't select type:"stub" and get fake "uploaded" status without
	// sending anywhere (review 2026-06-19 M3). The registry rejects an
	// unregistered type as "unknown forwarder type" at startup.
	"github.com/ColonelBlimp/station-manager/internal/forwarding/worker"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	_ "github.com/ColonelBlimp/station-manager/internal/lookup/hamnut" // registers the hamnut country provider (descriptor + constructor) via init()
	_ "github.com/ColonelBlimp/station-manager/internal/lookup/qrz"    // registers the QRZ callsign provider (descriptor + constructor) via init()
	_ "github.com/ColonelBlimp/station-manager/internal/lookup/qrzcq"  // registers the QRZCQ callsign provider (descriptor + constructor) via init()
	"github.com/ColonelBlimp/station-manager/internal/lookup/refresher"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Process exit codes. Named so service managers (systemd, monit,
// supervisord) can distinguish a clean startup-error exit from an
// uncaught panic. ExitOK (0) is implicit — Go returns 0 when main
// exits normally, so there's no constant for it.
const (
	ExitError = 1
	ExitPanic = 2
	// ExitRestart is a PLANNED self-restart (POST /v1/restart): the daemon runs
	// its normal graceful shutdown, then exits with this code so systemd
	// (RestartForceExitStatus in smd.service) respawns it. Distinct from
	// ExitError so the log + service manager can tell a requested restart from a
	// fault.
	ExitRestart = 3
)

// restartRequested is set by the POST /v1/restart trigger before it starts the
// graceful shutdown, so main() can exit ExitRestart (→ systemd respawn) rather
// than the normal 0 after run() returns cleanly.
var restartRequested atomic.Bool

// referenceDBFilename is the shared enrichment-cache database (country +
// contacted_station), opened alongside the log DB in the same directory
// (reference.db / log-db split).
const referenceDBFilename = "reference.db"

// ft8QsoLogTimeout bounds the off-pipeline log+enrich of one completed FT8
// exchange (a DB write plus a best-effort country lookup). Independent of the
// FT8 decode-loop context so Stop's cancellation can't abort persisting a QSO
// that already happened on the air (review 2026-06-19 M2); generous enough for a
// slow enrich on a flaky link, bounded so a hung lookup can't outlive shutdown.
const ft8QsoLogTimeout = 30 * time.Second

func main() {
	// Top-level panic safety net. run()'s own defers (dbSvc close,
	// loggerSvc close) still fire first as the panic unwinds through
	// its frame, so db/logger get shut down cleanly before we land
	// here. We just need to make sure the panic is logged in a
	// recognisable shape and that we exit with a distinct code so a
	// process supervisor can tell a panic from a graceful error exit.
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(os.Stderr, "smd: PANIC: %v\n", r)
			_, _ = fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			os.Exit(ExitPanic)
		}
	}()

	// Subcommand dispatch. Currently only one — `smd import <file.adi>` —
	// but inserted here as a switch so future subcommands (export,
	// migrate, etc.) drop in without restructuring. Argv shape:
	//   smd                              → daemon (run())
	//   smd --config <path>              → daemon with flag (run())
	//   smd import [flags] <file.adi>    → one-shot import (runImport())
	//   smd restore [flags]              → SM Cloud restore (runRestore())
	//   smd config-check [--config p]    → read-only unknown-key preflight (runConfigCheck())
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "import":
			if err := runImport(os.Args[2:]); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "smd: %v\n", err)
				os.Exit(ExitError)
			}
			return
		case "restore":
			if err := runRestore(os.Args[2:]); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "smd: %v\n", err)
				os.Exit(ExitError)
			}
			return
		case "config-check":
			if err := runConfigCheck(os.Args[2:]); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "smd: %v\n", err)
				os.Exit(ExitError)
			}
			return
		}
	}

	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "smd: %v\n", err)
		os.Exit(ExitError)
	}
	// A clean run() return after POST /v1/restart exits with the restart code so
	// systemd respawns; a normal SIGTERM / systemctl stop leaves this false → 0.
	if restartRequested.Load() {
		os.Exit(ExitRestart)
	}
}

// run wires the daemon's lifecycle into a single function with
// defer-based cleanup. Any failure returns an error; deferred closers
// run on the way out, so the "Open → Close" contract is honored in the
// happy path AND the failure path. The alternative — ad-hoc fatal()
// calls peppered through startup — left open handles when startup
// failed midway (see review L4).
//
// Subsystem lifecycle shapes intentionally diverge — CLAUDE.md's
// canonical Initialize → Start → Stop applies where it earns its keep:
//
//   - DB (sqlite): Initialize via container.Build, manual Open after
//     working-dir resolution, defer Close. Open is split from
//     Initialize because the on-disk DSN depends on cfg, which is
//     loaded between container construction and Open.
//   - Forwarders: per-config-entry, not singleton — constructed inside
//     spawnForwarderWorkers, run under safego.GoTracked, drained via
//     ctx cancel + WaitGroup. Doesn't fit DI (config-driven N).
//   - Bridge: manual New + Initialize + Start + Stop. Hand-wired
//     because the constructor takes the loaded cfg.Bridge snapshot,
//     and Start needs workerCtx (not available at container.Build
//     time). See "Bridge subsystem" section below.
//   - Refresher + lookup providers: hand-wired inside buildEnrichment
//     because operator config gates which providers exist — see
//     buildEnrichment's own doc for the rationale.
//   - Mailer: no Initialize / Start / Stop. The Service reports
//     Enabled() from cfg.Smtp.Enabled; handlers gate via Enabled() and
//     return 503 mailer_disabled otherwise. Lifecycle would be ceremony.
//   - Hub: constructed inline (events.NewHub), no Initialize / Start,
//     manual Close after publishers drain. It's a fan-out primitive,
//     not a service.
func run() error {
	const op errors.Op = "smd.run"

	configPath := flag.String("config", "", "path to config.json (default: $SM_WORKING_DIR, else the XDG data dir for a system install, else the executable's directory)")
	flag.Parse()

	// ---- Propagate build version to ADIF emission ----
	// adif.ProgramVersion is emitted as PROGRAMVERSION in ADIF export
	// headers. Package global; process-lifetime, set once at daemon
	// boot. Tests that import the adif package must reset it if they
	// care about isolation.
	adif.ProgramVersion = buildinfo.Version

	// ---- Load configuration ----
	// defaultConfigPath calls utils.WorkingDir as part of its
	// resolution, which MkdirAll-creates the directory before any
	// write — covers the systemd-unit-points-at-uncreated-path
	// first-run case without a separate explicit call here.
	cfg, firstRunPath, err := loadConfig(*configPath)
	if err != nil {
		// Pre-logger failure: mirror into smd.log so a bad config.json isn't a
		// silent exit (stderr alone only reaches the journal). See logStartupFailure.
		logStartupFailure(err)
		return err
	}

	// ---- Resolve global User-Agent ----
	// Single value used by every outbound HTTP caller (forwarders,
	// lookup providers). The operator may override in config.json;
	// when empty (fresh first-run, or operator cleared the field) we
	// fill it from the ldflags-injected build version and persist so
	// the value lands on disk and is visible to the operator. Fail
	// loudly if the final value is empty — every callsite assumes a
	// non-empty UA and would otherwise send a blank header.
	// Normalise first so the trimmed value is what we persist and what the QRZ
	// forwarder stamps on its header — not a stray " station-manager/dev "
	// with surrounding whitespace (review 2026-06-06 L1).
	cfg.UserAgent = strings.TrimSpace(cfg.UserAgent)
	if cfg.UserAgent == "" {
		cfg.UserAgent = "station-manager/" + buildinfo.Version
	}
	if cfg.UserAgent == "" {
		err := fmt.Errorf("global UserAgent resolved to empty string; cannot start daemon (build version=%q)", buildinfo.Version)
		logStartupFailure(err)
		return err
	}

	// ---- Build DI container ----
	cfgSvc := config.New(cfg)
	// Record the on-disk path so /v1/config PUT can rewrite it
	// atomically. firstRunPath covers the just-seeded path; for an
	// existing config we re-resolve via the same precedence used in
	// loadConfig.
	cfgPath, err := resolveConfigPath(*configPath, firstRunPath)
	if err != nil {
		logStartupFailure(err)
		return err
	}
	cfgSvc.SetPath(cfgPath)
	// Persist the resolved UserAgent (no-op when the operator
	// already set it explicitly; writes the daemon default otherwise
	// so the operator sees it on disk and can override later).
	// Soft-failure to match the first-run seed pattern: a read-only
	// working dir is degraded but not fatal — the daemon still serves
	// QSOs with the in-memory UA value, the operator just doesn't see
	// it persisted in config.json. Stderr is the only available
	// channel here (structured logger isn't built yet).
	startupChanges, uerr := persistResolvedConfig(cfgSvc, cfg.UserAgent)
	if uerr != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"smd: could not persist resolved config (UserAgent / ClubLog key scrub) to config.json: %v (continuing with in-memory values)\n",
			uerr)
	}
	// Forwarder package vars — every forwarder POST stamps this on the
	// User-Agent header. Set after the UA is final.
	qrz.UserAgent = cfg.UserAgent
	qrzcq.UserAgent = cfg.UserAgent
	clublog.UserAgent = cfg.UserAgent
	smcloud.UserAgent = cfg.UserAgent

	container := iocdi.New()

	// Event hub — registered before Build so every service with a
	// `di.inject:"eventhub"` field (qsoservice, future subscribers)
	// gets the same instance. Closed at shutdown after publishers
	// have stopped.
	hub := events.NewHub()

	if err = container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register config service")
	}
	if err = container.RegisterInstance(events.ServiceName, hub); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register event hub")
	}
	if err = container.Register(logging.ServiceName, reflect.TypeFor[*logging.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register logging service")
	}
	if err = container.Register(types.SqliteServiceName, reflect.TypeFor[*sqlite.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register sqlite service")
	}
	// Second sqlite bean: the shared enrichment-cache connection (reference.db),
	// distinguished from the log bean by name + migration set (configured after
	// resolve, before Open). qsoservice + the orchestrator inject it.
	if err = container.Register(types.ReferenceDBServiceName, reflect.TypeFor[*sqlite.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register reference-db sqlite service")
	}
	if err = container.Register(qsoservice.ServiceName, reflect.TypeFor[*qsoservice.Service]()); err != nil {
		return errors.New(op).WithErr(err).WithMsg("register qso service")
	}

	// The logging service's WorkingDir string field is resolved via
	// LiteralProvider. SetLiteralProvider writes to an iocdi package
	// global (atomic.Value); intentionally process-lifetime — the
	// container shares this provider with any other iocdi consumer in
	// the process, which is fine for a single-shot daemon binary.
	iocdi.SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		if id == "workingdir" && targetType.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})

	// ---- Wire the container ----
	// ADR 0070: the orchestrator, not Build(), drives Initialize. Wire constructs + injects the beans;
	// the lifecycle graph below drives each bean's Initialize in dependency order.
	if err = container.Wire(); err != nil {
		err = errors.New(op).WithErr(err).WithMsg("wire container")
		logStartupFailure(err) // still pre-logger
		return err
	}

	// ---- Resolve services ----
	loggerSvc, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		err = errors.New(op).WithErr(err).WithMsg("resolve logging service")
		logStartupFailure(err) // the logger itself didn't come up — mirror to smd.log
		return err
	}
	// The hub was built before the container (services inject it), so this is the first point a
	// logger exists to give it. It reports slow-reader evictions.
	hub.SetLogger(loggerSvc)

	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve sqlite service")
	}
	refDbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.ReferenceDBServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve reference-db sqlite service")
	}
	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve qso service")
	}

	// ---- Worker context ----
	// The forwarder workers and the FT8/bridge/psk long-lived goroutines bind here; it is cancelled
	// at shutdown before the ordered teardown so in-flight work observes ctx.Done().
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	// ---- Assemble the daemon + its lifecycle graph (ADR 0070 phase 3) ----
	d := &daemon{
		cfg:            cfg,
		cfgSvc:         cfgSvc,
		cfgPath:        cfgPath,
		firstRunPath:   firstRunPath,
		startupChanges: startupChanges,
		logger:         loggerSvc,
		hub:            hub,
		db:             dbSvc,
		refDB:          refDbSvc,
		qso:            qsoSvc,
		workerCtx:      workerCtx,
		workerCancel:   workerCancel,
		errCh:          make(chan error, 1),
		restartCh:      make(chan struct{}),
	}
	// bridge + ft8 are constructed here (not in their node Initialize) so the HTTP server keeps their
	// handles even when they are config-disabled and pruned from the active graph.
	d.bridge = bridge.New(cfg.ActiveBridge(), loggerSvc)
	d.ft8 = ft8.NewService(cfg.ActiveFt8(), loggerSvc, cfgSvc.WorkingDir())

	orch, err := d.registerLifecycle(container)
	if err != nil {
		err = errors.New(op).WithErr(err).WithMsg("register lifecycle graph")
		logStartupFailure(err)
		return err
	}

	// ---- Start the daemon through the orchestrator ----
	// The orchestrator drives Initialize→Start across the graph in dependency order (logging opens
	// the log file, the DBs open + migrate, the fleet + promoted infra start). On ANY failure it rolls
	// back every advanced node and returns; run() must NOT fall through into the legacy teardown — the
	// rollback already owns it (operator ruling, phase 3a).
	if err = orch.Start(workerCtx); err != nil {
		logStartupFailure(err) // rollback may have closed the logger; mirror to smd.log
		return err
	}

	// Start succeeded. The orchestrator now owns the ENTIRE teardown — orchestrator.Shutdown below
	// drives every node's Stop in the derived drain order (the RF fence first; the DBs, logger,
	// refresher and hub included) — so run() installs NO deferred closers: they would double-close
	// what Shutdown closes. The deferred workerCancel above stays as a belt for the error/panic paths.

	// ---- Wait for shutdown signal ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var runErr error
	select {
	case sig := <-sigCh:
		loggerSvc.InfoWith().Str("signal", sig.String()).Msg("shutdown signal received")
	case <-d.restartCh:
		loggerSvc.InfoWith().Msg("restart requested (POST /v1/restart); graceful shutdown then exit for systemd respawn")
	case runErr = <-d.errCh:
		if runErr != nil {
			loggerSvc.ErrorWith().Err(runErr).Msg("server exited with error")
		}
	}

	// ---- Graceful shutdown (ADR 0070 phase 3b) ----
	// The orchestrator drives the budgeted, dependency-ordered drain: PrepareStop (HTTP StopAccepting +
	// worker-cancel) → the RF fence (bridge, sole) → the topological drain, with logging draining LAST
	// so the logger records the whole teardown. cleanShutdown makes the logging node emit "smd stopped"
	// immediately before it closes. The observer logs the exceptional records live while the logger is
	// open; reportLoggingOutcome handles the logging node's own outcome afterward.
	budget := time.Duration(cfg.Server.ShutdownTimeoutSec) * time.Second
	if budget <= 0 {
		budget = 10 * time.Second // match config.applyDefaults' 10s floor
	}
	d.cleanShutdown = true
	report := orch.Shutdown(budget, d.shutdownObserver())
	d.reportLoggingOutcome(report)

	return runErr
}

// resolveQsoDialMHz decides the frequency a completed FT8 contact is logged on.
//
// The PINNED value wins: internal/ft8 stamps it from the dial the session read off
// the rig at start, so it is the frequency the contact actually happened on. A live
// read is the wrong answer in the one case they differ — a contact completed just
// before a QSY would be filed on the band we moved to, and that wrong-band row is
// forwarded to QRZ and ClubLog (codex P1 on 652821db).
//
// This used to prefer the live read, because the dial the SPA sends at session start
// goes stale across a Call-CQ pile-up (logged before the IC-7300's freq poll had
// landed, producing wrong-band QSOs). The session pin fixes that at the source: a
// start is refused while the dial is unreadable, and a QSY during the session ends
// the session, so the pin is always a real reading. live() therefore only covers a
// completion carrying no pin at all (no CAT — where it is unavailable too), kept so
// an unpinned path degrades rather than logging zero.
func resolveQsoDialMHz(pinnedMHz float64, live func() (float64, bool)) float64 {
	if pinnedMHz != 0 {
		return pinnedMHz
	}
	if live == nil {
		return 0
	}
	if mhz, ok := live(); ok {
		return mhz
	}
	return 0
}

// launchFt8QsoLog runs one completed-FT8-exchange log+enrich job off the decode
// loop (review 2026-06-19 M2). Two properties matter, both pinned by tests:
//
//   - Tracked: GoTracked calls wg.Add(1) synchronously here, on the decode loop.
//     ft8Svc.Stop() drains that loop, so by the time it returns every in-flight
//     job is on wg and shutdown can drain it before the hub/DB tear down.
//   - Context-decoupled: work runs under a fresh Background-derived context
//     bounded by ft8QsoLogTimeout, NOT the decode-loop context (which Stop
//     cancels). A QSO that already completed on the air must still be persisted
//     during the shutdown drain rather than aborting on a cancelled context; the
//     timeout still bounds a hung enrich/submit so a straggler can't run forever.
func launchFt8QsoLog(wg *sync.WaitGroup, onPanic safego.PanicHandler, work func(ctx context.Context)) {
	safego.GoTracked(context.Background(), "ft8.qsolog", onPanic, func() {
		ctx, cancel := context.WithTimeout(context.Background(), ft8QsoLogTimeout)
		defer cancel()
		work(ctx)
	}, false /* one-shot: a completed QSO is a single event, no respawn */, wg)
}

// ensureDefaultLogbook guarantees the row at cfg.DefaultLogbookID
// exists in the logbook table at startup. Self-heals the failure mode
// "config marks setup_complete=true and points at logbook id=N but
// the DB has no row at N" — most commonly hits an operator who nuked
// a dev DB without clearing config.json's setup_complete flag.
//
// Three branches:
//
//   - DefaultLogbookID < 1: log a warn and return — config is broken
//     in a way startup can't fix without operator input.
//   - SetupComplete == false: return with no work. This is the genuine
//     first-run path; PUT /v1/config will seed the row when the operator
//     finishes setup. Pre-seeding here would race the setup handler.
//   - Row exists at DefaultLogbookID: return (idempotent).
//   - Row missing: insert using StationCallsign. If the assigned ID
//     differs (rare; happens only when the operator hand-edited
//     DefaultLogbookID to a non-1 value), persist the corrected ID
//     back to config.json so subsequent reads stay consistent.
//
// Logs a warn and returns nil (rather than failing startup) when
// StationCallsign is empty: the daemon should still come up so the
// operator can re-submit setup; QSO submit will surface a FK-shaped
// error until the row exists.
func ensureDefaultLogbook(
	ctx context.Context,
	dbSvc *sqlite.Service,
	cfgSvc *config.Service,
	logger *logging.Service,
) error {
	const op errors.Op = "smd.ensureDefaultLogbook"
	cfg := cfgSvc.Snapshot()

	if cfg.DefaultLogbookID < 1 {
		logger.WarnWith().
			Int64("default_logbook_id", cfg.DefaultLogbookID).
			Msg("startup: default_logbook_id < 1; cannot ensure default logbook exists")
		return nil
	}
	if !cfg.SetupComplete {
		return nil
	}

	if _, ferr := dbSvc.FetchLogbookByIDWithContext(ctx, cfg.DefaultLogbookID); ferr == nil {
		return nil
	} else if !stderr.Is(ferr, errors.ErrNotFound) {
		return errors.New(op).WithErr(ferr).WithMsg("fetching default logbook")
	}

	callsign := strings.TrimSpace(cfg.LoggingStation.StationCallsign)
	if callsign == "" {
		logger.WarnWith().
			Int64("default_logbook_id", cfg.DefaultLogbookID).
			Msg("startup: default logbook is missing AND station_callsign is empty; QSO submit will fail until the operator re-runs setup")
		return nil
	}

	id, err := dbSvc.InsertLogbookWithContext(ctx, types.Logbook{
		Name:        "Default",
		Callsign:    callsign,
		Description: "Default logbook (auto-recreated at startup — config pointed at a missing row)",
	})
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("seeding default logbook")
	}

	logger.InfoWith().
		Int64("id", id).
		Str("callsign", callsign).
		Msg("startup: seeded default logbook (config marked setup complete but DB had no row)")

	if id != cfg.DefaultLogbookID {
		// Correct the ID in memory FIRST, then best-effort persist:
		// UpdateInMemoryThenPersist commits to s.Cfg regardless of the
		// disk outcome, so run's re-Snapshot (below this call) sees the
		// corrected id even when the write fails. Persist failure is
		// non-fatal — the daemon comes up with the right logbook this
		// session and the next startup re-runs the same self-heal;
		// failing startup over a config-write error (permissions,
		// read-only fs) would be a worse outcome than logging it.
		// (Plain Update would NOT do this: it writes the file first and
		// only commits memory on success, so a failed write would leave
		// the stale id in memory and the first QSO submit would hit the
		// exact missing-logbook FK error this heal prevents.)
		if uErr := cfgSvc.UpdateInMemoryThenPersist(func(c *config.Config) error {
			c.DefaultLogbookID = id
			return nil
		}); uErr != nil {
			logger.WarnWith().
				Err(uErr).
				Int64("from", cfg.DefaultLogbookID).
				Int64("to", id).
				Msg("startup: default_logbook_id corrected in memory but could not persist to config.json")
			return nil
		}
		logger.InfoWith().
			Int64("from", cfg.DefaultLogbookID).
			Int64("to", id).
			Msg("startup: default_logbook_id corrected after seeding")
	}
	return nil
}

// stripCredentialKey removes one key from a forwarder's JSON credentials blob,
// returning the rewritten blob and whether it changed anything. Used at startup
// to scrub the legacy build-injected ClubLog API key (credentials.api) out of
// config.json (ADR 0054). A blob that is empty, not a JSON object, or lacks the
// key is returned unchanged with false, so a normal config is never rewritten.
// persistResolvedConfig writes the startup-resolved values back to config.json
// ONLY when something actually changed, and reports what it changed for the save
// record emitted once the logger is up (SHIP GATE (a), site B).
//
// It writes for exactly three named reasons and stays silent otherwise, so a
// quiet log means a quiet file (ruling 5): a schema MIGRATION (the on-disk
// document was an older version — Load migrated it in memory and the migrated
// shape must reach disk once, ADR 0075); a UserAgent fill; or a legacy ClubLog
// key scrub. A boot that resolves to exactly what is already on disk leaves the
// file's content and mtime untouched — only a legacy wide-mode file is still
// tightened to 0600, as an explicit permission action (M1).
//
// Soft-failure is the caller's business — a read-only working dir is degraded,
// not fatal, and the daemon still serves with the in-memory values.
func persistResolvedConfig(cfgSvc *config.Service, userAgent string) ([]config.FieldChange, error) {
	// Did Load migrate the on-disk document up a schema version? If so the migrated
	// shape must be persisted once even when the UA/scrub below is a no-op. Soft and
	// read-only: an unreadable version just means "assume current", so a genuine
	// no-op still writes nothing.
	migrated := false
	if v, verr := config.FileSchemaVersion(cfgSvc.Path); verr == nil {
		migrated = v < config.CurrentSchemaVersion()
	}

	changes, err := cfgSvc.UpdateIfChanged(migrated, func(c *config.Config) error {
		c.UserAgent = userAgent
		// Scrub any legacy ClubLog application API key left in config
		// credentials. The key is build-injected now (ADR 0054); an older config
		// carried it in credentials.api in PLAINTEXT. The SPA no longer renders
		// that field, so this startup scrub is the only path that clears it from
		// disk — leaving it there defeats the whole point of moving the key out
		// of config. GUARD: only scrub when THIS build actually has a baked
		// replacement — a keyless build must not delete the operator's only
		// usable key (and would break a rollback to a pre-0054 binary that still
		// requires credentials.api).
		if strings.TrimSpace(clublog.InjectedAPIKey) != "" {
			for i := range c.Forwarders {
				if c.Forwarders[i].Type != clublog.Type {
					continue
				}
				if scrubbed, ok := stripCredentialKey(c.Forwarders[i].Credentials, "api"); ok {
					c.Forwarders[i].Credentials = scrubbed
				}
			}
		}
		return nil
	})
	if err != nil {
		// Nothing reached disk, so there is nothing to report as saved.
		return nil, err
	}

	if migrated {
		// Name the migration as its own persistence reason. Only the path is logged;
		// no config value is attached (ADR 0074 §diag / ADR 0075).
		changes = append([]config.FieldChange{{
			Field: "schema_version",
		}}, changes...)
	} else if len(changes) == 0 {
		// Semantic no-op: nothing was written, so mtime is untouched. Tighten a
		// legacy wide-mode file to 0600 as an explicit permission action anyway.
		if terr := config.TightenFileMode(cfgSvc.Path); terr != nil {
			return nil, fmt.Errorf("tightening config permissions: %w", terr)
		}
	}
	return changes, nil
}

func stripCredentialKey(raw json.RawMessage, key string) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return raw, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, false // not an object — leave it alone
	}
	if _, present := m[key]; !present {
		return raw, false
	}
	delete(m, key)
	out, err := json.Marshal(m)
	if err != nil {
		return raw, false
	}
	return out, true
}

// spawnForwarderWorkers constructs one worker per enabled forwarder
// in cfg and launches each under safego.GoTracked with respawn=true (tracked so
// shutdown waits for the workers to drain). A panic inside a worker's Run path is
// recovered, logged via the daemon logger, and the worker is respawned so that a
// transient panic doesn't permanently disable a destination — see forwarding.md §9.
//
// Retry config resolution: operator's `retry` block wins if present;
// otherwise the forwarder package's own registered DefaultRetry is
// used (tuned to the upstream's tolerances — see each package's
// DefaultRetry var for rationale). A type with no registered default
// AND no explicit config retry is a setup error and fails startup
// loudly.
//
// Disabled entries are skipped (no goroutine spawned, no queue rows
// processed). If forwarding.Build rejects a config, startup fails —
// better to refuse to run than silently drop a destination the
// operator thought was active.
//
// Load-bearing invariant: this function is the single enforcement
// point for "at most one worker per forwarder_name" (the config
// loader validates Name uniqueness across cfg.Forwarders before we
// see it). ClaimPendingUploadsWithContext's documentation calls this
// out — don't add a second spawn site without preserving that chain,
// or two workers will share the same destination's queue slice.
func spawnForwarderWorkers(
	ctx context.Context,
	wg *sync.WaitGroup,
	fwds []types.ForwarderConfig,
	dbSvc *sqlite.Service,
	qsoSvc *qsoservice.Service,
	loggerSvc *logging.Service,
	hub *events.Hub,
) error {
	const op errors.Op = "smd.spawnForwarderWorkers"

	panicHandler := func(name string, pv any, stack []byte) {
		loggerSvc.ErrorWith().
			Str("goroutine", name).
			Interface("panic", pv).
			Bytes("stack", stack).
			Msg("forwarder worker panic recovered")
	}

	// A post-upload ADIF stamp bumps the QSO row's revision, so row-mirror
	// forwarders (SM Cloud) must have the row re-enqueued or their copy drifts
	// until the hourly reconcile heals it with a full-manifest diff. Failure is
	// logged only — the stamp has already committed, and the reconciler stays
	// the backstop.
	onStamped := func(ctx context.Context, qsoID int64) {
		if _, err := qsoSvc.EnqueueStampSync(ctx, []int64{qsoID}); err != nil {
			loggerSvc.WarnWith().Err(err).Int64("qso_id", qsoID).
				Msg("stamp sync enqueue failed (reconcile will heal)")
		}
	}

	for _, fc := range fwds {
		if !fc.Enabled {
			loggerSvc.InfoWith().Str("forwarder", fc.Name).Msg("forwarder disabled, skipping")
			continue
		}

		fwd, err := forwarding.Build(fc)
		if err != nil {
			// Startup aborts on the return below, and the returned error reaches
			// only stderr (main.go's run() wrapper) — so without this line a
			// credential or config fault leaves `smd starting` followed by
			// `smd stopped` in smd.log with no cause. Error, deliberately not
			// zerolog Fatal: returning the error must stay responsible for the
			// orderly deferred cleanup that os.Exit would skip.
			//
			// Named fields only. Serializing fc would write the operator's
			// credentials into a 0644 file — the rule stated at
			// internal/forwarding/registry.go, whose doc comment this line makes
			// true (it previously described logging that did not exist).
			loggerSvc.ErrorWith().
				Str("forwarder", fc.Name).
				Str("type", fc.Type).
				Err(err).
				Msg("forwarder build failed")
			return errors.New(op).WithErr(err).WithMsgf("build forwarder %q", fc.Name)
		}

		// ST-4a: a successfully-built smcloud forwarder whose URL is plain http to a
		// remote host built ONLY because allow_insecure_http is set (construction
		// refuses a remote-http URL otherwise). Warn on every startup — the bearer
		// token and all QSO + enabled evidence payloads travel in cleartext, observable
		// and modifiable in transit. Name the forwarder, never the URL (A9).
		if smcloud.InsecureRemoteURL(fc) {
			loggerSvc.WarnWith().
				Str("forwarder", fc.Name).
				Msg("smcloud forwarder uses plain http to a remote host (allow_insecure_http): " +
					"the bearer token and all QSO and enabled evidence payloads are observable " +
					"and modifiable in transit")
		}

		var retry types.RetryConfig
		if fc.Retry != nil {
			retry = *fc.Retry
		} else {
			def, ok := forwarding.DefaultRetryFor(fc.Type)
			if !ok {
				return errors.New(op).WithMsgf(
					"forwarder %q (type %q) has no retry config and no default registered — "+
						"either add a `retry` block to config.forwarders or have the forwarder package call "+
						"forwarding.RegisterDefaultRetry in init()", fc.Name, fc.Type,
				)
			}
			retry = def
		}

		w, err := worker.New(worker.Config{
			Name:         fc.Name,
			Tick:         time.Duration(fc.TickIntervalSec) * time.Second,
			Batch:        fc.BatchSize,
			Retry:        retry,
			OnQsoStamped: onStamped,
		}, fwd, dbSvc, loggerSvc, hub)
		if err != nil {
			return errors.New(op).WithErr(err).WithMsgf("construct worker for %q", fc.Name)
		}

		// Capture loop var for the closure — Go 1.22+ makes this safe
		// without explicit shadowing, but the extra clarity is free.
		workerRef := w

		// GoTracked owns the WaitGroup lifecycle: Add(1) at the call
		// site, Done() once when the goroutine permanently exits.
		// Respawns after a recovered panic stay inside the same
		// goroutine (loop-based, not recursive), so the WG count
		// never drops to zero between attempts. wg.Wait() in the
		// shutdown drain therefore reflects "any worker still
		// running or in cooldown," not "fn currently between
		// attempts."
		safego.GoTracked(ctx, fc.Name, panicHandler, func() {
			workerRef.Run(ctx)
		}, true, wg)

		// The periodic queue-depth summary runs as a PEER goroutine, not on the claim
		// loop: a slow/hung Submit must not starve it (L11). Tracked in the same WaitGroup
		// so shutdown drains it too, and respawned independently of the claim loop.
		safego.GoTracked(ctx, fc.Name+".summary", panicHandler, func() {
			workerRef.RunSummary(ctx)
		}, true, wg)

		loggerSvc.InfoWith().
			Str("forwarder", fc.Name).
			Str("type", fc.Type).
			Int("tick_interval_sec", fc.TickIntervalSec).
			Int("batch_size", fc.BatchSize).
			// The effective retry policy — max attempts + backoff bounds — so later
			// retry behaviour is reconstructable from the log alone: a type's registered
			// DefaultRetry need not appear in config.json (F15).
			Int("retry_max_attempts", retry.MaxAttempts).
			Int("retry_initial_backoff_sec", retry.InitialBackoffSec).
			Int("retry_max_backoff_sec", retry.MaxBackoffSec).
			Msg("forwarder worker started")
	}
	return nil
}

// buildEnrichment constructs the lookup pipeline (orchestrator +
// refresher + providers) per ADR 0017. Reads config to decide which
// providers to instantiate; an entry with Enabled=false is skipped
// at construction time (the orchestrator's Chain only ever sees
// enabled providers).
//
// Returns (nil, nil, nil) — explicit "lookup pipeline disabled" — when
// neither hamnut nor any chain provider is enabled. The HTTP handler
// detects the nil orchestrator and returns empty-result responses,
// keeping the SPA's response shape uniform.
//
// The refresher is Started with workerCtx so daemon shutdown
// (workerCancel above) cancels in-flight refresh fns; Stop is
// deferred by the caller to wait for the drain.
//
// Intentionally hand-wired outside the iocdi container: which providers
// to instantiate is a runtime decision read from operator config
// (cfg.Lookup.Hamnut.Enabled, cfg.Lookup.Chain[i].Enabled, and the
// provider Name discriminator inside the chain loop). DI registration
// would have to model "instantiate iff config flag X" plus a Name →
// concrete-type dispatch — neither shape the container expresses well.
// The orchestrator and refresher itself are then trivial struct
// literals, so promoting just those to DI would split the pipeline
// across two construction sites for no benefit. A grep for
// container.Register won't surface these; this comment is the pointer.
func buildEnrichment(
	workerCtx context.Context,
	cfg config.Config,
	cfgSvc *config.Service,
	dbSvc *sqlite.Service,
	refDbSvc *sqlite.Service,
	loggerSvc *logging.Service,
) (*lookup.Orchestrator, *refresher.Service, error) {
	const op errors.Op = "smd.buildEnrichment"

	// Providers are wired from the REGISTRY (ADR 0062), not a switch on name:
	// each provider package registers its constructor in init() and cmd/smd
	// imports it (see the blank imports at the top). Adding a provider is a
	// package plus an import line — not an edit here, in config's defaults, in
	// config's credential rules, and in the SPA.
	var countryProvider lookup.CountryProvider
	if cfg.Lookup.Hamnut.Enabled {
		countryCfg := cfg.Lookup.Hamnut
		ctor, ok := lookup.CountryConstructorFor(countryCfg.Name)
		if !ok {
			return nil, nil, errors.New(op).WithMsgf(
				"unknown lookup country provider %q (no constructor registered in this build)",
				countryCfg.Name,
			)
		}
		svc := ctor(loggerSvc, &countryCfg, cfg.UserAgent)
		if err := svc.Initialize(workerCtx); err != nil {
			return nil, nil, errors.New(op).WithErr(err).WithMsgf("initialize country provider %q", countryCfg.Name)
		}
		countryProvider = svc
		loggerSvc.InfoWith().Str("provider", svc.Name()).Msg("lookup: country provider enabled")
	}

	entries := slices.Clone(cfg.Lookup.Chain)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Priority < entries[j].Priority })
	chain := make([]lookup.CallsignProvider, 0, len(entries))
	for _, entry := range entries {
		if !entry.Enabled {
			loggerSvc.InfoWith().Str("provider", entry.Name).Msg("lookup: chain entry disabled, skipping")
			continue
		}
		entryCopy := entry
		ctor, ok := lookup.CallsignConstructorFor(entry.Name)
		if !ok {
			// Loud failure beats a silent skip: the operator's config names a
			// provider no binary in this build knows how to wire, and quietly
			// dropping it would degrade enrichment with no explanation.
			return nil, nil, errors.New(op).WithMsgf(
				"unknown lookup chain provider %q (no constructor registered in this build)",
				entry.Name,
			)
		}
		svc := ctor(loggerSvc, &entryCopy, cfg.UserAgent)
		if err := svc.Initialize(workerCtx); err != nil {
			return nil, nil, errors.New(op).WithErr(err).WithMsgf("initialize chain provider %q", entry.Name)
		}
		// The old wiring re-checked the provider's own Config.Enabled here. That
		// was documented UNREACHABLE (entry.Enabled is filtered above and a QRZ
		// session-key failure no longer flips the flag) and it read a concrete
		// type's field, which the registry's interface deliberately does not
		// expose. Dropped with the switch it lived in.
		chain = append(chain, svc)
		loggerSvc.InfoWith().Str("provider", svc.Name()).Msg("lookup: chain provider enabled")
	}

	if countryProvider == nil && len(chain) == 0 {
		loggerSvc.InfoWith().Msg("lookup: pipeline disabled (no providers enabled)")
		return nil, nil, nil
	}

	// Refresher is needed regardless of which layer is active —
	// stale-hit branches on either layer schedule via it.
	ref := &refresher.Service{
		LoggerService: loggerSvc,
		MaxInFlight:   cfg.Lookup.RefreshMaxInFlight,
	}
	if err := ref.Initialize(); err != nil {
		return nil, nil, errors.New(op).WithErr(err).WithMsg("initialize refresher")
	}
	if err := ref.Start(workerCtx); err != nil {
		return nil, nil, errors.New(op).WithErr(err).WithMsg("start refresher")
	}

	orch := &lookup.Orchestrator{
		DB:              refDbSvc, // enrichment caches live in reference.db
		LogDB:           dbSvc,    // new-entity check queries the log DB's qso table
		Country:         countryProvider,
		Chain:           chain,
		ContinueIfBlank: slices.Clone(cfg.Lookup.ContinueIfBlank),
		CountryTTL:      cfgSvc.CountryTTL(),
		StationTTL:      cfgSvc.StationTTL(),
		Refresher:       ref,
		Logger:          loggerSvc,
	}
	return orch, ref, nil
}

// defaultConfigPath returns smd's default config.json location when
// no explicit --config flag was supplied. The canonical resolver is
// utils.WorkingDir(): it respects SM_WORKING_DIR; otherwise falls
// back to $XDG_DATA_HOME/station-manager when the binary is
// installed under a system path (the systemd-managed deployment
// case); otherwise the binary's own directory (portable/dev use).
//
// Earlier this function preferred a config.json sitting in the cwd,
// but that was a footgun: the systemd unit starts the daemon with
// cwd=$HOME by default, so a stray config.json in $HOME (left over
// from a misconfigured `smd import` run, for example) would silently
// take precedence over the real install. Operators who genuinely want
// a per-invocation override pass --config explicitly.
//
// Shared by loadConfig (initial resolution) and resolveConfigPath
// (post-load path-record for PUT /v1/config rewrites) so the two
// precedence ladders cannot drift.
func defaultConfigPath() (string, error) {
	// utils.WorkingDir is the canonical resolver (explicit SM_WORKING_DIR →
	// XDG data dir → executable dir, MkdirAll-ing the result). A failure here
	// means the operator's intended working directory is set-but-inaccessible
	// or uncreatable — surface it, never silently fall back to cwd. The cwd
	// fallback was a footgun: a systemd unit runs with cwd=$HOME, so the daemon
	// would come up reading/seeding config.json in $HOME while the real install
	// was merely unreachable (memory feedback_daemon_workingdir_resolution).
	dir, err := utils.WorkingDir()
	if err != nil {
		return "", fmt.Errorf("resolving working directory for default config path: %w", err)
	}
	return filepath.Join(dir, "config.json"), nil
}

// resolveConfigPath mirrors loadConfig's precedence to determine the
// on-disk path that holds the config we just loaded. Used to populate
// cfgSvc.Path so /v1/config PUT can atomically rewrite the same file.
// firstRunPath wins if non-empty (we just seeded it); otherwise we
// recompute by checking the explicit flag, env var, and cwd in turn.
func resolveConfigPath(flagPath, firstRunPath string) (string, error) {
	if firstRunPath != "" {
		return firstRunPath, nil
	}
	if flagPath != "" {
		return flagPath, nil
	}
	return defaultConfigPath()
}

// loadConfig resolves and loads the daemon's configuration. The named
// firstRunPath return is non-empty only when a default config was
// just seeded to disk — the caller emits a structured log line for
// it once the logger is initialised, so the first-run event lands in
// smd.log alongside the rest of startup. firstRunPath is empty when
// an existing config was loaded.
func loadConfig(path string) (cfg config.Config, firstRunPath string, err error) {
	// Explicit path: operator chose it, surface a not-found as an error
	// rather than silently writing a default file at an arbitrary
	// location they didn't pick.
	if path != "" {
		cfg, err = config.Load(path)
		return cfg, "", err
	}

	candidate, err := defaultConfigPath()
	if err != nil {
		return config.Config{}, "", err
	}
	if _, statErr := os.Stat(candidate); statErr == nil {
		cfg, err = config.Load(candidate)
		return cfg, "", err
	} else if !os.IsNotExist(statErr) {
		// Only a genuine "file does not exist" means first-run. Any other
		// stat error (permissions, I/O) must not be misread as "missing" →
		// seeding a default at a path we can't even stat; surface it.
		return config.Config{}, "", fmt.Errorf("checking for config at %s: %w", candidate, statErr)
	}
	return firstRunWrite(candidate, filepath.Dir(candidate))
}

// firstRunWrite seeds a default config.json at the resolved candidate
// path so the operator gets a discoverable, hand-editable file on
// first run rather than an in-memory-only DefaultConfig that vanishes
// at shutdown. If the write fails (read-only fs, permission denied)
// the daemon falls back to in-memory defaults so it can still start.
// Returns the written path on success (empty on fallback) so the
// caller can log a structured first-run event after the logger boots.
//
// The write-failure stderr line is the only one kept: at that moment
// the structured logger may not boot at all (config defaulted to
// file_logging=true, but the working dir might be unwritable), so
// stderr is the operator's last available channel.
func firstRunWrite(path, baseDir string) (config.Config, string, error) {
	cfg := config.DefaultConfig(baseDir)
	if err := config.WriteJSON(path, cfg); err != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"smd: could not seed default config at %s: %v (continuing with in-memory defaults)\n",
			path, err)
		return cfg, "", nil
	}
	loaded, err := config.Load(path)
	if err != nil {
		return loaded, "", err
	}
	return loaded, path, nil
}

// ft8Keyer adapts the bridge's FT8 keying methods to the ft8.TxKeyer seam,
// keeping internal/ft8 free of any internal/bridge import (ADR 0030). The bridge
// owns the guaranteed-stop machinery (hard auto-off, release-on-disconnect,
// single-flight shared with the tune carrier); this is a thin pass-through.
type ft8Keyer struct{ b *bridge.Service }

func (k ft8Keyer) KeyTx(ctx context.Context, mode string) error { return k.b.KeyFt8Tx(ctx, mode) }
func (k ft8Keyer) UnkeyTx(ctx context.Context) error            { return k.b.UnkeyFt8Tx(ctx) }
func (k ft8Keyer) TxReady() bool                                { return k.b.TxReady() }
