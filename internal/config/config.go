package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ServiceName is the DI bean ID for the config service (used by internal/iocdi).
const ServiceName = types.ConfigServiceName

// Config is the daemon's on-disk configuration.
type Config struct {
	// Version is the config schema version (config.md §13). A file with no
	// `version` field is the pre-versioning baseline — the catalogue-era shape,
	// treated as v1. Load migrates older documents up to currentConfigVersion
	// (migrateDocument) before unmarshalling; applyDefaults stamps it so a
	// freshly-loaded or first-run config always carries the current version.
	Version int `json:"version,omitempty"`

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

	// Operators is the operator roster (ADR 0055) — the people who may sit at
	// the key. The SPA's "current operator" selection picks one, whose Callsign
	// + Name stamp a QSO's OPERATOR + MY_NAME. Config-level, not a DB table: a
	// contest group registers once and each op picks themselves rather than
	// retyping call + name. Empty on a fresh or legacy config → applyDefaults
	// seeds a single entry from the existing operator identity, so the daemon
	// always has at least one operator to stamp (backward compatibility).
	Operators []types.Operator `json:"operators,omitempty"`

	// DefaultOperator is the callsign of the roster entry used when no current
	// operator has been selected (the usual single-op case). Must match an
	// Operators[].Callsign (or be empty when the roster is empty, pre-seed).
	// applyDefaults points it at the first roster entry when unset.
	DefaultOperator string `json:"default_operator,omitempty"`

	// SetupComplete is the explicit "operator has finished first-run
	// setup" flag. Server-managed: PUT /v1/config can never set or
	// clear it directly; the daemon flips false → true on the first
	// PUT that supplies a non-empty station_callsign. SPA reads it on
	// boot to decide whether to render the setup dialog. Once true it
	// stays true — operators may change their callsign later via the
	// My Station card without re-triggering setup.
	SetupComplete bool `json:"setup_complete"`

	// DefaultLogbookID is a scalar pointer at the operator's default
	// logbook row. The SPA reads it from GET /v1/config and submits it
	// as the required ?logbook=N on QSO submit (POST /v1/qso rejects a
	// missing logbook — it is NOT defaulted server-side). Stored as a
	// scalar here because the row's display fields (name, callsign) live
	// in the DB, not in config.json — the API GET /v1/config joins them
	// at response time. Defaults to 1, so the system runs on first
	// launch without "missing config" errors; the daemon ensures a
	// logbook row with that id exists during first-run setup.
	DefaultLogbookID int64 `json:"default_logbook_id"`

	// DefaultRigID is a scalar pointer at the rig used when the SPA
	// doesn't specify which rig a QSO came from. The same UX rationale as
	// DefaultLogbookID — defaults to 1 so the system runs on first
	// launch. The rig's display fields (model, port) live in cfg.Rigs
	// (when CAT lands); the API joins them at response time.
	DefaultRigID int64 `json:"default_rig_id"`

	// RestoreRigOnModeSwitch controls whether switching the operating mode
	// (Phone/CW ↔ FT8) auto re-tunes a CAT-LIVE rig back to that mode's last
	// freq/mode: the SPA snapshots each mode's operating state on leave and
	// restores it on return. Pointer so nil = unset = the default (ON); set false
	// to disable the live re-tune. The CAT-off restore (rewriting the SPA's local
	// manualState) is unaffected — it never touches the rig. The daemon only
	// stores + serves this; the behaviour lives in the SPA (LoggingCard +
	// rigControl).
	RestoreRigOnModeSwitch *bool `json:"restore_rig_on_mode_switch,omitempty"`

	// Station holds operator-typed station-property preferences that
	// participate in QSO-emission derivations (e.g. effectivePower in
	// the SPA's displayedState). Distinct from LoggingStation, which
	// carries ADIF MY_* identity fields. Reuses types.StationConfig
	// per the canonical-DTO project idiom — same struct travels on
	// the wire (ConfigResponse.Station) and on disk (config.json).
	Station types.StationConfig `json:"station"`

	// Qsl holds the operator's standing outgoing-QSL defaults (QSL_VIA, QSLMSG,
	// QSL_SENT_VIA). Stamped onto a logged QSO when the per-QSO field is empty,
	// across all modes (SPA for Phone/CW, e4 sink for FT8). Empty fields are
	// omitted, so an unset default never adds an empty ADIF tag. Reuses
	// types.QslDefaults per the canonical-DTO idiom (same struct on wire + disk).
	Qsl types.QslDefaults `json:"qsl"`

	// PskReporter holds FT8 reception-report upload settings (opt-in, default off).
	// Read at startup and fed to the internal/pskreporter subsystem; not exposed
	// over /v1/config (set-once, like the SMTP block).
	PskReporter types.PskReporterConfig `json:"psk_reporter,omitempty"`

	// Map holds the contacts-map display settings (per-band arc colour
	// overrides). Sparse: empty means the SPA's built-in palette. The daemon
	// only stores/validates/serves this — the colours apply client-side.
	Map types.MapConfig `json:"map,omitempty"`

	// Lookup holds the enrichment pipeline configuration per ADR 0017
	// — hamnut (country source-of-truth), the callsign-class provider
	// chain, per-table staleness TTLs, and the bound on the async-
	// refresh worker. Empty / zero-valued fields fall through to the
	// defaults applied in applyDefaults; an empty Chain is valid and
	// simply disables callsign-class enrichment.
	Lookup types.EnrichmentConfig `json:"lookup"`

	// Smtp holds the operator's SMTP submission credentials used by
	// the daemon's general-purpose mailer (internal/email). enabled=false
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

	// Evidence configures local FT8 evidence capture (spot-network design
	// §4.1) — a DEFAULT-OFF consent layer (§8): no evidence.db exists until
	// the operator opts in, and disabling capture later deletes nothing.
	// The db path is not configurable this slice: evidence.db lives beside
	// the working directory's other state. See types.EvidenceConfig.
	Evidence types.EvidenceConfig `json:"evidence"`

	// Rigs is the rig catalogue (ADR 0028) — the operator's installed
	// rigs, each a reused types.RigConfig (+audio) bundling the CAT driver
	// (Model), serial Port, and audio device. DefaultRigID selects the
	// ACTIVE rig: the one the bridge binds AND the one QSOs attribute to —
	// in the single-active model these coincide, so there is no separate
	// active-rig selector. A pre-catalogue config (loose bridge.serial /
	// bridge.cat / ft8.device, no rigs) is migrated at Load into a single
	// id-1 rig, so existing configs keep working untouched. The catalogue
	// is authoritative: the active rig's values are projected onto the
	// bridge/ft8 runtime view via ActiveBridge() / ActiveFt8(), winning
	// over any stale loose field left on disk. Switching is
	// edit-default_rig_id + restart in Phase 1; runtime hot-swap is future
	// work — see docs/v2-design/rig-profiles.md.
	Rigs []types.RigConfig `json:"rigs,omitempty"`
}

// legacyRigID is the id assigned to the single rig synthesised when a
// pre-catalogue config (loose bridge/ft8 fields, no Rigs) is migrated at
// Load. Matches the DefaultRigID default of 1 so the migrated rig is active.
const legacyRigID int64 = 1

// applyRigProfiles reconciles the rig catalogue (ADR 0028). It migrates a
// pre-catalogue config into a single id-1 rig, then validates the catalogue
// and confirms DefaultRigID resolves to a defined rig. It does NOT mutate the
// loose bridge/ft8 fields — the active rig is projected on demand via
// ActiveBridge() / ActiveFt8(), so the catalogue stays the single on-disk
// source of truth (the helpers always win over any stale loose field).
// Runs after applyDefaults. For a rig-less config DefaultRigID is still 0 here
// (applyDefaults only defaults it once rigs exist); the migration below sets it
// to legacyRigID for the synthesised rig.
func applyRigProfiles(cfg *Config) error {
	if len(cfg.Rigs) == 0 {
		// Migrate: a config predating the catalogue carries the rig identity
		// in the loose fields. Fold it into one rig so every config is
		// catalogue-shaped from here on. Skipped when there's no loose
		// identity (a bridge-disabled / FT8-only-default host).
		var driver, port string
		if cfg.Bridge.Cat != nil {
			driver = cfg.Bridge.Cat.Driver
		}
		if cfg.Bridge.Serial != nil {
			port = cfg.Bridge.Serial.Port
		}
		if driver != "" || port != "" {
			cfg.Rigs = []types.RigConfig{{
				ID:    legacyRigID,
				Model: driver,
				Port:  port,
				// Audio is left unset: the per-rig model is name-based
				// (RigConfig.Audio.RX/TX) and the legacy ft8.device/tx.device
				// are integer indices that can't be converted to names at load
				// time (no device enumeration here). They remain honoured as a
				// resolved fallback via ActiveFt8 until the operator sets named
				// devices on the rig — no auto-migration (config.md §10.4 #1).
			}}
			cfg.DefaultRigID = legacyRigID // the synthesised rig is the rig
		}
		return nil
	}

	// Validation (positive/unique ids, non-empty model, default_rig_id resolves,
	// per-rig mode-mappings) moved to validateRigs in Validate (config.md §12) so
	// the rules live in the one validator. applyRigProfiles now only folds legacy
	// loose fields into a catalogue; it has no error path of its own.
	return nil
}

