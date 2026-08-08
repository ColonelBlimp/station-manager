---
number: 0066
title: FT8 run knobs are session state; config.json holds only their defaults
status: Accepted try-and-adjust (operator, 2026-08-08: "Let's try it and
  adjust from there" — the drafted shape builds as-is; the proposed forks are
  provisionally ratified and expected to be revised from use)
date: 2026-08-08
---

# 0066 — FT8 run knobs are session state; config.json holds only their defaults

## Context

Within one hour of the `operator_pick` default flip going live (ADR 0065 dated
note), the operator hit "auto-work next contact is not working" on the air.
The diagnosis was correct behaviour: `caller_answer_mode` was `operator_pick`
(explicitly, from an earlier test-preparation edit) and
`ResolveFt8AutoWorkCallers` refuses to arm under the pick mode (invariant 7).
But reaching that diagnosis needed a code-side resolver trace, and the remedy
needed a config edit with the daemon stopped — mid-session, at the rig.

The operator's ruling, verbatim in substance: **"The way this works is too
confusing. We need to simplify for the user. All the config knobs should be
available session based (as with the checkbox in the UI for 'Auto-work the
next contact') with a default set in the config.json."**

The confusion has a precise shape: one knob doing two jobs. `ft8.tx.
caller_answer_mode` is simultaneously a durable preference (a config.json
field) and the live behaviour of the run about to start (read at CQ start) —
so changing how *this* run answers means stop-daemon → edit file → start.
Meanwhile auto-work is half-modernised: ADR 0065 made the arming intent
session-based (toggle + ctrl+shift+click), but a config knob still gates it
and can silently veto the visible control — which is exactly what happened.

The in-house precedent is **`tx_parity`** (session 193, 2026-06-26): a
selector in the TX control bar, plain operating state, carried on
`POST /v1/ft8/cq/start`, explicitly "settled as operating state, no config
field". The sequencer is already built for this shape — `StartCallCq` takes
the answer mode as a parameter; only the wire and the SPA treat it as config.

## Decision (per-fork status marked)

1. **RATIFIED (operator, 2026-08-08): the Call-CQ answer mode becomes a
   session start parameter.** A selector in the TX control bar's parameter
   row offering the three modes (First answerer / Strongest / I pick),
   carried on `POST /v1/ft8/cq/start` exactly as `tx_parity` is, and read by
   the daemon from the REQUEST, not from config, at run start.

2. **RATIFIED (operator, 2026-08-08, revised same conversation): single
   centred parameter row of the two selectors** — `Answer | CQ slot`, one
   row, above the Call CQ / Abandon / Next button row. (A two-row layout was
   rejected on the space argument; the `TX offset …` readout is REMOVED from
   the bar as duplicated in the Occupancy panel. The one thing that readout
   uniquely explained — "no clear channel yet" on a disabled Call CQ — moves
   into the Call CQ button's disabled tooltip so the explanation survives.)
   ADJUSTED from use, same day: the two selectors justify to the row's far
   ends (`justify-between`, was centred), and the label reads "Answer mode".
   The first adjust of the try-and-adjust phase — recorded, not re-ratified.

3. **Parity-precedent semantics (proposed):** the selector is
   `disabled` while a run is active — changes apply to the next run, which
   sidesteps mid-run mode-change semantics entirely; and the session value
   RESETS on page load to the config.json default (parity is plain `$state`;
   "config is the default" is only true if the default actually reasserts).

4. **Proposed: `ft8.tx.caller_answer_mode` becomes the default seed only.**
   Served resolved on GET as today (the SPA seeds the session selector from
   it); edited by the Settings→FT8 dropdown (the filed port gap, which then
   becomes a *defaults* editor). With the mode session-scoped, the ADR 0065
   "config.json-only" fork and its PUT 400 on `operator_pick` retire — they
   guarded a world where config WAS the live control. The PUT then accepts
   all three literals as defaults. (Fork retirement needs explicit
   ratification: it reverses a ratified decision.)

5. **Proposed: `ft8.tx.auto_work_callers` becomes the toggle's boot default
   instead of a hard gate.** The session toggle (ADR 0065) becomes the only
   live control; config seeds its initial state. ADR 0059's concern —
   enabling on upgrade must not change on-air behaviour without asking — is
   preserved: the default stays off unless config says on, and arming still
   requires the per-click intent.

6. **Cross-consistency is visible at the control, not silent at the gate:**
   with the session mode on "I pick", the auto-work toggle renders disabled
   with a title explaining why (a pick run cannot auto-work; invariant 7) —
   and vice versa the selector explains, never a silent arm refusal.

7. **The licensing invariant survives and gets clearer.** An unconfigured
   station starts every session at `operator_pick` with auto-work off
   (the 2026-08-08 default flip is unchanged); every automation becomes a
   VISIBLE per-session opt-in gesture instead of a config edit. Nothing
   here touches operator-initiation: sessions still start only from an
   operator action.

## Open questions — the operator's calls, deliberately unfilled

- **Scope of "all the config knobs":** does `ft8.tx.max_repeats` join the
  session surface (a small number input in the Operate area) or stay a
  Settings-level default? It is already live-applied via PUT, so the
  config-edit pain that motivated this ADR does not apply to it.
- Whether fork 4's PUT change (accepting `operator_pick` as a default)
  is ratified — it retires a ratified ADR 0065 fork.
- Whether fork 5 (gate → default) is ratified — it amends the ADR 0065
  arming grammar's gate half (G-rule tests re-derive).

## Consequences / build notes

- Wire: `POST /v1/ft8/cq/start` gains `answer_mode` (validated against the
  three literals; absent → the config default, so old clients keep working).
- Daemon: `StartCallCq` reads the request's mode; the config resolve feeds
  the GET default only. The arming gate reads the session state, not config.
- SPA: session `$state` seeded from the config GET; the selector in the
  parameter row; the toggle-vs-pick visibility rule; the pile-up drawer and
  badge behaviour unchanged (they follow the run's `answer_mode` frames).
- Tests: the ADR 0065 G-rules' gate half re-derives against fork 5; the
  operator-pick P-rules are unchanged (run semantics don't change, only how
  the mode is chosen); parity-selector tests are the template for the
  selector's lock/reset rules.
- Docs: ft8.md (the caller_answer_mode section rewrites around
  session-vs-default), api-endpoints.md (cq/start + the PUT change if fork 4
  ratifies), config.md (ft8.tx becomes a defaults block), the inbox port-gap
  entry (the dropdown becomes a defaults editor and its read-only-operator_pick
  constraint dissolves if fork 4 ratifies).

## Alternatives considered

- **Status quo (config-only live control):** the confusion that prompted
  this — rejected by the operator on the day it bit.
- **Two-row parameter layout:** rejected (operator): little horizontal
  space; single centred row chosen.
- **Session-persistent selection (localStorage, survives reload):** cuts
  against "config.json holds the default" — a sticky session choice IS a
  second config store, and the parity precedent resets. Proposed against.

## Built (same day, 2026-08-08)

Both halves, TDD with reversion probes on each:

- **Daemon** — spec `internal/ft8/adr0066_test.go` (R1–R6, criteria +
  confusables in the header): `cq/start`/`qso/start`/`qso/work` carry
  `answer_mode` (junk 400s at all three, `TestHandleFt8Qso_RejectsJunkAnswerMode`);
  `Service` start methods take the session mode, empty → config default
  (`effectiveAnswerMode`); the arming gate consumes staged intent + staged
  session mode (`armAutoWorkLocked`; the run pins `autoWork.selectMode` —
  not "mode": the sessionend AST guard rightly refuses any `.mode` selector
  write); `SetAutoWorkCallers`/`autoWorkPolicy` deleted, the service-init
  install removed. Re-derived: W2 (knob-off → no-intent), W10 (config-alone →
  config-DEFAULT-through-the-Service-path), V3, G2, the autoWorkRun helper,
  stallcooloff. Fork 4: the config PUT accepts all three literals
  (`TestConfig_Ft8CallerAnswerMode_AllThreeLiteralsAccepted`); GET gains
  `ft8_auto_work_callers` (the toggle seed).
- **SPA** — spec `ft8AutoWork.svelte.test.ts` SP1–SP4: `ft8State.answerMode`
  seeded by `setFt8SessionDefaults` from the config GET (junk ignored; plain
  state, reload reasserts the default); the Answer selector in the centred
  `Answer | CQ slot` row (locked while a run is active; the TX-offset
  readout removed, its no-offset explanation now the Call CQ button title);
  `callCq`/`answerCq`/`workCaller` carry the session mode; under "I pick"
  the intent is dropped at the source and the toggle renders
  disabled-with-reason (fork 6); the refused-sink toast reworded to name the
  mode, and its rule re-derived to run under an auto mode (the refusal path
  now requires the intent to have been sent).

Watch-list for the adjust phase: whether the seeded one-shot toggle
(config `auto_work_callers: true` → lit once per page load) feels right in
use, and whether `max_repeats` joins the session surface.

---

**Dated note (2026-08-08, ADR 0067):** the core of this ADR — the knobs are
SESSION state, config.json holds only defaults, `answer_mode` rides every
start — survives and is how ADR 0067's one-rule model gets its input. What
0067 retired from here: fork 5's arming gate (intent + auto mode → now the
mode ALONE), fork 6's toggle-disable (the toggle itself is gone), and the
`ft8_auto_work_callers` GET seed. The Answer selector moved from the TX
control bar to the run surface. The watch-list's "does the seeded one-shot
toggle feel right" resolved itself: the toggle retired before it was ever
dogfooded; `max_repeats` joining the session surface remains open.
