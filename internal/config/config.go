package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

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
}

// ServerConfig holds HTTP server tunables. All timeouts are in seconds.
type ServerConfig struct {
	// Protocol is the network protocol for the listener: "unix" (default)
	// or "tcp" for network deployment.
	Protocol           string `json:"protocol"`
	ReadTimeoutSec     int    `json:"read_timeout_sec"`
	WriteTimeoutSec    int    `json:"write_timeout_sec"`
	IdleTimeoutSec     int    `json:"idle_timeout_sec"`
	ShutdownTimeoutSec int    `json:"shutdown_timeout_sec"`
	MaxBodyBytes       int64  `json:"max_body_bytes"`

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
	// Pointer-typed so applyDefaults can distinguish "absent from config"
	// (nil — apply the protocol-driven default) from "explicitly false"
	// (operator wants a headless TCP daemon). Default: true on TCP,
	// false on Unix-socket (browsers can only reach TCP listeners).
	// See docs/v2-design/frontend-spa.md.
	ServeSPA *bool `json:"serve_spa,omitempty"`
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
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(os.TempDir(), "smd.sock")
	}

	// Server defaults
	if cfg.Server.Protocol == "" {
		cfg.Server.Protocol = "unix"
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

	// Datastore defaults
	if cfg.Datastore.Driver == "" {
		cfg.Datastore.Driver = types.SqliteDriverName
	}
	if cfg.Datastore.Path == "" {
		// Sits under ${DataDir}/db/ to match the build/{bin,db,log}
		// layout used in development and any future packaged install.
		cfg.Datastore.Path = filepath.Join(cfg.DataDir, "db", "station-manager.db")
	}
	if cfg.Datastore.MaxOpenConns == 0 {
		cfg.Datastore.MaxOpenConns = 1 // sqlite is single-writer
	}
	if cfg.Datastore.MaxIdleConns == 0 {
		cfg.Datastore.MaxIdleConns = 1
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

// Service is the runtime wrapper around Config that other services obtain
// via the iocdi container.
type Service struct {
	workingDir  string
	Cfg         Config
	initialized atomic.Bool
}

// New constructs a Service with the given Config.
func New(cfg Config) *Service {
	return &Service{Cfg: cfg}
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
