package main

import (
	"context"
	"encoding/json"
	stderr "errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/api"
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/enums/dxcc"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/evidence"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/clublog" // registers "clublog" forwarder + default retry via init(); main also sets clublog.UserAgent below
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrz"     // registers "qrz" forwarder + default retry via init(); main also sets qrz.UserAgent below
	"github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud" // registers "smcloud" forwarder (ADR 0040 backup client) via init(); main also sets smcloud.UserAgent below
	// The test-only "stub" forwarder is registered ONLY in dev builds (-tags dev,
	// see forwarder_stub_dev.go) — never in a release binary, so a production
	// config can't select type:"stub" and get fake "uploaded" status without
	// sending anywhere (review 2026-06-19 M3). The registry rejects an
	// unregistered type as "unknown forwarder type" at startup.
	"github.com/ColonelBlimp/station-manager/internal/forwarding/worker"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/inhibit"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	_ "github.com/ColonelBlimp/station-manager/internal/lookup/hamnut" // registers the hamnut country provider (descriptor + constructor) via init()
	_ "github.com/ColonelBlimp/station-manager/internal/lookup/qrz"    // registers the QRZ callsign provider (descriptor + constructor) via init()
	"github.com/ColonelBlimp/station-manager/internal/lookup/refresher"
	"github.com/ColonelBlimp/station-manager/internal/pskreporter"
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

	// Build triggers Initialize() on all beans in dependency order.
	if err = container.Build(); err != nil {
		err = errors.New(op).WithErr(err).WithMsg("build container")
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

	// The hub was built before the container (services inject it), so this is the
	// first point a logger exists to give it. It reports slow-reader evictions —
	// a subscriber dropped for not keeping up, which ends its SSE stream and is
	// otherwise indistinguishable from the client disconnecting normally.
	hub.SetLogger(loggerSvc)

	// Register logger cleanup first (defer-LIFO means it runs last, after
	// dbSvc close below, so later defers can still use the logger).
	defer func() {
		loggerSvc.InfoWith().Msg("smd stopped")
		if err := loggerSvc.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "smd: logger close error: %v\n", err)
		}
	}()

	// First-run event — structured log goes to smd.log so this major
	// startup transition is preserved alongside the rest of the
	// operator's record. The earlier stderr line covers the case where
	// logger init itself fails.
	if firstRunPath != "" {
		loggerSvc.InfoWith().
			Str("path", firstRunPath).
			Msg("first run: wrote default config to disk")
	}
	// Deferred from persistResolvedConfig above, which runs before the logger
	// exists — same shape as the first-run line. `source` is what separates
	// this from an operator's save; without it the two records are the same
	// event as far as a grep is concerned, which is the confusable state this
	// whole feature exists to break.
	if len(startupChanges) > 0 {
		loggerSvc.InfoWith().
			Str("source", "startup").
			Int("change_count", len(startupChanges)).
			Interface("changes", startupChanges).
			Msg("config saved")
	}
	// No Str("version", …) here: the base logger context carries it on every
	// record now, and setting it again would emit the key TWICE — legal JSON that
	// json.Unmarshal silently collapses, and two values that could drift.
	loggerSvc.InfoWith().
		Msg("smd starting")

	// Confirmation line so an operator can verify their config.json
	// `logging.level` setting actually took effect. Otherwise an
	// expected-debug-but-quiet daemon looks like a logger bug; the
	// real cause is usually "no DebugWith call in the active path"
	// rather than the level not loading. This makes the loaded level
	// observable.
	loggerSvc.InfoWith().
		Str("level", cfg.Logging.Level).
		Msg("logging configured")

	// Non-fatal config advisories (review m4). Surfaced once after the
	// logger boots so they land in smd.log alongside the rest of
	// startup. Currently the only check is "tcp protocol bound to a
	// non-loopback address" — accepted for trusted-LAN setups but
	// worth the operator seeing.
	for _, w := range config.Warnings(cfg) {
		loggerSvc.WarnWith().Msg(w)
	}

	// Unknown/misspelled config keys (review 2026-06-19 L1): Load silently
	// ignores them (forward-compat), so surface them here — a typo in a
	// hand-edited config otherwise just falls back to the default with no
	// signal. Best-effort: a re-read failure is non-fatal (Load already
	// succeeded on this path).
	if raw, rerr := os.ReadFile(cfgPath); rerr == nil {
		for _, k := range config.UnknownKeys(raw) {
			loggerSvc.WarnWith().Str("key", k).
				Msg("config: unrecognised key ignored (typo? — value falls back to default)")
		}
	}

	// Optional ADIF modes override: $SM_WORKING_DIR/modes.json layered
	// on top of the embedded baseline (`internal/enums/modes/adif-modes.json`).
	// Missing file is a no-op; malformed file is loud-and-fatal — an
	// operator who hand-edits this file should see the syntax error at
	// startup rather than silent IsValidMode rejections later.
	if err := modes.LoadOverride(cfg.DataDir); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("modes: override load failed")
		return errors.New(op).WithErr(err).WithMsg("load modes override")
	}

	// Optional DXCC entity override: $SM_WORKING_DIR/dxcc-entities.json layered
	// on top of the embedded baseline (`internal/enums/dxcc/dxcc-entities.json`),
	// used by the enrichment new-entity check (hamnut primaryDXCCPrefix → ADIF
	// DXCC number). Same loud-and-fatal contract as modes so a hand-edited file
	// surfaces its error at startup.
	if err := dxcc.LoadOverride(cfg.DataDir); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("dxcc: override load failed")
		return errors.New(op).WithErr(err).WithMsg("load dxcc override")
	}

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
	// Pin MY_RIG attribution to the rig active AT STARTUP — the same one the bridge
	// binds via cfg.ActiveBridge() below. A runtime "Set as default" only takes
	// effect for the bridge on the next restart, so MY_RIG must follow the same
	// startup rig, not the live default_rig_id, or QSOs made on the still-connected
	// old rig would be stamped with the new rig's identity (codex e539a080 P1).
	qsoSvc.SetActiveRig(cfg.DefaultRigID)

	// ---- Open databases and run migrations (reference.db / log-db split) ----
	// The log connection holds logbook/qso/qso_upload/qso_history; the reference
	// connection holds the operator-global enrichment caches in reference.db,
	// alongside the log DB in the same directory.
	logDBDir := filepath.Dir(dbSvc.DatabaseConfig.Path)
	refPath := filepath.Join(logDBDir, referenceDBFilename)

	// One-time, backup-first migration of an existing single-file DB into the
	// split. No-op on fresh installs and on already-split DBs. MUST run before
	// the connections open (it re-keys the log DB's migration tracking so the
	// log connection's migrate is a no-op rather than re-running 0002).
	if err = sqlite.BootstrapReferenceSplit(
		dbSvc.DatabaseConfig.Path, refPath, filepath.Join(logDBDir, "backups"), loggerSvc,
	); err != nil {
		return errors.New(op).WithErr(err).WithMsg("bootstrap reference split")
	}

	dbSvc.SetMigrationSets(sqlite.MigrationSetLog)
	if err = dbSvc.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open database")
	}
	// Registered AFTER Open succeeds, so that we never double-close or close a
	// handle we didn't open.
	defer func() {
		if err := dbSvc.Close(); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("database close error")
		}
	}()
	if err = dbSvc.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run migrations")
	}

	refDbSvc.SetMigrationSets(sqlite.MigrationSetReference)
	refDbSvc.SetDatabasePath(refPath)
	if err = refDbSvc.Open(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("open reference database")
	}
	defer func() {
		if err := refDbSvc.Close(); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("reference database close error")
		}
	}()
	if err = refDbSvc.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run reference migrations")
	}

	loggerSvc.InfoWith().Msg("databases open and migrated")

	// Self-heal the "config says setup-complete and points at logbook
	// id=N, but the DB has no row at N" failure mode. Most commonly
	// hits when an operator nukes a dev DB while keeping their
	// config.json — without this, QSO submit FK-violates on the first
	// log attempt with a blank cache. cfg is captured by value above;
	// re-snapshot here in case ensureDefaultLogbook persists a corrected
	// DefaultLogbookID and downstream code (server) needs the fresh value.
	if err = ensureDefaultLogbook(context.Background(), dbSvc, cfgSvc, loggerSvc); err != nil {
		return errors.New(op).WithErr(err).WithMsg("ensure default logbook")
	}
	cfg = cfgSvc.Snapshot()

	// ---- Forwarder workers ----
	// Orphan sweep: any qso_upload row left in 'in_progress' by a
	// previous crashed run is reset to 'pending'. This makes it
	// reclaimable.
	sweepCtx, sweepCancel := context.WithTimeout(context.Background(), 10*time.Second)
	n, err := dbSvc.ResetOrphanedUploadsWithContext(sweepCtx)
	sweepCancel()
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("reset orphaned upload rows")
	}
	if n > 0 {
		loggerSvc.InfoWith().Int64("reset", n).Msg("forwarder: orphaned in_progress rows reset to pending")
	}

	// ADR 0039: `enabled` gates enqueue, so a forwarder that is now disabled
	// must not retain a queue (the suspended state is gone). Discard its
	// not-yet-uploaded rows at startup — keeping 'uploaded' rows for upstream_id
	// provenance. The affected QSOs revert to "not uploaded to X" and are
	// recoverable via the logbook SPA. Logged loudly so the discard is never
	// silent ("disable" is now stop+drop, not pause).
	for _, fc := range cfg.Forwarders {
		if fc.Enabled {
			continue
		}
		discardCtx, discardCancel := context.WithTimeout(context.Background(), 10*time.Second)
		discarded, derr := dbSvc.DiscardQueuedUploadsForForwarderWithContext(discardCtx, fc.Name)
		discardCancel()
		if derr != nil {
			return errors.New(op).WithErr(derr).WithMsgf("discard queued uploads for disabled forwarder %q", fc.Name)
		}
		if discarded > 0 {
			loggerSvc.WarnWith().
				Str("forwarder", fc.Name).
				Int64("discarded", discarded).
				Msg("forwarder disabled; discarded queued uploads (re-upload via the logbook app)")
		}
	}

	// workerCtx is the parent context for all forwarder workers. It is
	// cancelled at shutdown, thus Run's select can observe it; each worker
	// then finishes its current processRow and exits.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	// Defer covers the error-return path between here and the
	// explicit workerCancel() in the happy-path shutdown sequence
	// below — that explicit call has to run before server.Shutdown
	// and the WG drain (it's part of the ordered teardown), not at
	// function return. context.CancelFunc is safe to call twice;
	// the second call is a no-op.
	defer workerCancel()

	// workerWG tracks live forwarder workers so shutdown can wait for
	// them to exit cleanly before the deferred dbSvc.Close() fires —
	// otherwise an in-flight DB query (inside processRow) can race the
	// close and surface as "database is closed" log spam on every
	// restart.
	var workerWG sync.WaitGroup

	// qsoLogWG tracks the off-pipeline FT8 completed-QSO log goroutines (below)
	// so shutdown can drain an in-flight log of an already-completed on-air QSO
	// before the daemon hub / DB tear down, instead of racing it (M2).
	var qsoLogWG sync.WaitGroup

	if err = spawnForwarderWorkers(workerCtx, &workerWG, cfg.Forwarders, dbSvc, qsoSvc, loggerSvc, hub); err != nil {
		return errors.New(op).WithErr(err).WithMsg("spawn forwarder workers")
	}

	// ---- SM Cloud reconcile (ADR 0040 S4) ----
	// One reconciler for the first enabled smcloud forwarder, guarding the
	// default logbook: a periodic detect+heal loop under the worker context
	// (its heal traffic rides that forwarder's queue, so it shares the
	// workers' lifecycle + WG drain), plus the on-demand
	// POST /v1/smcloud/reconcile wired onto the API server below.
	var smcloudRec *smcloud.Reconciler
	for _, fc := range cfg.Forwarders {
		if !fc.Enabled || fc.Type != smcloud.Type {
			continue
		}
		if cfg.DefaultLogbookID < 1 {
			loggerSvc.WarnWith().Str("forwarder", fc.Name).
				Msg("smcloud reconciler skipped: no default logbook yet (first-run setup pending)")
			break
		}
		rec, rerr := smcloud.NewReconciler(fc, cfg.DefaultLogbookID, dbSvc, qsoSvc, loggerSvc)
		if rerr != nil {
			return errors.New(op).WithErr(rerr).WithMsgf("build smcloud reconciler for %q", fc.Name)
		}
		smcloudRec = rec
		reconcilePanic := func(name string, pv any, stack []byte) {
			loggerSvc.ErrorWith().Str("goroutine", name).Interface("panic", pv).
				Bytes("stack", stack).Msg("smcloud reconciler panic recovered")
		}
		safego.GoTracked(workerCtx, fc.Name+"-reconcile", reconcilePanic, func() {
			rec.Run(workerCtx)
		}, true, &workerWG)
		loggerSvc.InfoWith().Str("forwarder", fc.Name).
			Int64("logbook_id", cfg.DefaultLogbookID).Msg("smcloud reconciler started")
		break // single smcloud destination in P1 — first enabled wins
	}

	// ---- Build enrichment pipeline (ADR 0017) ----
	// Constructs the lookup providers (hamnut + chain entries) from
	// operator config, the bounded async-refresh worker, and the
	// orchestrator that ties them together. nil orchestrator means
	// no providers were enabled — the API handler then returns
	// empty-result responses with source=none.
	enrichOrchestrator, lookupRefresher, err := buildEnrichment(workerCtx, cfg, cfgSvc, dbSvc, refDbSvc, loggerSvc)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("build enrichment pipeline")
	}
	if lookupRefresher != nil {
		// Stop the refresher BEFORE the deferred dbSvc.Close so
		// in-flight refresh fns (which run DB writes) finish against
		// a still-open handle. workerCancel above already cancelled
		// the parent context that the refresher was Started with, so
		// in-flight fns are seeing ctx.Done() — Stop just waits for
		// them to drain.
		defer func() {
			if err := lookupRefresher.Stop(); err != nil {
				loggerSvc.ErrorWith().Err(err).Msg("lookup refresher stop error")
			}
		}()
	}

	// ---- Mailer ----
	// Constructed regardless of whether SMTP is enabled; the Service
	// itself reports Enabled() based on cfg.Smtp.Enabled. Handlers check
	// Enabled() and return 503 mailer_disabled when disabled —
	// no startup-time error path for the "operator hasn't enabled
	// SMTP yet" state. (An enabled-but-incomplete block is rejected by
	// config validation at load, so it never reaches here.)
	mailerSvc := email.New(cfg.Smtp, loggerSvc)

	// ---- Bridge subsystem (ADR 0013 + ADR 0019) ----
	// Always constructed; the Service reports Enabled() from
	// cfg.Bridge.Enabled. When disabled (master smd / headless host
	// without a rig) Start() is a no-op and the api package skips
	// route registration. Started before the HTTP server so the
	// event emitter is publishing by the time SSE subscribers connect;
	// stopped BEFORE server.Shutdown in the teardown below — publishing
	// halts first, then the HTTP shutdown drains the SSE readers.
	//
	// cfg.ActiveBridge() projects the active rig's driver + serial port
	// (the rig Config.DefaultRigID selects, per ADR 0028) onto the bridge
	// knobs. Single-rig configs are migrated into a one-entry catalogue at
	// Load, so this resolves correctly whether or not the operator has
	// written a `rigs` block.
	bridgeSvc := bridge.New(cfg.ActiveBridge(), loggerSvc)
	if err := bridgeSvc.Initialize(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("initialize bridge")
	}
	if err := bridgeSvc.Start(workerCtx); err != nil {
		return errors.New(op).WithErr(err).WithMsg("start bridge")
	}
	// Defer covers the error-return path between here and the explicit
	// Stop in the happy-path shutdown sequence below. Stop is idempotent
	// (sync.Once), so the explicit call running first turns this defer
	// into a no-op on the happy path.
	defer func() {
		if err := bridgeSvc.Stop(); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("bridge: deferred stop error")
		}
	}()

	// FT8 decode subsystem (ADR 0024). Independent of the bridge — it
	// consumes receive audio, not CAT. When disabled (the default) Start is
	// a no-op; when enabled but capture won't start (no device, busy, or the
	// CGO-free build) it logs and stays idle. A decode is NOT a QSO: the
	// subsystem only logs "heard this" lines, so it never touches the
	// log/forward path (narrow-daemon-scope holds by the import graph).
	ft8Svc := ft8.NewService(cfg.ActiveFt8(), loggerSvc, cfgSvc.WorkingDir())
	// Wire the bridge as the FT8 TX keyer (ADR 0030): internal/ft8 keys PTT
	// through this adapter so it never imports internal/bridge (narrow-daemon-
	// scope by import graph). Only meaningful when the bridge is enabled and a
	// rig is connected; otherwise TxReady() stays false and arming is refused.
	ft8Svc.SetTxKeyer(ft8Keyer{bridgeSvc})
	// Ask the desktop to stay awake while TX is armed. An unattended FT8 run is
	// exactly when the host looks idle to its session, and a session/power event
	// mid-run is the suspected cause of the 2026-07-28 silent-transmit incident
	// (SM kept keying for 24 minutes with no audio reaching the rig). Injected like
	// the keyer, so internal/ft8 takes no D-Bus dependency; a host that grants no
	// inhibition (headless, no bus) logs once and transmits regardless.
	if types.ResolveFt8InhibitIdle(cfg.ActiveFt8().TX) {
		ft8Svc.SetIdleInhibitor(inhibit.New(loggerSvc))
	}
	// Self-decode filtering (dropping SM's own TX self-decoded off residual rig
	// TX-audio bleed) reads the ACTIVE session's pinned callsign directly from the
	// sequencer (ADR 0055, pin-at-arm) — no per-slot DB lookup, no fallback, no
	// timeout on the decode path. The call was resolved ONCE when the exchange was
	// armed (the /v1/ft8/qso/* handlers) and carried on the exchange, so nothing
	// needs to be wired here.
	// Gate FT8 capture on the rig/CAT being live — only when the bridge is
	// enabled (CAT configured). Without it, the daemon would grab the microphone
	// as soon as the FT8 view opens (e.g. SPA reopens to FT8 on PC boot) even with
	// the rig off. With no bridge (no CAT), no gate is installed and capture stays
	// purely demand-driven, so an audio-only setup is unaffected.
	if bridgeSvc.Enabled() {
		ft8Svc.SetCatGate(bridgeSvc.RigConnected)
		// Attribute each captured slot to the dial frequency it was heard on, so
		// an occupancy report says which band it measured instead of being
		// labelled with whatever band the rig is on when it reaches the SPA. Same
		// injection shape as the CAT gate — internal/ft8 never imports
		// internal/bridge. Without CAT there is nothing to attribute with, and
		// reports simply carry no frequency.
		ft8Svc.SetDialSource(bridgeSvc.CurrentDialMHz)
		// ADR 0064: the bridge's continuous ALC/PO meter poll lives and dies
		// with the FT8 capture session — this listener is the whole lifecycle
		// signal (no windowing state machine). Same injection shape as above.
		ft8Svc.SetCaptureListener(bridgeSvc.SetFt8CaptureLive)
	}
	// Wire the completed-QSO sink (ADR 0029 step e4): a finished FT8 exchange
	// becomes a logged QSO. The assembly + submit live here (the composition
	// root has config + qsoservice + adif), so internal/ft8 stays narrow — it
	// just emits a CompletedQso. Best-effort: a submit failure is logged, never
	// fatal (the QSO already happened on the air).
	ft8LogPanic := func(name string, pv any, stack []byte) {
		loggerSvc.ErrorWith().Str("goroutine", name).Interface("panic", pv).
			Str("stack", string(stack)).Msg("ft8: qso-log goroutine panicked")
	}
	ft8Svc.SetQsoLogger(func(_ context.Context, c ft8.CompletedQso) {
		// This callback fires on the FT8 decode loop (after the 73). Submit does
		// DB writes and the country lookup below does network I/O — running either
		// inline would stall slot decoding and drop slots (the scheduler buffers
		// one slot then drops on backpressure). So the whole log+enrich runs in a
		// one-shot safego goroutine, decoupled from the real-time pipeline; a panic
		// here is recovered and can't take the daemon (or the decode loop) down.
		//
		// launchFt8QsoLog tracks the goroutine on qsoLogWG and runs the work under
		// a fresh bounded context decoupled from the passed (decode-loop) context
		// — see its doc for why both matter (M2). The "_" input context is
		// deliberately unused here.
		launchFt8QsoLog(&qsoLogWG, ft8LogPanic, func(ctx context.Context) {
			snap := cfgSvc.Snapshot()
			// Authoritative frequency: c.DialFreqMHz, which internal/ft8 now stamps
			// from the dial the SESSION pinned (read from the bridge at start).
			//
			// This used to prefer a LIVE bridge read, because the value the SPA sends
			// at start goes stale across a Call-CQ pile-up (logged before the IC-7300's
			// freq poll had landed, producing wrong-band QSOs). The session pin fixes
			// that at the source and is strictly better: a start is refused while the
			// dial is unreadable, and a QSY during the session ends the session — so
			// the pin is always a real reading of where the contact happened. A live
			// read is now the WRONG answer in the one case they differ: a contact
			// completed just before a QSY would be filed on the new band.
			//
			// The live read stays only as a fallback for a completion carrying no
			// pinned dial at all (no CAT — where the read is unavailable too, so this
			// is close to dead code, kept so an unpinned path degrades rather than
			// logs zero).
			c.DialFreqMHz = resolveQsoDialMHz(c.DialFreqMHz, bridgeSvc.CurrentDialMHz)
			// Log to the logbook PINNED at arm (c.LogbookID, ADR 0055) — NOT the
			// current default — so a mid-exchange logbook switch can't relabel or
			// misroute the QSO. STATION_CALLSIGN is then derived by qsoservice.Submit
			// from that logbook (Slice B). Defensive fallback to the current default if
			// somehow unpinned (0 — shouldn't happen, a completion implies a Start).
			logbookID := c.LogbookID
			if logbookID < 1 {
				logbookID = snap.DefaultLogbookID
			}
			q := ft8.BuildQso(c, snap.LoggingStation, logbookID, time.Now().UTC(), loggerSvc)
			// ARRL Field Day RST_RCVD default (config ft8.field_day.default_rst_rcvd):
			// FD exchanges class/section, not a report, so we never receive an RST.
			// RST_SENT is the measured SNR (set by BuildQso); some OQRS systems require
			// RST_RCVD non-empty too, so fill it from the operator's configured default
			// for FD QSOs. Empty default / non-FD QSO → unchanged. A logging-policy
			// default, applied here beside the QSL / TX_PWR defaults (not on-air data).
			if q.ContestId == "ARRL-FD" && q.RstRcvd == "" && snap.Ft8.FieldDay != nil {
				q.RstRcvd = strings.TrimSpace(snap.Ft8.FieldDay.DefaultRstRcvd)
			}
			// Country/DXCC enrichment for the contacted station. The SPA logging
			// form gets this by calling /v1/enrich/callsign before it submits; the
			// FT8 path has no form, so the daemon runs the same lookup here. Besides
			// filling the QSO's country fields, Enrich's cold-miss path writes the
			// country cache row — without this call an FT8 QSO logged with country
			// "Unknown" and created no country record. Best-effort by contract:
			// Enrich never errors (failures fold to empty fields, source=none), so
			// the submit always runs — "enrichment never blocks logging" holds. The
			// on-air grid stays authoritative over any cached locator.
			if enrichOrchestrator != nil {
				enr := enrichOrchestrator.Enrich(ctx, q.Call)
				grid := q.Gridsquare
				q.ContactedStation = enr.Station
				q.Call = c.TheirCall
				if grid != "" {
					q.Gridsquare = grid
				}
				q.CountryDetails = enr.Country
			}
			// TX_PWR: the effective at-antenna power, mirroring the phone/CW SPA
			// derivation (displayedState.effectivePower) — the rig's live power
			// scaled by the linear amp when enabled, falling back to the
			// operator's default power when CAT can't report it (rig blip / not
			// yet read). Left unset (TX_PWR omitted) only when neither is known.
			rawW := float64(bridgeSvc.CurrentPowerW())
			if rawW == 0 {
				rawW = snap.Station.DefaultPower
			}
			if rawW > 0 {
				mult := 1.0
				if snap.Station.AmpEnabled && snap.Station.AmpMultiplier > 0 {
					mult = snap.Station.AmpMultiplier
				}
				q.TxPwr = strconv.Itoa(int(math.Round(rawW * mult)))
			}
			rec := adif.QsoToRecord(q)
			// Standing QSL defaults (config.json `qsl`) — fill the fields the QSO
			// leaves empty; empty defaults are skipped (omitempty drops them). FT8 has
			// no form, so the daemon applies them here; the Phone/CW submit handler
			// calls the same record method, so both modes log identical defaults.
			rec.ApplyQslDefaults(snap.Qsl)
			// force = the operator's explicit "work this station again" intent, pinned
			// at arm time and carried on the CompletedQso. Ordinary contacts pass
			// false and keep the duplicate protection; only a deliberate repeat
			// bypasses the minute-granular dedupe key, which would otherwise fold the
			// second on-air exchange into the first and store nothing.
			res, err := qsoSvc.Submit(ctx, q.LogbookID, rec, c.AllowDuplicate)
			if err != nil {
				loggerSvc.ErrorWith().Err(err).Str("call", c.TheirCall).
					Msg("ft8: failed to log completed QSO")
				return
			}
			// A contact completed ON AIR but deduplicated away stores NOTHING and
			// returns the FIRST contact's UUID, which the session sink then ignores
			// as already-seen — so without this the operator transmits a full
			// exchange and simply never sees a row (codex 0f9aa672 P1). The dedupe
			// key is minute-granular (call+band+mode+freq+date+HHMM), so this needs
			// two contacts with the same station inside one minute: unreachable on
			// the standard ladder (~60 s minimum) but reachable on the short ones —
			// work-a-caller and the single-rung type-4 work path. Surfacing it is
			// the floor, not the fix; the fix is an explicit operator override
			// reaching Submit's `force` (see docs/backlog.md, FT8 duplicate-QSO).
			if res.Status == "duplicate" {
				loggerSvc.WarnWith().Str("call", c.TheirCall).Str("band", q.Band).
					Str("uuid", res.UUID).
					Msg("ft8: completed QSO matched an existing row and was NOT stored — same station, band and minute")
			}
			loggerSvc.InfoWith().Str("call", c.TheirCall).Str("band", q.Band).
				Str("country", q.Country).Msg("ft8: completed QSO logged")
			// Surface the logged QSO to the SPA's session list (ADR 0029 step e4).
			// The canonical UUID flows through so the SPA's email-out / edit paths
			// work for FT8 rows; best-effort, after a confirmed store.
			ft8Svc.PublishQsoLogged(ft8.NewLoggedQso(q, res.UUID, loggerSvc))
		})
	})
	// Evidence capture (spot-network design §4.1, default-off consent layer):
	// the writer owns its own evidence.db beside the working directory's other
	// state, and receives every PHYSICAL slot's rich decode set via
	// SetEvidenceSink — the same one-way DI as the QSO logger, so internal/ft8
	// never imports the writer and the writer never imports ft8 (go-ft8 is the
	// shared vocabulary). Fail-soft like the rest of FT8: a writer that cannot
	// initialise or start logs and stays idle — evidence must never stop the
	// operator decoding or logging.
	evCfg := evidence.Config{
		Capture:  cfg.Evidence.Capture,
		CapBytes: cfg.Evidence.CapBytes,
		Path:     filepath.Join(cfgSvc.WorkingDir(), "evidence.db"),
		Antennas: cfg.Evidence.Antennas,
	}
	// §5 sync, consent layer 2: reuse the smcloud forwarder's channel —
	// validation already refused sync without it, so a resolution failure
	// here is a defensive log, not a reachable path.
	if cfg.Evidence.Sync {
		if url, token, err := config.EvidenceSyncCredentials(cfg); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("evidence: sync enabled but credentials unresolved; sync stays off")
		} else {
			evCfg.Sync, evCfg.SyncURL, evCfg.SyncToken = true, url, token
		}
	}
	evidenceSvc := evidence.New(evCfg, loggerSvc)
	if err := evidenceSvc.Initialize(); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("evidence: init failed; capture stays idle")
	} else if err := evidenceSvc.Start(); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("evidence: start failed; capture stays idle")
	} else if cfg.Evidence.Capture {
		ft8Svc.SetEvidenceSink(func(es ft8.EvidenceSlot) {
			start, err := time.Parse(time.RFC3339, es.Slot.StartUTC)
			if err != nil {
				return // a malformed slot ref cannot be archived honestly
			}
			evidenceSvc.CaptureSlot(evidence.SlotCapture{
				SlotStart:   start,
				DialMHz:     es.DialMHz,
				DialTracked: es.DialTracked,
				Outcome:     evidence.SlotOutcome(es.Outcome),
				Decodes:     es.Decodes,
			})
		})
	}

	// PSK Reporter upload (opt-in): every FT8 decode is a "heard this station"
	// reception report. The uploader is fed the decode stream via SetDecodeSink
	// (same one-way DI as the QSO logger), so internal/ft8 stays free of it and
	// narrow-daemon-scope holds. Best-effort: it never touches the decode timing.
	pskRxCall := strings.TrimSpace(cfg.LoggingStation.StationCallsign)
	if pskRxCall == "" {
		pskRxCall = strings.TrimSpace(cfg.LoggingStation.Operator)
	}
	pskSvc := pskreporter.New(
		pskreporter.Config{
			Enabled: cfg.PskReporter.Enabled,
			Host:    cfg.PskReporter.Host,
			Port:    cfg.PskReporter.Port,
		},
		pskreporter.Receiver{
			Call:     pskRxCall,
			Locator:  strings.TrimSpace(cfg.LoggingStation.MyGridsquare),
			Software: "StationManager " + buildinfo.Version,
			Antenna:  strings.TrimSpace(cfg.LoggingStation.MyAntenna), // ADIF MY_ANTENNA — single source
		},
		loggerSvc,
	)
	ft8Svc.SetDecodeSink(func(r ft8.DecodeReport) {
		// Gate on a configured receiver callsign too: an empty receiver record is
		// rejected by the server, so without a callsign we upload nothing.
		if !cfg.PskReporter.Enabled || pskRxCall == "" {
			return
		}
		// Absolute frequency = the dial the slot was CAPTURED on, stamped on the
		// report — never a live bridge read: publication lags capture by the
		// decode (~0.7–1.6 s), so a live read files a whole slot's spots on the
		// wrong band when the operator QSYs in that gap (review P1, 2026-08-07;
		// same attribution rule as occupancy). 0 = the slot was unattributable →
		// skip rather than spot at a guessed frequency (required field).
		dialMHz := r.DialMHz
		if dialMHz == 0 {
			return
		}
		t, err := time.Parse(time.RFC3339, r.Slot.StartUTC)
		if err != nil {
			return
		}
		dialHz := uint32(dialMHz * 1e6)
		unix := uint32(t.Unix())
		for _, d := range r.Decodes {
			call, grid, ok := ft8.SpotFrom(d.Text)
			if !ok || call == pskRxCall { // unparseable/hashed, or our own call
				continue
			}
			pskSvc.AddSpot(pskreporter.Spot{
				Call:     call,
				Grid:     grid,
				FreqHz:   dialHz + uint32(d.FreqHz),
				SNR:      int8(d.SNR),
				Mode:     "FT8",
				TimeUnix: unix,
			})
		}
	})

	if err := ft8Svc.Initialize(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("initialize ft8")
	}
	if err := ft8Svc.Start(workerCtx); err != nil {
		return errors.New(op).WithErr(err).WithMsg("start ft8")
	}
	// Start the PSK Reporter uploader (no-op + no socket when disabled). Best-effort:
	// a resolve/dial failure (DNS outage, typo'd host, bad port) must NOT stop the
	// daemon for an optional reporting path — log and continue without uploads.
	if err := pskSvc.Start(workerCtx); err != nil {
		loggerSvc.WarnWith().Err(err).Msg("pskreporter: start failed; FT8 spot upload disabled")
	}
	defer func() { _ = pskSvc.Stop() }()
	// Idempotent Stop (sync.Once); the explicit shutdown call below turns
	// this into a no-op on the happy path. Covers the error-return paths.
	defer func() {
		if err := ft8Svc.Stop(); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("ft8: deferred stop error")
		}
	}()

	// ---- Start HTTP server ----
	server := api.New(cfg, buildinfo.Version, cfgSvc, qsoSvc, dbSvc, loggerSvc, hub, enrichOrchestrator, mailerSvc, bridgeSvc, ft8Svc)
	server.SetEvidence(evidenceSvc)
	if smcloudRec != nil {
		// On-demand reconcile (the "back up / check now" action) — same
		// Reconciler instance as the periodic loop.
		server.SetSmcloudReconcile(func(ctx context.Context) (any, error) {
			return smcloudRec.RunOnce(ctx, smcloud.TriggerManual)
		})
	}

	// Planned self-restart (POST /v1/restart): the handler signals restartCh; run()
	// falls into the SAME graceful shutdown as a signal (the tune/FT8 carrier is
	// released cleanly), and main() exits ExitRestart so systemd respawns. Guarded
	// so a double POST can't close the channel twice.
	restartCh := make(chan struct{})
	var restartOnce sync.Once
	// Only wire the self-restart when the managing unit EXPLICITLY declares it will
	// respawn us — the bundled smd.service sets SM_SELF_RESTART=1 alongside
	// Restart=on-failure + RestartForceExitStatus=3. INVOCATION_ID is NOT enough:
	// systemd sets it for every unit, including Restart=no, which would exit
	// ExitRestart and stay stopped (codex 088bdb84 P2). A bare ./smd / `task
	// run:smd` (no supervisor) leaves this unset, so POST /v1/restart 503s rather
	// than killing the daemon for good.
	if os.Getenv("SM_SELF_RESTART") == "1" {
		server.SetRestart(func() {
			restartOnce.Do(func() {
				restartRequested.Store(true)
				close(restartCh)
			})
		})
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(cfg.SocketPath)
	}()

	// ---- Wait for shutdown signal ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var runErr error
	select {
	case sig := <-sigCh:
		loggerSvc.InfoWith().Str("signal", sig.String()).Msg("shutdown signal received")
	case <-restartCh:
		loggerSvc.InfoWith().Msg("restart requested (POST /v1/restart); graceful shutdown then exit for systemd respawn")
	case runErr = <-errCh:
		if runErr != nil {
			loggerSvc.ErrorWith().Err(runErr).Msg("server exited with error")
		}
	}

	// ---- Graceful shutdown ----
	// Release the bound port FIRST. The subsystem teardown below (bridge, ft8 —
	// which waits for an in-flight decode — and pskreporter) takes several seconds,
	// and server.Shutdown (which closes the listener) runs last. Closing the
	// listener up front frees :8080 immediately so a replacement process from a
	// deploy/restart can bind right away instead of racing the old daemon's
	// teardown ("address already in use" → systemd retry flap). Connections are
	// still drained by server.Shutdown below; only the accept listener closes here.
	server.StopAccepting()

	// Cancel forwarder workers first so that any in-flight forwarder Submit() call
	// with ctx-cancel support (e.g. HTTP POST to QRZ) aborts promptly,
	// and no new DB work is started against the about-to-close handle.
	// Each worker finishes its current processRow then returns from Run.
	workerCancel()

	// Stop the bridge subsystem. Cancels the publisher goroutine,
	// waits for it to exit, closes the BRIDGE subsystem's own hub (not
	// the daemon events.Hub, which is closed later, after HTTP handlers
	// drain) so any open rig-state SSE subscribers see a clean stream
	// end. Order matters relative to server.Shutdown: stop publishing
	// first, then drain readers.
	if err := bridgeSvc.Stop(); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("bridge: Stop error")
	}

	// Stop the FT8 subsystem alongside the bridge: cancels capture +
	// scheduler + decode worker, releases the audio device, and waits for an
	// in-flight decode (go-ft8 is not cancellable) to finish — bounded by one
	// slot's decode time.
	if err := ft8Svc.Stop(); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("ft8: Stop error")
	}
	// Flush + close the PSK Reporter uploader (final best-effort send).
	if err := pskSvc.Stop(); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("pskreporter: Stop error")
	}
	// Drain + close the evidence writer AFTER ft8Svc.Stop: the decode loop is
	// the only producer, so by now no new slots arrive and Stop persists any
	// accumulated loss before closing the archive.
	evidenceSvc.Stop()

	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeoutSec) * time.Second
	// config.applyDefaults sets ShutdownTimeoutSec=10 today, but a
	// hand-edited config.json with the field at zero (or omitted on
	// a future schema where applyDefaults misses it) would make
	// ctx.Done() fire immediately and spam-log "workers did not drain
	// within shutdown timeout" on every clean shutdown. Floor it so
	// the drain-reporting select stays meaningful.
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err = server.Shutdown(ctx); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("HTTP server shutdown error")
	}

	// Wait for forwarder workers to finish draining before the
	// deferred dbSvc.Close() fires. Bounded so a stuck worker (say,
	// a forwarder ignoring ctx inside a long upstream call) can't
	// wedge shutdown indefinitely.
	drainDone := make(chan struct{})
	go func() {
		workerWG.Wait()
		close(drainDone)
	}()
	select {
	case <-drainDone:
		loggerSvc.InfoWith().Msg("forwarder workers drained")
	case <-ctx.Done():
		loggerSvc.WarnWith().
			Dur("timeout", shutdownTimeout).
			Msg("forwarder workers did not drain within shutdown timeout")
	}

	// Drain in-flight FT8 completed-QSO log goroutines (M2). ft8Svc.Stop() above
	// halted the decode loop, so no new ones launch; these are exchanges that
	// finished on the air just as shutdown began and must still reach the DB.
	// Bounded by the same shutdown window — a straggler beyond it is left to the
	// deferred dbSvc.Close (its late Submit errors safely, same as any detached
	// goroutine racing close).
	qsoLogDone := make(chan struct{})
	go func() {
		qsoLogWG.Wait()
		close(qsoLogDone)
	}()
	select {
	case <-qsoLogDone:
	case <-ctx.Done():
		loggerSvc.WarnWith().
			Dur("timeout", shutdownTimeout).
			Msg("ft8 completed-QSO log goroutines did not drain within shutdown timeout")
	}

	// All publishers (workers, qsoservice via in-flight HTTP handlers, the FT8
	// QSO loggers drained above) have stopped by here — workers drained above,
	// handlers finished under server.Shutdown. Close the hub so any
	// still-connected SSE subscribers see a clean channel-close and return.
	//
	// Deliberately untimed: hub.Close holds the hub mutex only for a
	// brief synchronous "close every subscriber channel" loop and does
	// not wait on subscriber goroutines. If that ever changes, wrap
	// this in the same `select { <-done / <-ctx.Done() }` pattern
	// used for the worker drain above.
	hub.Close()

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
// and reports what it changed, for the save record emitted once the logger is
// up (SHIP GATE (a), site B).
//
// This runs on EVERY start and config.Service.Update writes unconditionally, so
// the file's mtime moves each boot whether or not anything moved with it. That
// is precisely why the record has to be delta-driven: an unconditional line
// here would be one noise entry per start, and mtime alone can never tell an
// operator's save from this one.
//
// Soft-failure is the caller's business — a read-only working dir is degraded,
// not fatal, and the daemon still serves with the in-memory values.
func persistResolvedConfig(cfgSvc *config.Service, userAgent string) ([]config.FieldChange, error) {
	var before, after config.Config
	err := cfgSvc.Update(func(c *config.Config) error {
		before = c.Clone()
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
		after = c.Clone()
		return nil
	})
	if err != nil {
		// Nothing reached disk, so there is nothing to report as saved.
		return nil, err
	}
	return config.Diff(before, after), nil
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

		loggerSvc.InfoWith().
			Str("forwarder", fc.Name).
			Str("type", fc.Type).
			Int("tick_interval_sec", fc.TickIntervalSec).
			Int("batch_size", fc.BatchSize).
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

	chain := make([]lookup.CallsignProvider, 0, len(cfg.Lookup.Chain))
	for _, entry := range cfg.Lookup.Chain {
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
		DB:         refDbSvc, // enrichment caches live in reference.db
		LogDB:      dbSvc,    // new-entity check queries the log DB's qso table
		Country:    countryProvider,
		Chain:      chain,
		CountryTTL: cfgSvc.CountryTTL(),
		StationTTL: cfgSvc.StationTTL(),
		Refresher:  ref,
		Logger:     loggerSvc,
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
