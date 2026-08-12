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
//	-callsign / SMCLOUD_CALLSIGN   tenant 1's callsign (required)
//	-max-concurrent /
//	SMCLOUD_MAX_CONCURRENT         in-flight request cap (default 16, ~3× the
//	                               DB pool; over-limit → 503 + Retry-After;
//	                               the accept-time connection cap is 4× this)
//	SMCLOUD_DSN                    Postgres DSN (env ONLY — the DSN carries the
//	                               DB password, and argv is world-readable via
//	                               ps; required)
//	SMCLOUD_TOKEN                  tenant 1's bearer token (env ONLY, same
//	                               reason; required, ≥32 chars, placeholder
//	                               rejected)
//	SMCLOUD_CALLSIGN_N /
//	SMCLOUD_TOKEN_N                additional tenants, N in [2, 32] (milestone
//	                               1, ADR 0052 — hand-provisioned multi-
//	                               tenancy). Fail-loud: orphaned halves,
//	                               unparseable indices, duplicate tokens and
//	                               duplicate callsigns all refuse boot; see
//	                               collectTenantPairs.
//
// Boot: open + ping Postgres, apply the embedded migrations (store.Migrate —
// same files + tracking table as `task migrate:cloud:up`), ensure every
// provisioned tenant, serve. TLS is the reverse proxy's job (S6 hosting);
// this listener is plain HTTP. Tenant isolation is structural in the store
// (uuid uniqueness is scoped per tenant, migration 0004); rotating one
// tenant's token is an env-file edit + restart, touching no other tenant's
// credentials.
//
// Request limiting is two layers with distinct jobs (decided 2026-07-18):
// per-IP rate limiting lives at the reverse proxy, which sees real client
// IPs (deploy/smcloud/Caddyfile.example); THIS binary carries a two-level
// in-process bound so it stays safe even run without the proxy — an
// accept-time connection cap (netutil.LimitListener, 4× the request cap;
// net/http spawns a goroutine per accepted connection BEFORE any handler
// runs, so a handler-level limiter alone cannot bound a connection flood)
// plus the in-handler request semaphore (internal/cloud/server/limit.go).
// The DB pool protects Postgres; these protect the process. The remaining
// pre-Phase-2 gate is the ADR 0040 security assessment (docs/backlog.md,
// "smcloud hardening — pre-Phase-2 gate").
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/net/netutil"

	"github.com/ColonelBlimp/station-manager/internal/cloud/server"
	"github.com/ColonelBlimp/station-manager/internal/cloud/store"
)

// Version is stamped by the build (scripts/version.sh → -ldflags -X main.Version).
var Version = "dev"

