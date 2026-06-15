package cat

import (
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// HasCommand reports whether the rigdef declares a command with the given
// name, exposed or not. The inbound command path (ADR 0026) uses the API op
// name as the rigdef command name directly, so this answers "does this rig
// define op X" with no translation layer.
//
// It is NOT the external-reachability check: an internal command (INIT,
// READ) or a TX-capable one (PLAYBACK) reports true here too. Gate the
// command path on EncodeCommand, which enforces the Exposed flag.
func HasCommand(def RigDefinition, name string) bool {
	_, ok := lookupCommand(def, name)
	return ok
}

// ExposedCommands returns the names of the rigdef's externally-reachable
// commands (Exposed == true), in rigdef order. This is the rig's advertised
// op vocabulary: GET /v1/config surfaces it in BridgeInfo so the SPA enables
// only the controls the connected rig actually offers (ADR 0026). The Exposed
// flag is the single source of truth for both this list and EncodeCommand's
// gate, so a rigdef edit can add an op with no Go change.
func ExposedCommands(def RigDefinition) []string {
	var out []string
	for _, c := range def.Commands {
		if c.Exposed {
			out = append(out, c.Name)
		}
	}
	return out
}

// EncodeCommand produces the wire bytes for an externally-reachable command
// (ADR 0026 inbound path). Where Encode is the low-level template filler,
// EncodeCommand enforces the command-path contract: the command must be
// flagged Exposed, then the value is translated through the command's
// ValueMap (when set) and left-zero-padded to its Pad width (when set)
// before filling the template's %s.
//
// value is the caller's semantic argument as a string — a rig mode literal
// for set_mode, decimal Hz for set_freq. CAT is an ASCII protocol, so a
// string is the universal carrier and every transform is string -> string;
// the data fields on Command, not Go code, describe each command's shape.
//
// A command with no value_map, no pad, and no %s verb (e.g. band_up = "BU;")
// is valueless — it emits its template verbatim and ignores value. Any other
// command requires a non-empty value.
//
// Errors (each wraps a sentinel for errors.Is):
//
//   - ErrUnknownCommand    — no command with this name in the rigdef
//   - ErrCommandNotExposed — command exists but is not reachable externally
//   - ErrUnmappedValue     — ValueMap is set but value is not in its table
//   - ErrMissingValue      — a value-bearing command was called with no value
//   - ErrInvalidPaddedValue — a padded (numeric) command got a non-digit or
//     over-wide value
//
// Width + digit validation IS enforced for padded commands (so a hand-crafted
// /v1/rig/command can't push a malformed CAT line like "PCabc;" onto the wire);
// semantic RANGE validation (e.g. a frequency within band) remains the command
// endpoint's responsibility — the codec stays permissive about value meaning.
func EncodeCommand(def RigDefinition, name, value string) ([]byte, error) {
	const op errors.Op = "cat.EncodeCommand"

	if def.Protocol == ProtocolIcomCIV {
		return encodeCommandCIV(def, name, value)
	}

	c, ok := lookupCommand(def, name)
	if !ok {
		return nil, errors.New(op).WithErr(ErrUnknownCommand).WithMsgf("unknown command %q", name)
	}
	if !c.Exposed {
		return nil, errors.New(op).WithErr(ErrCommandNotExposed).WithMsgf("command %q is not exposed", name)
	}

	// Valueless command — no value_map, no pad, no fmt verb (e.g. band_up =
	// "BU;"). Emit the template verbatim and ignore any value.
	if c.ValueMap == "" && c.Pad == 0 && !strings.Contains(c.Cmd, "%") {
		return Encode(def, name)
	}

	// Value-bearing command — a value is required.
	if value == "" {
		return nil, errors.New(op).WithErr(ErrMissingValue).WithMsgf("command %q requires a value", name)
	}

	v := value
	if c.ValueMap != "" {
		code, ok := valueCode(def, c.ValueMap, v)
		if !ok {
			return nil, errors.New(op).WithErr(ErrUnmappedValue).
				WithMsgf("value %q not in %q map", value, c.ValueMap)
		}
		v = code
	}
	if c.Pad > 0 {
		// A padded command names a fixed-width numeric field (set_freq FA%s;
		// pad 9, set_power PC%s; pad 3): the value must be ASCII digits that
		// fit the field — left-zero-pad fills the rest. Reject non-digit or
		// over-wide values so a hand-crafted /v1/rig/command can't pad a bad
		// value into a malformed CAT line, e.g. "PCabc;" or "FA000001.5;"
		// (review 2026-06-05 M1). Skipped when a value_map is also set — the
		// map already validated the literal and produced the wire code (no
		// padded+value_map command ships today; stay correct if one is added).
		if c.ValueMap == "" && (!isASCIIDigits(v) || len(v) > c.Pad) {
			return nil, errors.New(op).WithErr(ErrInvalidPaddedValue).
				WithMsgf("value %q must be at most %d digits for command %q", value, c.Pad, name)
		}
		v = leftZeroPad(v, c.Pad)
	}
	return Encode(def, name, v)
}

// isASCIIDigits reports whether s is non-empty and every byte is '0'..'9'.
// Used to gate padded numeric command values (set_freq, set_power); rejects
// signs, dots, hex, and any non-digit byte — only a bare unsigned integer
// fits a left-zero-padded CAT field.
func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// lookupCommand returns the named command from the rigdef and true, or a
// zero Command and false when absent. Mirrors codec.go's lookupState.
func lookupCommand(def RigDefinition, name string) (Command, bool) {
	for _, c := range def.Commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// CommandSetsState returns the State marker tag a command sets (its SetsState
// field), or "" if the command is unknown or doesn't declare one. The CI-V
// wait-for-ACK path (ADR 0034) uses it to key the synthesized state push from a
// commanded value once the rig ACKs with FB.
func CommandSetsState(def RigDefinition, name string) string {
	c, ok := lookupCommand(def, name)
	if !ok {
		return ""
	}
	return c.SetsState
}

// valueCode inverts the value_mappings of the marker carrying tag, mapping a
// rig literal back to the wire code its command field expects — the
// send-side counterpart to Decode's code -> literal lookup, and the generic
// engine behind a command's ValueMap. The inversion is well-defined only
// when the marker's value_mappings is injective on its Value side; for the
// FTdx10 MAINMODE table (the set_mode case) the bijection is pinned by
// TestEncodeCommandBijection.
//
// Returns ("", false) when no marker carries the tag or the literal is not
// in its table.
func valueCode(def RigDefinition, tag, literal string) (string, bool) {
	for _, st := range def.States {
		for _, mk := range st.Markers {
			if mk.Tag != tag {
				continue
			}
			for _, vm := range mk.ValueMappings {
				if vm.Value == literal {
					return vm.Key, true
				}
			}
		}
	}
	return "", false
}

// leftZeroPad pads s on the left with '0' to width n, returning s unchanged
// when it is already at least n long. The width lives in a command's Pad
// (e.g. 9 for the FTdx10 VFO frequency field) rather than a %09d verb,
// because the string value carrier cannot satisfy a numeric format verb.
func leftZeroPad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat("0", n-len(s)) + s
}
