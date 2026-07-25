package config

import (
	"bytes"
	"encoding/json"
	stderr "errors"
	"fmt"
	"strings"
)

// Config-load diagnostics.
//
// config.json is the one file operators edit by hand — the SPA covers only part
// of it, and the runbooks tell you to open it — so a syntax slip there is a
// routine event, not an exotic one. It is also the WORST place to fail badly:
// the error happens before the logging service exists, so it reaches only
// stderr (and, best-effort, smd.log via logStartupFailure), and it stops the
// daemon dead. What encoding/json gives us on its own is a byte offset and
// prose about Go types:
//
//	smd: migrating config: parsing config document: invalid character '}' looking for beginning of object key string
//	smd: parsing config file: json: cannot unmarshal number into Go struct field LoggingStation.logging_station.station_callsign of type string
//
// Neither names the file (WorkingDir resolves it from three places, so "which
// config.json?" is a real question), neither gives a line, and the second leaks
// Go type names at someone who just wants to know which line to fix.
//
// describeJSONError converts the offset into line/column, quotes the offending
// line, and adds a hint for the mistake that dominates hand edits — a trailing
// comma, which JSON forbids and almost every other config format allows.

// maxSnippetLen bounds the quoted line so a minified or machine-written
// config.json can't dump kilobytes onto the terminal.
const maxSnippetLen = 120

// describeJSONError renders a JSON syntax or type error against src as an
// operator-facing message, or returns nil when err is neither — so callers can
// fall through to their existing wrapping for non-JSON failures (a version
// downgrade guard, a missing migration).
//
// src must be the exact bytes that produced err. hasOffsets says whether those
// bytes are still the file on disk: after a config migration the document is
// re-marshalled, so its offsets no longer point at anything the operator can
// see, and the line/column and snippet are suppressed rather than made up.
func describeJSONError(path string, src []byte, hasOffsets bool, err error) error {
	if err == nil {
		return nil
	}
	if len(bytes.TrimSpace(src)) == 0 {
		return fmt.Errorf("%s is empty — it must contain a JSON object, e.g. {}", path)
	}

	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case stderr.As(err, &syn):
		// point may move the caret off the reported fault: for a trailing comma
		// json blames the closing brace, but the character to DELETE is the comma,
		// possibly lines earlier. Pointing at the fix beats pointing at the symptom.
		hint, point := syntaxHint(src, syn.Offset)
		return &loadError{
			path:     path,
			headline: fmt.Sprintf("%s is not valid JSON: %s", path, syn.Error()),
			src:      src, offset: point, hasOffsets: hasOffsets,
			hint: hint,
		}
	case stderr.As(err, &typ):
		return &loadError{
			path:     path,
			headline: fmt.Sprintf("%s has a value of the wrong type: %s", path, typeFault(typ)),
			src:      src, offset: typ.Offset, hasOffsets: hasOffsets,
		}
	}
	return nil
}

// typeFault describes an UnmarshalTypeError without Go type names. A nested
// field carries its JSON path (logging_station.station_callsign); an empty
// Field means the fault is the document's top-level shape.
func typeFault(e *json.UnmarshalTypeError) string {
	if e.Field == "" {
		return fmt.Sprintf("the file must contain a JSON object ({ … }), but starts with %s", e.Value)
	}
	return fmt.Sprintf("%q expects %s, got %s", e.Field, goTypeToJSON(e.Type.String()), e.Value)
}

// goTypeToJSON names a Go type the way it appears in JSON, so the message talks
// about the file the operator is editing rather than the struct behind it.
func goTypeToJSON(goType string) string {
	switch {
	case goType == "string":
		return "a string (in quotes)"
	case goType == "bool":
		return "true or false"
	case strings.HasPrefix(goType, "int"), strings.HasPrefix(goType, "uint"),
		strings.HasPrefix(goType, "float"):
		return "a number (no quotes)"
	case strings.HasPrefix(goType, "[]"):
		return "a list ([ … ])"
	case strings.HasPrefix(goType, "map"), strings.HasPrefix(goType, "config."),
		strings.HasPrefix(goType, "types."):
		return "an object ({ … })"
	}
	return goType
}

