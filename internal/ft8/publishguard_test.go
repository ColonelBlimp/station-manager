package ft8

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

/*
	Suite-wide enforcement of invariant 3 (see publishatomicity_test.go for the rule
	and why it needed an executable guard). newTestSeq's publish sink calls
	recordUnlockedPublish whenever a frame is published with s.mu released; TestMain
	reports every distinct source location at the end of the run.

	Collecting rather than failing in place is deliberate: one run then names EVERY
	offending site, which is what you need when converting them, instead of stopping
	at the first.
*/

var unlockedPublishes = struct {
	sync.Mutex
	sites map[string]int
}{sites: map[string]int{}}

// recordUnlockedPublish attributes the violation to the first non-test frame inside
// this package — the actual publish site, not the sink or the test that drove it.
func recordUnlockedPublish() {
	var pcs [16]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	site := "unknown"
	for {
		f, more := frames.Next()
		if strings.Contains(f.File, "/internal/ft8/") && !strings.HasSuffix(f.File, "_test.go") {
			site = fmt.Sprintf("%s:%d (%s)", f.File[strings.LastIndex(f.File, "/")+1:], f.Line, f.Function[strings.LastIndex(f.Function, ".")+1:])
			break
		}
		if !more {
			break
		}
	}
	unlockedPublishes.Lock()
	unlockedPublishes.sites[site]++
	unlockedPublishes.Unlock()
}

func TestMain(m *testing.M) {
	code := m.Run()
	unlockedPublishes.Lock()
	defer unlockedPublishes.Unlock()
	if len(unlockedPublishes.sites) > 0 {
		names := make([]string, 0, len(unlockedPublishes.sites))
		for k := range unlockedPublishes.sites {
			names = append(names, k)
		}
		sort.Strings(names)
		fmt.Fprintf(os.Stderr, "\nFAIL: %d publish site(s) released s.mu before publishing (invariant 3):\n", len(names))
		for _, k := range names {
			fmt.Fprintf(os.Stderr, "  %-52s  x%d\n", k, unlockedPublishes.sites[k])
		}
		code = 1
	}
	os.Exit(code)
}

/*
Source-level companion to the runtime probe above.

The probe only sees paths a test actually drives, and 23 of the 39 sites converted
on 2026-07-27 are executed by no test at all — a regression in any of those would
pass unnoticed. (Found the hard way: the first attempt to prove the probe bites
reverted one of those sites and the suite stayed green.) This check reads the
package source instead, so coverage is irrelevant.

It flags the exact shape that caused the defect: `s.mu.Unlock()` followed, later in
the same block or anything nested inside it, by `s.publish(...)`. `defer
s.mu.Unlock()` is correctly ignored — the lock is still held until return.
*/
func TestSource_NoStatusPublishedAfterUnlock(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	isCall := func(n ast.Node, sel string) bool {
		es, ok := n.(*ast.ExprStmt)
		if !ok {
			return false
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		var buf strings.Builder
		if err := printer.Fprint(&buf, fset, call.Fun); err != nil {
			return false
		}
		return buf.String() == sel
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				block, ok := n.(*ast.BlockStmt)
				if !ok {
					return true
				}
				for i, st := range block.List {
					if !isCall(st, "s.mu.Unlock") {
						continue
					}
					for _, rest := range block.List[i+1:] {
						ast.Inspect(rest, func(m ast.Node) bool {
							if isCall(m, "s.publish") {
								t.Errorf("%s:%d: status published after s.mu.Unlock() at %s:%d — "+
									"the transition and its publish must be atomic (invariant 3); "+
									"move the publish above the Unlock",
									name, fset.Position(m.Pos()).Line,
									name, fset.Position(st.Pos()).Line)
							}
							return true
						})
					}
				}
				return true
			})
		}
	}
}
