package cat

import (
	"bytes"
	stderr "errors"
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Status is the decoded-tag map produced by Decode. Alias for
// map[string]string so callers can pass literal maps interchangeably
// without conversion.
type Status = map[string]string

// ErrNoMatch is returned (wrapped) by Decode when the input line's head
// does not case-insensitively match any prefix in the rig definition's
// State table. Use errors.Is(err, cat.ErrNoMatch) to detect.
//
// Callers running a continuous read loop against a rig should treat
// ErrNoMatch as a normal condition — a stray byte, a command type the
// rig emits that we don't know how to parse, or an AI broadcast from a
// state we haven't described yet. It is not a transport error.
var ErrNoMatch = stderr.New("cat: no state matches input")

// ErrUnknownCommand is returned (wrapped) by Encode when the named
// command is not present in the rig definition's Commands table.
var ErrUnknownCommand = stderr.New("cat: unknown command")

// Decode parses one framed CAT line (terminator already stripped by the
// serial reader) against the given rig definition and returns a tag map
// of extracted fields. Behaviour matches v1's lineProcessor + lookup
// pair byte-for-byte, pinned by the fixture tests in
// decode_fixtures_test.go.
//
// Prefix matching is case-insensitive and longest-prefix-wins: the state
// with the longest prefix that matches the head of line wins. Marker
// bounds are clamped: markers whose Index is out of range are skipped,
// markers whose end overruns the tail are clamped to the end, markers
// that collapse to an empty slice are skipped. Value-mapped markers
// produce the empty string when the sliced value is not in the mapping
// table (v1 quirk, preserved).
//
// Returns an error wrapping ErrNoMatch when no prefix matches; an empty
// (but non-nil) Status when a prefix matches but all markers were
// skipped or clamped away (e.g. "FA" with no tail — v1 quirk).
func Decode(def RigDefinition, line []byte) (Status, error) {
	const op errors.Op = "cat.Decode"

	state, tail, ok := lookupState(line, def.States)
	if !ok {
		return nil, errors.New(op).WithErr(ErrNoMatch)
	}

	status := Status{}
	for _, m := range state.Markers {
		if m.Index < 0 || m.Index >= len(tail) {
			continue
		}
		end := m.Index + m.Length
		if end > len(tail) {
			end = len(tail)
		}
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
	return status, nil
}

// Encode produces the wire bytes for a named command from the rig
// definition's command table. If args are supplied, they substitute into
// any Go fmt verbs in the Cmd template (v1's convention restricts these
// to %s — see v1 internal/cat/helpers.go validateCommandFormat). With no
// args the template is returned verbatim as bytes.
//
// Returns an error wrapping ErrUnknownCommand when the name is not
// present in def.Commands.
func Encode(def RigDefinition, name string, args ...any) ([]byte, error) {
	const op errors.Op = "cat.Encode"

	for _, c := range def.Commands {
		if c.Name != name {
			continue
		}
		if len(args) == 0 {
			return []byte(c.Cmd), nil
		}
		return []byte(fmt.Sprintf(c.Cmd, args...)), nil
	}
	return nil, errors.New(op).WithErr(ErrUnknownCommand).WithMsgf("unknown command %q", name)
}

// lookupState finds the state whose Prefix is the longest case-insensitive
// match for the head of line, and returns it along with the tail of line
// (everything after the prefix). Matches v1's lookupCatState exactly.
func lookupState(line []byte, states []State) (State, string, bool) {
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
