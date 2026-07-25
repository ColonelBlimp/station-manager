package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadErrText writes body to a temp config.json, Loads it, and returns the
// operator-visible error string (what `smd: %v` prints on stderr).
func loadErrText(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatalf("Load accepted a malformed config:\n%s", body)
	}
	return err.Error()
}

// A malformed config.json is a routine hand-edit event that stops the daemon
// before logging exists, so the message on stderr is the operator's ONLY guide.
// Each case asserts the three things that make it actionable: which file, which
// line, and what to do about it — none of which encoding/json supplies on its
// own (it gives a byte offset and prose about Go types).
func TestLoad_MalformedConfigIsActionable(t *testing.T) {
	t.Run("trailing comma points at the comma, not the brace", func(t *testing.T) {
		// json blames the closing brace on line 5; the character to DELETE is the
		// comma on line 4, which is where the caret has to land.
		got := loadErrText(t, "{\n  \"version\": 2,\n  \"logging_station\": {\n    \"station_callsign\": \"7Q5MLV\",\n  }\n}\n")
		for _, want := range []string{
			"config.json",        // which file — WorkingDir resolves it 3 ways
			"line 4",             // the comma's line, not the brace's line 5
			`"station_callsign"`, // the offending line is quoted back
			"^",                  // with a caret under the character
			"trailing comma",     // and the fix named
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("message missing %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "line 5") {
			t.Fatalf("caret should point at the comma (line 4), not the brace:\n%s", got)
		}
		assertCaretOn(t, got, ',')
	})

	t.Run("truncated file says it ends early", func(t *testing.T) {
		got := loadErrText(t, "{\n  \"version\": 2,\n  \"logging_station\": {\n")
		if !strings.Contains(got, "ends early") {
			t.Fatalf("no ends-early hint:\n%s", got)
		}
		if !strings.Contains(got, "config.json") {
			t.Fatalf("message does not name the file:\n%s", got)
		}
	})

	t.Run("empty file says so plainly", func(t *testing.T) {
		got := loadErrText(t, "   \n")
		if !strings.Contains(got, "is empty") {
			t.Fatalf("an empty config should say so, not report a parse offset:\n%s", got)
		}
	})

	t.Run("wrong type names the field in JSON terms", func(t *testing.T) {
		got := loadErrText(t, "{\n  \"version\": 2,\n  \"logging_station\": { \"station_callsign\": 123 }\n}\n")
		for _, want := range []string{"logging_station.station_callsign", "a string (in quotes)", "got number", "line 3"} {
			if !strings.Contains(got, want) {
				t.Fatalf("message missing %q:\n%s", want, got)
			}
		}
		// The Go-internal phrasing must not survive to the operator.
		for _, leak := range []string{"cannot unmarshal", "Go struct field", "LoggingStation"} {
			if strings.Contains(got, leak) {
				t.Fatalf("message leaked Go internals (%q):\n%s", leak, got)
			}
		}
	})

	t.Run("top-level array explains the required shape", func(t *testing.T) {
		got := loadErrText(t, "[1,2,3]\n")
		if !strings.Contains(got, "must contain a JSON object") {
			t.Fatalf("message does not explain the required shape:\n%s", got)
		}
	})
}

// Non-JSON load failures must keep their own wording — describeJSONError returns
// nil for them so the caller's existing wrapping stands. The version downgrade
// guard is the live example.
func TestLoad_NonJSONFailuresKeepTheirMessage(t *testing.T) {
	got := loadErrText(t, `{"version": 9999}`)
	if !strings.Contains(got, "newer than this Station Manager supports") {
		t.Fatalf("downgrade guard message was swallowed:\n%s", got)
	}
	if strings.Contains(got, "not valid JSON") {
		t.Fatalf("a well-formed file was reported as a syntax error:\n%s", got)
	}
}

// A valid config must be unaffected — the diagnostics sit only on error paths.
func TestLoad_ValidConfigUnaffected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"version":2,"logging_station":{"station_callsign":"7Q5MLV"}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load rejected a valid config: %v", err)
	}
	if cfg.LoggingStation.StationCallsign != "7Q5MLV" {
		t.Fatalf("callsign = %q", cfg.LoggingStation.StationCallsign)
	}
}

