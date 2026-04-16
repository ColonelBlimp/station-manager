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

func main() {
	configPath := flag.String("config", "", "path to config.json (default: $SM_WORKING_DIR/config.json or ./config.json)")
	flag.Parse()

	// ---- Load configuration ----

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smd: %v\n", err)
		os.Exit(1)
	}

	// ---- Build DI container ----

	cfgSvc := config.New(cfg)

	container := iocdi.New()

	if err := container.RegisterInstance(config.ServiceName, cfgSvc); err != nil {
		fatal("register config service", err)
	}
	if err := container.Register(logging.ServiceName, reflect.TypeOf((*logging.Service)(nil))); err != nil {
		fatal("register logging service", err)
	}
	if err := container.Register(types.SqliteServiceName, reflect.TypeOf((*sqlite.Service)(nil))); err != nil {
		fatal("register sqlite service", err)
	}
	if err := container.Register(qsoservice.ServiceName, reflect.TypeOf((*qsoservice.Service)(nil))); err != nil {
		fatal("register qso service", err)
	}

	// The logging service's WorkingDir string field is resolved via LiteralProvider.
	iocdi.SetLiteralProvider(func(id string, targetType reflect.Type) (any, bool, error) {
		if id == "workingdir" && targetType.Kind() == reflect.String {
			return cfgSvc.WorkingDir(), true, nil
		}
		return nil, false, nil
	})

	// Build triggers Initialize() on all beans in dependency order.
	if err := container.Build(); err != nil {
		fatal("build container", err)
	}

	// ---- Resolve services ----

	loggerSvc, err := iocdi.ResolveAs[*logging.Service](container, logging.ServiceName)
	if err != nil {
		fatal("resolve logging service", err)
	}

	dbSvc, err := iocdi.ResolveAs[*sqlite.Service](container, types.SqliteServiceName)
	if err != nil {
		fatal("resolve sqlite service", err)
	}

	qsoSvc, err := iocdi.ResolveAs[*qsoservice.Service](container, qsoservice.ServiceName)
	if err != nil {
		fatal("resolve qso service", err)
	}

	// ---- Open database and run migrations ----

	if err := dbSvc.Open(); err != nil {
		fatal("open database", err)
	}

	if err := dbSvc.Migrate(); err != nil {
		fatal("run migrations", err)
	}

	loggerSvc.InfoWith().Msg("database open and migrated")

	// ---- Start HTTP server ----

	server := api.New(cfg, qsoSvc, dbSvc, loggerSvc)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe(cfg.SocketPath)
	}()

	// ---- Wait for shutdown signal ----

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		loggerSvc.InfoWith().Str("signal", sig.String()).Msg("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			loggerSvc.ErrorWith().Err(err).Msg("server exited with error")
		}
	}

	// ---- Graceful shutdown ----

	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("HTTP server shutdown error")
	}

	if err := dbSvc.Close(); err != nil {
		loggerSvc.ErrorWith().Err(err).Msg("database close error")
	}

	if err := loggerSvc.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "smd: logger close error: %v\n", err)
	}

	loggerSvc.InfoWith().Msg("smd stopped")
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

func fatal(context string, err error) {
	fmt.Fprintf(os.Stderr, "smd: %s: %v\n", context, err)
	os.Exit(1)
}
