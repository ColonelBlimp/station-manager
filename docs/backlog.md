# Backlog — the definitive ranked worklist

**Single purpose:** the one authoritative, priority-ordered list of every
triaged, not-yet-done piece of cross-cutting work. If it's real work and it
isn't being done *right now*, it lives here — ranked. This is where "what's
next, and in what order" is answered.

**How it relates to the other docs** (full map: `docs/README.md`):
- Raw, un-triaged notes jotted mid-operation live in `docs/dogfood-inbox.md`
  and **graduate here** once triaged (or are struck there as non-issues).
- The 1–3 items in flight **right now** live in `docs/session-handoff.md`'s
  active-cycle list, pulled from the top of this file — that doc does NOT
  re-rank the queue; **this file owns the ranking.**
- FT8-internal implementation mechanics live in `docs/ft8.md`; only
  *cross-cutting* FT8 work lands here.
- Shipped / resolved / ruled-out items are **moved** to `docs/backlog-archive.md`
  (NOT read at session start) so this file stays lean. Open the archive only when
  you need an item's history.

**Conventions:**
- The **Worklist index** below is the ranked table of contents — one terse line
  per item. The **detail** for each lives in its full entry further down; lead
  each entry with the surface (file/subsystem) so its title is greppable.
- When an item ships, **move its detail block to `backlog-archive.md`** and drop
  its index line — do NOT strike it in place. A `~~struck~~` top-level item left
  in this file is a bug: relocate it. (Struck *sub-bullets* inside a still-open
  item are fine — they show which sub-steps of live work are done.)
- Tiers: **P0** correctness/safety · **P1** finish in-flight · **P2** next
  features (by workstream) · **P3** deferred/large/needs-trigger · **Designed
  workstreams** (built on go-ahead) · **Parked** (not work).

## Worklist index (ranked) — the definitive "what's next"

> **▶ Active cycle (set 2026-07-04): _Get to the next shippable state for 7Q8AC._**
> The goal is a release the external operator (7Q8AC, Malawi, offline-first) can run;
> "stabilise & finish in-flight" is the means. Clear P0 + the P1 finish/validate items
> — they ARE the ship gate — before opening any new P2 workstream. Theming (P2 ·
> _UI cohesion_) is deliberately **not now**. The live active-cycle checklist is in
> `docs/session-handoff.md`; this index is the full ranked queue.

**P0 — correctness / safety (do first)**
- _None currently open._ (Last cleared: `PUT /v1/config` omitted-blocks-zeroed, FIXED 2026-07-04 → archive.)

**P1 — finish in-flight / validate (small; closes open arcs)**
- Behavioural retest of shipped daemon changes on the dogfood daemon (session 192/193 batch)
- Fix stale test `internal/api` `TestVersion_HappyPath` — expects schema v3, the DB now migrates to v4 (log migration `0004_utc_timestamps`); bump the expected version in `handler_version_test.go`. Quick; greens the full api suite. (dogfood triage 2026-07-08)

