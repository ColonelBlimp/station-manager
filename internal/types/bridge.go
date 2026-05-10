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
}

// BridgeSerialConfig is the serial-port end of the rig connection.
// Path is the device node (`/dev/ttyUSB0`, COM3, etc.); Baud the line
// speed. Other serial parameters (data bits, parity, stop bits) come
// from the per-driver definition in `internal/cat/rigs/*.json` —
// they're protocol-determined, not operator-configurable.
type BridgeSerialConfig struct {
	Port string `json:"port,omitempty"`
	Baud int    `json:"baud,omitempty"`
}

// BridgeCatConfig is the CAT-protocol end. Driver picks the per-rig
// command set + AUTO-mode parser from `internal/cat/rigs/`. Empty
// driver = subsystem cannot decode rig pushes; treated as a config
// error at startup when Enabled is true.
type BridgeCatConfig struct {
	Driver string `json:"driver,omitempty"`
}
