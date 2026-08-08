# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

**Structure (reworked 2026-08-02 — orientation and the record are now separate):**

- **`## Now`** — the ONLY section the SessionStart hook injects. Under ~25
  lines: where we are, what's next, what must not be started. Read it first.
- **`## Current state`** — the rolling detailed record, newest arc first. NOT
  injected. Read it when `## Now` isn't enough.
- **`## Active cycle`** — the 1–3 things in flight, newest block first.
- **[`session-handoff-archive.md`](session-handoff-archive.md)** — everything
  rolled off. Grep it; don't read it.

**Why the split.** Until 2026-08-02 the hook sliced `## Current state` at a
prose marker. The marker had been removed from the doc and nothing noticed, so
the hook emitted the file from that heading to EOF — 231 KB. The harness caps
injected output, so each session got `Output too large` plus a 2 KB preview, and
the RECONCILE staleness warning printed underneath was **never delivered at
all**. A section that grows without limit cannot be the thing that gets
injected; `## Now` is bounded by editorial rule and is what the hook reads.

- **At session end:** update **`## Now`** and bump its `(as of YYYY-MM-DD)` —
  the staleness guard keys off that date. Add an arc to `## Current state` if the
  session did something a future reader would otherwise have to re-derive.
- **Rolling window (enforced 2026-08-02):** keep **3 arcs** in `## Current
  state` and **1 block** in `## Active cycle`; roll the rest into the archive
  (newest-first, verbatim). The previous policy said ~12 sessions and, before
  that, 2–3; neither was enforced and the doc reached 3,005 lines / 233 KB. If
  the hook ever prints its `TRUNCATED` notice, `## Now` has outgrown its budget
  — trim it, don't raise the cap.
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Now (as of 2026-08-08)


<!-- THE ONLY SECTION THE SessionStart HOOK INJECTS. Keep it under ~25 lines.
     It is ORIENTATION, not the record — "where are we, what's next, what must
     I not do". Detail belongs in Current state below, which is NOT injected. -->

- **AFTERNOON 2026-08-08: pushed through `2da99b89`, CI GREEN (14m03s,
  full pipeline — the 0066 arc is through the gate); daemon DEPLOYED at
  `1183-g2da99b89`.** Earlier CI drama: one silently-FAILED push (found
  via `ahead 2` — pushes can fail without anyone noticing) and one lint
  red (3 `no-unnecessary-type-assertion` a flaky local pass had hidden —
  casts→annotations). **TREE HOLDS the drive-alarm poll-witness fix,
  uncommitted** (next bullet).
