---
number: 0067
title: FT8 runs — one rule: the Answer mode alone decides how callers are worked
status: Accepted (2026-08-08 — behavioural model AND the UI representation
  both operator-ratified in conversation, worked through entry point by
  entry point; not yet built)
date: 2026-08-08
---

# 0067 — FT8 runs: one rule — the Answer mode alone decides how callers are worked

## Context

The run controls grew one widget per ADR, each correct locally, never
unified: the armed pill (ADR 0059), the per-click intent — checkbox, chord,
`auto_work` wire flag, arm-refusal (ADR 0065), the session Answer-mode
selector (ADR 0066), the daemon answerer list + drawer for pick CQ runs
(0065 decision 3), and the older SPA-side curated pile-up stack with its
auto-drain (2026-06-17). Within hours of ADR 0066 going live the operator
identified the result: three control grammars (dropdown, checkbox, pill) for
one concept, in three places, with the same panel slot MORPHING between
control types — "things are still inconsistent and not obvious … we need a
more consistent approach, which explains itself."

Working the model through case by case (operator, 2026-08-08) also exposed
the deepest inconsistency, which is behavioural, not cosmetic: **"I pick"
means different things in different places.** On a Call-CQ run the daemon
lists answerers and the drawer pops them; on an answered CQ or a directed
call the arm is refused outright and the only manual path is the separate
curated stack — a different mechanism with different data that the operator
has to know about.

## Decision — the model (ratified in conversation, 2026-08-08)

**One rule, five entry points.** However a session starts — answering a CQ,
calling CQ, double-clicking an idle station, double-clicking a station
mid-QSO with someone else (call until it responds or the repeat cap ends
it), or working a caller — stations that then call us are treated per the
session's **Answer mode**, and nothing else:

- **`auto_first`** — a run works all subsequent callers, first come first
  served.
- **`auto_strongest`** — the same, strongest SNR first.
- **`operator_pick`** — nothing transmits until the operator chooses. The
  daemon lists callers in EVERY entry point (extending 0065 decision 3's
  CQ-only listing); the operator either works one at a time, or bags
  several into the queue — and **bagged stations are auto-worked from the
  queue in order**. Each was individually chosen, so working them without a
  further per-contact gesture keeps every transmission operator-selected.

Consequences the rule already implies, stated so nobody re-derives them:

- **The mode alone starts runs.** Under an auto mode, every operator-started
  session is a run — no arming gesture. This SUPERSEDES ADR 0065 decision 1
  (plain click works one station only) and retires the intent grammar with
  it. 0065's origin — silent run-arming surprised the operator — is answered
  differently now: the opt-in is the *visible, session-scoped mode
  selection*, not a per-click modifier. "Work just this one station" has a
  natural home: **pick IS the manual mode** — work them, choose nobody else.
- **A timed-out directed call can leave a live run.** W4 already rules that
  a run survives an uncompleted contact, so calling a station that never
  answers, under an auto mode, flows into working whoever called us
  meanwhile. Consistent, and worth the operator knowing: Stop (the pill)
  ends a run a failed call left behind.
- **The pick asymmetry dies.** The daemon's caller list exists in every
  mode-pick session; the curated stack stops being a separate mechanism —
  its bag-and-drain becomes pick's queue.
- **The licensing posture strengthens.** The default (`operator_pick`,
  ADR 0065 dated note) now means a fresh station is FULLY manual across all
  five entry points; automation exists only behind a visible mode choice.
  Operator-initiation is untouched: every transmission traces to an
  operator action — the entry-point click, plus (under pick) the pop or the
  bagging.

## Retired by this decision

- The `auto_work` per-click wire intent on `qso/start` / `qso/work`, and
  the staged-intent half of the arming gate.
- The "Auto-work the next contact" checkbox and the ctrl+shift+click chord.
- The arm-refusal under pick (invariant 7's application here reframes: a
  pick run never auto-transmits, so there is nothing to refuse and nothing
  falsely advertised).
- The toggle-disabled-under-pick visibility rule (ADR 0066 fork 6) — moot
  with the toggle gone.