// migrateGlobalFt8Mode folds the legacy global ft8.tx.mode into the active rig's
// per-rig Ft8Mode override (config.md §10). The Ft8TXConfig.Mode field is retained
// only as the runtime projection target (set by ActiveFt8), no longer an operator
// knob — so any value found there is moved onto the active rig (when that rig has
// no explicit override yet) and cleared. Typed + idempotent: a no-op once the
// value lives on the rig. The bridge.mode_mappings REMOVAL uses the raw versioned
// migration instead; ft8.tx.mode and MyRig keep their Go fields (as projection /
// QSO-emission targets), so a typed fold suffices for those.
func migrateGlobalFt8Mode(cfg *Config) {
	if cfg.Ft8.TX == nil || cfg.Ft8.TX.Mode == "" {
		return
	}
	rc := cfg.RigByID(cfg.DefaultRigID)
	if rc == nil {
		return // no active rig to carry it (degenerate config); leave as-is
	}
	if rc.Ft8Mode == nil {
		mode := cfg.Ft8.TX.Mode
		rc.Ft8Mode = &mode
	}
	cfg.Ft8.TX.Mode = "" // no longer a global knob; ActiveFt8 re-projects it per-rig
}

// migrateGlobalMyRig folds the legacy config logging_station.my_rig into the
// active rig's per-rig MyRig override (config.md §10). MY_RIG is now derived at
// submit (Config.ResolveMyRig) from the active rig, not stored in the config's
// logging_station block — so any value found there is moved onto the active rig
// (when it has no override yet) and cleared. Typed + idempotent. The
// LoggingStation.MyRig Go field is retained because it's embedded in types.Qso
// for ADIF MY_RIG emission; this only clears its config-block role.
func migrateGlobalMyRig(cfg *Config) {
	if cfg.LoggingStation.MyRig == "" {
		return
	}
	global := cfg.LoggingStation.MyRig
	cfg.LoggingStation.MyRig = "" // no longer a config field; derived per-rig at submit
	rc := cfg.RigByID(cfg.DefaultRigID)
	if rc == nil {
		return
	}
	// A legacy global MY_RIG that's just the stock rigdef name of SOME catalogue
	// rig is derived per-rig anyway — don't fold it onto the active rig, which may
	// be a different model (that would stamp a wrong name, e.g. "Yaesu FT-710" onto
	// an FTdx10). Drop it; each rig derives its own name. Only a genuinely custom
	// string (matching no rigdef name) is a real override worth carrying onto the
	// active rig.
	if rigNamed(cfg.Rigs, global) {
		return
	}
	if rc.MyRig == nil {
		rc.MyRig = &global
	}
}

// rigNamed reports whether any catalogue rig's rigdef name equals name.
func rigNamed(rigs []types.RigConfig, name string) bool {
	for i := range rigs {
		if def, ok := cat.Lookup(rigs[i].Model); ok && def.Name == name {
			return true
		}
	}
	return false
}

// RigByID returns the catalogue entry with the given id, or nil if none.
func (c Config) RigByID(id int64) *types.RigConfig {
	for i := range c.Rigs {
		if c.Rigs[i].ID == id {
			return &c.Rigs[i]
		}
	}
	return nil
}

// ActiveBridge returns the BridgeConfig the bridge subsystem runs with: the
// cross-rig bridge knobs (Enabled, Timeouts, Tune, ModeMappings) with the
// active rig's driver + serial port projected on. With no resolvable active
// rig (a bridge-disabled / catalogue-less host) it returns cfg.Bridge
// unchanged. The catalogue is authoritative: a resolved active rig always
// wins over any stale loose bridge.serial / bridge.cat left on disk.
//
// RigConfig.Overrides (per-rig serial param shadowing) ARE projected — the
// active rig's Overrides are carried onto the runtime BridgeSerialConfig below
// (pinned by TestActiveBridge_ProjectsRigSerialOverrides); the bridge merges
// them over the rigdef serial defaults at open time.
func (c Config) ActiveBridge() types.BridgeConfig {
	b := c.Bridge
	// Serial/Cat are nil in the stored config (config.md §10): build fresh blocks
	// for the runtime projection so the returned view is always populated (callers
	// deref freely) and never aliases — or persists onto — the stored config. Any
	// legacy values still on disk are carried, then the active rig overlays them.
	serialCfg := types.BridgeSerialConfig{}
	catCfg := types.BridgeCatConfig{}
	if b.Serial != nil {
		serialCfg = *b.Serial
	}
	if b.Cat != nil {
		catCfg = *b.Cat
	}
	if rc := c.RigByID(c.DefaultRigID); rc != nil {
		catCfg.Driver = rc.Model
		serialCfg.Port = rc.Port
		serialCfg.Overrides = rc.Overrides // per-rig serial overrides (config.md §10, B2)
	}
	b.Serial = &serialCfg
	b.Cat = &catCfg
	return b
}

// ActiveFt8 returns the Ft8Config the FT8 subsystem runs with: the cross-rig
// FT8 knobs (Enabled, EnableOSD) with the active rig's audio device projected
// onto Device. Same authority rule as ActiveBridge.
func (c Config) ActiveFt8() types.Ft8Config {
	f := c.Ft8
	rc := c.RigByID(c.DefaultRigID)
	if rc == nil {
		return f
	}
	// The active rig's audio device NAME wins per direction WHEN SET; an unset
	// per-rig audio must NOT clobber a loose ft8.device/ft8.tx.device back to ""
	// (system default), so un-migrated configs keep working. RX → capture
	// (Device), TX → playback (TX.Device); both are resolved name→index at
	// acquire time by the audio layer.
	if rc.Audio.RX != "" {
		f.Device = rc.Audio.RX
	}

	// Resolve the per-rig FT8 mode (config.md §10, B2): the operator's per-rig
	// override wins, else the rigdef default for this rig's Model. Project it
	// onto Ft8Config.TX.Mode so the FT8 TX controller's consumer (txMode) is
	// unchanged. Copy the TX block first so we never mutate the stored config's
	// shared TX pointer.
	mode := ""
	if rc.Ft8Mode != nil {
		mode = *rc.Ft8Mode
	} else if def, ok := cat.Lookup(rc.Model); ok {
		mode = def.Ft8Mode
	}
	if mode != "" || f.TX != nil || rc.Audio.TX != "" {
		tx := types.Ft8TXConfig{}
		if f.TX != nil {
			tx = *f.TX
		}
		tx.Mode = mode
		// Project the active rig's TX (playback) device name, when set, the same
		// way RX projects onto Device above — completing the per-direction wiring.
		if rc.Audio.TX != "" {
			tx.Device = rc.Audio.TX
		}
		f.TX = &tx
	}
	return f
}

// ResolveMyRig returns the ADIF MY_RIG value for the LIVE active rig
// (DefaultRigID). See ResolveMyRigFor for the rig-pinned variant.
func (c Config) ResolveMyRig() string {
	return c.ResolveMyRigFor(c.DefaultRigID)
}

