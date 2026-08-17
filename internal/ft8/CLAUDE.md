# internal/ft8 — FT8 subsystem (design detail)

Loaded automatically when working under `internal/ft8/`. This holds the full FT8
design / decision / gotcha detail, migrated out of the root `CLAUDE.md` on
2026-07-22 to keep always-loaded context lean; a high-level pointer remains in
the root file. The single canonical FT8 capture point is `docs/ft8.md`.

## TX + attribution invariants — READ BEFORE CHANGING THE TX OR SLOT PATH

**Never violate these without explicit discussion**, in the sense
`docs/v1-analysis/invariants.md` uses. They were extracted 2026-07-27 after a
nine-round review arc on FT8 dial attribution in which *every* P1 was a violation
of one of them — and in which several fixes broke a rule that was already written
down a few files away. Each is stated in terms an operator or another subsystem
can OBSERVE, deliberately: a logged QSO row, RF keyed or not, a published SSE
status, a spot emitted. Bind tests to these, not to whichever field a mechanism
happens to carry this week — the field-level assertions from that arc were all
deleted within a round or two, while the behavioural ones caught real defects.

1. **A contact the partner has rogered is logged exactly once, ON THE FREQUENCY IT
   HAPPENED ON, whatever happens to our closing rung.** Group A policy
   (`finalrung.go`): the QSO is recorded whether or not the courtesy RR73/73
   reaches the air. Refusing RF, losing the rig, disarming, or any new guard must
   run the rung's completion policy before retiring the session. *Every completion
   callback is generation-guarded, so retiring first makes the callback refuse and
   the contact vanishes silently.* The frequency is the dial the SESSION pinned —
   never a live read at completion time, which differs exactly when a QSY refused
   the closing rung. *This clause was added within an hour of the first draft: the
   original "logged exactly once" was satisfied by a contact filed on the band we
   had just moved to, which is worse than losing it — the wrong-band row is
   forwarded to QRZ and ClubLog (codex P1 on 652821db).*
   Observable: exactly one QSO row + one `ft8-logged` event per rogered contact,
   carrying the session's frequency.

2. **SM never keys unless the daemon can POSITIVELY confirm the rig's frequency —
   and, for a session rung, that it still matches the session's.** This binds
   MANUAL sends too (`TransmitNext`), which do not go through `sessionTxGate` and
   so must repeat the check; skipping them left `/v1/ft8/tx/send` keying with an
   unreadable dial (codex P1 on 652821db). An unknown reading is a refusal, not a pass —
   `bridge.TxReady` checks connection and identity and does NOT require the
   selected VFO to have been decoded, so "ready to key" and "we know where we
   are" are different facts (`Service.dialState`, tracked vs known). With no CAT
   at all the check is inert, because that deployment cannot key. **The check that
   COUNTS is the one adjacent to PTT** (`Service.preKeyDialCheck`, installed via
   `TxController.SetPreKeyCheck`): a manual send is committed up to ~15 s before it
   keys, and the rig's state can change in that window, so a check made when the
   request was accepted proves nothing about the moment of keying. Request-time
   checks are a courtesy — a fast refusal for a send that is already doomed — and
   they go LAST in precedence, after armed / active / in-flight / readiness, or
   they mask those conflicts (codex P1+P2 on 0d180e59).
   Observable: no PTT, and `ErrTxDialUnknown` → 503 `rig_dial_unknown` on a start.

