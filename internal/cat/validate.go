package cat

import (
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// unsafeExposableCommands are command names this project never permits a rigdef
// to mark `exposed`. They key the transmitter (tx_on/tx_off) or play back stored
// audio (PLAYBACK); the safety model routes TX only through the tune controller's
// guaranteed-stop path (ADR 0027), never the generic POST /v1/rig/command path.
// Pinned by tests for the shipped rigdefs (TestTxCommands_NotExposed); validated
// here so a future embedded or external rigdef cannot publish one by typo.
var unsafeExposableCommands = map[string]struct{}{
	"tx_on":    {},
	"tx_off":   {},
	"PLAYBACK": {},
}

// ValidateRigDefinition checks a rig definition for the structural faults the
// embedded loader (and a future external-dir loader) must reject, so a
// hand-authored rigdef typo fails loudly at load instead of becoming silent
// garbage on the CAT write path (review 2026-06-05 M2). `Encode` is
// `fmt.Appendf`, which never errors on a bad template — a missing/extra/wrong
// verb just emits malformed bytes — and the send-side value-map inversion
// silently picks the first match, so these faults are otherwise invisible
// until they reach the rig.
//
// It validates ENCODE-critical structure only. It deliberately does NOT check
// semantic value ranges (the command endpoint's job — see EncodeCommand), nor
// decode-only niceties the Decode path already degrades gracefully on (state
// prefixes are longest-prefix-wins and tolerate duplicates; serial defaults
// are validated when the port is opened).
//
// Checks:
//   - command names are non-empty and unique (lookupCommand returns the first
//     match, so a duplicate would silently shadow);
//   - command templates are non-empty — an empty Cmd encodes to zero bytes with
//     no error, which the serial client treats as a no-op success, so a safety
//     dry-run (e.g. TuneSupported / StartTune proving tx_off encodes) would pass
//     while the eventual write sends nothing (review 2026-06-06 M1);
//   - TX-capable / playback commands (tx_on, tx_off, PLAYBACK) are NOT Exposed —
//     these are controller-only (the tune controller keys TX via the guaranteed-
//     stop path, ADR 0027); exposing one would route it through the generic
//     command path, bypassing the mode/power snapshot + auto-off + release-on-
//     disconnect machinery (review 2026-06-06 M2);
//   - value-bearing command templates (those with a ValueMap, a Pad, or a %
//     verb) contain exactly one %s verb and no other format verb;
//   - Pad is non-negative;
//   - every command ValueMap names a marker Tag that exists and is non-empty
//     and injective on its Value side (valueCode picks the first matching
//     Value, so a duplicate would encode the wrong wire code);
//   - markers have Length > 0 and Index >= 0.
func ValidateRigDefinition(def RigDefinition) error {
	const op errors.Op = "cat.ValidateRigDefinition"

	seen := make(map[string]struct{}, len(def.Commands))
	for _, c := range def.Commands {
		if c.Name == "" {
			return errors.New(op).WithMsg("command with empty name")
		}
		if _, dup := seen[c.Name]; dup {
			return errors.New(op).WithMsgf("duplicate command name %q", c.Name)
		}
		seen[c.Name] = struct{}{}

		// An empty template encodes to nothing without error — invisible to every
		// caller that treats encode success as a capability proof, and dangerous
		// on the safety-critical tune unkey (M1).
		if c.Cmd == "" {
			return errors.New(op).WithMsgf("command %q has an empty template", c.Name)
		}

		// The TX/playback commands are controller-only by safety design; the
		// schema — not just the tests — must forbid a rigdef from exposing one (M2).
		if c.Exposed {
			if _, unsafe := unsafeExposableCommands[c.Name]; unsafe {
				return errors.New(op).WithMsgf(
					"command %q must not be exposed (TX/playback commands are controller-only, ADR 0027)", c.Name)
			}
		}

		if c.Pad < 0 {
			return errors.New(op).WithMsgf("command %q has negative pad %d", c.Name, c.Pad)
		}

		// Mirror EncodeCommand's valueless test: a command is valueless only
		// when it has no ValueMap, no Pad, and no % verb. Everything else fills
		// a %s, so it must have exactly one and nothing else fmt could trip on.
		valueBearing := c.ValueMap != "" || c.Pad > 0 || strings.Contains(c.Cmd, "%")
		if valueBearing {
			sCount, otherCount := countFormatVerbs(c.Cmd)
			if sCount != 1 || otherCount != 0 {
				return errors.New(op).WithMsgf(
					"command %q template %q must have exactly one %%s verb and no other format verb (got %d %%s, %d other)",
					c.Name, c.Cmd, sCount, otherCount)
			}
		}

		if c.ValueMap != "" {
			if err := validateValueMap(op, def, c.Name, c.ValueMap); err != nil {
				return err
			}
		}
	}

	for _, st := range def.States {
		for _, mk := range st.Markers {
			if mk.Length <= 0 {
				return errors.New(op).WithMsgf(
					"state %q marker %q has non-positive length %d", st.Prefix, mk.Tag, mk.Length)
			}
			if mk.Index < 0 {
				return errors.New(op).WithMsgf(
					"state %q marker %q has negative index %d", st.Prefix, mk.Tag, mk.Index)
			}
		}
	}

	return nil
}

// validateValueMap ensures the named value_map references a marker Tag that
// exists, carries at least one mapping, and is injective on its Value side, so
// the send-side inversion in valueCode is well-defined. Injectivity is checked
// across every marker carrying the tag, because valueCode scans them all.
func validateValueMap(op errors.Op, def RigDefinition, cmdName, tag string) error {
	values := make(map[string]struct{})
	found := false
	for _, st := range def.States {
		for _, mk := range st.Markers {
			if mk.Tag != tag {
				continue
			}
			found = true
			for _, vm := range mk.ValueMappings {
				if _, dup := values[vm.Value]; dup {
					return errors.New(op).WithMsgf(
						"command %q value_map %q is not injective: duplicate value %q", cmdName, tag, vm.Value)
				}
				values[vm.Value] = struct{}{}
			}
		}
	}
	if !found {
		return errors.New(op).WithMsgf("command %q value_map %q references no marker tag", cmdName, tag)
	}
	if len(values) == 0 {
		return errors.New(op).WithMsgf("command %q value_map %q has no value mappings", cmdName, tag)
	}
	return nil
}

// countFormatVerbs counts %s verbs and any other format verbs in a template,
// treating %% as a literal percent (not a verb) and a dangling trailing % as a
// non-%s verb. Used to enforce "exactly one %s, no other verb" on value-bearing
// command templates.
func countFormatVerbs(tmpl string) (sCount, otherCount int) {
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '%' {
			continue
		}
		if i+1 >= len(tmpl) {
			otherCount++ // dangling '%'
			break
		}
		switch tmpl[i+1] {
		case '%':
			i++ // escaped "%%" — a literal percent, not a verb
		case 's':
			sCount++
			i++
		default:
			otherCount++ // %d, %03d, %v, … — anything that isn't %s
			i++
		}
	}
	return sCount, otherCount
}
