package main

import (
	"embed"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/buildinfo"
)

// packageSources is the package's own Go source, compiled into the test binary.
//
// EMBEDDED, not read from disk. Two source-inspecting tests below previously used
// os.ReadDir(".") and os.ReadFile("main.go"), which assume the process starts in
// the package directory: `go test` satisfies that, a `go test -c` binary run
// elsewhere does not. The single-carrier check was the worse of the two — from
// /tmp it found zero Go files, iterated nothing, and PASSED. A guard that reports
// success when it cannot see its subject is worse than no guard.
//
// This is the third instance of the same class in one day (the origin enum guard
// twice, then here); embedding removes the filesystem from the problem entirely.
//
//go:embed *.go
var packageSources embed.FS

// parsePackageFile returns the AST of one embedded source file.
func parsePackageFile(t *testing.T, name string) (*token.FileSet, *ast.File) {
	t.Helper()
	src, err := packageSources.ReadFile(name)
	if err != nil {
		t.Fatalf("embedded %s: %v", name, err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse embedded %s: %v", name, err)
	}
	return fset, f
}

// Diff C contracts owned by cmd/smd (SHIP GATE item (d)). Operator's wording,
// 2026-08-01:
//
//	2. A startup failure that happens before the logging service is wired carries
//	   the version too. logStartupFailure hand-writes its JSON and bypasses
//	   logging.Service, so a stamp applied at the logger would miss it — and this
//	   is the message most likely to be read on a failed fresh deploy, where
//	   "which build is this?" is the first question.
//
//	3. The `smd starting` line carries exactly one version value — not a duplicate
//	   key, and not two disagreeing ones.
//
// Criterion 1 (every record) is pinned in internal/logging, where the base
// context is built.
//
// NO t.Parallel() IN THIS FILE. These tests swap buildinfo.Version, a package
// global, and restore it via Cleanup; running them alongside anything that reads
// it would race.

const smdVersionSentinel = "2.0.0-alpha.1-998-gaba61729-dirty"

func withSmdVersionSentinel(t *testing.T) {
	t.Helper()
	original := buildinfo.Version
	buildinfo.Version = smdVersionSentinel
	t.Cleanup(func() { buildinfo.Version = original })
}

// smdVersionValues returns every value under the `version` key of one JSON
// record, in order and WITHOUT deduplication. json.Unmarshal into a map keeps
// only the last of two identical keys, so it would pass on precisely the
// duplicate criterion 3 forbids.
func smdVersionValues(t *testing.T, line string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("decode %q: %v", line, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("record is not a JSON object: %q", line)
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
			t.Fatalf("decode value for %q: %v", key, err)
		}
		if key == "version" {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatalf("version is not a string in %q: %v", line, err)
			}
			out = append(out, s)
		}
	}
	return out
}

// Criterion 2: the pre-logger fatal line carries the version.
//
// logStartupFailure runs when config.json is missing or malformed — before the
// logging service exists — so it hand-writes its JSON. A stamp applied at the
// logger cannot reach it, which is why this is asserted against the file the
// function actually writes rather than through any logging seam.
func TestLogStartupFailure_CarriesTheBuildVersion(t *testing.T) {
	withSmdVersionSentinel(t)

	// logStartupFailure resolves its path via utils.WorkingDir(), which honours
	// SM_WORKING_DIR — so point it at a temp dir rather than the real install.
	tmp := t.TempDir()
	t.Setenv("SM_WORKING_DIR", tmp)

	logStartupFailure(errors.New("config.json is not valid JSON"))

	path := filepath.Join(tmp, "log", "smd.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("logStartupFailure wrote no log at %s: %v", path, err)
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		t.Fatal("logStartupFailure wrote an empty file")
	}

	got := smdVersionValues(t, line)
	if len(got) != 1 {
		t.Fatalf("pre-logger fatal record carries %d version values, want exactly 1: %s",
			len(got), line)
	}
	if got[0] != smdVersionSentinel {
		t.Errorf("pre-logger fatal version = %q, want %q byte-for-byte", got[0], smdVersionSentinel)
	}
}

// The pre-logger writer must not lose what it already carried. Without this, a
// change that adds `version` by rebuilding the record could silently drop the
// error text — the one field that says what actually went wrong.
func TestLogStartupFailure_StillCarriesItsExistingFields(t *testing.T) {
	withSmdVersionSentinel(t)

	tmp := t.TempDir()
	t.Setenv("SM_WORKING_DIR", tmp)

	logStartupFailure(errors.New("SENTINEL-CAUSE-TEXT"))

	data, err := os.ReadFile(filepath.Join(tmp, "log", "smd.log"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("record is not JSON: %v", err)
	}
	for _, k := range []string{"level", "time", "message", "error"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("pre-logger record lost the %q field", k)
		}
	}
	if rec["level"] != "error" {
		t.Errorf("level = %v, want error", rec["level"])
	}
	if s, _ := rec["error"].(string); !strings.Contains(s, "SENTINEL-CAUSE-TEXT") {
		t.Errorf("error = %q, want it to carry the cause", s)
	}
}

// Criterion 3, at source level: `smd starting` must not set `version` on the
// event once the base context carries it.
//
// A source check rather than a running-daemon assertion: starting the daemon in a
// test would acquire the operator's serial and audio devices. The duplicate this
// guards is created by the explicit Str("version", …) on that call, so the
// explicit call is what must be absent — and its absence is exactly what the
// source can show.
func TestSmdStarting_DoesNotSetVersionOnTheEvent(t *testing.T) {
	entries, err := fs.Glob(packageSources, "*.go")
	if err != nil {
		t.Fatalf("glob embedded sources: %v", err)
	}
	var found, setsVersion bool
	for _, name := range entries {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		_, f := parsePackageFile(t, name)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Msg" || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Value != `"smd starting"` {
				return true
			}
			found = true
			// Walk back down the builder chain looking for Str("version", …).
			ast.Inspect(sel.X, func(inner ast.Node) bool {
				c, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				s2, ok := c.Fun.(*ast.SelectorExpr)
				if !ok || s2.Sel.Name != "Str" || len(c.Args) == 0 {
					return true
				}
				if k, ok := c.Args[0].(*ast.BasicLit); ok && k.Value == `"version"` {
					setsVersion = true
				}
				return true
			})
			return false
		})
	}

	if !found {
		t.Fatal(`no Msg("smd starting") call found in the package sources`)
	}
	if setsVersion {
		t.Error("the smd-starting statement still sets version on the event; the base " +
			"logger context now carries it, so this emits the key TWICE — legal JSON that " +
			"json.Unmarshal silently collapses, and two disagreeing values if they drift")
	}
}

// main.Version must be gone, not aliased: two carriers that must agree, with no
// mechanism to make them, is the state this diff removes (operator, 2026-08-01).
func TestBuildVersion_HasASingleCarrier(t *testing.T) {
	entries, err := fs.Glob(packageSources, "*.go")
	if err != nil {
		t.Fatalf("glob embedded sources: %v", err)
	}
	var scanned int
	for _, name := range entries {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		_, f := parsePackageFile(t, name)
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range vs.Names {
					if id.Name == "Version" {
						t.Errorf("%s declares a package-level Version; buildinfo.Version is "+
							"the single carrier and an alias here could diverge from it", name)
					}
				}
			}
		}
	}
	// Without this the test passes vacuously when it cannot see the package —
	// exactly how the os.ReadDir(".") version passed from /tmp.
	if scanned == 0 {
		t.Fatal("scanned no package sources; the guard is not reading the embedded files")
	}
}
