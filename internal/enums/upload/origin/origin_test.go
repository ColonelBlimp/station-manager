package origin

import (
	_ "embed"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// originSource is the package's own source, compiled into the test binary so the
// enum guard below never touches the filesystem. See its comment for why.
//
//go:embed origin.go
var originSource string

// Mirrors action_test.go / status_test.go, plus one guard the others do not need:
// origin's value set is duplicated in a database CHECK constraint (migration
// 0007), so the two can drift silently. TestParse_CoversEveryDeclaredConstant
// reads the const block from source and fails when a constant is added here
// without being taught to Parse — the half of that drift this package can see.

func TestOriginString(t *testing.T) {
	cases := map[Origin]string{
		Live:      "live",
		Import:    "import",
		Edit:      "edit",
		Manual:    "manual",
		StampSync: "stamp_sync",
		Reconcile: "reconcile",
		Legacy:    "legacy",
	}

	for in, want := range cases {
		if got := in.String(); got != want {
			t.Fatalf("%q.String() = %q, want %q", in, got, want)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Origin
	}{
		{in: "live", want: Live},
		{in: "import", want: Import},
		{in: "edit", want: Edit},
		{in: "manual", want: Manual},
		{in: "stamp_sync", want: StampSync},
		{in: "reconcile", want: Reconcile},
		{in: "legacy", want: Legacy},
	}

	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Fatalf("Parse(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParse_UnknownReturnsError(t *testing.T) {
	got, err := Parse("unknown")
	if err == nil {
		t.Fatalf("Parse(%q) = (%q, nil), want error", "unknown", got)
	}
	if got != "" {
		t.Fatalf("Parse(%q) = (%q, err), want empty Origin on error", "unknown", got)
	}
}

// A value the DB CHECK would reject must not parse either. `startup_recovery` is
// the specific one considered and deliberately NOT added (2026-08-01): orphan
// recovery preserves the existing origin rather than assigning a new one.
func TestParse_RejectsDeliberatelyExcludedValues(t *testing.T) {
	for _, s := range []string{"startup_recovery", "backfill", "sync", "LIVE", ""} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded; the value set is closed and mirrored by "+
				"migration 0007's CHECK constraint", s)
		}
	}
}

// Every constant DECLARED IN THE SOURCE must round-trip through Parse.
//
// Source inspection, not a hand-written list. The first version of this test kept
// its own `all := []Origin{...}` slice and asserted `len(all) != 7` — which could
// never fire: a newly declared constant simply would not appear in the slice, so
// the test claimed to detect exactly the drift it was blind to (found in review,
// 2026-08-01). Reading the const block is the only way the claim can be true.
//
// The drift this guards is real: the value set is duplicated in migration 0007's
// CHECK constraint, so a constant added here and taught to neither Parse nor the
// CHECK is a split brain that surfaces only on the write path.
func TestParse_CoversEveryDeclaredConstant(t *testing.T) {
	// The source is EMBEDDED, not located. Two clean-room reviews in a row caught
	// this test failing before it tested anything, each time because it tried to
	// find origin.go on disk:
	//
	//   * plain "origin.go" assumed the process started in the package directory —
	//     fine under `go test`, broken for a `go test -c` binary run elsewhere;
	//   * runtime.Caller(0) then returned a MODULE-relative path under
	//     `-trimpath`, so filepath.Dir produced something that opens nowhere.
	//
	// The second fix replaced one path-resolution mechanism with another and
	// inherited the same class of bug. //go:embed removes the filesystem from the
	// problem: the bytes are compiled into the test binary, so working directory,
	// -trimpath and where the binary is run all stop mattering. The filename below
	// is only a label for parser error messages.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "origin.go", originSource, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse embedded origin.go: %v", err)
	}

	declared := map[string]string{} // const name -> literal value
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Only constants explicitly typed Origin.
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "Origin" {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("constant %s has a non-literal value; this guard cannot read it", name.Name)
				}
				declared[name.Name] = strings.Trim(lit.Value, `"`)
			}
		}
	}

	if len(declared) == 0 {
		t.Fatal("found no Origin constants in origin.go — the guard is not reading the source")
	}
	for name, value := range declared {
		got, err := Parse(value)
		if err != nil {
			t.Errorf("constant %s (%q) is declared but Parse rejects it: %v", name, value, err)
			continue
		}
		if got.String() != value {
			t.Errorf("Parse(%q) = %q, want %q (constant %s)", value, got, value, name)
		}
	}
}