// ResolveMyRigFor returns the ADIF MY_RIG value for a SPECIFIC rig id (config.md
// §10, B2): the per-rig override if set (a non-nil value, including "" to
// suppress), else the rigdef Name for the rig's Model (e.g. "Yaesu FTdx10"); ""
// when the id resolves to no rig.
//
// qsoservice pins this to the rig the bridge connected to AT STARTUP, not the live
// DefaultRigID: a runtime "Set as default" only takes effect for the bridge on the
// next restart, so following the live default would stamp QSOs still made on the
// old (connected) rig with the new rig's identity — a misattribution window
// (codex e539a080 P1). Pinning keeps MY_RIG consistent with the rig actually on
// the air; both change together, only on restart.
func (c Config) ResolveMyRigFor(rigID int64) string {
	rc := c.RigByID(rigID)
	if rc == nil {
		return ""
	}
	if rc.MyRig != nil {
		return *rc.MyRig
	}
	if def, ok := cat.Lookup(rc.Model); ok {
		return def.Name
	}
	return ""
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

	// Migrate the raw document up to the current schema version before
	// unmarshalling (config.md §13). A newer-than-supported file is fatal here
	// (downgrade guard). Rewrite-on-migration (persisting the upgraded shape) is
	// deferred until the first real migration lands with §10.
	// Keep the file's own bytes: migrateDocument re-marshals the document when a
	// migration applies, and offsets into THAT bear no relation to the file the
	// operator is looking at.
	raw := data
	data, err = migrateDocument(data)
	if err != nil {
		// A malformed file fails here first (migrateDocument parses before it
		// migrates), so this is where nearly every hand-edit slip surfaces.
		if d := describeJSONError(path, raw, true, err); d != nil {
			return cfg, d
		}
		return cfg, fmt.Errorf("migrating config: %w", err)
	}

	if err = json.Unmarshal(data, &cfg); err != nil {
		if d := describeJSONError(path, data, bytes.Equal(data, raw), err); d != nil {
			return cfg, d
		}
		return cfg, fmt.Errorf("parsing config file: %w", err)
	}

	applyDefaults(&cfg, filepath.Dir(path))

	// Resolve the rig catalogue (ADR 0028) before validating the bridge:
	// migration may synthesise the catalogue from legacy loose fields, and
	// validateBridge runs against the ACTIVE rig's projected values.
	if err = applyRigProfiles(&cfg); err != nil {
		return cfg, fmt.Errorf("resolving rig profiles: %w", err)
	}

	// Fold the legacy global ft8.tx.mode into the active rig's per-rig override
	// (config.md §10). Typed + idempotent — the Ft8TXConfig.Mode field is kept as
	// the runtime projection target, so a typed fold is simpler than a raw
	// versioned migration (those are reserved for field REMOVALS).
	migrateGlobalFt8Mode(&cfg)
	migrateGlobalMyRig(&cfg)

	// Normalize identity fields (uppercase callsign, grid → lat/lon, trim zones)
	// before validating — the same pipeline the PUT handler runs (config.md §12).
	Normalize(&cfg)

	// Single validation entry (config.md §12): the consolidated Validate replaces
	// the per-validator calls. Any error finding is fatal at Load (clear message);
	// warnings are advisory and surfaced separately via Warnings() after the logger
	// is up. (Rig-catalogue validation still runs in applyRigProfiles above; it
	// folds into Validate with §12b.)
	for _, f := range Validate(cfg) {
		if !f.Warning {
			return cfg, fmt.Errorf("invalid config (%s): %s", f.Code, f.Message)
		}
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
	// Base default is empty; the non-sparse seed happens in applyDefaults
	// (ADR 0039), which ADDS a disabled entry per registered forwarder type
	// the operator hasn't configured. Seeding here in the base wouldn't
	// survive the operator-file overlay (a JSON array replaces, not merges),
	// so it has to be an add-missing pass after load. `enabled` now gates
	// enqueue, so a disabled seed entry queues nothing (no 3B8IDX bug class).
	cfg.Forwarders = nil
	// Hamnut country provider — disabled by default, prepopulated
	// with the canonical public endpoint. Operator flips enabled=true
	// The enrichment providers are NOT hardcoded here. applyDefaults seeds one
	// DISABLED entry per REGISTERED provider (ADR 0062), so the list is
	// non-sparse and a provider compiled into the daemon appears in Settings
	// ready to be given credentials — the same add-missing shape forwarders use
	// (ADR 0039). Seeding by name here would reintroduce the hardcoding the
	// registry exists to remove, and would list QRZ even in a build without it.
	applyDefaults(&cfg, dataDir)
	return cfg
}

// WriteJSON serialises cfg to path as indented JSON via temp-file +
// rename so a partial write can never leave a half-formed config on
// disk for the next startup. The parent directory must already exist;
// daemon startup resolves that elsewhere via utils.WorkingDir.
//
// The file is written 0600 (owner read/write only): config.json holds
// plaintext secrets — SMTP credentials, lookup-provider passwords, forwarder
// API keys — that are deliberately kept off the /v1/config API, so the on-disk
// copy must not be world/group-readable on a multi-user host either (review
// 2026-06-19 M1). An existing file that's already stricter (e.g. 0400) is
// preserved; a wider one (a legacy 0644) is tightened on the next write.
func WriteJSON(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	mode := os.FileMode(0o600)
	if fi, statErr := os.Stat(path); statErr == nil {
		// Preserve an operator-tightened mode (a subset of owner-rw, e.g. 0400);
		// never loosen, and tighten anything wider down to 0600.
		if perm := fi.Mode().Perm(); perm&^0o600 == 0 && perm != 0 {
			mode = perm
		}
	}

	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("writing temp config: %w", err)
	}
	// WriteFile applies the mode through umask on create; force it exactly so a
	// permissive umask can't leave the secret-bearing file group/world-readable.
	if err = os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("setting temp config mode: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp config to %s: %w", path, err)
	}
	return nil
}

// Normalize applies the operator-identity transforms shared by Load and the PUT
// handler (config.md §12): uppercase + trim the station callsign, normalise the
// Maidenhead grid and derive MY_LAT/MY_LON from it (cleared when the grid is
// empty), trim the zone / DXCC strings, and drop per-rig overrides that merely
// restate the rigdef default. Pure transform — validation is a separate step
// (Validate). Run before Validate so both paths see one shape.
func Normalize(cfg *Config) {
	ls := &cfg.LoggingStation
	ls.StationCallsign = strings.ToUpper(strings.TrimSpace(ls.StationCallsign))
	// Operator and OwnerCallsign are callsign identity fields too (the SPA
	// validates all three; both can be emitted into logged ADIF as OPERATOR /
	// OWNER_CALLSIGN, and the FT8 path falls back to Operator when
	// StationCallsign is empty) — canonicalise them the same way.
	ls.Operator = strings.ToUpper(strings.TrimSpace(ls.Operator))
	ls.OwnerCallsign = strings.ToUpper(strings.TrimSpace(ls.OwnerCallsign))
	ls.MyGridsquare = utils.NormalizeMaidenhead(ls.MyGridsquare)
	// The grid SUGGESTS a position; it does not impose one (operator's ruling,
	// 2026-08-04). Derived only when none is held, and in DECIMAL degrees — the
	// canonical internal form everything else already speaks. This field used to
	// be re-derived unconditionally in ADIF Location form, which made it the one
	// place storing a wire format AND destroyed any value the operator set, at
	// every startup. Their own station is the one position we could know exactly,
	// and a 6-character locator is a ~5x10 km cell.
	//
	// A position that CONTRADICTS the grid is not corrected here: a human typed
	// it and is present to be told, so validate.go refuses it by name. Silently
	// relocating an operator to their own cell centre would ignore their input
	// and explain nothing.
	// MIGRATION, and it is load-bearing: every config.json written before
	// 2026-08-04 holds MY_LAT in ADIF Location form ("S011 26.250"). Converting
	// it here is what stops the new validation refusing an existing install at
	// startup — leaving it would turn a format change into a daemon that will
	// not boot, which is the shape of the QRZ-password defect found the same day.
	// It also absorbs a client that still PUTs the wire form.
	if dec, err := utils.ConvertFromXDDDMMM(ls.MyLat, true); err == nil {
		ls.MyLat = dec
	}
	if dec, err := utils.ConvertFromXDDDMMM(ls.MyLon, false); err == nil {
		ls.MyLon = dec
	}
	if ls.MyLat == "" && ls.MyLon == "" && ls.MyGridsquare != "" {
		if lat, lon, ok := utils.MaidenheadToDecimal(ls.MyGridsquare); ok {
			ls.MyLat = strconv.FormatFloat(lat, 'f', 6, 64)
			ls.MyLon = strconv.FormatFloat(lon, 'f', 6, 64)
		}
	}
	ls.MyCqZone = strings.TrimSpace(ls.MyCqZone)
	ls.MyITUZone = strings.TrimSpace(ls.MyITUZone)
	ls.MyDXCC = strings.TrimSpace(ls.MyDXCC)

	// Operator roster (ADR 0055): canonicalise each roster callsign + the
	// default pointer like the identity fields above, so validation and the
	// current-operator match are case- and space-insensitive.
	for i := range cfg.Operators {
		cfg.Operators[i].Callsign = strings.ToUpper(strings.TrimSpace(cfg.Operators[i].Callsign))
		cfg.Operators[i].Name = strings.TrimSpace(cfg.Operators[i].Name)
	}
	cfg.DefaultOperator = strings.ToUpper(strings.TrimSpace(cfg.DefaultOperator))

	normalizeRigOverrides(cfg)

	// Drop vestigial empty loose serial/cat blocks (config.md §10): serial port +
	// CAT driver are per-rig now (RigConfig.Port + Model), so an empty stored block
	// is noise — and a non-nil pointer to a zero struct re-serializes as `{}`, which
	// omitempty can't drop, so nil it explicitly. Runs after applyRigProfiles, so a
	// legacy block that still carried a value has already been folded into the
	// catalogue; only genuinely-empty leftovers are cleared here.
	if cfg.Bridge.Serial != nil && cfg.Bridge.Serial.Port == "" {
		cfg.Bridge.Serial = nil
	}
	if cfg.Bridge.Cat != nil && cfg.Bridge.Cat.Driver == "" {
		cfg.Bridge.Cat = nil
	}

	normalizeLookupURLs(cfg)
	normalizeLookupTTLs(&cfg.Lookup)
	normalizeLookupPolicy(&cfg.Lookup)
	normalizeSmtpDefaults(&cfg.Smtp)
}