3. **The last published `ft8-qso` status always reflects the live session.** A
   terminal publish happens while the sequencer lock still excludes a replacement
   `Start*`; publishing after the unlock lets a newer session publish ACTIVE first
   and be overwritten by the stale idle, leaving the hub caching idle for a live
   session and the operator without controls. `finalrung.go` documents this; it
   has been found by review FOUR times — the fourth (`NextAnswerer`, codex P2 on
   a9e51f96) inherited the shape by copying `SetSkipIfSilent`, which had it too. A
   comment was demonstrably not enough, so the operator-command entry points now
   carry an executable guard: `publishatomicity_test.go` asserts the sequencer lock
   is HELD at publish time (TryLock succeeding means it was not), and `newTestSeq`
   installs that probe on EVERY test sequencer so the whole suite enforces it on any
   path it drives (`publishguard_test.go` collects violations by source location and
   reports them from TestMain). All 39 sites across the four sequencer files were
   converted on 2026-07-27; the pattern to never reintroduce is
   `s.mu.Unlock()` … `s.publish(...)`. Publishing under the lock is safe because the
   hub's publish takes its own mutex and sends NON-BLOCKING per subscriber (slow
   readers are evicted), and never re-enters the Sequencer. Enforcement is BOTH:
   the runtime probe (paths any test drives) and a source-level AST check
   (`TestSource_NoStatusPublishedAfterUnlock`) that is independent of coverage —
   necessary because 23 of the 39 sites are executed by no test, so the probe alone
   would not catch a regression in them. The AST check understands three forms —
   direct `s.publish`, a local ALIAS of it (this package really uses that, to hand
   the sink to a completion callback), and a call to any Sequencer method that
   publishes transitively — because a guard that knew only the first could be evaded
   by a rename (codex P2 on 603cd026). Methods that take `s.mu` themselves are
   exempt: they own their ordering, which is what `publishCurrent` is for (11 correct
   call sites depend on that exemption). The exemption is STRUCTURAL and now minimal: `s.mu.Lock()`
   must be the method's FIRST statement. Two looser rules each had a hole — "a Lock
   anywhere in the body" passed a conditional or closure-only lock (codex P2 on
   e3a7e605); "before any control-flow statement" passed a bare block hiding an early
   return, and a publish placed BEFORE the lock, since a call is not control flow
   (codex P2 on 30be7fb5). Both attempts enumerated what to REJECT and both
   enumerations were incomplete, so the rule now enumerates what to ACCEPT and the
   accepted set has one member. An unsound exemption is worse than none, because it
   reads as coverage.
   Observable: the final `ft8-qso` frame matches whether a session is running.

4. **No decode is displayed as workable, spotted, or acted on unless its capture
   window is attributable to ONE known frequency.** Every consumer downstream
   resolves a decode against the CURRENT dial, so a window spanning two
   frequencies produces stations rendered as workable where they are not, an
   answer keyed at nobody, and wrong spots published to PSK Reporter. A slot whose
   dial moved is suppressed like a TX slot — the empty `ft8-decode` still fires so
   the slot clock ticks.
   Observable: no decode rows, no spots, no sequencer advance from such a slot.

5. **A session ends only by: operator abandon, disarm, its completion policy, or
   a failed frequency confirmation — INCLUDING one that fires asynchronously.**
   The dial guard's full behaviour is specified as executable tests in
   **`dialguard_test.go`** — read that file before touching anything on this path.
   It was written BEFORE the implementation (2026-07-27), after ten rounds in which
   the rules were inferred one at a time from whatever the last review noticed; each
   inferred rule was wrong in a case the next review found. NO TOLERANCE is an
   operator decision, not an oversight: survivability depends on where the partner
   sits in the passband and which way you moved, so there is no clean edge to pick,
   and every threshold tried before the spec existed was wrong. A dial change now
   also DISARMS TX — including with no session running, because an arm is bound to a
   frequency just as a session is.
   The pre-key gate refuses inside the launched TX goroutine, so its caller cannot
   run the synchronous refuse-then-retire policy; `startTransmission` carries an
   `onDialRefusal` hook for exactly that, invoked strictly AFTER the completion
   callback. Without it a rung with no completion policy (most of them) suppressed
   PTT and left the exchange running (codex P1 on e0207074). Nothing else may retire one, and anything
   that does must be generation-scoped (`AbandonIfCurrent`) so it cannot end a
   session that replaced it. *An unconditional abandon driven by a stale capture
   slot killed a valid session started on the new dial in the meantime.*
   Observable: a session started after the triggering event survives it.
   **And the operator must be able to SEE it end and why** — the terminal
   `ft8-qso` frame carries `end_reason` (`dial_moved` | `dial_unknown`) whenever
   the operator did not cause the end. A safety stop nobody can see is
   indistinguishable from a hang: the first on-air read of a WORKING dial guard was
   "moving the dial does not stop TX", and it took a log dive to establish that it
   had (dogfood 2026-07-27). The terminal frame is the ONLY carrier of that
   reason, so nothing may republish over it: `publishCurrent` returns early when
   the session is idle, because `transmit()` returns as soon as its goroutine
   LAUNCHES and an async refusal can end the session before any post-transmit
   publish runs.