// lineCol and snippet handle the awkward inputs directly: an offset at EOF (the
// truncated-file case), and a tab-indented file, where the caret must line up
// with the tabs as PRINTED (expanded) rather than as stored.
func TestLineColAndSnippetEdgeCases(t *testing.T) {
	src := []byte("{\n\t\"a\": 1\n}")
	// The final byte is the closing brace: line 3, column 1.
	if line, col := lineCol(src, len(src)-1); line != 3 || col != 1 {
		t.Fatalf("last byte → line %d col %d, want 3/1", line, col)
	}
	// An index past the end must clamp rather than panic.
	if line, _ := lineCol(src, 9999); line != 3 {
		t.Fatalf("clamped line = %d, want 3", line)
	}
	// Byte column 2 of "\t\"a\": 1" is the quote. Once the tab is printed as four
	// spaces the quote sits at display column 5, so the caret must too — measuring
	// on the raw byte column would bury it in the indentation.
	text, caret, _, ok := snippet(src, 2, 2)
	if !ok {
		t.Fatal("snippet not produced for a tab-indented line")
	}
	if strings.Contains(text, "\t") {
		t.Fatalf("tabs must be expanded for display: %q", text)
	}
	if caret != "    ^" {
		t.Fatalf("caret = %q, want 4 spaces then ^ (aligned with the expanded tab)", caret)
	}
	if idx := strings.IndexByte(caret, '^'); idx >= len(text) || text[idx] != '"' {
		t.Fatalf("caret at index %d does not land on the quote in %q", idx, text)
	}
}

// assertCaretOn checks the caret row lines up with the expected character in the
// quoted snippet above it. Asserting only that a caret EXISTS let an off-by-one
// through the first review round — the output looked right at a glance.
func assertCaretOn(t *testing.T, msg string, want byte) {
	t.Helper()
	lines := strings.Split(msg, "\n")
	for i, ln := range lines {
		caretAt := strings.IndexByte(ln, '^')
		if caretAt < 0 || !strings.HasPrefix(strings.TrimSpace(ln), "|") && !strings.Contains(ln, "|") {
			continue
		}
		if i == 0 {
			continue
		}
		src := lines[i-1]
		if caretAt >= len(src) {
			t.Fatalf("caret at column %d is past the end of %q", caretAt, src)
		}
		if src[caretAt] != want {
			t.Fatalf("caret points at %q, want %q\n  %s\n  %s", src[caretAt], want, src, ln)
		}
		return
	}
	t.Fatalf("no caret row found in:\n%s", msg)
}

// TestLoad_SnippetRedactsCredentials guards the disclosure the snippet opened.
// config.json is written 0600 BECAUSE it holds SMTP and lookup-provider
// passwords and forwarder credentials; this message goes to stderr (→ the
// journal) and is mirrored into smd.log at 0644. Quoting the raw line would copy
// a secret out of the protected file into a world-readable one.
func TestLoad_SnippetRedactsCredentials(t *testing.T) {
	const secret = "hunter2-smtp-password"
	got := loadErrText(t, "{\n  \"version\": 2,\n  \"smtp\": {\n    \"password\": \""+secret+"\",\n  }\n}\n")

	if strings.Contains(got, secret) {
		t.Fatalf("the snippet leaked a credential into a message bound for smd.log (0644):\n%s", got)
	}
	// The line must stay identifiable: keys are not secrets and are what tell the
	// operator WHICH line to open.
	if !strings.Contains(got, `"password"`) {
		t.Fatalf("redaction removed the key too, leaving the line unidentifiable:\n%s", got)
	}
	if !strings.Contains(got, "values shown as") {
		t.Fatalf("redaction is not explained, so the asterisks look like corruption:\n%s", got)
	}
	// Redaction preserves length, so the caret must still land on the comma.
	assertCaretOn(t, got, ',')
}

// TestLoad_TrailingJunkIsNotCalledTruncation: a fault on the LAST byte is not
// proof the document ended early. A complete document followed by junk faults
// there too, and the ends-early hint would send the operator hunting for an
// unclosed bracket that does not exist.
func TestLoad_TrailingJunkIsNotCalledTruncation(t *testing.T) {
	got := loadErrText(t, `{"version":2}?`)
	if strings.Contains(got, "ends early") {
		t.Fatalf("a structurally complete document was reported as truncated:\n%s", got)
	}
	// The `?` is at 0-based index 13 → column 14. It is itself redacted (it is
	// neither structure nor a key, and redaction fails closed), so assert the
	// POSITION rather than the character — the character is still named for the
	// operator by encoding/json's own text in the headline.
	if !strings.Contains(got, "column 14") {
		t.Fatalf("caret is not on the offending character (column 14):\n%s", got)
	}
	if !strings.Contains(got, "'?'") {
		t.Fatalf("the offending character is not identified anywhere in the message:\n%s", got)
	}
	assertCaretOn(t, got, redactChar)
}