// normalizeLookupTTLs fills the two cache TTLs when the operator did not supply
// them. ONLY nil is filled — an explicit 0 is the operator saying "trust this
// cache indefinitely" (Orchestrator.isStale reads a non-positive TTL that way)
// and overwriting it is the defect this replaced: applyDefaults used to stamp
// 365/90 over any zero, so a deliberate 0 worked until the next restart and then
// silently became a year.
//
// In Normalize for the same reason as normalizeSmtpDefaults: applyDefaults runs
// only on Load, and a PUT that omits a TTL must still end up with a definite
// value before Validate and before the accessors read it.
func normalizeLookupTTLs(lc *types.EnrichmentConfig) {
	if lc.CountryTTLDays == nil {
		d := defaultCountryTTLDays
		lc.CountryTTLDays = &d
	}
	if lc.StationTTLDays == nil {
		d := defaultStationTTLDays
		lc.StationTTLDays = &d
	}
}

// normalizeLookupPolicy gives legacy chains explicit ADR 0068 priorities and
// makes numeric priority, rather than JSON array position, authoritative. An
// all-zero chain is the legacy shape and migrates in-place. A mixed chain is
// left intact so validation can reject it rather than guess the operator's
// intended authority order.
func normalizeLookupPolicy(lc *types.EnrichmentConfig) {
	if lc.ContinueIfBlank == nil {
		lc.ContinueIfBlank = lookupdef.DefaultCompletionFields()
	}
	if len(lc.Chain) == 0 {
		return
	}

	allMissing := true
	for _, p := range lc.Chain {
		if p.Priority != 0 {
			allMissing = false
			break
		}
	}
	if allMissing {
		for i := range lc.Chain {
			lc.Chain[i].Priority = i + 1
		}
	}

	for _, p := range lc.Chain {
		if p.Priority <= 0 {
			return // mixed/invalid: retain original positions for a precise error
		}
	}
	sort.SliceStable(lc.Chain, func(i, j int) bool {
		return lc.Chain[i].Priority < lc.Chain[j].Priority
	})
}

// normalizeSmtpDefaults stamps the two SMTP numbers that HAVE a sane default
// when the operator left them blank. Port 587 is the IANA submission port (RFC
// 6409) — right for any operator going through their ISP or a hosted provider;
// TimeoutSec bounds the whole connect+auth+send round-trip, and 30s tolerates a
// flaky link.
//
// In Normalize (not applyDefaults) for the same reason as normalizeLookupURLs
// above: applyDefaults runs only on Load, so a blank arriving over PUT was stored
// as 0 and silently became 587 at the NEXT daemon start — and on an enabled block
// it never got that far, because validateSmtp rejects port 0 with a 400 telling
// the operator to supply a number the daemon already knows. Running here puts the
// resolved value in front of Validate on both paths, and in the PUT response, so
// what the operator sees after saving is what was stored.
//
// Deliberately narrow: host / from / username / password have no default that
// could be correct, so they stay empty and validateSmtp goes on refusing an
// enabled block without them. Idempotent; an operator value is never overwritten.
func normalizeSmtpDefaults(s *types.SmtpConfig) {
	if s.Port == 0 {
		s.Port = defaultSmtpPort
	}
	if s.TimeoutSec == 0 {
		s.TimeoutSec = defaultSmtpTimeoutSec
	}
}

// normalizeLookupURLs stamps the canonical public endpoint for the known
// providers when the operator left the URL empty — so the config SPA's
// Enrichment tab stays frictionless (credentials only, never a URL). In
// Normalize (not applyDefaults) so it runs on PUT too: a newly-enabled QRZ entry
// with no URL gets the default before validateLookup's "enabled provider needs a
// URL" check. Idempotent; a non-empty operator URL is left untouched.
func normalizeLookupURLs(cfg *Config) {
	normalizeLookupProvider(&cfg.Lookup.Hamnut)
	for i := range cfg.Lookup.Chain {
		normalizeLookupProvider(&cfg.Lookup.Chain[i])
	}
}

// normalizeLookupProvider fills one provider's endpoint defaults from its
// REGISTERED DESCRIPTOR (ADR 0062) — the provider declares them next to its own
// implementation, so adding a provider needs no edit here.
//
// An UNREGISTERED provider keeps whatever the operator wrote. That is the
// important case and it has two sources: a config naming a provider from a
// newer build, and internal/config's own unit tests, which cannot import a
// provider package (internal/lookup/qrz imports internal/config) and therefore
// always see an empty registry. Inventing defaults for an unknown provider
// would be guessing; refusing to load it would strand the operator on a daemon
// that will not start.
//
// The generic timeout fallback stays unconditional: "some positive timeout" is
// safe for any provider, and validateLookupProvider requires one when enabled.
// seedRegisteredLookupProviders makes the provider list NON-SPARSE: every
// provider compiled into this daemon gets an entry, DISABLED, carrying its own
// declared defaults. Same add-missing shape as forwarders (ADR 0039), and the
// reason enabling a source is a toggle in Settings rather than a config.json
// edit.
//
// Without it the registry defeated its own purpose (clean-room review
// 83d595f88838): a newly registered provider appeared in GET /v1/lookup-types
// but in no config block, so the Settings section had no row for it. "Adding a
// provider is a package plus an import" was only true if the operator also
// hand-wrote the JSON.
//
// ADD-MISSING, never overwrite: an entry the operator already has keeps its
// settings, and re-running is a no-op. applyDefaults runs on every Load and the
// daemon rewrites config.json, so an unconditional append would add a duplicate
// per restart until validateLookup's duplicate-name check refused to start.
//
// A no-op on an empty registry — the normal state in this package's own tests,
// which cannot import a provider package, and in a build compiled without any.
func seedRegisteredLookupProviders(lc *types.EnrichmentConfig) {
	have := make(map[string]struct{}, len(lc.Chain))
	nextPriority := 1
	for _, c := range lc.Chain {
		have[c.Name] = struct{}{}
		if c.Priority >= nextPriority {
			nextPriority = c.Priority + 1
		}
	}
	for _, d := range lookupdef.Descriptors() {
		seed := types.LookupConfig{
			Name:           d.Name,
			Enabled:        false,
			URL:            d.DefaultURL,
			ViewURL:        d.DefaultViewURL,
			HttpTimeoutSec: d.DefaultTimeoutSec,
		}
		switch d.Kind {
		case lookupdef.KindCountry:
			// Exactly one country slot (EnrichmentConfig.Hamnut), filled only
			// when the operator has not named a provider there. There can be
			// only one country descriptor — lookupdef.RegisterProvider refuses a
			// second, because config could not represent it and it would drop
			// silently here (review a47cccfb3b93).
			//
			// FIELD-WISE, never a wholesale replace: an operator can leave the
			// name out while still setting a URL (the old canonical-name stamp
			// existed for exactly that config shape), and assigning the seed
			// over the block threw their URL away.
			if lc.Hamnut.Name == "" {
				lc.Hamnut.Name = d.Name
			}
			if lc.Hamnut.Name == d.Name {
				if lc.Hamnut.URL == "" {
					lc.Hamnut.URL = d.DefaultURL
				}
				if lc.Hamnut.ViewURL == "" {
					lc.Hamnut.ViewURL = d.DefaultViewURL
				}
				if lc.Hamnut.HttpTimeoutSec == 0 {
					lc.Hamnut.HttpTimeoutSec = d.DefaultTimeoutSec
				}
			}
		case lookupdef.KindCallsign:
			if _, present := have[d.Name]; !present {
				seed.Priority = nextPriority
				nextPriority++
				lc.Chain = append(lc.Chain, seed)
				have[d.Name] = struct{}{}
			}
		}
	}
}

func normalizeLookupProvider(c *types.LookupConfig) {
	if d, ok := lookupdef.Descriptor(c.Name); ok {
		if c.URL == "" {
			c.URL = d.DefaultURL
		}
		if c.ViewURL == "" {
			c.ViewURL = d.DefaultViewURL
		}
		if c.HttpTimeoutSec == 0 && d.DefaultTimeoutSec > 0 {
			c.HttpTimeoutSec = d.DefaultTimeoutSec
		}
	}
	// Per-provider timeout default — here (Normalize, both Load + PUT) rather
	// than applyDefaults so an enabled provider added via the config SPA passes
	// validateLookupProvider's "timeout_sec > 0 when enabled" check.
	if c.HttpTimeoutSec == 0 {
		c.HttpTimeoutSec = defaultLookupHTTPTimeoutSec
	}
}

