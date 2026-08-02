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

## Now (as of 2026-08-02)

<!-- THE ONLY SECTION THE SessionStart HOOK INJECTS. Keep it under ~25 lines.
     It is ORIENTATION, not the record — "where are we, what's next, what must
     I not do". Detail belongs in Current state below, which is NOT injected.
     The 2026-08-02 failure that created this split: the hook printed the whole
     Current-state section (231 KB), the harness truncated it to a 2 KB preview,
     and the RECONCILE warning underneath was never delivered at all. -->

- **DEPLOY IS 9 COMMITS BEHIND.** RPM at `2c6c22f3`, HEAD `b6efaf6c`. NOT
  running: the forwarder `label` field, the **`endpoints` carry-over fix**, the
  whole Forwarding tab, and the **logging-card centring fix**. Consequence while
  behind: a forwarder save from the OLD config SPA still drops custom
  `endpoints` (harmless for this operator — theirs equal the registry defaults),
  and the Phone/CW logging card still sits ~105px left of centre.
  `task deploy:local:dev` when ready. Daemon runs on demand (`smd` is
  deliberately NOT auto-start — a stopped daemon is not a fault).
- **Shipped today:** SHIP GATE (a) config-save records (closed api A4 + A8); the
  **SessionStart hook fix** (it had been emitting 231 KB, truncated to a 2 KB
  preview, so the RECONCILE warning had never once been delivered); the
  **Forwarding tab** ported into app Settings; and the **logging card re-centred**
  — it lost its auto-margin centring when ADR 0058 retired the ADR 0046 tile
  board, leaving a fixed-width card flex-start-aligned in a wider container.
- **Next, unblocked, pick one:** (a) port **Email** or **Enrichment** — both
  reuse the masked-credential + `Clearable` pattern Forwarding just proved;
  (b) **SSE reconnect on `visibilitychange`**, which closes two dogfood reports
  at once; (c) surface the forwarder `label` in the logbook upload-status column;
  (d) a Playwright bounding-box/screenshot check for layout — THREE centring
  defects landed today (Forwarding width, Rigs centring, the logging card) and
  none is catchable in vitest, because jsdom does no layout and the only
  assertable thing is a class name.
- **BLOCKED, do not start as a standalone build:** SHIP GATE (c) notification
  records — it is ADR 0061's subject matter and that ADR is still `Proposed`
  with the "does `notification` join the event table" question unanswered. The
  ADR's own prescription is to ship the alarm pilot first and let it decide.
- **PARKED — do not start without the operator:** `operator_pick` / Call-CQ
  auto_off (see the `ft8-cq-answerer-selection` memory).
- **STANDING:** do not tune the hub buffers (8 ft8 / 64 bridge+events) until the
  eviction records show healthy clients actually being evicted.
- **Watch out:** `~/pCloudDrive/station-manager/` is an ABANDONED data dir
  (July 3). The live one is `~/.local/share/station-manager/`; logs are
  `log/smd.log` there, mode 0600.

---

## Current state (as of 2026-08-02)