// looksTruncated is the structural test behind that hint: it must key on unclosed
// brackets/strings, not on where the parser happened to stop.
func TestLooksTruncated(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{`{"a":1}`, false},
		{`{"a":1}?`, false},    // complete, then junk
		{`{"a":{`, true},       // unclosed object
		{`{"a":"b`, true},      // unterminated string
		{`{"a":"}"}`, false},   // brace inside a string is data, not structure
		{`{"a":"x\""}`, false}, // escaped quote must not flip string state
		{`[1,2`, true},         // unclosed array
	}
	for _, c := range cases {
		if got := looksTruncated([]byte(c.src)); got != c.want {
			t.Errorf("looksTruncated(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestRedactLine_FailsClosed pins the allowlist. The first version hunted for
// string VALUES and blanked them — a blacklist — and on the only input this code
// ever sees (malformed JSON) it leaked three ways. Each case here is one of those
// bypasses; the rule is that anything whose role is not provable gets blanked.
func TestRedactLine_FailsClosed(t *testing.T) {
	const secret = "hunter2"
	cases := []struct {
		name string
		line string
		keep []string // structure/keys that must survive to keep the line usable
	}{
		{
			// A stray colon after the VALUE satisfied a lone "followed by ':'"
			// key test, so the secret was preserved as though it were a key.
			name: "value followed by a stray colon",
			line: `{"version":2,"smtp":{"password":"` + secret + `":}}`,
			keep: []string{`"password"`, `"smtp"`, "{", "}"},
		},
		{
			// Only double-quoted strings were scanned, so a JS/YAML-habit quote
			// style passed through untouched.
			name: "single-quoted value",
			line: `  'password': '` + secret + `',`,
			keep: []string{":", ","},
		},
		{
			// Unquoted values were never considered at all.
			name: "bare unquoted value",
			line: `  "password": ` + secret + `,`,
			keep: []string{`"password"`, ":", ","},
		},
		{
			// An array element is not a key however it is punctuated.
			name: "array element",
			line: `  ["` + secret + `", "tok-bbb",]`,
			keep: []string{"[", "]", ","},
		},
		{
			// No closing quote means the role cannot be established either way.
			name: "unterminated string",
			line: `  "password": "` + secret,
			keep: []string{":"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := redactLine(c.line, false)
			if strings.Contains(got, secret) {
				t.Fatalf("secret survived redaction:\n  in : %s\n  out: %s", c.line, got)
			}
			if !changed {
				t.Fatalf("redaction reported no change but should have blanked something: %s", got)
			}
			if len(got) != len(c.line) {
				t.Fatalf("length changed (%d → %d), which would misalign the caret", len(c.line), len(got))
			}
			for _, k := range c.keep {
				if !strings.Contains(got, k) {
					t.Fatalf("structure %q did not survive, leaving the line unreadable:\n  %s", k, got)
				}
			}
		})
	}
}

// The counterweight: a well-formed line must keep every key, or the snippet stops
// telling the operator which field to look at.
func TestRedactLine_KeepsKeysOnWellFormedLines(t *testing.T) {
	got, _ := redactLine(`    "station_callsign": "7Q5MLV",`, false)
	if !strings.Contains(got, `"station_callsign"`) {
		t.Fatalf("key was blanked: %s", got)
	}
	if strings.Contains(got, "7Q5MLV") {
		t.Fatalf("value survived: %s", got)
	}
}

// TestRedactLine_StructuralCharsInsideValues: punctuation is only structure when
// it is OUTSIDE a string. Marking `{ } [ ] : ,` safe by CHARACTER let a password
// made of them survive whole, and disclosed the punctuation of an ordinary one in
// place — enough to narrow a guess considerably.
func TestRedactLine_StructuralCharsInsideValues(t *testing.T) {
	cases := []struct{ name, line, secret string }{
		{"password of pure punctuation", `{"password":"[{,:}]":}`, `[{,:}]`},
		{"punctuation inside a normal password", `  "password": "p@ss,word{1}",`, `p@ss,word{1}`},
		{"comma inside a value", `  "token": "aaa,bbb",`, `aaa,bbb`},
		{"braces inside a value", `  "url": "https://h/{x}",`, `{x}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := redactLine(c.line, false)
			if strings.Contains(got, c.secret) {
				t.Fatalf("value content survived:\n  in : %s\n  out: %s", c.line, got)
			}
			if len(got) != len(c.line) {
				t.Fatalf("length changed (%d → %d), which would misalign the caret", len(c.line), len(got))
			}
		})
	}
}

// A value can open on one line and close on a later one, so a snippet line must
// inherit the string state its predecessors left open — otherwise the middle of a
// multi-line value is treated as fresh structure and printed.
func TestRedactLine_InheritsOpenStringState(t *testing.T) {
	got, _ := redactLine(`secret{,:}continues",`, true)
	if strings.Contains(got, "secret") || strings.Contains(got, "{,:}") {
		t.Fatalf("content of a string opened on an earlier line was printed: %s", got)
	}
}

// stringStateAt is what supplies that inherited state.
func TestStringStateAt(t *testing.T) {
	src := []byte("{\n  \"a\": \"open\n  more\",\n}")
	// Offset 0 and the start of line 2 are outside any string.
	if stringStateAt(src, 0) {
		t.Fatal("start of file reported as inside a string")
	}
	// Byte offset of line 3 ("  more\",") — the value opened on line 2 is still open.
	line3 := strings.Index(string(src), "  more")
	if !stringStateAt(src, line3) {
		t.Fatal("line inside an unclosed string reported as outside")
	}
	// An escaped quote must not flip the state.
	if stringStateAt([]byte(`"a\"b"`), 6) {
		t.Fatal("escaped quote miscounted, leaving the state inside a string")
	}
}
