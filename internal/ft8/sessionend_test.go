package ft8

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

/*
	SESSION-END specification (2026-07-27) — invariant 6, applied to the ABANDONMENT
	paths rather than the completion paths.

	Invariant 6 says every ending of a session performs the SAME session-identity
	transition: retire the generation, consume any staged teardown reason, clear the
	ladder, and publish the terminal frame under the lock. `retireSessionLocked` is
	that transition.

	F2 (2026-07-27) applied it to the four COMPLETION paths — the ways a session ends
	because a contact finished. It did not touch the ways a session ends because the
	contact FAILED: the repeat cap expiring, an armed skip firing on a silent cycle,
	and the defensive "exchange exhausted" branches. Those still do it by hand —
	`s.ex = nil; s.mode = seqIdle` — which skips two of the four steps.

	Both omissions are observable, and neither is theoretical:

	  - The generation is not retired, so a callback created under it is not refused.
	    Every stale-callback guard in the package keys off that generation; skipping
	    the bump leaves them unable to tell a finished session from a live one.
	  - A staged teardown reason is dropped. The dial guard stages `dial_moved` and
	    then tears down; if the cap or a skip wins that race, the operator sees the
	    session stop with NO explanation — which invariant 5 exists to prevent, having
	    already cost a log dive on air (dogfood 2026-07-27).

	Rules (each stated for a path that ends a session WITHOUT completing a contact):

	  1. The repeat cap retires the generation.
	  2. The repeat cap consumes a staged teardown reason onto the terminal frame.
	  3. An armed skip firing does both.
	  4. An ordinary end with nothing staged carries no reason (the reason field is
	     not invented).

	Driven through the answer-a-CQ ladder; the source guard below is what covers the
	other 18 sites, including those no test reaches.
*/

// silentCycles drives enough empty partner slots to exhaust the repeat cap.
func silentCycles(s *Sequencer) {
	for i := int64(0); i <= int64(s.maxRepeats)+2; i++ {
		driveTheir(s, 30+i*30, nil)
	}
}

func TestSessionEnd_RepeatCapPerformsTheSameTransition(t *testing.T) {
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	t.Run("retires the generation", func(t *testing.T) {
		r := &seqRecorder{}
		s := newTestSeq(r)
		require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))
		gen := s.currentGen()

		silentCycles(s)
		require.False(t, s.Active(), "fixture: the cap ended the session")

		require.NotEqual(t, gen, s.currentGen(),
			"a session that ends must retire its generation — the stale-callback guards "+
				"all key off it, and one that survives leaves them unable to tell a "+
				"finished session from a live one")
		require.False(t, s.AbandonIfCurrent(gen, EndReasonDialMoved),
			"and a holder of the old generation must find nothing to act on")
	})

	t.Run("consumes a staged teardown reason", func(t *testing.T) {
		r := &seqRecorder{}
		s := newTestSeq(r)
		require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))

		// The dial guard stages its reason, then the cap wins the race and ends the
		// session first. If the reason is not consumed HERE it is lost: TX stopped,
		// session gone, nothing on screen saying why.
		s.setPendingEndReason(EndReasonDialMoved)
		silentCycles(s)

		last := r.lastStatus()
		require.False(t, last.Active)
		require.Equal(t, EndReasonDialMoved, last.EndReason,
			"the operator must be told why the session ended when they did not end it")
	})

	t.Run("invents no reason when none was staged", func(t *testing.T) {
		r := &seqRecorder{}
		s := newTestSeq(r)
		require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))

		silentCycles(s)

		require.Empty(t, r.lastStatus().EndReason,
			"an ordinary no-answer end needs no explanation beyond itself")
	})
}

func TestSessionEnd_ArmedSkipPerformsTheSameTransition(t *testing.T) {
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	r := &seqRecorder{}
	s := newTestSeq(r)
	require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))
	// A skip only fires on an already-sent rung, so let one cycle go out first.
	driveTheir(s, 30, nil)
	require.NoError(t, s.SetSkipIfSilent(true))
	gen := s.currentGen()

	s.setPendingEndReason(EndReasonBandChange)
	driveTheir(s, 60, nil) // silent cycle → the skip fires
	require.False(t, s.Active(), "fixture: the armed skip ended the session")

	require.NotEqual(t, gen, s.currentGen(), "the generation must be retired")
	require.Equal(t, EndReasonBandChange, r.lastStatus().EndReason,
		"a skip firing is still an end the operator did not ask for at this moment")
}

