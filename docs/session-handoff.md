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

## Now (as of 2026-08-03)

<!-- THE ONLY SECTION THE SessionStart HOOK INJECTS. Keep it under ~25 lines.
     It is ORIENTATION, not the record — "where are we, what's next, what must
     I not do". Detail belongs in Current state below, which is NOT injected.
     The 2026-08-02 failure that created this split: the hook printed the whole
     Current-state section (231 KB), the harness truncated it to a 2 KB preview,
     and the RECONCILE warning underneath was never delivered at all. -->

- **DEPLOYED AT HEAD** (`bba15ede`, daemon started 10:22, smcloud reconcile
  `in_sync` 6919/6919). Everything below is live. `smd` is deliberately NOT
  auto-start — a stopped daemon is not a fault.
- **Shipped today:** **Settings → Email**, plus the two daemon gaps the port
  exposed (a stored SMTP password could never be REMOVED; a blank port/timeout
  stored 0 and became 587/30 only at the next restart). Also a **stale-reload
  class fix** across all four Settings sections, and the **CAT chip** is now a
  readout outside Operate. Detail in Current state.
- **EYEBALL WHEN CONVENIENT:** Email's password keep / type / remove states and
  the 587/30 placeholders — live but never seen by hand; vitest checks the DOM,
  not the look.
