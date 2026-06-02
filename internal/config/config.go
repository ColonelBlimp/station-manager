package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ServiceName is the DI bean ID for the config service (used by internal/iocdi).
const ServiceName = types.ConfigServiceName

// Config is the daemon's on-disk configuration.
type Config struct {
	// DataDir is the root directory for on-disk state: sqlite database,
	// log files, any future cache directories. Resolved via
	// utils.WorkingDir() at daemon startup.
	DataDir string `json:"data_dir"`

	// UserAgent is the single User-Agent header value the daemon sends
	// on every outbound HTTP call (callsign lookups, forwarders). One
	// knob applies to every external dependency — there's no reason
	// the QRZ forwarder, the QRZ lookup, and the hamnut lookup should
	// identify themselves to upstream services differently. The daemon
	// fills this on first-run startup with `station-manager/<version>`
	// (Version is ldflags-injected at build time); operators may
	// override in config.json. Daemon fails loudly at startup if this
	// is empty after Load + first-run-fill — see cmd/smd/main.go.
	UserAgent string `json:"useragent"`

	// SocketPath is the absolute path to the Unix domain socket the daemon
	// binds its HTTP API to.
	SocketPath string `json:"socket_path"`

	// Server holds HTTP server tunables.
	Server ServerConfig `json:"server"`

	// Datastore is the sqlite datastore configuration. Reuses
	// types.DatastoreConfig so the carry-forward sqlite service can consume
	// it without adapter glue.
	Datastore types.DatastoreConfig `json:"datastore"`

	// Logging is the zerolog + lumberjack configuration used by
	// internal/logging.
	Logging types.LoggingConfig `json:"logging"`

	// Forwarders declares the list of forwarding destinations the daemon
	// dispatches QSO events to. Shape and defaults: see
	// docs/v2-design/forwarding.md §2. Each enabled entry produces one
	// worker goroutine at startup.
	Forwarders []types.ForwarderConfig `json:"forwarders"`

	// LoggingStation describes the operator's own station — the
	// "logging station" side of every QSO in ADIF terms (vs the
	// "contacted station" side). Reuses types.LoggingStation rather
	// than defining a parallel config-only struct, per the canonical-
	// DTO project idiom: every field already has the right ADIF
	// JSON tag and ,omitempty rule. The "My Station" card and the
	// QSO ingest path read from the same shape.
	//
	// On first-run write the block marshals empty (`{}`) because all
	// fields are omitempty; the operator fills station_callsign via
	// the setup dialog (or hand-edits) and may add my_gridsquare,
	// my_rig, etc. as desired. The validate tag on StationCallsign
	// only runs on QSO ingest, not on config Load — empty here is
	// the legitimate pre-setup state.
	LoggingStation types.LoggingStation `json:"logging_station"`

	// SetupComplete is the explicit "operator has finished first-run
	// setup" flag. Server-managed: PUT /v1/config can never set or
	// clear it directly; the daemon flips false → true on the first
	// PUT that supplies a non-empty station_callsign. SPA reads it on
	// boot to decide whether to render the setup dialog. Once true it
	// stays true — operators may change their callsign later via the
	// My Station card without re-triggering setup.
	SetupComplete bool `json:"setup_complete"`

	// DefaultLogbookID is a scalar pointer at the logbook row used
	// when the SPA doesn't supply ?logbook=N on QSO submit. Stored as
	// a scalar here because the row's display fields (name, callsign)
	// live in the DB, not in config.json — the API GET /v1/config
	// joins them at response time. Defaults to 1, so the system runs
	// on first launch without "missing config" errors; the daemon
	// ensures a logbook row with that id exists during first-run
	// setup.
	DefaultLogbookID int64 `json:"default_logbook_id"`

	// DefaultRigID is a scalar pointer at the rig used when the SPA
	// doesn't specify which rig a QSO came from. The same UX rationale as
	// DefaultLogbookID — defaults to 1 so the system runs on first
	// launch. The rig's display fields (model, port) live in cfg.Rigs
	// (when CAT lands); the API joins them at response time.
	DefaultRigID int64 `json:"default_rig_id"`

	// Station holds operator-typed station-property preferences that
	// participate in QSO-emission derivations (e.g. effectivePower in
	// the SPA's displayedState). Distinct from LoggingStation, which
	// carries ADIF MY_* identity fields. Reuses types.StationConfig
	// per the canonical-DTO project idiom — same struct travels on
	// the wire (ConfigResponse.Station) and on disk (config.json).
	Station types.StationConfig `json:"station"`

	// Lookup holds the enrichment pipeline configuration per ADR 0017
	// — hamnut (country source-of-truth), the callsign-class provider
	// chain, per-table staleness TTLs, and the bound on the async-
	// refresh worker. Empty / zero-valued fields fall through to the
	// defaults applied in applyDefaults; an empty Chain is valid and
	// simply disables callsign-class enrichment.
	Lookup types.EnrichmentConfig `json:"lookup"`

	// Smtp holds the operator's SMTP submission credentials used by
	// the daemon's general-purpose mailer (internal/email). Empty Host
	// disables the mailer; Send returns ErrMailerDisabled and callers
	// fold that into a user-visible "email not configured" path. The
	// SessionPanel's "email session ADIF" button is the first consumer;
	// future alert paths (forwarder backlog, refresher repeated
	// failures, daemon health) plug into the same Send primitive.
	Smtp types.SmtpConfig `json:"smtp"`

	// Bridge configures the serial/CAT bridge subsystem (ADR 0013 +
	// ADR 0019). Enabled gates the whole subsystem: false = no serial
	// port acquisition, no `/v1/rig/events` route. The master-smd /
	// headless deployment shape sets it to false; the field-laptop /
	// any-host-with-a-rig deployment sets it to true with serial +
	// driver populated.
	Bridge types.BridgeConfig `json:"bridge"`

	// Ft8 configures the FT8 decode subsystem (ADR 0024). Enabled gates
	// the whole subsystem: false (the default) = no audio device acquired,
	// no decoder goroutines. Live capture requires the CGO build; on the
	// static default build an Enabled subsystem logs "capture unavailable"
	// and stays idle. There are no required sub-fields — an empty Device
	// means the system default capture device.
	Ft8 types.Ft8Config `json:"ft8"`
}

