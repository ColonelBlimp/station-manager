package ft8

import (
	"go/ast"
	"go/printer"
	"go/token"
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
// TestSource_SessionsEndOnlyThroughThePrimitive enforces invariant 6 structurally.
//
// It needs TWO rules, because a session ends when mode becomes seqIdle and there are
// two disjoint ways to get there — naming the constant, or reaching zero without it.
// Each rule alone leaves a hole the other covers, which is how round six happened:
// the previous version dropped the second and `s.mode = 0` walked straight through a
// guard that had already been closing it (codex P2 on bd2f31fa).
//
//  1. CONSTANT. Outside the two primitives, `seqIdle` may appear only AS a comparison
//     operand or case expression — not merely somewhere beneath one, or
//     `launder(seqIdle) == x` passes it out again. Never where it can be stored,
//     aliased or passed. This is what makes aliasing irrelevant: `alias.mode =
//     seqIdle` is caught by the right-hand side, whatever `alias` is called.
//  3. ADDRESS. `&x.mode` is refused outright: a write through the pointer names no
//     constant and puts no `.mode` on the left, so it evades rules 1 and 2 together.
//  2. LVALUE. Any assignment to a `.mode` selector must name an enumerated ACTIVE
//     mode; `++`/`--`/compound assignment on one is refused outright. seqMode is
//     integer-backed and seqIdle is 0, so `s.mode = 0`, `seqMode(0)`, a zero-valued
//     variable and `seqAnswering(1)--` all reach idle silently.
//
// The three rules were arrived at by AUDIT rather than by patching the latest
// finding: every check any earlier version of this guard performed was listed, and
// each mapped to a rule that still performs it. That audit is what found rule 3
// missing — one round after "when replacing a check, enumerate what the old one
// caught" was written into this very file, and not applied to it.
//
// Rule 2 deliberately matches ANY `.mode` selector without asking whose it is. That
// is sound here because no other type in the package is written through an
// assignment to a `.mode` field — verified before relying on it — and it is what
// finally made the guard immune to naming, after five rounds of tracking receivers,
// parameters and aliases. If a future type does need one, this test fails and the
// author has a conversation rather than a silent pass.
//
// A survey of production code before writing this confirmed the rule costs nothing:
// every existing use of seqIdle outside the primitives is `s.mode == seqIdle` or
// `!=`. Adding a legitimate third primitive means adding it to `permitted` — a
// two-line change and a conversation, rather than a silent pass.
// unwrapParens strips redundant parentheses: (x) and ((x)) reduce to x.
func unwrapParens(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func TestSource_SessionsEndOnlyThroughThePrimitive(t *testing.T) {
	const allowed = "retireSessionLocked, abandonLocked"
	permitted := map[string]bool{"retireSessionLocked": true, "abandonLocked": true}
	// Assigning one of these is a session START, which legitimately happens outside
	// the primitives. Anything else written to a mode is an end, or unreadable.
	activeMode := map[string]bool{
		"seqAnswering": true, "seqCalling": true, "seqWorking": true,
		"seqAnsweringFd": true, "seqWorkingFd": true,
		"seqAnsweringT4": true, "seqWorkingT4": true,
	}

	// Embedded sources, not ParseDir(".") — see source_guard_test.go.
	fset, sourceFiles, sourceNames := packageSourceFiles(t)
	{
		for _, file := range sourceFiles {
			name := sourceNames[file]
			for _, d := range file.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				// The primitives are exempt from rules 1 and 2 — assigning seqIdle is
				// their whole job — but NOT from rule 3. Skipping their bodies wholesale
				// let a primitive leak `&s.mode`, after which a write through the escaped
				// pointer carries no `.mode` lvalue and no seqIdle, so all three rules
				// pass (codex P2 on 33c66232). Nothing needs that address anywhere, so
				// the rule is simply universal — which is also easier to state than the
				// carve-out it replaces.
				isPrimitive := fd.Name != nil && permitted[fd.Name.Name]
				// Positions of seqIdle mentions that are merely READ.
				readOnly := map[token.Pos]bool{}
				// The identifier must BE the operand. Marking everything BENEATH it let
				// `capture(seqIdle) == want` launder the value through a call and out
				// again (codex P2 on bd2f31fa).
				markRead := func(e ast.Expr) {
					if id, ok := unwrapParens(e).(*ast.Ident); ok && id.Name == "seqIdle" {
						readOnly[id.Pos()] = true
					}
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch v := n.(type) {
					case *ast.BinaryExpr:
						if v.Op == token.EQL || v.Op == token.NEQ {
							markRead(v.X)
							markRead(v.Y)
						}
					case *ast.CaseClause:
						for _, e := range v.List {
							markRead(e)
						}
					}
					return true
				})

				isModeSel := func(e ast.Expr) bool {
					for {
						switch v := e.(type) {
						case *ast.ParenExpr:
							e = v.X
						case *ast.StarExpr:
							e = v.X
						default:
							sel, ok := e.(*ast.SelectorExpr)
							return ok && sel.Sel != nil && sel.Sel.Name == "mode"
						}
					}
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch v := n.(type) {
					case *ast.Ident:
						if !isPrimitive && v.Name == "seqIdle" && !readOnly[v.Pos()] {
							t.Errorf("%s:%d: seqIdle used outside a comparison in %s — a "+
								"session may only be ended by the session-identity "+
								"transition (invariant 6), which retires the generation and "+
								"consumes a staged end_reason. Allowed only in: %s",
								name, fset.Position(v.Pos()).Line, fd.Name.Name, allowed)
						}
					case *ast.UnaryExpr:
						// RULE 3. Taking the address is how a write escapes both rules at
						// once: `m := &s.mode; z := seqMode(0); *m = z` names no constant
						// and has no `.mode` on the left. There is no legitimate need for
						// it, so refuse it outright (codex P2 on 95b5da25 — a check this
						// file HAD, dropped in a rewrite and then not restored alongside
						// the lvalue rule, which is the same mistake twice).
						if v.Op == token.AND && isModeSel(v.X) {
							t.Errorf("%s:%d: the address of a mode is taken in %s — a write "+
								"through it would evade both rules. See invariant 6; "+
								"allowed only in: %s",
								name, fset.Position(v.Pos()).Line, fd.Name.Name, allowed)
						}
					case *ast.IncDecStmt:
						if isPrimitive {
							return true
						}
						if isModeSel(v.X) {
							t.Errorf("%s:%d: arithmetic on a mode in %s (%s) reaches seqIdle "+
								"without naming it — see invariant 6. Allowed only in: %s",
								name, fset.Position(v.Pos()).Line, fd.Name.Name,
								v.Tok.String(), allowed)
						}
					case *ast.AssignStmt:
						if isPrimitive {
							return true
						}
						for i, l := range v.Lhs {
							if !isModeSel(l) {
								continue
							}
							if v.Tok != token.ASSIGN { // +=, -=, |= …
								t.Errorf("%s:%d: arithmetic on a mode in %s (%s) reaches "+
									"seqIdle without naming it — see invariant 6. Allowed "+
									"only in: %s",
									name, fset.Position(v.Pos()).Line, fd.Name.Name,
									v.Tok.String(), allowed)
								continue
							}
							// seqMode is integer-backed and seqIdle is 0, so a write can
							// reach idle without ever mentioning the constant: s.mode = 0,
							// seqMode(0), or a zero-valued variable. The constant rule
							// above cannot see those, so the LVALUE rule stands alongside
							// it and demands an enumerated ACTIVE mode (codex P2 on
							// bd2f31fa, a hole this file had closed and then reopened).
							rhs := "?"
							if len(v.Rhs) == len(v.Lhs) {
								var b strings.Builder
								if printer.Fprint(&b, fset, v.Rhs[i]) == nil {
									rhs = b.String()
								}
							}
							if !activeMode[rhs] {
								t.Errorf("%s:%d: a mode is set to %q in %s — outside the "+
									"primitives a write may only name an ACTIVE mode "+
									"(a session START). See invariant 6; allowed only in: %s",
									name, fset.Position(v.Pos()).Line, rhs, fd.Name.Name,
									allowed)
							}
						}
					}
					return true
				})
			}
		}
	}
}
