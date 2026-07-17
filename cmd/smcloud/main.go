// smcloud is the SM Cloud backup/restore service (ADR 0040 /
// docs/v2-design/sm-cloud-p1.md): a small HTTP API (internal/cloud/server)
// over a Postgres store (internal/cloud/store) that the daemon's smcloud
// forwarder pushes QSOs to and the restore command pulls from.
//
// Configuration is flag + environment (a 12-factor service on a VPS under
// systemd, not a workstation app — no config.json):
//
//	-listen / SMCLOUD_LISTEN       listen address        (default :8091)
//	-dsn    / SMCLOUD_DSN          Postgres DSN          (required)
//	-callsign / SMCLOUD_CALLSIGN   the P1 tenant callsign (required)
//	SMCLOUD_TOKEN                  bearer token (env ONLY — secrets stay out
//	                               of argv/ps; required)
//
// Boot: open + ping Postgres, apply the embedded migrations (store.Migrate —
// same files + tracking table as `task migrate:cloud:up`), ensure the tenant,
// serve. TLS is the reverse proxy's job (S6 hosting); this listener is plain
// HTTP. P1 is single-tenant by provisioning — one (token, tenant) pair —
// while the server's token→tenant map keeps multi-tenant a data change.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/ColonelBlimp/station-manager/internal/cloud/server"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// Version is stamped by the build (scripts/version.sh → -ldflags -X main.Version).
var Version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "smcloud: %v\n", err)
		os.Exit(1)
	}
}

// envDefault returns the environment value for key, else def.
func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run() error {
	var listen, dsn, callsign string
	var showVersion bool
	flag.StringVar(&listen, "listen", envDefault("SMCLOUD_LISTEN", ":8091"), "listen address")
	flag.StringVar(&dsn, "dsn", os.Getenv("SMCLOUD_DSN"), "Postgres DSN (or SMCLOUD_DSN)")
	flag.StringVar(&callsign, "callsign", os.Getenv("SMCLOUD_CALLSIGN"),
		"tenant callsign (or SMCLOUD_CALLSIGN)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(Version)
		return nil
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if dsn == "" {
		return errors.New("no Postgres DSN (set -dsn or SMCLOUD_DSN)")
	}
	if callsign == "" {
		return errors.New("no tenant callsign (set -callsign or SMCLOUD_CALLSIGN)")
	}
	token := os.Getenv("SMCLOUD_TOKEN")
	if token == "" {
		return errors.New("no bearer token (set SMCLOUD_TOKEN)")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()
	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	if err := store.Migrate(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	st := store.New(db)
	tenantID, err := st.EnsureTenant(context.Background(), callsign, "")
	if err != nil {
		return fmt.Errorf("ensure tenant %q: %w", callsign, err)
	}
	log.Info("smcloud starting", "version", Version, "listen", listen,
		"tenant", callsign, "tenant_id", tenantID)

	srv := server.New(st, db, log, map[string]int64{token: tenantID}, Version)
	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // a full-logbook backup batch on a slow link
		WriteTimeout:      2 * time.Minute, // a full export on a slow link
		IdleTimeout:       2 * time.Minute,
	}

	// Graceful shutdown on SIGINT/SIGTERM (systemd stop).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}
	log.Info("smcloud stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
