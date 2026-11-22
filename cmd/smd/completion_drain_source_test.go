package main

import (
	"bytes"
	_ "embed"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"
)

// The daemon's wait for FT8 completion logging already earned has two halves,
// in this order: a contact the decode loop is still handing over is waited
// for, and then every logging job already counted (drainFt8Completions; ADR
// 0100 §3 and §4). A confirmed exit of the app and an archive switch both
// close a session after it.
//
// What each half does is shown by behaviour: the hand-over wait in
// internal/ft8, with a contact the sequencer earned
// (TestSealTxAdmission_AContactBeingHandedOverIsWaitedForAfterTheSeal,
// TestOperating_TheExitWaitsForAContactHandedOverFromTheDecodeLoop), and the
// jobs' wait through the daemon's exit and switch. That the daemon's drain
// makes the FIRST wait at all is not reachable by a behavioural test from this
// package: holding a slot open needs the FT8 service's own lock, and the
// operator decided against adding a method to internal/ft8 for a test alone
// (2026-11-22). Without the first wait every other test still passes, since
// each begins with its job already counted. So the function is pinned at its
// source: its statements must be exactly these. A change to the drain, however
// well meant, stops here and is looked at.
//
// The source is embedded, not read by path, and parsed, not searched as text:
// a commented-out call is not a statement (hub_logger_wiring_test.go says why).
//
//go:embed lifecycle_adapters.go
var lifecycleAdaptersSource string

const drainFt8CompletionsBody = `{
	if d.ft8 != nil {
		d.ft8.AwaitCompletionHandover()
	}
	d.qsoLogWG.Wait()
}`

func TestSource_TheCompletionDrainWaitsForTheHandoverAndThenTheJobs(t *testing.T) {
	fset := token.NewFileSet()
	// Comments are not parsed, so none can stand in for a statement.
	f, err := parser.ParseFile(fset, "lifecycle_adapters.go", lifecycleAdaptersSource, 0)
	if err != nil {
		t.Fatalf("parse the embedded lifecycle_adapters.go: %v", err)
	}
	var found []*ast.FuncDecl
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "drainFt8Completions" {
			found = append(found, fn)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d functions named drainFt8Completions, want 1", len(found))
	}
	fn := found[0]
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		t.Fatal("drainFt8Completions is not a method of the daemon")
	}
	var body bytes.Buffer
	if err := (&printer.Config{Mode: printer.TabIndent, Tabwidth: 8}).Fprint(&body, fset, fn.Body); err != nil {
		t.Fatalf("print the body: %v", err)
	}
	if body.String() != drainFt8CompletionsBody {
		t.Fatalf("drainFt8Completions is\n%s\nwant exactly\n%s\nThe hand-over wait must come first: a contact still being handed over is not yet counted among the jobs.",
			body.String(), drainFt8CompletionsBody)
	}
}