- The curated pile-up stack as a distinct mechanism (its drain semantics
  live on inside pick's queue).
- The `ft8.tx.auto_work_callers` config key and its `ft8_auto_work_callers`
  GET seed — the toggle they seeded no longer exists. (Removal tolerates
  the legacy key on disk, as `alc_red`'s removal did.)

## Survives unchanged

- Run STATUS + the Stop control (stop the run, never the active contact) —
  though their WIDGET changes: the pill retires into the run surface below,
  which owns both.
- The repeat cap, Next, Abandon (and Abandon's pause-the-drain behaviour is
  an open question below, not silently inherited).
- ADR 0066's session/default split: the Answer mode stays session state
  seeded from `ft8.tx.caller_answer_mode`; the selector still locks while a
  run is active or armed.
- Run stop conditions (disarm, CAT loss, dial move, band change).

## Decision — the UI (ratified 2026-08-08)

**One run surface, in the panel slot the checkbox/chip morph occupies
today, replacing the morphing outright.** Fixed structure in every state
(the audio card's V6 lesson) — three rows; only text and the Stop button's
presence vary:

    ┌─ Run ────────────────────────────────┐
    │ Answer mode  [First answerer ▾]      │
    │ ● <state line>                       │
    │ [ Stop run ]     (only when live)    │
    └──────────────────────────────────────┘

- **The Answer mode selector RELOCATES into the surface.** The TX control
  bar's parameter row keeps only the genuinely CQ-scoped `CQ slot` parity
  selector — resolving the location-implies-CQ complaint — and the bar
  keeps its verbs (Call CQ / Abandon / Next).
- **The pill retires.** The surface's state dot + line IS the run status;
  `Stop run` replaces the pill click.
- **The state strings are the ratified wording** (they are the
  "explains itself"):

  | Mode · state | Dot | State line |
  |---|---|---|
  | auto · no run | grey | "Your next contact starts a run — callers worked first come" / "…strongest first" |
  | auto · run live, in contact | amber | "Run live — working DL9UW (first come)" |
  | auto · run live, waiting | amber | "Run live — waiting for callers (strongest first)" |
  | I pick · no session | grey | "Manual — callers will be listed; nothing transmits until you choose" |
  | I pick · listing | blue | "3 calling you — open the drawer to work or bag" |
  | I pick · queue draining | amber | "Working your queue — 2 bagged left" |

- **`Stop run` on a draining queue PAUSES the drain** (ratified): the queue
  is kept and Resume (drawer) continues it — today's stack semantics carry
  over deliberately, so a stop never costs the operator their curated
  choices.
- **The state line is click-to-open** for the drawer (ratified): an
  operator-initiated click is not the unprompted surfacing the badge-only
  discovery rule (0065) forbade — that rule stays for toasts/auto-open,
  which remain out.

## Acceptance criteria (ratified with the model)

1. When I start a session by ANY of the five entry points with the mode on
   an auto setting, callers are worked hands-off in that mode's order — and
   I can tell a live run from a stopped one at a glance from the run
   surface, in the same place, in every case.
2. When the mode is "I pick", NOTHING transmits beyond the contact I
   started until I choose a station; callers appear in one list, the same
   list, whatever the entry point — and I can tell "listing, waiting for
   me" from "auto-working" from "no run at all".
3. When I bag stations under pick, they are worked from the queue in order
   without further gestures — and I can tell a bagged station apart from a
   merely-listed one.
4. When a directed call times out under an auto mode and someone called me
   meanwhile, the run works them — and the pill tells me a run is live even
   though my original call failed.
5. Nothing anywhere arms silently: every run is explained by a control I
   can see (the mode selector), not by a modifier or a hidden setting.

## Open questions — the operator's calls, deliberately unfilled

- Queue order under pick: strictly bag order? Reorderable (the old stack
  had ↑)?
- Abandon vs the pick queue: `Stop run` PAUSES the drain (ratified above);
  whether Abandon does the same (today's stack behaviour) or also stops the
  run — decide at build time with a test pinning the choice.
- Whether `max_repeats` joins the same surface (carried over from ADR
  0066's open questions).

## Consequences / build notes (deliberately sketch-level until the UI half)

- Daemon: caller LISTING generalises from the CQ run to all session types;
  the pick queue becomes daemon state (the drain transmits, so the daemon
  must own it, per ADR 0059's "the daemon owns run state"); the arming gate
  simplifies to "auto mode ⇒ run". The 0065 G/W rule families re-derive
  again; the 0066 R-rules mostly survive (the mode carriage is unchanged).
- SPA: the checkbox/chord/intent plumbing comes out; the drawer becomes the
  one caller surface; SP-rule re-derivation.
- Wire: `auto_work` retires from requests; frames carry the list in every
  session mode; a bag/unbag endpoint joins pick's pop.
- Docs: ft8.md run-model section rewrites; 0059/0065/0066 get dated notes.
