package ft8

import (
	"embed"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// Shared source access for this package's STRUCTURAL guards
// (TestSource_NoStatusPublishedAfterUnlock, TestSource_SessionsEndOnlyThroughThePrimitive).
//
// EMBEDDED, not read from disk. Both guards previously did
// parser.ParseDir(fset, ".", …), which assumes the process starts in the package
// directory. `go test` and `task ci:local` satisfy that, so it looked fine — but a
// binary built with `go test -c` and run from anywhere else parses NOTHING, finds
// no violations, and PASSES.
//
// Demonstrated 2026-08-01 by injecting a real publish-after-unlock into
// sequencer.go: in-package the guard failed with
//
//	sequencer.go:1772: s.publish reached after s.mu.Unlock() at sequencer.go:1771
//
// and the same guard, compiled and run from /tmp, printed PASS.
//
// That matters more here than almost anywhere else in the tree. These two guards
// exist precisely BECAUSE coverage cannot reach their subjects — per
// internal/ft8/CLAUDE.md, 23 of the 39 publish sites are executed by no test, so
// the AST check is the only thing protecting them. A guard that reports success
// when it cannot see its subject is worse than no guard: it reads as coverage.
//
// //go:embed *.go rather than an explicit file list, deliberately. A hand-listed
// set would silently stop covering any newly added source file — the same
// vacuity failure in slower motion. Test files are matched by the pattern and
// filtered below.
//
//go:embed *.go
var ft8PackageSources embed.FS

// packageSourceFiles parses every non-test source file of this package from the
// embedded copy, returning the ASTs and their filenames.
//
// It FAILS if it finds nothing, or if a file the guards specifically rely on is
// absent. Without that, a broken embed pattern would restore exactly the silent
// no-op this helper was written to remove.
func packageSourceFiles(t *testing.T) (*token.FileSet, []*ast.File, map[*ast.File]string) {
	t.Helper()

	entries, err := fs.Glob(ft8PackageSources, "*.go")
	if err != nil {
		t.Fatalf("glob embedded sources: %v", err)
	}

	fset := token.NewFileSet()
	var files []*ast.File
	names := map[*ast.File]string{}
	seen := map[string]bool{}
	for _, name := range entries {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, rerr := ft8PackageSources.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read embedded %s: %v", name, rerr)
		}
		f, perr := parser.ParseFile(fset, name, src, 0)
		if perr != nil {
			t.Fatalf("parse embedded %s: %v", name, perr)
		}
		files = append(files, f)
		names[f] = name
		seen[name] = true
	}

	if len(files) == 0 {
		t.Fatal("parsed no package sources — the guards would pass vacuously, " +
			"which is the failure mode this helper exists to prevent")
	}
	// The files that actually carry the guarded constructs. If the embed ever
	// stops reaching them the guards go quiet, so name them positively rather
	// than trusting a count.
	for _, required := range []string{
		"sequencer.go", "caller_sequencer.go", "work_sequencer.go", "type4_sequencer.go",
	} {
		if !seen[required] {
			t.Fatalf("%s is not among the embedded sources; the guards would not see "+
				"the sequencer files they exist to check", required)
		}
	}
	return fset, files, names
}

// The helper's own guarantee: it must actually be reading this package.
//
// Runs everywhere the other guards run, so a regression in the embed is caught
// by an ordinary test rather than only by someone thinking to execute a compiled
// binary from another directory.
func TestSourceGuard_ReadsThePackageItGuards(t *testing.T) {
	_, files, names := packageSourceFiles(t)
	if len(files) < 10 {
		t.Errorf("parsed only %d source files; internal/ft8 has many more, so the "+
			"embed is probably not matching what it should", len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(names[f], "_test.go") {
			t.Errorf("%s is a test file and must be filtered out", names[f])
		}
		if f.Name == nil || f.Name.Name != "ft8" {
			t.Errorf("%s declares package %v, want ft8", names[f], f.Name)
		}
	}
}
