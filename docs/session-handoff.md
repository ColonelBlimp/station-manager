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

- **DEPLOYED AT HEAD** (`ce989c1d`), daemon active. Everything below is live,
  **including ADR 0062 — and it is verified on the REAL config**: both providers
  wired through the registry (`lookup: country provider enabled` / `chain
  provider enabled` in `smd.log`), the config block unchanged at hamnut + QRZ,
  TTLs 365/90. The startup seeding was the no-op predicted. `smd` is deliberately
  NOT auto-start — a stopped daemon is not a fault.
- **Shipped today:** **Settings → Email** + **Settings → Enrichment**; the FOUR
  daemon defects those ports exposed (SMTP password unremovable; blank SMTP
  port/timeout; lookup TTL 0 silently rewritten; enabled QRZ with no password
  stopping the daemon starting); a **stale-reload class fix** across all
  four Settings sections; the **CAT chip** as a readout outside Operate;
  operator **`label`s** on lookup sources; **ADR 0062** — enrichment providers
  now self-register, so adding one is a package plus an import line; and the
  forwarder **`label` in the logbook** (tooltip, dropdown, upload button, empty
  message, queued notice — display only; `name` stays the durable key).
  Detail in Current state.
- **EYEBALL WHEN CONVENIENT** (none of it seen by hand; vitest checks the DOM,
  not the look): Email's password keep / type / remove states and its 587/30
  placeholders; Enrichment's disclosures, TTL-of-0 notice, and Remove switching
  a source off.
- **OBSERVE WHEN THE CHANCE ARISES — NOT tasks, and they gate nothing.** Do not
  schedule these and do not open a session asking whether they are done; confirm
  opportunistically. SSE revival (background the MAP tab — same window, browsers
  throttle on visibility not focus; **a negative result means the TRIGGER is
  wrong, not `openReviving`** — see Current state) · FT8 dead-source watchdog ·
  ADR 0059 auto-work · stuck-TX / RF-ingress 2 s tune trials INTO THE ANTENNA
  (operator's call on RF exposure).
- **NEXT ACTUAL WORK — (a) IS THE ONE THE OPERATOR PICKED, deferred only because
  the session ended:** (a) a **Settings navigation guard**. **Draft the
  acceptance criteria BEFORE any mechanism** — two things are non-obvious and
  neither is mine to decide. The discard happens on RETURN, not on leaving (the
  draft survives navigation; the remount's `load()` → `#apply()` overwrites it),
  so there is no natural moment for an "as you leave" warning and the guard has
  to be placed deliberately. And "leaving Settings" has several readings —
  sidebar links, browser back, tab close, switching BETWEEN Settings sections
  while one is dirty — which do not all deserve the same treatment. Open
  questions for the operator: block or warn, and should unsaved edits SURVIVE a
  round trip rather than be discarded at all.
  Then: (b) **Playwright** layout check — NOT scaffolded at all, so config + CI
  + a first spec; (c) the **R9LAU map bug** — verify against the DB first.
- **CONFIG-SPA RETIREMENT (checked 2026-08-03):** Settings now has Station ·
  Rigs · Forwarding · Email · Enrichment. Two tabs remain unported — **FT8**
  (colours, display, PSK Reporter, decode log) and **General** (operating,
  contacts map, about). Those are the last blockers on retiring it.
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

> **2026-08-03, LAST — ADR 0062: ENRICHMENT PROVIDERS SELF-REGISTER. Three
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

> **2026-08-03 — SETTINGS → ENRICHMENT SHIPPED, over four commits and four
> reviews. The two middle reviews each found a real defect in the PREVIOUS fix.**
>
> - **PORTING FOUND TWO MORE DAEMON DEFECTS**, both predating the port and both
>   reachable from the config SPA too. (1) **A lookup TTL of 0 was silently
>   rewritten.** `Orchestrator.isStale` treats a non-positive TTL as "trust the
>   cache indefinitely" — which is what the UI has always told the operator —
>   but `applyDefaults` treated 0 as unset and stamped 365/90 over it at every
>   Load. So a deliberate 0 worked until the next restart and then meant a year.
>   Fixed by making the two TTLs `*int` (nil = default, filled in `Normalize` so
>   it applies on PUT too; explicit 0 survives). **I nearly filed the opposite
>   finding** — the UI text looked wrong until I read `isStale` itself. The
>   inference was wrong; only the code settled which half was broken.
>   (2) **An enabled QRZ with no password returned 200 and then stopped the
>   daemon starting** (`qrz.Initialize` rejects it → `buildEnrichment` →
>   `run()` returns). Now refused at save time by `validateLookupProvider`, with
>   the limits in `internal/types` because `internal/config` → `internal/lookup/qrz`
>   would be a cycle.
> - **THE SECOND DEFECT WAS ONE I INTRODUCED BY PATTERN-MATCHING.** The Remove
>   control was carried over from Email, where `validateSmtp` has never required
>   a password because unauthenticated submission is legitimate. QRZ's is not.
>   `forwarding.svelte.ts` ALREADY documents this exact failure mode ("emptying a
>   required credential is not a reset — the forwarder's New() rejects it,
>   aborting startup after the PUT returned 200"). **"The same masked-credential
>   pattern" says nothing about whether the credential is OPTIONAL.**
> - **THEN THE FIX CARRIED ITS OWN DEFECT, TWICE OVER.** Auto-disabling on Remove
>   mutated `enabled`, and neither way back restored it — so changing your mind
>   saved QRZ disabled while the notice said a new password would revive it. The
>   fix that stuck REMOVES state rather than adding a restore step: `enabled` is
>   never mutated and the effective value is derived as "enabled AND no removal
>   pending", which also drives the toggle, its locked state and the summary
>   pill from one place. My own U3 had exercised the reversal path but asserted
>   only `passwordCleared` — **a weaker statement than the rule it claimed to
>   pin**, which is why the defect survived it.
> - **A TEST WAS PASSING FOR THE WRONG REASON** and is worth remembering as a
>   shape: W4c asserted the draft was intact AFTER `save()`, but a successful
>   save re-hydrates the draft from the response, so it held whether or not the
>   mutation had happened. Moved before the save, it failed correctly.
> - **EVERY SOURCE IS A DISCLOSURE** (operator's call), the Forwarding shape.
>   More than a restyle: the flat layout rendered only QRZ and Hamnut, so a
>   provider this build does not recognise was PRESERVED BUT INVISIBLE. Listing
>   every source makes the thing the whole-chain rule protects something the
>   operator can see. `mergeLookup` replaces the chain WHOLE, so the state model
>   holds every provider as a draft — that makes the safe payload the natural one
>   to build rather than something the save must remember to reconstruct.
> - **`label` ON LOOKUP SOURCES**, mirroring forwarders: operator-set in
>   config.json, read-only on the wire, display chain label → built-in → raw id.
>   `mergeLookupProvider` carries it from the STORED entry, because the rebuild
>   keeps only what it names — the same trap that silently ate forwarder
>   `endpoints` for a while.

> **2026-08-03 — SETTINGS → EMAIL SHIPPED, and the port exposed two daemon
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