6. **Every completion that ENDS a session performs the SAME session-identity
   transition** — retire the generation, consume any staged teardown reason, clear
   the ladder's state, and publish the terminal status while the lock still
   excludes a replacement start. One primitive, `retireSessionLocked`; doing it by
   hand is how four paths drifted into three different versions, so that a stale
   callback could not be told from a live session and the dial guard's explanation
   vanished when a completion won the race. Call-CQ is deliberately NOT one of
   these — it RESUMES CQ rather than ending, a different transition.
   Observable: after any ending completion the generation has moved on, and the
   terminal frame carries whatever reason was staged.

   **This binds the ABANDONMENT paths too, not just the completions** (2026-07-27).
   F2 converted the four ways a session ends because a contact FINISHED; the ways it
   ends because the contact FAILED — the repeat cap, an armed skip firing, the
   defensive exhausted-exchange branches — were still hand-rolled at 19 sites, so
   they retired no generation and dropped any staged `end_reason`. Both are
   observable: a stale callback cannot be told from a live session, and a dial-guard
   teardown that loses the race leaves the operator watching a session stop with no
   explanation. All 19 now call the primitive, which also clears the per-session
   operator flags so it is the WHOLE transition. Enforced structurally:
   `TestSource_SessionsEndOnlyThroughThePrimitive` requires every write to `s.mode`
   outside `retireSessionLocked`/`abandonLocked` to assign an enumerated ACTIVE mode
   (i.e. to be a session START); anything else — `seqIdle`, a variable holding it, or
   an unreadable multi-value call — fails. It needs TWO rules, because a session ends
   when mode becomes `seqIdle` and there are two disjoint routes: naming the
   constant, or reaching zero without it. (1) outside those functions `seqIdle` may
   appear only AS a comparison operand or case expression — never nested beneath one,
   stored, aliased or passed; (2) any assignment to a `.mode` selector must name an
   enumerated ACTIVE mode, and arithmetic on one is refused; (3) `&x.mode` is refused
   outright — UNIVERSALLY, including inside the primitives themselves, since a
   primitive that leaked the pointer would let a write elsewhere carry neither a
   `.mode` lvalue nor `seqIdle` and pass all three rules (codex P2 on 33c66232). The
   primitives are exempt from (1) and (2) only; nothing anywhere needs that address,
   which makes the universal rule simpler to state than the carve-out it replaced — `seqMode` is
   integer-backed and `seqIdle` is 0, so `s.mode = 0` and `seqAnswering(1)--` reach
   idle silently. Rule 2 matches ANY `.mode` selector without asking whose it is,
   which is sound because nothing else in the package assigns to a `.mode` field, and
   is what finally made the guard immune to naming after five rounds of tracking
   receivers, parameters and aliases (61a875d8, 5cffed06, 980c9e04, 0c80f894,
   bd2f31fa). **The costly lesson was not any individual hole: it was
   rewriting the guard around one axis and DROPPING the others. That happened TWICE —
   the lvalue rule (bd2f31fa) and then the address rule (95b5da25), the second one a
   round AFTER "enumerate what the old check caught" was written here and not applied.
   The rules above were finally settled by auditing every check any earlier version
   performed against the current one, which is the step that should follow any
   rewrite of a guard.**
   That makes THREE guards in this package fixed by inverting a denylist into an
   allowlist, so treat the allowlist as the default shape for a new guard here rather
   than the remedy after a review finds the hole.

