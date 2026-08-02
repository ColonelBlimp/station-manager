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

- **Deployed:** RPM at HEAD; daemon runs on demand (`smd` is deliberately NOT
  auto-start — a stopped daemon is not a fault).
- **Just shipped:** SHIP GATE (a) — config saves now leave a durable record
  (field-level delta, `source` api/startup, secrets masked). Closed api A4 + A8.
  Four commits, four review rounds, the last clean.
- **Next (proposed, awaiting operator):** port the **Forwarding** tab from the
  standalone config SPA into the app shell. Smallest of the five remaining tabs
  AND the one carrying the unsolved masked-credential + `Clearable` reset
  problem that Email and Enrichment will both reuse.
- **Also open, unblocked:** SSE reconnect on `visibilitychange` (closes two
  dogfood reports at once); SHIP GATE (c) notification records — BLOCKED on ADR
  0061's open questions, do not start it as a standalone build.
- **PARKED — do not start without the operator:** `operator_pick` / Call-CQ
  auto_off (see the `ft8-cq-answerer-selection` memory).
- **STANDING:** do not tune the hub buffers (8 ft8 / 64 bridge+events) until the
  eviction records show healthy clients actually being evicted.
- **Watch out:** `~/pCloudDrive/station-manager/` is an ABANDONED data dir
  (July 3). The live one is `~/.local/share/station-manager/`; logs are
  `log/smd.log` there, mode 0600.

---