// normalizeRigOverrides nils out per-rig Ft8Mode / MyRig overrides that merely
// repeat the rig's own rigdef default (config.md §10): a nil override derives
// from the rigdef, so "nil" and "== default" produce the same runtime value —
// storing the redundant copy is noise and freezes the value against a future
// rigdef-default change. Keeps the persisted config to "only what differs from
// the rig's defaults" (the operator's pointer-omit-when-default model). An
// override that genuinely differs, and the explicit "" MyRig suppress, are left
// untouched; an unknown model (no rigdef) is left as-is.
func normalizeRigOverrides(cfg *Config) {
	for i := range cfg.Rigs {
		rc := &cfg.Rigs[i]
		def, ok := cat.Lookup(rc.Model)
		if !ok {
			continue
		}
		if rc.Ft8Mode != nil && *rc.Ft8Mode == def.Ft8Mode {
			rc.Ft8Mode = nil
		}
		if rc.MyRig != nil && *rc.MyRig == def.Name {
			rc.MyRig = nil
		}
	}
}

// Config default-fill values + the defaults discoverability index live in
// defaults.go; safety ceilings are enforced in their owning subsystems (see
// defaults.go for the index). config.md §14.

// SeedOperatorRoster fills an EMPTY operator roster (ADR 0055) with a single
// entry from the station identity — OPERATOR, falling back to STATION_CALLSIGN —
// plus MY_NAME, and points default_operator at the first entry when unset.
// Idempotent: a non-empty roster and an already-set default are left alone.
//
// Called from applyDefaults at Load AND from first-run setup completion
// (PUT /v1/config), because PUT does not re-run applyDefaults — without the
// setup-time call the roster would stay empty until the next daemon restart
// (codex review of 23d2df7a, #3).
func SeedOperatorRoster(cfg *Config) {
	if len(cfg.Operators) == 0 {
		seed := strings.TrimSpace(cfg.LoggingStation.Operator)
		if seed == "" {
			seed = strings.TrimSpace(cfg.LoggingStation.StationCallsign)
		}
		if seed != "" {
			cfg.Operators = []types.Operator{{
				Callsign: seed,
				Name:     strings.TrimSpace(cfg.LoggingStation.MyName),
			}}
		}
	}
	if cfg.DefaultOperator == "" && len(cfg.Operators) > 0 {
		cfg.DefaultOperator = cfg.Operators[0].Callsign
	}
}