**P2 — next features (open one workstream per active focus)**
- ~~**▶ NEXT (set 2026-07-14, operator directive): _FT8 — reduced type-4 hashed QSO ladder._**~~ **BUILT 2026-07-16 (ADR 0048), offline-gated — on-air validation is the one remaining step.** The reduced `bare-calls→RR73→73` ladder (no grid/report — the protocol has none for a hashed call) completes a QSO with any **nonstandard call** (`/D`, `/M`, prefix-compound `PJ4/NA2AA`). Built as an **isolated parallel path** (Field Day pattern): `type4.go` + `type4_sequencer.go` (`T4Exchange`/`T4WorkExchange`, `seqAnsweringT4`/`seqWorkingT4`), `Service.StartQsoT4`/`StartWorkCallerT4`, `mode:"type4"` on the two existing routes, `type4:true` on the `ft8-qso` SSE, and the SPA answer path (`isCqType4` / `isNonstandardCall` → reduced ladder). **Matching is on the SPELLED partner** — no 22-bit hash table (go-ft8 exposes no decoded-hash integer, and the partner always spells itself, so a table buys nothing; ADR 0048 chose this over a persistent decoder). Logging is degraded: RST_SENT = our SNR, RST_RCVD blank, no grid. RF-safety gate `TestType4_RoundTrip` green. **Work-a-caller SPA trigger deferred** (our call is hashed on the wire, so the browser can't tell "called me" from "called someone else" — a nonstandard caller is worked via the answer path). **Next: work a real nonstandard station on air → flip ADR 0048 Proposed→Accepted.** Detail in `docs/ft8.md` "Nonstandard / compound calls"; the 2026-07-14 `/D` probe under "FT8 — work type-4 compound calls".
- **▶ was-NEXT (bumped 2026-07-05):** _Re-enrich a logged QSO._ **SHIPPED as the frontend/app logbook-page Re-enrich repair path (session 212, 2026-07-13).** Companion **manual FAQ** on the name-missing cause/remedy still open (small). _(Relocate this line to the archive once the FAQ lands.)_
- _SPA architecture (post-ship · ADR 0044):_ consolidate logging+config+logbook into **one app shell** (dashboard + Operate[Phone/CW+FT8] + Logbook + Settings; manual stays zero-JS — 3→1). **Subsumes** the _UI cohesion_ cluster below (theme built into the shell from the first commit). Gated behind the 7Q8AC ship.
- _UI cohesion:_ shared theme layer (token convergence) → UI themes + dark mode → FT8 Spectrum colour revision · version-in-tab-title
- _FT8:_ ~~reduced type-4 compound QSO ladder (`/D` / prefix-compound)~~ **BUILT 2026-07-16 (ADR 0048), on-air pending** · type-4 free-text messages · type-4 work-a-caller SPA trigger (deferred — hashed-us ambiguity) · attempt-limit SPA control · callsign ignore list · Call-CQ waiting feedback · offset-picker no-overlap snap · same-session dupe → auto-workers · accumulate-mode duplicate rows → slot-grouped display · footer info-strip rehome · shift+ctrl freq-step key parity in FT8 (match phone/CW) · work-path opening: prefer clean next-slot start over truncated immediate fire
- _Forwarding / data:_ clear queued-upload backlog for a forwarder · configurable session-email subject/body · operator-email-address config field
- _Infra:_ SPA SSE consolidation (one multiplexed stream) · `/v1/hardware` audio availability + enum caching · CI-V `sets_state` value-compat validation · `internal/iocdi` contract hardening · multi-tab operating-lock (ownership + take-over; awareness banner already shipped)
- _Data / SM-Cloud prep (do before S3):_ ~~migration 0004~~ **DEPLOYED + VERIFIED 2026-07-05** on the live 5,148-QSO dogfood DB (`schema_migrations_log` v4 clean; 0 debris/unparseable rows; `created_at` matches `qso_date`/`time_on` in UTC) → move to archive · `internal/database` review lows (cold-insert retry, bootstrap stale-table detection, + 5 nits) — still open
- _Code-review lows (2026-07-05 `internal/api` review):_ ~~disabled-subsystem routes 200-HTML/405 + `/assets/` listing~~ **FIXED 2026-07-05** (`spaHandler` `/v1/`→404 guard + directory→SPA-fallback; tests) · negative server-limit panics at startup (→ validation error) · credential-clear asymmetry (forwarder clears on blank, SMTP/lookup keep) — unify or document side-by-side · stale `middleware.go` Unwrap comment
- _Code-review nits (2026-07-05 `internal/qsoservice` review):_ `uuid_conflict` classification unreachable under `force` (`submit.go:322`, drop `&& !force` — trap for a future `--force` import) · `importBatchFallback` publishes Hub events, contradicting `SubmitImportBatch`'s "does NOT publish" doc (note the fallback exception) · best-effort `contacted_station` cache warm-up uses the request ctx (a detached short-timeout ctx would make it client-independent, like the dedupe refetch)
- _Daemon / data (dogfood triage 2026-07-08):_ **ADIF export omits populated `MY_*` fields** — investigate the compose/export path (`/v1/session/export` + email; possible data-loss) · fill `country.dxcc` entity number in enrichment (`DXCCForPrefix` on `dxcc_prefix`; ~38% of QSOs otherwise carry no DXCC number for awards) · downgrade client-abort enrichment WARN→debug when the cause is request-ctx cancellation (flaky-link log noise) · backport the tightened RST validators (scale + mode-aware) from frontend/app to the shipping logging SPA (entry-error protection) · rename the Operate "Rig" panel → "Rig Control" when rig-control ops land in frontend/app
- _Rig / bands (dogfood triage 2026-07-14):_ **configurable operating bands** — a `config.json` `operating_bands` list feeding the Phone/CW band grid + FT8 buttons + manual dropdown from ONE source (default 160–6m; additive, = today's behaviour); build BEFORE/WITH the rig-control band-jump so the Ctrl+Shift+digit map follows the configured list, not a hardcoded table · **contact view (working panel) re-organise** (frontend/app Operate UI)
- _Onboarding:_ install / first-run friction for non-Linux operators
- _Diagnostics:_ operator log viewer (DB-manager tab)
- _Code-review lows (2026-07-05 SPA review):_ 13 verified low-severity fixes (the fetch-timeout standout was promoted to P1 and SHIPPED 2026-07-05 → archive) — TX_PWR sub-0.5 W rounding (durable ADIF) · state-reset gaps (tabCount / freqKnown / stale decodes / enrich zombies) · FT8 UI nits (bearing 360°, drain-abort, FD tooltip, isWorking split, canAnswer TX-guard) · edit-overlay mode dropdown
- _Bridge/TX hardening (2026-07-05 `internal/bridge` review):_ 3 low fail-safe items — auto-off retry can clobber a fresh key's timer (per-key generation counter) · garbled first IDENTITY permanently write-blocks the instance (let a later exact match confirm) · `bridge.New` trusts Serial/Cat non-nil (nil-check in Initialize)

**P3 — deferred / large / needs a trigger**
- CAT poll mode (ADR 0034) · FT8 semi-auto watch-list (SET ASIDE) · spot-submitter registry (on 2nd destination) · operator / user profiles (contesting lens: bundle op-identity + contest params, swap mid-event — dogfood 2026-07-06) · outbound UDP telemetry (WSJT-X-compatible) · FT8 occupancy waterfall render · POTA fields · config hot-reload · settings help tooltips + beginner/expert mode · FT8 Monitor/Listen toggle (DISCUSSION) · download-site install page · `PUT /v1/config` `default_logbook.id` wiring (no consumer yet)
- _Deferred features / design (dogfood triage 2026-07-08):_ `MY_RIG` follow the CAT-identified rig when connected (config = fallback) · single-source the freq→band table + regional band-plan design (three hand-synced copies today) · FT8 tune-carrier occupancy-skip (pending HW check on whether the RTTY tune tone bleeds into RX audio) · QSO contacts map — **session-first** (Operate/Session panel list⇄map toggle), whole-log dashboard map as phase 2 (Natural Earth bundled, d3-geo, offline; scoped 2026-07-14) · FT8 auto band-hop / "run the bands" · voice keyer + phone/CW auto-CQ + QSO copilot (crosses the v1 "no phone/CW PTT-for-operating" line — post-ship) · movable / dockable nav · propagation / conditions panel (external online data source — dogfood 2026-07-09) · 2nd callsign-enrichment provider (HamQTH fallback link in the lookup chain, catches QRZ-absent calls — dogfood 2026-07-13) · smcloud "am I being heard?" pile-up status site (community-phase, capture-don't-build — dogfood 2026-07-11)

**Designed workstreams — built on go-ahead (not queued)**
- SM Cloud P1 (ADR 0040 + `docs/v2-design/sm-cloud-p1.md`) · DB-manager SPA (4th SPA — incl. a data-validation / DXCC-consistency-checker surface mirroring `scripts/qso-audit.py`, dogfood 2026-07-13)

**Parked — blocked or out of scope (do not pick up now)**
- _Blocked on external event:_ **FT8 Field Day UI** (FD-aware Operate ladder render · pile-up Ctrl-click · config-SPA section dropdown) + any further FD on-air validation — the FD path can only be exercised **during a Field Day contest**, so it waits for the next one. Flows already shipped + on-air-validated 2026-06-28. NOT a 7Q8AC ship concern (ARRL/RAC-only; a Malawi op doesn't run FD).
- _Out of scope (never):_ FT8 automatic / unattended sequencing — the FT8 spec forbids it; SM is attended-only.
- _Future thinking:_ "design our own sequencing / timing".

## Bugs (detail)

- **`PUT /v1/config` — wire `default_logbook.id` on PUT (P3 residual).** The
  omitted-blocks-zeroed data-loss half (M5) shipped 2026-07-04 (see backlog-archive);
  this is the leftover: `default_logbook.id` is still never copied on a PUT. Deliberately
  left — there is no logbook-switching consumer yet and it is NOT a data-loss path (an
  omitted id just isn't applied). Wire it (with active-row validation) if/when a
  logbook-switch UI lands. See `docs/reviews/internal-api-2026-06-14.md`.

- **FT8 accumulate-mode "duplicate rows" → slot-grouped display (NOT dedup).**
  Reframed 2026-07-02 (was "Rx Frequency shows duplicate rows when feed ≠ single").
  The apparent duplicates in `accumulate` feed mode are *legitimate* — the same
  station decoded across multiple 15 s slots — so the fix is **presentation (group
  by slot)**, not dedup (dedup would hide the useful "still calling / SNR-trend"
  signal). **Discovery 2026-07-02:** Band Activity **already has a slot divider** —
  `slotSeparator` (`Ft8Panel.svelte` ~758) draws one on each slot change showing the
  slot's UTC **time + band** (e.g. `14:30:15 · 20m`), gated on
  `feedMode === 'accumulate' && !cqToTop`. So it's largely done for the
  accumulate + non-cqToTop case (the time is already there). **Decided (operator,
  remote): keep the Band Activity divider; Rx pane left OPEN pending dogfood.**
  **Todo next:**
  1. ~~Add **parity** (even/odd) to the `slotSeparator` label.~~ **DONE 2026-07-03.**
     The divider now reads `14:30:15 · 20m · even` (`slotParity(utc)`), and took a
     styling pass at the operator's request the same day: filled `bg-gray-400` bar,
     `text-gray-700`, left pad `pl-2`, top border removed. `Ft8Panel.svelte`
     `slotSeparator`.
  2. Confirm whether the "duplicate rows" were seen under **`cqToTop`** — that
     ordering **suppresses the divider** (decodes get reordered, so slot-grouping
     can't apply). Decide: WAI, or should grouping still apply under cqToTop?
  3. **Dogfood** the divider (with parity added) to get a better story, THEN decide
     whether the **Rx Frequency pane** needs its own grouping/time or is fine as-is
     (no dedup today; `rxDecodes` ~520, filters by worked-call / offset±tol). Rx
     stays open until then.

- **Multi-tab operating-lock — ownership + take-over (P2; awareness banner already shipped).**
  Filed 2026-06-27 (operator flagged as a real risk). The SPA is multi-tab: every tab
  subscribes to `/v1/rig/events` and any tab can `POST /v1/rig/command` — no "which tab
  owns the rig" concept. The *dangerous* cases are already prevented daemon-side: writes
  serialise at `cmdMu`; TX is single-flight (`keyMu`/`ErrTxActive`, shared by tune + FT8-TX)
  so two tabs can't double-key or steal the mic. The only residual is the **soft** hazard —
  a freq/band/mode change in one tab moves the one radio another tab is operating on.
  **Advisory awareness shipped 2026-07-04** (see backlog-archive): the daemon emits a
  `rig-clients {count}` SSE event on multi-tab transitions and the logging SPA shows a
  passive banner when >1 tab is open — enough for the single-op 7Q8AC ship. **Remaining
  (the real lock, P2):** a daemon-tracked owner so a non-owner's write is *rejected*
  (read-only), with explicit take-over. Design facts from the 2026-07-04 dig: there is NO
  client/tab identity on any rig endpoint today (all anonymous; `EventSource` can't send
  headers), so it needs a per-tab id (body/query on commands + a correlating handshake on
  the SSE), a new `ErrNotRigOwner` gate in `SendCommands` (mirroring the `ErrTxActive`
  check + 409 mapping), a `POST /v1/rig/control` acquire/release/take-over endpoint, an
  owner-broadcast SSE event, and lock UI in the logging SPA (only SPA that controls the
  rig). Only worth it when multi-tab / multi-op is real (e.g. alongside a 2nd operator or
  smcloud). Related dogfood notes (same root): "Next during TX moves on mid-transmit",
  "currently-worked station still clickable in Band Activity".

- **`internal/iocdi` contract hardening (concurrency + build-time validation).**
  Filed from the `internal/iocdi` review (2026-06-19, M1 + M3 + M4); deferred
  because the daemon registers single-threaded then builds, so none is exercised
  today — the fail-fast wins (M2 reject duplicate/invalid registration) + L1/L2
  shipped 2026-06-19. **M1:** registration isn't transactional — dependency
  discovery + the `built` check happen before `regMu` is taken, so a concurrent
  `Register`/`Build` can race the dependency map or accept a post-build bean. Fix:
  do the whole register transaction (check `built` → discover deps → validate →
  mutate both maps) under one lock; add a race test. **M3:** `requiredDependency`
  stores one type per bean ID, so two fields sharing a tag ID with different types
  lose one; and unexported/incompatible tagged fields are silently skipped, so
  `Build` can succeed with a nil `di.inject` dependency that fails at first use.
  Fix: store dependency edges per-field and validate EVERY tagged field at build
  (settable, bean exists, assignable) — treat tagged-but-unset as a build error.
  **NB before doing M3: confirm the current cmd/smd wiring has no latent
  unsatisfied tag, or it will turn into a startup failure.** **M4:** `Build` runs
  user `Initialize()` while holding `regMu` (an initializer that resolves/registers
  would deadlock), and a failed initializer leaves `built=false` so a retry re-runs
  the already-succeeded ones. Fix: release the registry lock before invoking
  initializers, add a `building` state, and document/enforce initializer
  idempotence. See `docs/reviews/internal-iocdi-2026-06-19.md`.

- **`/v1/hardware` per-direction audio availability + enumeration caching.**
  Filed from the `internal/hardware` review (2026-06-19, M2 + M3), deferred to the
  **config-SPA workstream** (the unbuilt consumer). **M2:** `AudioDevices` returns
  `(capture, playback, err)` but errors on EITHER direction's failure, and the
  handler collapses that to `audio.available=false` with both lists empty — so a
  playback-enumeration failure hides a working capture list (and vice versa).
  Fix: surface per-direction availability (e.g. `capture_available` /
  `playback_available` on the `/v1/hardware` response, or populate the successful
  direction + log which failed). It's a Tier-1 `api-endpoints.md` wire change, so
  shape it WITH the config SPA that consumes it. **M3:** `/v1/hardware` does
  uncached live OS/audio enumeration on every request (a fresh malgo
  context per call), with only the default 128-wide request limiter — a buggy SPA
  tab / retry loop can spin up many audio contexts. Fix: a short-TTL cache or
  singleflight around enumeration (+ maybe a lower per-route cap), with a refresh
  path if the picker needs immediate hot-plug detection. Both are hardening for
  the config SPA; the M1 wrong-codec safety guard + L2 labels shipped 2026-06-19.
  See `docs/reviews/internal-hardware-2026-06-19.md`.

- **CI-V `sets_state` value-compatibility validation.** Filed from the
  `internal/cat` review (2026-06-19, L1). `ValidateRigDefinition` rejects a CI-V
  `sets_state` that names no State marker, but it does NOT verify the command's
  *encoding* can populate that marker — so a future CI-V rigdef could pass
  validation with a mismatched pair (a `bcd_freq` command setting a mode tag, a
  `bcd_power` command setting `MAINMODE`, or a valueless command declaring
  `sets_state`), and the wait-for-ACK path (ADR 0034) would then synthesize the
  wrong/empty state push after a successful ACK. NOT an active bug — the shipped
  IC-7300 rigdef's `sets_state` pairs are all correct (pinned by
  `TestCommandSetsState`). Deferred until external/operator rigdef loading is real
  (`RegisterExternalDir` is still a stub), since that's when an unvetted rigdef
  could actually reach this gap. Fix: extend CI-V validation so `sets_state` is
  encoding-compatible (`bcd_freq`→BCD-freq marker, `bcd_power`→BCD-level marker,
  `mode_seq`→marker whose mapped values include the mode literals; valueless
  commands can't declare `sets_state`), with negative tests per incompatible pair
  + a positive test per shipped IC-7300 `sets_state` command. See
  `docs/reviews/internal-cat-2026-06-19.md`.

- **Configurable session-email subject + body (formatting tags).** Flagged
  2026-06-17 as multi-operator interest grows — a QSL manager receiving logs from
  several operators benefits from operator-tailored, distinguishable mail. Today the
  subject (logbook-callsign-prefixed default) and body ("ADIF for this session
  attached." / "Contains N QSOs." / "Generated by …") are **hardcoded** in
  `sessionEmailSubject` / `sessionEmailBody` (`internal/api/handler_session_email.go`).
  Let the operator configure both via **templates with substitution tags** — e.g.
  `{callsign}`, `{count}`, `{date}`, `{logbook}`, `{to}` — stored in `config.json`
  (durable per-operator setting, per the settings-in-config rule; natural home is
  alongside the SMTP block) and edited from the SPA email/Settings surface. Keep the
  current hardcoded strings as the **defaults**. Design points when picked up: the tag
  set + a *small, safe* substitution (no general templating engine — just `{tag}`
  replacement); and whether a **per-send free-text note** (a transient SPA input,
  distinct from the durable template) is also wanted. The callsign-prefix + QSO-count
  shipped 2026-06-17 as the hardcoded first step toward this.

- **CAT poll mode (rigdef-configurable) — deferred (ADR 0034).** The bridge read
  model (ADR 0019) is push-only: the rig broadcasts on change (Yaesu/Kenwood AI,
  Icom CI-V Transceive). Designed but deferred: an optional rigdef `poll` block
  (interval + read command) that sends `READ` on a timer and flips liveness so a
  *missed poll* — not silence — is the disconnect signal. For Icom we instead
  document **CI-V Transceive ON** as an operator prerequisite. Add the `poll`
  block (additive; push-only rigs unaffected) if an operator can't keep
  Transceive on, the bus contends with other software, or state Transceive
  doesn't broadcast surfaces. Decide one-missed-poll vs N-strikes then.

- **FT8 offset picker — daemon-side no-overlap snap + click-anywhere.**
  `Ft8OccupancyStrip` offers daemon-vetted clear offsets as discrete markers
  today; clicking arbitrary spectrum (with a daemon-side snap to the nearest
  no-overlap slot) is future work.

- **FT8 semi-auto response to a session watch-list — SET ASIDE 2026-06-12 in favour
  of the caller-side work-stack; hunter/auto-fire variant stays UNDER CONSIDERATION
  (grayline, NOT decided).** The 2026-06-12 stack discussion concluded the
  **caller-side pile-up work-stack** (feature above) is the path: it delivers the
  "curate calls + work a queue" benefit while staying attended — the operator pops
  each contact, no auto-fire. The watch-list's *only* unique value was the **hunter**
  case (auto-respond to a wanted CQ inside the one-slot reply window, faster than a
  human can click), and that auto-initiation is exactly what crosses into
  daemon-initiated operation. So it is parked, not dropped; the original idea is
  preserved below. Idea: the operator manually selects a set of callsigns into a
  **session-bound** list and clicks **'Go'** to arm it; when one of those calls
  then appears as a CQ in a decode, the daemon responds in the **immediate next
  slot** — using the ADR 0032 synchronised/truncated send to hit the tight
  end-of-decode → next-sequence window a human can't reliably click within.
  Technically a small delta on ADR 0032: the only new piece is swapping the
  per-QSO click for a watch-list match as the initiation trigger; sequencer,
  timing, and off-ramps already exist.
  **Attended framing + guardrails (the operator's design):** the list is
  session-bound (cleared on session end — never persistent, never unmanned), the
  operator manually picks the targets and gives an explicit 'Go', is present and
  supervising, can abort instantly (Abandon / Disarm), and there is no auto-CQ
  cycle — the human supplies the operating *intent* (the selection + 'Go'); the
  software only covers human reaction time in the reply window.
  **Open regulatory question (acknowledged grayline, still being thought through):**
  does pre-authorising a batch + auto-responding count as *attended* (the operator
  initiated the intent, analogous to WSJT-X "Call 1st") or does it cross into
  *daemon-initiated* operation (QEX §9 forbids robotic/unattended; attended-only
  stance)? Not resolved — recorded to keep thinking. **If ever built, it must be
  framed as attended-assisted; public docs must never present it as automatic
  operation.** See memory `project_sm_ft8_attended_only`.

- **FT8 callsign ignore list.** An operator-maintained list of callsigns to
  suppress in the FT8 view — already worked, not being sought, known nuisance, etc.
  Listed calls should be hidden (or clearly de-emphasised) in Band Activity and not
  offered as answerable CQ rows. Distinct from the existing *automatic*
  worked-before tint (`ft8Enrich`): this is a **manual** list with mixed reasons,
  so keep it separate from worked-detection. Open design points: (1) storage — a
  non-session setting → daemon `config.json` (per the settings-in-config rule), with
  an add/remove UX in the FT8 view; (2) behaviour — hide entirely vs grey-out vs
  just non-clickable (lean: hide, with a toggle to reveal); (3) match semantics —
  exact callsign vs prefix/wildcard. Whether it also feeds AP-hint *de*-prioritising
  (ADR 0025) is a later question, not v1.

- **FT8 — work type-4 compound calls + free-text messages.**
  **▶ PRIORITISED NEXT (2026-07-14, operator directive):** build the **reduced type-4
  hashed QSO ladder** so SM can complete a contact with any nonstandard call (`/D`,
  `/M`, prefix-compound `PJ4/NA2AA`). Empirically confirmed 2026-07-14 (probe against
  go-ft8 v0.7.0): a `/D` suffix packs **type-4**, identical to a prefix-compound —
  `CQ` / `RR73` / `73` encode (partner hashed to `<...>`) but the `CQ`+grid /
  opening-call+grid / report / rogered-report rungs are **rejected** ("unsupported
  standard message" — no type-4 grid/report form), so SM's standard grid→report→73
  ladder can't be walked and the sequencer fails soft (`ErrTxBadMessage`). `/P` is the
  exception — it packs *standard*, carries grid+report, and already works end-to-end.
  **Design + alternatives recorded in ADR 0048** (spelled-partner matching · degraded
  FD-style logging · FD-clone isolated path · answer+work-only v1). The build shape is
  in the type-4 sub-bullet below.
  The answer-a-CQ,
  work-a-caller, and Call-CQ paths work any station whose exchange encodes as a
  **standard structured FT8 message**; the sequencer defensively **skips** anything
  else (the dynamic "reply does not encode" guard — every site tries
  `goft8.EncodeStandardMessage` and treats an error as `ErrTxBadMessage`, in
  `caller_sequencer.go` / `sequencer.go` / `work_sequencer.go` / `EncodeWaveform`).
  - ~~**Standard `/P` variant.**~~ **SHIPPED 2026-06-18** with the **go-ft8 v0.3.4→v0.3.5**
    bump — `EncodeStandardMessage` now accepts the standard `/P` variant, so the dynamic
    guards pass it through with **no SM code change** (the encode-check seam was designed
    for exactly this). Proven offline: `internal/ft8/modulate_test.go`
    (`TestEncodeStandardMessage_Portable` + `TestModulate_RoundTrip_Portable`).
  - **type-4 compound / nonstandard calls (`PJ4/NA2AA`, `K1ABC/4`, …) — go-ft8 v0.5.0
    LANDED the encode/decode (2026-07-05), so this is now buildable, not blocked.** State
    after the v0.5.0 bump (session 199): **RX works** — a type-4 CQ (`CQ PJ4/NA2AA`) and
    directed type-4 (`PJ4/NA2AA <...> RR73`) now decode and reach Band Activity (round-trip
    tested, `TestCompoundCQ_Decodes`). **TX does NOT yet run a full QSO:** type-4 carries
    only `CQ`/`RR73`/`73` with the partner call HASHED to `<...>` — there is **no type-4
    grid/report form** — so SM's standard grid→report→R-report→73 ladder can't be walked
    with a prefix-compound partner, and `StartQso`/`StartQsoFd` correctly fail soft
    (`ErrTxBadMessage`). **BUILT 2026-07-16 (ADR 0048):** a distinct reduced
    `bare-calls→RR73→73` ladder (`type4.go` / `type4_sequencer.go`). **No 22-bit hash
    table** — the earlier "resolve `<...>` back to the real call" idea was dropped: go-ft8
    exposes no decoded-hash integer to match against, and the partner always spells itself,
    so matching is on the **spelled** partner (ADR 0048 weighed and rejected a persistent
    decoder / SM-side `hashCall`). Only on-air validation remains.
    `TestPrefixCompound_EncoderBoundary` pins the encoder boundary and will
    flip if a later go-ft8 adds the grid/report forms. **`/R` suffix:** encodes in v0.5.0 but
    go-ft8 does NOT yet DECODE it ("RTTY Roundup … not yet unpacked"), so it fails the
    round-trip gate — do not transmit `/R` until decode lands. **Free text** (71-bit) encode +
    an entry UX is still separate work. Capture point: `docs/ft8.md`; see ADR 0029 (the
    `EncodeStandardMessage` seam).

- **FT8 caller-side sequencing — BOTH flows SHIPPED; only on-air validation remains.**
  `auto_first` (Call CQ, ADR 0033) shipped 2026-06-12 and the operator-pick experience
  shipped 2026-06-17 as the SPA-owned pile-up stack (+ up-arrow reorder, session 192) —
  which **supersedes** the never-built daemon `operator_pick` Call-CQ mode (501-rejected,
  not needed). The one true remainder is **on-air validation** of the caller side + the
  session-tab logging (unit-tested + offline-encode-verified only). Detail below; this was
  the gap on 2026-06-12 when a real 7Q pile-up (DK8IF / DL9UW / …) was unworkable.
  - **SHIPPED — `auto_first` (the WSJT-X "Auto Seq" mode):** the Operate-tab **Call CQ**
    button starts a sequenced session (`POST /v1/ft8/cq/start` → `Service.StartCallCq` →
    the `Sequencer` caller mode) that calls CQ, **auto-works the first answerer** through
    report → RR73, **logs it via the e4 sink**, then resumes CQ — looping until Abandon.
    Caller ladder: `internal/ft8/caller.go` (`CallerExchange`); driver:
    `caller_sequencer.go` (`onSlotCalling`); live role-aware ladder + "Calling CQ…":
    `Ft8MsgPanel`. Config: `ft8.tx.caller_answer_mode` (default `auto_first`). **Needs
    on-air validation** — unit-tested + offline-encode-verified only so far.
  - **SHIPPED 2026-06-28 — `auto_strongest` answerer selection + Settings-tab knob.**
    `onSlotCalling` now picks the **highest-SNR** encodable answerer in the slot (clear
    the loud ones first) when `caller_answer_mode == auto_strongest`, else the first by
    decode order (`auto_first`). Surfaced over `/v1/config` as `ft8_caller_answer_mode`
    (presence-aware; only the two attended auto modes accepted, `operator_pick`/junk →
    400) and editable from the logging SPA's **FT8 Settings tab → Call CQ → Answer**
    (First answerer / Strongest signal). Pile-up drain stays **FIFO** — the knob governs
    only the hands-off auto-answerer. Tests: `caller_sequencer_test.go`
    (auto_strongest-picks-highest / auto_first-picks-first), `handler_config_ft8_test.go`,
    `types/ft8_test.go`. **Needs on-air validation** like the rest of the caller side.
  - **SHIPPED 2026-06-17 — pile-up callsign stacking (the operator-pick experience, as
    an SPA-owned FIFO).** Realised the "pick which caller to work" need via a different
    (operator-chosen) shape than the original daemon `operator_pick` Call-CQ mode:
    **Ctrl/Cmd+click** a calling-you decode in Band Activity to push it onto an in-memory
    **FIFO** (`ft8PileupStack.svelte.ts`), worked **oldest-first**; the Operate view
    **drains** it via the existing work-a-caller path (`StartWorkCaller`) whenever the rig
    is armed+idle, advancing as each contact completes, while the operator keeps adding.
    Capture (Ctrl+click) is available in **any state** (mid-QSO, disarmed — pure capture,
    no TX), which is the whole point: callers are only visible in your RX parity and the
    work-now click is gated on armed+idle, so you grab them when you see them and the SPA
    works them when it can. Drawer (`Ft8PileupDrawer.svelte`) in the Operate tab + a depth
    badge on the tab; **Abandon pauses** the drain (queue kept, Resume on the drawer);
    Clear-all + per-entry remove. **SPA-only** — daemon untouched (reuses work-a-caller +
    the `ft8-qso` idle signal). In-memory (erased on tab/browser close), mirroring
    `callsignStack`. This **supersedes the daemon `caller_answer_mode: operator_pick`
    Call-CQ mode** (still `501`-rejected at `StartCallCq`; not needed — the stack gives
    operator-chosen working for *anyone* calling you, whether or not you called CQ).
    `auto_first` Call-CQ stays as the hands-off "call CQ + auto-work answerers" loop.
  - **Attended either way:** operator initiates by calling CQ, is present, Abandon stops
    it instantly; **no auto-CQ cycle, no auto-fire-on-watch-match** — which is why this
    **supersedes the auto-responder framing** of the watch-list item above.

- **Spot-submitter registry — generalize when a 2nd destination lands (e.g. DX cluster).**
  PSK Reporter is the first "submit what I heard" destination. A **DX cluster spot submit**
  (telnet/TCP to a cluster node, e.g. announce a worked/heard DX) would be a second — at
  which point it's a natural fit to extract a **spot-submitter interface + registry**,
  mirroring `internal/forwarding` (Forwarder interface + `init()`-registered destinations)
  and the lookup-provider chain: one decode-sink fans out to N registered submitters, each
  with its own config/enable/transport. **Deliberately NOT done now** ("build specific, not
  generic" — the v1 `internal/adapters` cautionary tale): one destination doesn't justify a
  framework, and the cluster transport/semantics (TCP, often selective/manual announce) differ
  enough that the abstraction should be designed against **two** real implementations, not
  one. The current `pskreporter.Service` (AddSpot/Flush/Start/Stop) is shaped so the extraction
  is clean when the DX-cluster submit actually arrives. Trigger: that second destination.

- **FT8 main-panel footer → an info strip (rehome the offset readout there).** The
  bottom-of-main-panel footer now holds "Next slot in Ns · even/odd" (added with the
  countdown move). Grow it into a small **info / status strip** and relocate the
  **"Offset N Hz ±tol"** readout into it — today that's the `rxCaption` under the Rx
  Frequency column (`Ft8Panel.svelte` ~line 282). **Pairs with the Rx-pane redesign
  above:** that pane becomes the worked-station enrichment card / blank empty-state, so
  its caption ("Offset N Hz ±tol" / "No offset selected") needs a new home — this strip
  is it. (The caption's "Following <call>" variant is subsumed by the enrichment card,
  so really only the idle offset readout moves.) Net: one tidy status strip along the
  panel bottom — next-slot countdown · parity · selected TX offset — leaving the three
  top panes (Main Freq / Band Activity / Rx-now-enrichment) uncluttered.

- **Install + first-run onboarding is too high-friction for non-Linux operators.**
  Filed from the 2026-06-23 clean-DB dogfood deploy. `docs/install.md` walks the
  operator through detailed manual rig configuration; for a non-experienced Linux
  user that's too much. North-star (KISS): the daemon discovers serial/audio
  devices and offers friendly labels, the SPA picks — the operator never
  hand-edits `config.json` or types hardware ids (extends the rig-profiles
  direction, ADR 0028). Scope is the whole onboarding arc — install, first-run
  rig setup, identity — not just the doc. Design initiative; pairs with the
  fresh-install config-defaults bug above.

- **Download-site install page (derive from `docs/install.md`).** Filed
  2026-06-23. The operator manual deliberately omits install/uninstall (ADR
  0036 arc starts at First Run; the embedded manual is unreachable pre-install
  anyway), so the public download site needs its own install page. Make it a
  lightly-edited operator-friendly rendering of `docs/install.md` (§1–3 install
  + enable, §10 uninstall) so the two don't drift — install.md stays the single
  canonical source. External/website work, out of this repo.

- **Clear the queued-upload backlog for a forwarder (esp. a disabled one).**
  Filed 2026-06-23. The shadow side of ADR 0022's enqueue-by-presence: a
  configured-but-disabled forwarder silently accumulates `pending` `qso_upload`
  rows, so *enabling it later flushes the whole backlog*. The operator needs a
  deliberate way to **discard that queue** ("don't upload the backlog, start
  sending from now"). Design points:
  - Daemon op: purge `pending`/`failed` `qso_upload` rows for a named forwarder;
    **never touch `uploaded`/`success` rows** (those are real upload history).
    Per-forwarder scope (not global). Safe for a disabled forwarder (worker idle,
    no race); for an enabled one, coordinate with the in-flight batch.
  - UX home: forwarder management in the **config SPA** — show "N queued for
    upload · [Clear queue]" next to each forwarder, with confirmation. Pairs with
    the enable/disable toggle (consider offering "clear queue?" when disabling).
  - Consider recording the purge in the audit trail (`qso_history`) so a cleared
    backlog is explainable later.
  - **Document the underlying behaviour for operators** (manual forwarding
    chapter, currently a stub): adding a forwarder queues *future* QSOs; disabling
    doesn't stop the queue growing, only the sending; this is the lever to empty
    it. The inverse (bulk-forward an existing log to a newly-enabled service) is
    the separate backfill feature.

- **Operator / user profiles — selectable op-identity bundles.**
  Filed 2026-06-24. Surfaced during the config-SPA Station-tab split: the
  **operational op-identity pair** (`Operator` callsign + `Operator Name`, and
  possibly `Owner's Callsign`) stays in the logging SPA precisely because it
  swaps per session — in a contest or multi-op you change operator mid-event.
  The future enhancement: turn that pair into **named, selectable profiles** the
  operator picks at session start instead of retyping (e.g. a dropdown of saved
  operators on the logging My Station / QSO surface). Daemon owns the profile
  list (config.json), SPA picks. Keep the current single-pair fields working;
  profiles layer on top without foreclosing today's split. Not scoped/started —
  a direction, not a commitment. Depends on the config-SPA workstream existing
  first (that's where profile CRUD would live).

- **Configurable operating bands (P2 · daemon + all band surfaces).** Filed from
  dogfood 2026-07-09. Antenna coverage varies and many ops don't work all bands
  (7Q5MLV skips 160/60/30), so add a station-level **`operating_bands`** list to
  `config.json` and drive EVERY band surface from that one source for consistency:
  the Phone/CW band-button grid, the FT8 band buttons, and the manual band
  dropdown. Default when unset = full 160m..6m (additive — today's behaviour);
  render canonical low→high. Keep **distinct** from `ft8_frequencies` (FT8 buttons =
  `operating_bands` ∩ ft8-freq bands). **Sequencing catch — do this BEFORE/WITH the
  rig-control band-jump (Slice 4):** the shipping SPA's Ctrl+Shift+[digit] band-jump
  maps digits 1–0 → 160m..6m as a FIXED table; with configurable bands the digit→band
  mapping must become configurable too (simplest = digits follow `operating_bands`
  order; fuller = an explicit digit→band map). Build the band-jump against the
  configured list rather than a hardcoded table, so it isn't hardcode-then-rework.
  Editor home is the Settings card (config surface, not yet in frontend/app); the
  dogfood shortcut is to add the daemon field + wire the grid consumer FIRST
  (config.json hand-editable), Settings checkboxes follow as polish.

- **2nd callsign-enrichment provider (HamQTH fallback link).** Filed from dogfood
  2026-07-13 when the live re-enrich flow was validated but couldn't name-repair
  **RG6S** — the callsign isn't on QRZ.com, and QRZ is the only callsign-class
  provider configured (not a flow bug; no source had the data, and the country layer
  still repaired country/dxcc/zones via hamnut). The enrichment orchestrator already
  runs a provider **chain** (`o.Chain`) with QRZ as its only link, so a second
  callsign provider (e.g. HamQTH, free tier) as a fallback would catch some
  QRZ-absent calls (Russian/CIS calls are a common QRZ gap). Needs: a provider client
  + chain config + the ADR 0017 cache semantics it already gets for free. Flaky-link /
  Malawi-relevant (more sources = more complete offline records). Untriaged detail was
  in the inbox 2026-07-13.

- **smcloud "am I being heard?" pile-up status site (P3 · community phase, capture-don't-build).**
  Filed 2026-07-11, refined across that session. When running a pile-up, SM (local)
  publishes to a PUBLIC website; a caller opens the page, types their callsign, and
  sees their **status** — no SM install needed caller-side. **Publish STATUS, not
  queue rank** (the critical reframe): data source = the DECODE FEED (everyone SM
  decoded calling the op this session), NOT the operator's curated Ctrl-click stack
  (most callers aren't in it → stack lookup returns "not found" for the common case).
  States: **worked ✓** / **heard — not yet worked** (decoded this session) / **not
  heard**. Avoid a "#N position" — FT8 pile-ups aren't ordered queues, so a rank
  promises a fairness the op won't honour. **Unique niche:** ClubLog Live Stream shows
  the DX's LOG (worked-✓ half); PSK Reporter shows where MONITORS heard you; NEITHER
  shows "the DX's own receiver is hearing you" — that middle state is the gap SM can
  own (it has the decode feed + session log locally). Also show **on-air + frequency**
  now ("7Q8AC is on-air: 14.074 MHz (20m FT8)") so the page is discovery, not just
  status — data is already local (CAT dial freq + FT8 band/mode). **Cost:** local side
  mostly exists; new work = smcloud (a small endpoint taking per-slot snapshots
  `{dx_call, on_air, freq, band, mode, decoded[], worked[]}` + a lookup page
  `?dx=7Q8AC&me=G4XYZ`) — ~weekend MVP, not a platform. **Caveats:** best-effort
  publish (enrichment-never-blocks discipline — a failed push never touches the QSO);
  FLAKY-LINK staleness is the real risk (a stale freq/decode misleads worse than
  nothing → a prominent "updated Ns ago / STALE" stamp is mandatory, and an explicit
  active-vs-idle concept: idle shows "last operated Xh ago"). **Distribution:** embed
  as a QRZ.com bio `<iframe>` (callers reflexively open qrz.com/db/<dx>) — MAKE-OR-BREAK
  UNKNOWN = whether QRZ permits iframes in bios AND whether HTML-bio editing needs paid
  QRZ XP; fallback = a prominent link/button. **Notifications guardrail:** do NOT
  auto-email callers (spam — GDPR/PECR + CAN-SPAM + QRZ ToS + deliverability; link-only
  email doesn't fix the unsolicited SEND, and the QRZ-embed already puts the link where
  callers go). The only legitimate shape = CALLER-initiated opt-in ON smcloud ("notify
  me when 7Q8AC works me", 1:1, transactional-after-a-real-QSO, with unsubscribe).
  Sits with the SM Cloud P1 designed workstream (ADR 0040) + the P4 community bucket;
  orthogonal to the frontend/app daily-driver work. Full note: `docs/dogfood-inbox.md`
  2026-07-11.

- **QSO contacts map — time-window view over logged data (P3 · frontend/app; design settled 2026-07-16).**
  A great-circle map from the operator's QTH to worked stations — a loved ham feature
  (WSJT-X / QRZ / Log4OM all have one), not eye candy. **Decision: the map is a read-only
  view over logged QSOs for an operator-picked time window** (last 60 min / 5 h / 10 days /
  …), live-updated as QSOs land. No session entity — stored or derived.
  - **▶ DESIGN SETTLED 2026-07-16 — time-window, not sessions; ADR 0049 REJECTED.** The
    2026-07-14 session framing (sessions table + `session_id` stamped in `prepareQso` +
    `GET /v1/session/{id}/qsos`) was rejected before implementation: a structural
    write-path change for a display feature, and every boundary scenario demanded more
    machinery (merge-correction UI, restart semantics, threshold config) for boundaries
    that are derivable at read time — a derived window is *recomputable*, a stamped one is
    frozen wrong (full rationale: ADR 0049's rejection note). The replacement needs
    (almost) **zero daemon change**:
    - **Initial render:** fetch the window from `GET /v1/logbook/{id}/qso` — cursor pages
      are newest-first over `{qso_date, time_on, id}`, so page until rows pass the window
      edge. Rows are full `types.Qso`, so the precomputed `lat`/`lon`/`my_lat`/`my_lon` in
      `additional_data` come along free. Optional nicety if paging feels clumsy in
      practice: a small read-only `since` query param on the existing endpoint.
    - **Live update:** subscribe to the existing `GET /v1/events` firehose —
      `qso.stored`/`qso.updated`/`qso.deleted` already fire from `qsoservice` for EVERY
      logging path (Phone/CW submit, FT8 e4 sink, PATCH edits; `submit.go`). Per the
      documented reconnect contract: open the stream first, then fetch the window; on a
      `qso.*` event refresh the window head (first cursor page — the payload is minimal
      `{qso_id, logbook_id}` by design, and head-refresh is cheap + idempotent).
    - **Delivery:** a separate full-window route in `frontend/app`, opened in its own tab
      (second monitor) — ADR 0049's overlay-blocks-operating reasoning stands; only the
      data source changed. An **"Open map ↗"** button in the shared Session tile.
  - **▶ IMPLEMENTATION PLAN (revised 2026-07-16 — SPA-only, two phases; the drafted ADR 0049
    daemon spine [migration 0005 / lifecycle store / session endpoints] is DROPPED with the
    rejection).**
    - **Phase 1 — render engine.** Add `d3-geo` + `topojson-client`; bundle Natural Earth
      110m TopoJSON (`import`ed → Vite bundles → `//go:embed`'d, never fetched). Reusable
      engine: projection, country render, great-circle **arc sampler** (`geoInterpolate`),
      fed by the precomputed `lat`/`lon`/`my_lat`/`my_lon` in `additional_data`. **Proof:**
      engine unit tests (projection, arc sampler, antimeridian cases).
    - **Phase 2 — map route (the payoff).** New **full-window route** in `frontend/app`
      (empty dashboard `{:else}` is a candidate host), opened in its own tab; duration
      picker (60 min / 5 h / 24 h / 10 days / custom); open `/v1/events` first, then the
      windowed fetch (cursor pages until past the window edge); arcs + markers + hover
      tooltip (call/grid/distance/bearing via `pathInfo`); window-head refresh on `qso.*`
      events so arcs appear live as QSOs are logged; theme-aware; fail-soft "N of M
      plotted" for grid-less rows. **Proof:** component render over a fixture window + a
      simulated `qso.stored` adding an arc.
  - **Data readiness (verified against the live 5,418-QSO dogfood DB, 2026-07-14):**
    contacted `gridsquare` 98.6%, `dxcc` 100%, `my_gridsquare` 100%; the `additional_data`
    blob already carries **pre-computed `lat`/`lon` + `my_lat`/`my_lon`** (+ `distance`,
    `cont`) — the daemon has done the geo math. So the map is almost pure rendering.
  - **Primitives we HAVE** (`frontend/app/src/lib/utils/bearing.ts`): `gridToDecimal`
    (grid→lat/lon), `pathInfo` (short/long-path bearing + distance), Maidenhead validation.
    Rendering idiom is SVG/DOM (no canvas anywhere) — a vector map fits. **Need to add:**
    a projection (lat/lon→x/y), a great-circle **arc sampler** (`pathInfo` gives endpoints
    only; nothing samples a polyline today), a bundled **basemap**, and a host component.
  - **Superseded earlier framings (kept for the trail):** the original "v1 = Session-panel
    list ⇄ map toggle over in-memory `session.qsos`" (2026-07-14) died with the
    separate-tab decision (a second tab can't see the operating tab's state — which is
    fine, because the daemon fetch replaces it); its `SessionQso`-needs-`gridsquare`
    plumbing gap is moot — the map fetches full `types.Qso` rows, coords included. The
    daemon-owned-sessions framing died with ADR 0049's rejection (see the design bullet
    above).
  - **Render decisions (these survive both rejections unchanged):**
    - **Add `d3-geo` + `topojson-client`.** The one call that pushes against minimize-deps —
      but hand-rolling a spherical projection + antimeridian clipping + `geoInterpolate` arc
      sampling is exactly the fiddly, well-solved math a focused lib should own (~30 KB gz,
      MIT → GPL-clean). Alternative (zero-dep equirectangular + hand-rolled slerp) saves the
      dep but reinvents antimeridian handling and looks flatter. Lean d3-geo.
    - **Basemap = Natural Earth 110m TopoJSON** (public domain → GPL-clean, no ODbL
      share-alike), **`import`ed so Vite bundles it** into `app/dist` → shipped in the binary
      by `//go:embed all:app/dist` (`frontend/embed.go`), **never fetched** — same offline-first
      posture as the emoji-flag util. ~100 KB. AVOID OSM/ODbL-derived data (share-alike).
  - **Follow-on — whole-log Dashboard map (later).** The `frontend/app` dashboard route is an
    empty placeholder (`App.svelte` `{:else}`) wanting a first tenant. The whole-log map
    reuses the SAME render engine; the only new piece is the data source — a small aggregate
    endpoint **`GET /v1/logbook/{id}/map`** returning dedup'd plot coords
    (`[{grid,lat,lon,dxcc,cont,bands[],modes[],count}]`; 5.4k QSOs → a few hundred unique
    grids / ~150 DXCC → one tiny offline-friendly request), rather than paging the cursor API
    over a flaky link. NB this bespoke aggregate slightly tensions with the ADR 0043/0044
    "compose existing + subscribe, resist aggregates" guidance — justified because a
    coordinate projection is a genuinely different shape than paginated rows and is the
    flaky-link-correct choice; record the exception if built.
  - **Per-QSO origin (refinement):** v1 uses a single fixed `myGrid`. Per-QSO `my_gridsquare`
    (100% populated in the blob, but not on the typed client row) would let a roving/multi-site
    log draw per-QSO origins — deferred; not needed for a fixed-location or DXpedition op.
  - **Effort:** engine (d3-geo + basemap + projection + country render + arc sampler) ~1 day ·
    map route (duration picker + windowed fetch + `/v1/events` live refresh + markers/tooltip)
    ~1 day · theming + "Open map ↗" + tests ~0.5 day → **~2–2.5 days for the time-window map**
    (down from ~2.5–3 + the dropped daemon spine), after which the Dashboard map is "swap the
    data source." **Stays P3** — a delight feature; a shovel-ready block for a UI cycle.
    Related: the LSPA "future POTA fields", the FT8 session log, `docs/dogfood-inbox.md`
    2026-07-04 + 2026-07-06 (original map notes), ADR 0049 (rejected daemon-sessions design).

- **SPA consolidation — one app shell (ADR 0044, post-ship).** Merge the three
  Svelte SPAs (`frontend/{logging,config,logbook}`) into one Vite + Svelte 5 app
  (`frontend/app/`) with a persistent shell — dashboard/status home, **Operate**
  (Phone/CW + FT8 as sibling modes over the shared session log), **Logbook**,
  **Settings** (the config surface; route stays `/config`), and a link out to the
  zero-JS **manual** (which stays separate per
  ADR 0036 — this is **3→1, not 4→1**). Drivers: the FT8/logging seam is wrong
  (FT8 uses logging but isn't logging — they're siblings over one session log);
  plumbing is triplicated and drifting (three `_helpers.ts`; the session-198
  fetch-timeout fix reached only logging's copy); theming/dark-mode is re-authored
  per app. It is the **client-side mirror of ADR 0043**'s per-surface `internal/api`
  split. Design is settled in the ADR; three sub-decisions **endorsed 2026-07-06**
  (History-API real-path routing [provisional] · lean status-home dashboard ·
  config-as-route) — with one open finer point: the **default landing view is an
  operator preference** (a `startup_view` config setting — dashboard / operate→FT8
  / operate→phone / last-used; dashboard stays the default), settled at build. Key
  constraints baked into the ADR: **per-route code-splitting
  is a requirement, not an optimisation** (one bundle now spans all surfaces — the
  7Q8AC link); the **theme system is built first** from logging's tokens as the
  baseline (utility *nomenclature* open to a rationalisation pass during the merge);
  **API endpoint count is unchanged** — usage simplifies (one hydration, one stream
  lifecycle, a natural first consumer for the deferred 0043 `qso.*` events spine) —
  and **resist a bespoke `GET /v1/dashboard` aggregate** (compose existing +
  subscribe, per 0043). **Subsumes** the three _UI cohesion_ items below (shared
  theme layer · UI themes + dark mode · version-in-tab-title): they become work
  *inside* the shell build, not separate cross-SPA passes. **Post-ship — gated
  behind the 7Q8AC release; do NOT open before the ship gate clears.** See
  `docs/decisions/0044-consolidate-operator-spas-into-one-shell.md`.

- **UI consistency across SPAs — shared theme layer.** _(Reframed 2026-07-06 by
  ADR 0044 — see "SPA consolidation" above: once the three SPAs become one app,
  the "lift logging's `@theme` into a file all three import" step is **absorbed**
  and the token-convergence sweep happens *inside* the single-shell migration. The
  measurements + safety-rail below stay accurate and load-bearing for that sweep.)_
  THIS IS THE LOAD-BEARING
  WORK — theming (dark mode / selectable palettes, see "UI themes + dark mode")
  is the *carrot*, not the task; converging on one token layer is what actually
  tidies the CSS, and it pays off even if a theme picker never ships.
  Filed 2026-06-24; sized + sequenced 2026-07-04. The logging SPA already has a
  genuinely good token layer in `frontend/logging/src/styles/app.css` — a Tailwind
  v4 `@theme` block of semantic colours (`surface`, `ink`, `line`, `focus`,
  `invalid`, `vfo-*`) plus a `@layer components` of shared classes (`.btn`,
  `.input-base`, `.toast-*`, `.tab-item`). The mess is that it's **half-adopted**:
  measured 2026-07-04, config = 275 raw palette colour classes / **0** tokens,
  logbook = 98 / **0**, and even logging still bypasses its own tokens ~217 times
  (79 token uses). So a dev moving between SPAs juggles two mental models — that
  inconsistency is most of the "Tailwind feels cumbersome" pain.
  **Safety rail that makes this mechanical:** logging's tokens were deliberately
  defined EQUAL to their raw Tailwind values (`--color-surface: #fff`, `--color-focus`
  = indigo-600, each mapping noted in a comment), so converting `text-indigo-600`
  → `text-focus` is a **visual no-op** — the whole sweep is diff-reviewable, not a
  redesign. **Order (de-risked):** (1) lift logging's `@theme` + `@layer components`
  into a CSS file all three SPAs import (one source of truth); (2) convert config
  onto it (biggest win — 275 raw / 0 tokens, visual no-op); (3) finish logging's
  own conversion (retire its ~217 raw classes) + do logbook. Do NOT big-bang all
  three mid-other-work, and don't lead with a picker UI. Steps 1–3 ARE the tidy-up;
  the theme toggle (see "UI themes + dark mode") is a thin follow-on once colours
  route through variables. Not now — flagged for a dedicated pass.

- **Operator email address — config Station tab field (needs a daemon home).**
  Filed 2026-06-24. Surfaced while building the config-SPA Station tab: there's
  no operator/station email field anywhere today. It **can't ride
  `logging_station`** — that block strictly follows ADIF, and ADIF has no
  `MY_EMAIL`. So a working field needs a small daemon-side home; the leading
  option is a new SM config string **`operator_email`** (served on `/v1/config`
  GET, set via PUT, echoed like the rest), with the input dropped into the
  Station tab's Postal-address section. Rejected reusing `mailer.default_recipient`
  (that's "where session-log emails go," not the operator's contact address —
  don't overload). Related context (operator): **QRZ.com exposes an email address
  and uses it to populate the ADIF `EMAIL` field** — note that ADIF `EMAIL` is the
  *contacted station's* address (already modelled at `types.ContactedStation.Email`
  + filled by QRZ enrichment), distinct from the operator's *own* email this item
  is about. Worth checking, when this lands, whether the operator email should
  also flow anywhere outbound (e.g. forwarder profiles) or stays purely local
  contact info. Deferred — no concrete consumer yet.

- **Outbound UDP telemetry stream (WSJT-X-protocol-compatible).**
  Filed 2026-06-24, prompted by an external query about feeding WSJT-X data into
  Prometheus/Loki/Grafana. Idea: SM **emits** a UDP datagram stream of its FT8
  decodes, QSO-logged events, and rig status, the way WSJT-X's UDP Message
  Protocol does — so the existing ham tooling ecosystem (GridTracker, JTAlert,
  Grafana/Prometheus exporters, the operator-observability stacks people build)
  works against SM as the FT8 engine for free. Turns SM from a walled garden into
  a first-class citizen of the UDP-consuming tooling world; a real interop
  differentiator.
  - **Building blocks already exist:** `internal/events` (the hub) + the SSE
    surface (`/v1/rig/events`, `/v1/ft8/events`, `ft8-logged`, `rig-state`)
    already produce + fan out exactly this data — a UDP emitter is just another
    hub sink. `internal/pskreporter` already proves the outbound-UDP,
    never-block-the-decode-loop pattern (fire-and-forget, I/O off the decode
    goroutine). Architecturally a clean opt-in egress subsystem beside
    bridge/ft8/pskreporter; config-gated, default off.
  - **The decision (wants an ADR when it lands):** emit the **WSJT-X UDP protocol**
    (Qt QDataStream, big-endian, the Status/Decode/QSOLogged schema) for instant
    ecosystem compatibility — vs an SM-native JSON-over-UDP that's trivial to
    build but nothing consumes. Lean WSJT-X-compatible; the whole value is riding
    existing tooling. Cost: implement + MAINTAIN QDataStream encoding against a
    schema that's WSJT-X's to change (a maintenance tail — hence the ADR).
  - **Hard constraints to bake in:** (1) **emit-only** — expose only the OUTBOUND
    subset; the WSJT-X protocol's inbound control side (Reply / HaltTx / Replay)
    is a remote-rig-control surface that collides with the attended-only FT8
    invariant and the existing daemon-owned command path. Telemetry out, never
    control in. (2) **never block** decode/TX — same discipline as PSK Reporter
    (send off the decode path, drop on a full buffer, fail-soft).
  - Meaty subsystem; **later**, after SM is releasable. Closes the loop with the
    external WSJT-X→Grafana request (they could point existing WSJT-X-consuming
    tools straight at SM).

- **UI themes + dark mode (all SPAs).** From dogfood-inbox 2026-06-24; operator
  reaffirmed the selectable-theme angle 2026-07-04. Wants a theme system: a dark
  mode **and** an operator-selectable theme (the operator picks a named palette for
  the UIs). **This entry is the thin follow-on, NOT the work** — the cost lives in
  the token-convergence pass under "UI consistency across SPAs — shared theme
  layer" (colours are inline in every component today: measured 275 raw classes in
  config, 98 in logbook, ~217 still-raw in logging). Once that pass routes colours
  through the shared `@theme` variables, a theme is cheap: a `data-theme` attribute
  plus a second set of variable values (dark mode = one such theme), and the
  operator's selectable theme is just N such sets. Do NOT start here — start with
  the convergence; wiring a toggle before the tokens exist just paints over the
  problem. The theme **choice** is a durable setting → daemon `config.json`, not
  localStorage (per the settings-in-config rule); the FT8 highlight colours already
  live there, so a `display`/`theme` config block is the natural home. The theme
  **picker** belongs on the config SPA's new **`General` tab** (see "Config SPA — a
  `General` tab" below), alongside the other cross-cutting preferences.

- **FT8 occupancy — multiple switchable spectral views (channelised strip +
  waterfall).** Filed from dogfood-inbox 2026-06-25; rationale + direction settled
  2026-06-26. **Decision: provide BOTH the current channelised strip AND a scrolling
  waterfall, switchable** — not one or the other. Today TX-offset selection is a
  per-slot **data** view: `Ft8OccupancyStrip` lays the passband out horizontally with
  busy bands shaded + daemon-vetted clear offsets as clickable markers (deliberately
  *not* a render — CLAUDE.md).

  **Why the waterfall matters (the load-bearing rationale — operator's insight):**
  SM is, as far as we know, the **only FT8 app that channelises the offset**. On the
  air FT8 is *continuous and overlap-tolerant* — stations sit at arbitrary audio
  offsets (6.25 Hz tone spacing, ~50 Hz wide), the decoder processes the whole
  passband, and two close/partially-overlapping signals routinely BOTH decode (strong
  LDPC FEC + Costas sync; a real collision needs them within ~a signal-width *and*
  comparable strength). Channelising a continuous space has two costs: (a) the band
  **looks fuller than it is** (a neighbour that merely touches your nominal window
  shades the channel), and (b) the binary **red "occupied"** manufactures operator
  *guilt* — "why did they come onto my channel" / "am I transmitting on top of
  someone" — when in FT8 sharing offsets is normal, low-stakes, and usually fine. A
  waterfall shows the **continuous truth** and lets the operator exercise *judgment* —
  see real gaps, **straddle** between signals, pick a spot that's "clear enough." That
  is a genuine capability the channel markers can't give (channelising stays the right
  frictionless default for the common "click a green one" case; the waterfall is the
  complementary expert view).

  **Two distinct strands (weigh separately):**
  1. ~~**Soften the occupancy semantics.**~~ **SHIPPED 2026-06-26 as the switchable
     "Spectrum" view.** Rather than alter the channelised strip in place, this landed
     as a **second, switchable presentation** (Channels | Spectrum toggle in
     `Ft8OccupancyPanel`, operating state `ft8State.occupancyView`): the Spectrum view
     (`Ft8OccupancySpectrum.svelte`) shows signals as soft shading at their **true
     continuous positions** (no cells), the daemon clear offsets as ▾/★ ticks at real
     positions (aligned with the Clear Offsets list), **click-anywhere** continuous
     offset pick, and a **graded clear / near / sharing** status (neutral status words,
     no advice — the operator judges) instead of binary red — directly killing the
     false-full + TX-guilt. Pure logic in
     `lib/utils/ft8Spectrum.ts` (`signalProximity`/`offsetFromFraction`, tested). Both
     views write the one `selectedOffset`. The grading is **position-only** (`Ft8Band`
     has no strength — loud-vs-weak needs the waterfall's FFT magnitudes). Docs: `ft8.md`,
     CLAUDE.md FT8 bullet.
  2. **The waterfall itself (rich — the continuous view).** Feasibility assessed
     2026-06-26: **the browser render is NOT the bottleneck** and the "JS redraw is
     slow vs C/Go" worry is misplaced *if* done right — Canvas 2D self-blit scroll
     (`drawImage(canvas,0,1)` + one `putImageData` row), sub-ms/frame, proven by every
     web SDR (WebSDR/KiwiSDR/OpenWebRX) at far higher data rates than FT8's ~3 kHz /
     ~10 fps. **DOM-per-cell would be catastrophic — never do that.** The FFT stays in
     Go (the browser does zero numeric work — it rasterises pre-computed magnitude
     rows). **The real work/cost is daemon-side:** a sub-slot FFT cadence (~10 fps vs
     today's once-per-15s — ~150× more FFTs, still cheap absolute, but the exact trigger
     to revisit PocketFFT for the occupancy/waterfall FFT — memory
     `project_sm_realfft_stays_pure_go`), a streaming push channel (~8 KB/s of quantised
     rows — binary WebSocket cleaner than text SSE), demand-driven (only while the view
     is open), plus scaling/contrast (AGC) + slot-time gridlines for readability.
     **De-risk by spiking the Canvas render with synthetic rows first** (an afternoon,
     no daemon work) before building the FFT-streaming pipeline.
     - **FFT choice — gonum vs PocketFFT (noted 2026-07-09, operator raised).** Don't
       assume CGO/PocketFFT is *needed*: the waterfall's is a **lightweight display FFT**
       (a few-thousand-point real FFT for magnitude bins, ~10–20 fps → tens of µs each,
       <1 ms/s of CPU in gonum) — a different, far lighter workload than the heavy
       *decode* FFT (jt9-style demod that must finish inside the 15 s slot, which is why
       decode already uses PocketFFT). The scary "~150×" is 150× a once-per-15s baseline
       = still only ~10–20 small FFTs/s. **But it's a cheap, reversible call because CGO
       is already in the picture:** the shipped build is CGO + PocketFFT, and **live FT8
       already REQUIRES CGO** (audio capture), so the waterfall is *de-facto CGO-gated*
       already — using PocketFFT for its bins costs nothing new and loses no static-build
       capability (that build has no live FT8 anyway). **Approach:** build the waterfall
       FFT behind the existing `gonum`-default / `pocketfft`-opt-in seam (same as decode),
       **measure at target fps + resolution, switch only if gonum shows in the profile.**
       Don't pre-optimise; the PocketFFT door is open and free if measurement says so.

- **FT8 Spectrum view — colour revision.** Filed 2026-06-26. The Spectrum occupancy
  view (`Ft8OccupancySpectrum.svelte`) shipped with first-pass colours: soft slate
  shading for signals, green/amber/orange-red footprint by proximity (clear/near/
  sharing), indigo/amber ▾/★ offset ticks. Operator wants these revised (palette TBD)
  — likely tighten the proximity ramp + the signal-vs-pick contrast, and reconcile with
  the eventual shared theme layer / dark-mode work (the FT8 highlight colours are
  already operator-configurable daemon config `ft8.display`; consider whether the
  Spectrum palette should join that or stay component-level). Cosmetic; no logic change.
  **Light-mode half (dogfood 2026-07-14, P2):** the **frontend/app** Occupancy pane
  (`Ft8OccupancyStrip` / `Ft8OccupancySpectrum`) colours only read correctly in dark
  mode — the busy/clear cell fills + amber recommendation markers wash out or look
  wrong on the light surface (red-500 / green-700 opacities tuned for the dark canvas).
  Needs a light-mode pass on the cell fills + spectrum tints; fold into this colour
  revision so both SPAs' occupancy palettes are reconciled at once.

- **LSPA → My Station → Location: future POTA fields.** Filed from dogfood-inbox
  2026-06-25, alongside the Location-tab field trim (the trim itself is the
  active phase-2 LSPA cleanup, not backlog). Once the Location tab is reduced to
  Grid Square / Altitude / Lat / Lon, the future addition is **POTA fields**
  (park references — `MY_SIG`/`MY_SIG_INFO` ADIF, or POTA park id). Sibling to
  the already-deferred IOTA/POTA/SOTA bucket (memory
  `project_sm_adif_my_star_buckets`). Not scoped — a placeholder so the "future
  add POTA" intent isn't lost when the trim lands.

- **Config hot-reload — apply changes without a daemon restart (whole-thing review).**
  Filed 2026-06-26. Today most config is **restart-only**: a subsystem binds its config
  at boot, so changing it needs a full `smd` restart (the config SPA shows a
  "restart required" banner). The friction the operator hit: **add a rig, then switch the
  active rig to it** — `rigs` + `default_rig_id` are restart-bound (ADR 0028 Phase 1: rig
  switch = edit `default_rig_id` + restart), so a perfectly routine "I just added my new
  rig, make it active" is a restart. Same for the bridge, forwarders, FT8/PSK/decode-log,
  SMTP. Meanwhile *some* config already applies **live** (My Station identity, `ft8_display`
  prefs — re-read by the SPA after a `/v1/config` PUT). So the ask is a **whole-thing
  review of what can/should hot-reload vs stay restart-only**, then make the high-friction
  ones live — **rig add + active-rig switch first** (re-open the bridge against the new
  active rig without dropping the process; the demand-driven FT8 capture + the bridge
  supervisor already tear down / reopen, so a "reconfigure + re-acquire" path is plausible).
  Context: this is `config.md §11` (hot-reload, previously deferred *to* the config-SPA
  workstream) made concrete by real dogfooding. Open design points: which blocks are
  safe to swap live (rig/audio device re-acquire, serial reopen) vs genuinely need a
  restart (e.g. server listen address); how the SPA signals "applied live" vs "needs
  restart" per block; and the daemon mechanism (a reload/reconfigure entry point per
  subsystem vs a coarse re-init). **Not now — recorded as a whole-area initiative.**

- **Version display in a more permanent place (e.g. tab title) across all SPAs.**
  Filed 2026-06-26 (the second half of the About→config inbox note). The build version
  now lives on the config SPA's General-tab About section, but the operator wants it
  more *ambient* — surfaced consistently across all three SPAs without opening a tab,
  e.g. the browser **tab title** (`<title>Station Manager — Config (2.0.0-…)`), a footer,
  or a header chip. Small per-SPA; do it with the cross-SPA shared-shell / nav work so
  the three stay consistent. Source is `GET /v1/version` (`daemon` field).

- **Logbook SPA — the management surface (beyond browse).** Filed 2026-06-26. The
  logbook SPA's QSO **browse** (selector + cursor-paged read-only table) shipped
  2026-06-26; the richer logbook-management UX — present in the operator's v1 reference
  app (`7Q-Station-Manager.20250823/logbook-app`) and flagged by the logging-vs-logbook
  scope memory — is deferred. Each is its own pass (design the UX first, per the
  design-SPA-UX-before-building rule):
  - ~~**Per-row QSO edit**~~ **SHIPPED 2026-06-27** — an Edit button per row opens a
    modal seeded from the row; Save PATCHes `/v1/qso/{uuid}` and replaces the row in
    place. Form covers the editable fields (date/times, call, freq+band, mode/submode,
    RST sent/rcvd, country, name, grid, comment); the daemon restores immutables,
    re-derives band from freq, and re-validates (a bad edit returns a message, modal
    stays open). ESC cancels, Ctrl/Cmd+Enter saves. `EditQsoModal.svelte` +
    `api/qso.ts` (`patchQso`) + edit orchestration in `logbook.svelte.ts`.
  - ~~**Multi-select (selection mechanism)**~~ **SHIPPED 2026-06-26/27** — first-column
    row checkboxes + a header select-all (indeterminate when partial), selection
    persists across pages (keyed on QSO id), cleared on logbook switch; a "N selected ·
    Clear" indicator. The **bulk ACTIONS** it feeds are still deferred:
    - **Export (selected / all) as ADIF** — reuses the session email-out's server-side
      ADIF rebuild from `{uuids[]}`, or a dedicated export endpoint / download.
    - **Send selected by email** — the session email-out endpoint already takes
      `{to, uuids[]}`; generalise it to an arbitrary selection.
    - **Upload selected to online services** — bulk-enqueue chosen QSOs to forwarders.
      This is the operator-driven **backfill** lever (ADR 0022 enqueues *future* QSOs by
      presence; retrospective upload of an existing log is explicitly this app's job —
      see the forwarder-enqueue memory + the "clear queued-upload backlog" item, its
      inverse).
  - **Per-row edit — more fields (from dogfood-inbox 2026-06-29).** Extend the edit
    form beyond today's set with **notes** (distinct from `comment`) and
    **long-path/short-path (LP/SP)** propagation info.
  - **"Emailed" column (from dogfood-inbox 2026-06-29).** Surface per-QSO whether it's
    been sent via session email-out — mirror the SessionPanel "Emailed" column
    (`SmFwrdByEmail*` in `additional_data`) in the logbook table. (Sent-flag *edits*
    are already noted as future logbook work — memory `project_sm_session_email_sent_status`.)
  - **Bulk email / export as a dialog overlay (from dogfood-inbox 2026-06-29).** When
    the export-selected / email-selected bulk actions (above) land, present them in a
    dialog overlay rather than inline — operator UX preference.
  - **Search / filter** — by callsign / date range / band / mode / country. Needs new
    daemon query params on the QSO-list endpoint (today it's cursor paging only, no
    filters) — a wire + validation + test change, so design it WITH the SPA.
  - **QSL-awaiting view** — filter to QSOs flagged for QSL (e.g. `app_sm_request_qsl` /
    QSL status) for card/label workflows.
  - **Edit-history viewer** — surface the `qso_history` append-only audit table for a
    QSO (who/what/when changed) — read-only forensics.
  - **Logbook management** — create / rename / delete logbooks from the UI (daemon
    endpoints exist: POST / PATCH / DELETE `/v1/logbook`; DELETE refuses a non-empty one).
  Build order for what remains: search/filter → bulk actions (export/email/upload) →
  QSL-awaiting → edit-history → logbook CRUD (edit + the selection mechanism are done).
  The reference app is a UX guide, not a port (it's Wails + page-number paging; SM is
  HTTP + cursor paging + its own utils/tokens).

- **FT8 Call CQ — no operator feedback while waiting for a chosen slot parity.** Filed
  2026-06-28. When **CQ slot** is set to **Even** or **Odd** (not the default **Next**),
  `StartCallCq` forces our CQ parity and deliberately does **not** immediate-fire — the
  first CQ is held until the next slot of the chosen parity (`caller_sequencer.go:63-88`),
  which can be up to ~one extra slot (~15–30 s) after the click. Correct behaviour (it's
  the point of choosing a parity), but the UI gives no sign it's waiting: the button flips
  to *Calling CQ…* immediately while the rig stays silent, so a non-default parity can read
  as "it didn't fire." Enhancement: a transient **"waiting for even/odd slot…"** indicator
  (or a countdown on the Call CQ control) until the first CQ actually keys, then drop to the
  normal calling state. SPA-only — the daemon already publishes the chosen `cq_period`
  (`QsoStatus`) and the first `tx-state {transmitting:true}` marks the real start, so the
  SPA can show "waiting" between StartCallCq and that first TX edge. Surfaces: `Ft8MsgPanel`
  (the Call CQ control), `ft8.svelte.ts`. Low effort; pure clarity. Default **Next** is
  unaffected (it fires on the very next boundary, so there's nothing to wait through).

- **FT8 Monitor/Listen on-off toggle — DISCUSSION POINT (not a committed build).** Filed
  2026-06-27. Today FT8 audio capture is tied to the **FT8 view being open**: `Ft8Panel`'s
  `onMount` calls `startFt8` → subscribes `/v1/ft8/events` → the daemon acquires the mic
  (demand-driven, refcounted, ~5 s linger, CAT-gated). So as long as a tab sits on the FT8
  operating mode — even backgrounded — the daemon holds the microphone and decodes every
  slot. That's correct and WSJT-X-like (its receiver runs while the window is up), and a
  2026-06-27 "why is the mic held?" turned out to be exactly this (an FT8 browser tab open
  alongside a Phone/CW one — benign). The discussion: should capture instead be gated on an
  explicit **Monitor/Listen** control the operator toggles, so the mic engages only when
  they deliberately start listening, not merely because the view is on screen? Points to
  weigh: (a) does Monitor *replace* the view-open trigger or *augment* it (view open but not
  monitoring → no mic, no Band Activity)? (b) interaction with the existing demand-driven
  refcount + CAT gate (Monitor would become the primary gate, subscriber-count secondary);
  (c) what the FT8 view shows when not monitoring (empty Band Activity + a "Listening off"
  state); (d) is Monitor per-tab UI state or daemon-owned (ties into the multi-tab
  operating-lock item — two tabs, who's monitoring?); (e) is the current behaviour actually
  a problem, or fine once the operator understands it (the trigger was a misunderstanding,
  not a fault)? Decide the model before any build. Surfaces if pursued: `Ft8Panel` (gate
  `startFt8`/subscription on a Monitor toggle), `ft8.svelte.ts`, possibly daemon capture
  gating. Related: the multi-tab rig-lock item (Bugs) shares the "which surface is doing
  what" question.

- **FT8 same-session dupe rule — extend to the daemon auto-workers.** Filed 2026-06-27.
  The SPA now blocks working/queuing a **same-session dupe** (a call already logged on the
  current band this session — contest-dupe style) from **Band Activity clicks**
  (`Ft8Panel` `workedThisSession`, gating `answerCq`/`workCaller`/enqueue; greyed rows).
  But the rule only covers operator clicks — the **daemon auto-workers don't honour it**:
  Call-CQ `auto_first` (`caller_sequencer.go` answerer-pick) and the pile-up drain could
  still work a station already logged this session. To make the "no same-session dupes"
  rule airtight, the daemon needs a session-dupe notion too — which is awkward because
  **the session is an SPA concept** (`sessionQsosState`, per-tab), not a daemon one (the
  daemon has no session_id, by design — see the session-scope memory). Options to weigh:
  (a) the SPA passes a "skip these calls" set / a since-timestamp to the daemon on
  `cq/start`; (b) the daemon dedupes against *today's* logbook rows on this band+mode (a
  proxy for "this session" — simpler, but not exactly session-scoped); (c) leave the
  auto-workers as-is and accept that hands-off modes may re-work a session dupe (document
  it). Decide the model before building. Until then, the SPA-click guard is the protection
  and the auto-workers are a known gap (noted in `docs/ft8.md`).

- **Operator log viewer (daemon diagnostics) — DB-manager tab to start.** Surfaced
  2026-06-30 while specifying ADR 0039's "loud startup log line" for the
  disabled-forwarder queue discard. The realisation: a loud `smd.log` line is
  worthless to an operator who won't `tail` a file — and external ops (7Q8AC etc.)
  have *no* window into daemon activity/errors. Distinct from the **live
  operational toasts** that already exist (rig-disconnected, upload-failed, bridge
  errors via SSE in the logging SPA) — this is a viewer for the **structured log
  history + admin-class events that never become toasts** (forwarder discards,
  migrations, the reference.db bootstrap, startup warnings). Decided shape (2026-06-30):
  **a diagnostics surface inside the DB-manager**, not a 5th standalone SPA — same
  admin/troubleshooting audience + cadence as queue-health/DB-health; promote to its
  own SPA later only if it grows teeth (live streaming, heavy filtering, multi-source).
  Daemon side stays narrow: a recent-log endpoint (`GET` over a ring buffer of the
  last N structured lines, or a bounded `smd.log` read), optionally an SSE tail for
  live — no coupling into log/forward internals. Build alongside the DB-manager SPA
  workstream.

- **SPA SSE consolidation — one multiplexed event stream (all SPAs).** The logging
  SPA opens 2-3 long-lived **SSE** streams per tab (`/v1/rig/events`,
  `/v1/ft8/events`, and the `/v1/events` firehose). Browsers cap **~6 connections
  per host** over HTTP/1.1, so several SM tabs each holding these **starve the
  browser** — new connections (a fresh tab, or even the SPA's own fetches) queue
  forever → "Connecting…" / frozen tab. Surfaced 2026-07-02: the (then new-tab)
  cross-SPA nav accumulated a tab per click and hung Firefox after 5-7 clicks.
  **Immediate fix shipped:** cross-SPA nav now navigates **same-tab** (only Manual
  opens a new tab), so only one SPA's SSE set is live at a time — removes the
  auto-accumulation. **Residual risk:** manually opening ~3+ logging tabs can still
  brush the 6-connection limit. **Durable fix:** collapse the per-topic SSE into ONE
  multiplexed stream (e.g. `GET /v1/stream` carrying rig/ft8/qso/bridge events tagged
  by type, SPA fans out client-side) so a tab holds ONE SSE regardless of how many
  event topics it watches — the events hub already multiplexes internally, so this
  is mostly a new combined endpoint + a client demultiplexer. NOT urgent (same-tab
  nav covers normal use); revisit if tab-starvation recurs or before a wider release.

- **FT8: operator-adjustable attempt limit before Next.** **Daemon side SHIPPED
  2026-07-03** — `ft8.tx.max_repeats` is surfaced on `/v1/config` as `ft8_max_repeats`
  ([1–10], resolved default 6) and **applied live** to the running sequencer via
  `Service.SetMaxRepeats`/`Sequencer.SetMaxRepeats` (mutex-guarded, affects an in-flight
  contact on its next slot — no restart). Decided: **logging FT8 Settings tab, live**
  (not the config SPA). Tests: sequencer clamp + service forward/nil-safe (`ft8`),
  GET/PUT/400/omit round-trip (`api`); docs: api-endpoints.md + config.md §11.5 (the one
  live-applied config field). **SPA field still pending.** Filed 2026-06-27 (dogfood),
  triaged 2026-07-03. In a big pile-up, if a station stops hearing you the sequencer
  works the full rung count (daemon-set `maxRepeats`) before the operator's Next can
  advance — wasting slots on a non-responder. The "N calls left" readout is display-only
  today (`Ft8Panel` Working banner, from `ft8State.qso.maxRepeats`, set by the daemon per
  rung — not SPA-editable). Add an operator control to **cap the attempts before
  auto-advancing** (a small numeric field beside Next, or a session default in the FT8
  Settings tab) so a dead contact is dropped sooner. Design points: SPA-only nudge of an
  early-Next threshold vs a daemon `max_repeats` override on `/v1/ft8/qso/*`; per-session
  vs per-QSO. Surfaces: `Ft8MsgPanel` (Next control), `ft8.svelte.ts`, possibly the
  sequencer rung count.
  **Config default + INFINITE option (2026-07-04):** resolved default is **6**, current
  clamp **[1–10]**. Add an **infinite / keep-going** option — never auto-abandon a contact,
  chase until answered or Abandoned (for a rare/weak/fading station worth staying on).
  *Encoding wrinkle:* `0`/absent already means "use default 6", so infinite needs its own
  value (e.g. `-1`, or a `max_repeats: "infinite"` literal) — don't overload `0`. *Caveat:*
  infinite chases a *silent* contact forever, blocking the pile-up on one non-responder — a
  deliberate choice; the operator still has Next/Abandon. NB this is the **contact** off-ramp;
  **CQ itself is already uncapped** by design (`caller_sequencer.go`: "calling CQ is the
  operator's standing intent — keep calling until answered or Abandoned").

- **FT8 Field Day — FD-aware Operate ladder render (+ remaining FD UI). PARKED — blocked until the next Field Day contest.**
  Parked 2026-07-04: the FD UI can only be meaningfully exercised on-air during a Field
  Day contest, so it waits for the next one; ARRL/RAC-only, so it is not a 7Q8AC
  ship-gate item. Filed
  2026-06-28 (dogfood "correct the ladder display for the ARRL FD"), triaged 2026-07-03.
  FD-over-FT8 shipped + on-air-validated 2026-06-28 (ADR 0037, both directions), but the
  **Operate-tab message ladder still renders the standard exchange placeholders**
  (`<DX>`/`<GRID>`/`<RST>`), not the FD class+section exchange — so the ladder is wrong
  for an FD QSO. This is the documented FD remainder set (CLAUDE.md + memory
  `project_sm_ft8_field_day`): (1) the **FD-aware ladder render**; (2) **FD pile-up
  Ctrl-click** (enqueue an FD caller); and (3) the **config-SPA section dropdown**
  (`ft8.field_day.section`, validated by `goft8.ValidARRLFieldDaySection`). SPA-side for
  (1)/(2); config-SPA for (3). The daemon FD path is done — this is presentation/entry.

- **Settings help tooltips + beginner/expert mode (all SPAs).** Filed 2026-07-02
  (dogfood), triaged 2026-07-03. Many FT8 (and other) settings knobs are terse; add
  larger explanatory tooltips/help text, with an operator toggle to switch them off
  (**beginner ↔ expert** mode) so an experienced op isn't nagged. Cross-cutting UI: the
  beginner/expert flag is a durable pref → daemon `config.json` (settings-in-config
  rule), natural home the config-SPA General tab; the tooltip copy lives per component.
  Start with the FT8 Settings tab (densest), extend as friction surfaces. Pairs with the
  shared-theme / cross-SPA-shell work.

- **`actions/rigControl` — shift+ctrl freq-step key parity in FT8 (match phone/CW).**
  From dogfood-inbox 2026-07-03, graduated 2026-07-04. In phone/CW the Shift+Ctrl arrow
  cluster tunes the rig (±100 Hz / ±10 Hz / ±5 kHz band-hop, per CLAUDE.md rig-control);
  the operator wants the same freq-step keys live while an FT8 view is focused. Today those
  bindings are wired for the logging (phone/CW) surface only. Scope: decide whether FT8
  reuses the same `actions/rigControl` handler (routing set_freq/set_freq_b by selected VFO
  as today) and which FT8 focus contexts should capture the keys without clashing with FT8's
  own Shift+Ctrl shortcuts. Small, but needs a keymap-collision check against the FT8 panel.

- **SPA code-review low-severity batch (2026-07-05 review).** The verified LOW
  findings from the same review whose highs/mediums (findings 1–7) shipped
  2026-07-05. Each was confirmed by the slice reviewers; none is ship-gate. The
  one standout (SPA fetch timeouts) was promoted to P1 and SHIPPED 2026-07-05 (see
  backlog-archive). The rest are grouped as the reviewer grouped them; each line
  leads with its surface
  so it's greppable. Batch them in a dedicated cleanup pass (mostly one-liners);
  pull an individual item forward if a related file is already open.

  **Standout still in the batch:**
  - **`TX_PWR` in (0, 0.5) W rounds to the `0` sentinel while passing the `> 0`
    omit-guard (`adif.ts:240`).** Durable outbound ADIF data — a fractional
    QRP power emits a wrong `0`. Fix: round before the omit gate, not after.

  **States (`lib/states/`):**
  - `bridge.svelte.ts:182–193` — `tabCount` not reset on SSE error, so the
    "another tab is operating" banner stays stuck after a daemon restart.
  - `bridge.svelte.ts` (~330) — `closeSource()` doesn't clear
    `catState.freqKnown`, unlike both involuntary-disconnect paths (inconsistent
    reset → a stale freq can read as known after a voluntary close).
  - `ft8.svelte.ts:454` — single-feed mode keeps the prior slot's decodes on a
    silent slot (should clear to reflect "nothing this slot").
  - `ft8Enrich` — `ft8EnrichState.clear()` doesn't invalidate in-flight lookups,
    so a lookup resolving after view-close re-inserts a zombie cache row.

  **FT8 UI:**
  - `Ft8PileupDrawer` — drawer-close can't abort an in-flight drain start; the
    `AbortSignal` param exists but is unused.
  - FD callers advertise "Ctrl+click to add to pile-up" in the tooltip, but
    `enqueueCaller` parses with `parseDirectedToMe` only → silent no-op (FD
    pile-up is listed pending in CLAUDE.md / the parked FD-UI item). Hide the
    affordance until FD pile-up Ctrl-click actually lands.
  - `bearing.ts:105` — bearing rounds *after* normalisation, so 359.97° renders
    `360` instead of `000`. Normalise after rounding.
  - `Ft8Panel.svelte:698` — `isWorking` splits on `' '` where sibling parsers use
    `/\s+/` (inconsistent whitespace handling).
  - `canAnswer` omits `!tx.transmitting` unlike `canSend` (a TX-in-progress guard
    the sibling has).

  **QSO UI:**
  - Edit-overlay mode dropdown renders blank for stored modes outside its static
    9-entry list (QsoPanel's own `modeList` appends the stored value; the overlay
    doesn't). Mirror the append so an out-of-list mode still shows.
  - Session-row fields re-read from live state *after* the `await`, so a CAT push
    mid-POST can skew the Session-tab display. Snapshot before the await.
  - F3 "Stop" re-snaps Time On to now — documented design, but a real data trap;
    worth a confirm or a relabel so it can't silently overwrite a set Time On.

  This whole batch, plus the two standouts, came from the 2026-07-05 SPA review;
  the "Verified sound" list from that review (ADIF byte-length prefixes,
  enrichment-never-blocks invariant, midnight rollover, i18n catalogue, EventSource
  lifecycles, mode.ts submode table) was checked hard and should NOT be re-flagged.

- **Bridge / TX-safety hardening batch (2026-07-05 `internal/bridge` review).** The
  three verified LOW findings from the bridge-subsystem review; its MEDIUM-HIGH (#1,
  stranded-key backstop on a failed key write) + LOW-MED (#2, defensive/teardown
  unkey bypassing cmdMu) + doc nits (#6) shipped the same day. All three below are
  fail-safe-directioned (they fail toward unkey / write-block, never toward a stuck
  carrier), which is why they're LOW despite touching TX code. Verify each still
  applies before fixing — the #1/#2 fixes moved nearby code.
  - **Auto-off retry re-arm can clobber a fresh key's timer** (`tune.go` ~292–302 /
    `ft8tx.go` ~236–246). `tuneAutoOff`/`ft8TxAutoOff` release `keyMu` inside
    `releaseTune`/`releaseFt8Tx`, then take `mu` to re-arm the retry. Interleaving:
    release fails → operator StopTune succeeds → operator StartTune keys anew with a
    fresh timer → the old callback resumes, sees `active=true`, overwrites the timer
    with a 1 s retry → the new tune is cut at ~1 s and the orphaned original timer
    fires a redundant release later. Fail-safe (unkeys early, never strands). Fix: a
    per-key **generation counter** captured at key time and re-checked in the callback
    before re-arming.
  - **A garbled first IDENTITY permanently write-blocks the pipeline instance**
    (`pipeline.go` ~660–696). `identityVerified` latches on the FIRST IDENTITY
    response; if startup serial noise corrupts the ID digits it decodes to "" →
    `identityUnrecognised` → toast + write paths blocked, and because the latch is set
    a later clean matching IDENTITY can never upgrade to confirmed. Recovery needs a
    pipeline restart that may never come on a healthy link. Fail-closed is the right
    default; the fix is to let a later **exact-match** IDENTITY confirm while KEEPING
    the mismatch-halts semantics (only the unrecognised→confirmed upgrade opens up).
    Shipped rigdefs can't produce a false `identityMismatch`, so only the unrecognised
    path is exposed.
  - **`bridge.New` trusts `Serial`/`Cat` non-nil** (`pipeline.go` ~174 derefs
    `*s.cfg.Serial`; every write path derefs `s.cfg.Cat`). Safe today because
    `config.ActiveBridge()` always populates both, but nothing in the package enforces
    it — a future caller/test passing `Enabled: true` with raw config panics inside the
    supervisor goroutine and kills the daemon. Fix: a two-line nil check in
    `Initialize()` (which today only checks the logger) to make the invariant local.
  - Also noted (not a fix, just a documented window): a slow/wedged tab evicted by the
    hub keeps the other tabs' `rig-clients` count stale until its handler goroutine
    hits the 10 s SSE write deadline and its deferred unsub broadcasts — bounded and
    advisory.

- **migration 0004: timestamp `localtime`→UTC + normalise pre-fix debris rows — AUTHORED
  2026-07-05, ONE STAGED-REVIEW BUG CAUGHT + FIXED, awaiting operator review + deploy.** Files:
  `migrations/log/0004_utc_timestamps.{up,down}.sql`; test `TestMigrate0004_NormalisesTimestampsToUTC`
  (seeds all FOUR formats → asserts canonical UTC). Full `internal/database` suite green under
  `-race` (incl. the down path via `TestMigrate_DownRestoresRSTLengthConstraint`, step count bumped
  −2→−3). **HIGH bug caught in staged review (2026-07-05) BEFORE deploy — the reason the staged
  gate exists:** the normalisation CASE had only two arms (`… +00:00` keep / else −2h), but
  **PRE-fix** sqlboiler stored UTC `created_at`/`deleted_at` via Go's `time.Time.String()` as
  `'… +0000 UTC'` (UTC-correct, but not `+00:00`), so the −2h arm would have shifted every pre-fix
  `created_at` (≈ every QSO since April) 2 h WRONG. Fixed with a **third CASE arm**
  `WHEN v LIKE '% +0000 UTC%' THEN datetime(substr(v,1,19))` (reformat, no shift — first 19 chars
  are already UTC wall time), applied to all six normalised columns + a `seed(4)` regression case.
  The down path stays coherent (post-up rows are naive-UTC; its `+2 hours` arm round-trips).
  **Empirical scoping:** `boil` defaults to **UTC**, so sqlboiler (`created_at`/`deleted_at`) and
  the Go writers (`modified_at`) store UTC POST-fix; only the SQL **DEFAULTs**
  (`qso_upload.created_at`, `qso_history.at`) + the two **triggers** stamped local, and PRE-fix
  sqlboiler UTC wore the `+0000 UTC` skin (the caught bug). The migration rebuilds the three tables
  with `datetime('now')` (UTC) defaults + UTC triggers, normalising every value during the copy.
  **Still pending YOUR go-ahead to deploy** — eyeball the SQL against the live DB first; it
  auto-runs on the next `smd` start once committed. **Deploy safety (reviewer advice):** golang-migrate
  gives no rollback net, so **back up the DB first** (a `VACUUM INTO` like the bootstrap split does)
  and **spot-check a known QSO's `created_at` against its `qso_date`/`time_on` after** it runs.
  Original staged spec follows for reference. The **fix-forward half shipped 2026-07-05** (`_time_format=sqlite` on `getDsn`/`bootstrapDSN` + `time.Now().UTC()`
  on the 10 `null.Time` DATETIME writers → every *Go-written* stamp is now SQLite-canonical UTC;
  `TestModifiedAt_StoredCanonicalUTC` locks it). This migration is the **staged half** — deliberately
  separate so it can be eyeballed against the live dogfood DB before it runs. Do **before SM Cloud
  S3** (reconcile diffs `qso.modified_at` across hosts; the Postgres store canonicalises to µs UTC).
  Empirically verified against modernc v1.48.1 (scratch probes):
  - **Trigger/default change:** `datetime('now','localtime')` → `datetime('now')` (UTC) on
    `qso.created_at` + `qso_history.at` defaults and the `trg_qso_set_modified_at` /
    `trg_qso_upload_set_updated_at` trigger bodies. SQLite can't alter a default/trigger in place →
    **table rebuild**, same pattern migrations 0002/0003 already use (FK re-point, trigger + index
    recreate, `qso_history` append-only guards).
  - **Normalise existing rows (per-format — single-TZ CAT assumption):**
    - *Naive-local strings* (`YYYY-MM-DD HH:MM:SS`, no offset/`T`/`Z` — trigger/default writes):
      written as CAT local, scanned back as UTC → **2 h off**. Shift to UTC (`datetime(col,'-2 hours')`
      for the fixed +02:00 CAT), format-gated so only these rows are touched.
    - *Go monotonic-debris strings* (`… +0200 CAT m=+…` — pre-fix `time.Time.String()`): offset-aware
      so **correct-instant** but `datetime()`-unparseable; strip to canonical UTC (offset embedded, so
      no shift — just reformat). Pure-SQL string surgery is fiddly; a Go-side migration step may be
      cleaner. Bounded set (only pre-fix updates).
    - *Already-canonical* (`…+00:00`, post-fix): leave.
  - **Decision to confirm before authoring:** the −2 h shift assumes ALL historical naive-local rows
    were written in CAT — correct for the single-operator dogfood DB; **confirm that's the only DB with
    pre-fix data** (7Q8AC hasn't onboarded). If any row was written elsewhere the blanket shift is wrong.
  - **Test:** seed a table with one row of each format, run the migration, assert all rows end
    canonical UTC + correct instant (extend `review_findings_test.go` / a migration test).
  - NB after the fix-forward but before this migration, `created_at` + trigger-written `modified_at`
    stay naive-local (2 h off) while Go-written `modified_at` is clean UTC — a known, harmless-today
    interim (ordering works on the prefix; nothing external reads these yet).

- **`internal/database` review low-severity batch (2026-07-05).** Verified LOW findings from the
  same review; none ship-gate, none touch the reconcile invariant (that's finding 1, above).
  - **Cold-insert race → retry instead of erroring** (`api_context.go:1311-1368`, `writeContactedStation`):
    two concurrent enrichment writes for the same callsign can both miss the fetch and both insert; the
    loser hits the UNIQUE index and the error propagates. Enrichment is fail-soft so logging is never
    blocked, but `IsUniqueConstraintError` exists exactly for this — catch it and retry as the
    update-merge branch (turns a spurious warning into correct behaviour). Read-merge-write is also
    last-write-wins under concurrency — acceptable for a cache, noted.
  - **Bootstrap split-detection can leave a stale table** (`bootstrap.go:56-61`): the "already split?"
    check keys on the `country` table alone, but the drops run `country` then `contacted_station`; a
    crash between them leaves a stale `contacted_station` in the log DB forever (data safe — already
    copied to reference.db). Detect on *either* table.
  - **Nits:** `FetchAllLogbooksWithContext` fails the whole list on one bad adapter row while every QSO
    list path warns-and-skips — pick one policy · `MarkSessionEmailedWithContext` builds an unbounded
    `IN (?)` list (fine at session scale; guard-comment before anything import-sized feeds it) ·
    `getDsn`/`bootstrapDSN` interpolate the file path raw into `file:%s?…` — a path with `?`/`#` breaks
    the DSN, one `url.PathEscape` away · `DeleteLogbookByIDWithContext` check-then-act window (concurrent
    QSO submit between Exists and soft-delete orphans a QSO under a deleted logbook — negligible single-op)
    · `Ping` sleeps 25 ms once more after its final failed attempt (cosmetic).

- **`internal/api` review low-severity batch (2026-07-05).** Verified LOW findings; the
  MEDIUM (PUT /v1/config lost-update race between the two SPAs) shipped 2026-07-05 (see
  backlog-archive). None below is ship-gate.
  - ~~**Disabled-subsystem routes return misleading statuses** (`server.go` / `spaHandler`).~~
    **FIXED 2026-07-05.** A `/v1/*` path reaching the SPA catch-all (disabled bridge/FT8, or a
    typo) now returns an honest 404 instead of 200-HTML/405; and a real directory (`/assets/`)
    SPA-falls-through to index.html instead of an `http.FileServer` listing (the disclosure nit,
    closed by the same change). `spaHandler` `/v1/`-guard + `f.Stat().IsDir()` rewrite; tests
    `TestSpaHandler_ApiPathReturns404` + `TestSpaHandler_DirectoryServesIndexNotListing`.
  - **Nits:**
    - Negative server-limit config panics at startup: `Normalize` defaults only `== 0`, so
      `max_concurrent_requests: -1` reaches `make(chan struct{}, -1)` in `newLoadLimiter` → panic
      (`limits.go:47`). A validation error would be kinder than a loud crash.
    - Credential-clear semantics differ across masked surfaces: **forwarder** creds treat an
      empty-string value as overwrite-with-empty (clearable), while **SMTP/lookup** passwords
      treat blank as keep (not clearable via the API). Each is locally documented, but the
      asymmetry invites a confused bug report — unify, or document side-by-side in api.md.
    - ~~`spaHandler` serves **directory listings** for real embed-FS dirs~~ **FIXED 2026-07-05**
      (same change — a directory hit now SPA-falls-through to index.html).
    - Stale comment: `middleware.go:206-216` (Unwrap) still says the bridge SSE handler "clears
      its write deadline at stream open" — it arms per-write deadlines now. Doc drift only.

- **`internal/qsoservice` review nits (2026-07-05).** The review's two LOW findings +
  the #3 pinning test were FIXED 2026-07-05 (audit `before_image` now marshalled before
  the merge so a `contact_history` body can't taint it via the shared `ContactHistory`
  backing array — protects the SM Cloud sync input, ADR 0016; `EnqueueUploads` doc
  corrected to describe the actual per-TYPE ADIF-stamp check; `TestUpdate_RestoresAllForwarderStamps`
  reflects over `types.Qso` stamp tags and pins the immutable-restore list against drift).
  None ship-gating. Residual nits:
  - **`uuid_conflict` classification unreachable under `force`** (`submit.go:322`): the guard
    is `IsUniqueConstraintError(err) && !force`, so a forced re-import of an exported log (UUID
    collision, random dedupe key) aborts with a generic error instead of the per-record
    `uuid_conflict` report. Unreachable today (batch path hardwires `force=false`,
    `SubmitImport` has no non-test caller) but a trap for whoever wires a `--force` import.
    Dropping `&& !force` is safe.
  - **`importBatchFallback` publishes Hub events** (via `s.submit`), contradicting
    `SubmitImportBatch`'s "does NOT publish" doc — harmless (import runs daemon-offline) but the
    doc should flag the fallback as the exception.
  - **Best-effort `contacted_station` cache warm-up uses the request ctx** — a client that
    disconnects right after commit skips the cache write. Deliberately best-effort; a detached
    short-timeout ctx (like the dedupe refetch already uses) would make it client-independent.

- **FT8 work-a-caller / Resume opening — prefer a clean next-slot start over a truncated
  immediate fire.** From dogfood-inbox 2026-07-05 ("abandon while TX → Resume immediately
  starts TX — too late into the slot?"), triaged + **confirmed WAI (not a bug)**: the immediate
  TX is gated by `fireOpening` (`sequencer.go:805-816`) — it keys immediately ONLY within the
  first `txLateWindowSec` (4.5 s) of an our-parity slot, where ADR 0032 truncate-don't-shift keeps
  the signal decodable; past 4.5 s (or in the partner's parity) it defers to the next slot. So it
  never actually fires "too late." **The enhancement:** `fireOpening` is SHARED by the answer-a-CQ
  path (which legitimately NEEDS the truncated immediate fire — a DXpedition moves on if you're a
  slot late) and the **work-a-caller / pile-up-drain** path (`StartWorkCaller` → `fireOpening`,
  reached on Resume). But a station you're *working* is calling YOU and will keep calling — there
  is no tight reply window — so a truncated opening (up to ~36 % of symbols dropped at 4.5 s) is
  arguably worse than just waiting one slot for a clean, full-length start. Consider gating the
  **opening rung on the work path** (`seqWorking`, and maybe the caller-side too) to skip the
  immediate-truncate and wait for the next clean our-parity `OnSlot`, while leaving the answer-a-CQ
  opening's immediate fire unchanged. `fireOpening` is per-mode already (the `switch s.mode`), so
  the differentiation is local; decide whether the caller-side Call-CQ opening wants the same
  treatment. Purely a TX-quality nicety — the current behaviour is correct and on-air-validated,
  just not optimal for the no-time-pressure work case.

- **Re-enrich a logged QSO — in the logbook SPA (BUMPED 2026-07-05; next session).**
  From dogfood-inbox 2026-07-04, **bumped out of P2/P3 parking** after a second flaky-link
  occurrence: **there is no UI way to re-run enrichment on an already-logged QSO**, so a name
  dropped by a QRZ timeout at log time stays dropped with no backfill path. **Justification
  (why it's more than a nicety):** this is the recurring 7Q8AC/Malawi operating condition —
  on a flaky link, QRZ lookups intermittently time out during logging, and while the
  "enrichment never blocks logging" invariant holds (the QSO logs fine) + the QRZ resilience
  recovers per-lookup (no permanent disable, session-198 work), a few QSOs slip through
  nameless each bad-internet session. Measured 2026-07-05: **3/52 nameless** (RG6S, R2BNC,
  SP9SOF) during the 13:00–14:00 UTC timeout window; a 128-QSO FT8 pile-up earlier lost more.
  Hand-editing each is the only fix today. **Decision (operator, 2026-07-05): implement in the
  LOGBOOK SPA** (the QSO management surface — where you go to fix up historical rows), not just
  the logging SPA's session-tab edit overlay. **Approach:** in the logbook SPA's per-row edit
  modal (`EditQsoModal` already exists), add a **"Re-enrich"** action that calls
  `/v1/enrich/callsign` for the row's call and merges the result (name, QTH, country, grid,
  DXCC/zones) into the editable fields, which the operator then saves via the existing
  `patchQso` PATCH. Progressive + fail-soft (a re-enrich that times out changes nothing).
  Reuses the exact endpoint the new-QSO `Callsign` component uses; no daemon change. Consider
  a bulk "re-enrich selected" once the logbook multi-select bulk-actions land (folds into
  "Logbook SPA — the management surface"). **Companion doc task (below).**

- **Manual FAQ: "Why is a QSO logged without a name, and how do I fix it?" (operator, 2026-07-05).**
  Document the name-missing cause + remedy in the operator manual (Hugo, ADR 0036) as an FAQ /
  troubleshooting entry. **Cause:** on a poor internet link the QRZ callsign lookup can time out
  at the moment you log — SM deliberately logs the QSO anyway (enrichment must never block
  logging) rather than making you wait or lose the contact, so the name/QTH just aren't filled
  in for that one. It is NOT a lost QSO and NOT a credentials problem; the lookup service
  recovers on its own for the next contacts. **Remedy:** re-run enrichment on the row once the
  link is back (the logbook-SPA "Re-enrich" action above), or hand-edit the name. Pairs with the
  re-enrich feature — write the FAQ so its "remedy" references that button once it ships (until
  then: hand-edit). Keep it operator-plain (no "QRZ session key / i/o timeout" jargon).

## Scope notes (NOT backlog — recorded so they aren't mistaken for it)

- **FT8 automatic / unattended sequencing is OUT OF SCOPE and unsupported** — the
  QEX FT8 specification forbids automatic operation and it is licence-restricted
  in many jurisdictions. SM is attended-only. This is not a roadmap item.

- **"Design our own sequencing/timing" — future thinking (flagged 2026-06-12).**
  Operator wants to revisit, later, whether SM grows its own sequencing/timing
  design rather than mirroring WSJT-X's. Hard constraint to carry into that
  conversation: anything on the air as *FT8* must stay protocol-interoperable —
  the Costas sync, 15 s cadence, and 0.5 s nominal start are protocol, not SM
  choices; a genuinely new mode would need its own Costas arrays (per the QEX
  licence restriction on non-conforming streaming) and would not be "FT8". So the
  open design space is SM's own sequencer *architecture / policy / UX*, not the
  on-air timing of standard FT8. No action now — recorded so it isn't lost.
