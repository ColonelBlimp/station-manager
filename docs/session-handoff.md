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
  ~~band filter~~ — **SHIPPED**. Still open on the map, none urgent: the **FT8
  RX propagation overlay** (P2), the **whole-log Dashboard map** (P3, needs a
  `/v1/logbook/{id}/map` aggregate), a **solar-time-zone overlay** (analysed
  2026-08-04, `dogfood-inbox.md` — read it before starting; the licensing and
  solar-vs-political forks are already settled there), and the SSE-revival item
  listed below.
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

---

## Current state (as of 2026-08-04)

> **2026-08-04 (afternoon), LAST — THE COORDINATE ARC. Four commits, six
> reviews, five P1s, every one mine — plus the map band filter. Started as "what
> map items are left?" and became the whole coordinate perimeter.**
>
> - **What was actually wrong.** A whole-logbook scan found FIVE rows across four
>   stations carrying a correct grid beside South Pole coordinates, all dated
>   27 Jul – 1 Aug — i.e. still being produced, because the 2026-07-30 fix was
>   DISPLAY-ONLY (`rowPoint()`) and the cause was untouched. Root traced to
>   R9LAU's QRZ profile: **Grid Square `AA00aa`, Geo Source "From Grid"**, so QRZ
>   derives the polar coordinates itself and the pair AGREES on arrival. The
>   contradiction appears later, when the real grid arrives from the on-air
>   exchange and the field-wise merge replaces the grid alone.
> - **The architecture that came out of it** (operator's framing): decimal
>   degrees is the canonical internal form and every perimeter converts. Provider
>   ingress normalises FORMAT; the storage merge ARBITRATES grid vs coordinates,
>   because that contradiction spans two writes from two sources and no single
>   adapter can see it; ADIF converts both directions; SM Cloud needs nothing,
>   since it mirrors the canonical form. **The grid ARBITRATES rather than
>   supplies** — precise coordinates survive while it vouches for them. `my_lat`
>   followed: the grid now SUGGESTS, a contradiction is REFUSED BY NAME (a human
>   typed it and is present to be told, unlike third-party data), and legacy
>   ADIF-format configs are MIGRATED — without which the new validation would
>   have refused every existing install at startup.
> - **Four predicates now live once in `utils`** (`CoordsValid` /
>   `CoordsReadable` / `CoordsInsideGrid` / `IsPlaceholderGrid`) so the four
>   layers cannot drift on what "contradicts" means. The naming carries the
>   distinction that cost most: boundaries that ADMIT a value ask `CoordsValid`;
>   the merge, which only COMPARES, asks `CoordsReadable`.
> - **THE REVIEW LESSONS — all five P1s mine, worth reading as a set.**
>   (1) I applied the `AA00` sentinel to the MERGED record, so a refresh carrying
>   only a placeholder ERASED a grid and precise coordinates we already held
>   (verified live before fixing). It is a property of ONE INPUT, and I put it on
>   the wrong side of a line I had drawn an hour earlier. (2)/(3) I wrote
>   `ConvertFromXDDDMMM` as the inverse of a function that validates axis and
>   range, and dropped the `isLat` parameter — an inverse weaker than its
>   forward, in the very change where I cited the asymmetric-round-trip lesson.
>   (4) `canonicalCoord` treated any successful `ParseFloat` as a coordinate
>   (`NaN`, `±Inf`, latitude 91). (5) I then bound-checked the PROVIDER ingress
>   and not the CONFIG one **in the same commit** — a boundary promise is only as
>   good as its least-guarded door, and I guarded the one I was looking at.
> - **Also mine, caught by the reversion discipline rather than by review:** two
>   proofs were inconclusive because the revert failed to BUILD (unused import,
>   unused variable) rather than failing a test — the trap already recorded that
>   morning, hit twice more. And R7b initially passed BY CONSTRUCTION, because I
>   wrote its fixture in post-ingress shape so it could not reproduce the defect
>   it was named for.
> - **A rule DROPPED rather than implemented.** An early `my_lat` criterion
>   distinguished a DERIVED position from an operator-SET one when the grid is
>   cleared. That needs provenance the system does not carry, and inventing a
>   marker is the trap CLAUDE.md names — so the rule is uniform (no grid →
>   coordinates stand) and the reasoning sits in the test file.
> - **Operator decisions, not to be re-litigated:** the five polar rows are NOT
>   repaired or re-uploaded — QRZ shows the CORRECT position for them (its
>   logbook page for the 2026-07-27 R9LAU QSO reads 57.173356 N / 65.559720 E),
>   so the bad coordinates never landed there. ClubLog unchecked.
> - **Provider formats: survey abandoned in favour of the boundary.** QRZ (our
>   code), HamQTH and QRZCQ (both API docs) all return DECIMAL; HamCall could not
>   be checked at all (site unreachable). My earlier "formats vary by source"
>   claim was **wrong** — built on reading a rendered QRZCQ page as an API
>   contract. The durable conclusion is the operator's: you cannot know what a
>   provider sends until you implement it, so the boundary must not assume.
> - **Map band filter shipped** (dogfood-inbox 2026-08-01): a select defaulting
>   to All, options from `station.operating_bands` — the STATION's bands, not the
>   window's contents, which would flicker as QSOs age out. Deliberately NOT
>   persisted: the grey-line toggle beside it ADDS an overlay, this REMOVES
>   contacts, so a filter surviving into the next session opens on an empty world
>   with nothing to explain why.
>
> - **A DOC-LOSS INCIDENT WORTH THE ENTRY.** Updating `## Now` earlier, I sliced
>   from a marker to END OF FILE and replaced it with two bullets — deleting
>   `## Current state` (all three arcs) and `## Active cycle`. The file went
>   317 → 109 lines and rode into a commit unnoticed, because I checked the HOOK
>   OUTPUT (which only reads `## Now`, and looked fine) instead of the file.
>   Recovered from `0dfb24de`. **The rule: an edit anchored on `s[index:]` has no
>   right-hand boundary — anchor on both ends, and after editing a structured
>   doc, check its SECTION LIST, not the part you were looking at.** Exactly the
>   least-guarded-door shape as review finding (5) above, in a different medium.

> **2026-08-04 (morning) — TWO SHIPS AND ONE DECISION REAFFIRMED. Both ships were
> caught wrong by review or by the operator before they were right.**
>
> - **Settings navigation guard (ADR 0063).** Confirm-on-leave naming the dirty
>   sections; `beforeunload`; a confirmed discard happens THERE AND THEN, so the
>   app stops holding edits it has reported as gone. **Three exits, not one** —
>   `navigate()`, `popstate` (which bypasses it, and needs the URL pushed BACK
>   because it fires after the address bar moved), and `setMode()` from the
>   always-visible OperateNav. Rigs needed a new `anyDirty`: `dirty` answers only
>   for the SELECTED rig. **Two review findings, both mine.** P1: I exempted Rigs
>   because `#applyFetched` preserves dirty drafts — and `load()` wipes drafts
>   and baselines outright two statements later, so the exemption was built on
>   half a sequence and rig edits were lost silently. P2: the confirm promised a
>   discard while a PUT was already on the wire, which the daemon would then
>   persist. Both fixed; `R3b` is the characterisation test whose absence let the
>   first one ship. MDN settled `beforeunload` — `preventDefault()` **and**
>   `returnValue`, quoted in the code.
> - **Session ends now say WHY** (three commits, three reviews — `3531e1ed`,
>   `ea0c91a5`, `d13fcb22`, the last clean). Dogfooding a 50-QSO run showed seven
>   of eight `session abandoned` records carrying `reason: ""`. Not one missing
>   label: `Abandon()` is reached from TWELVE places and only the two dial paths
>   staged a reason, so a session that DIED read exactly like one the operator
>   stopped. Three families now: operator Abandon and TX disarm are named in the
>   LOG only (`operator` / `tx_disarmed`) — the frame stays silent because the
>   operator caused them and a toast would narrate their own click; the eight
>   terminal-TX sites carry `tx_not_armed` / `tx_bad_message`, which DO reach the
>   frame per invariant 5. Repeat-cap ends log `no_answer`; their frame half is
>   ACCEPTED with the rationale at the code site (the countdown already
>   telegraphs it, so a toast per unanswered call is noise).
>   **The trap found on the way:** the SPA's unknown-code fallback said "the rig
>   frequency could not be verified" — safe only while every code was
>   frequency-related, and a lie the moment a `tx_*` code existed. Now
>   cause-agnostic. `api-endpoints.md` corrected too (it listed two codes and had
>   never gained `band_change`).
>   **THEN TWO ROUNDS OF MY OWN FIXES BEING THE DEFECT.** Round 2: I staged the
>   reason under one lock hold and abandoned under a second, so a teardown landing
>   between them consumed it — and because the dial guard stages into that SAME
>   slot, a rung failure could OVERWRITE its explanation and report a safety stop
>   as a transmit failure. Fixed by passing the cause as an argument through one
>   lock hold. Round 3 (**P1**): the teardown was still unconditional, so a stale
>   rung could end the session that REPLACED its own — invariant 5's named hazard,
>   reached down a new path. **I had declined exactly this in round 2**, saying the
>   generation was hidden inside `transmitLocked`'s closure and gen-scoping was "a
>   larger change". It was five lines: make the function return the generation it
>   had already bound. "Not in scope" was a fact about the signature, which I
>   treated as a fact about the cost. See [[review-findings-fix-dont-defer]].
> - **FT8 dupe guard: asked for, then correctly NOT built.** The 50-QSO audit
>   found KK2A logged twice. `caller_sequencer.go` carries an operator-ratified
>   2026-07-26 note saying **do not** suppress the re-work; the log showed KK2A
>   asked twice and we re-sent twice, so they never copied the RR73 and the
>   second contact is the only one they got. Operator reaffirmed: no way to know
>   whether they logged it, and the dupe costs little. **The defect is the extra
>   ROW, not the QSO.**
> - **The rest of the run was clean:** 50 stored, 50 forwarded to QRZ + ClubLog +
>   smcloud, zero errors, no drive alarms, `meter_po_max` never zero across 246
>   keyings. The power dips were the operator adjusting volume — confirmed by
>   step-shaped transitions (109→95 between consecutive slots, not a ramp), which
>   also validated the drive-alarm instrumentation in the field: a deliberate 13%
>   drop for 25 minutes raised nothing.

> **2026-08-03 — ADR 0062: ENRICHMENT PROVIDERS SELF-REGISTER. Three
> commits, three reviews; the first two each found a real gap and both were
> mine.**
>
> - **THE TRIGGER WAS THE OPERATOR, ONE SENTENCE AFTER THE PORT SHIPPED:**
>   "adding another service becomes a code change rather than a config
>   addition." Counting the sites proved him right — FIVE besides the provider
>   package, four of them hand-written name checks, and TWO of those added that
>   same day by the port. The gap had been *observed* during the port ("there is
>   no `/v1/lookup-types` the way Forwarding has") and used as an argument FOR
>   hardcoding rather than raised as the defect.
> - **THE ADR'S FEASIBILITY CLAIM WAS WRONG AND THE BUILD FOUND IT IN A MINUTE.**
>   It said the registry could live in `internal/lookup` because that package
>   does not import `internal/config`. It does — TRANSITIVELY, via
>   `internal/database/sqlite`. One hop checked, feasibility asserted. The
>   registry therefore splits: **`internal/lookupdef`** (true leaf) holds
>   descriptors that `config` and `api` read; **`internal/lookup`** holds
>   constructors, where the provider interfaces already are.
> - **THREE STRUCTURAL GUARDS FIRED, ALL CORRECTLY.** The ADR 0043 import ratchet
>   rejected `api` → `lookupdef` (added with intent — `api` has always imported
>   `internal/forwarding` for the same reason). `maintidx` tripped because ONE
>   added route pushed `api.New` over; the exemption list is documented as the
>   refactor backlog, so `registerRoutes` was extracted instead of growing it.
>   And a UI test caught two fail-opens being collapsed: the validator must not
>   REQUIRE credentials for an undescribed provider, but the UI must not HIDE
>   its credential fields — same instinct, opposite directions.
> - **REVIEW 1 (P1): SEEDING WAS MISSING**, which defeated the ADR's own goal — a
>   newly registered provider appeared in `/v1/lookup-types` and in no config
>   block, so Settings had no row for it. I had flagged seeding as an optional
>   "accepted cost" and left it out; that was wrong. Verifying the finding also
>   turned up what the review did not mention: `DefaultConfig` still seeded QRZ
>   BY NAME, so the claim "all five hardcoded sites are gone" was false. Fixing
>   the P1 removed it.
> - **MY OWN NEW TEST THEN CAUGHT A BUG IN THAT FIX**: an operator who sets a
>   country URL but omits the `name` — exactly the shape the old canonical-name
>   stamp existed for — had their URL destroyed, because the seed replaced the
>   whole block. Now filled FIELD-WISE.
> - **REVIEW 2 (P2): A SECOND COUNTRY PROVIDER DROPPED SILENTLY**, behind a
>   comment I had written excusing it ("the first by name wins — deterministic").
>   A defect dressed as a design note. Config has ONE country slot by decision
>   (ADR 0017, country data is single-source), so the fix is to refuse the second
>   at registration, not to represent it.
> - **NET:** adding a provider is now a package plus a blank import in `cmd/smd`.
>   The config entry seeds itself disabled, the descriptor endpoint feeds
>   Settings, the SPA changes not at all.
> - **THEN THE FORWARDER `label` REACHED THE LOGBOOK** (3 commits, 2 more review
>   findings, both mine). The first pass did the tooltip and dropdown and stopped:
>   `selectedDestination` was treated as purely a KEY — its job in
>   enqueueUploads — so the three places it is ALSO RENDERED went unlooked-for,
>   and one destination appeared under two identities in a single workflow
>   ("Upload 1 to qrz" after picking "QRZ (club account)"). Fixed with ONE getter
>   so the display sites cannot drift again. The follow-up review then caught a
>   RACE in that fix: the `<select>` is not disabled during an upload, and the
>   label was resolved AFTER the await, so a mid-flight change of destination
>   produced a notice naming somewhere the QSOs were never sent. The pre-existing
>   code already captured `dest` before awaiting for exactly that reason —
>   the second read broke a symmetry that was deliberate. **`name` stays the
>   durable key everywhere it is sent** (`qso_upload`'s UNIQUE constraint,
>   `missing_from`, `POST /v1/forwarder/{name}/uploads`); the label is display
>   only.

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
