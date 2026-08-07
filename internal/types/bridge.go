package types

// BridgeConfig holds the daemon's serial/CAT bridge subsystem
// configuration. Per ADR 0013 the bridge runs in-process as a
// subsystem of `cmd/smd`. State display is read-only (rig pushes
// state via AUTO-mode CAT — ADR 0019 — the bridge filters and
// forwards via SSE on `/v1/rig/events`, the SPA displays). On top of
// that read path the bridge now also carries a narrow inbound command
// path (`/v1/rig/command` — freq/mode/VFO/band, ADR 0026) and two
// daemon-owned keyed-transmission features that drive PTT: the tune
// carrier (ADR 0027) and FT8 TX (ADR 0030). Still NOT in v1:
// phone/CW PTT-for-operating (QSO keying) and rigctld-compat TCP.
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
//
// Serial / Cat are pointers (omitempty) because serial port + CAT driver live
// per-rig now (RigConfig.Port + Model, config.md §10): the stored config carries
// nil here — only Config.ActiveBridge() populates them, as a fresh runtime
// projection of the active rig — so a clean config no longer persists empty
// `"serial": {}` / `"cat": {}` blocks. A loaded legacy config that still has
// them is read once by the catalogue-synthesis fold.
type BridgeConfig struct {
	Enabled  bool                 `json:"enabled"`
	Serial   *BridgeSerialConfig  `json:"serial,omitempty"`
	Cat      *BridgeCatConfig     `json:"cat,omitempty"`
	Timeouts BridgeTimeoutsConfig `json:"timeouts,omitempty"`

	// Tune configures the tune-carrier feature (ADR 0027): the operator's
	// one-button external-amp tune — a steady reduced-power RTTY carrier the
	// daemon keys, holds, and is guaranteed to drop. Both knobs are code
	// constants the operator may adjust via config up to a hard ceiling; the
	// daemon clamps each at construction so config can never create an unsafe
	// tune. Zero / omitted = built-in default. See BridgeTuneConfig.
	Tune BridgeTuneConfig `json:"tune,omitempty"`

	// Mode-mapping overrides moved to RigConfig.ModeMappings (config.md §10, B2):
	// the rig knows its own Model, so they live per-rig (keyed by rig literal)
	// rather than in a global driver-keyed block here. The merged view (rigdef
	// defaults + the active rig's overrides) is still served at /v1/config GET.
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
	// omitempty: under ADR 0028 the active rig's Port is projected onto this
	// field by Config.ActiveBridge() and always wins, so a value left here is
	// inert; the empty loose field shouldn't persist in rewritten configs.
	Port string `json:"port,omitempty"`

	// Overrides carries the active rig's per-rig serial overrides (config.md §10,
	// B2), projected here from RigConfig.Overrides by Config.ActiveBridge().
	// Runtime-only (json:"-") — the persisted home is the rig, not this bridge
	// block. A non-zero field wins over the rigdef serial default in
	// buildSerialConfig; a zero field inherits the rigdef default.
	Overrides RigOverrides `json:"-"`
}

// BridgeCatConfig is the CAT-protocol end. Driver picks the per-rig
// command set + AUTO-mode parser from `internal/cat/rigs/`. Empty
// driver = subsystem cannot decode rig pushes; treated as a config
// error at startup when Enabled is true.
type BridgeCatConfig struct {
	// omitempty: same rationale as BridgeSerialConfig.Port — the active rig's
	// Model is projected onto Driver by Config.ActiveBridge() and always wins,
	// so the empty loose field shouldn't persist in rewritten configs.
	Driver string `json:"driver,omitempty"`
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
//
// WriteWatchdogMs bounds a single serial write (the bridge passes it to
// serial.Config.WriteTimeoutMS). go.bug.st/serial has no write deadline, so a
// blocking port.Write on a driver/HW fault would otherwise hang the writer
// goroutine forever (and with it the tune guaranteed-stop). On overrun the port
// is closed and the supervisor reopens it. Default 2000ms (2s) — a fault
// backstop well above any legitimate CAT-write latency, NOT a per-write SLA.
//
// CivReadGapMs is the inter-frame delay the bridge inserts between the
// individual read frames of a CI-V state snapshot (READ). CI-V is half-duplex:
// a second read sent while the rig is turning around its reply to the first
// makes the rig abandon the in-progress reply and answer only the last read, so
// a back-to-back read-freq + read-mode burst loses the freq reply (bench
// 2026-06-15: a fresh SPA tab showed stale freq, current mode). Spacing the
// frames lets each reply complete. Default 50ms; only the icom_civ path uses it
// (the Kenwood ASCII rigs queue a ;-delimited burst fine, so it's ignored
// there). Clamped to a sane ceiling so a typo can't stall every snapshot.
//
// CivAckMs is how long the CI-V command path waits for the rig's FB/FA ACK
// after writing each command frame (ADR 0034 wait-for-ACK). The IC-7300 confirms
// a set-command with a bare FB (OK) / FA (NG) and never broadcasts the change,
// so the bridge adopts the commanded state on FB and surfaces FA as an error;
// this bounds that wait. Measured round-trip is ~20ms, so the 500ms default is
// generous; only the icom_civ path uses it (the Kenwood path is fire-and-forget,
// confirm-by-push).
type BridgeTimeoutsConfig struct {
	LivenessMs             int `json:"liveness_ms,omitempty"`
	BackoffInitialMs       int `json:"backoff_initial_ms,omitempty"`
	BackoffMaxMs           int `json:"backoff_max_ms,omitempty"`
	SteadyStateThresholdMs int `json:"steady_state_threshold_ms,omitempty"`
	WriteWatchdogMs        int `json:"write_watchdog_ms,omitempty"`
	CivReadGapMs           int `json:"civ_read_gap_ms,omitempty"`
	CivAckMs               int `json:"civ_ack_ms,omitempty"`

	// CivPollIntervalMs is the steady-state cadence of the Icom state-mirror
	// poll (ADR 0035) — the daemon fires the rigdef's POLL read-list this often
	// to mirror the fields CI-V Transceive never pushes (non-operating VFO, mode
	// data flag, split). Default ~1s; only icom_civ rigs that declare a POLL
	// command use it. CivPollQuietMs is the collision back-off: a poll tick is
	// skipped if a Transceive broadcast arrived within this window (the rig is
	// mid dial-turn storm on the half-duplex bus), since freq is pushed in
	// real-time anyway and the missed gap-read recovers on the next tick.
	CivPollIntervalMs int `json:"civ_poll_interval_ms,omitempty"`
	CivPollQuietMs    int `json:"civ_poll_quiet_ms,omitempty"`

	// Ft8MeterPollIntervalMs / Ft8MeterPollTimeoutMs are the ADR 0064 FT8
	// ALC/PO meter poll knobs (defaults 250 / 100, operator-ratified
	// 2026-08-06). The interval is the RM4;RM5; cadence while an FT8 capture
	// session is live; the timeout bounds each poll write so the unkey never
	// queues behind more than one bounded exchange. Only rigs whose rigdef
	// declares a METERPOLL command poll.
	Ft8MeterPollIntervalMs int `json:"ft8_meter_poll_interval_ms,omitempty"`
	Ft8MeterPollTimeoutMs  int `json:"ft8_meter_poll_timeout_ms,omitempty"`
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
