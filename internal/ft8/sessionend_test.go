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
				render := func(e ast.Expr) string {
					var b strings.Builder
					if printer.Fprint(&b, fset, e) != nil {
						return "?"
					}
					return b.String()
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					as, ok := n.(*ast.AssignStmt)
					if !ok {
						return true
					}
					for i, l := range as.Lhs {
						if render(l) != "s.mode" {
							continue
						}
						// Pairable only when the arities match; a multi-value call on the
						// right cannot be read, so it is not an allowed start.
						rhs := "?"
						if len(as.Rhs) == len(as.Lhs) {
							rhs = render(as.Rhs[i])
						}
						if activeMode[rhs] {
							continue // a session start
						}
						t.Errorf("%s:%d: session ended by hand in %s (s.mode = %s) — every "+
							"ending must go through the session-identity transition "+
							"(invariant 6), so the generation is retired and a staged "+
							"end_reason is consumed. Allowed only in: %s",
							name, fset.Position(as.Pos()).Line, fd.Name.Name, rhs, allowed)
					}
					return true
				})
			}
		}
	}
}
