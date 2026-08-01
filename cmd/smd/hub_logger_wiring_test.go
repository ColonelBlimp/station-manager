package main

import (
	"bytes"
	_ "embed"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// PRODUCTION WIRING GUARD for the events hub's eviction logger.
//
// THE HOLE THIS CLOSES. `internal/events` reports slow-reader evictions only if
// something calls Hub.SetLogger — and every test in that package installs the
// logger itself. So deleting the one production call in main.go left the whole
// suite green while `/v1/events` evictions went silent again. Found by review of
// the three-hub class fix, 2026-08-01.
//
// It is a SOURCE guard rather than a behavioural one because the alternative is
// not available: main.go's run() builds the container, opens the database, starts
// the HTTP server and blocks. There is no seam to drive it from a test, and
// inventing one to make this assertion possible would be a larger change than the
// line being guarded.
//
// The hub cannot simply take the logger in NewHub, which would make this guard
// unnecessary: the hub is constructed BEFORE the DI container builds, because
// services with an `eventhub` inject field must all receive the same instance.
// That ordering is the reason a setter exists at all, and it is what leaves the
// wiring assertable only at the source.
//
// TWO FAILURE MODES, BOTH CLOSED, and they are different:
//
//  1. //go:embed rather than a relative-path read. Four guards in this tree read
//     their own source by path and silently passed when run from anywhere else —
//     proven earlier the same day by compiling one and running it from /tmp, where
//     it parsed nothing and reported success. An embed cannot do that.
//
//  2. PARSED, not string-matched. The first version of this guard did
//     strings.Contains("hub.SetLogger("), which a commented-out call or
//     `hub.SetLogger(nil)` both satisfy — it would have certified a wiring that
//     does nothing (review, same day). An embed fixes working-directory vacuity;
//     it does nothing about a check that never looked at the code's MEANING.
//
//go:embed main.go
var mainSource string

// findHubSetLogger returns the argument expressions of every `hub.SetLogger(...)`
// call in the embedded source. Comments are not part of the AST, so a
// commented-out call yields nothing.
func findHubSetLogger(t *testing.T) [][]ast.Expr {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", mainSource, 0)
	if err != nil {
		t.Fatalf("parse embedded main.go: %v", err)
	}
	var out [][]ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "SetLogger" {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if !ok || recv.Name != "hub" {
			return true
		}
		out = append(out, call.Args)
		return true
	})
	return out
}

// TestEventsHubLoggerIsWired asserts a REAL call exists, passing the REAL logger.
//
// It checks the argument by name and checks where that name comes from, because
// every looser version was evadable: `nil`, `logging.Noop()` and
// `(*logging.Service)(nil)` all wire a logger that emits nothing, and a local
// `loggerSvc := logging.Noop()` would defeat a name check alone.
//
// It still does NOT check where in run() the call sits. Ordering relative to the
// container build would break on an innocuous move while catching nothing the
// checks below do not — the hub simply must have the real logger before anything
// can subscribe, and nothing can subscribe before the HTTP server starts.
func TestEventsHubLoggerIsWired(t *testing.T) {
	if strings.TrimSpace(mainSource) == "" {
		t.Fatal("embedded main.go is empty — the guard would pass vacuously, which is " +
			"the failure mode //go:embed is here to prevent")
	}
	// The hub must exist for the assertion to mean anything.
	if !strings.Contains(mainSource, "events.NewHub()") {
		t.Fatal("main.go no longer constructs an events hub — this guard is stale and " +
			"should be re-pointed rather than deleted")
	}

	calls := findHubSetLogger(t)
	if len(calls) != 1 {
		t.Fatalf("hub.SetLogger call expressions in main.go = %d, want exactly 1 — "+
			"without it, /v1/events slow-reader evictions are silent in production and "+
			"no test in internal/events would notice, because they all install a logger "+
			"themselves", len(calls))
	}
	args := calls[0]
	if len(args) != 1 {
		t.Fatalf("hub.SetLogger called with %d arguments, want 1", len(args))
	}

	// THE ARGUMENT MUST BE THE RESOLVED PRODUCTION LOGGER, named. Rejecting only the
	// literal `nil` was not enough: `logging.Noop()` and `(*logging.Service)(nil)`
	// both pass a nil-literal check and both emit nothing, so the guard would have
	// certified three different ways of wiring silence (review, 2026-08-01).
	//
	// This makes the test SOURCE-SPECIFIC on purpose — renaming loggerSvc must update
	// it. That is the deliberate trade: a guard loose enough to survive any rename is
	// loose enough to accept any argument, which is what it exists to prevent.
	id, ok := args[0].(*ast.Ident)
	if !ok {
		t.Fatalf("hub.SetLogger's argument is %T, want the identifier loggerSvc — a call "+
			"expression here (logging.Noop(), a nil conversion) constructs a logger that "+
			"emits nothing, which is the deleted-call case wearing a call's clothes",
			args[0])
	}
	if id.Name != "loggerSvc" {
		t.Errorf("hub.SetLogger(%s) — want the resolved production logger `loggerSvc`; "+
			"if it was renamed, update this test, and if it was replaced check the "+
			"replacement actually writes to smd.log", id.Name)
	}

	// ...and loggerSvc must BE the container's logger, not a local rebinding. Without
	// this, `loggerSvc := logging.Noop()` a line earlier would satisfy everything above.
	assignments := assignmentsTo(t, "loggerSvc")
	if len(assignments) != 1 {
		t.Fatalf("loggerSvc is assigned %d times in main.go, want exactly 1 — more than "+
			"one makes it impossible to tell from here which value reaches SetLogger",
			len(assignments))
	}
	if !strings.Contains(assignments[0], "ResolveAs") {
		t.Errorf("loggerSvc is assigned from %q, want the container resolution "+
			"(iocdi.ResolveAs) — anything else can be a logger that discards records",
			assignments[0])
	}
}

// assignmentsTo renders the right-hand side of every assignment whose left side
// names the given identifier.
func assignmentsTo(t *testing.T, name string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", mainSource, 0)
	if err != nil {
		t.Fatalf("parse embedded main.go: %v", err)
	}
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok || ident.Name != name {
				continue
			}
			var buf bytes.Buffer
			for i, rhs := range as.Rhs {
				if i > 0 {
					buf.WriteString(", ")
				}
				if err := printer.Fprint(&buf, fset, rhs); err != nil {
					t.Fatalf("render rhs: %v", err)
				}
			}
			out = append(out, buf.String())
			break
		}
		return true
	})
	return out
}