// syntaxHint recognises the hand-edit mistakes worth naming. Derived from the
// DOCUMENT, not from matching encoding/json's message text — that prose is not
// a stable contract, and a Go release rewording it must not silently drop the
// hint.
// It also returns where the caret should point, which is not always the offset
// json reported: for a trailing comma the parser blames the closing bracket,
// while the character the operator must delete is the comma before it.
func syntaxHint(src []byte, offset int64) (hint string, point int64) {
	// A trailing comma: the last meaningful character before the fault is a
	// comma, and the fault itself is a closing bracket. JSON forbids this while
	// most config formats allow it, so it is the single most common slip.
	i := int(offset) - 1
	if i > len(src) {
		i = len(src)
	}
	var at byte
	if int(offset)-1 >= 0 && int(offset)-1 < len(src) {
		at = src[offset-1]
	}
	for i--; i >= 0; i-- {
		if c := src[i]; c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			if c == ',' && (at == '}' || at == ']') {
				// +1 so lineCol's 1-based column lands ON the comma.
				return "JSON does not allow a trailing comma before } or ]", int64(i) + 1
			}
			break
		}
	}
	if offset >= int64(len(src)) {
		return "the file ends early — a { [ or \" is probably never closed", offset
	}
	return "", offset
}

// loadError carries the pieces so Error() can render them together; keeping it
// a type (rather than pre-formatting) lets tests assert on the parts.
type loadError struct {
	path       string
	headline   string
	src        []byte
	offset     int64
	hasOffsets bool
	hint       string
}

func (e *loadError) Error() string {
	var b strings.Builder
	b.WriteString(e.headline)
	if e.hasOffsets {
		line, col := lineCol(e.src, e.offset)
		fmt.Fprintf(&b, "\n  at line %d, column %d", line, col)
		if snip, caret, ok := snippet(e.src, line, col); ok {
			fmt.Fprintf(&b, "\n\n    %d | %s\n      | %s", line, snip, caret)
		}
	}
	if e.hint != "" {
		fmt.Fprintf(&b, "\n\n  hint: %s", e.hint)
	}
	return b.String()
}

// lineCol converts a byte offset into 1-based line and column. An offset at or
// past the end (the "ends early" case) reports the final position.
func lineCol(src []byte, offset int64) (line, col int) {
	if offset > int64(len(src)) {
		offset = int64(len(src))
	}
	line, lineStart := 1, 0
	for i := 0; i < int(offset); i++ {
		if src[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, int(offset) - lineStart + 1
}

// snippet returns the 1-based line's text (tabs expanded so the caret lines up)
// and a caret row pointing at col.
func snippet(src []byte, line, col int) (text, caret string, ok bool) {
	const tabWidth = "    "

	lines := strings.Split(string(src), "\n")
	if line < 1 || line > len(lines) {
		return "", "", false
	}
	raw := strings.TrimRight(lines[line-1], "\r")
	if strings.TrimSpace(raw) == "" {
		return "", "", false
	}
	if col > len(raw)+1 {
		col = len(raw) + 1
	}
	// col is a BYTE column, but the line is printed with tabs expanded — so the
	// caret's position has to be measured on the expanded PREFIX, not on col.
	// Expanding only the display line (and trusting col) puts the caret inside
	// the indentation of any tab-indented file.
	text = strings.ReplaceAll(raw, "\t", tabWidth)
	caretCol := len(strings.ReplaceAll(raw[:col-1], "\t", tabWidth))
	if len(text) > maxSnippetLen {
		text = text[:maxSnippetLen] + " …"
		if caretCol > maxSnippetLen {
			return text, "", true
		}
	}
	return text, strings.Repeat(" ", caretCol) + "^", true
}
