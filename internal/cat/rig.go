package cat

import "github.com/ColonelBlimp/station-manager/internal/types"

// Protocol values select the per-family codec behind the seam in
// codec.go/commands.go (ADR 0034). An empty Protocol defaults to
// ProtocolKenwood, so every existing rigdef is unchanged.
const (
	// ProtocolKenwood is the Kenwood-style ASCII CAT family — `;`-terminated
	// text, two-letter prefixes, ASCII-digit fields. Named for the family,
	// not the vendor: Yaesu/Elecraft/Flex all speak Kenwood's protocol.
	ProtocolKenwood = "kenwood"
	// ProtocolIcomCIV is the Icom CI-V family — binary `FE FE…FD` frames on a
	// shared addressed bus, little-endian BCD frequency, byte mode codes.
	ProtocolIcomCIV = "icom_civ"
)

// Encoding kinds name how a Command's value becomes its wire payload
// (ADR 0034). An empty Encoding defaults to EncodingASCII, so every existing
// Kenwood command is unchanged. The bcd_*/mode_seq/none kinds are CI-V only.
const (
	// EncodingASCII is the Kenwood default: a %s template filled via value_map
	// + pad (the historical behaviour, see commands.go EncodeCommand).
	EncodingASCII = "ascii"
	// EncodingNone is a CI-V command with no value substitution — its full
	// byte sequence (command + optional subcommand + fixed data) lives in Cmd
	// as hex. Used by INIT/tx_on/tx_off.
	EncodingNone = "none"
	// EncodingFrameSeq is a valueless command that emits a fixed SEQUENCE of
	// CI-V frames (the bytes live in Command.Frames, not Cmd). The CI-V
	// analogue of the Kenwood READ packing several `;`-terminated queries into
	// one string: READ polls freq (`03`) + mode (`04`) as two frames so a
	// freshly-connected SPA gets a state snapshot (Transceive only broadcasts
	// on change, never on connect).
	EncodingFrameSeq = "frame_seq"
	// EncodingBCDFreq encodes a decimal-Hz value as 5-byte little-endian BCD
	// appended to the command bytes (CI-V set_freq).
	EncodingBCDFreq = "bcd_freq"
	// EncodingModeSeq expands a mode literal into an ordered sequence of one or
	// more complete CI-V frames (CI-V set_mode). Icom mode is two-dimensional —
	// a base mode plus an orthogonal data flag — so one literal can need more
	// than one frame; the sequence lives in Command.ModeSeq as data.
	EncodingModeSeq = "mode_seq"
	// EncodingBCDPower encodes a decimal level (0–255 on the IC-7300) as 2-byte
	// big-endian BCD appended to the command bytes (CI-V set_power). NOTE: the
	// value is a 0–255 rig level, not watts — the watts→level mapping is the
	// caller's job (tune-on-Icom is future work, ADR 0034).
	EncodingBCDPower = "bcd_power"
)

