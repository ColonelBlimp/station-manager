package main

import (
	"context"
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
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/api"
	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/email"
	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrz" // registers "qrz" forwarder + default retry via init(); main also sets qrz.UserAgent below
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
	"github.com/ColonelBlimp/station-manager/internal/lookup/hamnut"
	lookupqrz "github.com/ColonelBlimp/station-manager/internal/lookup/qrz"
	"github.com/ColonelBlimp/station-manager/internal/lookup/refresher"
	"github.com/ColonelBlimp/station-manager/internal/pskreporter"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// Version is the daemon build version, served by /v1/version. Override
// at build time with: go build -ldflags "-X main.Version=1.2.3" ...
var Version = "dev"

// Process exit codes. Named so service managers (systemd, monit,
// supervisord) can distinguish a clean startup-error exit from an
// uncaught panic. ExitOK (0) is implicit — Go returns 0 when main
// exits normally, so there's no constant for it.
const (
	ExitError = 1
	ExitPanic = 2
)

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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "import":
			if err := runImport(os.Args[2:]); err != nil {
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
	adif.ProgramVersion = Version

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
		cfg.UserAgent = "station-manager/" + Version
	}
	if cfg.UserAgent == "" {
		err := fmt.Errorf("global UserAgent resolved to empty string; cannot start daemon (build version=%q)", Version)
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
	if uerr := cfgSvc.Update(func(c *config.Config) error {
		c.UserAgent = cfg.UserAgent
		return nil
	}); uerr != nil {
		_, _ = fmt.Fprintf(os.Stderr,
			"smd: could not persist resolved UserAgent to config.json: %v (continuing with in-memory value %q)\n",
			uerr, cfg.UserAgent)
	}
	// Forwarder package var — every QRZ forwarder POST stamps this
	// on the User-Agent header. Set after the UA is final.
	qrz.UserAgent = cfg.UserAgent

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
	loggerSvc.InfoWith().
		Str("version", Version).
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

	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve sqlite service")
	}

	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve qso service")
	}

	// ---- Open database and run migrations ----
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

	loggerSvc.InfoWith().Msg("database open and migrated")

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

	if err = spawnForwarderWorkers(workerCtx, &workerWG, cfg.Forwarders, dbSvc, loggerSvc, hub); err != nil {
		return errors.New(op).WithErr(err).WithMsg("spawn forwarder workers")
	}

	// ---- Build enrichment pipeline (ADR 0017) ----
	// Constructs the lookup providers (hamnut + chain entries) from
	// operator config, the bounded async-refresh worker, and the
	// orchestrator that ties them together. nil orchestrator means
	// no providers were enabled — the API handler then returns
	// empty-result responses with source=none.
	enrichOrchestrator, lookupRefresher, err := buildEnrichment(workerCtx, cfg, cfgSvc, dbSvc, loggerSvc)
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
			// Authoritative frequency: the rig's live dial from the bridge, NOT the
			// SPA's start-time snapshot (c.DialFreqMHz). The SPA value is captured once
			// when the session starts and reused for every QSO — across a whole Call-CQ
			// pile-up — so it goes stale (e.g. logged before the IC-7300's freq poll had
			// landed, producing wrong-band QSOs). The bridge is on frequency; prefer it,
			// falling back to the SPA value only when the bridge has no dial yet.
			if dialMHz, ok := bridgeSvc.CurrentDialMHz(); ok {
				c.DialFreqMHz = dialMHz
			}
			q := ft8.BuildQso(c, snap.LoggingStation, snap.DefaultLogbookID, time.Now().UTC())
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
			res, err := qsoSvc.Submit(ctx, q.LogbookID, rec, false)
			if err != nil {
				loggerSvc.ErrorWith().Err(err).Str("call", c.TheirCall).
					Msg("ft8: failed to log completed QSO")
				return
			}
			loggerSvc.InfoWith().Str("call", c.TheirCall).Str("band", q.Band).
				Str("country", q.Country).Msg("ft8: completed QSO logged")
			// Surface the logged QSO to the SPA's session list (ADR 0029 step e4).
			// The canonical UUID flows through so the SPA's email-out / edit paths
			// work for FT8 rows; best-effort, after a confirmed store.
			ft8Svc.PublishQsoLogged(ft8.NewLoggedQso(q, res.UUID))
		})
	})
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
			Software: "StationManager " + Version,
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
		// Absolute frequency needs the rig dial (the bridge has it; FT8 logs the
		// dial, not dial+offset). No reliable dial → skip the slot rather than
		// report a wrong/zero frequency (frequency is a required spot field).
		dialMHz, ok := bridgeSvc.CurrentDialMHz()
		if !ok {
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
	server := api.New(cfg, Version, cfgSvc, qsoSvc, dbSvc, loggerSvc, hub, enrichOrchestrator, mailerSvc, bridgeSvc, ft8Svc)

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

	for _, fc := range fwds {
		if !fc.Enabled {
			loggerSvc.InfoWith().Str("forwarder", fc.Name).Msg("forwarder disabled, skipping")
			continue
		}

		fwd, err := forwarding.Build(fc)
		if err != nil {
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
			Name:  fc.Name,
			Tick:  time.Duration(fc.TickIntervalSec) * time.Second,
			Batch: fc.BatchSize,
			Retry: retry,
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
	loggerSvc *logging.Service,
) (*lookup.Orchestrator, *refresher.Service, error) {
	const op errors.Op = "smd.buildEnrichment"

	var countryProvider lookup.CountryProvider
	if cfg.Lookup.Hamnut.Enabled {
		hamnutCfg := cfg.Lookup.Hamnut
		hamnutSvc := hamnut.NewService(loggerSvc, cfgSvc, &hamnutCfg, nil)
		hamnutSvc.UserAgent = cfg.UserAgent
		if err := hamnutSvc.Initialize(workerCtx); err != nil {
			return nil, nil, errors.New(op).WithErr(err).WithMsg("initialize hamnut provider")
		}
		countryProvider = hamnutSvc
		loggerSvc.InfoWith().Str("provider", hamnutSvc.Name()).Msg("lookup: country provider enabled")
	}

	chain := make([]lookup.CallsignProvider, 0, len(cfg.Lookup.Chain))
	for _, entry := range cfg.Lookup.Chain {
		if !entry.Enabled {
			loggerSvc.InfoWith().Str("provider", entry.Name).Msg("lookup: chain entry disabled, skipping")
			continue
		}
		entryCopy := entry
		switch entry.Name {
		case types.QRZLookupServiceName:
			qrzSvc := lookupqrz.NewService(loggerSvc, cfgSvc, &entryCopy, nil)
			qrzSvc.UserAgent = cfg.UserAgent
			if err := qrzSvc.Initialize(workerCtx); err != nil {
				return nil, nil, errors.New(op).WithErr(err).WithMsgf("initialize chain provider %q", entry.Name)
			}
			// QRZ Initialize disables itself on session-fetch failure
			// rather than returning an error — the Enabled flag may
			// have flipped under our feet. Skip in that case.
			if !qrzSvc.Config.Enabled {
				loggerSvc.WarnWith().Str("provider", entry.Name).Msg("lookup: chain provider disabled itself during init")
				continue
			}
			chain = append(chain, qrzSvc)
			loggerSvc.InfoWith().Str("provider", qrzSvc.Name()).Msg("lookup: chain provider enabled")
		default:
			// Unknown provider name in config. Loud failure beats
			// silent skip — operator's config has an entry that no
			// daemon binary knows how to wire.
			return nil, nil, errors.New(op).WithMsgf(
				"unknown lookup chain provider %q (no constructor wired in cmd/smd)",
				entry.Name,
			)
		}
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
		DB:         dbSvc,
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
