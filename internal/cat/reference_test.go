package cat

import (
	"bytes"
	"fmt"
)

// This file holds a frozen reference implementation of v1's CAT decode
// pipeline. It exists as the acceptance criteria for the §4 carve-out
// in docs/v2-design/cat-serial-reuse.md: when cat.Decode is written, it
// must produce the same outputs as referenceDecode for the fixtures in
// decode_fixtures_test.go.
//
// Logic mirrors v1 byte-for-byte:
//
//   - Lookup: longest-prefix-wins, case-insensitive, against a caller-
//     supplied list of State entries. See v1 internal/cat/internal.go
//     (lookupCatState).
//   - Decode: walk markers in order, extract tail[Index:Index+Length] per
//     marker, apply ValueMappings if present (empty-string fallback on
//     no match — v1 quirk, preserved). See v1 internal/cat/processor.go
//     (lineProcessor).
//
// Bounds handling matches v1: markers with Index out of range are skipped,
// markers whose end exceeds the tail are clamped, markers where start >=
// end after clamping are skipped.
//
// Do NOT rewrite to use cat.Decode once that exists. The reference is
// frozen on purpose.

// referenceLookup picks the longest prefix in states that case-
// insensitively matches the head of line. Returns the matched state and
// the tail (line minus the matched prefix). Boolean false when no prefix
// matches.
func referenceLookup(line []byte, states []State) (State, string, bool) {
	if len(line) == 0 {
		return State{}, "", false
	}
	var best State
	bestLen := -1
	for _, s := range states {
		pl := len(s.Prefix)
		if pl == 0 || pl > len(line) {
			continue
		}
		if !bytes.EqualFold(line[:pl], []byte(s.Prefix)) {
			continue
		}
		if pl > bestLen {
			best = s
			bestLen = pl
		}
	}
	if bestLen < 0 {
		return State{}, "", false
	}
	return best, string(line[bestLen:]), true
}

// referenceEncode is the send-path mirror of v1. Given a rig definition
// and a high-level command name, it finds the matching Command entry and
// applies fmt.Sprintf to its Cmd template with args. v1 rejected %d / %f
// verbs and only permitted %s (see v1 internal/cat/helpers.go
// validateCommandFormat); we preserve that contract at encode time by
// simply using fmt.Sprintf — non-%s verbs will produce malformed output
// that callers can detect in validation (out of scope for the reference).
// Returns an error if no command with the given name is registered.
func referenceEncode(def RigDefinition, name string, args ...any) (string, error) {
	for _, c := range def.Commands {
		if c.Name != name {
			continue
		}
		if len(args) == 0 {
			return c.Cmd, nil
		}
		return fmt.Sprintf(c.Cmd, args...), nil
	}
	return "", fmt.Errorf("unknown command %q", name)
}

// referenceDecode walks state.Markers and builds v1's CatStatus map from
// tail. Out-of-range markers skipped; long-end markers clamped; empty
// slices skipped; ValueMappings with no match produce "".
func referenceDecode(state State, tail string) map[string]string {
	status := map[string]string{}
	for _, m := range state.Markers {
		if m.Index < 0 || m.Index >= len(tail) {
			continue
		}
		end := min(m.Index+m.Length, len(tail))
		if m.Index >= end {
			continue
		}
		slice := tail[m.Index:end]
		if len(m.ValueMappings) == 0 {
			status[m.Tag] = slice
			continue
		}
		mapped := ""
		for _, vm := range m.ValueMappings {
			if slice == vm.Key {
				mapped = vm.Value
				break
			}
		}
		status[m.Tag] = mapped
	}
	return status
}