// Marker decode kinds for the CI-V path (ADR 0034). The empty kind is the
// Kenwood default (raw byte-slice → string, optionally value-mapped); the
// CI-V decoder recognises only the kinds below.
const (
	// MarkerKindBCDFreq reads Length little-endian BCD bytes as a decimal-Hz
	// string (CI-V frequency broadcast/reply).
	MarkerKindBCDFreq = "bcd_freq"
	// MarkerKindByte reads the marker's byte slice as an uppercase hex code and
	// maps it through value_mappings (CI-V mode byte → literal). The base mode
	// only — the data flag is never broadcast (ADR 0034 read strategy).
	MarkerKindByte = "byte"
	// MarkerKindBCDLevel reads Length big-endian BCD bytes as a 0–255 CI-V level
	// (the power-setting format, 14 0A) and scales it to watts via the marker's
	// ScaleMaxWatts: watts = round(level × ScaleMaxWatts / 255). So a rig whose
	// power is a scaled level (Icom) surfaces the same watts the Yaesu PC field
	// reports directly, and the bridge's TXPWR→Power mapping stays rig-agnostic.
	MarkerKindBCDLevel = "bcd_level"
)

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

	// Protocol selects the codec family (ADR 0034). Empty defaults to
	// ProtocolKenwood (the ASCII path) so every existing rigdef is unchanged;
	// ProtocolIcomCIV selects the CI-V engine.
	Protocol string `json:"protocol,omitempty"`

	// CivAddress is the rig's CI-V bus address as a hex string (e.g. "94" for
	// the IC-7300). Required for ProtocolIcomCIV, ignored otherwise. The
	// controller address is an engine constant (civControllerAddress).
	CivAddress string `json:"civ_address,omitempty"`

	// Ft8Mode is the rig-mode literal FT8 operates in on this model (e.g.
	// "DATA-U" on the Yaesus) — the per-model default the FT8 TX controller
	// switches to before keying, and the mode band-select sets. The rigdef
	// supplies it as the default; an operator may override per rig via
	// RigConfig.Ft8Mode (config.md §10). Empty = no FT8 default for this model
	// (leave the rig's current mode).
	Ft8Mode string `json:"ft8_mode,omitempty"`
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

	// RTS and DTR are the initial modem-output-line states the transport sets
	// at open (*bool tri-state: nil/omitted = leave go.bug.st default of both
	// asserted; an explicit false DE-ASSERTS). Icom CI-V sets both false: its
	// USB SEND can map PTT to a control line, so opening with the line asserted
	// would key the rig (ADR 0034). The Yaesu USB-CDC rigs set true (the
	// default, where the lines aren't flow control anyway).
	RTS *bool `json:"rts,omitempty"`
	DTR *bool `json:"dtr,omitempty"`
}

// RigTiming holds CAT-level timing tunables for the rig's background
// listener loop. All values are milliseconds.
type RigTiming struct {
	ListenerIntervalMS    int `json:"listener_interval_ms"`
	ListenerReadTimeoutMS int `json:"listener_read_timeout_ms"`
}

// Command is a named wire-template. Name is the semantic identifier the
// caller uses to look up the command (e.g. "read_freq_a"); Cmd is the raw
// wire string, possibly including a single Go fmt %s verb that the value
// substitutes into.
//
// The trailing three fields drive the data-driven inbound command path
// (ADR 0026) and default to off (zero value), so commands that only INIT/
// READ/decode logic touch need none of them:
//
//   - ValueMap names a marker Tag whose value_mappings translate the
//     caller's rig literal into the wire code the field expects (e.g. tag
//     "MAINMODE": "DATA-U" -> "C"). The send-side inversion is well-defined
//     only when that map is injective on its Value side; pinned by test.
//   - Pad left-zero-pads the (post-ValueMap) value to this width before it
//     fills the template's %s, putting fixed field widths in data rather
//     than a %09d verb the string value carrier cannot satisfy. 0 = none.
//   - Exposed gates reachability via /v1/rig/command. Absent = default
//     deny: a command is reachable from the external path only when
//     explicitly flagged, keeping TX-capable (PLAYBACK) and internal
//     (INIT, READ) commands off that surface by default.
type Command struct {
	Name     string `json:"name"`
	Cmd      string `json:"cmd"`
	ValueMap string `json:"value_map,omitempty"`
	Pad      int    `json:"pad,omitempty"`
	Exposed  bool   `json:"exposed,omitempty"`

	// ScaleMaxWatts is the rated full-scale power in watts for an
	// EncodingBCDPower command whose rig takes a 0–255 level (Icom 14 0A): the
	// incoming watts value is scaled to a level (level = round(watts × 255 /
	// ScaleMaxWatts)) before BCD encoding. Zero means the value is already the
	// raw level (no scaling). Must match the power marker's ScaleMaxWatts.
	ScaleMaxWatts int `json:"scale_max_watts,omitempty"`

	// Encoding selects how the value becomes the wire payload (ADR 0034). Empty
	// defaults to EncodingASCII (the Kenwood %s-template path), so every Kenwood
	// command is unchanged. For ProtocolIcomCIV, Cmd holds the hex command bytes
	// (command + optional subcommand + fixed data) rather than an ASCII
	// template, and Encoding names the CI-V kind (none/bcd_freq/mode_seq/
	// bcd_power).
	Encoding string `json:"encoding,omitempty"`

	// ModeSeq carries the per-literal CI-V frame sequence for an
	// EncodingModeSeq command (CI-V set_mode). Each entry maps a rig mode
	// literal to the ordered frames that select it — one frame for a non-data
	// mode, two for a data mode (base mode then the `1A 06` data flag). Empty
	// for every other command and encoding.
	ModeSeq []ModeSequence `json:"mode_seq,omitempty"`

	// Frames carries the ordered hex frame bodies for an EncodingFrameSeq
	// command (CI-V READ). Each is wrapped in `FE FE…FD` and concatenated;
	// the rig answers each query in turn. Empty for every other encoding.
	Frames []string `json:"frames,omitempty"`

	// SetsState names the State marker tag this command sets (e.g. set_freq →
	// "VFOAFREQ", set_mode → "MAINMODE"). Used by the CI-V wait-for-ACK command
	// path (ADR 0034): the IC-7300 confirms a command with a bare FB/FA ACK and
	// never broadcasts the change, so on a successful FB the bridge synthesizes
	// the state push from the commanded value keyed by this tag. Data-driven, so
	// a later Icom adds settable ops without code. Empty for commands that don't
	// map to a display state (and for the Kenwood path, which confirms by push).
	SetsState string `json:"sets_state,omitempty"`
}

