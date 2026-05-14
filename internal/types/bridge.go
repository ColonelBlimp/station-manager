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
	Enabled bool               `json:"enabled"`
	Serial  BridgeSerialConfig `json:"serial,omitempty"`
	Cat     BridgeCatConfig    `json:"cat,omitempty"`

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
	Port string `json:"port,omitempty"`
}

// BridgeCatConfig is the CAT-protocol end. Driver picks the per-rig
// command set + AUTO-mode parser from `internal/cat/rigs/`. Empty
// driver = subsystem cannot decode rig pushes; treated as a config
// error at startup when Enabled is true.
type BridgeCatConfig struct {
	Driver string `json:"driver,omitempty"`
}
