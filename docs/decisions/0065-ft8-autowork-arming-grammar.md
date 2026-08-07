---
number: 0065
title: FT8 auto-work arming grammar — per-click intent, and operator_pick as the pile-up
status: Accepted
date: 2026-08-07
---

# 0065 — FT8 auto-work arming grammar — per-click intent, and operator_pick as the pile-up

## Context

ADR 0059 made every operator-started session arm an auto-work run whenever
the `ft8.tx.auto_work_callers` policy is on — including answering a CQ,
deliberately, because "I answer a CQ and now stations call me directly" was
the case the feature was asked for (W9, `sequencer.go:687`). A week of
dogfooding shows the cost of policy-time arming: the run outlives the
contact by design, so every CQ answer leaves an **Abandon-debt**. On
2026-08-07 alone the operator had to explicitly stop a run he never asked
for twice (06:01:17 after the D2UY contact completed; 07:26:06 after the
C25ELO `no_answer` abandon), and filed three dogfood notes the same
morning: answering a CQ arms auto-work; there should be a way to answer
without arming; a click-modifier grammar sketch.

Separately, `ft8.tx.caller_answer_mode = operator_pick` has been accepted
by config validation and **rejected at runtime as unimplemented** since
ADR 0033 — a promise in the config schema with nothing behind it. The
Call-CQ path always auto-works the first grid-bearing answerer
(`auto_first` via `pickAnswererLocked`), and the SPA's pile-up queue — the
natural manual-selection surface — is disabled during CQ runs.

## Decision (operator-ratified 2026-08-07, all four forks)

1. **A plain single-click on a CQ works that station only.** It never arms
   an auto-work run. Arming becomes an explicit act, and the silent
   Abandon-debt becomes impossible. This supersedes ADR 0059's W9 arming
   rule *on the answer path* — an operator-started session is still the
   only thing that can head a run (the ADR 0059 safety posture is
   unchanged); what changes is that starting a session no longer implies
   starting a run.

2. **The run is armed by ctrl+shift+click on the CQ, mirrored by a visible
   Auto-work toggle** — two handles, one state. The modifier keeps the
   gesture one-action for the practiced operator; the toggle/pill makes the
   state discoverable and gives a future 7Q8AC operator a mouse-only path.
   The existing armed pill stops being status-only: clicking it disarms the
   run.

3. **`operator_pick` is built, on the Call-CQ side, as the pile-up shape.**
   During a CQ run with `caller_answer_mode = operator_pick`, answerers are
   queued into the pile-up drawer instead of being auto-committed; the CQ
   keeps calling until the operator pops one; the run works that station
   and resumes CQ after. This reuses the entire existing drain UI and
   un-disables the pile-up during CQ runs for this mode. `auto_first`
   remains the default and keeps today's behaviour.

4. **The "already worked this session" toast splits by evidence.**
   "Already worked" is reserved for a `session.qsos` hit (a logged QSO).
   An engaged-only hit (started earlier, abandoned or still in the async
   logging window) says: *"You started {call} earlier this session —
   nothing was logged. Working as new."* The engaged set itself — and
   `allow_duplicate`'s over-marking-is-safe direction — is unchanged; only
   the wording was wrong. (Live evidence: VK5GR 2026-08-07 07:20 — worked
   at 07:15, no reply, abandoned; toast claimed "worked" on the re-click;
   the contact then completed and logged as qso 7096.)

## Alternatives considered

- **Keep policy-time arming** (status quo): rejected on the measured
  Abandon-debt and the operator's explicit ask. The policy knob also made
  the *choice* invisible — nothing at click time said a run would start.
- **Modifier-only arming** (the original sketch): rejected as
  undiscoverable — a gesture with no visible state can't be learned from
  the UI, and the pill would stay status-only.
- **Toggle-only arming**: viable but slower for the practiced operator;
  the modifier costs nothing once the toggle exists to teach it.
- **Dropping `operator_pick`** (make validation reject it): least work and
  honest, but the pile-up UI already exists and the mode is the only
  answerer-selection control a busy CQ run has; the build reuses more than
  it adds.
- **Toast says nothing on engaged-only hits**: quieter, but loses the one
  genuinely useful reminder ("you tried them already and it went nowhere").

## Consequences / build notes

- The answer/work start requests must carry the per-click intent (an
  `auto_work` flag on `/v1/ft8/qso/start` and `/v1/ft8/qso/work`), and
  `armAutoWorkLocked` fires only when it is set. Daemon-side, because the
  daemon owns run state (ADR 0059).
- Disarming the run from the pill needs a path that stops the RUN without
  abandoning an ACTIVE contact — today Abandon is the only stop and it does
  both. New narrow endpoint (or a flag on abandon); decide at build time
  with a test pinning "disarm run ≠ end contact".
- `operator_pick` needs the daemon to publish answerer candidates during a
  CQ run (they are currently consumed silently by `pickAnswererLocked`) and
  an operator-pop path that commits the chosen caller into the run.
- The engaged-set toast split is SPA-only (`workedThisSession` callers in
  `Ft8BandActivity.svelte` / `Ft8Operate.svelte` learn WHICH source hit).

## Open questions — two RATIFIED at build time (same day, 2026-08-07)

- ~~**`ft8.tx.auto_work_callers` semantics after the change.**~~ **RATIFIED:
  a GATE.** `true` (default) allows the arm gesture/toggle; `false` means an
  arm request starts the CONTACT but arms no run — a refused arm must never
  cost the QSO in a 15 s window. The refusal is visible as the active
  `ft8-qso` frame carrying `auto_work_armed:false` after an intent-carrying
  start; the SPA toasts the explanation once (autowork_test.go G3 + the SPA
  refused-sink tests). The knob arms nothing by itself.
- ~~**Whether the Auto-work toggle can be armed while idle.**~~ **RATIFIED:
  yes — standing intent.** Toggle on while idle = "the next contact I start
  also starts a run", giving mouse-only operators a full arming path. The
  run still arms only ON a session start, so ADR 0059's operator-headed rule
  holds. The intent is ONE-SHOT (consumed by the start that carries it) and
  in-memory (dies with the tab; the RUN itself lives in the daemon).
- **Build clarification made explicit:** a start WITHOUT the intent also
  CLEARS any armed run — "work that station only" defines the operator's
  whole intent, and leaving the old run armed would resume auto-working on
  parameters pinned by a previous session (the W12 stale-pin hazard). Pinned
  by autowork_test.go G5.
- **ctrl+click on a CQ row** (the sketch's third gesture): today pile-up
  enqueue exists only for calling-YOU rows. Whether a CQ-ing station
  should be enqueueable too is a separate feature — still not decided.