// ServerConfig holds HTTP server tunables. All timeouts are in seconds.
type ServerConfig struct {
	// Protocol is the network protocol for the listener: "tcp"
	// (default — needed for the embedded SPA, which browsers reach
	// over HTTP) or "unix" for the headless-daemon deployment shape.
	Protocol             string `json:"protocol"`
	ReadHeaderTimeoutSec int    `json:"read_header_timeout_sec"`
	ReadTimeoutSec       int    `json:"read_timeout_sec"`
	WriteTimeoutSec      int    `json:"write_timeout_sec"`
	IdleTimeoutSec       int    `json:"idle_timeout_sec"`
	ShutdownTimeoutSec   int    `json:"shutdown_timeout_sec"`
	MaxBodyBytes         int64  `json:"max_body_bytes"`

	// DefaultPageLimit is the page size used when a list request omits
	// ?limit. MaxPageLimit is the ceiling applied to any client-supplied
	// limit. Both live in config, not as code constants, per the no-magic-
	// numbers rule.
	DefaultPageLimit int `json:"default_page_limit"`
	MaxPageLimit     int `json:"max_page_limit"`

	// MaxContactHistoryResults caps the number of prior contacts returned
	// by /v1/contact-history. The endpoint doesn't paginate — it's a
	// "recent contacts" view, not a log browser — so the cap keeps
	// response size bounded for callsigns that have been worked many
	// times.
	MaxContactHistoryResults int `json:"max_contact_history_results"`

	// Accidental self-DoS floor. See docs/v2-design/api.md §6: a buggy
	// local client (cron loop, retry storm, SSE reconnect loop) can
	// exhaust daemon resources even in single-user mode, so a minimal
	// floor is maintained even though no malicious threat model applies.
	// All four values fail loud (429 / 503) rather than failing silent
	// (timeouts, OOM, starved forwarder).

	// MaxConcurrentRequests caps simultaneous non-SSE requests in
	// flight. When exceeded the server returns 503 server_busy rather
	// than queueing indefinitely. SSE requests to /v1/events are NOT
	// counted here — they are long-lived by design and have their own
	// cap below. Default: 128.
	MaxConcurrentRequests int `json:"max_concurrent_requests"`

	// MaxEventSubscribers caps simultaneous SSE subscribers on
	// /v1/events. Guards against a reconnect storm spawning unbounded
	// goroutines. Returns 503 when full. Default: 16.
	MaxEventSubscribers int `json:"max_event_subscribers"`

	// SubmitRatePerSec and SubmitRateBurst implement a token-bucket rate
	// limit on POST /v1/qso specifically (the hot path that writes
	// sqlite rows and spawns forwarder work). Requests beyond the burst
	// get 429 rate_limited. The submit endpoint is the one place a
	// runaway local client can cause the most damage, so it carries its
	// own cap on top of the concurrent-request floor. Defaults:
	// 20/sec, burst 40.
	SubmitRatePerSec int `json:"submit_rate_per_sec"`
	SubmitRateBurst  int `json:"submit_rate_burst"`

	// ServeSPA controls whether the daemon hosts the embedded logging
	// SPA at "/" (and any future SPAs at their respective prefixes).
	// Pointer-typed so applyDefaults can distinguish "absent from
	// config" (nil — apply the protocol-driven default) from
	// "explicitly false" (operator wants a headless TCP daemon).
	// Default: true on TCP (the first-run default), false on
	// Unix-socket (browsers can only reach TCP listeners).
	// See docs/v2-design/frontend-spa.md.
	ServeSPA *bool `json:"serve_spa,omitempty"`

	// EnableProfiling mounts the stdlib net/http/pprof handlers at
	// /debug/pprof/* when true. Off by default — pprof exposes
	// goroutine state, heap dumps, and CPU profiles that are
	// load-bearing forensic data for an attacker (they can also
	// serve as a DoS vector via /debug/pprof/profile?seconds=N).
	// Operator opts in for stress / soak tests; flip back off when
	// done. The /debug/* prefix lives outside the /v1/* API surface
	// and isn't documented in api.md — it's a development affordance,
	// not a stable contract.
	EnableProfiling bool `json:"enable_profiling,omitempty"`
}

