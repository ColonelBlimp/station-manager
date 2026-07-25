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

// redactChar replaces the content of string VALUES in a quoted snippet. Single
// byte on purpose: the caret is positioned by byte width, so a multi-byte glyph
// would shift it off the character it points at.
const redactChar = '*'

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
		// idx may move the caret off the reported fault: for a trailing comma json
		// blames the closing brace, but the character to DELETE is the comma,
		// possibly lines earlier. Pointing at the fix beats pointing at the symptom.
		hint, idx := syntaxHint(src, faultIndex(syn.Offset))
		return &loadError{
			path:     path,
			headline: fmt.Sprintf("%s is not valid JSON: %s", path, syn.Error()),
			src:      src, index: idx, hasOffsets: hasOffsets,
			hint: hint,
		}
	case stderr.As(err, &typ):
		return &loadError{
			path:     path,
			headline: fmt.Sprintf("%s has a value of the wrong type: %s", path, typeFault(typ)),
			src:      src, index: faultIndex(typ.Offset), hasOffsets: hasOffsets,
		}
	}
	return nil
}

// faultIndex converts encoding/json's Offset into a 0-based index of the
// OFFENDING byte. Offset counts through that byte (it is the position just
// after it), so using it directly puts the caret one column late — invisible in
// a casual read of the output, which is how it survived the first round.
func faultIndex(offset int64) int {
	if offset <= 0 {
		return 0
	}
	return int(offset) - 1
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
func syntaxHint(src []byte, idx int) (hint string, point int) {
	if idx >= len(src) {
		idx = len(src) - 1
	}
	if idx < 0 {
		idx = 0
	}
	var at byte
	if idx < len(src) {
		at = src[idx]
	}
	// A trailing comma: the last meaningful character before the fault is a
	// comma, and the fault itself is a closing bracket. JSON forbids this while
	// most config formats allow it, so it is the single most common slip.
	for i := idx - 1; i >= 0; i-- {
		if c := src[i]; c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			if c == ',' && (at == '}' || at == ']') {
				return "JSON does not allow a trailing comma before } or ]", i
			}
			break
		}
	}
	// "Ends early" must mean STRUCTURALLY incomplete, not merely "the fault is at
	// the last byte" — trailing junk after a complete document (`{"version":2}?`)
	// also faults on the final byte and would be sent chasing an unclosed bracket
	// that isn't there.
	if looksTruncated(src) {
		return "the file ends early — a { [ or \" is probably never closed", idx
	}
	return "", idx
}

// looksTruncated reports whether the document ends with something still open —
// an unterminated string, or more { [ than } ]. Scanned rather than inferred
// from the fault position, and string-aware so a brace inside a value doesn't
// skew the depth.
func looksTruncated(src []byte) bool {
	depth := 0
	inString, escaped := false, false
	for _, c := range src {
		switch {
		case escaped:
			escaped = false
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// Punctuation inside a string is data, not structure.
		case c == '{', c == '[':
			depth++
		case c == '}', c == ']':
			depth--
		}
	}
	return inString || depth > 0
}

