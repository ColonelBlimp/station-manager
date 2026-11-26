package qsoservice

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

/*
   W-0029 part 5, B6 and the operator's ruling of 2026-11-24 (question 1), as
   guards on the source: which production files may name the membership's
   pieces at all. Each is an allow-list of files, not a search for a known bad
   shape, so a new use anywhere fails here and is looked at.

     - the member table is named in production Go by the session's record and
       by the reads beside it (part 6), and is WRITTEN in exactly one statement
       in the whole tree: the logging step's;
     - no migration puts a row into it (B5);
     - the logging step is called from this package's submit alone;
     - a live submit is made by the submit route and the FT8 completion sink
       alone (B1, B2: no import, restore, copy or transfer path makes one);
     - the membership is connected by one function of the API server, and
       NOTHING in production calls that function: the daemon's ordinary start
       does not connect it until part 6, whose change adds its caller here.

   What these cannot see: a name built from pieces at run time, and a write
   through a database handle that never names the table in Go. Neither exists
   in the tree; the behaviour tests beside this file are what would catch one.
*/

// productionGo is every non-test Go file under the module's internal and cmd
// trees, by its slash-separated path from the module root.
func productionGo(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	for _, tree := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = string(src)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) < 100 {
		t.Fatalf("only %d production Go files were found: the walk is not looking at the tree", len(out))
	}
	return out
}

// naming is the files whose source matches re, sorted.
func naming(files map[string]string, re *regexp.Regexp) []string {
	var out []string
	for name, src := range files {
		if re.MatchString(src) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func TestSource_TheMembershipIsNamedOnlyWhereItIsAllowed(t *testing.T) {
	files := productionGo(t)
	const (
		record  = "internal/database/sqlite/operating_session.go"
		reads   = "internal/database/sqlite/operating_session_read.go"
		step    = "internal/qsoservice/session_membership.go"
		submit  = "internal/qsoservice/submit.go"
		route   = "internal/api/handler_qso.go"
		sink    = "cmd/smd/lifecycle_adapters.go"
		connect = "internal/api/operating_session.go"
	)
	for _, c := range []struct {
		what string
		name string
		want []string
	}{
		{"the member table", `\boperating_session_member\b`, []string{record, reads}},
		{"the logging step", `\bJoinOperatingSessionTx\b`, []string{record, step}},
		{"a live submit", `\bSubmitLive\b`, []string{sink, route, step, submit}},
		{"setting the membership", `\bSetSessionMembership\b`, []string{connect, step, submit}},
		{"connecting the membership", `\bConnectSessionMembership\b`, []string{sink, connect}},
	} {
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		if got := naming(files, regexp.MustCompile(c.name)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s is named in %v; want exactly %v", c.what, got, want)
		}
	}

	// The member table is written once in the whole tree, by the record, and no
	// row of it is ever changed or removed.
	writes := regexp.MustCompile(`(?i)\b(insert(\s+or\s+\w+)?\s+into|replace\s+into|update|delete\s+from)\s+operating_session_member\b`)
	for name, src := range files {
		got := writes.FindAllString(src, -1)
		switch {
		case name == record:
			if len(got) != 1 || !strings.HasPrefix(strings.ToUpper(got[0]), "INSERT INTO") {
				t.Errorf("the session's record writes the member table as %q; want one INSERT, the logging step's", got)
			}
		case len(got) != 0:
			t.Errorf("%s writes the member table: %q", name, got)
		}
	}

	// Connecting is declared once and called once in production: by the
	// daemon's ordinary start, on the server it has just built (W-0029 part 6,
	// G1). Until part 6 nothing in production called it.
	calls := regexp.MustCompile(`[.\s(]ConnectSessionMembership\(\)`)
	decl := regexp.MustCompile(`func \(s \*Server\) ConnectSessionMembership\(\)`)
	if got := len(calls.FindAllString(files[connect], -1)) - len(decl.FindAllString(files[connect], -1)); got != 0 {
		t.Errorf("%s calls ConnectSessionMembership %d time(s); want only its declaration there", connect, got)
	}
	if got := calls.FindAllString(files[sink], -1); len(got) != 1 || !strings.Contains(files[sink], "d.server.ConnectSessionMembership()") {
		t.Errorf("%s calls ConnectSessionMembership as %q; want once, on the daemon's server", sink, got)
	}
}

// B5: no migration puts a row into the member table.
func TestSource_NoMigrationWritesAMember(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "database", "sqlite", "migrations")
	writes := regexp.MustCompile(`(?i)\b(insert(\s+or\s+\w+)?\s+into|replace\s+into|update)\s+operating_session_member\b`)
	seen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "operating_session_member") {
			seen++
		}
		if m := writes.FindString(string(src)); m != "" {
			t.Errorf("%s writes the member table: %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("no migration names the member table: the walk is not looking at the migrations")
	}
}