// Load reads a JSON config file and returns a populated Config with defaults
// applied for any zero-valued fields.
func Load(path string) (Config, error) {
	var cfg Config

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("reading config file: %w", err)
	}

	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	applyDefaults(&cfg, filepath.Dir(path))

	if err = validateForwarders(cfg.Forwarders); err != nil {
		return cfg, fmt.Errorf("validating forwarders: %w", err)
	}
	if err = validateLookup(cfg.Lookup); err != nil {
		return cfg, fmt.Errorf("validating lookup: %w", err)
	}
	if err = validateSmtp(cfg.Smtp); err != nil {
		return cfg, fmt.Errorf("validating smtp: %w", err)
	}

	if err = validateBridge(cfg.Bridge); err != nil {
		return cfg, fmt.Errorf("validating bridge: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns a Config with sensible defaults. Used by the
// daemon's first-run path to seed config.json, and as a starting point
// in tests.
//
// The booleans set here are defaults-on for fields where false is a
// legitimate operator choice — flipping them in applyDefaults would
// silently rewrite an explicit operator false on every Load. So they
// live here, where the input is a zero-valued Config{} with no operator
// preference attached. applyDefaults below only fills the unambiguously
// blank fields (empty strings, zero ints with min validation).
func DefaultConfig(dataDir string) Config {
	var cfg Config
	cfg.Logging.WithTimestamp = true
	cfg.Logging.FileLogging = true
	cfg.Logging.LogFileCompress = true
	// StartTLS on by default — submission-port (587) practice and the
	// safer choice on any non-loopback SMTP server. Operators who run
	// a local fake (cleartext over loopback to a test smtp sink) flip
	// this to false. Lives in DefaultConfig (not applyDefaults) so an
	// explicit operator false doesn't get silently rewritten on every
	// load — same rationale as the Logging.* booleans above.
	cfg.Smtp.StartTLS = true
	// No default forwarder entries per ADR 0022: "defined in
	// config.json" carries operator intent ("I want this destination"),
	// so the daemon must not pre-populate the slice. Operators add
	// destinations they actually want, either by hand or via a future
	// setup affordance. The pre-ADR-0022 disabled-QRZ template lived
	// here as a UX convenience but produced the 3B8IDX bug class
	// (rows enqueued for destinations the operator never chose to use).
	cfg.Forwarders = nil
	// Hamnut country provider — disabled by default, prepopulated
	// with the canonical public endpoint. Operator flips enabled=true
	// to activate; nothing else needs editing for the common case.
	// applyDefaults below still fills Name + HttpTimeoutSec but the
	// URL is set here so it survives a deletion-and-restart cycle.
	cfg.Lookup.Hamnut = types.LookupConfig{
		Name:           types.HamNutLookupServiceName,
		Enabled:        false,
		URL:            "https://api.hamnut.com/v1/call-signs/prefixes",
		HttpTimeoutSec: 10,
	}
	// Symmetric: a disabled QRZ lookup chain entry, prepopulated with
	// the canonical QRZ XML endpoint and a placeholder credential
	// pair. Operator fills in username + password and flips
	// enabled=true. Same DefaultConfig-not-applyDefaults rationale as
	// the forwarder above.
	cfg.Lookup.Chain = []types.LookupConfig{
		{
			Name:           types.QRZLookupServiceName,
			Enabled:        false,
			URL:            "https://xmldata.qrz.com/xml/current",
			Username:       "",
			Password:       "",
			HttpTimeoutSec: 10,
			// Profile URL prefix — the SPA concatenates the
			// uppercased callsign onto the end (e.g.
			// "https://www.qrz.com/db/M0CMC"). Trailing slash is
			// load-bearing; v1 had the same comment in defaults.go.
			ViewURL: "https://www.qrz.com/db/",
		},
	}
	applyDefaults(&cfg, dataDir)
	return cfg
}

// WriteJSON serialises cfg to path as indented JSON via temp-file +
// rename so a partial write can never leave a half-formed config on
// disk for the next startup. The parent directory must already exist;
// daemon startup resolves that elsewhere via utils.WorkingDir.
func WriteJSON(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp config to %s: %w", path, err)
	}
	return nil
}

func applyDefaults(cfg *Config, baseDir string) {
	if cfg.DataDir == "" {
		cfg.DataDir = baseDir
	}

	// Server protocol defaults to TCP on first run because SM is
	// SPA-driven by default (see ADR 0001 — browser SPA as the
	// primary UI). Browsers can't reach a Unix socket, so a
	// unix-socket default would mean every operator hits a "blank
	// browser" failure mode on first launch and has to find +
	// hand-edit two config keys before they see anything. The
	// "install → run → only one prompt" UX principle picks TCP.
	// The unix-socket headless deployment shape is still supported
	// — the operator opts in by editing protocol+socket_path.
	if cfg.Server.Protocol == "" {
		cfg.Server.Protocol = "tcp"
	}
	if cfg.SocketPath == "" {
		// Default address for each protocol. localhost-only TCP so
		// the SPA's Vite dev proxy ("/v1" → http://localhost:8080)
		// reaches a daemon out of the box; the unix path is the
		// fallback for operators who flipped Protocol back.
		if cfg.Server.Protocol == "tcp" {
			cfg.SocketPath = "127.0.0.1:8080"
		} else {
			cfg.SocketPath = filepath.Join(os.TempDir(), "smd.sock")
		}
	}
	if cfg.Server.ReadHeaderTimeoutSec == 0 {
		// Slow-headers DoS guard (review M3). Bounds the pre-handler
		// exposure independently of ReadTimeout — operators may tune
		// ReadTimeout up for slow clients, but a fixed 5s cap on
		// headers-only protects against slowloris-style attacks
		// regardless. See docs/v2-design/api.md §6 for the threat
		// model (self-DoS from buggy local clients).
		cfg.Server.ReadHeaderTimeoutSec = 5
	}
	if cfg.Server.ReadTimeoutSec == 0 {
		cfg.Server.ReadTimeoutSec = 10
	}
	if cfg.Server.WriteTimeoutSec == 0 {
		cfg.Server.WriteTimeoutSec = 30
	}
	if cfg.Server.IdleTimeoutSec == 0 {
		cfg.Server.IdleTimeoutSec = 120
	}
	if cfg.Server.ShutdownTimeoutSec == 0 {
		cfg.Server.ShutdownTimeoutSec = 10
	}
	if cfg.Server.MaxBodyBytes == 0 {
		cfg.Server.MaxBodyBytes = 1 << 20 // 1 MiB
	}
	if cfg.Server.DefaultPageLimit == 0 {
		cfg.Server.DefaultPageLimit = 50
	}
	if cfg.Server.MaxPageLimit == 0 {
		cfg.Server.MaxPageLimit = 500
	}
	if cfg.Server.MaxConcurrentRequests == 0 {
		cfg.Server.MaxConcurrentRequests = 128
	}
	if cfg.Server.MaxEventSubscribers == 0 {
		cfg.Server.MaxEventSubscribers = 16
	}
	if cfg.Server.SubmitRatePerSec == 0 {
		cfg.Server.SubmitRatePerSec = 20
	}
	if cfg.Server.SubmitRateBurst == 0 {
		cfg.Server.SubmitRateBurst = 40
	}
	if cfg.Server.MaxContactHistoryResults == 0 {
		cfg.Server.MaxContactHistoryResults = 100
	}
	if cfg.Server.ServeSPA == nil {
		// Default: serve the SPA on TCP, not on Unix-socket. Browsers
		// can only reach TCP listeners; a Unix-socket deployment is
		// implicitly headless.
		def := cfg.Server.Protocol == "tcp"
		cfg.Server.ServeSPA = &def
	}
	if cfg.Ft8.EnableOSD == nil {
		// OSD-2/MRB fallback decode defaults on: negligible time cost,
		// real weak-signal recall gain (see types.Ft8Config.EnableOSD).
		// nil (absent from config) → true; an explicit false is honoured.
		def := true
		cfg.Ft8.EnableOSD = &def
	}

	// Datastore defaults
	if cfg.Datastore.Driver == "" {
		cfg.Datastore.Driver = types.SqliteDriverName
	}
	if cfg.Datastore.Path == "" {
		// Sits under ${DataDir}/db/ to match the build/{bin,db,log}
		// layout used in development and any future packaged install.
		cfg.Datastore.Path = filepath.Join(cfg.DataDir, "db", "station-manager.db")
	}
	// SQLite WAL allows many concurrent readers + a single writer, so
	// a pool > 1 lets the read queries on the QSO submit path
	// (LogbookCallsignByID, FetchQsoByDedupeKey, FetchContactedStation)
	// run in parallel rather than queueing behind the same connection.
	// 8 is generous for a single-operator workload (typical peak: 2-3
	// concurrent requests when a Tab fires enrichment + contact-history
	// while a previous submit is still committing) and captured most of
	// the pool=16 win in stress testing 2026-05-09 (143 → ~1100 req/s
	// at pool=8 vs ~1400 at pool=16).
	//
	// Earlier default of 1 came from a real concern that didn't
	// survive scrutiny: under the prior mattn-style DSN options
	// (`_busy_timeout` / `_journal_mode`), modernc.org/sqlite silently
	// ignored the per-connection PRAGMAs, so a pool > 1 returned
	// SQLITE_BUSY immediately on write contention rather than waiting
	// up to busy_timeout. Once the DSN was fixed to use modernc's
	// `_pragma=name(value)` syntax (every new connection inherits the
	// PRAGMAs), the single-writer limit is enforced by SQLite itself,
	// not by the pool size.
	if cfg.Datastore.MaxOpenConns == 0 {
		cfg.Datastore.MaxOpenConns = 8
	}
	if cfg.Datastore.MaxIdleConns == 0 {
		cfg.Datastore.MaxIdleConns = 8
	}
	if cfg.Datastore.ContextTimeout == 0 {
		cfg.Datastore.ContextTimeout = 10
	}
	if cfg.Datastore.TransactionContextTimeout == 0 {
		cfg.Datastore.TransactionContextTimeout = 10
	}

	// Logging defaults. Boolean fields (with_timestamp, file_logging,
	// log_file_compress) are intentionally NOT touched here — they are
	// set in DefaultConfig instead so an explicit `false` from the
	// operator's config file isn't silently flipped back on. Long-term
	// fix: convert to *bool (mirrors Server.ServeSPA).
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.RelLogFileDir == "" {
		cfg.Logging.RelLogFileDir = "log"
	}
	if !cfg.Logging.ConsoleLogging && !cfg.Logging.FileLogging {
		cfg.Logging.FileLogging = true
	}
	if cfg.Logging.LogFileMaxSizeMB == 0 {
		cfg.Logging.LogFileMaxSizeMB = 100
	}
	if cfg.Logging.LogFileMaxBackups == 0 {
		cfg.Logging.LogFileMaxBackups = 5
	}
	if cfg.Logging.LogFileMaxAgeDays == 0 {
		cfg.Logging.LogFileMaxAgeDays = 30
	}

	// Station defaults. Amp disabled with a 1.0 multiplier so an
	// unset block reads as "no amp" and behaves identically to
	// pre-Station-block deployments. Operator opts in via the My
	// Station panel's QSO sub-tab.
	if cfg.Station.AmpMultiplier == 0 {
		cfg.Station.AmpMultiplier = 1.0
	}

	// Default-pointer defaults. 1/1 so the system has a sane "default
	// logbook" and "default rig" to point at on first run, even before
	// the operator finishes setup. The first-run logbook row is seeded
	// to id=1 by the /v1/config setup transition; the default rig is
	// a no-op until CAT lands.
	if cfg.DefaultLogbookID == 0 {
		cfg.DefaultLogbookID = 1
	}
	if cfg.DefaultRigID == 0 {
		cfg.DefaultRigID = 1
	}

	// Forwarder defaults — see docs/v2-design/forwarding.md §4.
	// Zero-valued tunables pick up the operator-environment defaults; a
	// nil Retry stays nil so the forwarder package supplies its own
	// type-specific retry numbers.
	for i := range cfg.Forwarders {
		fc := &cfg.Forwarders[i]
		if fc.TickIntervalSec == 0 {
			fc.TickIntervalSec = 120
		}
		if fc.BatchSize == 0 {
			fc.BatchSize = 5
		}
		if len(fc.ActionFilter) == 0 {
			fc.ActionFilter = []string{
				string(action.Insert),
				string(action.Update),
				string(action.Delete),
			}
		}
	}

	// Enrichment pipeline defaults (ADR 0017). Per-table TTLs differ
	// because DXCC entities turn over rarely (long country TTL) but
	// operator licenses / addresses change more often (shorter
	// contacted_station TTL). RefreshMaxInFlight matches
	// refresher.DefaultMaxInFlight — operator can tune via config.
	if cfg.Lookup.CountryTTLDays == 0 {
		cfg.Lookup.CountryTTLDays = 365
	}
	if cfg.Lookup.StationTTLDays == 0 {
		cfg.Lookup.StationTTLDays = 90
	}
	if cfg.Lookup.RefreshMaxInFlight == 0 {
		cfg.Lookup.RefreshMaxInFlight = 4
	}
	// Stamp the canonical Hamnut name when the operator left it empty
	// — keeps LookupServiceConfig(name) lookups predictable without
	// forcing the operator to type the magic string in their config.
	if cfg.Lookup.Hamnut.Name == "" {
		cfg.Lookup.Hamnut.Name = types.HamNutLookupServiceName
	}
	if cfg.Lookup.Hamnut.HttpTimeoutSec == 0 {
		cfg.Lookup.Hamnut.HttpTimeoutSec = 10
	}
	// Per-chain-entry timeout default — operators rarely need to
	// override this so the explicit zero is treated as "use the
	// daemon default" rather than "infinite timeout."
	for i := range cfg.Lookup.Chain {
		c := &cfg.Lookup.Chain[i]
		if c.HttpTimeoutSec == 0 {
			c.HttpTimeoutSec = 10
		}
	}

	// SMTP defaults. Port 587 is the IANA submission port (RFC 6409)
	// — the right default for any operator running through their ISP
	// or a hosted SMTP provider. TimeoutSec bounds the entire
	// connect+auth+send round-trip; 30s tolerates the operator's
	// flaky-internet baseline (per the operator-network memory).
	// Host / From / Username / Password / DefaultRecipient stay empty
	// — the operator fills them. Enabled stays false (zero value) so
	// no surprise sends happen until the operator opts in.
	if cfg.Smtp.Port == 0 {
		cfg.Smtp.Port = 587
	}
	if cfg.Smtp.TimeoutSec == 0 {
		cfg.Smtp.TimeoutSec = 30
	}

	// Bridge has no defaults to backfill — Serial.Port and Cat.Driver
	// MUST be operator-supplied when Enabled=true (no sane default is
	// hardware-correct), and every other serial parameter (baud rate,
	// data bits, parity, stop bits, line delimiter, timeouts) comes
	// from the rigdef at `internal/cat/rigs/*.json`. validateBridge
	// below enforces the Enabled=true → Port+Driver requirement.
}

// Warnings returns a slice of human-readable advisory messages about
// the loaded configuration. Distinct from validate* functions which
// return hard errors that prevent startup; warnings are non-fatal and
// surface conditions an operator probably wants to know about but
// might legitimately have chosen.
//
// Returned in load order so daemon startup can log them once after the
// logger is initialised. Empty slice means "no advisories"; never
// returns nil.
func Warnings(cfg Config) []string {
	out := make([]string, 0)
	if cfg.Server.Protocol == "tcp" && !isLoopbackBind(cfg.SocketPath) {
		out = append(out,
			"server.protocol=tcp with non-loopback socket_path "+
				strconv.Quote(cfg.SocketPath)+
				" — daemon HTTP API has no auth and will accept QSO submits "+
				"from any host that can reach this address. Trusted-LAN "+
				"deployments only; otherwise bind 127.0.0.1 or use a Unix socket.")
	}
	return out
}

// isLoopbackBind reports whether the host portion of a host:port
// string resolves to a loopback address. Empty host (":8080") is
// treated as wildcard bind (NOT loopback). Hostnames that aren't
// "localhost" are conservatively treated as non-loopback — name
// resolution at startup may not match resolution at request time, so
// we can't reliably classify them.
func isLoopbackBind(socketPath string) bool {
	host, _, err := net.SplitHostPort(socketPath)
	if err != nil {
		// SplitHostPort wants host:port; a Unix-socket path or a
		// malformed entry returns an error. Configs that reach this
		// helper have Protocol=tcp, so anything we can't parse is by
		// definition not a recognisable loopback bind.
		return false
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// validateForwarders checks the statically decidable correctness of every
// forwarder entry. Type-specific credential validation happens later when
// the forwarder package is constructed at daemon startup.
func validateForwarders(fwds []types.ForwarderConfig) error {
	names := make(map[string]struct{}, len(fwds))
	for i, fc := range fwds {
		if fc.Name == "" {
			return fmt.Errorf("forwarder[%d]: name is empty", i)
		}
		if fc.Type == "" {
			return fmt.Errorf("forwarder[%d] (%s): type is empty", i, fc.Name)
		}
		if _, dup := names[fc.Name]; dup {
			return fmt.Errorf("forwarder[%d]: duplicate name %q", i, fc.Name)
		}
		names[fc.Name] = struct{}{}

		for _, a := range fc.ActionFilter {
			if _, err := action.Parse(a); err != nil {
				return fmt.Errorf("forwarder %q: %w", fc.Name, err)
			}
		}

		if fc.TickIntervalSec < 0 {
			return fmt.Errorf("forwarder %q: tick_interval_sec must be >= 0", fc.Name)
		}
		if fc.BatchSize < 0 {
			return fmt.Errorf("forwarder %q: batch_size must be >= 0", fc.Name)
		}

		if fc.Retry != nil {
			if fc.Retry.MaxAttempts < 1 {
				return fmt.Errorf("forwarder %q: retry.max_attempts must be >= 1", fc.Name)
			}
			if fc.Retry.InitialBackoffSec < 1 {
				return fmt.Errorf("forwarder %q: retry.initial_backoff_sec must be >= 1", fc.Name)
			}
			if fc.Retry.MaxBackoffSec < fc.Retry.InitialBackoffSec {
				return fmt.Errorf("forwarder %q: retry.max_backoff_sec must be >= initial_backoff_sec", fc.Name)
			}
		}
	}
	return nil
}

// validateLookup checks the enrichment pipeline configuration per
// ADR 0017. Empty Chain is valid (no callsign-class enrichment runs).
// TTL / max-in-flight values must be non-negative; zero is valid and
// is interpreted by downstream consumers ("never stale" for TTL,
// "use the package default" for max-in-flight).
//
// Per-provider URL / UserAgent / timeout checks fire only when
// Enabled is true — the operator can have a fully-configured-but-
// disabled provider sitting in config without triggering errors.
// Type-specific credential validation (QRZ wants username/password)
// happens later when each provider is constructed at daemon startup.
func validateLookup(lc types.EnrichmentConfig) error {
	if lc.CountryTTLDays < 0 {
		return fmt.Errorf("country_ttl_days must be >= 0, got %d", lc.CountryTTLDays)
	}
	if lc.StationTTLDays < 0 {
		return fmt.Errorf("station_ttl_days must be >= 0, got %d", lc.StationTTLDays)
	}
	if lc.RefreshMaxInFlight < 0 {
		return fmt.Errorf("refresh_max_in_flight must be >= 0, got %d", lc.RefreshMaxInFlight)
	}

	if err := validateLookupProvider("hamnut", lc.Hamnut); err != nil {
		return err
	}

	names := map[string]struct{}{}
	for i, p := range lc.Chain {
		if p.Name == "" {
			return fmt.Errorf("lookup.chain[%d]: name is empty", i)
		}
		if _, dup := names[p.Name]; dup {
			return fmt.Errorf("lookup.chain[%d]: duplicate name %q", i, p.Name)
		}
		if p.Name == lc.Hamnut.Name {
			return fmt.Errorf("lookup.chain[%d]: name %q collides with hamnut", i, p.Name)
		}
		names[p.Name] = struct{}{}
		if err := validateLookupProvider(fmt.Sprintf("chain[%d] (%s)", i, p.Name), p); err != nil {
			return err
		}
	}
	return nil
}

// validateLookupProvider runs the static checks every enabled lookup
// provider must pass — URL non-empty, UserAgent non-empty, positive
// timeout. Disabled providers skip these checks so an operator can
// stash a partially-configured entry in config without breaking
// startup.
func validateLookupProvider(label string, p types.LookupConfig) error {
	if !p.Enabled {
		return nil
	}
	if p.URL == "" {
		return fmt.Errorf("lookup.%s: url is empty", label)
	}
	if p.HttpTimeoutSec <= 0 {
		return fmt.Errorf("lookup.%s: timeout_sec must be > 0", label)
	}
	// User-Agent is checked at provider Initialize time (sourced from
	// the daemon's global Config.UserAgent, not per-provider config).
	return nil
}

// validateSmtp checks the SMTP block. Enabled=false = mailer
// disabled, no further checks (the operator hasn't configured email
// yet, which is a legitimate state). When Enabled, Host + From +
// Port + TimeoutSec must all be valid — Host is the submission
// server, From is always present in the envelope so the daemon
// can't send without one, Port=0 would imply "default" but RFC has
// no universal default for SMTP submission (applyDefaults stamps
// 587 unless the operator overrode it).
func validateSmtp(s types.SmtpConfig) error {
	if !s.Enabled {
		return nil
	}
	if s.Host == "" {
		return fmt.Errorf("smtp.host is required when smtp.enabled is true")
	}
	if s.From == "" {
		return fmt.Errorf("smtp.from is required when smtp.enabled is true")
	}
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("smtp.port must be in 1..65535, got %d", s.Port)
	}
	if s.TimeoutSec <= 0 {
		return fmt.Errorf("smtp.timeout_sec must be > 0, got %d", s.TimeoutSec)
	}
	return nil
}

// validateBridge checks the bridge subsystem block. Enabled=false
// means the subsystem doesn't acquire a serial port and `/v1/rig/events`
// isn't registered — disabled config is always valid (master smd /
// headless deployments). Enabled=true means the operator wants the
// bridge running, which requires Serial.Port AND Cat.Driver to be
// non-empty — there's no sane default for either (hardware-specific).
// Surfaces missing required sub-fields loudly at startup rather than
// failing later with a SQLITE_BUSY-style obscure error.
func validateBridge(b types.BridgeConfig) error {
	// ModeMappings are validated unconditionally — operators may
	// configure mappings ahead of enabling CAT, and bad entries
	// should surface at startup regardless of whether the bridge
	// will actually run. Empty map is always fine.
	for driver, perRig := range b.ModeMappings {
		for rigStr, mm := range perRig {
			if mm.Mode == "" {
				return fmt.Errorf("bridge.mode_mappings[%s][%s]: mode must not be empty", driver, rigStr)
			}
			if !modes.IsValidMode(mm.Mode) {
				return fmt.Errorf("bridge.mode_mappings[%s][%s]: mode %q is not a known ADIF main mode", driver, rigStr, mm.Mode)
			}
			if mm.SubMode != "" && !modes.IsValidSubMode(mm.SubMode) {
				return fmt.Errorf("bridge.mode_mappings[%s][%s]: submode %q is not a known ADIF submode", driver, rigStr, mm.SubMode)
			}
		}
	}

	// Timeout overrides — validated unconditionally so a malformed
	// value surfaces at startup regardless of Enabled. All four are
	// optional (zero = use the daemon's built-in default); a positive
	// value must fall in a sane range that catches operator typos
	// (extra/missing zero) without being so strict that legitimate
	// operator tuning is rejected.
	const (
		minTimeoutMs = 50      // below this the daemon would thrash on retries
		maxTimeoutMs = 3600000 // above 1h almost certainly a typo
	)
	checkTimeout := func(label string, v int) error {
		if v == 0 {
			return nil
		}
		if v < minTimeoutMs || v > maxTimeoutMs {
			return fmt.Errorf("%s = %d: must be 0 (default) or between %d and %d milliseconds", label, v, minTimeoutMs, maxTimeoutMs)
		}
		return nil
	}
	if err := checkTimeout("bridge.timeouts.liveness_ms", b.Timeouts.LivenessMs); err != nil {
		return err
	}
	if err := checkTimeout("bridge.timeouts.backoff_initial_ms", b.Timeouts.BackoffInitialMs); err != nil {
		return err
	}
	if err := checkTimeout("bridge.timeouts.backoff_max_ms", b.Timeouts.BackoffMaxMs); err != nil {
		return err
	}
	if err := checkTimeout("bridge.timeouts.steady_state_threshold_ms", b.Timeouts.SteadyStateThresholdMs); err != nil {
		return err
	}
	if b.Timeouts.BackoffInitialMs > 0 && b.Timeouts.BackoffMaxMs > 0 && b.Timeouts.BackoffInitialMs > b.Timeouts.BackoffMaxMs {
		return fmt.Errorf("bridge.timeouts.backoff_initial_ms (%d) must not exceed backoff_max_ms (%d)", b.Timeouts.BackoffInitialMs, b.Timeouts.BackoffMaxMs)
	}

	if !b.Enabled {
		return nil
	}
	if b.Serial.Port == "" {
		return fmt.Errorf("bridge.serial.port is required when bridge.enabled is true")
	}
	if b.Cat.Driver == "" {
		return fmt.Errorf("bridge.cat.driver is required when bridge.enabled is true")
	}
	return nil
}

// Service is the runtime wrapper around Config that other services obtain
// via the iocdi container.
//
// The mu mutex serialises read/write access to Cfg and Path so that
// concurrent PUT /v1/config requests can't tear the in-memory config
// or race the on-disk file write. Callers should use Snapshot() for
// reads and Update() for writes; direct .Cfg field access remains for
// startup-time call sites that read once before any PUT can race.
type Service struct {
	workingDir  string
	Cfg         Config
	Path        string // filesystem path the daemon loaded from / writes back to
	initialized atomic.Bool
	mu          sync.RWMutex
}

// New constructs a Service with the given Config.
func New(cfg Config) *Service {
	return &Service{Cfg: cfg}
}

// SetPath records the on-disk path the config was loaded from. The
// daemon calls this once after loadConfig so subsequent Update() calls
// know where to atomically rewrite. An empty path means the config
// was never loaded from disk (in-memory DefaultConfig fallback) and
// Update() returns an error rather than guessing a path.
func (s *Service) SetPath(p string) {
	s.mu.Lock()
	s.Path = p
	s.mu.Unlock()
}

// Snapshot returns a copy of the current config under the read lock.
// Callers receive a value (not a pointer) so they can read fields
// without holding any lock. Adequate for everything except hot-loop
// reads — the daemon doesn't have any of those.
func (s *Service) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Cfg
}

// Update applies fn to a copy of the current config under the write
// lock, then atomically rewrites the on-disk config file via
// WriteJSON and replaces the in-memory Cfg only after the disk write
// succeeds. fn may return an error to abort (e.g. validation
// failure); in that case neither the in-memory state nor the file is
// touched.
//
// Pattern: the API handler reads Snapshot(), constructs the new
// value, calls Update with a closure that copies the new value into
// *cfg. Keeps the write window narrow and the mutation explicit.
func (s *Service) Update(fn func(cfg *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Path == "" {
		return fmt.Errorf("config.Service.Update: no on-disk path set; cannot persist update")
	}

	next := s.Cfg
	if err := fn(&next); err != nil {
		return err
	}

	if err := WriteJSON(s.Path, next); err != nil {
		return fmt.Errorf("config.Service.Update: writing config: %w", err)
	}

	s.Cfg = next
	return nil
}

// Initialize prepares the service for use. Resolves the working directory.
func (s *Service) Initialize() error {
	if s.initialized.Load() {
		return nil
	}

	dir, err := utils.WorkingDir(s.Cfg.DataDir)
	if err != nil {
		return fmt.Errorf("config.Service.Initialize: %w", err)
	}
	s.workingDir = dir
	s.initialized.Store(true)
	return nil
}

// Close resets the service state.
func (s *Service) Close() error {
	s.initialized.Store(false)
	return nil
}

// WorkingDir returns the resolved data directory.
func (s *Service) WorkingDir() string {
	if s.workingDir != "" {
		return s.workingDir
	}
	return s.Cfg.DataDir
}

// LoggingConfig returns the logging configuration.
func (s *Service) LoggingConfig() (types.LoggingConfig, error) {
	return s.Cfg.Logging, nil
}

// DatastoreConfig returns the sqlite datastore configuration.
func (s *Service) DatastoreConfig() (types.DatastoreConfig, error) {
	return s.Cfg.Datastore, nil
}

// Forwarders returns the configured forwarding destinations. The caller
// should not mutate the returned slice.
func (s *Service) Forwarders() []types.ForwarderConfig {
	return s.Cfg.Forwarders
}

// Enrichment returns the enrichment pipeline configuration per
// ADR 0017. Caller should not mutate the returned struct.
func (s *Service) Enrichment() types.EnrichmentConfig {
	return s.Cfg.Lookup
}

// CountryTTL returns the country-table staleness threshold as a
// time.Duration. Operator config holds the value in days; this
// accessor performs the conversion so consumers (the orchestrator)
// don't have to.
func (s *Service) CountryTTL() time.Duration {
	return time.Duration(s.Cfg.Lookup.CountryTTLDays) * 24 * time.Hour
}

// StationTTL returns the contacted_station staleness threshold.
func (s *Service) StationTTL() time.Duration {
	return time.Duration(s.Cfg.Lookup.StationTTLDays) * 24 * time.Hour
}

// RefreshMaxInFlight returns the bound on concurrent async refresh
// goroutines. Zero is interpreted by refresher.Service as "use
// package default."
func (s *Service) RefreshMaxInFlight() int {
	return s.Cfg.Lookup.RefreshMaxInFlight
}

// LookupServiceConfig returns the per-provider config keyed by
// service name. Hamnut is matched first (its Name field is stamped
// canonical at applyDefaults time); chain entries are matched by
// their Name. Returns ErrLookupConfigNotFound when no entry matches.
//
// Used by lookup-provider Initialize paths to pull their config
// when the DI container injects a *config.Service rather than
// constructing the provider with an explicit Config block.
func (s *Service) LookupServiceConfig(name string) (types.LookupConfig, error) {
	if s.Cfg.Lookup.Hamnut.Name == name {
		return s.Cfg.Lookup.Hamnut, nil
	}
	for _, p := range s.Cfg.Lookup.Chain {
		if p.Name == name {
			return p, nil
		}
	}
	return types.LookupConfig{}, ErrLookupConfigNotFound
}

// ErrLookupConfigNotFound is returned by LookupServiceConfig when
// no provider with the given name exists in operator config. Sentinel
// (rather than fmt.Errorf) so callers can branch on
// errors.Is(err, ErrLookupConfigNotFound) — providers may want to
// disable themselves cleanly rather than fail startup when the
// operator hasn't configured them.
var ErrLookupConfigNotFound = fmt.Errorf("lookup: provider config not found")