/*
Source guard for the same rule.

The behaviour tests above drive the answer-a-CQ ladder; there are 19 hand-rolled
session ends across the four sequencer files and no test reaches most of them. So
this asserts the STRUCTURE instead: `s.mode = seqIdle` may appear only inside the
two primitives that perform the full transition.

It covers two independent axes, because fixing one at a time is what made this take
four review rounds:

  - the write FORM: plain and multi-value assignment, compound assignment, ++/--, and
    taking the address (the only route to writing through a pointer);
  - the way the lvalue is SPELLED: matched structurally after unwrapping parentheses
    and pointer indirection, so `s.mode`, `(s.mode)`, `(*s).mode` and `(*(s)).mode`
    are one thing — and anchored to the *Sequencer identifiers in scope, its receiver
    AND any parameter, since `mode` is not a unique field name in this package.

The rounds: multi-value assignment (61a875d8), `s.mode--` walking seqAnswering(1) to
seqIdle(0) (5cffed06), then printed-text comparison letting parenthesised and
dereferenced spellings through (980c9e04). Each fix patched the instance reported
instead of asking what the complete set was.

It is an ALLOWLIST twice over, deliberately. Outside the two primitives, a write to
s.mode must assign one of the enumerated ACTIVE modes — a session start. Anything
else is flagged: seqIdle, a variable holding it, or an expression we cannot read.

Matching "s.mode = seqIdle" instead was too narrow and missed both a multi-value
assignment (`s.mode, s.ex = seqIdle, nil` — codex P2 on 61a875d8) and the same value
through a local. That is the third guard in this package to be fixed by inverting a
denylist into an allowlist; the pattern is now the default here, not the remedy.

A new legitimate primitive or a new mode has to be added to a list, which is a
two-line change and a conversation, rather than a silent pass.
*/
// unwrap strips parentheses and pointer indirection, so (x), *x and (*x) all reduce
// to x. Comparing PRINTED source instead let `(s.mode) = seqIdle`, `(s.mode)--` and
// `(*s).mode = seqIdle` through — all valid Go for the same lvalue (codex P2 on
// 980c9e04).
func unwrap(e ast.Expr) ast.Expr {
	for {
		switch v := e.(type) {
		case *ast.ParenExpr:
			e = v.X
		case *ast.StarExpr:
			e = v.X
		default:
			return e
		}
	}
}

// isModeOf reports whether e denotes the mode field of one of the given *Sequencer
// identifiers, however it is spelled. Anchoring on the identifier matters: `mode` is
// not a unique field name in this package (TxController has one), so matching the
// selector alone would flag unrelated code.
func isModeOf(e ast.Expr, seqIdents map[string]bool) bool {
	sel, ok := unwrap(e).(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "mode" {
		return false
	}
	id, ok := unwrap(sel.X).(*ast.Ident)
	return ok && seqIdents[id.Name]
}

// sequencerIdents names every *Sequencer in scope for a function — its receiver and
// any parameter — so a helper taking one cannot sidestep the guard either.
func sequencerIdents(fd *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			st, ok := f.Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			if id, ok := st.X.(*ast.Ident); !ok || id.Name != "Sequencer" {
				continue
			}
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	add(fd.Recv)
	if fd.Type != nil {
		add(fd.Type.Params)
	}
	return out
}

func TestSource_SessionsEndOnlyThroughThePrimitive(t *testing.T) {
	const allowed = "retireSessionLocked, abandonLocked"
	permitted := map[string]bool{"retireSessionLocked": true, "abandonLocked": true}
	// The ACTIVE modes: assigning one of these is a session START, which legitimately
	// happens outside the primitives. Every other right-hand side is a session end (or
	// unreadable), and must go through them.
	activeMode := map[string]bool{
		"seqAnswering": true, "seqCalling": true, "seqWorking": true,
		"seqAnsweringFd": true, "seqWorkingFd": true,
		"seqAnsweringT4": true, "seqWorkingT4": true,
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, d := range file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				if fd.Name != nil && permitted[fd.Name.Name] {
					continue
				}
				seqIdents := sequencerIdents(fd)
				if len(seqIdents) == 0 {
					continue // nothing of ours can be written here
				}
				render := func(e ast.Expr) string {
					var b strings.Builder
					if printer.Fprint(&b, fset, e) != nil {
						return "?"
					}
					return b.String()
				}
				flag := func(n ast.Node, how string) {
					t.Errorf("%s:%d: session state written by hand in %s (%s) — every ending "+
						"must go through the session-identity transition (invariant 6), so "+
						"the generation is retired and a staged end_reason is consumed. "+
						"Allowed only in: %s",
						name, fset.Position(n.Pos()).Line, fd.Name.Name, how, allowed)
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch v := n.(type) {
					case *ast.AssignStmt:
						for i, l := range v.Lhs {
							if !isModeOf(l, seqIdents) {
								continue
							}
							if v.Tok != token.ASSIGN { // +=, -=, |= …
								flag(v, render(l)+" "+v.Tok.String()+" …")
								continue
							}
							// Pairable only when the arities match; a multi-value call on
							// the right cannot be read, so it is not an allowed start.
							rhs := "?"
							if len(v.Rhs) == len(v.Lhs) {
								rhs = render(v.Rhs[i])
							}
							if !activeMode[rhs] {
								flag(v, render(l)+" = "+rhs)
							}
						}
					case *ast.IncDecStmt:
						// s.mode-- walks seqAnswering(1) to seqIdle(0) (codex P2 on
						// 5cffed06). Arithmetic on the mode is never legitimate.
						if isModeOf(v.X, seqIdents) {
							flag(v, render(v.X)+v.Tok.String())
						}
					case *ast.UnaryExpr:
						// Taking the address is how a write escapes this analysis
						// entirely: p := &s.mode; *p = seqIdle. There is no reason to
						// need it, so refuse it outright.
						if v.Op == token.AND && isModeOf(v.X, seqIdents) {
							flag(v, "&"+render(v.X))
						}
					}
					return true
				})
			}
		}
	}
}
