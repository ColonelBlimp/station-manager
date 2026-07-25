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
	if line, col := lineCol(src, int64(len(src))); line != 3 || col != 2 {
		t.Fatalf("EOF offset → line %d col %d, want 3/2", line, col)
	}
	// Offset past the end must clamp rather than panic.
	if line, _ := lineCol(src, 9999); line != 3 {
		t.Fatalf("clamped line = %d, want 3", line)
	}
	// Byte column 2 of "\t\"a\": 1" is the quote. Once the tab is printed as four
	// spaces the quote sits at display column 5, so the caret must too — measuring
	// on the raw byte column would bury it in the indentation.
	text, caret, ok := snippet(src, 2, 2)
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