7. **A control that stops RF is offered only where it can actually stop RF.**
   Skip-if-silent means "if they do not come back, end the session instead of
   repeating this rung" — a property of the RUNG, not of the session mode. The
   code treated it as a mode, so every answer/work mode accepted an arm and the
   status advertised `skip_armed`, while the skip check is only ever reached on
   PRE-FINAL rungs: type-4 work (whose sole rung IS the terminal RR73) and any
   ladder already on its final rung armed a stop that could never fire. A false
   promise on the TX path is worse than no feature — this is the button pressed
   when the operator wants the radio to stop. `rungSkippableLocked` enumerates the
   pre-final rungs POSITIVELY and defaults to false, so a new mode must claim
   skippability deliberately: failing safe costs an unavailable button, failing
   open costs a stop that never comes. Refusing is the operator's decision
   (2026-07-27) over inventing final-rung skip semantics — Abandon already ends a
   contact, and a second meaning on the rung that decides whether a QSO logs is
   not worth the ambiguity. The refusal is DISTINCT on the wire
   (`ft8_rung_not_skippable` vs `ft8_no_active_qso`): "nothing is running" and
   "this rung cannot be skipped" lead to different operator actions.
   Observable: an arm the sequencer refused is never reported as armed, and
   disarm is always accepted.

## Adding a sequencer mode — the coordinated-edit list

Nothing enforces this, and no abstraction should be invented to (the modes differ
in ways that matter — see "Build specific, not generic"). It is a checklist because
that is what it honestly is. Surfaced by the 2026-07-27 package review: seven modes
across four protocol families, each of which had to be taught to every one of these
sites, and skip validation was the one that got missed.

A new mode must be added to: `OnSlot` dispatch · `ActiveCallsign` ·
`rungSkippableLocked` (invariant 7 — omission means NOT skippable, which is the
safe default) · `abandonLocked` · `statusLocked` · `fireOpening` (or a deliberate
decision not to fire an opening, recorded at the start function — see
`StartCallCq`) · the completion snapshot (`completed*QsoLocked`) · and the
Service-side staging in `servicetx.go`.

Three structural rules the modes already obey, worth keeping:

- **New per-contact state goes INSIDE `contactFlags`, not beside it.** A field
  whose lifetime is one contact and which is added as a bare `Sequencer` field
  must then be remembered at nine reset sites (seven `Start*`,
  `retireSessionLocked`, `abandonLocked`); added to `contactFlags` it is reset by
  all nine for free, because each does `s.contact = contactFlags{}`. The struct's
  doc comment enumerates what deliberately stays OUT and why — `autoWork*` (a RUN,
  which outlives a completed contact), `confirmHold` (set *during* the retire it
  outlives), `stalledCalls`/`stallCooloff` (exclusion memory on their own clocks),
  `lastTxSlot` (a property of the RIG, and what stops two sessions transmitting in
  one slot across a start/abandon boundary). Answer that question before adding a
  field; the grouping exists to make it answerable rather than to save typing.
  *The gap that prompted it: `nextArmed`'s own comment claimed it was cleared "at
  session start", and six of the seven starts did not clear it. Harmless only by
  luck — both routes to `seqIdle` clear it, so no start could observe it set.*

- **An active mode always has exactly its corresponding exchange pointer**
  (`seqAnswering`↔`ex`, `seqWorking`↔`caller`, and so on). The nil checks scattered
  through the switch statements are defensive, not a supported state: a mode set
  without its pointer leaves `mode` and the published status disagreeing, and the
  operator sees a session the sequencer cannot advance.
- **A mode's family decides its final-rung policy, and the Group A/Group B split
  is not symmetric** — it turns on whether the PARTNER already holds a complete
  QSO (Group A: they rogered, so log whether or not our courtesy closer keys;
  Group B: we owe them the roger, so retry and log only on true on-air success).
  Standard answer / FD work / type-4 answer are Group A; standard work / Call-CQ /
  FD answer / type-4 work are Group B. Copying the wrong sibling's policy is an
  easy mistake that either loses a real QSO or logs one the partner never got.

**Corollary that cost three rounds:** if a behaviour test for one of these cannot
be written without inventing a fact the system does not carry, the SYSTEM is
missing that fact — do not settle for a threshold, an age check, or a heuristic in
its place. The occupancy quarantine, the freshness gate and the slot-distance test
were all attempts to infer "was this captured after the QSY?" from data that could
not answer it; the fix was to make the daemon stamp the frequency it measured on.

## Further context

Current FT8 status and subsystem detail live in `docs/ft8.md`. Shipped chronology remains in Git
history and the session archive; load it only when investigating the history of a specific change.