func main() {
	if err := run(); err != nil {
		// Structured + version-stamped, not a bare stderr line: a boot failure (bad
		// DSN, migrate, bind) is the message most likely to be read on a failed deploy,
		// and journald otherwise captures it without version or consistent fields (C3).
		slog.New(slog.NewTextHandler(os.Stderr, nil)).With("version", Version).
			Error("smcloud failed to start", "err", err)
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
// varName is the environment variable being validated (SMCLOUD_TOKEN or a
// numbered SMCLOUD_TOKEN_N), so a multi-tenant boot failure names the exact
// line of the env file to fix.
func validateToken(varName, token string) error {
	switch {
	case token == "":
		return fmt.Errorf("no bearer token (set %s)", varName)
	case token == tokenPlaceholder:
		return fmt.Errorf("%s is the shipped placeholder — generate a real one (openssl rand -base64 32)", varName)
	case len(token) < minTokenLen:
		return fmt.Errorf("%s too short (%d chars, need ≥%d) — generate one with: openssl rand -base64 32", varName, len(token), minTokenLen)
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

const (
	minTenantCallsignLen = 3
	maxTenantCallsignLen = 32
)

// validTenantCallsign applies the same callsign shape used by the daemon's
// logbook/config surfaces. Tenant callsigns are durable namespace identities,
// so accepting punctuation or a digit-free label here would let a typo create
// a separate tenant that a later correction no longer authenticates into.
// The input has already been normalised by normalizeCallsign.
func validTenantCallsign(s string) bool {
	if len(s) < minTenantCallsignLen || len(s) > maxTenantCallsignLen {
		return false
	}
	hasDigit := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
		case c == '/' || c == '-':
		default:
			return false
		}
	}
	return hasDigit
}

func validateTenantCallsign(source, callsign string) error {
	if validTenantCallsign(callsign) {
		return nil
	}
	return fmt.Errorf("%s has invalid tenant callsign %q — use 3..32 ASCII letters/digits, '/' or '-', including at least one digit", source, callsign)
}

// tenantPair is one boot-provisioned (callsign, bearer token) credential pair.
type tenantPair struct {
	Callsign string // normalised (trimmed + uppercased)
	Token    string
	Source   string // the env variable pair that defined it, for error/log text
}

// Env-name shapes for numbered tenant pairs (SM Cloud milestone 1, ADR 0052).
const (
	callsignVarPrefix = "SMCLOUD_CALLSIGN_"
	tokenVarPrefix    = "SMCLOUD_TOKEN_"
)

// maxTenantPairs bounds the numbered-pair index. Hand-provisioned tenants
// (the ADR 0052 model — self-signup is explicitly out of scope) number a
// handful; 32 is far above any realistic env file while keeping a mistyped
// index (SMCLOUD_TOKEN_2000) a loud boot error instead of a silent tenant.
const maxTenantPairs = 32

// collectTenantPairs assembles the boot provisioning set: the legacy
// unnumbered pair (tenant 1 — callsign pre-resolved by the caller from
// -callsign/SMCLOUD_CALLSIGN, token from SMCLOUD_TOKEN) plus numbered
// SMCLOUD_CALLSIGN_N/SMCLOUD_TOKEN_N pairs, N in [2, maxTenantPairs].
//
// FAIL-LOUD by construction — every malformed shape is a boot error, never a
// silently missing tenant:
//   - the whole environ is scanned, so indices need not be contiguous (a gap
//     cannot skip a later pair) and an unparseable, non-canonical ("02", "+2"
//     — alternate spellings of one slot could cross-combine halves) or
//     out-of-range suffix on either prefix is refused rather than ignored;
//     (a REPEATED identical key is not our concern — systemd's EnvironmentFile
//     resolves it last-wins before exec, so os.Environ() never shows it twice;
//     see the scan loop);
//   - _1 is refused (the first tenant is the unnumbered pair — two spellings
//     for one slot would drift);
//   - each index needs BOTH halves (an orphaned callsign or token names the
//     missing variable);
//   - callsigns must match the daemon's canonical 3..32-character
//     [A-Z0-9/-] shape and contain a digit, so a typo cannot become a durable
//     backup namespace;
//   - duplicate tokens are refused: the server's token→tenant map would
//     silently collapse two tenants into one, authenticating writes into the
//     wrong logbook namespace;
//   - duplicate callsigns are refused: two tokens for one tenant is the
//     device-tokens feature (ADR 0052 roadmap) arriving unmanaged.
//
// Pairs return in deterministic order: legacy first, then ascending index.
func collectTenantPairs(legacyCallsign, legacyToken string, environ []string) ([]tenantPair, error) {
	legacy := tenantPair{
		Callsign: normalizeCallsign(legacyCallsign),
		Token:    legacyToken,
		Source:   "SMCLOUD_CALLSIGN/SMCLOUD_TOKEN",
	}
	if legacy.Callsign == "" {
		return nil, errors.New("no tenant callsign (set -callsign or SMCLOUD_CALLSIGN)")
	}
	if err := validateTenantCallsign("-callsign/SMCLOUD_CALLSIGN", legacy.Callsign); err != nil {
		return nil, err
	}
	if err := validateToken("SMCLOUD_TOKEN", legacy.Token); err != nil {
		return nil, err
	}

	// Gather every numbered variable, refusing any shape we don't understand.
	type half struct {
		callsign, token       string
		callsignSet, tokenSet bool
	}
	numbered := map[int]*half{}
	indexOf := func(name, prefix string) (int, error) {
		// The suffix must be the CANONICAL decimal spelling: Atoi alone maps
		// "02"/"+2" to the same index as "2", so two spellings of one slot
		// could coexist in the environ, cross-combining one spelling's
		// callsign with the other's token or silently discarding a pair —
		// the exact silent merge this validation exists to prevent (codex
		// review of the milestone commit).
		suffix := name[len(prefix):]
		n, err := strconv.Atoi(suffix)
		if err != nil || strconv.Itoa(n) != suffix {
			return 0, fmt.Errorf("unrecognised variable %s — numbered tenant pairs are %sN/%sN (canonical decimal index, no leading zeros or sign)",
				name, callsignVarPrefix, tokenVarPrefix)
		}
		switch {
		case n == 1:
			return 0, fmt.Errorf("%s: the first tenant is the unnumbered SMCLOUD_CALLSIGN/SMCLOUD_TOKEN pair — numbered pairs start at 2", name)
		case n < 2 || n > maxTenantPairs:
			return 0, fmt.Errorf("%s: tenant index must be 2..%d", name, maxTenantPairs)
		}
		return n, nil
	}
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(name, callsignVarPrefix):
			n, err := indexOf(name, callsignVarPrefix)
			if err != nil {
				return nil, err
			}
			h := numbered[n]
			if h == nil {
				h = &half{}
				numbered[n] = h
			}
			h.callsign, h.callsignSet = value, true
		case strings.HasPrefix(name, tokenVarPrefix):
			n, err := indexOf(name, tokenVarPrefix)
			if err != nil {
				return nil, err
			}
			h := numbered[n]
			if h == nil {
				h = &half{}
				numbered[n] = h
			}
			h.token, h.tokenSet = value, true
		}
		// NB no same-KEY duplicate check: the deployed config path is systemd
		// EnvironmentFile → process env → os.Environ(), and systemd resolves a
		// repeated assignment (two SMCLOUD_CALLSIGN_2= lines) to the LAST value
		// before exec, so an identical key never reaches us twice — a guard here
		// would be unreachable theatre (codex review of the milestone-fix
		// commit). This last-wins is generic env behaviour (SMCLOUD_DSN/TOKEN
		// too), not a tenant-provisioning-specific hole. What DOES reach us and
		// IS guarded is two DISTINCT keys for one logical index (_2 and _02):
		// the canonical-suffix rejection in indexOf refuses _02 outright, so the
		// same-index cross-combine can't form.
	}

	indices := make([]int, 0, len(numbered))
	for n := range numbered {
		indices = append(indices, n)
	}
	sort.Ints(indices)

	pairs := []tenantPair{legacy}
	for _, n := range indices {
		h := numbered[n]
		csVar := callsignVarPrefix + strconv.Itoa(n)
		tokVar := tokenVarPrefix + strconv.Itoa(n)
		if !h.callsignSet {
			return nil, fmt.Errorf("%s is set but %s is missing (each tenant needs both halves)", tokVar, csVar)
		}
		if !h.tokenSet {
			return nil, fmt.Errorf("%s is set but %s is missing (each tenant needs both halves)", csVar, tokVar)
		}
		cs := normalizeCallsign(h.callsign)
		if cs == "" {
			return nil, fmt.Errorf("%s is empty", csVar)
		}
		if err := validateTenantCallsign(csVar, cs); err != nil {
			return nil, err
		}
		if err := validateToken(tokVar, h.token); err != nil {
			return nil, err
		}
		pairs = append(pairs, tenantPair{Callsign: cs, Token: h.token, Source: csVar + "/" + tokVar})
	}

	seenCallsign := map[string]string{} // callsign → source
	seenToken := map[string]string{}    // token → source
	for _, p := range pairs {
		if prev, dup := seenCallsign[p.Callsign]; dup {
			return nil, fmt.Errorf("duplicate tenant callsign %s (%s and %s) — one credential per tenant; per-device tokens are a future milestone", p.Callsign, prev, p.Source)
		}
		if prev, dup := seenToken[p.Token]; dup {
			return nil, fmt.Errorf("duplicate bearer token in %s and %s — the token→tenant map would silently merge the tenants", prev, p.Source)
		}
		seenCallsign[p.Callsign] = p.Source
		seenToken[p.Token] = p.Source
	}
	return pairs, nil
}

