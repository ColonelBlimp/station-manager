package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
	"github.com/ColonelBlimp/station-manager/internal/config"
)

// Acceptance criteria for Diff C — SHIP GATE item (d), build-version stamping
// (docs/backlog.md; docs/reviews/forwarding-logging-gaps.md F1 decided the cost
// alongside it). Operator's wording, 2026-08-01:
//
//	1. Every record emitted by a build containing this change names the build that
//	   wrote it. A frequency counted across a rotated file is attributable without
//	   replaying startup markers, and a -dirty development build is
//	   distinguishable from the tagged build it derives from.
//
//	3. The `smd starting` line carries exactly one version value — not a duplicate
//	   key, and not two disagreeing ones.
//
// (Contract 2 — the pre-logger `logStartupFailure` record — lives in cmd/smd,
// which is where that writer is.)
//
// Criterion 1 is scoped to records emitted BY THIS BUILD, deliberately: retained
// records cannot be stamped retroactively, so a mixed historical file stays
// partly legacy until rotation ages it out. Nothing here should be read as a
// claim about existing files.
//
// Cost, measured and accepted: 40 B per record for the full string (46 B when
// -dirty). Against the post-Diff-A baseline that is 12.57 -> 15.18 MiB, +6% vs
// today's 14.31 MiB over a 15.51-day window — versus +23% had this shipped
// without Diff A's restructuring. The 30-day lumberjack age limit still binds
// well before the 100 MiB size cap.

// versionSentinel deliberately carries the -dirty suffix: it is the part most
// easily lost to normalisation or truncation, and the difference between a build
// that matches its tag and one that does not.
const versionSentinel = "2.0.0-alpha.1-998-gaba61729-dirty"

// withVersionSentinel swaps buildinfo.Version for the duration of one test and
// restores it afterwards.
//
// buildinfo.Version is a package GLOBAL, so a test that mutates it must not run
// in parallel with anything reading it — no t.Parallel() in this file, and none
// may be added. Cleanup restores the original rather than assuming "dev", so a
// stamped test binary is left as it was found.
func withVersionSentinel(t *testing.T) {
	t.Helper()
	original := buildinfo.Version
	buildinfo.Version = versionSentinel
	t.Cleanup(func() { buildinfo.Version = original })
}

// versionValues returns every value carried under the `version` key of a single
// JSON record, IN ORDER and WITHOUT DEDUPLICATION.
//
// A json.Decoder token stream, not json.Unmarshal into a map: a map silently
// keeps only the last of two identical keys, so it would report success on
// exactly the defect criterion 3 exists to catch. Duplicate keys are legal JSON
// and zerolog will happily emit them when a field is set both on the base
// context and on the event.
func versionValues(t *testing.T, line string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("record does not start with an object: %q", line)
	}
	var out []string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("decode key in %q: %v", line, err)
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("decode value for %q in %q: %v", key, line, err)
		}
		if key == "version" {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatalf("version value is not a string in %q: %v", line, err)
			}
			out = append(out, s)
		}
	}
	return out
}

func recordLines(t *testing.T, buf *bytes.Buffer) []string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// Criterion 1, via the capture seam: every record carries the build version,
// byte-for-byte including -dirty.
func TestVersionStamp_EveryRecordCarriesTheBuildVersion(t *testing.T) {
	withVersionSentinel(t)

	var buf bytes.Buffer
	s := NewForWriter(&buf)

	s.InfoWith().Str("k", "v").Msg("one")
	s.WarnWith().Msg("two")
	s.ErrorWith().Msg("three")
	s.With().Str("child", "yes").Logger().InfoWith().Msg("four")

	lines := recordLines(t, &buf)
	if len(lines) != 4 {
		t.Fatalf("want 4 records, got %d: %s", len(lines), buf.String())
	}
	for i, line := range lines {
		got := versionValues(t, line)
		if len(got) != 1 {
			t.Errorf("record %d carries %d version values, want exactly 1: %s", i, len(got), line)
			continue
		}
		if got[0] != versionSentinel {
			t.Errorf("record %d version = %q, want %q byte-for-byte (the -dirty suffix "+
				"is what distinguishes a build from the tag it derives from)",
				i, got[0], versionSentinel)
		}
	}
}

// The same guarantee on the REAL initialisation path, not just the test seam —
// NewForWriter could satisfy the test above while Initialize did not.
func TestVersionStamp_InitializedServiceAlsoStamps(t *testing.T) {
	withVersionSentinel(t)

	tmp := t.TempDir()
	cfgSvc := config.New(config.DefaultConfig(tmp))
	if err := cfgSvc.Initialize(); err != nil {
		t.Fatalf("config init: %v", err)
	}
	s := &Service{ConfigService: cfgSvc, WorkingDir: tmp}
	if err := s.Initialize(); err != nil {
		t.Fatalf("logging init: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	s.InfoWith().Msg("through the real initialiser")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The file is named after the executable under RelLogFileDir; find it rather
	// than reconstructing the name, which differs between `go test` and a built
	// binary.
	logDir := filepath.Join(tmp, s.LoggingConfig.RelLogFileDir)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("read log dir %s: %v", logDir, err)
	}
	var data []byte
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			data, err = os.ReadFile(filepath.Join(logDir, e.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", e.Name(), err)
			}
			break
		}
	}
	if data == nil {
		t.Fatalf("no .log file under %s", logDir)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" || !strings.Contains(line, "through the real initialiser") {
			continue
		}
		found = true
		got := versionValues(t, line)
		if len(got) != 1 || got[0] != versionSentinel {
			t.Fatalf("initialized-service record version = %v, want exactly [%q]: %s",
				got, versionSentinel, line)
		}
	}
	if !found {
		t.Fatalf("test record not found in the log file under %s", tmp)
	}
}

// Criterion 3's mechanism, asserted on its own so a regression is legible.
//
// A field set BOTH on the base context and on the event emits the key twice.
// json.Unmarshal into a map cannot see that — it keeps the last and reports
// success — which is why versionValues walks a token stream. This test proves the
// helper detects what it claims to.
func TestVersionValues_DetectsDuplicateKeys(t *testing.T) {
	const twice = `{"level":"info","version":"a","version":"b","message":"x"}`
	if got := versionValues(t, twice); len(got) != 2 {
		t.Fatalf("versionValues found %d values in a record with two version keys, want 2 — "+
			"a map-based check would report 1 and miss the defect criterion 3 guards", len(got))
	}

	var asMap map[string]any
	if err := json.Unmarshal([]byte(twice), &asMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := asMap["version"]; !ok || len(asMap) != 3 {
		t.Fatalf("sanity: the map form should collapse the duplicate to one key, got %v", asMap)
	}
}