func applyDefaults(cfg *Config, baseDir string) {
	// Stamp the current schema version so a freshly-loaded (version-less) or
	// first-run config always carries it; migrateDocument has already brought
	// any older document up to currentConfigVersion by this point (config.md §13).
	if cfg.Version == 0 {
		cfg.Version = currentConfigVersion
	}

	if cfg.DataDir == "" {
		cfg.DataDir = baseDir
	}

	// Evidence capture: only the cap defaults — Capture itself is a consent
	// layer and stays exactly as the operator left it (default off).
	if cfg.Evidence.CapBytes == 0 {
		cfg.Evidence.CapBytes = defaultEvidenceCapBytes
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
		cfg.Server.ReadHeaderTimeoutSec = defaultReadHeaderTimeoutSec
	}
	if cfg.Server.ReadTimeoutSec == 0 {
		cfg.Server.ReadTimeoutSec = defaultReadTimeoutSec
	}
	if cfg.Server.WriteTimeoutSec == 0 {
		cfg.Server.WriteTimeoutSec = defaultWriteTimeoutSec
	}
	if cfg.Server.IdleTimeoutSec == 0 {
		cfg.Server.IdleTimeoutSec = defaultIdleTimeoutSec
	}
	if cfg.Server.ShutdownTimeoutSec == 0 {
		cfg.Server.ShutdownTimeoutSec = defaultShutdownTimeoutSec
	}
	if cfg.Server.MaxBodyBytes == 0 {
		cfg.Server.MaxBodyBytes = defaultMaxBodyBytes
	}
	if cfg.Server.DefaultPageLimit == 0 {
		cfg.Server.DefaultPageLimit = defaultPageLimit
	}
	if cfg.Server.MaxPageLimit == 0 {
		cfg.Server.MaxPageLimit = defaultMaxPageLimit
	}
	if cfg.Server.MaxConcurrentRequests == 0 {
		cfg.Server.MaxConcurrentRequests = defaultMaxConcurrentRequests
	}
	if cfg.Server.MaxEventSubscribers == 0 {
		cfg.Server.MaxEventSubscribers = defaultMaxEventSubscribers
	}
	if cfg.Server.SubmitRatePerSec == 0 {
		cfg.Server.SubmitRatePerSec = defaultSubmitRatePerSec
	}
	if cfg.Server.SubmitRateBurst == 0 {
		cfg.Server.SubmitRateBurst = defaultSubmitRateBurst
	}
	if cfg.Server.MaxContactHistoryResults == 0 {
		cfg.Server.MaxContactHistoryResults = defaultMaxContactHistoryResults
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
	// NB: ft8.tx.inhibit_idle is deliberately NOT defaulted here. ActiveFt8() can
	// leave the whole TX block nil, and there is nowhere to write a default into a
	// block that does not exist — so the default lives in
	// types.ResolveFt8InhibitIdle, which every reader goes through.

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
		cfg.Datastore.MaxOpenConns = defaultMaxOpenConns
	}
	if cfg.Datastore.MaxIdleConns == 0 {
		cfg.Datastore.MaxIdleConns = defaultMaxIdleConns
	}
	if cfg.Datastore.ContextTimeout == 0 {
		cfg.Datastore.ContextTimeout = defaultContextTimeoutSec
	}
	if cfg.Datastore.TransactionContextTimeout == 0 {
		cfg.Datastore.TransactionContextTimeout = defaultTxContextTimeoutSec
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
		cfg.Logging.LogFileMaxSizeMB = defaultLogFileMaxSizeMB
	}
	if cfg.Logging.LogFileMaxBackups == 0 {
		cfg.Logging.LogFileMaxBackups = defaultLogFileMaxBackups
	}
	if cfg.Logging.LogFileMaxAgeDays == 0 {
		cfg.Logging.LogFileMaxAgeDays = defaultLogFileMaxAgeDays
	}

	// Station defaults. Amp disabled with a 1.0 multiplier so an
	// unset block reads as "no amp" and behaves identically to
	// pre-Station-block deployments. Operator opts in via the My
	// Station panel's QSO sub-tab.
	if cfg.Station.AmpMultiplier == 0 {
		cfg.Station.AmpMultiplier = defaultAmpMultiplier
	}

	// Default-pointer defaults. The default logbook is always 1 — the
	// first-run logbook row is seeded to id=1 by the /v1/config setup
	// transition, so the pointer is valid even before setup completes.
	// The default rig, by contrast, is only set when a rig catalogue
	// exists: a rig-less fresh install must leave DefaultRigID at 0
	// ("no active rig"), never dangle it at a non-existent rig 1 — that
	// left a fresh install with a default_rig_id matching no rig (and the
	// Phone/CW panel with no rig to display). The pre-catalogue migration
	// path sets it explicitly in applyRigProfiles after synthesising the
	// id-1 rig.
	if cfg.DefaultLogbookID == 0 {
		cfg.DefaultLogbookID = defaultLogbookID
	}
	if cfg.DefaultRigID == 0 && len(cfg.Rigs) > 0 {
		cfg.DefaultRigID = defaultRigID
	}

	SeedOperatorRoster(cfg)

	// Non-sparse seeding (ADR 0039): ensure every registered forwarder TYPE has
	// a config entry. For each registered default the operator hasn't already
	// configured (matched by type — one instance per type, per the
	// ham-services-singleton model), append a disabled seed. The loop below
	// then fills its tick/batch/endpoint defaults. This makes the config
	// non-sparse so the operator toggles `enabled` + supplies credentials
	// rather than hand-adding entries. With an empty registry (config unit
	// tests that don't import the forwarder packages) this is a no-op.
	haveType := map[string]bool{}
	for _, fc := range cfg.Forwarders {
		haveType[fc.Type] = true
	}
	for _, seed := range forwarding.DefaultForwarderConfigs() {
		if !haveType[seed.Type] {
			cfg.Forwarders = append(cfg.Forwarders, seed)
		}
	}

	// Forwarder defaults — see docs/v2-design/forwarding.md §4.
	// Zero-valued tunables pick up the operator-environment defaults; a
	// nil Retry stays nil so the forwarder package supplies its own
	// type-specific retry numbers.
	for i := range cfg.Forwarders {
		fc := &cfg.Forwarders[i]
		tickDefault := defaultForwarderTickIntervalSec
		batchDefault := defaultForwarderBatchSize
		if defaults, ok := forwarding.WorkerDefaultsFor(fc.Type); ok {
			tickDefault = defaults.TickIntervalSec
			batchDefault = defaults.BatchSize
		}
		if fc.TickIntervalSec == 0 {
			fc.TickIntervalSec = tickDefault
		}
		if fc.BatchSize == 0 {
			fc.BatchSize = batchDefault
		}
		// Seed the per-type default endpoints when the operator set none, so the
		// URLs are visible + overridable in config.json (ADR 0039). A
		// partially-set map is left as-is — the forwarder constructor falls back
		// to its package const for any missing key.
		if len(fc.Endpoints) == 0 {
			if eps, ok := forwarding.DefaultEndpointsFor(fc.Type); ok {
				fc.Endpoints = eps
			}
		}
		if len(fc.ActionFilter) == 0 {
			// Default an omitted filter to the forwarder's SUPPORTED action
			// set when it registered one (e.g. ClubLog = insert/delete only),
			// so a natural config never queues rows the forwarder must reject.
			// Forwarders that register nothing keep the historical all-three
			// default.
			if supported, ok := forwarding.SupportedActionsFor(fc.Type); ok {
				fc.ActionFilter = actionsToStrings(supported)
			} else {
				fc.ActionFilter = []string{
					string(action.Insert),
					string(action.Update),
					string(action.Delete),
				}
			}
		}
	}

	// Enrichment pipeline defaults (ADR 0017). Per-table TTLs differ
	// because DXCC entities turn over rarely (long country TTL) but
	// operator licenses / addresses change more often (shorter
	// contacted_station TTL). RefreshMaxInFlight matches
	// refresher.DefaultMaxInFlight — operator can tune via config.
	// TTL defaults live in Normalize (normalizeLookupTTLs) so they apply on PUT
	// too, not just Load — otherwise a save that omits a TTL stores nil and the
	// accessors have to invent a meaning for it. Called here as well because
	// DefaultConfig runs applyDefaults without Normalize.
	normalizeLookupTTLs(&cfg.Lookup)
	// Migrate the existing array order before registry seeding. Adding an
	// explicitly-prioritised seed first would turn a legacy all-zero chain into
	// a mixed configuration that must (correctly) be rejected.
	normalizeLookupPolicy(&cfg.Lookup)
	if cfg.Lookup.RefreshMaxInFlight == 0 {
		cfg.Lookup.RefreshMaxInFlight = defaultRefreshMaxInFlight
	}
	seedRegisteredLookupProviders(&cfg.Lookup)
	normalizeLookupPolicy(&cfg.Lookup)

	// SMTP port + timeout defaults live in Normalize (normalizeSmtpDefaults) so
	// they apply on PUT too, not just Load — a blank port saved from the Settings
	// Email section needs the default stamped before validateSmtp runs. Host /
	// From / Username / Password / DefaultRecipient stay empty — the operator
	// fills them. Enabled stays false (zero value) so no surprise sends happen
	// until the operator opts in.
	normalizeSmtpDefaults(&cfg.Smtp)

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

// UnknownKeys returns the dotted paths of JSON keys present in the raw config
// document but NOT recognised by the Config schema (review 2026-06-19 L1). Load
// deliberately ignores unknown keys (forward-compatibility — an old daemon must
// still load a config a newer one wrote), but that means a hand-editing
// operator's typo — "bridge.enable", "smtp.timeot_sec", "server.max_body_byte"
// — silently falls back to the default. The daemon logs these at startup so the
// mistake is visible without making one typo fatal. Advisory only.
//
// It runs the same migrateDocument as Load first, so a key a migration renames
// or removes isn't falsely flagged. Recursion descends struct / *struct blocks
// (where the review's nested examples live); slice elements and map values are
// treated as opaque (a map's keys are data, not schema). Returns nil for a
// document that doesn't parse as a JSON object — Load surfaces real parse errors.
func UnknownKeys(data []byte) []string {
	migrated, err := migrateDocument(data)
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(migrated, &raw) != nil {
		return nil
	}
	var out []string
	collectUnknownKeys("", raw, reflect.TypeOf(Config{}), &out)
	sort.Strings(out)
	return out
}

// collectUnknownKeys diffs the raw JSON object against the json-tagged fields of
// struct type t, appending dotted paths of unrecognised keys to out and
// recursing into struct-typed fields whose value is itself a JSON object.
func collectUnknownKeys(prefix string, raw map[string]json.RawMessage, t reflect.Type, out *[]string) {
	known := jsonFieldsOf(t)
	for k, v := range raw {
		ft, ok := known[k]
		if !ok {
			*out = append(*out, prefix+k)
			continue
		}
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Struct {
			continue // scalars, slices, and maps are leaves for this check
		}
		var child map[string]json.RawMessage
		if json.Unmarshal(v, &child) == nil {
			// Value is a JSON object → descend. A non-object value (e.g. a
			// time.Time serialised as a string) fails this and is left alone.
			collectUnknownKeys(prefix+k+".", child, ft, out)
		}
	}
}

// jsonFieldsOf maps the JSON key → field type for every exported, encodable
// field of struct type t, honouring `json:"name,..."` tags, `json:"-"` skips,
// and anonymous-field promotion (an embedded struct with no json name promotes
// its fields to this level — matching encoding/json).
func jsonFieldsOf(t reflect.Type) map[string]reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	out := map[string]reflect.Type{}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // unexported, non-embedded
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" && f.Anonymous {
			for k, ft := range jsonFieldsOf(f.Type) {
				out[k] = ft
			}
			continue
		}
		if name == "" {
			name = f.Name // encoding/json default: the field name itself
		}
		out[name] = f.Type
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
// actionsToStrings converts a forwarder's supported-action set (as
// returned by forwarding.SupportedActionsFor) into the string form used
// by ForwarderConfig.ActionFilter.
func actionsToStrings(as []action.Action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.String()
	}
	return out
}

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
		// Reject an explicit action the forwarder type can't perform (e.g.
		// "update" on ClubLog) so the operator learns at load time, not via
		// a stream of failed upload rows. Only enforced when the type
		// registered a supported set; an unregistered type isn't gated.
		if supported, ok := forwarding.SupportedActionsFor(fc.Type); ok {
			allowed := actionsToStrings(supported)
			for _, a := range fc.ActionFilter {
				if !slices.Contains(allowed, a) {
					return fmt.Errorf(
						"forwarder %q: type %q does not support action %q (supports %v)",
						fc.Name, fc.Type, a, allowed,
					)
				}
			}
		}

		// Upper bounds catch a hand-edit typo (an extra zero) before it becomes a
		// self-DoS or — for the backoff seconds — a `secs * time.Second` int64
		// overflow that wraps to a non-positive duration and silently COLLAPSES
		// the configured backoff (review 2026-06-19 L1). The ceilings are far
		// above any sane operator value, so real tuning is never rejected.
		const (
			maxForwarderTickSec       = 86400 // 1 day — a poll slower than this is a typo
			maxForwarderBatchSize     = 1000  // far above the default 5
			maxForwarderRetryAttempts = 100   // beyond this, retrying is pointless
			maxForwarderBackoffSec    = 86400 // 1 day; also keeps secs*time.Second well inside int64
		)
		if fc.TickIntervalSec < 0 || fc.TickIntervalSec > maxForwarderTickSec {
			return fmt.Errorf("forwarder %q: tick_interval_sec must be in 0..%d", fc.Name, maxForwarderTickSec)
		}
		if fc.BatchSize < 0 || fc.BatchSize > maxForwarderBatchSize {
			return fmt.Errorf("forwarder %q: batch_size must be in 0..%d", fc.Name, maxForwarderBatchSize)
		}

		if fc.Retry != nil {
			if fc.Retry.MaxAttempts < 1 || fc.Retry.MaxAttempts > maxForwarderRetryAttempts {
				return fmt.Errorf("forwarder %q: retry.max_attempts must be in 1..%d", fc.Name, maxForwarderRetryAttempts)
			}
			if fc.Retry.InitialBackoffSec < 1 || fc.Retry.InitialBackoffSec > maxForwarderBackoffSec {
				return fmt.Errorf("forwarder %q: retry.initial_backoff_sec must be in 1..%d", fc.Name, maxForwarderBackoffSec)
			}
			if fc.Retry.MaxBackoffSec > maxForwarderBackoffSec {
				return fmt.Errorf("forwarder %q: retry.max_backoff_sec must be <= %d", fc.Name, maxForwarderBackoffSec)
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
	// Direct callers get the same legacy all-zero migration as Load/PUT. The
	// copy is intentional: validation does not mutate its input.
	lc.Chain = slices.Clone(lc.Chain)
	lc.ContinueIfBlank = slices.Clone(lc.ContinueIfBlank)
	normalizeLookupPolicy(&lc)

	// nil is "use the default", not a value to range-check. An explicit 0 is a
	// legitimate instruction ("never goes stale"); only a negative is a typo.
	if lc.CountryTTLDays != nil && *lc.CountryTTLDays < 0 {
		return fmt.Errorf("country_ttl_days must be >= 0, got %d", *lc.CountryTTLDays)
	}
	if lc.StationTTLDays != nil && *lc.StationTTLDays < 0 {
		return fmt.Errorf("station_ttl_days must be >= 0, got %d", *lc.StationTTLDays)
	}
	if lc.RefreshMaxInFlight < 0 {
		return fmt.Errorf("refresh_max_in_flight must be >= 0, got %d", lc.RefreshMaxInFlight)
	}

	if err := validateLookupProvider("hamnut", lc.Hamnut); err != nil {
		return err
	}

	completion := map[string]struct{}{}
	for i, field := range lc.ContinueIfBlank {
		if !lookupdef.IsCompletionField(field) {
			return fmt.Errorf("continue_if_blank[%d]: unknown callsign field %q", i, field)
		}
		if _, dup := completion[field]; dup {
			return fmt.Errorf("continue_if_blank[%d]: duplicate callsign field %q", i, field)
		}
		completion[field] = struct{}{}
	}

	names := map[string]struct{}{}
	priorities := map[int]struct{}{}
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
		if p.Priority <= 0 {
			return fmt.Errorf("lookup.chain[%d] (%s): priority must be greater than zero", i, p.Name)
		}
		if _, dup := priorities[p.Priority]; dup {
			return fmt.Errorf("lookup.chain[%d] (%s): duplicate priority %d", i, p.Name, p.Priority)
		}
		priorities[p.Priority] = struct{}{}
		if err := validateLookupProvider(fmt.Sprintf("chain[%d] (%s)", i, p.Name), p); err != nil {
			return err
		}
	}
	for priority := 1; priority <= len(lc.Chain); priority++ {
		if _, ok := priorities[priority]; !ok {
			return fmt.Errorf("lookup.chain: priority %d is missing (priorities must be contiguous 1..%d)",
				priority, len(lc.Chain))
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
	// A credentialed provider (QRZ) carries username/password in the request
	// URL, so it must not send them over plain http to a remote host — require
	// https (http allowed only for loopback, for local mocks). Review 2026-06-19 M2.
	if (p.Username != "" || p.Password != "") && !lookupTransportSecure(p.URL) {
		return fmt.Errorf("lookup.%s: url must use https when credentials are set (http allowed only for loopback)", label)
	}
	// An ENABLED provider that cannot authenticate must be refused HERE, at the
	// save, rather than at the next daemon start. QRZ's own Initialize enforces
	// these same limits, and a failure there aborts buildEnrichment and with it
	// the whole daemon — so a 200 on this PUT would be a station that does not
	// come back up, with nothing to connect it to the settings change that did
	// it. Reachable in one click since the Settings → Enrichment "Remove stored
	// password" control shipped (clean-room review 9732ab7914af).
	// The requirement comes from the provider's own descriptor (ADR 0062), so a
	// new provider brings its rule with it. An UNREGISTERED provider is not
	// assumed to need a login — guessing would refuse to save a config the next
	// build supports, and internal/config's tests always see an empty registry.
	if d, ok := lookupdef.Descriptor(p.Name); ok && d.NeedsCredentials {
		if len(p.Username) < d.MinUsernameLen {
			return fmt.Errorf("lookup.%s: username must be at least %d characters when enabled",
				label, d.MinUsernameLen)
		}
		if len(p.Password) < d.MinPasswordLen {
			return fmt.Errorf("lookup.%s: password must be at least %d characters when enabled",
				label, d.MinPasswordLen)
		}
	}
	// User-Agent is checked at provider Initialize time (sourced from
	// the daemon's global Config.UserAgent, not per-provider config).
	return nil
}

// lookupTransportSecure reports whether a credentialed lookup provider's URL is
// safe to carry credentials: https to any host, or http only to a loopback
// address (so a local mock / test server works while a real remote URL must be
// https). Review 2026-06-19 M2.
func lookupTransportSecure(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
	// Parse From (and a non-empty default recipient) as a single mailbox at
	// startup/PUT so a malformed address fails fast here rather than only when
	// the operator first tries to send (review 2026-06-19 M2). Matches the
	// mailbox check internal/email.Send applies at send time.
	if _, err := mail.ParseAddress(s.From); err != nil {
		return fmt.Errorf("smtp.from %q is not a valid email address", s.From)
	}
	if s.DefaultRecipient != "" {
		if _, err := mail.ParseAddress(s.DefaultRecipient); err != nil {
			return fmt.Errorf("smtp.default_recipient %q is not a valid email address", s.DefaultRecipient)
		}
	}
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("smtp.port must be in 1..65535, got %d", s.Port)
	}
	if s.TimeoutSec <= 0 {
		return fmt.Errorf("smtp.timeout_sec must be > 0, got %d", s.TimeoutSec)
	}
	return nil
}

// validatePskReporter checks the PSK Reporter block. The subsystem is opt-in and
// fills Host/Port defaults at runtime (report.pskreporter.info:4739) when empty,
// so an empty block is valid; the only thing to guard is an out-of-range port —
// 0 means "use the default", anything outside 0..65535 is a typo the SPA's
// numeric input shouldn't produce but the daemon stays the authoritative gate.
func validatePskReporter(p types.PskReporterConfig) error {
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("psk_reporter.port must be in 0..65535 (0 = default), got %d", p.Port)
	}
	return nil
}