- **DRIVE ALARM FALSE POSITIVE, live at 12:29 — fix PUSHED (`773cc0b9`,
  CI watched); tree holds its review P1 follow-up (DP4: re-arm from
  witness EXPIRY, not a fresh full window — the full-window re-arm could
  defer a real collapse past unkey). Wants: commit DP4 + redeploy:**
  `drive_no_output` fired on EVERY transmission while
  the ADR 0064 poll measured PO 104–108 at 53 answers/slot — full output.
  The pushed RM0 stream had collapsed to 6–8 frames/slot (hypothesis: the
  rig pushes on CHANGE and the envelope was dead flat, po_min=po_max=105;
  this also CORRECTS the morning's gap dismissal — those gaps tracked
  steady PO, not hands on the rig). Fix: the alarm now consults the poll
  as a second witness — a polled PO **> 0** inside the silence window
  withholds the alarm (zero IS the alarm's claim; a measurement beat it);
  polls at zero or stale still alarm (the real collapse keeps firing).
  Spec DP1–DP3 in `drivealarm_test.go`; new watch state `poll_output`;
  recovery untouched (it keys off the pushed gap). P4 intact — the push
  liveness bookkeeping is not fed.
- **ADR 0066 (Accepted try-and-adjust, designed+built same day): FT8 run
  knobs are SESSION STATE; config.json holds only defaults.** Born on air:
  "auto-work next contact is not working" was the flip working as ratified
  (explicit operator_pick in config from the 7c prep edit — NOT an absent
  key; my grep missed the space), and the operator ruled the config-knob
  model too confusing. Now: the **Answer mode selector** in the TX control
  bar (with CQ slot, justified to the row's far ends; TX-offset readout
  removed → Call CQ button title) is the live control, carried on
  cq/start + the auto-work intent; **the arming gate reads the session,
  not config** (`SetAutoWorkCallers`/`autoWorkPolicy` DELETED; config knob
  = the toggle's boot seed, served as `ft8_auto_work_callers`); under "I
  pick" the toggle disables-with-reason and the intent drops at the
  source; the config PUT accepts all three literals as DEFAULTS (the 0065
  fence retired). Specs `internal/ft8/adr0066_test.go` R1–R6 +
  `ft8AutoWork.svelte.test.ts` SP1–SP4b, all reversion-probed.
- **Review rounds on the arc:** d7fbf935 P1 (selector editable while
  idle-and-ARMED — a UI claiming "I pick" while an armed run auto-works;
  lock widened to `active || autoWorkArmed`) · a1a0aaca, c1e17c12,
  2da99b89 all clean. ALC chip is now label+dot (number on the card only).
- **OPERATIONALLY, at the rig right now (1183):** the Answer mode selector
  seeds to "I pick" from your config; flip it to **First answerer** to
  restore auto-working — no config edit, ever. A pick run = leave it and
  Call CQ (= check 7c in normal use). Until the alarm fix deploys, treat
  the "no RF output" banner as noise — every summary line carries the poll
  PO proving output.
- **NEXT:** on-air try-and-adjust (selector flow · pill on a work-caller
  arm · FD click · adjust-list: seeded one-shot toggle's feel,
  max_repeats session scope) · redeploy at leisure (cosmetics) · Settings
  dropdown = DEFAULTS editor now · findings 9/10 · paste-list ·
  Tune-coverage · Q4–Q10 · backlog strikes await the word. Morning's
  record (hardware 4/4, ADR 0064 Accepted, stuck-TX parked, PSK announce)
  is in Current state.

## Current state (as of 2026-08-08)

### 2026-08-08 (afternoon) — ADR 0066 designed+built in hours; the drive alarm's poll witness

**ADR 0066 — "the way this works is too confusing" became a shipped design
the same afternoon.** The flip's first on-air contact ("auto-work next
contact is not working") turned out to be an explicit `operator_pick` in
config (the 7c prep edit — my "key absent" grep had missed the space after
the colon, corrected in-flight), and the operator ruled the config-knob
model itself the defect: **all run knobs session-based, config.json holds
only defaults**. Designed criteria-first (layout forks ratified in
conversation: one row → far-ends justified → "Answer mode" label → TX-offset
readout removed into the Call CQ title), Accepted try-and-adjust, built both
halves TDD: `answer_mode` rides cq/start + the auto-work intent, the arming
gate consumes staged session facts (`SetAutoWorkCallers`/`autoWorkPolicy`
DELETED — the knob is only the toggle's GET-served boot seed), "I pick"
drops the intent at the source with the toggle disabled-and-explained, the
config PUT accepts all three literals as defaults. Re-derived en route: W2,
W10 (inverted to config-DEFAULT-through-the-service), V3, G2, the
refused-sink and AW fixtures; the sessionend AST guard rejected the first
run-mode field name (`autoWork.mode` → `selectMode`) exactly as designed.
Specs R1–R6 + SP1–SP4b, all reversion-probed. Reviews: d7fbf935 P1
(selector editable while idle-and-armed → lock widened, SP4b) · three clean
rounds. ALC chip trimmed to label+dot on request (value on the card).

**CI: a silently-failed push, a lint flake, then green.** The 0066 push
never reached origin (found via `ahead 2` — the run everyone assumed was
running didn't exist); the re-push failed on 3
`no-unnecessary-type-assertion` errors a flaky local pass had hidden
(getByTestId returns `any` here; casts→annotations, deterministic both ends
now). Run `31252713891` then went green — 14m03s, full pipeline.

**The drive alarm cried "no RF output" on every transmission (12:29) with
full output measured in the same log lines.** The pushed RM0 stream had
collapsed to 6–8 frames/13 s (the rig pushes on CHANGE; the PO envelope was
dead flat, po_min=po_max=105 — which also corrects the morning's gap
dismissal: those gaps tracked STEADY PO, not hands on the rig) while the
ADR 0064 poll answered 53×/slot at 104–108. Fix: the alarm consults the
poll as a second witness — polled PO **> 0** inside the silence window
withholds it (zero is the alarm's own claim; a measurement beat it), polls
at zero or stale still alarm, recovery untouched (keys off the pushed gap),
P4 intact. Spec DP1–DP3; new watch state `poll_output`; pushed as
`773cc0b9`. Its review found a real P1: the full-window re-arm trusted one
positive poll for ~2× the freshness bound and could defer a real collapse's
alarm past unkey (timer cancelled, nothing fires) — DP4 pins the timeline,
the re-arm now uses the witness's REMAINING lifetime; in tree awaiting
commit. Mid-session triage note: DL6LG stalling at the reporting rung was
propagation (−19 at their end), not the station.

### 2026-08-08 (midday) — antenna repro clean; the operator_pick default flip; the CI gate found red and fixed

**Stuck-TX: the antenna experiment closed the deliberate-repro programme.**
Pre-registered in the inbox before the run (per ATDD), executed by the
operator at 08:30: three 2 s tunes into the DX Commander on 20 m, every off
confirmed idle within ~2 s, zero alarms/re-unkeys/auto-offs in the whole
hour. With duration and FT8-residue refuted on 2026-07-24 and ingress now
unconfirmed, no hypothesis survives with evidence — the fault is filed
intermittent, deliberate keying stops, and the next natural occurrence on a
current build self-recovers (the txrecheck re-unkey retry) and logs richly.
Two corrections recorded on the way: tune power is the ADR 0027 code-const
20 W (the incident narrative's "30 W" was the SSB operating power, so power
fidelity held automatically), and the 07:23 `tune auto-off fired` lines were
the overdrive session's deliberate full-length carriers, not the 05:10
anomaly class. The backlog P1 "tx-alarm self-clear" validation stays open
but passive — it needs a real stick, and provoking one is no longer
justified.

**The operator_pick default flip (built TDD, dated notes in ADR 0065 +
0033).** Asking where the Settings knob for `caller_answer_mode` lived
surfaced two things: the knob is a PORT GAP (the dropdown existed only in
the retired logging SPA — inbox entry filed, `ft8_max_repeats` is the
natural same-card sibling since its "blocked on placeholder Settings"
reason expired 2026-08-05), and the DEFAULT contradicted the operator's
licensing intent. Ratified same session: `DefaultFt8CallerAnswerMode` =
`operator_pick`, scoped as the RESOLVE FALLBACK (option A — absent AND
invalid keys fail toward the non-automatic mode, everywhere, not just
fresh installs). Spec: `TestResolveFt8CallerAnswerMode` (hardcoded
literals), `TestResolveFt8AutoWorkCallers_AbsentModeDoesNotArm` (the
sharpest consequence: knob-on + no mode arms NOTHING), the api GET default
test; reversion probe put all five failures on their own assertions. Five
ft8 run-semantics tests had their fixtures opt into automation explicitly.
Docs swept (config.md, ft8.md, api-endpoints.md, ft8/CLAUDE.md). The codex
P1 on the follow-up ("fresh installs can't save — the frontend echoes the
resolved pick back and PUT 400s") was REFUTED with rationale in
handler_config.go: no served client echoes the field (app sends
display/psk/decode_log only; config SPA never touches it; the one client
that did was retired + un-embedded 2026-07-21). The kernel it contained —
a future hydrate-echo UI would 400 — is the read-only-render constraint
now recorded at the validation site and the inbox port-gap entry.

**The CI gate had been red ~2 days and nobody noticed.** Every push since
mid-2026-08-07 failed at golangci-lint: `runPipeline` and `onSlotWorking`
crossed the maintidx floor (both MI 19 vs 20) by GROWING with ratified
work — the ADR 0064 meter-poll lifecycle and the ADR 0065 arming
refinement. Not drift (2.11.3 pinned both ends, reproduced locally
byte-for-byte). Fix: exemptions-with-rationale in `.golangci.yml`, same
class as their already-exempted siblings readLoop/onSlotCalling
(dispatch-shaped, comment-dense, TX-path, invariant-covered) — "exempted,
not blessed", they join the refactor backlog the list is. Because every
red run truncated at lint, the tail (race, five build shapes, FT8 decode
test) had run dark for days — de-risked locally before the push (race
suite, static + pocketfft builds all green), then the push produced the
first fully-green run in 40+: **14m02s is the honest cost of the full
gate**. Habit fix folded into the codex-reviews memory: `gh run list -L1`
in the post-commit loop. Cosmetic follow-up noted: the workflow's
checkout/setup actions target deprecated Node 20 — a version-bump commit
someday, own-commit rule.

**Also:** the PSK Reporter IPFIX announcement was wordsmithed, its
test-collector detail verified (tree + external sources agree: production
`report.pskreporter.info:4739`, test listener 14739, analyse-don't-write),
and SENT to the PSK Reporter Google group — collector-side feedback, if
any, lands in the inbox. Incidental: our ipfix.go cites RFC 5101; the
current IPFIX RFC is 7011 (same wire version 0x000A) — cosmetic.

### 2026-08-08 (morning) — hardware-acceptance day: everything validated, ADR 0064 Accepted

Operator deployed at 05:59 (`1171-gf9444aac`, = HEAD at the time) and ran
the itemised acceptance list top to bottom. Commits: `eaba629b`
(ratification records) + `02733fa3` (the fold); both reviews clean.

**Passive batch — four for four, first try.** Deploy confirmed from the log
version field. VFO select validated exactly as pre-registered: VFO-B on a
deliberately different frequency, one click, the operating dial moved to
B's own content — VS is a true select, the manual legend holds, and archive
S~2527's "flag only" observation was the equal-contents confound (citations
written at verification time into `rig.svelte.ts` selectVfo + the inbox
entry). Selection-restore round trip confirmed on hardware. A26 boot-capture
demonstrated and RATIFIED (park on phone → boot the browser on
/app/operate/ft8 → rig untouched, confirming boot-is-not-a-switch → FT8
band click moves it → switch to phone returns it): the A4 amendment is no
longer drafted-only; the criterion header records the ratifying demo.

**The overdrive run — the datum was about the INSTRUMENT.** First attempt
"couldn't reach red": sink pushed to the 1.0 digital ceiling, RM ALC
answered max 30. Operator then reported the front-panel needle +20 dB OVER
the zone — and the log agreed something was badly hot: in-band PO collapsed
from the healthy 109–121 to ~35 on both PO witnesses across three slots
while ALC answered 29–30. Conclusion, measured: **the RM ALC answer
saturates at ~30 of 255** — it cannot distinguish zone-edge drive from
gross overdrive, §4 (iii)'s meter-face agreement fails in the over-region,
and no ALC-only threshold above ~30 can ever fire (even the 3900% mixer
typo would read 30). Measurement recorded in `internal/bridge/meters.go`'s
measured-on-hardware lineage. Meter-frame gaps of 2.0–2.5 s appeared ONLY
on hands-on slots (meter-face flip, mixer slide; hands-off ≤ 400 ms) —
dismissed on that correlation, re-check trigger: a 2 s+ gap hands-off.

**Red folded into amber (operator-ratified) — built TDD both halves.**
Daemon: `ft8.meter` is amber-only (`Ft8MeterConfig`/`Ft8MeterLevels`,
`DefaultFt8AlcRed` gone, no cross-clamp — 999 clamps to 255 now); served
shape pinned (`{"alc_amber":30}`); a legacy `alc_red` key is tolerated and
ignored (pinned; the dogfood config.json has no meter block at all —
verified). SPA: `red` state removed, amber terminal with the action label
("ALC high — reduce the audio level"), card marker moved to the amber
floor. Informative RED both halves, reversion probes both halves (each spec
failed on its own fold assertion against the restored old code), full gates
green. ADR 0064 flipped **Accepted** with a §4 results block (including the
qualified (iii) outcome and the gap dismissal); the §4 "iii"→"(ii)"
cross-reference fixed; config.md + api-endpoints.md match. The paired
ALC+PO overdrive detector (ALC-at-ceiling AND PO-collapsed — the run's
two-witness signature) is captured in the inbox with its open judgement
calls (PO floor, chip-vs-alarm, persistence), explicitly NOT built.

**Deploy note:** the fold is not live until the next redeploy — the running
daemon serves the old two-threshold shape, the SPA falls back fine either
way. Nothing rig-critical.

## Active cycle (the 1–3 things in flight now)

> **▶ RE-UPDATED 2026-08-02. Nothing is mid-flight — SHIP GATE (a) closed clean
> and the tree is committed, deployed and at HEAD. These are the next picks, in
> the order the operator and I last discussed them.**
>
> - **1. SHIP GATE (c) — notification records. THE LAST GATE ITEM.** The whole
>   notification category has no daemon record: toasts are client-side, several
>   with no daemon counterpart at all, so closing the tab erases them. This is
>   what still blocks "ship anything". (a) and (d) are done; (b) QSO deletes
>   remains open but is partly covered — the `qso_history` row lands, so
>   provenance survives; it is the admin-readable file that misses it.
> - **2. The config UI port — five tabs.** The operator's framing when picking
>   (a): *"(a) also points us toward completing the config implementation for the
>   UI."* The standalone config SPA is STILL SERVED at `/config/`
>   (`internal/api/server.go:309`), so nothing is unreachable — this is
>   consolidation debt under ADR 0044, not a functional gap. App-shell Settings
>   has **Station + Rigs**; still to port: **General (174 ln), FT8 (219),
>   Email (158), Enrichment (128), Forwarding (114)**. Budget realistically:
>   Station's 178-line tab became 413 lines with its state module and tests, and
>   Rigs' 273 became 1,273 — so ~2,500–3,500 lines over five increments, not one
>   sitting. **Now cheaper to verify:** every save the new tabs make is logged
>   with a field-level delta, so a ported tab is checkable against `smd.log`
>   rather than by eyeballing `config.json`.
> - **3. SSE reconnect on `visibilitychange`.** From the 2026-08-01 inbox triage:
>   the 07-28 "Cannot reach the daemon" report and the 07-18 map-staleness report
>   share ONE root cause — nothing recreates a dead `EventSource` when a tab is
>   restored. `mapData.svelte.ts:310` heals map DATA but not the stream. Fix once
>   at the SSE layer and both reports close.
> - **PARKED, operator-flagged "come back to this shortly":** `operator_pick` /
>   Call-CQ auto_off. A Call-CQ run ALWAYS auto-works answerers; the one manual
>   mode is accepted by config validation and REJECTED at runtime as
>   unimplemented. Detail in the `ft8-cq-answerer-selection` memory. **Not scoped
>   — do not start without the operator.**
> - **EVIDENCE NOW ACCRUING (no action, just don't lose it):** the three hub
>   eviction logs have been live since the 08-01 17:01 deploy. The operator's
>   standing instruction is **DO NOT TUNE THE BUFFERS** (8 ft8 / 64 bridge/events)
>   until those records show healthy clients actually being evicted. Zero
>   evictions in the 15 days before the feature existed.