> **2026-08-02, LATER — THE ORIENTATION HOOK WAS BROKEN, AND THE FORWARDING TAB
> LANDED. Seven more commits. The operator's question was "why is there confusion
> every session — are we monitoring too many documents?" The answer turned out to
> be mechanical, not editorial.**
>
> - **THE HOOK HAD BEEN DEAD FOR AN UNKNOWN NUMBER OF SESSIONS.**
>   `scripts/session-status.sh` sliced `## Current state` at a prose marker,
>   `/Earlier arc/`, which had been deleted from the handoff at some point.
>   `grep -c` returned **0**, so the awk never exited and printed to EOF: **231
>   KB**. The harness caps injected output, so every session received `Output too
>   large` plus a **2 KB preview** — about 40 lines. **And the RECONCILE staleness
>   warning was printed AFTER that block, so it had never been delivered at all.**
>   The guard built after the 2026-07-05 re-opened-finished-work incident was
>   unreachable for its whole life. Now 1,596 bytes.
> - **THE FIX IS A SPLIT, NOT A TRIM.** Orientation and the record were one
>   section that had to be both short and complete. `## Now` (≤25 lines) is the
>   ONLY injected section; `## Current state` is the rolling record and is NOT
>   injected. Live doc 3,005 → ~400 lines; 2,612 lines rolled to the archive with
>   line-by-line accounting that nothing was lost. **Rule: a section that grows
>   without limit must never be the injected one.**
> - **FOUR REVIEW ROUNDS ON THAT SCRIPT, THREE FINDING A DEFECT IN THE PREVIOUS
>   ROUND'S FIX.** Worth reading before touching it again: (1) the cap counted
>   CHARACTERS (`${#s}`), so multibyte content overflowed it — 6,000 "bytes"
>   emitted 24,843; (2) my fix for that dropped the last LINE to avoid slicing a
>   glyph, which DELETED EVERYTHING when the section was one long line — silence,
>   the original failure; (3) the RECONCILE warning was itself unbounded (12
>   commit subjects; this repo writes 250–300 char ones, 2,666 bytes for twelve),
>   so it could floor the body and still bust the cap; (4) the two truncation
>   sites had different iconv fallbacks. Round 5 clean. **Every fix added a code
>   path whose failure mode nobody had enumerated; the one that finally held
>   REMOVED a path (one `utf8_trim` helper for both sites).**
> - **`47e5225e` — forwarder `label`, and a data-loss bug found by writing its
>   rule first.** `label` is operator-set in config.json ONLY (no API write, no
>   Settings control) because the built-in display name is a string in the binary
>   — "SM Cloud backup" is already dated and renaming it is a release. It is
>   deliberately NOT `name`: `qso_upload` keys `UNIQUE (qso_id, forwarder_name,
>   action)` on that, so renaming it would make the daemon forget which QSOs were
>   already sent and **re-upload them to ClubLog and QRZ**. Asking "what else does
>   `mergeForwarders` drop?" found that **`Endpoints` was never carried over** —
>   a save wrote it empty and `applyDefaults` re-seeded the registry default at
>   the next Load, silently reverting an operator's override. Both now carried;
>   L2/L3 pin them. **Any future config-only field on `ForwarderConfig` must join
>   that carry-over** — recorded in `docs/v2-design/config.md`.
> - **THE FORWARDING TAB (app Settings, ADR 0044).** Three blank states exist and
>   only one may reach the wire: never-touched and typed-then-erased are omitted
>   (daemon keeps the stored value), and ONLY an explicit reset sends `""`.
>   `Clearable` is far narrower than the backlog implied — exactly **two** fields
>   system-wide (`smcloud.logbook`, dev-only `stub.mode`) and **no password is
>   clearable anywhere**, because emptying a required credential is a daemon that
>   won't restart. Destinations collapse into `<details>` disclosures; an edited
>   card is starred and **refuses to collapse**, so a pending change cannot hide.
> - **THREE MORE DEFECTS THE RULES CAUGHT, none in the feature being built:**
>   `reset()` (the Cancel button) restored drafts from the dirty-compare
>   projection, which carries no `type` — so Cancel silently made every
>   destination "unsupported"; the Rigs pill read **active** while branching on
>   `default_rig_id`, claiming "you are on air with this rig" exactly when a
>   pending restart made it false (relabelled **default**; the rig LIST already
>   said "default", so the component contradicted itself); and `display: flex` on
>   a `<summary>` suppresses the native disclosure triangle.
> - **CORRECTED MID-SESSION, twice, both mine:** `smd.log` is **0600**, not 0644
>   (I cited 0644 all session as the redaction rationale — the policy stands, the
>   argument overstated it); and I "corrected" A4's startup-rewrite claim on the
>   strength of `config.Load` alone before finding `main.go:237` rewrites on every
>   start. **Grep the whole path before contradicting a written finding.**
>
> **2026-08-02 — SHIP GATE (a) SHIPPED, both write sites, across four commits.
> "When did this setting change, and to what?" now has an answer. Three of four
> clean-room review rounds found a real defect and TWO of those were in the
> previous round's fix; the fourth came back clean, which is what settled it.**
>
> - **DEPLOY STATE — AT HEAD.** Installed RPM is
>   `2.0.0~alpha.1.1030.g2c6c22f3` = HEAD (`2c6c22f3`). The daemon is currently
>   **inactive**, which is normal — `smd` is deliberately not auto-start, so a
>   stopped daemon is not a fault. **This supersedes the "FOUR COMMITS BEHIND"
>   line in the 2026-08-01 block below, which was already stale before this
>   session began** (the operator deployed at 17:01:38 on 08-01).
> - **`e8b36905` — go-ft8 v0.7.0 → v0.7.1.** Its own commit, per the dependency
>   rule. The substantive change is a **hash-table transactional snapshot**: the
>   concurrent candidate path (which SM runs) previously mutated the live table
>   during unpacking, out of candidate order; workers now use an isolated
>   snapshot and commit saves in order. That table resolves compound/nonstandard
>   callsigns. The commit message's "stricter option validation" oversells an
>   overflow-safe rewrite of a `blocks` guard SM never passes. Also: `pfft` now
>   PANICS on use-after-Close (was a nil plan into C) — in the live dogfood path,
>   but `ft8.decoder` runs under `safego.GoTracked`, so a decode-slot panic is
>   recovered. Tested on BOTH backends deliberately: the CGO-free default suite
>   does not exercise pocketfft, which is what the deploy actually uses.
> - **`7b21b2b1` — the record itself, closing api A4 AND A8 in one edit.** One
>   Info `config saved` per committed change, carrying a field-level delta,
>   `source` (`api` | `startup`) and `setup_completed`. Emitted **before**
>   `buildConfigResponse`, which is what closes A8 — a change that commits and
>   then 500s still leaves proof it applied. Criterion + the five operator
>   rulings live in the header of `internal/api/config_save_log_test.go`
>   (CS1–CS10); startup half in `cmd/smd/config_save_startup_test.go` (B1–B3);
>   the diff engine is `internal/config/diff.go`.
> - **OPERATOR RULINGS, 2026-08-02 (asked before implementing, not inferred).**
>   (1) Non-secret fields log their VALUE; secrets log only THAT they changed,
>   mirroring the API's `credentials_set`/`password_set` masking. Email fields
>   (`smtp.username`/`from`/`default_recipient`) count as non-secret; lookup URLs
>   log scheme+host only. Classification is an **allowlist**. (2) Compute the
>   delta, before→after. (3) Info. (4) No-op saves log nothing — falls out of the
>   delta. (5) The startup rewrite is in scope. **#2, #4 and #5 turned out to be
>   ONE decision** — all three reduce to "does the handler compute a delta?".
> - **A CORRECTION OF A CORRECTION, worth not repeating.** Mid-session I
>   "corrected" A4's claim that the daemon rewrites `config.json` at startup,
>   having checked only `config.Load` (which indeed does not write). **A4 was
>   right and I was wrong:** `cmd/smd/main.go:237` calls `Update` on every start
>   and `config.Service.Update` (`config.go:1746`) writes **unconditionally, with
>   no delta check**. So mtime moves every boot — which is exactly why `source`
>   is on the record. Re-verified and written into A4's banner with the citation.
> - **`479245e9` — review round 2, both findings REAL.** (P1) forwarder
>   `Endpoints` is `map[string]string` keyed by ACTION, so its URLs sit at leaves
>   called `insert`/`delete` and sailed past a `urlLeaves` check that asked the
>   question of the FIELD NAME — a denylist in the one place the comment claimed
>   an allowlist. A token in an endpoint would have gone from a 0600 file into a
>   0644 one. Fixed by `originIfURL`, which reduces any URL-**shaped** value
>   wherever it appears. (P2) `keyList` indexed by identity and DISCARDED ORDER,
>   but `lookup.chain` is priority-ordered (`orchestrator.go:576`, first non-empty
>   wins) — so a provider swap committed to disk and diffed to **nothing**. Now
>   reported against the container, but only when membership is unchanged (D2b
>   pins that; P11 proves the guard is load-bearing).
> - **`2c6c22f3` — review round 3, and the fix was 8× the report.** The reviewer
>   named three bare prefixes (`forwarders`, `rigs`, `operators`) that made
>   `HasPrefix` match sibling top-level fields like `forwarders_api_token`. I had
>   flagged that exact change for a second pair of eyes when I made it. Rather
>   than patch the three, the constraint went on the LIST — every prefix entry
>   must end in `.` or `[` — and that guard immediately found **23** unbounded
>   entries (`version` matching `versionsecret`, `smtp.username` matching
>   `smtp.username_token`). Split into `valueAllowlistExact` (whole-path, all
>   scalars + the three container paths so a reorder renders its order) and
>   `valueAllowlistPrefix` (subtree, all delimiter-bound). D4 states the rule over
>   `valuePolicy` **paths rather than a Config**, because the fields it guards
>   against do not exist yet — that is the point.
> - **13 REVERSION PROOFS**, each red on its OWN rule's assertion, harness at
>   `scratchpad/prove.sh`. Guards that earned their keep this session: the
>   match-count check aborted 5 of 7 on the first run (`grep -cF` counts LINES,
>   so every multi-line pattern miscounted), and the compile check aborted 2 more
>   where the revert orphaned a variable. **A proof that does not apply certifies
>   the implementation it was meant to challenge.**
> - **THREE FIXTURES WERE WRONG BEFORE THEY WERE RIGHT**, all caught by asserting
>   preconditions. Sharpest: CS4 (commit logged despite a 500) originally CHANGED
>   the callsign, which trips the callsign-lock guard at `handler_config.go:621`
>   — that guard reads the DB and 500s **before** the commit. Same status code,
>   no commit; the rule would have passed against a daemon that logged nothing.
>
> **2026-08-01, LATER THE SAME DAY — the audit findings started shipping. SIX
> findings across six commits: the auto-work pill fix, bridge B1 (drive-watch state
> reporting), api A7, and the three-hub eviction class fix. Deployed mid-session and
> validated on the air on 12 m; everything after that deploy is NOT running.**
>
> - **DEPLOY STATE — FOUR COMMITS BEHIND.** Running
>   `2.0.0-alpha.1-1021-gda962028` since **14:47:36**; version stamp verified
>   against HEAD at the time (not `dev` — the `-X` trap did not bite). HEAD is now
>   `cb21cf8b`. **NOT running: api A7 and all three eviction logs.** Two
>   consequences: a datastore fault still reports as unset configuration, and the
>   eviction records — the evidence the operator wants before deciding anything
>   about buffer sizes — do not start accruing until a deploy.
> - **`b93bd417` — starting a Call-CQ run stops an armed auto-work run.** From a
>   dogfood report ("the auto-work armed stays active, or the pill stays viewable").
>   It was BOTH and the pill was innocent: `StartCallCq` reset caller / stalledCalls
>   / confirmHold / contact and never touched `autoWork`. It could not FIRE
>   (`onSlotIdleArmed` needs `mode == seqIdle`, and a Call-CQ contact resumes CQ
>   rather than ending), so this was never a rogue-transmission risk — it removed an
>   indicator naming the wrong mechanism plus a stale offset/dial pin. Cleared ahead
>   of the status publish; W12 pins the state, **V5 pins that the CQ's own frame
>   carries it** and is demonstrably load-bearing (move the clear after the publish:
>   W12 green, V5 red).
> - **`1273752d` + `da962028` — bridge B1, and it went WIDER than the finding.**
>   Drive-watch is now a four-state transition machine (`armed` / `no_meter` /
>   `meter_not_po` / `meter_moved_off_po`), **Warn on entering a dark state, Info on
>   recovery, NO line when unchanged** — the operator's design, and the level was his
>   call against my hesitation. The measurement backed him: the declined branches are
>   ~3% of transmissions here, not the sticky whole-session state I predicted. Also
>   added on his direction: a per-transmission `drive_watch` field on the existing
>   meters record, and the mid-TX taint branch (a third silent early return no
>   arm-time transition could see). **The Error alarm now carries its own evidence** —
>   `meter_sel`, `meter_n`, `meter_po_max`, `gap_ms`, `gap_max_ms`, `tx_gen` — all of
>   which were already computed at the emit point and discarded.
>   **B1 had the priority wrong and I stated it wrongly too:** I ranked it first
>   because it had a measured cost that morning, but the 08:45 alarms took the ARMED
>   path, so B1's own fix would not have explained them. The alarm-evidence half is
>   what addresses them.
> - **`3fdba1a2` — api A7, and the operator OVERRULED the finding's own
>   prescription, correctly.** "Log only, do not change the fail-closed behaviour"
>   would have logged the cause while leaving the operator reading *"set your station
>   callsign in My Station"* with a broken database. His ruling: **fail closed means
>   never fall back to another callsign and never transmit; it does not require
>   preserving the misleading 400.** So: unexpected DB error → **503 `db_unavailable`**
>   (the code `handler_health.go` already uses) + Error log with cause, `logbook_id`,
>   `op`; genuinely unset config → 400 `no_station_callsign`, unchanged. Neither
>   starts a session or keys PTT, and **neither disarms** — these routes require TX
>   already armed. The SPA needed no change (`ft8qso.ts` already maps ≥500 to a
>   `server` outcome carrying the daemon's message).
> - **ON-AIR VALIDATION, 12 m, 16 transmissions:** all `drive_watch=armed`, **zero
>   transition lines, zero alarms**, `gap_max_ms` 245–269 ms against a 3000 ms
>   threshold, `po_max` 109, `po_n` 559–615. That is the no-spray design confirmed in
>   production rather than predicted — a per-transmission scheme would have emitted 16
>   warns for those slots.
> - **THE DAY'S REAL LESSON — FIVE WAYS A RULE CAN BE GREEN AND WORTHLESS.** The
>   drive-watch work took five review rounds and four of the findings were FIXTURE
>   failures, not logic errors. All five shapes are written into
>   `internal/bridge/drivewatch_test.go`'s header with their proofs: (1) the fixture
>   excluded the interval; (2) presence asserted instead of value; (3) an identifier
>   that identified nothing (and the obvious fix carried the WRONG generation, because
>   `finishFt8Tx` increments before flushing); (4) **one fix, two sites, one proof** —
>   when a fix touches N call sites the proof must revert each separately or N-1 are
>   unguarded; (5) **every rule was single-threaded and the subject is not** — twelve
>   green rules could not see an ordering defect that left "drive detection restored"
>   as the last line while detection was dark. Read that header before writing rules
>   for anything concurrent.
> - **THREE DRIVE ALARMS TODAY (08:45:03, 08:45:33, 10:08:35) CANNOT BE RE-EXAMINED.**
>   They were logged by the pre-deploy build and carry `code` alone. Anything firing
>   after `da962028` carries full evidence and joins to its meters record by `tx_gen`.
>   The 10:08:35 one was never looked at.
> - **`57619abe` + `cb21cf8b` — THE THREE-HUB EVICTION CLASS FIX** (ft8 #1 +
>   bridge B2, plus a THIRD site the audits missed: `internal/events/hub.go`, which
>   feeds the map's stream). All three dropped a too-slow subscriber with a bare
>   `close(ch); delete(...)` and said nothing. Each now emits ONE Warn per evicted
>   subscriber with `subscriber_id`, `event`, `queue_depth`, `queue_capacity`,
>   `subs_before`, `subs_after`.
>   **THE TEARDOWN IS UNCHANGED — operator's ruling, and the reasoning is the part
>   to preserve:** the enforced proxy is a FUNCTIONING SSE subscription, not an open
>   browser tab; once the channel overflows, operator-facing state is no longer
>   flowing, and EventSource reconnect plus the existing linger already IS the
>   recovery distinction. Exempting eviction could leave TX running behind a dead
>   display or create a phantom subscriber that can never unsubscribe. **Buffers stay
>   at 8 (ft8) / 64 (bridge, events) until the new records show HEALTHY clients being
>   evicted.**
>   **A codex P2 was REFUTED, not fixed** (rationale in all three `logEvictions`
>   comments): the eviction log is synchronous and zerolog writes straight to
>   lumberjack, so the mechanism is real — but these goroutines already emit **1,502
>   `ft8 tx meters` records against 0 evictions** in 15 days, so it is the rarest
>   instance of an accepted pattern, not a new class. If a stalled log destination
>   endangers them it belongs at the logging layer, fixed once.
> - **FIVE REVIEW ROUNDS ON THAT ONE, AND EVERY FINDING WAS A PROOF GAP, NOT A BUG.**
>   The implementation was right early; the tests were not. In order: F3 called the
>   handler's unsubscribe BY HAND, so deleting `defer unsub()` would not have failed
>   it (now drives the real `HTTPHandler` with a client that stalls mid-write) · F4
>   could not reach the raced-timer interleaving at all, so the subCount guard was
>   unproven (F5 added, entering `onLingerExpired` directly) · `subscriber_id` was
>   checked only for non-negativity, which a constant 0 passed (fixtures now burn id
>   0) · the production wiring guard was `strings.Contains`, which a commented-out
>   call, `logging.Noop()`, `(*logging.Service)(nil)` and a local rebinding all
>   passed (now an AST check requiring `loggerSvc` resolved from the container) · F3's
>   barrier counted writes cumulatively, and the hub replays cached events on
>   subscribe so it was pre-satisfied (now a signal raised by `Write` at the gate).
>   **The generalisable one: a green test after touching ONE of two redundant
>   mechanisms is not evidence the other is dead code.**
> - **NEXT:** SHIP GATE (a) config saves and (c) notification records — the last two
>   open gate items. **~55 of 66 audit findings remain.** Also queued from the
>   inbox triage: the SSE-reconnect-on-visible fix, which two separate dogfood
>   reports (07-18 map, 07-28 daemon-unreachable) turn out to share.
> - **PARKED, operator-flagged for "shortly":** a Call-CQ run has **no auto_off** —
>   the answer-mode enum is three values and `operator_pick`, the only manual one, is
>   rejected at runtime (`servicetx.go:855`) while config validation still ACCEPTS it,
>   so saving it disables Call CQ until the error is noticed. The "off" the operator
>   wants is the unbuilt pile-up stack. Memory: `ft8-cq-answerer-selection`.
>
> ---
>
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