// bandToken matches ADIF band names ("160m", "70cm", "1.25m", "2.5mm",
// "submm") — lowercase required, matching the SPA's normalisation so a
// stored key always hits its lookup.
var bandToken = regexp.MustCompile(`^([0-9]+(\.[0-9]+)?(m|cm|mm)|submm)$`)

// hexColour is the strict 6-digit form the config SPA's colour picker emits.
var hexColour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// validateMap checks the contacts-map block. An empty block is the normal
// state (the SPA's default palette applies); each stored override must be a
// lowercase ADIF band token mapped to a #rrggbb colour — the daemon stays
// the authoritative gate even though the config SPA's picker can't produce
// anything else.
func validateMap(m types.MapConfig) error {
	for band, colour := range m.BandColors {
		if !bandToken.MatchString(band) {
			return fmt.Errorf("map.band_colors: %q is not a lowercase ADIF band token (e.g. \"20m\", \"70cm\")", band)
		}
		if !hexColour.MatchString(colour) {
			return fmt.Errorf("map.band_colors[%s]: %q is not a #rrggbb colour", band, colour)
		}
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
	// Mode-mapping validation moved to applyRigProfiles (per-rig, config.md §10):
	// the override layer now lives on each RigConfig, not in a global block here.

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
	// The serial-write watchdog (bridge.New → serial.Config.WriteTimeoutMS)
	// closes the port if a single blocking write hangs, so an out-of-range
	// value here is hardware-write-path relevant: too small closes the port on
	// normal scheduling jitter, too large leaves command / tune-stop writes
	// blocked far past the intended fault backstop. Same sane-range guard as
	// the other timeout overrides.
	if err := checkTimeout("bridge.timeouts.write_watchdog_ms", b.Timeouts.WriteWatchdogMs); err != nil {
		return err
	}
	// CI-V snapshot inter-frame gap (icom_civ only). Same sane-range guard; the
	// bridge additionally clamps it to a 2 s ceiling at construction so a large
	// value can't stall every state snapshot (ADR 0034 snapshot-timing fix).
	if err := checkTimeout("bridge.timeouts.civ_read_gap_ms", b.Timeouts.CivReadGapMs); err != nil {
		return err
	}
	// CI-V command ACK wait (icom_civ only). The command path waits this long for
	// the rig's FB/FA after each frame before giving up (ADR 0034 wait-for-ACK).
	// Same sane-range guard as the other timeout overrides.
	if err := checkTimeout("bridge.timeouts.civ_ack_ms", b.Timeouts.CivAckMs); err != nil {
		return err
	}
	// CI-V state-mirror poll cadence + collision back-off (icom_civ only, ADR
	// 0035). Same sane-range guard as the other timeout overrides.
	if err := checkTimeout("bridge.timeouts.civ_poll_interval_ms", b.Timeouts.CivPollIntervalMs); err != nil {
		return err
	}
	if err := checkTimeout("bridge.timeouts.civ_poll_quiet_ms", b.Timeouts.CivPollQuietMs); err != nil {
		return err
	}
	// FT8 meter-poll cadence + answer bound (ADR 0064). Same sane-range guard,
	// but here the range is load-bearing beyond typo-catching: these two feed
	// time.NewTicker / context.WithTimeout via a raw ms→Duration multiply in
	// bridge.New, so an overflow-scale positive value becomes a non-positive
	// Duration and panics the ticker at the first FT8 capture session, and a
	// 1 ms interval saturates the CAT link (codex 2026-08-09 P2).
	if err := checkTimeout("bridge.timeouts.ft8_meter_poll_interval_ms", b.Timeouts.Ft8MeterPollIntervalMs); err != nil {
		return err
	}
	if err := checkTimeout("bridge.timeouts.ft8_meter_poll_timeout_ms", b.Timeouts.Ft8MeterPollTimeoutMs); err != nil {
		return err
	}
	if b.Timeouts.BackoffInitialMs > 0 && b.Timeouts.BackoffMaxMs > 0 && b.Timeouts.BackoffInitialMs > b.Timeouts.BackoffMaxMs {
		return fmt.Errorf("bridge.timeouts.backoff_initial_ms (%d) must not exceed backoff_max_ms (%d)", b.Timeouts.BackoffInitialMs, b.Timeouts.BackoffMaxMs)
	}

	// Tune knobs (ADR 0027) — optional (zero = built-in default). A negative
	// value is a config error; positive values are accepted and clamped at
	// bridge.Service construction (power ≤ 40 W, duration ≤ 30 s) so config
	// can't create an unsafe tune. Validated unconditionally so a typo
	// surfaces at startup regardless of Enabled.
	if b.Tune.PowerW < 0 {
		return fmt.Errorf("bridge.tune.power_w = %d: must be 0 (default) or a positive wattage", b.Tune.PowerW)
	}
	if b.Tune.MaxDurationMs < 0 {
		return fmt.Errorf("bridge.tune.max_duration_ms = %d: must be 0 (default) or a positive duration in milliseconds", b.Tune.MaxDurationMs)
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
//
// The copy is SHALLOW: nested slices/maps/pointers (Rigs, Forwarders,
// Lookup.Chain, the mode-mapping maps, …) still alias the live config's
// backing storage. That's fine for read-only inspection, but a caller that
// intends to MUTATE the result (e.g. building a candidate to run through
// Normalize before persisting) must take an independent copy with Clone()
// first — otherwise the mutation reaches the live config outside the lock.
func (s *Service) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Cfg
}

// Clone returns a fully independent deep copy of the config — no shared
// slices, maps, or pointers with the receiver. Used for the candidate-editing
// path (PUT /v1/config builds a candidate, Normalizes it in place, then
// persists): without a deep copy, Normalize's in-place edits (e.g.
// normalizeRigOverrides niling per-rig overrides) would mutate the live
// config's backing arrays before the change is validated or committed.
//
// Implemented as a JSON round-trip rather than a hand-written field-by-field
// deep copy: Config is exactly the on-disk config.json shape (WriteJSON
// marshals it, Load unmarshals it), so a marshal→unmarshal is provably
// faithful and — unlike a hand-rolled deep copy — cannot silently rot the day
// a new nested slice/map field is added. PUTs are rare, so the cost is moot.
func (c Config) Clone() Config {
	b, err := json.Marshal(c)
	if err != nil {
		// Config is plain JSON-serializable data (no channels/funcs); marshal
		// cannot fail in practice. Fall back to the shallow value copy rather
		// than panicking — strictly no worse than pre-Clone behaviour.
		return c
	}
	var out Config
	if err := json.Unmarshal(b, &out); err != nil {
		return c
	}
	return out
}

// Update applies fn to a copy of the current config under the write
// lock, then atomically rewrites the on-disk config file via
// WriteJSON and replaces the in-memory Cfg only after the disk write
// succeeds. fn may return an error to abort (e.g. validation
// failure); in that case neither the in-memory state nor the file is
// touched.
//
// The working value is a deep Clone, not a shallow value copy: a shallow copy
// shares nested slices/maps/pointers with the live config, so a closure that
// appended to cfg.Forwarders or edited cfg.Rigs[i].ModeMappings before
// returning an error (or before a write failure) would leak that mutation into
// the live in-memory config despite Update reporting failure (review 2026-06-19
// M3). Cloning keeps the "abort touches nothing" contract true for nested edits.
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

	next := s.Cfg.Clone()
	if err := fn(&next); err != nil {
		return err
	}

	if err := WriteJSON(s.Path, next); err != nil {
		return fmt.Errorf("config.Service.Update: writing config: %w", err)
	}

	s.Cfg = next
	return nil
}

// UpdateInMemoryThenPersist applies fn and commits the result to the in-memory
// config IMMEDIATELY, then makes a best-effort attempt to persist it to disk.
// It is the inverse commit order of Update: the in-memory mutation ALWAYS takes
// effect, and a disk-write failure is returned (non-nil) but the memory change
// stands.
//
// Use only for session-critical corrections the daemon must honour this run
// even when config.json is unwritable — e.g. the startup default-logbook
// self-heal, where coming up with the wrong logbook id (and failing the first
// QSO submit) is worse than not persisting the fix; the next startup re-runs
// the heal. For ordinary updates use Update, where the file is the source of
// truth and a failed write leaves memory untouched.
func (s *Service) UpdateInMemoryThenPersist(fn func(cfg *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Deep clone for the same reason as Update (M3): on an fn error we return
	// with memory untouched, so the working value must not alias the live
	// config's nested slices/maps.
	next := s.Cfg.Clone()
	if err := fn(&next); err != nil {
		return err
	}
	s.Cfg = next // memory wins regardless of the disk outcome below

	if s.Path == "" {
		return fmt.Errorf(
			"config.Service.UpdateInMemoryThenPersist: in-memory update applied but no on-disk path set to persist it")
	}
	if err := WriteJSON(s.Path, next); err != nil {
		return fmt.Errorf(
			"config.Service.UpdateInMemoryThenPersist: in-memory update applied but writing config failed: %w", err)
	}
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
// These accessors all read s.Cfg, which Update() reassigns wholesale under
// the write lock — so each takes the read lock to avoid racing a concurrent
// PUT /v1/config (a torn read of an unrelated field is still a data race).
// They return values / copies, never the live backing storage.

func (s *Service) WorkingDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.workingDir != "" {
		return s.workingDir
	}
	return s.Cfg.DataDir
}

// LoggingConfig returns the logging configuration.
func (s *Service) LoggingConfig() (types.LoggingConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Cfg.Logging, nil
}

// DatastoreConfig returns the sqlite datastore configuration.
func (s *Service) DatastoreConfig() (types.DatastoreConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Cfg.Datastore, nil
}

// Forwarders returns a defensive copy of the configured forwarding
// destinations. Callers (qsoservice submit/update/delete) range over it; a
// copy means they can neither race Update's reassignment nor accidentally
// mutate the live config slice. The copy is deep: each entry's mutable nested
// fields (Credentials, ActionFilter, Retry) are cloned, so a caller that
// writes through a returned entry can't reach the live config under the lock
// (review 2026-06-19 L2).
func (s *Service) Forwarders() []types.ForwarderConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Cfg.Forwarders) == 0 {
		return nil
	}
	out := make([]types.ForwarderConfig, len(s.Cfg.Forwarders))
	for i, f := range s.Cfg.Forwarders {
		out[i] = cloneForwarder(f)
	}
	return out
}

