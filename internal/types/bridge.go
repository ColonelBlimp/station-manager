package types

// BridgeConfig holds the daemon's serial/CAT bridge subsystem
// configuration. Per ADR 0013 the bridge runs in-process as a
// subsystem of `cmd/smd`; per ADR 0019 the v1 shape is read-only
// (rig pushes state via AUTO-mode CAT, bridge filters and forwards
// via SSE on `/v1/rig/events`, SPA displays — no inbound command
// path, no PTT awareness in v1).
//
// Enabled gates the whole subsystem. When false (operator's master
// smd, headless server, or any deployment without a rig connected),
// the bridge does not acquire the serial port and the
// `/v1/rig/events` HTTP route is not registered. Default true; flip
// to false on rig-less hosts via config.json.
//
// Serial / CAT sub-blocks describe the rig hardware. Driver names
// match the JSON files under `internal/cat/rigs/` (yaesu-ft710,
// yaesu-ftdx10 today; more as `internal/cat` grows).
type BridgeConfig struct {
	Enabled  bool                 `json:"enabled"`
	Serial   BridgeSerialConfig   `json:"serial"`
	Cat      BridgeCatConfig      `json:"cat"`
	Timeouts BridgeTimeoutsConfig `json:"timeouts,omitempty"`

	// Tune configures the tune-carrier feature (ADR 0027): the operator's
	// one-button external-amp tune — a steady reduced-power RTTY carrier the
	// daemon keys, holds, and is guaranteed to drop. Both knobs are code
	// constants the operator may adjust via config up to a hard ceiling; the
	// daemon clamps each at construction so config can never create an unsafe
	// tune. Zero / omitted = built-in default. See BridgeTuneConfig.
	Tune BridgeTuneConfig `json:"tune,omitempty"`

	// ModeMappings is the operator-override layer for the per-rig
	// translation table that turns rig-pushed mode strings (e.g.
	// "DATA-U") into ADIF (MODE, SUBMODE) pairs. Outer key is the rig
	// driver id (e.g. "yaesu-ftdx10"); inner key is the rig literal
	// mode string. Shipped defaults live inside each rigdef JSON
	// under `mode_mappings` and are merged with this override at
	// `/v1/config` GET time — operator's entry wins on key collision.
	//
	// The split (shipped defaults vs operator overrides) means a
	// daemon upgrade picks up new shipped mappings without touching
	// the operator's edits; the operator can change any value without
	// risking the next upgrade overwriting it.
	ModeMappings map[string]map[string]ModeMapping `json:"mode_mappings,omitempty"`
}

// ModeMapping pairs an ADIF MODE with an optional SUBMODE refinement.
// The wire shape used both in rigdef shipped defaults (decoded by
// `internal/cat`) and in operator overrides on config.json. Mode is
// validated against the daemon's loaded `internal/enums/modes`
// catalogue (which is itself data-driven via the embedded
// `adif-modes.json` + an optional `$SM_WORKING_DIR/modes.json`
// operator override). SubMode, when non-empty, is validated against
// the same catalogue's submode set.
type ModeMapping struct {
	Mode    string `json:"mode"`
	SubMode string `json:"submode,omitempty"`
}

// BridgeSerialConfig is the serial-port end of the rig connection.
// Port is the device node (`/dev/ttyUSB0`, COM3, etc.) — operator-
// specific to their hardware layout, can't be encoded in a rigdef.
// Every other serial parameter (baud rate, data bits, parity, stop
// bits, line delimiter, timeouts) comes from the per-driver
// definition in `internal/cat/rigs/*.json` — they're
// protocol-determined and not operator-configurable.
type BridgeSerialConfig struct {
	Port string `json:"port"`
}

// BridgeCatConfig is the CAT-protocol end. Driver picks the per-rig
// command set + AUTO-mode parser from `internal/cat/rigs/`. Empty
// driver = subsystem cannot decode rig pushes; treated as a config
// error at startup when Enabled is true.
type BridgeCatConfig struct {
	Driver string `json:"driver"`
}

// BridgeTimeoutsConfig surfaces the supervisor + readLoop tuning
// vars that were previously package-level defaults inside
// `internal/bridge`. Zero = "use the built-in default" so an
// operator's existing config.json keeps working without edits;
// non-zero values override per-Service.
//
// All values in milliseconds for round-trip clarity in JSON. The
// daemon snapshots them at `bridge.Service.New` time — runtime
// PUT /v1/config changes don't reach a running Service. Operator
// restart picks up edits, same pattern as the rest of BridgeConfig.
//
// LivenessMs is the read-deadline window in `readLoop`. Shorter =
// faster disconnect detection on rigs whose serial port doesn't
// surface a kernel-level USB-detach error (e.g. FTdx10 — the device
// node persists and reads simply go silent). Longer = fewer
// false-positive disconnect events during legitimate idle. Default
// 5000ms (5s) since 2026-05-16; was 30000ms (30s) prior. The
// no-data branch's INIT+READ probe means even a false-positive
// disconnect self-recovers within milliseconds if the rig is alive.
//
// BackoffInitialMs / BackoffMaxMs bound the supervisor's
// exponential retry after a transient pipeline exit. Doubled each
// failed attempt, capped at Max. Default 1000 / 30000 (1s / 30s).
//
// SteadyStateThresholdMs is how long a runPipeline must survive
// before the supervisor counts it as "interrupted steady state"
// and resets the backoff + dedup token. Default 10000ms (10s).
type BridgeTimeoutsConfig struct {
	LivenessMs             int `json:"liveness_ms,omitempty"`
	BackoffInitialMs       int `json:"backoff_initial_ms,omitempty"`
	BackoffMaxMs           int `json:"backoff_max_ms,omitempty"`
	SteadyStateThresholdMs int `json:"steady_state_threshold_ms,omitempty"`
}

// BridgeTuneConfig holds the tune-carrier knobs (ADR 0027). Both are optional
// (zero = built-in default) and operator-overridable up to a hard
// code-enforced ceiling: config can lower a value or raise it toward the
// ceiling but never past it — the daemon clamps each at bridge.Service
// construction. Same lifecycle as BridgeTimeoutsConfig: read once,
// snapshotted at New, operator restart to pick up edits.
//
// PowerW is the rig's output in watts during a tune (default 20, clamp ≤ 40)
// — the external amp is tuned at reduced drive. MaxDurationMs is the hard
// auto-off backstop (default 15000, clamp ≤ 30000): the carrier drops
// unconditionally after this long even if the operator never toggles tune
// off, the SPA tab closes, or the network drops.
//
// RestoreSettleMs is the pause between unkeying (tx_off) and restoring the
// pre-tune mode+power on tune stop (default 150, clamp ≤ 2000). Some rigs
// (the FTdx10) ignore a mode change during the TX→RX transition tail right
// after unkey, so the mode restore is sent after this settle. The carrier is
// already down before the pause, so it never affects the guaranteed stop —
// only the best-effort restore. Raise it if a rig's mode still doesn't restore.
type BridgeTuneConfig struct {
	PowerW          int `json:"power_w,omitempty"`
	MaxDurationMs   int `json:"max_duration_ms,omitempty"`
	RestoreSettleMs int `json:"restore_settle_ms,omitempty"`
}