// defaultMaxConcurrent mirrors server.limit.go's default so the flag help and
// an empty env agree with the server's own fallback.
const defaultMaxConcurrent = "16"

// maxMaxConcurrent is the operational ceiling on the request cap. 4096
// in-flight requests (→ 16384 connections via connCap) is far beyond any
// sane smcloud deployment; the bound also keeps connCap's ×4 from integer
// overflow — an extreme value would wrap negative and panic LimitListener's
// semaphore at boot (2026-07-19 review round 2 #3).
const maxMaxConcurrent = 4096

// tenantProvisionTimeout bounds the post-migration provisioning phase. The
// initial PingContext proves reachability, but EnsureTenant can still wait on
// a conflicting Postgres row/table lock. Without a deadline the Type=simple
// systemd service would remain "active" indefinitely before it opened its
// listener, so neither health checks nor Restart=on-failure could recover it.
const tenantProvisionTimeout = 10 * time.Second

// parseMaxConcurrent validates the in-flight request cap. Junk or an
// out-of-range value is a boot error, not a silent fallback — a mistyped
// limit on an internet-facing box should fail loudly.
func parseMaxConcurrent(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("max-concurrent %q is not an integer", s)
	}
	if n < 1 || n > maxMaxConcurrent {
		return 0, fmt.Errorf("max-concurrent must be 1..%d (got %d)", maxMaxConcurrent, n)
	}
	return n, nil
}

