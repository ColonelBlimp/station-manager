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

	It flags a publish reached after `s.mu.Unlock()` in three forms, because a check
	that only understood the first would let the defect back in by rename (codex P2 on
	603cd026):

	  1. `s.publish(...)` directly;
	  2. a local ALIAS — `publish := s.publish` then `publish(...)` — which this package
	     really does use, to hand the sink to a completion callback
	     (caller_sequencer.go); and
	  3. a call to another Sequencer method that publishes, computed to a fixed point
	     so a helper-of-a-helper still counts. Methods that take `s.mu` themselves are
	     excluded: they manage their own ordering, which is exactly what publishCurrent
	     exists to do.

	Function literals are NOT scanned as part of the enclosing block. A closure defined
	after an unlock runs later with its own lock discipline — the real one in
	caller_sequencer.go re-locks and publishes correctly — so treating its body as
	"after the unlock" would be wrong. Their bodies are still checked, on their own,
	because ast.Inspect visits every block.
*/

// seqMethod returns the receiver-method name for a Sequencer method declaration.
func seqMethod(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Name == nil {
		return ""
	}
	st, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	if id, ok := st.X.(*ast.Ident); ok && id.Name == "Sequencer" {
		return fd.Name.Name
	}
	return ""
}

// locksOnEntry reports whether a method provably holds s.mu on EVERY path through its
// body. The rule is deliberately the strictest one that still works: s.mu.Lock() must
// be the method's FIRST statement.
//
// This started as "a Lock anywhere in the AST" and was tightened twice, because each
// looser rule had a hole an unlocked publish could walk through:
//
//   - anywhere in the AST accepted a lock on one conditional path, or one sitting in
//     an unrelated closure (codex P2 on e3a7e605);
//   - "before any control-flow statement" accepted a bare *ast.BlockStmt, which hides
//     an early return inside it — and equally accepted a publish placed BEFORE the
//     lock, since a call is not control flow (codex P2 on 30be7fb5).
//
// Each fix enumerated what to REJECT and each time the enumeration was incomplete.
// So the rule now enumerates what to ACCEPT, and the accepted set has one member.
// publishCurrent — the only method the exemption exists for, and the one 11 correct
// call sites depend on — locks first, so nothing real is lost.
//
// An unsound exemption is worse than no exemption, because it reads as coverage.
func locksOnEntry(fd *ast.FuncDecl, calleeOf func(ast.Node) (string, bool)) bool {
	if len(fd.Body.List) == 0 {
		return false
	}
	c, ok := calleeOf(fd.Body.List[0])
	return ok && c == "s.mu.Lock"
}

func TestSource_NoStatusPublishedAfterUnlock(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	var files []*ast.File
	names := map[*ast.File]string{}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			files = append(files, f)
			names[f] = name
		}
	}

	render := func(e ast.Expr) string {
		var b strings.Builder
		if err := printer.Fprint(&b, fset, e); err != nil {
			return ""
		}
		return b.String()
	}
	// calleeOf reports the rendered callee of a call statement, e.g. "s.publish".
	calleeOf := func(n ast.Node) (string, bool) {
		es, ok := n.(*ast.ExprStmt)
		if !ok {
			return "", false
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		return render(call.Fun), true
	}
	// aliasesIn collects locals bound to s.publish within a function.
	aliasesIn := func(fd *ast.FuncDecl) map[string]bool {
		out := map[string]bool{}
		ast.Inspect(fd, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range as.Rhs {
				if render(rhs) == "s.publish" && i < len(as.Lhs) {
					if id, ok := as.Lhs[i].(*ast.Ident); ok {
						out[id.Name] = true
					}
				}
			}
			return true
		})
		return out
	}

	// Which Sequencer methods publish, and which manage their own lock.
	publishes := map[string]bool{}
	selfLocks := map[string]bool{}
	decls := map[string]*ast.FuncDecl{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			name := seqMethod(fd)
			if name == "" {
				continue
			}
			decls[name] = fd
			al := aliasesIn(fd)
			if locksOnEntry(fd, calleeOf) {
				selfLocks[name] = true
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if c, ok := calleeOf(n); ok && (c == "s.publish" || al[c]) {
					publishes[name] = true
				}
				return true
			})
		}
	}
	// Fixed point: a method calling a non-self-locking publisher publishes too.
	for changed := true; changed; {
		changed = false
		for name, fd := range decls {
			if publishes[name] {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				c, ok := calleeOf(n)
				if ok && strings.HasPrefix(c, "s.") && publishes[strings.TrimPrefix(c, "s.")] &&
					!selfLocks[strings.TrimPrefix(c, "s.")] {
					publishes[name], changed = true, true
				}
				return true
			})
		}
	}

	for _, f := range files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			al := aliasesIn(fd)
			offends := func(c string) bool {
				if c == "s.publish" || al[c] {
					return true
				}
				m := strings.TrimPrefix(c, "s.")
				return strings.HasPrefix(c, "s.") && publishes[m] && !selfLocks[m]
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				block, ok := n.(*ast.BlockStmt)
				if !ok {
					return true
				}
				for i, st := range block.List {
					if c, ok := calleeOf(st); !ok || c != "s.mu.Unlock" {
						continue
					}
					for _, rest := range block.List[i+1:] {
						ast.Inspect(rest, func(m ast.Node) bool {
							if _, isLit := m.(*ast.FuncLit); isLit {
								return false // its own scope; checked separately
							}
							if c, ok := calleeOf(m); ok && offends(c) {
								t.Errorf("%s:%d: %s reached after s.mu.Unlock() at %s:%d — "+
									"the transition and its publish must be atomic (invariant 3); "+
									"move it above the Unlock",
									names[f], fset.Position(m.Pos()).Line, c,
									names[f], fset.Position(st.Pos()).Line)
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