// cloneForwarder deep-copies the mutable nested fields of a ForwarderConfig
// (the scalar fields copy by value with the struct assignment).
func cloneForwarder(f types.ForwarderConfig) types.ForwarderConfig {
	if f.Credentials != nil {
		f.Credentials = append(json.RawMessage(nil), f.Credentials...)
	}
	if f.ActionFilter != nil {
		f.ActionFilter = append([]string(nil), f.ActionFilter...)
	}
	if f.Retry != nil {
		r := *f.Retry
		f.Retry = &r
	}
	return f
}

// Enrichment returns a defensive copy of the enrichment pipeline
// configuration per ADR 0017. The Chain slice is cloned so a caller can't
// mutate the live provider list under the lock; LookupConfig itself is all
// scalars, so a per-element value copy suffices (review 2026-06-19 L2).
func (s *Service) Enrichment() types.EnrichmentConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.Cfg.Lookup
	if out.Chain != nil {
		out.Chain = append([]types.LookupConfig(nil), out.Chain...)
	}
	if out.ContinueIfBlank != nil {
		out.ContinueIfBlank = slices.Clone(out.ContinueIfBlank)
	}
	return out
}

// CountryTTL returns the country-table staleness threshold as a
// time.Duration. Operator config holds the value in days; this
// accessor performs the conversion so consumers (the orchestrator)
// don't have to.
// An explicit 0 converts to a zero Duration, which Orchestrator.isStale reads
// as "trust the cache indefinitely" — that is the operator's knob, not a bug.
// nil falls back to the default here as well as in Normalize, because a Service
// can be constructed directly from a Config that was never normalised.
func (s *Service) CountryTTL() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Duration(resolveTTLDays(s.Cfg.Lookup.CountryTTLDays, defaultCountryTTLDays)) * 24 * time.Hour
}

// StationTTL returns the contacted_station staleness threshold.
func (s *Service) StationTTL() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Duration(resolveTTLDays(s.Cfg.Lookup.StationTTLDays, defaultStationTTLDays)) * 24 * time.Hour
}

// resolveTTLDays is the ONE place that reads a nil TTL as "use the default", so
// Normalize and the accessors cannot drift about what absent means.
func resolveTTLDays(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// RefreshMaxInFlight returns the bound on concurrent async refresh
// goroutines. Zero is interpreted by refresher.Service as "use
// package default."
func (s *Service) RefreshMaxInFlight() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	s.mu.RLock()
	defer s.mu.RUnlock()
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
