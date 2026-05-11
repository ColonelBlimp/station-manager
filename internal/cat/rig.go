package cat

import "github.com/ColonelBlimp/station-manager/internal/types"

// RigDefinition is the shape of a rig-database entry. Ships as an embedded
// rigs/*.json file or (once implemented) via an operator-registered external
// directory. Contains everything SM knows about a supported rig: identity,
// serial port defaults, CAT timing tunables, command table, state parsers,
// and shipped ADIF mode-mapping defaults. Port name is operator-specific
// and is NOT part of a RigDefinition — it lives in the operator's own
// config alongside the rig id.
type RigDefinition struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Manufacturer string                       `json:"manufacturer"`
	Model        string                       `json:"model"`
	Family       string                       `json:"family"`
	Description  string                       `json:"description,omitempty"`
	Terminator   string                       `json:"terminator"`
	Serial       RigSerial                    `json:"serial"`
	Timing       RigTiming                    `json:"timing"`
	Commands     []Command                    `json:"commands"`
	States       []State                      `json:"states"`
	ModeMappings map[string]types.ModeMapping `json:"mode_mappings,omitempty"`
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

// RigModes returns the unique rig-mode strings the rigdef's MAINMODE
// parser can produce. Pulled from the value_mappings of the State
// whose Marker tag is "MAINMODE" — that's the canonical "what mode
// can this rig push" set. Used by the daemon's /v1/config to populate
// the SPA's Mode Mappings sub-tab (one row per returned string).
// Returns nil when the rigdef has no MAINMODE marker (defensive — a
// rigdef without one is still valid CAT but has nothing to translate).
func RigModes(def RigDefinition) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, st := range def.States {
		for _, mk := range st.Markers {
			if mk.Tag != "MAINMODE" {
				continue
			}
			for _, vm := range mk.ValueMappings {
				if _, ok := seen[vm.Value]; ok {
					continue
				}
				seen[vm.Value] = struct{}{}
				out = append(out, vm.Value)
			}
		}
	}
	return out
}