- **OBSERVE WHEN THE CHANCE ARISES — NOT tasks, and they gate nothing.** Do not
  schedule these and do not open a session asking whether they are done; confirm
  opportunistically. SSE revival (background the MAP tab — same window, browsers
  throttle on visibility not focus; **a negative result means the TRIGGER is
  wrong, not `openReviving`** — see Current state) · FT8 dead-source watchdog ·
  ADR 0059 auto-work · stuck-TX / RF-ingress 2 s tune trials INTO THE ANTENNA
  (operator's call on RF exposure).
- **NEXT ACTUAL WORK (the list to pick from):** (a) port **Enrichment** — same
  masked-credential pattern, but its wire shape is a provider CHAIN while the
  config SPA renders a QRZ-specific section, so porting re-opens that choice;
  (b) a **Settings navigation guard** — sidebar links still discard unsaved
  edits silently (knowingly open; reasoning in `Header.svelte.test.ts`);
  (c) forwarder **`label` in the logbook upload-status column** (verified open
  2026-08-03); (d) **Playwright** layout check — NOT scaffolded at all, so it is
  config + CI + a first spec, not just a test.
- **BLOCKED, not as a standalone build:** SHIP GATE (c) notification records —
  ADR 0061's subject matter, and that ADR is still `Proposed`.
- **PARKED — do not start without the operator:** `operator_pick` / Call-CQ
  auto_off (see the `ft8-cq-answerer-selection` memory).
- **STANDING:** do not tune the hub buffers (8 ft8 / 64 bridge+events) until the
  eviction records show healthy clients actually being evicted.
- **Watch out:** `~/pCloudDrive/station-manager/` is an ABANDONED data dir
  (July 3). The live one is `~/.local/share/station-manager/`; logs are
  `log/smd.log` there, mode 0600.

---

## Current state (as of 2026-08-03)

> **2026-08-03, LAST — SETTINGS → EMAIL SHIPPED, and the port exposed two daemon
> defects that had nothing to do with the SPA. Deployed at HEAD (`bba15ede`).**
>
> - **PORTING IS A DEFECT DETECTOR.** Neither gap was in the Email tab; both were
>   in the daemon, and both had been shipped for months. (1) **A stored SMTP
>   password could never be REMOVED.** `mergeSmtp` keeps the stored value on
>   blank and SMTP has no `Clearable` concept, so once set it could only be
>   replaced — short of stopping the daemon and hand-editing config.json.
>   Unauthenticated relays are a legitimate setup, so this was a real dead end.
>   Fixed with an explicit `password_clear` command, NOT by overloading blank:
>   blank has to go on meaning KEEP, because it is what an operator editing the
>   host sends on every single save. Operator's ruling on the both-fields case:
>   **clear wins** (fail-safe for secret removal, sensible against stale form
>   state) — recorded at the code site and in the test that guards it, so a later
>   "surely that should be a 400" rewrite has to argue with a test.
>   (2) **A blank port/timeout stored 0** and silently became 587/30 at the NEXT
>   restart, because `applyDefaults` runs only on Load while the PUT path runs
>   `Normalize`. On an ENABLED block it never got that far — `validateSmtp`
>   returned a 400 telling the operator to type a number the daemon already knew.
>   Fixed by `normalizeSmtpDefaults` in `Normalize`, following the precedent two
>   lines away (`normalizeLookupURLs`, moved for exactly this reason).
> - **THE RED STEP FOR A GREENFIELD SPA MODULE IS THE CARELESS PORT.** There is
>   nothing to revert when the file does not exist yet, so the config SPA's
>   `saveEmail` was copied across verbatim first and the tests run against it.
>   Four wire rules and three UI rules failed — including that it sends
>   `logging_station` and `station` (the clobber review 2026-07-20 #3 removed
>   from Station), and that it has no Remove control and no default placeholders.
>   The other eight passed against the naive port and are guards, not
>   discoveries. **Say which is which** rather than reporting "all red".
> - **ONE CRITERION WAS NOT OPERATOR-OBSERVABLE AND WAS LABELLED, NOT FAKED.**
>   "A typed password replaced the old one" cannot be seen in a browser —
>   `password_set` reads true either way, and the only human proof is a
>   successful send. Its proof is a wire assertion, and the test header says so.
> - **A P1 CAME BACK ON THE FIX, TWICE, AND BOTH ROUNDS WERE RIGHT.** Round 1
>   (`dcb0316e`): a failed RELOAD left `loaded` true, so the section rendered the
>   previous session's values with no error — and since every Settings PUT
>   replaces its block WHOLE, editing one field rewrites the rest at stale
>   values. Reachable because `App.svelte:100` mounts Settings behind a router
>   branch while the state modules are singletons. **It was in all four sections,
>   not just Email** — the commit replicated a pre-existing pattern. Round 2
>   (`2c64c7aa` → `bba15ede`): clearing `loaded` only in the error branch still
>   leaves the retained draft live for the whole PENDING reload; it must be
>   invalidated BEFORE the await. That round also pushed the `!loaded` save
>   precondition to all four (it had been Email-only) and to `rigs.setDefault`,
>   which is the same class of write and had been missed.
> - **CAT CHIP IS A READOUT OUTSIDE OPERATE** (operator's call). It used to
>   navigate to Operate and reveal the rig panel — deliberate, and pinned by its
>   own test, which had to be DELETED. That is the spec changing, not the code
>   being wrong, so the reversal's reasoning went into the test file rather than
>   letting the old rule look like it never existed. Rendered as a `<div>`, not a
>   disabled button: an inert control you can still click and hover is
>   indistinguishable from a broken one. **NARROW BY INSTRUCTION** — every
>   sidebar link still leaves dirty Settings and discards the edits on RETURN
>   (the reload's `#apply` overwrites the draft), so there is no moment at which
>   the operator could be warned. Recorded as knowingly open.
> - **`api-endpoints.md` was missing the whole `lookup` block** on both GET and
>   PUT — found while documenting `smtp`. Added, along with `password_clear` and
>   the default resolution.

> **2026-08-02 — SSE REVIVAL SHIPPED, and its scope was CUT IN HALF by
> reading `smd.log` instead of reasoning. Deployed at HEAD; its on-desktop
> confirmation is an opportunistic OBSERVATION, not a scheduled task.**
>
> - **THE SUSPICION WAS WRONG, and checking cost one query.** The 2026-07-28
>   report reads as though a dead SSE might have ended an FT8 run: subscriber
>   count → 0, `captureLinger` (5 s) → `onLingerExpired` → **disarm TX + abandon
>   the QSO**. The log that day shows `06:16:47 session abandoned / 06:17:08
>   session abandoned / 06:17:55 ft8 tx: disarmed`, which looks exactly like it.
>   **It was four operator CLICKS** — every one carries an HTTP request
>   (`POST qso/abandon`, `cq/start`, `qso/abandon`, `tx/arm`; an arm re-runs the
>   disarm path, hence that log line). Timing disconfirms it independently: last
>   SSE disconnect 06:16:55 + 5 s = ~06:17:00, a minute earlier. **And both
>   "disconnects" were RELOADS** — `/app/operate/ft8` + `index.js` + `index.css`
>   + `logo.svg` in the same second, SSE reopening immediately. **A dropped
>   stream has never silently ended a run.**
> - **THE OTHER HALF WAS ALREADY FIXED.** Idle inhibition (`65dbcee5`, landed the
>   same day as the report) holds a logind `idle:sleep` block +
>   `org.freedesktop.ScreenSaver` while FT8 TX is ARMED — proven by the
>   operator's own KDE A/B, 44 min untouched vs a lock within 5 min once
>   disarmed. So "screen blanks mid-run" is closed. It does NOT cover
>   TX-unarmed monitoring, Phone/CW, Logbook or the map, and it does not stop a
>   TAB being backgrounded, which browsers throttle regardless of screen state.
> - **NET: this is a STALE-MAP fix, not a TX-safety fix.** The live target is the
>   2026-07-18 background-tab report. The inbox entry carries the full
>   correction so triage cannot re-inflate it.
> - **`openReviving` — one wrapper, all three clients.** Revives ONLY when dead:
>   `OPEN` → leave; `CLOSED` → revive; `CONNECTING` with no error → a first
>   connect, leave; `CONNECTING` after an error → stuck retrying, revive. **No
>   thresholds** — the error-since-last-open flag is what separates the last two,
>   and a `CLOSED`-only check would miss exactly the silent failure. **Never
>   "always recreate"**: tearing down a healthy `/v1/ft8/events` starts the
>   capture linger, and a failed reopen disarms TX mid-run. R1 proves it — that
>   one-line simplification turns FIVE rules red.
> - **A second redundant-guard pair, same shape as the Forwarding one.** Teardown
>   is protected by removing the listener AND by `src === null` in `isDead()`;
>   removing either alone left V6 green, only both turn it red. They are NOT
>   equivalent — the listener removal is the sole defence against a leak onto
>   `document` per route change, which no behaviour test can observe. Written up
>   in the code rather than papered over with a mechanism assertion.
> - **STILL OPEN, and it is the whole point:** jsdom cannot suspend a tab. The
>   rules pin POLICY. Whether `visibilitychange` fires on this desktop is
>   unverified — see `## Now`.
>
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
