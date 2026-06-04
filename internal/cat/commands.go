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
//
// Range and width validation of value (e.g. a frequency within band) is the
// command endpoint's responsibility; the codec stays permissive.
func EncodeCommand(def RigDefinition, name, value string) ([]byte, error) {
	const op errors.Op = "cat.EncodeCommand"

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
		v = leftZeroPad(v, c.Pad)
	}
	return Encode(def, name, v)
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