// ModeSequence maps one rig mode literal to the ordered CI-V frame bodies that
// select it (ADR 0034). Frames are hex strings of the command+subcommand+data
// bytes (a ":" separator is allowed for readability, e.g. "0601:01"); the
// engine strips separators, hex-decodes, and wraps each in `FE FE…FD`. The
// engine never interprets the bytes — `1A 06` (the data flag) is just data.
type ModeSequence struct {
	Mode   string   `json:"mode"`
	Frames []string `json:"frames"`
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

	// Kind selects how the CI-V decoder reads the marker's byte slice (ADR
	// 0034): MarkerKindBCDFreq (little-endian BCD → decimal Hz),
	// MarkerKindByte (uppercase hex code → value_mappings lookup), or
	// MarkerKindBCDLevel (big-endian BCD 0–255 level → watts via ScaleMaxWatts).
	// Empty is the Kenwood default — a raw byte-slice string, optionally
	// value-mapped — and is the only kind the ASCII decoder understands.
	Kind string `json:"kind,omitempty"`

	// ScaleMaxWatts is the rated full-scale power in watts for a
	// MarkerKindBCDLevel marker: the rig's 0–255 power level maps linearly onto
	// 0–ScaleMaxWatts (Icom 14 0A is a scaled level, not watts). Ignored for
	// other kinds. Must match the set_power command's ScaleMaxWatts so read and
	// write agree.
	ScaleMaxWatts int `json:"scale_max_watts,omitempty"`
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
//
// For ProtocolIcomCIV the settable mode set is a SUPERSET of the broadcast
// base modes (the data flag is never pushed, ADR 0034), so the list comes
// from the set_mode command's ModeSeq — the literals SM can actually set
// (e.g. "USB-D") — not from the decode MAINMODE marker (base modes only).
func RigModes(def RigDefinition) []string {
	if def.Protocol == ProtocolIcomCIV {
		for _, c := range def.Commands {
			if c.Encoding != EncodingModeSeq {
				continue
			}
			out := make([]string, 0, len(c.ModeSeq))
			for _, m := range c.ModeSeq {
				out = append(out, m.Mode)
			}
			return out
		}
		return nil
	}

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
