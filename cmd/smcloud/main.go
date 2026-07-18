// smcloud is the SM Cloud backup/restore service (ADR 0040 /
// docs/v2-design/sm-cloud-p1.md): a small HTTP API (internal/cloud/server)
// over a Postgres store (internal/cloud/store) that the daemon's smcloud
// forwarder pushes QSOs to and the restore command pulls from.
//
// Configuration is flag + environment (a 12-factor service on a VPS under
// systemd, not a workstation app — no config.json):
//
//	-listen / SMCLOUD_LISTEN       listen address (default 127.0.0.1:8091 —
//	                               loopback; a LAN/proxy posture opts into a
//	                               wider bind explicitly)
//	-callsign / SMCLOUD_CALLSIGN   the P1 tenant callsign (required)
//	SMCLOUD_DSN                    Postgres DSN (env ONLY — the DSN carries the
//	                               DB password, and argv is world-readable via
//	                               ps; required)
//	SMCLOUD_TOKEN                  bearer token (env ONLY, same reason;
//	                               required, ≥32 chars, placeholder rejected)
//
// Boot: open + ping Postgres, apply the embedded migrations (store.Migrate —
// same files + tracking table as `task migrate:cloud:up`), ensure the tenant,
// serve. TLS is the reverse proxy's job (S6 hosting); this listener is plain
// HTTP. P1 is single-tenant by provisioning — one (token, tenant) pair —
// while the server's token→tenant map keeps multi-tenant a data change.
//
// NOT yet built — required before any internet-facing (Phase 2) deploy: a
// request rate limiter. The pool is bounded but requests are not, and
// /v1/health pings Postgres unauthenticated. Tracked in docs/backlog.md
// ("smcloud hardening — pre-Phase-2 gate"), gated with the ADR 0040
// security assessment.
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
	"strings"
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
		_, _ = fmt.Fprintf(os.Stderr, "smcloud: %v\n", err)
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

// tokenPlaceholder is the value shipped in smcloud.env.example. A service
// started with it would accept a publicly known credential, so boot refuses it.
const tokenPlaceholder = "CHANGE_ME_TOKEN"

// minTokenLen is the minimum accepted bearer-token length. The documented
// generator (`openssl rand -base64 32`) yields 44 chars; 32 keeps room for
// other strong generators while rejecting short guessable strings.
const minTokenLen = 32

// validateToken rejects an absent, placeholder, or too-short bearer token.
func validateToken(token string) error {
	switch {
	case token == "":
		return errors.New("no bearer token (set SMCLOUD_TOKEN)")
	case token == tokenPlaceholder:
		return errors.New("SMCLOUD_TOKEN is the shipped placeholder — generate a real one (openssl rand -base64 32)")
	case len(token) < minTokenLen:
		return fmt.Errorf("SMCLOUD_TOKEN too short (%d chars, need ≥%d) — generate one with: openssl rand -base64 32", len(token), minTokenLen)
	}
	return nil
}

// normalizeCallsign canonicalises the tenant callsign. The tenants table keys
// on exact (case-sensitive) text, so "7q5mlv" and "7Q5MLV " would provision
// SEPARATE tenants and the existing backup would vanish from the token's view —
// trim + uppercase makes equivalent spellings one tenant.
func normalizeCallsign(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func run() error {
	var listen, callsign string
	var showVersion bool
	flag.StringVar(&listen, "listen", envDefault("SMCLOUD_LISTEN", "127.0.0.1:8091"),
		"listen address (loopback default; LAN staging binds wider explicitly)")
	flag.StringVar(&callsign, "callsign", os.Getenv("SMCLOUD_CALLSIGN"),
		"tenant callsign (or SMCLOUD_CALLSIGN)")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(Version)
		return nil
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// The DSN embeds the DB password, so it is env-only — no -dsn flag, argv
	// leaks through ps/procfs (same rule as SMCLOUD_TOKEN).
	dsn := os.Getenv("SMCLOUD_DSN")
	if dsn == "" {
		return errors.New("no Postgres DSN (set SMCLOUD_DSN)")
	}
	callsign = normalizeCallsign(callsign)
	if callsign == "" {
		return errors.New("no tenant callsign (set -callsign or SMCLOUD_CALLSIGN)")
	}
	token := os.Getenv("SMCLOUD_TOKEN")
	if err := validateToken(token); err != nil {
		return err
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	// Bound the pool: /v1/health pings the DB unauthenticated, so an unbounded
	// pool lets request bursts eat Postgres's whole connection allowance.
	// (store.Migrate borrows one of these during boot and returns it.)
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
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