// connCap sizes the accept-time connection cap from the request cap. The
// request semaphore (server/limit.go) runs only after net/http has accepted a
// connection, spawned its goroutine, and parsed headers — so a connection or
// slow-header flood would grow goroutines outside it (2026-07-19 review #1).
// 4× leaves headroom for idle keep-alive connections from legitimate clients
// (each daemon holds a couple between worker ticks) while bounding the
// goroutine-per-connection growth a flood can cause.
func connCap(maxConcurrent int) int { return maxConcurrent * 4 }

func run() error {
	var listen, callsign, maxConcurrentStr string
	var showVersion bool
	flag.StringVar(&listen, "listen", envDefault("SMCLOUD_LISTEN", "127.0.0.1:8091"),
		"listen address (loopback default; LAN staging binds wider explicitly)")
	flag.StringVar(&callsign, "callsign", os.Getenv("SMCLOUD_CALLSIGN"),
		"tenant callsign (or SMCLOUD_CALLSIGN)")
	flag.StringVar(&maxConcurrentStr, "max-concurrent", envDefault("SMCLOUD_MAX_CONCURRENT", defaultMaxConcurrent),
		"in-flight request cap; over-limit requests get 503 + Retry-After")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(Version)
		return nil
	}

	// version on the base logger's context so EVERY record carries the build (C3) —
	// "which build wrote this" is otherwise unanswerable across a redeploy.
	log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("version", Version)
	// The DSN embeds the DB password, so it is env-only — no -dsn flag, argv
	// leaks through ps/procfs (same rule as SMCLOUD_TOKEN).
	dsn := os.Getenv("SMCLOUD_DSN")
	if dsn == "" {
		return errors.New("no Postgres DSN (set SMCLOUD_DSN)")
	}
	// Tenant provisioning (milestone 1, ADR 0052): the legacy pair is tenant 1,
	// numbered SMCLOUD_CALLSIGN_N/SMCLOUD_TOKEN_N pairs add more. Every
	// malformed shape is a boot error — see collectTenantPairs.
	pairs, err := collectTenantPairs(callsign, os.Getenv("SMCLOUD_TOKEN"), os.Environ())
	if err != nil {
		return err
	}
	maxConcurrent, err := parseMaxConcurrent(maxConcurrentStr)
	if err != nil {
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

	migStart := time.Now()
	if err := store.Migrate(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	// A durable "migrations applied" marker with duration: a boot that ran migrations
	// is otherwise indistinguishable from one that had nothing to apply (C3).
	log.Info("migrations applied", "duration_ms", time.Since(migStart).Milliseconds())
	st := store.New(db)
	tokens := make(map[string]int64, len(pairs))
	provisionCtx, cancelProvision := context.WithTimeout(context.Background(), tenantProvisionTimeout)
	for _, p := range pairs {
		tenantID, err := st.EnsureTenant(provisionCtx, p.Callsign, "")
		if err != nil {
			cancelProvision()
			return fmt.Errorf("ensure tenant %q: %w", p.Callsign, err)
		}
		tokens[p.Token] = tenantID
		// Callsign + id only — a token must never reach the log.
		log.Info("tenant provisioned", "callsign", p.Callsign, "tenant_id", tenantID, "source", p.Source)
	}
	cancelProvision()
	log.Info("smcloud starting", "version", Version, "listen", listen,
		"tenants", len(pairs), "max_concurrent", maxConcurrent)

	srv := server.New(st, db, log, tokens, Version, maxConcurrent)
	httpSrv := &http.Server{
		Addr:              listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute, // a full-logbook backup batch on a slow link
		WriteTimeout:      2 * time.Minute, // a full export on a slow link
		IdleTimeout:       2 * time.Minute,
		// Route net/http's own transport diagnostics + any panic escaping the handler
		// through slog (version + structured fields) instead of the default stderr
		// logger, which journald captures unstructured and unversioned (C6).
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	// Accept-time connection cap: LimitListener blocks Accept past the cap,
	// so a flood queues in the kernel backlog instead of becoming goroutines.
	// Together with the in-handler request semaphore this is the two-level
	// inbound bound — connections at accept, request work in limit.go.
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// The READY marker, logged only AFTER the bind succeeds: "smcloud starting" alone
	// (above, before the Listen) could not be told from a start that then failed to
	// bind and vanished (C3).
	log.Info("smcloud listening", "addr", ln.Addr().String())

	// Graceful shutdown on SIGINT/SIGTERM (systemd stop).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(netutil.LimitListener(ln, connCap(maxConcurrent))) }()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	log.Info("smcloud stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("smcloud stopped") // clean-exit marker — the pair to "listening" (C3)
	return nil
}
