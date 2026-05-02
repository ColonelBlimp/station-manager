package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/api"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrz"    // registers "qrz" forwarder + default retry via init(); main also sets qrz.UserAgent below
	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/stub" // side-effect: register "stub" forwarder + default retry
	"github.com/ColonelBlimp/station-manager/internal/forwarding/worker"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/safego"
	"github.com/ColonelBlimp/station-manager/internal/types"
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
func run() error {
	const op errors.Op = "smd.run"

	configPath := flag.String("config", "", "path to config.json (default: $SM_WORKING_DIR/config.json or ./config.json)")
	flag.Parse()

	// ---- Propagate build version to package-level vars ----
	// These packages expose a mutable var with a "dev" default so tests
	// stay hermetic; main.go injects the ldflags-bound Version so real
	// daemon runs report their build version correctly.
	//   - qrz.UserAgent goes on the User-Agent header of every QRZ API
	//     call (QRZ's developer guide asks for ≤128 chars identifying
	//     the client + app).
	//   - adif.ProgramVersion is emitted as PROGRAMVERSION in ADIF
	//     export headers.
	qrz.UserAgent = "station-manager/" + Version
	adif.ProgramVersion = Version

	// ---- Load configuration ----
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	// ---- Build DI container ----
	cfgSvc := config.New(cfg)

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

	// The logging service's WorkingDir string field is resolved via LiteralProvider.
	iocdi.SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		if id == "workingdir" && targetType.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})

	// Build triggers Initialize() on all beans in dependency order.
	if err = container.Build(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("build container")
	}

	// ---- Resolve services ----
	loggerSvc, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("resolve logging service")
	}

	// Register logger cleanup first (defer-LIFO means it runs last, after
	// dbSvc close below, so later defers can still use the logger).
	defer func() {
		loggerSvc.InfoWith().Msg("smd stopped")
		if err = loggerSvc.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "smd: logger close error: %v\n", err)
		}
	}()

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
		if err = dbSvc.Close(); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("database close error")
		}
	}()

	if err = dbSvc.Migrate(); err != nil {
		return errors.New(op).WithErr(err).WithMsg("run migrations")
	}

	loggerSvc.InfoWith().Msg("database open and migrated")

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
	defer workerCancel()

	// workerWG tracks live forwarder workers so shutdown can wait for
	// them to exit cleanly before the deferred dbSvc.Close() fires —
	// otherwise an in-flight DB query (inside processRow) can race the
	// close and surface as "database is closed" log spam on every
	// restart.
	var workerWG sync.WaitGroup

	if err = spawnForwarderWorkers(workerCtx, &workerWG, cfg.Forwarders, dbSvc, loggerSvc, hub); err != nil {
		return errors.New(op).WithErr(err).WithMsg("spawn forwarder workers")
	}

	// ---- Start HTTP server ----
	server := api.New(cfg, Version, qsoSvc, dbSvc, loggerSvc, hub)

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
	// Cancel forwarder workers first so that any in-flight forwarder Submit() call
	// with ctx-cancel support (e.g. HTTP POST to QRZ) aborts promptly,
	// and no new DB work is started against the about-to-close handle.
	// Each worker finishes its current processRow then returns from Run.
	workerCancel()

	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeoutSec) * time.Second
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

	// All publishers (workers, qsoservice via in-flight HTTP handlers)
	// have stopped by here — workers drained above, handlers finished
	// under server.Shutdown. Close the hub so any still-connected SSE
	// subscribers see a clean channel-close and return.
	hub.Close()

	return runErr
}

// spawnForwarderWorkers constructs one worker per enabled forwarder
// in cfg and launches each under safego.Go with respawn=true. A panic
// inside a worker's Run path is recovered, logged via the daemon
// logger, and the worker is respawned so that a transient panic doesn't
// permanently disable a destination — see forwarding.md §9.
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

func loadConfig(path string) (config.Config, error) {
	if path != "" {
		return config.Load(path)
	}

	// Try SM_WORKING_DIR/config.json, then ./config.json
	if dir := os.Getenv("SM_WORKING_DIR"); dir != "" {
		candidate := dir + "/config.json"
		if _, err := os.Stat(candidate); err == nil {
			return config.Load(candidate)
		}
	}

	if _, err := os.Stat("config.json"); err == nil {
		return config.Load("config.json")
	}

	// No config file found — use defaults.
	cwd, _ := os.Getwd()
	return config.DefaultConfig(cwd), nil
}