## Current state (as of 2026-08-02)

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
> **2026-08-01 (earlier) — FIVE package logging audits (66 findings, one file each in
> `docs/reviews/*-logging-gaps.md`), then SIX findings shipped across four atomic
> diffs, plus SHIP GATE item (d). NOT DEPLOYED — the running build predates all
> of it.**
>
> - **SHIPPED TODAY, in order:** api A1 + A9 + forwarding F4 (`b1c50913`/`0265f04a`)
>   · forwarding F6 and F1's volume half — the `forwarding: attempt` restructure
>   (`f31738bc`, regression fixed by `191ac370`) · F1's provenance half —
>   `qso_upload.origin` + migration 0007 (`59542a8a`/`ed0f0657`) · **SHIP GATE (d)**
>   — version stamping on every record (`116cf34b`) · the ft8 structural-guard fix
>   (`336329a6`). **Backlog: forwarding 14 of 17 remain; api 10 of 12.**
> - **MEASURED EFFECT once deployed and rotated:** `internal/forwarding` drops from
>   41% of `smd.log` to ~23%; the whole log 14.31 → 15.18 MiB *including* full
>   version stamping — **+6%**, against **+23%** had (d) shipped without the F1
>   restructure. Both decided together for exactly that reason. Figures on a
>   corrected 15.51-day divisor (an earlier pass wrongly divided by 17 distinct
>   calendar days).
> - **SHIP GATE now: (a) config saves and (c) notification records still open;
>   (b) STRUCK as false; (d) SHIPPED.**
> - **TRAP, now documented in seven places:** `-X` on a symbol that no longer
>   exists **exits 0 and stamps nothing**, so any stale `-X main.Version=` silently
>   produces a `dev` build. `internal/buildinfo.Version` is the single carrier;
>   `cmd/smd`'s `main.Version` was REMOVED, not aliased. `cmd/smcloud` keeps its
>   own and is out of scope.
> - **A GUARD CLASS THAT READ AS COVERAGE.** Four tests inspected their own source
>   via a relative path, so outside the package directory they parsed nothing and
>   PASSED. Two were pre-existing and load-bearing: `internal/ft8`'s
>   publish-atomicity and session-end AST checks, which exist *because* 23 of 39
>   publish sites have no test. Proven by injecting real invariant violations —
>   they failed in-package and passed from `/tmp`. All four now use `//go:embed`,
>   and each fails if it parses nothing. **If you write a test that reads source,
>   embed it; a relative path is a silent no-op waiting for a different runner.**
>
> - **THE AUDITS.** `internal/ft8` (14) · `internal/bridge` (14) · `internal/api` (12) ·
>   `internal/qsoservice` (10) · `internal/forwarding` (16). Each file has a
>   **"verified NOT gaps"** section — in three of the five it is longer than the
>   findings and is the part that stops a later pass re-filing settled questions or
>   applying a dangerous fix. **Read it before acting on any finding.**
> - **SHIPPED (`b1c50913` tests + `0265f04a` implementation): api A1, api A9,
>   forwarding F4** — three independent routes by which "why did the daemon stop?"
>   bypassed `smd.log`. Marked ✅ in their files with shipped detail; backlog index
>   lines updated so they are not re-picked.
> - **NEW TEST SEAM: `logging.NewForWriter(io.Writer) *Service`.** Before it, no
>   package outside `internal/logging` could assert on emitted records. It unblocks the
>   remaining 63 findings, not just these three. Its own guarantees are pinned in
>   `writer_service_test.go` — note the precise one: a later `Initialize()` still
>   RETURNS the nil-ConfigService error; what cannot happen is the capture logger being
>   replaced.
> - **PROCESS FAULT, NOW THREE TIMES IN ONE DAY: RED tests committed without their
>   implementation.** `b1c50913` (closed by `0265f04a`), the `f31738bc` pair, and
>   `59542a8a` (followed by `ed0f0657`). CI gates every push, so each left main red
>   on a revision that was never a release candidate and put a broken commit in
>   `git bisect`'s path. Codex filed a correct P1 on each.
>   **ACCEPTED HISTORICAL DAMAGE, NOT "CLOSED."** All three are on `origin/main`.
>   A later commit makes the TREE correct; it does not repair the history, and
>   rewriting shared main would be worse than the damage. So `git bisect` across
>   this day's range will land on revisions that do not build or that serve a wrong
>   API shape — expect it, do not re-litigate it.
>   **The third is materially worse and is the one to remember.** `59542a8a` carried
>   `types.QsoUpload.Origin` — `json:"origin"` with no `omitempty` — without its
>   migration or producers, so that revision **changed the public API to emit
>   `"origin": ""`** on every item of `GET /v1/qso/{uuid}/uploads`. Not a failing
>   gate: a wire contract shipped with a wrong value. The operator had explicitly
>   said the field "must remain part of the same atomic Diff B and never ship
>   alone", and that warning was written verbatim into the field's own doc comment.
>   **Ship tests + implementation as ONE commit — and STAGE it as one, rather than
>   describing a diff as atomic and leaving the split to the commit step.**
> - **A REVERSION PROOF LIED, AND WAS CAUGHT.** The first A9 revert deleted the
>   `ErrorLog` field, which broke the build — the test never ran, so the proof was
>   worthless. It surfaced only because the run produced NO matching output and the
>   failure text was read rather than the silence accepted. The valid proof reverts to
>   `stdlog.New(os.Stderr, …)`, the actual pre-fix behaviour.
> - **TWO STALE-DOC CORRECTIONS, both found by grepping code not docs.** SHIP GATE item
>   (b) ("QSO deletes write no log line") is **FALSE** — `delete.go:85` has logged since
>   `d516d816`, 2026-05-17, *before the entry was written*; struck in the backlog. And
>   `registry.go` documented a startup log that did not exist; F4 made the comment true.
> - ~~**NEXT — A7, then B1, then the hub evictions.**~~ **A7 and B1 both SHIPPED
>   later the same day — see the block above.** The ordering advice itself was
>   partly wrong: B1 was ranked ahead of the hub evictions because it "had a
>   measured cost this morning", but the 08:45 alarms took the armed path and B1's
>   fix would not have explained them. Remaining next step is unchanged: the two
>   silent hub evictions.
> - **This morning's two `drive_no_output` alarms (08:45:03, 08:45:33) are almost
>   certainly FALSE** — 6–7 meter samples against ~490 on healthy transmissions, a
>   12.88 s gap in a 13.36 s key, and `meter_po_max` **120 vs 98**, i.e. the rig
>   reported MORE output on the few samples it sent. Operator disarmed and re-armed
>   at 08:44:24/08:44:31, seven seconds before the first. **Cause never established,
>   and now unestablishable** — they predate the deploy, so they carry `code` alone.
>   A third alarm at 10:08:35 was never examined. The post-deploy 12 m session gives
>   the healthy baseline to judge any future one against: `gap_max_ms` 245–269 ms,
>   `po_max` 109, `po_n` 559–615.
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