// redactLine blanks everything on a snippet line that is not PROVABLY safe to
// print, keeping only JSON structure and unambiguous key names.
//
// config.json is written 0600 because it holds SMTP and lookup-provider
// passwords and forwarder credentials; this snippet, by contrast, goes to stderr
// (→ the journal) and is mirrored into smd.log at 0644. Quoting the raw line
// would copy a secret out of a private file into a world-readable one whenever
// the syntax error landed on a credential line.
//
// It is an ALLOWLIST, and that is the whole point. The first version hunted for
// string values and blanked them — a blacklist — which on the only input this
// code ever sees (malformed JSON) leaked three different ways: a value followed
// by a stray colon passed as a key (`{"password":"hunter2":}`), single-quoted
// values were not double-quoted so were skipped entirely, and bare unquoted
// values were never considered at all. Ambiguity has to fail CLOSED, so anything
// not recognised is redacted rather than the reverse.
//
// Survives: structural punctuation, whitespace, and a double-quoted string that
// is unambiguously a key — preceded by `{`, `,` or line start AND followed by
// `:`. Checking BOTH sides matters: a lone "followed by ':'" test is what let the
// stray-colon value through. Everything else — values of every quoting style,
// numbers, bare tokens, unterminated strings — is blanked.
//
// Byte length is preserved exactly so the caret still lands on the character it
// describes.
func redactLine(line string) (string, bool) {
	out := []byte(line)
	keep := make([]bool, len(out))
	for i, c := range out {
		switch c {
		case '{', '}', '[', ']', ':', ',', ' ', '\t':
			keep[i] = true
		}
	}
	for i := 0; i < len(out); {
		if out[i] != '"' {
			i++
			continue
		}
		open, j := i, i+1
		for j < len(out) {
			if out[j] == '\\' {
				j += 2
				continue
			}
			if out[j] == '"' {
				break
			}
			j++
		}
		if j >= len(out) {
			break // unterminated: role unknowable, so leave it unmarked → redacted
		}
		p := open - 1
		for p >= 0 && (out[p] == ' ' || out[p] == '\t') {
			p--
		}
		precededOK := p < 0 || out[p] == '{' || out[p] == ','
		k := j + 1
		for k < len(out) && (out[k] == ' ' || out[k] == '\t') {
			k++
		}
		if precededOK && k < len(out) && out[k] == ':' {
			for m := open; m <= j; m++ {
				keep[m] = true
			}
		}
		i = j + 1
	}
	changed := false
	for i := range out {
		if !keep[i] {
			out[i] = redactChar
			changed = true
		}
	}
	return string(out), changed
}

// loadError carries the pieces so Error() can render them together; keeping it
// a type (rather than pre-formatting) lets tests assert on the parts.
type loadError struct {
	path       string
	headline   string
	src        []byte
	index      int // 0-based byte index of the offending character
	hasOffsets bool
	hint       string
}

func (e *loadError) Error() string {
	var b strings.Builder
	b.WriteString(e.headline)
	if e.hasOffsets {
		line, col := lineCol(e.src, e.index)
		fmt.Fprintf(&b, "\n  at line %d, column %d", line, col)
		if snip, caret, redacted, ok := snippet(e.src, line, col); ok {
			fmt.Fprintf(&b, "\n\n    %d | %s\n      | %s", line, snip, caret)
			if redacted {
				b.WriteString("\n\n  (values shown as " + string(redactChar) +
					" — this message reaches the log and journal, config.json does not)")
			}
		}
	}
	if e.hint != "" {
		fmt.Fprintf(&b, "\n\n  hint: %s", e.hint)
	}
	return b.String()
}

// lineCol converts a 0-BASED byte index into 1-based line and column — the
// position of src[idx] itself, not of the byte after it.
func lineCol(src []byte, idx int) (line, col int) {
	if idx >= len(src) {
		idx = len(src) - 1
	}
	if idx < 0 {
		return 1, 1
	}
	line, lineStart := 1, 0
	for i := 0; i < idx; i++ {
		if src[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return line, idx - lineStart + 1
}

// snippet returns the 1-based line's text (tabs expanded so the caret lines up)
// and a caret row pointing at col.
func snippet(src []byte, line, col int) (text, caret string, redacted, ok bool) {
	const tabWidth = "    "

	lines := strings.Split(string(src), "\n")
	if line < 1 || line > len(lines) {
		return "", "", false, false
	}
	raw := strings.TrimRight(lines[line-1], "\r")
	if strings.TrimSpace(raw) == "" {
		return "", "", false, false
	}
	if col > len(raw)+1 {
		col = len(raw) + 1
	}
	// Redact BEFORE expanding tabs: redaction preserves byte length exactly, so
	// the caret arithmetic below is unaffected by it.
	raw, redacted = redactLine(raw)
	// col is a BYTE column, but the line is printed with tabs expanded — so the
	// caret's position has to be measured on the expanded PREFIX, not on col.
	// Expanding only the display line (and trusting col) puts the caret inside
	// the indentation of any tab-indented file.
	text = strings.ReplaceAll(raw, "\t", tabWidth)
	caretCol := len(strings.ReplaceAll(raw[:col-1], "\t", tabWidth))
	if len(text) > maxSnippetLen {
		text = text[:maxSnippetLen] + " …"
		if caretCol > maxSnippetLen {
			return text, "", redacted, true
		}
	}
	return text, strings.Repeat(" ", caretCol) + "^", redacted, true
}
