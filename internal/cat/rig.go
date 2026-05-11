package cat

// RigDefinition is the shape of a rig-database entry. Ships as an embedded
// rigs/*.json file or (once implemented) via an operator-registered external
// directory. Contains everything SM knows about a supported rig: identity,
// serial port defaults, CAT timing tunables, command table, state parsers,
// and shipped ADIF mode-mapping defaults. Port name is operator-specific
// and is NOT part of a RigDefinition — it lives in the operator's own
// config alongside the rig id.
type RigDefinition struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Manufacturer string                 `json:"manufacturer"`
	Model        string                 `json:"model"`
	Family       string                 `json:"family"`
	Description  string                 `json:"description,omitempty"`
	Terminator   string                 `json:"terminator"`
	Serial       RigSerial              `json:"serial"`
	Timing       RigTiming              `json:"timing"`
	Commands     []Command              `json:"commands"`
	States       []State                `json:"states"`
	ModeMappings map[string]ModeMapping `json:"mode_mappings,omitempty"`
}

// ModeMapping translates one rig-pushed mode string (e.g. "DATA-U" or
// "CW-U" — the value side of a MAINMODE/SUBMODE rigdef value_mapping)
// into an operator-friendly mode plus an optional explicit submode
// refinement. Ships per-rig in RigDefinition.ModeMappings; operator
// overrides live in their config.json under bridge.mode_mappings and
// are merged in at /v1/config GET time (rigdef provides defaults;
// operator's value wins when present).
//
// Mode is the operator-friendly value the SPA's mode dropdown shows
// (USB, LSB, CW, FM, AM, RTTY, FT8, FT4, PSK31, etc.) — same list
// the operator picks from when CAT is off. The ADIF main-vs-submode
// split is resolved at QSO-submit time via the existing
// resolveModeAndSubmode utility (e.g. Mode="FT8" → ADIF MODE=MFSK
// SUBMODE=FT8; Mode="USB" → ADIF MODE=SSB SUBMODE=USB; Mode="CW" →
// ADIF MODE=CW SUBMODE=""). Validated server-side: either a valid
// ADIF main mode (enums/modes.IsValidMode) or a known submode
// (enums/modes.IsValidSubMode).
//
// SubMode is for the rare case where the operator wants an explicit
// refinement on top of the dropdown value (e.g. Mode="CW" SubMode=
// "CW-N" for narrow CW, once the daemon's submode enum grows to
// include such variants). Empty for almost all defaults.
//
// Defaults (shipped in each rigdef): unambiguous Yaesu strings map
// to their obvious dropdown value (LSB→LSB, USB→USB, CW-U/CW-L→CW,
// FM/FM-N→FM, etc.). The ambiguous DATA-U/DATA-L cases default to
// FT8 since that's the most common digital protocol today; operator
// changes the override via the My Station → Mode Mappings sub-tab
// when running something else.
type ModeMapping struct {
	Mode    string `json:"mode"`
	SubMode string `json:"submode,omitempty"`
}

// RigSerial carries serial-port defaults for the rig. Values are in
// JSON-friendly form (parity as a string, line_delimiter as a
// single-character string); translation into go.bug.st/serial enum values
// and a concrete serial.Config happens at the caller, not inside this
// package. Keeping cat free of a serial import preserves the pure-codec
// layering described in docs/v2-design/bridge.md §3c and keeps the
// pluggable transport abstraction in bridge.md §8.3 reachable.
type RigSerial struct {
	BaudRate       int    `json:"baud_rate"`
	DataBits       int    `json:"data_bits"`
	StopBits       int    `json:"stop_bits"`
	Parity         string `json:"parity"`
	LineDelimiter  string `json:"line_delimiter"`
	ReadTimeoutMS  int    `json:"read_timeout_ms"`
	WriteTimeoutMS int    `json:"write_timeout_ms,omitempty"`
	RTS            bool   `json:"rts,omitempty"`
	DTR            bool   `json:"dtr,omitempty"`
}

// RigTiming holds CAT-level timing tunables for the rig's background
// listener loop. All values are milliseconds.
type RigTiming struct {
	ListenerIntervalMS    int `json:"listener_interval_ms"`
	ListenerReadTimeoutMS int `json:"listener_read_timeout_ms"`
}

// Command is a named wire-template. Name is the semantic identifier the
// caller uses to look up the command (e.g. "read_freq_a"); Cmd is the raw
// wire string, possibly including Go fmt %s verbs that the caller fills
// with positional arguments.
type Command struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
}

// State describes how to parse an incoming framed line whose first bytes
// match Prefix. Markers cut fields out of the line's tail (the bytes after
// the prefix) by index and length.
type State struct {
	Prefix  string   `json:"prefix"`
	Markers []Marker `json:"markers"`
}

// Marker extracts a single tagged field from a state line's data tail.
// Index and Length are offsets into the tail (byte 0 = first byte after
// the prefix). Value mappings, when present, translate the raw slice into
// a human-readable value (e.g. Yaesu mode code "2" -> "USB").
type Marker struct {
	Tag           string         `json:"tag"`
	Index         int            `json:"index"`
	Length        int            `json:"length"`
	ValueMappings []ValueMapping `json:"value_mappings,omitempty"`
}

// ValueMapping is one key/value pair in a Marker's lookup table. Applied
// by exact match against the sliced field bytes (as a string).
type ValueMapping struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
