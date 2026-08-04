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

## Now (as of 2026-08-04)

<!-- THE ONLY SECTION THE SessionStart HOOK INJECTS. Keep it under ~25 lines.
     It is ORIENTATION, not the record — "where are we, what's next, what must
     I not do". Detail belongs in Current state below, which is NOT injected.
     The 2026-08-02 failure that created this split: the hook printed the whole
     Current-state section (231 KB), the harness truncated it to a 2 KB preview,
     and the RECONCILE warning underneath was never delivered at all. -->

- **HEAD is `4e637b42`; the DAEMON still runs `d13fcb22`.** The four coordinate
  commits are NOT live — they are preventive, and the bad rows they concern are
  historical, so this is not urgent. Everything from the morning IS deployed and
  was verified in the running build. `smd` is deliberately NOT auto-start — a
  stopped daemon is not a fault.
- **Shipped 2026-08-04, morning (DEPLOYED):** the **Settings navigation guard**
  (ADR 0063) and **FT8 session-end causes** — `Abandon()` is reached from twelve
  places and only the dial paths named themselves, so a session that DIED logged
  identically to one the operator stopped.
- **Shipped 2026-08-04, afternoon (NOT deployed) — the COORDINATE arc, four
  commits, six reviews, five P1s, every one mine.** Coordinates are decimal
  everywhere internally and convert only at the perimeter: provider ingress, both
  ADIF directions, and operator config. **The grid ARBITRATES rather than
  supplies** — precise coordinates survive while the grid vouches for them, and
  `AA00` is rejected as a sentinel. Four predicates now live once in `utils`
  (`CoordsValid` / `CoordsReadable` / `CoordsInsideGrid` / `IsPlaceholderGrid`)
  so the four layers cannot drift on what "contradicts" means. Detail + the
  review lessons in Current state.
- **EYEBALL WHEN CONVENIENT** (none of it seen by hand; vitest checks the DOM,
  not the look): the guard's confirm text, and F5 with an edit pending (browser
  dialog); after the next FT8 run, `smd.log` session-end reasons should be
  populated — an Abandon now reads `reason: operator` where the 08-04 run gave
  seven blanks (**but only when a session was ACTIVE**; abandoning with nothing
  running still logs nothing, which is correct); Email's password keep / type /
  remove states and its 587/30 placeholders; Enrichment's disclosures.
- **OBSERVE WHEN THE CHANCE ARISES — NOT tasks, and they gate nothing.** Do not
  schedule these and do not open a session asking whether they are done. SSE
  revival (background the MAP tab; **a negative result means the TRIGGER is
  wrong, not `openReviving`**) · FT8 dead-source watchdog · stuck-TX /
  RF-ingress 2 s tune trials INTO THE ANTENNA (operator's call on RF exposure).
- **NEXT ACTUAL WORK:** (a) **Playwright** layout check — NOT scaffolded at all,
  so config + CI + a first spec; (b) config-SPA retirement needs **FT8** and
  **General**, the last two unported tabs. ~~the R9LAU map bug~~ — **CLOSED**:
  the display was fixed 2026-07-30 and the data cause is now fixed at ingress.
  Still open on the map, none urgent: a **band filter** (dogfood-inbox 2026-08-01,
  untriaged), the **FT8 RX propagation overlay** (P2), the **whole-log Dashboard
  map** (P3, needs a `/v1/logbook/{id}/map` aggregate), and the SSE-revival item
  already listed below.
- **DECIDED, DO NOT REOPEN — no FT8 dupe guard.** Reaffirmed 2026-08-04 after a
  50-QSO run logged KK2A twice: they never copied our RR73 (asked twice,
  re-sent twice), so the re-work was CORRECT on-air behaviour. The reasoning is
  in `caller_sequencer.go` above `confirmHold`. The defect is the extra ROW, not
  the QSO; any fix belongs at log level. **An absent feature here was a
  decision — grep before reporting one as a gap.**
- **BLOCKED, not as a standalone build:** SHIP GATE (c) notification records —
  ADR 0061's subject matter, and that ADR is still `Proposed`.
- **PARKED — do not start without the operator:** `operator_pick` / Call-CQ
  auto_off (see the `ft8-cq-answerer-selection` memory).
- **STANDING:** do not tune the hub buffers (8 ft8 / 64 bridge+events) until the
  eviction records show healthy clients actually being evicted.
- **Watch out:** `~/pCloudDrive/station-manager/` is an ABANDONED data dir
  (July 3). The live one is `~/.local/share/station-manager/`; logs are
  `log/smd.log` there, mode 0600 — and UNROTATED at 17.8 MB back to 16 July.
- **DECIDED, DO NOT REOPEN — the five polar QSO rows are NOT being repaired or
  re-uploaded** (operator, 2026-08-04). QRZ shows the CORRECT position for them
  (its logbook page for the 2026-07-27 R9LAU QSO reads 57.173356 N / 65.559720 E),
  so the bad coordinates did not land there. ClubLog unchecked.
