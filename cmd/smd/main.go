package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/api"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/iocdi"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Version is the daemon build version, served by /v1/version. Override
// at build time with: go build -ldflags "-X main.Version=1.2.3" ...
var Version = "dev"

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "smd: %v\n", err)
		os.Exit(1)
	}
}

// run wires the daemon's lifecycle into a single function with
// defer-based cleanup. Any failure returns an error; deferred closers
// run on the way out so the "Open → Close" contract is honored in the
// happy path AND the failure path. The alternative — ad-hoc fatal()
// calls peppered through startup — left open handles when startup
// failed mid-way (see review L4).
func run() error {
	configPath := flag.String("config", "", "path to config.json (default: $SM_WORKING_DIR/config.json or ./config.json)")
	flag.Parse()

	// ---- Load configuration ----

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	// ---- Build DI container ----

	cfgSvc := config.New(cfg)

	container := iocdi.New()

	if err = container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		return fmt.Errorf("register config service: %w", err)
	}
	if err = container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil))); err != nil {
		return fmt.Errorf("register logging service: %w", err)
	}
	if err = container.Register(types.SqliteServiceName, reflect.TypeOf((*sqlite.Service)(nil))); err != nil {
		return fmt.Errorf("register sqlite service: %w", err)
	}
	if err = container.Register(qsoservice.ServiceName, reflect.TypeOf((*qsoservice.Service)(nil))); err != nil {
		return fmt.Errorf("register qso service: %w", err)
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
		return fmt.Errorf("build container: %w", err)
	}

	// ---- Resolve services ----

	loggerSvc, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		return fmt.Errorf("resolve logging service: %w", err)
	}
	// Register logger cleanup first (defer-LIFO means it runs last, after
	// dbSvc close below, so later defers can still use the logger).
	defer func() {
		loggerSvc.InfoWith().Msg("smd stopped")
		if err := loggerSvc.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "smd: logger close error: %v\n", err)
		}
	}()

	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		return fmt.Errorf("resolve sqlite service: %w", err)
	}

	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		return fmt.Errorf("resolve qso service: %w", err)
	}

	// ---- Open database and run migrations ----

	if err = dbSvc.Open(); err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	// Registered AFTER Open succeeds so we never double-close or close a
	// handle we didn't open.
	defer func() {
		if err := dbSvc.Close(); err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("database close error")
		}
	}()

	if err = dbSvc.Migrate(); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	loggerSvc.InfoWith().Msg("database open and migrated")

	// ---- Start HTTP server ----

	server := api.New(cfg, Version, qsoSvc, dbSvc, loggerSvc)

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

	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err = server.Shutdown(ctx); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("HTTP server shutdown error")
	}

	return runErr
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
