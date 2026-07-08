# Station Manager — Session Handoff

**Purpose:** rolling handoff document across Claude sessions. Captures what was
done in the previous session, where the repo currently is, and **what the next
session should pick up**. Read this first when starting a session — it exists
precisely so we don't re-derive state or redo finished work.

**How to use this document:**

- **At session start:** read top-to-bottom. The "Current state" section tells
  you where the repo is. The "Next steps" section tells you what to do. If the
  next session's goals have already been set, work from them.
- **At session end:** the assistant updates this document before stopping.
  Move anything in "Next steps" that was completed into "What happened this
  session" with a date. Leave anything unfinished in "Next steps" and add new
  items discovered during the session.
- **Rolling window (enforced):** keep about the **last ~12 sessions** of
  `### Session N` entries live. When the list grows past **~15**, move the
  oldest block down into [`session-handoff-archive.md`](session-handoff-archive.md)
  (newest-first, verbatim) so this doc — read top-to-bottom every session start —
  stays lean. The archive is the grep-able convenience copy; the authoritative
  long-form record is git history + the v1-analysis docs + the memory files.
  (Prior policy said "2–3 sessions" but was never enforced — the doc reached 197
  entries / ~1 MB before the first roll-off on 2026-06-14. ~12 keeps the current
  multi-session arcs intact without the bloat.)
- **Durable facts go in memory files,** not here. This document is for
  transitory session-to-session state. If something is stable across all
  future sessions (a project invariant, a user preference, a design rule),
  capture it in a memory file under `~/.claude/projects/.../memory/`.

---

## Current state (as of 2026-07-08)

> **Session 209 (2026-07-08, same day) — `frontend/app` Operate polish +
> the draggable/pinnable tile-layout decision (ADR 0046 + POC).** Reuse-first
> ([[sm-reuse-dogfood-spas-first]]) throughout.
>
> - **Contact overlay replaces the Details card.** The always-on Details panel
>   + its rail glyph are **gone** (`DetailsPanel.svelte` deleted, `'details'`
>   off the `Panel` type). A new **`ContactDialog.svelte`** overlay opens from
>   the **Worked** card's **View…** button — enabled only when a QSO is underway
>   (`qsoClock.started`). It shows ONLY what isn't already on the logging card /
>   header (no repeats): **QRZ page link, email, CQ/ITU zone** (read-only
>   enrichment) + **Gridsquare, QTH, Rig, Notes** (operator fields). **View-only
>   by default; a pencil Edit glyph** unlocks the operator fields (grid included
>   — its only editing home now, off the fast path). Plumbing: `notes` + `rig`
>   added to `QsoDraft`; the submit sink now emits ADIF `RIG`/`NOTES`/`CQZ`/`ITUZ`;
>   `Enrichment` gained `email`/`cqZone`/`ituZone` (mapped in `apiEnrich` from
>   `station.email` + `country.cq_zone`/`itu_zone`). check/lint/336 tests green.
> - **Smaller Operate tweaks:** favicon stroke thinned (150→130, `public/logo.svg`
>   only — in-app `Logo.svelte` untouched); Worked-card **View…** button; Worked
>   panel auto-opens on Tab (QSO start) if nothing's open; the unconfirmed-CAT
>   Log-QSO tooltip now states the block ("Cannot log yet — confirm…").
> - **Tile-layout feature DECIDED (not built): [ADR 0046](decisions/0046-operate-tile-layout.md).**
>   Resolves ADR 0044's deferred content-model fork toward a **tiling** model —
>   fixed-size tiles, **no overlap** (reflow), non-destructive Default, **single
>   global pin** (not per-card), cards unchanged in size/content, CardFrame chrome
>   only in an explicit arrange mode, rail → show/hide-a-tile. Validated by a
>   pointer-drag **POC at `docs/v2-design/tile-layout-poc/`** (interaction only —
>   no persistence, no Svelte). Design guards recorded so per-op-profile
>   persistence is later *wiring*, not a refactor. Deferred to lift time: one
>   shared arrangement vs per-Operate-sub-mode; adaptive column count.
> - **Tile-layout LIFTED into `frontend/app` + browser-validated (same session).**
>   New `layout.svelte.ts` (serialisable layout value — ordered tile ids per
>   column + hidden set; actions; **injected persistence seam** `setLayoutPersistence`
>   wired in `main.ts` to localStorage `sm.layout.default.phone`; tile registry),
>   `CardFrame.svelte` (drag grip, arrange-mode only), `TileBoard.svelte`
>   (Pointer-Events drag engine, live reorder, no overlap), `ArrangeBar.svelte`
>   (global Pin/Unpin · Reset · Done — **fixed near the window bottom**). The
>   single-slot `panel` model is gone: `worked.svelte` auto-open drives tile
>   visibility, `Header` rig chip shows the Rig tile, `UtilRail` toggles tile
>   show/hide + Arrange, the three panels are self-contained tiles (own header +
>   action + an **always-visible X to hide**, logging excluded), `InfoPanel.svelte`
>   deleted, `app.css` swapped `operate-center` for the board. **Decisions:**
>   Default = logging only, info tiles **stack BELOW logging** (its column) on
>   show — faithful to today; **centred on a shared axis** (`align-items:center`);
>   cards keep exact size (info tiles `w-2xl`); board centres when it fits, else
>   left-aligns + `<main>` scrolls; FT8 untouched (own tile surface = FT8 pass).
>   Guards honoured (data-not-coords, state-driven, injected persistence,
>   profile-keyed). `layout.svelte.test.ts` (5) locks show/hide + reset + pin;
>   check/lint/**341** tests/build green. Known: cross-column drag of the logging
>   tile briefly remounts it (Svelte each-block boundary) → refocuses callsign;
>   harmless in arrange.
> - **Two dogfood fixes (frontend/app):** (1) Tab-to-start a QSO now **warns via
>   toast** when the rig gate is unconfirmed/lost (clock still starts; explains
>   the disabled Log button); (2) each info-card's **X refocuses the callsign
>   input** (`hideTile`→`focusCallsign`). check/lint/341 green.
> - **Inbox TRIAGED (2026-07-08).** Everything untriaged dispositioned; only #73
>   (name-overflow, in progress) left plain. → `backlog.md`: **P1** stale
>   `TestVersion_HappyPath` (schema 3→4); **P2** new _Daemon/data_ line — ADIF
>   export omits `MY_*` (investigate, possible data-loss) · fill `country.dxcc`
>   number · enrich abort-WARN→debug · RST-validator backport to logging SPA ·
>   "Rig"→"Rig Control" rename with rig-control; **P3** MY_RIG follow-CAT ·
>   single-source freq→band · tune-carrier occupancy (pending HW) · world map ·
>   FT8 band-hop · voice-keyer/copilot · movable nav. **Struck as scope-notes**
>   (decided, not backlog): DX-cluster (don't build) · MQTT (P4 only) · app-name
>   (keep). Draggable-card notes marked IMPLEMENTED.
> - **Rig-panel polish + band/freq consistency (frontend/app).** Header rig-chip
>   tooltip is gate-aware + terse ("Waiting for confirmation" when unconfirmed).
>   RigPanel: **Confirm** moved to the card's bottom-right with the message
>   **above** it; message reworded to "Logging is blocked until the **QSO
>   settings** are confirmed" (band/mode/freq — changing band doesn't move the
>   freq). **#1 default freq per band (CAT-off):** picking a band jumps the freq
>   to a representative centre (`BAND_DEFAULT_HZ`, editable) so band+freq can't
>   silently disagree; pairs with the existing `syncBand` (freq→band).
>   **#2 region-AGNOSTIC out-of-band flag:** freq input red-outlines + "Outside
>   the {band} band" when the freq isn't in the selected band's ADIF envelope
>   (`frequencyToBand`, no region data) — catches the 40m/14.2 mismatch + out-of-
>   any-band typos; the message sits in its OWN row below the inputs so it can't
>   break the `items-end` alignment. **Explicitly NOT built:** region-aware
>   TX-legality (needs IARU region + national band plan → the backlogged
>   band-plan item). check/lint/341 green.
> - **Code-review fixes (pasted review; fixed 1–3, skipped 4).** **#1 (High):**
>   `LoggingCard.windowKeydown` now guards `operate.exportOpen` (Esc closes it,
>   log/clear inert) and `operate.pileup` (inert; `PileupDrawer` owns its Esc) —
>   was clearing the draft / logging behind an open Export modal or pile-up.
>   **#2 (Med):** RigPanel gained a `bandOptions` derived (mirrors `modeOptions`)
>   so a CAT/`frequencyToBand` VHF-UHF band (2m/70cm…) joins the `<select>`
>   instead of rendering blank. **#3 (Med):** `apiHistory` drops non-object rows
>   + rows without a unique non-empty uuid before mapping (Svelte keyed-each
>   safety) — new test + a fixed unrealistic fixture. **#4 (Low, storage-guard
>   wrapper) skipped** (can't bite this operator; widest change).
> - **Worked-before list gained a Notes column** (operator added the header): `notes`
>   plumbed through `WorkedQso` + `toWorkedQso` (`row.notes`), rendered truncated
>   with a title tooltip. check/lint/**342** green. Inbox note (not acted): the
>   contact-detail overlay (from the Worked panel) needs re-organising.
>
> **Session 208 (2026-07-08, same day) — session export/email + session timer
> for `frontend/app`; a hard reuse-first lesson.** The operator drove a
> **reuse-the-dogfood-SPAs-first** principle (now memory
> [[sm-reuse-dogfood-spas-first]]): the frontend/app work is primarily
> restyling battle-tested behaviour — study `frontend/logging` + the daemon
> for the existing mechanism BEFORE building.
>
> - **Session export/email (Export dialog, off the Session card).** An
>   **Export…** button in the InfoPanel header (Session panel only) opens a
>   modal (same pattern as DuplicateDialog) with **Download ADIF** + **Send
>   to QSL manager** — decided over an on-card control or a rail submenu
>   (header-action keeps the log table clean; the rail stays uniform).
>   **Email** = `session-email.ts` ported verbatim from logging → the
>   existing `POST /v1/session/email`.
> - **Reuse lesson (cost me two corrections):** I first built a
>   **client-side ADIF exporter** — WRONG. It came out sparse because the
>   daemon back-fills the record at submit (MY_* block, DXCC, zones,
>   lat/lon); the client's pre-submit copy can't carry that. The dogfood
>   SPAs never build ADIF client-side — they send UUIDs and the daemon
>   rebuilds (that's what produces the rich `exports/sent-adif/` archives).
>   Fix: **NEW daemon `POST /v1/session/export`** — reuses the *existing*
>   `adif.ComposeToAdifString` + `archiveSessionAdif` (so a download also
>   backs up to `exports/sent-adif/`, operator ask); the email + export
>   handlers now share `Server.fetchSessionQsos` (honest DRY, 2 real
>   consumers; email tests confirm the refactor). Returns `application/x-adif`
>   attachment. SPA "Download ADIF" streams it; the client-side
>   `adifExport.ts` + `session.adif` stash + `markEmailed`/`emailedDate` were
>   **deleted**. **QSL column removed** from the Session card (operator).
>   Archive-filename collision (email vs export share
>   `session-YYYYMMDD-HHMMSS.adi`) assessed as harmless — second-precision +
>   identical content on a same-session collision; optional `export-` prefix
>   noted, not done.
> - **Session timer ported** (`SessionTimer.svelte` verbatim from logging —
>   sessionStorage `sm.session.startedAt`, survives F5, resets on tab close,
>   per-tab) + `formatDurationHms` (+ tests) into a lean `utils/time.ts`.
>   Placed **leading-left in the Header** (opposite the trailing rig chip —
>   the two always-visible ambient readouts); app-wide (operator choice).
>   The logging SPA put it in its card because that SPA had no shell; the new
>   app's header is the better home.
> - **Operator polish (post-build tweaks):** InfoPanel card width
>   `w-[42rem]` → **`w-2xl`** (Tailwind v4 `--container-2xl` = 42rem —
>   equivalent, named token; verified in built CSS, centring calc unchanged;
>   comment updated to name `w-2xl`) + Export button `-mt-3` nudge; session
>   timer bumped to **`text-lg`** with the header glyph at **`size-5`**.
> - 336 SPA tests + 4 new Go handler tests green. Two inbox notes filed
>   (client-aborted enrich WARN noise; stale `TestVersion_HappyPath` schema
>   3-vs-4). `api-endpoints.md` updated for the export route.
> - **COMMIT STATE: committed through `ae894b9d` (session export/email +
>   timer + duplicate modal all landed); UNCOMMITTED = the operator polish
>   tweaks (`InfoPanel.svelte` w-2xl + comment + button nudge,
>   `Header.svelte` glyph size, `SessionTimer.svelte` text-lg) and this
>   handoff.** Dev daemon RESTARTED for the export route (go-run builds need
>   a restart for daemon changes — SPA hot-reloads, daemon doesn't); still
>   the DEV daemon (`build/` working dir), QA DB intact.

> **Session 207 (2026-07-08, same day) — clean-DB live QA run of the
> `frontend/app/` logging flow on the FTdx10.** Parked the QSO DB in
> `build/db/backups/pre-qa-20260708/`, bootstrapped a fresh one (daemon
> self-seeded the default logbook — "config marked setup complete but DB had
> no row"), ran real QSOs and checked daemon log + DB row each time. **The
> clean dev daemon runs in the background under this session** (`task
> run:smd`, `2.0.0-alpha.1-532`), reading its own stdout + `build/log/smd.log`.
>
> - **Verified correct end-to-end:** USB QSO stores `mode=SSB submode=USB`;
>   CW QSO stores bare `mode=CW` (no submode) with **599/599** — the
>   rig-driven `rstDefaultFor` proven on the wire; duplicate flow is
>   minute-precision dedupe (same call/band/mode/freq/minute) → `200`
>   refusal → force sends a **random nonce dedupe key** (daemon
>   `submit.go:243`) so the forced row coexists under the unique constraint.
>   Same call across SSB/CW = two rows (mode is in the key), correct.
> - **3 SPA fixes this run:** (1) `observeWorked` now gates on
>   `isValidCallsign` like enrich — malformed partials were 400ing the
>   daemon (fail-soft hid it); (2) Rig-panel rail click hands focus back to
>   the callsign field when CAT is `connected` (fields locked = read-only),
>   keeps focus CAT-off/lost; (3) **the duplicate refusal is now a centred
>   modal + backdrop** (`DuplicateDialog.svelte`, TWP modal-dialogs ref) —
>   the off-centre anchored popover is gone. Esc dismisses the DIALOG only
>   (LoggingCard's window handler intercepts + returns before clear-draft);
>   Ctrl+Enter inert while open (no blind force on key-repeat); Cancel is
>   the focused safe default; backdrop-click cancels; draft preserved.
>   `dismissDuplicate()` added. 323 SPA tests green.
> - **2 daemon items INBOXED (not fixed — daemon out of this SPA's scope):**
>   (a) client-aborted enrichments log at WARN with full error chains
>   (`context canceled` from superseded /v1/enrich aborts) — noise that
>   masks real warns, fix = debug-level when cause is ctx.Canceled;
>   (b) MY_RIG design Q — stored blob carried `Yaesu FT-710` (stale config)
>   while the FTdx10 was on CAT; should rig-identity win when connected?
> - **Operator to-do noticed:** `build/config.json` `my_rig` is stale
>   (FT-710) — edit + restart for correct QA rows. Parked QA DB restorable
>   from `build/db/backups/pre-qa-20260708/`.
>
> **COMMIT STATE: committed through `8c8e23ef` (InfoPanel centring);
> UNCOMMITTED = the duplicate-modal round (`DuplicateDialog.svelte` +
> test / `qso.svelte.ts` + tests / `LoggingCard.svelte`), the Worked gate
> (`worked.svelte.ts`), the Rig focus tweak (`UtilRail.svelte`), the two
> inbox notes (`dogfood-inbox.md`), operator's own `SessionPanel.svelte` +
> `WorkedPanel.svelte` edits (SessionPanel has prettier drift to clean),
> and this handoff.**

> **Session 206 (2026-07-08) — `frontend/app/` RIG SSE SEAM LIVE + validated on
> the FTdx10: the Operate surface now has NO stub seams left.** Closed session
> 205's NEXT item:
>
> - **The SSE transport + CAT-link state machine.** `lib/api/rig-sse.ts` (new)
>   is a thin EventSource wrapper for `/v1/rig/events` — parse-guards each
>   event, hands payloads to injected handlers (ADR 0045; `tune-state` /
>   `rig-clients` deliberately not consumed yet). The transitions live in
>   `rig.svelte.ts` as `catLink`, pure + transport-free: partial-payload VFO
>   merge → freq/band; rig mode literal → operator-friendly form via the
>   injected `bridge.mode_mappings` table (`submode||mode`; unmapped passes
>   raw and the panel's select grows the odd value); the shipping **800 ms
>   flash suppression** (`rig-disconnected` only schedules the
>   connected→lost flip; a rig-state inside the window cancels silently);
>   transport error flips a live link immediately. **A never-connected stream
>   stays `off`** — the daemon's replayed `rig_no_data` on subscribe (rig-off
>   day) means manual logging, not a blocked form. `goManual()` = the interim
>   stand-in for the ADR 0044 confirm flow (on `lost`, take ownership, keep
>   last values; a returning rig auto-lifts). `bridge-error` shows raw
>   code+details under the pill (no i18n catalogue in this SPA yet).
>   `StationContext` gained `catEnabled` + narrowed `modeMappings`
>   (`/v1/config` bridge block); `main.ts` opens the stream only when CAT is
>   enabled (config fetched once at boot — re-wiring on config change comes
>   with this SPA's config surface). **The Rig panel's dev sim select is
>   deleted — no stub surface remains anywhere.**
> - **LIVE PASS on the FTdx10 (dev daemon, port 8080), all green:** freq
>   tracks the dial; `DATA-U`→**FT8** (mode_mappings resolution, which also
>   flips the report validators); VFO-B tracks; rig-off → pill red within
>   ~6 s (5 s liveness + suppression), Log blocked, **auto-lift** to green on
>   power-up with no operator action (wire capture confirmed the daemon side:
>   `rig-disconnected` on power-off, full INIT replay 65 s later). Idle
>   flicker-watch implicitly good (long connected stretches, no churn) —
>   watch during dogfooding; `FLASH_SUPPRESS_MS` is the knob.
> - **Frequency display converged on the SM dot-grouped convention** after
>   two operator catches (`14.19995` → `14.199.950`): ported
>   `validators/frequency.ts` **verbatim + tests** from shipping (parses
>   dot-grouped AND decimal MHz unambiguously to Hz, 100 kHz–30 GHz);
>   `rig.freq` is always dot-grouped when rig-fed (Go-manual continuity reads
>   like the rig); **every `parseFloat(rig.freq)` is gone** (submit sink +
>   band-sync via `parseFrequency` — parseFloat reads the grouped form as
>   14.199); ADIF `TX_FREQ` is now exact Hz, not a float round-trip.
> - **Panel polish (operator-driven):** CAT-locked view shows **both VFOs**
>   as dot-grouped read-outs with a selection dot (read-only by design —
>   click-to-swap needs the ADR 0026 command path, lands with rig control);
>   manual/lost keeps the single freq entry (no VFO concept by hand). Header
>   now reads **"Rig · FTdx10 · ● CAT connected"** (`rig.identity` from the
>   wire, kept across a loss; pill moved from the panel body to InfoPanel's
>   header). Icon swaps: Operate parent nav = the broadcast-arcs glyph (was
>   Rig's), Rig rail = arrows-right-left (operator-supplied), Sidebar's
>   shadowed operate branch synced. Rename "Rig"→"Rig Control" agreed for
>   WHEN control ops land (inbox note names the two touch points).
> - **`rstDefaultFor` SHIPPED (the thrice-parked item):** CW→`599`, else
>   `59` (shipping parity incl. the '59'-passes-SNR-pattern quirk). A
>   module-level fill effect overwrites both report fields ONLY when the
>   default changes — it tracks a memoized `$derived`, not `rig.mode`, so
>   USB↔LSB↔FT8 hops never clobber a typed report; only a CW↔voice boundary
>   crossing rewrites (deliberate clobber, shipping tradeoff, documented
>   in-code). `clearDraft` resets to the CURRENT mode default. Shipping's
>   no-empty-refill lesson preserved. **Live-verified: CW-U on the rig flips
>   the form to 599/599.** The rig SSE made this urgent — mode changes under
>   the operator now.
> - **Confirm-once-per-band CAT gate SHIPPED + operator-tested (ADR 0044's
>   full design target — the first-pass `off|connected|lost` gate is
>   superseded):** `rigGate()` = `live` / `manual` (off + THIS band
>   confirmed) / `unconfirmed` (off, not asserted — blocks) / `lost`
>   (blocks). Enforced in `logDraft` itself, not only the buttons (the
>   Log-anyway bypass lesson). Decisions: **single-slot confirm memory**
>   (any band change re-arms, including returns — simpler + safer than a
>   session set); **Go manual merged into Confirm** (on `lost` the button is
>   "Go manual — confirm": ownership + assertion in one act); **values
>   persist, confirmation doesn't** (band/mode/freq → localStorage
>   `sm.rig.context` for the fast next-session prefill; the confirm is a
>   per-session assertion by design). Surface: **header chip** live
>   (always-visible `freq · mode · band` + status dot, click → Rig panel —
>   the Header's "lands with the operating surface" placeholder is
>   fulfilled); InfoPanel pill gained amber "Manual — confirm to log";
>   entering Operate with a blocked gate **auto-opens the Rig panel** (only
>   if no panel is open — never a takeover; mid-session gate changes = the
>   deferred rail badge). Known softness, noted: on boot with CAT up
>   there's a sub-second `unconfirmed` window before the first rig-state,
>   so the Rig panel auto-opens then the gate lifts green — harmless,
>   fix-if-annoying (delay the auto-open check).
> - 292 SPA tests green (was 234 at session start: +CAT-link machine,
>   +transport, +bridge-block parsing, +frequency validator's 27, +RST
>   defaults, +gate).
> - **TOAST SYSTEM + system-message routing (operator: the in-card
>   "Logged…" message reflowed the Clear/Log buttons — find messages a
>   better home):** ported `toasts.svelte.ts` + tests VERBATIM from shipping
>   (info/warn/error, TTL 4/6/8 s, ttl=0 sticky, max stack 5) into
>   `lib/ui/`; NEW `Toasts.svelte` renderer styled to the operator-picked
>   **Tailwind Plus overlays/notifications/01-simple** reference (adapted to
>   @theme tokens per the TWP licence rule — surface panel, leading level
>   icon green-check/amber-triangle/red-x, sr-only severity prefix,
>   per-toast live-region roles), single-mounted in App.svelte.
>   **Bottom-centre placement** (operator choice after a
>   placement-conventions discussion; shipping's top-centre rationale noted
>   as the fallback if toasts get missed). Routing: success + non-duplicate
>   refusals = toasts; the **duplicate refusal stays card-local** (its "Log
>   anyway" action belongs beside the button) as an absolutely-positioned
>   anchored popover — **nothing reflows the card anymore**. `submitState`
>   slimmed to `{busy, error, duplicate}` (error = duplicate-only now).
> - **"No toast seen" bug → Vite HMR lesson (durable, now in memory):**
>   module-level `$state` singletons duplicate across HMR generations — the
>   submit path pushed into one toast-queue instance while the mounted
>   renderer read another; hard reload fixed it. Proven code-side via a NEW
>   component test (`Toasts.svelte.test.ts`: render + push + assert DOM) +
>   an `Element.animate` stub in `src/test/setup.ts` (jsdom lacks the Web
>   Animations API that `transition:fade` needs). Rule: hard-reload before
>   diagnosing cross-module state bugs; trust a component test over the
>   hot-swapped tab.
> - **Keyboard fast path (operator-specified):** **Ctrl+Enter** = log —
>   `logDraft` now returns a stored/refused boolean, and focus returns to
>   the callsign field ONLY on success (a refusal leaves the cursor where
>   the operator is fixing); **Esc** = clear + focus callsign (also
>   dismisses the duplicate popover via the submitState reset); the Clear
>   button click refocuses too; **the card mount-focuses the callsign
>   input** (covers app load, view switches, FT8→Phone/CW). Listener is
>   `<svelte:window>` INSIDE LoggingCard, so the shortcuts live and die
>   with the card (Phone/CW only). Tooltips advertise them (Log =
>   "Ctrl+Enter" when not gate-blocked, Clear = "Esc").
> - **Rig-panel auto-open on a blocked gate REMOVED** (operator: annoying —
>   the header chip already shows the state and click-opens the panel; the
>   chip serves ADR 0044's intent without moving panels under the operator;
>   why-comment left in Operate.svelte so it doesn't get re-added). Also
>   kills the boot-window flash from the known-softness list. "Logged" toast
>   tick dropped (icon carries success). 311 SPA tests green.
> - **Panel polish (operator) + focus hand-back seam:** operator gave
>   Session/Worked matching fixed-height bodies (`h-55` via a `tableHeight`
>   const, all empty/pending states same height — no more panel-height
>   jumps), switched both to `overflow-y-auto`, and applied the review
>   catch (SessionPanel's `<thead>` now `bg-surface sticky top-0 z-10`
>   like Worked's, so the header holds while a long session scrolls).
>   NEW **focus hand-back seam** (`operate/state.svelte.ts`
>   `registerCallsignInput`/`focusCallsign`): clicking **Worked/Session**
>   on the rail (open OR close — deliberate acts on read-only panels)
>   returns focus to the callsign field; **Details/Rig keep focus** (opened
>   to use their own inputs); the Worked panel's AUTO-open on a lookup hit
>   deliberately does NOT touch focus (it fires mid-typing and would steal
>   the cursor). LoggingCard registers its input on mount / unregisters on
>   unmount — the rail never reaches into another component's DOM
>   (ADR 0045).
> - **RST validators tightened to the actual scale (operator catch: 77 and
>   000 passed in USB):** the ported shape-only pattern (`[0-9]{2,3}`)
>   became scale-aware — R 1–5, S 1–9, T 1–9, zero invalid in every
>   position — AND mode-aware in digit count after the operator ruling
>   **"tone is only relevant to CW"**: `isValidRst` (CW — tone optional,
>   59 and 599 both fine) + NEW `isValidRs` (voice/RTTY/PSK31 — exactly two
>   digits, so 599-on-USB is malformed) + the unchanged signed-dB validator
>   for the WSJT-X family; `draftProblems` picks by rig mode. Lines up with
>   `rstDefaultFor` (RTTY/PSK31 already defaulted to 59). 320 SPA tests
>   green. **Backport note filed in the inbox** — shipping still carries
>   the loose pattern (low urgency: entry-error protection, daemon is
>   presence-only either way).
> - **InfoPanel card centred on the logging card's axis (operator ask):**
>   `.operate-center` anchors children to the logging card's LEFT edge (the
>   stationary-card centring mechanism), so the wider 42rem info card stuck
>   out to the right. Fix: a computed negative side margin —
>   `ml-[calc((var(--card-w,42rem)-42rem)/2)]` (−57 px at the 558 px card
>   width) — so it overhangs symmetrically; the `var()` fallback makes it a
>   no-op outside operate-center (FT8 branch). Two noted consequences:
>   Details rides along (one card, per-panel centring would jolt on tab
>   switch), and at cramped widths the 57 px left overhang can clip beyond
>   the scroll origin (the container's left stop protects the logging card;
>   rail auto-collapse largely prevents reaching it). The 42rem appears in
>   both `w-[42rem]` and the margin calc — keep in step (in-code comment).
> - **Narrow-rail Operate flyout stuck-open FIXED (operator catch):** the
>   flyout is CSS `:hover`/`:focus-within`-driven, so a click never closed
>   it — pointer still hovering AND the clicked button keeps focus. Fix in
>   `OperateNav.svelte` + `app.css`: a `flyout-suppressed` class set on any
>   click (parent or flyout item), CSS override placed AFTER the show rules
>   (wins by source order at equal specificity), cleared on mouseleave to
>   re-arm hover — PLUS a mouse-click blur on the parent (`e.detail > 0`
>   distinguishes mouse from keyboard activation), because retained focus
>   re-opened the flyout via `:focus-within` the moment mouseleave cleared
>   the suppression. Keyboard activation keeps focus (a11y); known limit
>   noted in-code: a keyboard user's suppression only re-arms on a pointer
>   move.
>
> **NEXT (frontend/app):** **rig control** (ADR 0026 ops: VFO
> click-to-swap, band step, set_mode — brings the "Rig Control" rename
> from the inbox), then the **FT8 surface**.
> **COMMIT STATE: committed through `cc1ac6d7` (RST validators);
> UNCOMMITTED = the InfoPanel centring (`InfoPanel.svelte`) and this
> handoff.** The local daemon is still the DEV daemon (`build/` working
> dir).

> **Session 205 (2026-07-07, same day) — `frontend/app/` Log-QSO side FINISHED:
> enrichment lookup gate + the mode.ts port + mode-aware report validation.**
> Closed out session 204's first NEXT item and two follow-ons:
>
> - **Enrichment lookup gate (flaky-link motivation):** `observeCall` now
>   clears/idles on any input failing `isValidCallsign`, so malformed partials
>   never reach the daemon (or onward to QRZ). Known floor, noted in-code: the
>   validator (deliberately mirroring the daemon's) passes any ≥3-char
>   letter+digit string, so a partial like `DL3` is structurally valid — only
>   the 400 ms debounce suppresses those. Per-character behaviour verified:
>   fast typing = exactly ONE lookup (for the final call); slow typing lets
>   partials through, where the country layer prefix-matches the cache (232
>   rows → ~always a hit) but the station layer's exact-match miss runs the
>   QRZ chain (not-found, uncached by design); SPA abort propagates to the
>   handler ctx so a superseded partial's upstream call is cancelled.
>   `DEBOUNCE_MS` 400→~700 is the next knob if live monitoring warrants.
> - **`utils/mode.ts` PORTED verbatim + tests** (SUBMODE_TO_MODE /
>   `resolveModeAndSubmode` / `usesSignalReport`); **Rig panel dropdown = the
>   shipping nine** (USB LSB CW FM AM RTTY FT8 FT4 PSK31 — sidebands, not
>   families; default USB); the `main.ts` sink resolves before building ADIF,
>   so **USB now logs MODE=SSB SUBMODE=USB** (was bare SSB, sideband lost).
>   `adif.ts` already had subMode support. Session rows keep the operator
>   literal (USB) — more informative at a glance.
> - **Mode-aware report validation wired** (`draftProblems` picks
>   `isValidSignalReport` vs `isValidRst` by `usesSignalReport(rig.mode)`), so
>   manual FT8/FT4 entry with signed-dB reports (−12/+04) isn't a dead form.
>   One-way `qso → rig` sibling import (mode drives validation, never enters
>   the draft). **Integration tests added at the operator's request**
>   (`qso.svelte.test.ts`, 7 tests over the REAL state modules + the injected
>   submit seam — this path is never driven by hand, so the tests are its only
>   routine exercise). Rune modules test fine (svelte plugin is in the vitest
>   config). 214 tests green.
> - **Verified while wiring (both operator questions):** (1) the daemon has
>   NO RST shape validation — presence-only for non-FT8 + the `59` default
>   fill; the SPA gate is the ONLY report-shape defence. Asymmetry noted (not
>   filed): daemon keys on literal `Mode=="FT8"` while the SPA's
>   SIGNAL_REPORT_MODES is broader (FT4/JT*/JS8…). (2) the **FT8 Field-Day
>   path is untouched** — `BuildQso` sets RST_SENT=SNR / RST_RCVD empty, the
>   e4 sink back-fills `ft8.field_day.default_rst_rcvd`, and `Submit` skips
>   both RST checks for FT8; this session touched zero Go files and the FT8
>   flow never passes through a SPA form.
> - **Deliberately NOT ported:** the shipping mode-appropriate RST default
>   fill (CW→599 + the default-tracking effects) — belongs with the rig-SSE
>   work. Operator hand-tuned card layout + fixed the `accent-[--color-focus]`
>   v3-shorthand (v4 gotcha strikes again — spotted in review, he fixed).
> - **HISTORY SEAM LIVE (`GET /v1/contact-history`).** Ported
>   `api/contact-history.ts` verbatim + its 14 tests (incl. the
>   malformed-200 guard); `seams.ts` gained `apiHistory` + the pure
>   wire→display mappers (`20260508`→`2026-05-08`, `1430`/`143045`→`14:30`,
>   unrecognised values pass through — odd beats invisible) + `seams.test.ts`.
>   Fail-soft: any non-ok outcome = empty history (worked-before is a
>   convenience, never a gate). `HistoryFn` now takes an **AbortSignal**
>   (same inflight-AbortController discipline as enrich). **The dev-stub
>   layer is GONE** — `historyStub.ts` + `lib/dev/` deleted; the last stub
>   surface is the Rig panel's sim select (goes with the rig SSE).
>   Live-verified against the dev daemon (port 8080, `SM_WORKING_DIR=build/`
>   — its own DB, so production calls come back empty; dev-DB test calls:
>   M0CMC, 7Q7EB, 9Q1AAA…).
> - **WorkedPanel polish (operator) + a real crash fix:** operator added the
>   scrollable body + sticky header + fixed column widths; review caught the
>   each-key (`date+timeOn+band+mode`) colliding on **force-logged
>   duplicates** — exactly what "Log anyway" creates — and Svelte 5 THROWS on
>   duplicate keys. `WorkedQso` now carries **`uuid`** (mapped from the wire)
>   and the panel keys on it.
> - **Config-secrets assessment DECIDED (operator question after a config
>   dump exposed the SMTP app password into the session transcript):**
>   encryption-at-rest **REJECTED** — key-beside-ciphertext is obfuscation;
>   the accepted posture is plaintext + enforced 0600 (WriteJSON tightens,
>   never loosens) + API redaction (`password_set`, GET never carries values
>   — verified live for smtp AND lookup providers) + revocable app passwords
>   + FDE for the stolen-machine case. `internal/config/doc.go` now records
>   the assessment (was "deferred pending a security assessment"); upgrade
>   path if multi-user hosting ever appears = opt-in systemd LoadCredential.
>   **Operator action item: rotate the exposed Fastmail app password.**
> - **Review-findings round (operator ran a review, pasted 3 findings; fixed
>   2, skipped 1 deliberately):** (1) the duplicate **"Log anyway" button now
>   carries the same CAT gate** (`!rigReady() || busy` + stale-CAT tooltip) as
>   the primary Log button — it went straight to `logDraft(true)`, so a CAT
>   drop between the duplicate refusal and the retry click could log stale rig
>   context (academic until the rig SSE lands; fixed now so the SSE inherits a
>   correct gate). (2) **invalid gridsquare is now omitted at the submit sink**
>   (`main.ts`: emit only when `isValidMaidenhead` passes) — NOT blocked at
>   `canLog()`, because enrichment writes the grid into the draft and gating
>   Log on it would let a bad upstream value stop logging (invariant); the
>   DetailsPanel warning stays the operator-facing signal. (3) blank-RST →
>   daemon back-fills 59: **skipped, working as designed** — blanks only exist
>   if the operator clears the '59' default, and the daemon presence-default
>   is the decided posture (same as shipping + what FD relies on). 234 tests
>   green, check + lint clean.
>
> **NEXT: the rig SSE** — the last seam: `/v1/rig/events` → rig state (replace
> the sim select; shipping `bridge.svelte.ts` is the reference, incl. ~800 ms
> flash suppression), the CAT-live `bridge.mode_mappings` literal-resolution
> path from `/v1/config`, the fuller ADR 0044 confirm-per-band CAT gate, and
> the rstDefaultFor machinery (CW→599 defaults). Then the FT8 surface.
> **COMMIT STATE: committed through `3bd9872b` (review-fixes round —
> everything above is in, incl. `internal/config/doc.go`); UNCOMMITTED =
> this handoff only.** The local daemon is still the DEV daemon.

> **Session 204 (2026-07-07) — `frontend/app/` Operate surface is now
> INTERACTION-COMPLETE on stubbed seams.** Continued the ADR 0044/0045 build from
> session 203 (post-ship, additive, ship gate unchanged). Built, in order:
>
> - **Enrichment card (the session-203 NEXT item).** `lib/operate/enrich.svelte.ts`
>   = state + **injected `setEnricher` seam** (mirrors `setSubmit`; dev stub now,
>   `/v1/enrich/callsign` later), 400 ms debounce, min 3 chars, **monotonic seq
>   token** (slow lookup can't overwrite a newer call), fail-soft (error = done-
>   with-nothing). On resolve it **back-fills the draft**: `gridsquare` AND `name`
>   (guarded: call still matches + field empty — operator entry always wins).
>   `EnrichmentCard.svelte` renders flag / country / DXCC + **NEW badge** /
>   **SP–LP radio group** (choice in `enrich.prefs.path` — shared state, sticky
>   across lookups, the future **rotator** reads the same selection) / bearing ·
>   distance for the selected path / **"Their time"** destination clock
>   (longitude-derived solar offset from their grid, 15°/hr — approximation by
>   design, noted in-code; 15 s ticker gated on data showing). Op name + grid
>   were **removed from the display** (name lives on the card, grid in Details) —
>   the data still flows. Hosted in the logging card's right square; positionless
>   per ADR 0045 (radio `name` namespaced via `$props.id()`).
> - **Utils ported verbatim from `frontend/logging` WITH their tests** (39 pass):
>   `utils/bearing.ts` (pathInfo — battle-tested v1 port), `utils/flag.ts`
>   (ccode→emoji), `validators/maidenhead.ts`.
> - **Worked panel.** `worked.svelte.ts` (same seam/debounce/seq discipline;
>   `setHistory` stub) + `WorkedPanel.svelte` (Date/Time/Band/Mode/Sent/Rcvd/Name
>   table). **Auto-open** on history hit — only if no panel is open, once per
>   call; **auto-close** only if it auto-opened (manual opens stay). The
>   observation lives in **LoggingCard** (must run while the panel is closed).
> - **Session panel + THE SUBMIT SINK WIRED.** `session.svelte.ts` (in-memory by
>   design — daemon owns the durable log; later fed from POST response +
>   `ft8-logged` SSE) + `SessionPanel.svelte` (Time/Call/**Band**/**Mode**/RST/
>   Name/Country — Band+Mode added because **FT8 + Phone/CW share one session
>   list**). `main.ts` `setSubmit` composes draft + displayed enrichment
>   (country only if `enrich.call` matches — a fast log can outrun the debounce)
>   + **rig context merged at log time** — the same composition-at-the-wiring-
>   layer shape as the daemon's e4 sink.
> - **`rig.svelte.ts`** — rig context (band/mode/freq) EXCLUDED from the draft by
>   design, merged at log; **CAT gate** `off|connected|lost`: off = manual entry
>   trusted (audio-only fine), **lost blocks logging** (stale context), fields
>   lock when connected. `RigPanel.svelte` = Band/Mode/Freq + status pill + a
>   **dev sim select** (stands in for the bridge SSE; removed at wiring). The
>   ADR 0044 confirm-once-per-band gate flow is the target at SSE-wiring time.
> - **Details panel.** QTH + Gridsquare over the draft (grid enrichment-filled,
>   corrected here; live Maidenhead validation, non-blocking). Deliberately
>   minimal — operator: "features and functions later" (Rig panel likewise).
> - **Tailwind v4 gotcha (recorded for reuse):** `w-[--card-w]` (v3 shorthand)
>   silently compiles to invalid `width:--card-w` in v4 — **`w-(--card-w)`
>   parens is the v4 shorthand**; square brackets only with full `var(...)`.
>   Was the root cause of the "card lost its width" mystery.
> - **Drag/pin chrome decision** (agreed, inbox `[2026-07-07]` note): NO baked-in
>   titlebars — a uniform **wrapper frame** (CardFrame-style) supplied by the
>   layout layer, revealed in an explicit **arrange-layout mode**; cards
>   contribute only a display name.
> - **DOC DEBT CLEARED:** ADR 0044 got a **2026-07-07 build-reconciliation
>   amendment** (rail = all Operate; 64rem floor removed → main-scroll-container
>   model; CAT-gate first pass vs confirm-per-band target; ADR 0045 cross-ref).
>
> **Second half of the session — the `/v1` WIRING ARC is substantially DONE**
> (operator switched the local daemon to the DEV daemon for safe write-testing):
>
> - **Enrichment LIVE**: ported `lib/api/{_helpers,enrichment}.ts` verbatim (+
>   `_helpers` tests) from frontend/logging; `lib/api/seams.ts` adapts wire →
>   card shape. Live-test surfaced fixes: **retract-writes** (enrichment
>   remembers what it wrote into the draft — name/grid/qth — and retracts on
>   call-change ONLY if the field still holds its value, so a previous
>   station's name can't leak into the next QSO and operator entry always
>   survives); **QTH mapped + back-filled**; **DXCC falls back to
>   `country.dxcc_prefix`** (numeric `station.dxcc` is absent on most cache
>   hits).
> - **External review round fixed 4 findings:** (1) `stampOff` is
>   **fill-if-empty per field** (manual off times survive submit); (2)
>   `clearDraft` restamps/disarms so a post-Clear QSO can't log blank times +
>   `canLog` requires dateOn/timeOn; (3) the "GBR flags" finding was
>   **INVERTED** — live-verified hamnut emits **alpha-2** (232/232 cached
>   country rows), so `api.md`'s example + the hamnut test fixture were fixed
>   GBR→GB (daemon test green), flag helper untouched; (4) **AbortSignal
>   threaded** through the enrich seam (superseded lookups cancel the upstream
>   request, not just the UI write).
> - **Submit LIVE** — `POST /v1/qso`: ported `utils/adif.ts` (+74 tests) +
>   `api/qso.ts`; `fetchStationContext()` (grid/callsigns/default logbook from
>   /v1/config); the `main.ts` sink composes draft + rig + enrichment →
>   `formatAdifRecord` (incl. **ANT_AZ/ANT_PATH** from the card's bearing +
>   SP/LP choice; numeric-only DXCC) → submit. **`logDraft` is async**: only a
>   stored QSO clears the card (draft PRESERVED on refusal, message shown);
>   busy latch (in-flight POST disables the button — a click-handler bug where
>   the MouseEvent landed in the `force` param and would have force-logged
>   EVERY click was caught + fixed); session row only on confirmed store, now
>   carries the daemon **UUID**. **Verified end-to-end WITHOUT pollution** via
>   a duplicate-echo POST of an existing QSO (full daemon pipeline → 200
>   duplicate, row count unchanged).
> - **Log-QSO tidy pass:** green "Logged CALL ✓" line (`--color-logged`
>   token); ported `validators/{callsign,rst}` + `utils/frequency` (+tests) —
>   malformed fields get red outlines (`.input-error`) and block `canLog`;
>   **duplicate → "Log anyway"** (force=1) affordance; **band follows
>   frequency** in the Rig panel (IARU table).
> - **QSO clock (operator's flow):** the QSO starts on **Tab out of the
>   callsign field** (not card-mount) — stamps Date/Time On, populates
>   Date/Time Off and **ticks Time Off every second** (the visible QSO
>   timer; midnight rollover handled). Hand-editing an off field **stops the
>   ticker** (correction survives); typo-fix re-Tab can't reset TIME_ON;
>   Clear disarms; before first Tab, Log is blocked (blank times).
> - **Modes/bands investigation** (operator question): shipping SPA gets modes
>   from `/v1/config` `bridge.rig_modes` (CAT-live literals) + a hardcoded
>   CAT-off list + the `SUBMODE_TO_MODE` mirror in `utils/mode.ts`; bands are
>   never fetched — derived from freq via the hardcoded ADIF-envelope table.
>   **Inbox notes filed:** (a) DXCC enrichment fill — CORRECTED: the
>   prefix→number table **already exists** (`internal/enums/dxcc`), the gap is
>   only the enrichment handler not filling `country.dxcc`; (b) band data —
>   single-source the 3 hand-synced band tables daemon-side + the operator's
>   design catch that **regional/jurisdiction band plans** are a different
>   dataset the future operating aids (band-edge warnings, FT8 band-hop
>   legality) must be designed against.
>
> **NEXT (agreed, next session): finish the Log-QSO side** — align the Rig
> panel's base mode list with the shipping nine + port `utils/mode.ts` so
> `USB` logs as `SSB/USB` (today it submits bare `SSB` and loses the
> sideband); then the remaining seams: **history** (Worked panel — still the
> dev stub) and the **rig SSE** (`/v1/rig/events` → rig state + the fuller
> ADR 0044 confirm-per-band CAT gate); then the FT8 surface. **COMMIT STATE:
> everything through the QSO clock is COMMITTED (`880ebdad`); uncommitted =
> the 2 dogfood-inbox notes + this handoff/memory update.** NOTE: the local
> daemon is currently the DEV daemon (operator switched for write-testing).

> **Session 203 (2026-07-06) — started BUILDING ADR 0044's consolidated SPA at
> `frontend/app/`** (Svelte 5 + Vite, dev port 5176). Post-ship, **additive** — a
> new dir that does not touch the shipping `frontend/logging/`; **ship gate
> unchanged.** Everything is frontend-first with STUBS (no `/v1` wiring yet). Built
> this session, in order:
>
> - **Scaffold + shell chrome:** `Sidebar` (collapsible left nav, brand + `Logo`,
>   theme toggle), `Header` (empty placeholder), `App` composer. Theme
>   (`data-theme`) + nav collapse (`data-nav`) reflected onto `<html>` via `$effect`,
>   persisted (`sm-theme`/`sm-nav`), with an anti-FOUC inline script in `index.html`.
>   State in `lib/ui/state.svelte.ts`.
> - **History-API client router** (`lib/router.svelte.ts`): views
>   dashboard/operate/logbook/config; **Operate has sub-routes** `/operate/phone` +
>   `/operate/ft8` (`setMode`, persisted `sm-op-mode`, bare `/operate` normalises);
>   `popstate` sync; SPA index-fallback (Vite + daemon `spaHandler`). Routes are
>   client-side view-switching only — data comes from `/v1/*` later.
> - **Expandable Operate nav** (`lib/ui/OperateNav.svelte`): Phone/CW + FT8 inline
>   sub-items in the full rail, **hover flyout** in the narrow rail.
> - **Right util rail** (`lib/operate/UtilRail.svelte`): Worked/Session/Details/Rig
>   + Pile-up + Collapse; shown across **ALL of Operate (both modes)**; collapsible
>   full↔narrow (`sm-util`, `data-util`). `InfoPanel` (card-below, placeholder),
>   `PileupDrawer` (docked, **emerges from the rail's inner edge**, tucks behind rail
>   when closed → no flash; shadow only when open). Panel/pileup state in
>   `lib/operate/state.svelte.ts`.
> - **Responsive Operate layout:** (1) effective-vs-preference **auto-collapse**
>   (`matchMedia` util<72rem, nav<61rem; the saved preference is never clobbered,
>   restores on widen); (2) `<main>` is now the **horizontal-scroll container**
>   bounded by the rail offsets, so the fixed rails can **never overlap the logging
>   card** — it scrolls within the region (**replaced** the `min-width:64rem` page
>   floor); (3) **stationary logging card** anchored to the viewport
>   (`margin-left: max(0px, 50vw − var(--card-w)/2 − var(--sidebar-w))`).
> - **Logging card = the spine (ADR 0045 first real demo).** `lib/operate/qso.svelte.ts`
>   holds the shared `draft` (callsign, rstSent/rcvd, name, qth, gridsquare,
>   dateOn/timeOn/dateOff/timeOff, comment) + `resetDraft`/`canLog`/`stampOn`/`stampOff`
>   + an **injected submit seam** (`setSubmit` → `logDraft`, mirrors the daemon's
>   `SetQsoLogger`). `lib/operate/LoggingCard.svelte` = self-contained fast-path card
>   (callsign/RST · date/time on+off · name · comment), **two-column** with a blank
>   grayed **enrichment square** on the right, **golden-ratio** width
>   (`--card-w: 573px` = φ × 354px height, defined on `.operate-center`). **QTH + grid
>   are NOT on the card** — grid is enrichment-filled, QTH is Details-card (rarely
>   touched); `onMount` stamps on-time, log stamps off-time.
> - **ADR 0045 written (Accepted):** "Frontend component architecture — decoupled,
>   relocatable by construction" — the client-side parallel to ADR 0043; 5 principles
>   (presentation≠state, data-in-nothing-reached-out, backend injected/subscribed, no
>   positioning assumptions, DRY-not-generic). Forcing function = the draggable-cards
>   idea.
> - **`docs/dogfood-inbox.md` notes added:** draggable/pinnable cards (+ the DRY
>   architecture driver), rotator control (enrichment SP/LP heading → future daemon
>   subsystem, blocked on kit purchase), operator-profiles contesting lens.
>
> **NEXT:** build the **enrichment** into the square — an `EnrichmentStrip`/`Card`
> reading `draft.callsign` + a stubbed `enrich()` seam → flag / DXCC + **NEW?** /
> **bearing SP-LP** / distance (bearing kept as a first-class value for the future
> rotator button), decoupled per ADR 0045. Then Worked (reads `draft.callsign`,
> auto-open), Session (receives logged QSOs via the sink), Details (QTH/grid/comment
> + enrichment detail), Rig panel + **CAT gate**. Wire real `/v1` + SSE after the
> stubbed interactions are proven.
>
> **DOC DEBT (do when next in the docs):** update the **ADR 0044** 2026-07-06
> amendment — the rail is now **all of Operate** (not "Phone/CW-scoped"), the
> `min-width:64rem` floor was **removed** (superseded by the `<main>` scroll
> container), and niggle **#2b** (card-never-overlapped) is **implemented +
> generalised** to the rails; add the ADR 0045 cross-ref.
>
> **COMMIT STATE:** the shell chrome + Operate flyout were committed mid-session by
> the operator; **everything from the RH util rail onward (responsive layout,
> logging card + draft spine, ADR 0045, inbox notes, this handoff) is UNCOMMITTED**
> for his review.

> **Design note (session 202, 2026-07-06):** designed the **post-ship** SPA
> consolidation — **ADR 0044** (merge logging/config/logbook into one Svelte
> shell; manual stays zero-JS per ADR 0036; client-side mirror of ADR 0043)
> drafted `Proposed`, with a P2 backlog entry gated behind the 7Q8AC ship and
> sub-decisions endorsed (History-API routing · lean status-home dashboard +
> `startup_view` pref · config-as-route). Docs-only, uncommitted; ship gate
> unchanged.
>
> **Recent arc (session 201, 2026-07-05):** the review-hardening arc continued
> through three more packages, and the timestamp migration went live. **(1)
> `internal/database` timestamps:** the fix-forward (`_time_format=sqlite` +
> `time.Now().UTC()` writers) shipped, then **migration 0004** (localtime→UTC table
> rebuild + normalise pre-fix debris rows). The staged-review gate earned its keep —
> a **HIGH data-corruption bug was caught BEFORE deploy:** pre-fix sqlboiler stored
> UTC `created_at`/`deleted_at` as `'… +0000 UTC'` (correct instant, wrong skin), and
> the two-arm normalisation CASE would have shifted every `created_at` (≈ every QSO
> since April) 2 h wrong; fixed with a third CASE arm + a `seed(4)` test. **0004 then
> DEPLOYED + VERIFIED on the live 5,148-QSO dogfood DB** — `schema_migrations_log` v4
> clean, 0 debris/unparseable rows, `created_at` matches `qso_date`/`time_on` in UTC.
> **(2) `internal/api`:** the PUT /v1/config lost-update race (two SPAs, second
> silently reverts the first) — overlay + Normalize + Validate moved INSIDE the
> `Update` lock against the fresh clone (extracted `overlayConfig`; concurrency
> regression test); plus `spaHandler` `/v1/`→404 + directory→SPA-fallback
> (disabled-subsystem routes were returning 200-HTML/405). **(3)
> `internal/qsoservice`:** audit `before_image` now marshalled BEFORE the merge (a
> `contact_history` body could taint `existing`'s shared `ContactHistory` array →
> poison the SM Cloud sync input, ADR 0016); `EnqueueUploads` doc corrected to the
> actual per-TYPE stamp check; a reflection pinning test locks the immutable-restore
> denylist against `types.Qso` drift. **(4)** an FT8 dogfood note (abandon-while-TX →
> Resume immediately keys TX) triaged **WAI** — gated by `fireOpening`'s 4.5 s
> late-window (ADR 0032 truncation keeps it decodable) — and graduated to backlog as
> a work-path-opening TX-quality enhancement. Deferred lows from all three reviews →
> backlog batches. **(5) go-ft8 v0.5.0→v0.6.0 adopted** — a decode-perf bump
> (concurrent candidate analysis, **~22% faster**: ~494 ms → ~383 ms/slot on the
> 8-core dogfood box), `go.mod`/`go.sum` only, NO SM code. Protocol unchanged
> (`TestPrefixCompound_EncoderBoundary` still green → type-4 TX + `/R` decode still
> blocked); RF-safety gate + decode-file + `-race -short` + static CGO-free build all
> clean. Nice tie-in with (4): faster decode → earlier TX reply → less ADR 0032
> truncation. **Ship gate unchanged: P0 clear; the one open P1 is the
> dogfood-daemon behavioural retest (operator hardware).**
>
> **Earlier arc (session 200, 2026-07-05):** a review-driven hardening pass toward
> the 7Q8AC ship — three code reviews closed, CI made real, and the last small P1
> shipped. **(1) Bridge TX-safety review:** a failed key write (CI-V no-ACK, or a
> watchdog-closed port that flushed the frame first) could leave the rig keyed with
> the daemon believing it idle and NO backstop — `StartTune`/`KeyFt8Tx` now arm
> `strandedKeyed` on the failed-write rollback, routing into the existing F1(b)
> defensive-unkey (**ADR 0042 amendment**, scoped CI-V-decisive); CI-V teardown +
> defensive unkeys take `cmdMu` and re-check busy-TX under it; doc nits. Lows
> (auto-off timer clobber, garbled-first-IDENTITY write-block, `bridge.New`
> nil-trust) → backlog. Commits `172195dd`/`90c4de84`. **(2) smcloud store review
> CLOSED (7 findings):** 9 real-Postgres integration tests (skip-gated on a reachable
> DB), tenant-scoped upsert (no cross-tenant UUID hijack), µs `modified_at`
> canonicalisation (reconcile-hash churn — pinned in `sm-cloud-p1.md` + test),
> applied-row count, non-destructive `EnsureTenant` name, covering manifest index,
> typed `ErrEmptyPayload`. **CI now runs them for real** — a `postgres:16` service
> added to `ci.yml` (they were silently skipping); Taskfile `db:pg:up` podman
> short-name fix. Commits `3abb9be5`/`abf872e8`. **(3) SPA fetch timeouts — the last
> small P1, SHIPPED:** `safeFetch` applies a default 15 s timeout (30 s for the
> QSO-log write) so a wedged/half-open daemon can't hang boot or the submit latch —
> the flaky-Malawi failure mode; a fired timeout now surfaces as retriable
> `'network'` (was misfiled `'aborted'`, which callers drop silently). NB the earlier
> `e0b860f0` "add fetch timeouts" commit was **docs-only** — the actual wiring landed
> here. **(4)** the SPA code-review low batch triaged to backlog. **P1 now has one
> item left: the dogfood-daemon behavioural retest (operator hardware).** Active
> cycle stays the 7Q8AC ship.
>
> **Earlier arc (session 199, 2026-07-05):** the go-ft8 release SM was paused on
> LANDED (v0.5.0, tagged same day) and was **adopted** — pause LIFTED. Bumped
> `go.mod` v0.4.0→v0.5.0 (type-4 compound/nonstandard-call + `/R` encode/decode,
> upstream fixes) and audited the callsign seams. **Honest outcome — the win is
> mostly RX:** a type-4 compound-call CQ (a DXpedition's `CQ PJ4/NA2AA`) now
> **decodes** and reaches Band Activity, where before v0.5.0 it was silently
> missed. Round-trip verified; the parse/self-filter/SPA seams were already
> slash-tolerant, so no SM code change was needed for that. **TX stays
> protocol-limited:** a full standard-ladder QSO with a *prefix*-compound partner
> is still impossible — type-4 carries only CQ/RR73/73 with the partner hashed,
> no grid/report form — so the ladder's opening rung is unencodable and SM keeps
> failing soft (`StartQso → ErrTxBadMessage`; no regression). The reduced type-4
> QSO flow is now unblocked at the library level but is a **separate backlog
> feature**. Corrections to earlier framing: the standard `/P` suffix already
> worked since the v0.3.5 bump (not new here — now a regression lock); `/R`
> *encodes* but go-ft8 does NOT yet decode it, so it fails the round-trip gate and
> must not be transmitted. RF-safety gate added (`TestCompoundCQ_Decodes` +
> `TestPortableP_RoundTrip` + `TestPrefixCompound_EncoderBoundary`). Commit
> `54f1b59c`. Active cycle returns to the 7Q8AC ship goal + `backlog.md` P-tiers.
>
> **Earlier arc (session 198, 2026-07-04→05):** QRZ flaky-link resilience +
> FT8 self-transmitted-slot accuracy, both shipped and closed. **(1) QRZ:** no
> permanent self-disable on a boot-time session-key failure (lazy, cooldown-bounded
> re-auth on lookups; single-flighted via `authMu` TryLock; detached login context;
> credential redaction in logs; compare-and-clear on session-key expiry races) —
> commits `431b7eca`/`e04d643a`/`25e10f84`; the arc is **complete per the operator
> (2026-07-05)**. **(2) FT8:** self-transmitted slots are skipped for decode +
> occupancy (no more false busy readouts / garbled ghost rows from our own TX);
> TX slots recorded only after successful PTT engagement; ring buffer of recent
> TX slots; `SlotRefFromTime` floors to the 15 s lattice + test — commits
> `0ec9328c`/`98d7beab`/`f51542c3`. Also a dev-bootstrap full-upgrade warning
> (`3e84dd30`).
>
> **Earlier arc (session 197, 2026-07-03):** a non-shipping-feature session — **dev-environment
> reproducibility + a coupling-architecture pass.** Proved DEVELOPING.md against a genuinely clean
> **Fedora 41** (4 real deviations fixed in the doc + a new `scripts/dev-bootstrap.sh` for the
> incoming machine ~mid-July), then a measured coupling audit → **ADR 0043** (seven coupling
> principles, unified by *tighten what's stable / loosen what's uncertain*) → the first
> `internal/api` split steps: **`internal/api/httpkit`** extraction (the reusable response/body
> leaf) + an **import-freeze ratchet** on `internal/api`. The `qso-logged` announce spine and the
> bulk per-surface split were deliberately **DEFERRED** (the spine already exists as minimal
> `qso.stored`; the split waits for smcloud to pull the seams — the doctrine's own "loosen
> uncertain" applied to itself). Commits `8eab75d`/`3734cca` (dev-setup), `9f746a7`/`f9fb2ae`
> (ADR 0043 + factual correction), `e3b57cb` (httpkit), `a7948e5` (ratchet).
>
> **Recent arc (session 196, 2026-07-01→02):** a two-day data-integrity + safety-hardening
> arc plus a design. **(1) HH:MM:SS time precision** (ADR 0041, migration 0003): store/export
> native `HHMM`/`HHMMSS` (dedupe stays minute-precision; display HH:MM), driven by a QRZ
> round-trip destroying local seconds a QSL manager (M0URX) matches on; `QSO_DATE_OFF` now
> always-populated → fixes a midnight-crossing QSO being dropped. **(2) Forwarder-backfill SPA
> + config toggles** (ADR 0039 SPA half): logbook tri-state "uploaded?" column + manual upload
> (`POST /v1/forwarder/{name}/uploads` + `missing_from`), non-sparse config Forwarders toggles,
> same-tab cross-SPA nav (new-tab caused an SSE-starvation hang), a DEV/release pill
> (`buildinfo.Env` → `/v1/version` `env`). **(3) SM Cloud P1 DESIGNED** (ADR 0040 +
> `docs/v2-design/sm-cloud-p1.md`, NOT built): an `smcloud` forwarder → Postgres, same-repo
> `cmd/smcloud`, split-ownership, soft-delete superset, reconcile on `(UUID, modified_at)`;
> reactivates ADR 0016. **(4) FOUR code-review passes fixed + tested** (qsoservice F1–F6, bridge
> F1(a)/F2/F5 + F1(b) per ADR 0042, FT8 F1/F2 + Stop) — real latent bugs: a **midnight FT8 QSO
> silently dropped**, a **daemon-shutdown-strands-keyed** rig hole (F1(a) bench-validated:
> `systemctl restart` drops PTT), an **FT8 capture double-mic → deadlock**. **(5)**
> station-manager.org LIVE; crash-loop guards (StartLimit + deploy preflight) after a two-smd
> crash. See §"Session 196" + ADRs 0040/0041/0042. **Earlier arc (session 195, 2026-06-29→30):** daemon-heavy storage+forwarding arc, all
> deployed+validated on the live station. **(1)** ADR 0038 — forwarder retries connectivity
> outages **indefinitely** (`OutcomeUnreachable`); `failed` reserved for host-up rejections
> (offline-first fix). **(2)** **reference.db / log-db split** SHIPPED+DEPLOYED+VALIDATED —
> enrichment caches (`country`/`contacted_station`) now in a shared `reference.db`, log
> tables stay in the log DB; two `sqlite.Service` beans + nil-fallback routing + idempotent
> backup-first bootstrap (`VACUUM INTO` + rename `schema_migrations`→`_log`, no 0002 re-run).
> The **DB-manager-SPA spine is designed** (build deferred). **(3)** ADR 0039 — `enabled`
> gates enqueue (disabled = don't queue + startup-discard); non-sparse config-driven
> forwarders (action-keyed `endpoints`, registry-seeded). Daemon side deployed; SPA side
> (logbook "uploaded?" column + manual upload, config-SPA forwarder toggles) pending. **(4)**
> a 4-finding code review fixed (High: stale-completion race on re-armed upload rows). See
> §"Session 195" below + memory `project_sm_forwarder_durability` / `_db_manager_and_multifile`
> / `_forwarder_enqueue_policy`. **Earlier arc (session 193, 2026-06-26):** **FT8 occupancy Spectrum view + Tx parity
> selector + backlog staleness audit.** Two FT8 SPA features. (1) A **Tx even/odd parity
> selector** for Call CQ — 3-state **Next/Even/Odd** (daemon `tx_parity` threaded
> handler→`Service.StartCallCq`→`Sequencer.StartCallCq`, choosing the CQ-slot parity;
> SPA operating-state in localStorage; settled the open config-vs-operating-state question
> as operating state). (2) A switchable **Spectrum occupancy view**
> (`Ft8OccupancySpectrum.svelte`, Channels|Spectrum toggle) — a *continuous* alternative to
> the channelised strip: signals at their true `low_hz`→`high_hz` positions, daemon clear
> offsets as ▾/★ ticks, **click-anywhere** continuous offset pick, and graded
> **clear/near/sharing** with soft wording instead of binary red. Rationale (operator's
> insight): SM is the only FT8 app that *channelises*, which over-reports "full" and
> manufactures TX guilt; FT8 is continuous + overlap-tolerant, so the Spectrum view shows the
> real picture (pure logic in `lib/utils/ft8Spectrum.ts`, tested). Also: the **new-DXCC `*`**
> added to the worked-station enrichment card (matching Band Activity), and a **backlog
> staleness audit** — 5 items cleaned (ft8.device name-matching DONE, fresh-install
> config-shape RESOLVED, caller-side sequencing retitled "both flows shipped", PUT /v1/config
> partial-credit, Rx-pane idle-state resolved WAI, stray Tune button DONE). A rendered
> scrolling **waterfall** stays scoped in the backlog (feasibility assessed: Canvas not DOM,
> FFT stays in Go, real cost is the daemon streaming pipeline). **Earlier arc (session 192,
> 2026-06-25):** new-entity **DXCC-match fix** (country-name match false-positived European
> Russia/Germany → match numeric DXCC via embedded `internal/enums/dxcc` + `HasQsoForDxcc` +
> name fallback; memory `project_sm_new_entity_dxcc`) + FT8 `*` marker + pile-up ↑ reorder +
> logbook-count fix + config-SPA decode-log toggle + LSPA cleanup + setup→config hand-off.
> **Before (191):** config-SPA build-out (Email/PSK/Station/QSL + `PasswordField` + favicon +
> bulk import ~9× + serial by-id + 3-SPA build fix). **Before (187–190):** ClubLog forwarder;
> importer NO-UPLOAD + `smctl import`; fresh-install config fixes; RST-length migration +
> 4509-QSO import validated. Per-session detail below.

**main is v2.** Daemon (`cmd/smd`) + embedded Svelte 5 SPA (`frontend/logging/`, served at `GET /` when `Protocol=tcp && ServeSPA=true`). Day-to-day ham ops run from the frozen `v1` branch; v2 is under active development. Full suite green; CI gates every push to main.

In-tree and shipped:

- **Daemon core** — milestones 1/1b/1c (ingest → validate → store → forward → emit status → serve queries). CGO-free SQLite (modernc), UUIDv7 QSO identity (ADR 0016), `qso_history` append-only audit, dedupe key, soft-delete, one-fails-all-fail QSO writes.
- **Enrichment** (ADR 0017) — hamnut + QRZ providers, domain-tables-as-cache, three-state read policy, bounded async refresh worker. Never blocks logging.
- **Forwarder** (ADR 0022) — multi-destination `Forwarder` interface + worker + registry; enqueue gated on config presence, not `Enabled`.
- **Bridge** (`internal/bridge`, ADR 0013 + 0019) — M3a closed 2026-05-11. Read-only rig state over `/v1/rig/events` SSE; AUTO-mode CAT → filter → SPA; pipeline supervisor (ADR 0020) self-heals first-boot ordering + mid-session disruption; rig-mode → ADIF mappings; i18n error codes (ADR 0010). **Inbound command path** (ADR 0026): `POST /v1/rig/command` drives freq/mode/VFO/band (data-driven `cat` commands + `BridgeInfo.ops`); SPA rig-control on Shift+Ctrl shortcuts. **Tune-carrier path** (ADR 0027): `POST /v1/rig/tune` + a daemon-owned TX state machine keys a reduced-power RTTY carrier for external-amp tuning — the first TX feature; the daemon owns the guaranteed stop; click-only Tune button. **Rig profiles** (ADR 0028, Phase 1 shipped 2026-06-05): `config.Rigs []types.RigConfig` (+`audio`) catalogue with `default_rig_id` as the active selector; legacy loose `bridge.serial`/`bridge.cat` migrate into a single id-1 rig at load; per-rig audio is **per-direction name-based** `audio.{rx,tx}` (Session 177); `ActiveBridge()`/`ActiveFt8()` project the active rig; bridge/ft8 internals unchanged. Switch = edit `default_rig_id` + restart. Discovery endpoint + picker UI + runtime hot-swap deferred to the config-SPA work.
- **SPA logging client** — QsoPanel + CountryPanel + InfoPanel with four tabs (Worked / Details / My Station / Session), all shipped. **FT8 view (`Ft8Panel`)** carries the live Band Activity decode feed + occupancy/Clear-Slots readout; CQ decode lines are enriched SPA-side with a country flag + worked-before tint (Session 158). Keyboard-first flow, enrichment + contact-history wiring, QSO edit overlay, per-session QSO list. My Station has four sub-tabs (identity / location / equipment / qso); Mode Mappings + CW moved to the config SPA and Location was trimmed to grid/altitude/lat/lon (session 192), and **About/version moved to the config SPA's General tab** (2026-06-26). The Comment field carries a **paste-list** (localStorage MRU of recently-logged comments, clipboard-list dropdown). **Session email-out**: posts `{to, uuids[]}`; the daemon rebuilds the ADIF from the live DB rows (proper `<EOH>` header), durably stamps `sm_fwrd_by_email_*` (SessionPanel "Emailed" column), and archives a copy under `<workingDir>/exports/sent-adif/`. See memory `project_sm_session_email_sent_status`.
- **Config SPA (`frontend/config/`)** — second embedded SPA, scaffolded 2026-06-14, served at `/config/` (sub-path on the same origin; Vite `base:'/config/'` + `StripPrefix` route; dev on :5174). Separate sibling project, NOT a route in the logging SPA. The **parking place for set-once config** that's UI noise in the logging client. **Built out into a category-tab shell** (sessions 188–194): **Station** (identity + QSL defaults + morse), **Rigs** (rig-profiles editor + `ModeMappingsEditor`; backed by `GET /v1/hardware`), **FT8** (display + PSK Reporter + decode-log + enable toggle), **Forwarding**, **Email** (SMTP), **Enrichment**, and **General** (cross-cutting prefs — the `restore_rig_on_mode_switch` toggle + About/version; added session 194). Per-tab dirty/save model (presence-aware `/v1/config` PUTs); masked-secret pattern via `PasswordField`. See `docs/v2-design/api-endpoints.md` + `frontend-spa.md`.
- **Logbook SPA (`frontend/logbook/`)** — third embedded SPA, served at `/logbook/` (sub-path; dev on :5175). Separate sibling project. **First real surface shipped session 194 (2026-06-26): QSO browse** — `LogbookView.svelte`, a logbook selector + cursor-paged read-only QSO table (Date/Time/Callsign/Band/Freq/Mode/Country/Name/Comment, callsign tinted by forward/upload status) + count, over the existing `/v1/logbook`, `/v1/logbook/{id}/qso` (cursor), `/count` endpoints (no daemon change). Next/Prev/First cursor paging (no page-number jumps — daemon has no offset endpoint). The heavier management surface (edit, multi-select, export/email/upload, search, QSL-awaiting, edit-history, logbook CRUD) is a backlog item. See `frontend-spa.md` → "Logbook SPA — QSO browse".
- **CD pipeline** — `.github/workflows/ci.yml` gates every push to main (SPA lint/check/test/build + gofmt/vet/`go test -race`/embed-build/all `cmd/...`). Local mirror `task ci:local`; dogfood refresh `task deploy:local:dev`.
- **Operator daemon control** — the RPM ships `/usr/bin/smctl` (`start|stop|restart|status`) alongside `/usr/bin/smd`; it wraps `systemctl --user … smd` and prints a state-verified `SM Started.` / `SM Stopped.` line (bare `systemctl` is silent on success). See `docs/install.md §3`.
- **FT8 decode subsystem** (`internal/ft8`, ADR 0024) — opt-in (`ft8.enabled`), fail-soft, decode-is-not-a-QSO (logs "heard this" lines only; narrow-daemon-scope holds by import graph). Offline `DecodeFile` + CGO-free pipeline core (ring + UTC slot scheduler + Service) shipped 2026-05-31; **live miniaudio/malgo capture shipped 2026-06-02** behind `//go:build cgo` (+ a `!cgo` idle stub). Live FT8 needs a **CGO build** (`SM_FFT=pocketfft`); the static default decodes WAVs but can't capture. FTdx10 smoke: 4/4 slots, 0 drops, 12–16 decodes/slot. **Capture is demand-driven (Session 157):** the device is acquired on the first `/v1/ft8/events` subscriber and released a short linger after the last leaves — an idle daemon holds no mic until the FT8 view is open. **FT8 TRANSMIT (ADR 0029/0030/0031/0033) — both flows shipped.** Steps (a)–(d) + e1–e4 done: **answer-a-CQ completes + logs** (click a CQ → daemon auto-advances → 73 → logged QSO), and **Call CQ runs a sequenced caller session** (ADR 0033 `auto_first`: calls CQ, auto-works the first answerer to RR73, logs, loops the pile-up until Abandon). Daemon-owned guaranteed stop throughout; attended-only. **Completed FT8 QSOs surface to a shared session log** via the one-shot **`ft8-logged` SSE event** → SPA `sessionQsosState` → a new **Session tab** in `Ft8Panel` (same `SessionPanel` + email-out as Phone/CW; UUID carried so edit/email work). Band Activity shows a **per-CQ beam-heading column** (short-path bearing to aim the antenna). Pending: the `operator_pick` answerer-stack + its Settings toggle, and on-air validation of the caller side + the session-tab logging. See `docs/install.md §8` + `docs/ft8.md` + memory `project_sm_ft8_integration`.

Out of tree:

- **The FT8 decoder library** is out-of-tree **go-ft8** (`github.com/ColonelBlimp/go-ft8`), a WSJT-X/jt9-derivative (GPL-3.0-only) that SM links — the in-tree clean-room MIT decoder was abandoned and preserved at tag `ft8-snapshot-2026-05-30` (recoverable via `git checkout`). SM carries only the thin `internal/ft8` wrapper + live capture, not the decoder. `internal/audio` (CGO-free WAV/FFT) deliberately retained. See the CLAUDE.md FT8 bullet + memory `project_ft8_library`.

**Licence: GPL-3.0-only as of 2026-05-31 (was MIT).** Linking go-ft8 (a GPL-3.0-only WSJT-X derivative) pulls SM under copyleft. See ADR 0023 + `docs/licensing.md` + memory `project_sm_license_gplv3`.

Authoritative current-state detail lives in `CLAUDE.md` + the memory files; the long-form session-by-session record is the `### Session N` entries below + git history. **Next steps** are at the bottom of this file.

### Session 201 (2026-07-05) — **review-hardening continued (database / api / qsoservice) + migration 0004 went LIVE; a HIGH data-corruption bug caught by the staged gate before it hit the live DB.** Continuation of session 200's clean-room-review arc. **(1) `internal/database` — timestamp storage (review finding 1, MEDIUM-HIGH).** modernc stored timestamps three inconsistent ways (monotonic-debris Go `String()`, naive-local from SQL DEFAULTs/triggers read back as UTC → 2 h off, and canonical). **Fix-forward** (commit `b0ec149f`): `_time_format=sqlite` on `getDsn`/`bootstrapDSN` + `time.Now().UTC()` on the 10 `null.Time` writers → Go-written stamps now SQLite-canonical UTC (`TestModifiedAt_StoredCanonicalUTC`). Empirical scoping found the taint NARROWER than first assumed: `boil` defaults to UTC so sqlboiler (`created_at`/`deleted_at`) already stored UTC; only SQL DEFAULTs (`qso_upload.created_at`, `qso_history.at`) + the two triggers stamped local. **Migration 0004** (`0004_utc_timestamps.{up,down}.sql`, commit `291b86fd`): rebuilds the three tables with `datetime('now')` (UTC) defaults + UTC triggers, normalising every value during the copy. **STAGED-REVIEW CAUGHT A HIGH BUG** (commit `6bb2091e`): the two-arm CASE (`…+00:00` keep / else −2h) missed a fourth format — PRE-fix sqlboiler UTC rendered by Go's `time.Time.String()` as `'… +0000 UTC'` (UTC-correct, but not `+00:00`) — so the −2h arm would have shifted every pre-fix `created_at` (≈ every QSO since April) 2 h wrong. Fixed with a third arm `WHEN v LIKE '% +0000 UTC%' THEN datetime(substr(v,1,19))` (reformat, no shift) on all six columns + `seed(4)` in `TestMigrate0004`. The `-2→-3` down-step bump in `TestMigrate_DownRestoresRSTLengthConstraint` handles the new migration. Two doc-comment fixes (`61b4a2d6`). **DEPLOYED + read-only-VERIFIED on the live 5,148-QSO dogfood DB** (`~/.local/share/station-manager/db/station-manager.db`): `schema_migrations_log` version=4 dirty=false; 0 debris-format / 0 datetime()-unparseable rows; spot-check `id=5148` `qso_date=20260704 time_on=161100` → `created_at='2026-07-04 16:11:43'` (UTC, matches — a −2h bug would read 14:11); trigger now `datetime('now')`. Backlog carries the deploy-safety note (VACUUM INTO backup + post-migrate spot-check). **(2) `internal/api` review.** MEDIUM: **PUT /v1/config lost-update race** — the handler built a candidate from a pre-lock `Snapshot()` and did `*cfg = candidate` inside `Update`, so two SPAs saving different surfaces concurrently → the second wholesale-replace reverts the first. Fix: presence-aware overlay + Normalize + Validate now run INSIDE the `Update` callback against the fresh lock-held clone (extracted `overlayConfig`; request-only field validations hoisted to loud 400s; in-lock Validate failure → sentinel `errPutValidation` → 400, live config untouched; setup seed stays outside the lock). Regression test `TestHandlePutConfig_ConcurrentCrossSurfaceNoClobber` (30×2 concurrent cross-surface PUTs, both survive). Commit `985ab64f`. Later: **`spaHandler` fix** — a `/v1/*` path reaching the SPA catch-all (disabled bridge/FT8, or a typo) now returns an honest 404 instead of 200-index.html/405, and a real directory (`/assets/`) SPA-falls-through instead of an `http.FileServer` listing; tests `TestSpaHandler_ApiPathReturns404` + `TestSpaHandler_DirectoryServesIndexNotListing`. Lows (negative-limit panic, credential-clear asymmetry, stale Unwrap comment) → backlog. **(3) `internal/qsoservice` review.** LOW but real: audit `before_image = json.Marshal(existing)` ran AFTER the merge, and a `contact_history` body can decode into `existing`'s shared `ContactHistory` backing array → the audit row (SM Cloud sync input, ADR 0016) would capture post-tamper state. Fix: marshal `beforeImage` at the top of `Update`, before the merge. Also: `EnqueueUploads` doc corrected (the check is per-TYPE ADIF stamp, not per-`forwarder_name`); `TestUpdate_RestoresAllForwarderStamps` reflects over `types.Qso` stamp tags and pins the immutable-restore list against drift. Commit `e5490481`. Nits → backlog. **(4) FT8 dogfood triage.** "abandon (while Tx) → Resume immediately keys TX — too late into the slot?" → **WAI** (operator confirmed): the Resume→drain→`StartWorkCaller`→`fireOpening` path is gated by `txLateWindowSec` (4.5 s) — immediate TX only within the first 4.5 s of an our-parity slot, where ADR 0032 truncate-don't-shift keeps it decodable; past that it defers to the next slot. Struck in the inbox; graduated to backlog as a work-path enhancement (prefer a clean next-slot start over a truncated immediate fire, since a worked station keeps calling — no CQ-answer reply-window pressure). **Commit-message drift** flagged again (`b0ec149f`/`985ab64f` overclaimed vs their diffs; operator owns commit messages, handling it). **(5) go-ft8 v0.5.0→v0.6.0 adopted** (dep bump, `go.mod`/`go.sum` only, no SM code — perf-only release as the operator/author flagged). Standard adoption drill: bump + `go mod tidy` + CGO(pocketfft) build clean; RF-safety round-trip gate green (`TestCompoundCQ_Decodes`/`TestPortableP_RoundTrip`/`TestPrefixCompound_EncoderBoundary`/`TestFieldDay_RoundTrip`); decode-file tests (real 20 m + live FTdx10 slots) find the SAME decode set (no determinism break from the concurrent path); full ft8 suite green; `-race -short` clean (go-ft8's new internal goroutines are safe through SM's single-call `DecodeSlot` seam); static CGO-free build green. **v0.6.0 = concurrent candidate analysis, ~22% faster decode** (`BenchmarkDecodeSlot`, 8-core dogfood box: v0.5.0 ~494 ms → v0.6.0 ~383 ms/slot; ~2× transient allocs — negligible per 15 s slot). **Protocol UNCHANGED** — the boundary test stayed green, so type-4 grid/report forms did NOT arrive → type-4 compound TX still blocked, `/R` still undecodable (memory `sm-waiting-goft8-release` updated so a resume doesn't mistake this for a type-4 unblock). Synergy with item (4): a faster decode lands the answer-a-CQ/work reply earlier in the slot → fewer symbols truncated by ADR 0032 → better far-end copy, softening the "work-path opening: prefer a clean next-slot start" backlog note. Uncommitted: `go.mod`/`go.sum`. **(6) Dogfood log check + backlog bump.** Flaky-link day: read the smd.log — QRZ resilience healthy on the current boot (session key OK, ZERO self-disables, ~20 lookup timeouts recovered per-lookup), and the DB confirmed **3/52 QSOs logged nameless** (RG6S/R2BNC/SP9SOF) during the timeout window — the "enrichment never blocks logging" invariant held, but there's no backfill path. Operator **BUMPED** the parked "re-enrich a logged QSO" item out of P2/P3 → **next-session code work, target the LOGBOOK SPA** (`EditQsoModal` gains a "Re-enrich" action calling `/v1/enrich/callsign` + save via `patchQso`), plus a **companion manual FAQ** on the name-missing cause/remedy. **Next:** P1 = dogfood-daemon behavioural retest (operator hardware, unchanged); **next-session CODE pick = re-enrich in the logbook SPA + the FAQ** (see backlog "▶ NEXT SESSION"); the review-hardening arc is otherwise done (remaining backlog LOW/deferred).

### Session 200 (2026-07-05) — **review-driven hardening toward the 7Q8AC ship: three code reviews closed, CI integration-tests made real, the last small P1 (SPA fetch timeouts) shipped.** A pass built entirely from pasted code reviews, each verified against the code before acting. **(1) Bridge TX-safety review (`internal/bridge`).** Finding #1 (MEDIUM-HIGH): a key-on write that returns an error may still have keyed the rig — a CI-V no-ACK (`ErrCommandNoAck` = "may or may not have applied") or a watchdog-closed port that flushed the frame first — and the old rollback cleared `active` + the auto-off timer, so `unkeyOnTeardown` (gated on `tuneActive||ft8TxActive`) skipped a rig it believed idle and F1(b) never armed → a possibly-live carrier with **no daemon backstop**. Fix: `StartTune`/`KeyFt8Tx` now set `strandedKeyed=true` in the same `mu` critical section as the rollback, routing into the existing ADR 0042 F1(b) defensive-unkey (fires on the next confirmed frame). Finding #2: `unkeyOnTeardown` + `defensiveUnkeyIfStranded` now take `cmdMu` via `underCmdMuCIV`, and the defensive fire re-checks `tuneActive||ft8TxActive` **under** `cmdMu` so a legitimately-started TX isn't cut short (CI-V-decisive; a narrow fail-safe Yaesu window documented as accepted). Doc nits (#6). Lows #3–5 (auto-off retry timer-clobber, garbled-first-IDENTITY permanent write-block, `bridge.New` nil-trust) → backlog batch. **ADR 0042 amendment** documents the failed-key-write arming + the CI-V-only scoping. Tests: `TestStartTune_FailedKeyWriteArmsStranded`, `TestKeyFt8Tx_FailedKeyWriteArmsStranded`, + a busy-skip subtest on `TestDefensiveUnkeyIfStranded`; full package green under `-race`. Commits `172195dd`/`90c4de84`. **(2) smcloud store review (`internal/cloud/store`) — CLOSED, all 7 findings.** Was zero-test storage code carrying the reconcile-soundness invariant. Added **9 integration tests against a real Postgres 16** (self-apply schema from `migrations/`, skip-gate on a reachable DB via `SMCLOUD_TEST_DSN`/default localhost) covering: stale/equal/newer upsert guard + applied count, tenant-scope rejection, tombstone round-trip + resurrect-by-recency + stale-missed-delete-holds, µs precision, `Ensure*` idempotency + name-preserve, `ErrNotFound`, `ErrEmptyPayload`, manifest order/flags. Code fixes: tenant-scoped `ON CONFLICT` (`AND qsos.tenant_id = EXCLUDED.tenant_id` — no cross-tenant UUID hijack); µs `canonicalTime` truncation on write with the **peer-side obligation pinned** in `sm-cloud-p1.md` (reconcile hashes `(UUID|modified_at)` — ns/µs mismatch would re-push whole logbooks); `Upsert` returns applied count; non-destructive `EnsureTenant` name (`COALESCE(NULLIF(...))`); `INCLUDE (deleted_at)` covering manifest index; typed `ErrEmptyPayload` precheck; doc.go softened (sqlboiler configured-not-generated). Commit `3abb9be5`. **CI made real:** a `postgres:16` service added to `ci.yml` so those tests **run** instead of skip-gating (the reconcile invariants were undefended); `ci.yml` YAML validated. Taskfile `db:pg:up` fully-qualified the podman image (short-name resolution needs a TTY). Commit `abf872e8`. **(3) SPA fetch timeouts — the last small P1, SHIPPED (this session, verify committed).** No `safeFetch` call passed a timeout, so a wedged/half-open daemon hung boot on a blank page + `submitQso`'s latch for minutes (the flaky-Malawi failure mode). Fix: `safeFetch` applies `DEFAULT_TIMEOUT_MS` (15 s) to every call, composed with any caller signal via `AbortSignal.any` (operator-cancel still works); `WRITE_TIMEOUT_MS` (30 s) on the QSO-log POST (a timed-out write is ambiguous). A fired timeout now surfaces as retriable `'network'`, **not** `'aborted'` (which callers drop silently — it would have swallowed the hang). Tests: `_helpers.test.ts` (timeout→network, default-signal injection, caller-signal composition, opt-out) + 5 caller tests migrated from signal-identity to signal-propagation assertions. SPA suite 876 green; type-check/lint/prettier clean; build OK. **NB the earlier `e0b860f0` "add fetch timeouts" commit was docs-only — the wiring is this session's uncommitted change.** **(4)** the 2026-07-05 SPA code-review low batch + the bridge lows triaged into `backlog.md`. **Working-note:** the user's commit messages are drifting hard from their diffs (`e0b860f0` a standout) — a resume-staleness hazard the date-based SessionStart guard doesn't catch; flagged to the operator. **Next:** P1 remaining = the dogfood-daemon behavioural retest (operator hardware); then P2 workstreams per `backlog.md`.

### Session 199 (2026-07-05) — **go-ft8 v0.5.0 adopted → compound-call CQs now DECODE (RX win); full compound TX still a backlog feature; pause lifted.** The release SM was paused on landed (v0.5.0, tagged same day: type-4 compound/nonstandard-call encode+decode, `/R` suffix, upstream bug fixes). Bumped `go.mod` v0.4.0→v0.5.0 (`go mod tidy`, CGO build clean) and ran the callsign-seam audit the pause note called for. **Honest finding — the practical win is mostly RX, not TX:** **(a) type-4 compound-call CQs now DECODE.** A DXpedition's `CQ PJ4/NA2AA` (and directed type-4 like `PJ4/NA2AA <...> RR73`) now round-trips through the shipped decoder and would reach Band Activity; before v0.5.0 the unpacker had no type-4 path, so those CQs were silently **missed**. The parse/self-filter/SPA seams the pause note flagged (`callRe`, `parseMessage`, `dropOwnTransmissions`, SPA `parseCqCall`) were already slash-tolerant, so no SM code change was needed to surface them. **(b) A full standard-ladder QSO with a PREFIX-compound partner is still impossible.** Type-4 messages carry only `CQ`/`RR73`/`73` with the partner call HASHED to `<...>`; there is no type-4 grid/report form, so the ladder's opening `<them> <us> <grid>` rung won't encode. SM already fails soft — the M1-review guard in `StartQso`/`StartQsoFd` returns `ErrTxBadMessage` before publishing a dead ladder — so **no regression**. The reduced type-4 QSO flow (hashed CQ→RR73→73) is now unblocked at the library level but is a genuine **new SM feature** (backlog P2 "type-4 compound + free-text" refreshed). **Two earlier-framing corrections made after empirical probing:** the standard `/P` suffix already worked from the v0.3.5 bump (2026-06-18) — NOT new here, now a regression lock (`TestPortableP_RoundTrip`); and `/R` *encodes* but go-ft8 does NOT yet decode it (package doc: "RTTY Roundup … not yet unpacked"), so it fails the round-trip gate and **must not be transmitted** (noted in `EncodeToSlot` + the boundary test). **RF-safety gate added** (offline, zero RF): `TestCompoundCQ_Decodes` (type-4 CQ + hashed RR73 round-trip — the new capability), `TestPortableP_RoundTrip` (the /P ladder regression lock), `TestPrefixCompound_EncoderBoundary` (pins which prefix-compound / `/R` forms encode vs reject; flips the day go-ft8 adds the grid/report forms → the signal to lift the guard + build the type-4 flow). Full FT8 suite (incl. heavy round-trips) + whole-module short sweep green; `gofmt`/`vet` clean. Commit `54f1b59c`; docs/pause-banner removal + memory update this session. **Pending on-air:** validate a full QSO with a real `/P` station (offline-verified only). Memory `sm-waiting-goft8-release` updated. **Next:** back to the 7Q8AC ship goal — pull from `backlog.md` P-tiers.

### Session 198 (2026-07-04→05) — **QRZ flaky-link resilience (shipped + review-hardened, arc CLOSED) + FT8 self-transmitted-slot decode/occupancy fixes. Project then PAUSED pending the next go-ft8 release.** *(Entry reconstructed 2026-07-05 from git + backlog — the session ended without a handoff update.)* **(1) QRZ enrichment resilience on flaky links** (found on-air 2026-07-04, 7Q8AC-relevant): `Initialize` no longer permanently disables QRZ on a boot-time session-key timeout — the service stays enabled but keyless and `ensureSessionKey` lazily re-fetches (30 s cooldown, single-flighted `authMu.TryLock`, detached login context, cooldown stamped at completion); expired-key re-auth routes through the same path with compare-and-clear (`clearSessionKeyIf`) so concurrent expiry races can't strand a fresh key; credentials redacted from transport-error logs (`scrubURLError`). Commits `431b7eca` → `e04d643a` → `25e10f84`. **Arc declared complete by the operator 2026-07-05** — the backlog's residual ideas ((2) per-lookup retry, (3) nameless-cache-row re-lookup) are dropped, not pending; re-open only via a fresh dogfood-inbox note if flaky-link name-loss recurs in practice. **(2) FT8 self-transmitted slots:** decode + occupancy now skip slots SM itself transmitted in (was: false "busy" readouts + garbled ghost rows in Band Activity from our own signal); `TxController` records a TX slot only after successful PTT engagement; a ring buffer tracks recent TX slots for consecutive transmissions; `SlotRefFromTime` floors timestamps to the 15 s lattice (+ `TestSlotRefFromTime_FloorsToLattice`). Commits `0ec9328c`/`98d7beab`/`f51542c3`. **(3)** dev-bootstrap warning for full system upgrades (`3e84dd30`). **Next:** nothing — waiting on the go-ft8 release (bug fixes + compound-callsign support); on arrival bump the dep + re-check slash-aware parsing (memory `sm-waiting-goft8-release`).

### Session 197 (2026-07-03) — **Clean-Fedora-41 dev-setup walkthrough (DEVELOPING.md proven + `scripts/dev-bootstrap.sh`) + coupling audit → ADR 0043 → first `internal/api` split steps (httpkit + import ratchet).** No shipping feature; a reproducibility + architecture session. All green (gofmt/vet, `go test -race ./internal/api/...`, full-module build). **(1) Dev setup walked on a genuinely clean Fedora 41** (dogfood host downgraded F43→F41 over GPU desktop crashes — memory `dev-host-fedora41`). Four real deviations from `DEVELOPING.md`, all fixed in the doc + scripted: **(a)** the anaconda-base `sqlite-libs-3.46.1-1.fc41` lacks `sqlite3session_attach`, which Fedora's `libnode` links → `node`/`npm` die on first run until `sudo dnf upgrade` (now a mandatory "update base first" §0 step); **(b)** F41's Go is **1.24.10 < go.mod 1.26.2** → upstream Go (`/usr/local/go`) is **required**, not a fallback — dropped `golang` from the dnf list (it still arrives transitively via `hugo`, shadowed by PATH); **(c)** F41's **Hugo 0.126.2 builds `manual/` fine** → the 0.162.1 pin downgraded to break-glass; **(d)** a fresh install has **no git identity** → documented + a warn-only bootstrap check. New **`scripts/dev-bootstrap.sh`** — idempotent, Go version **derived from `go.mod`**, warns (never sets) on unset git identity — scripts the non-interactive path for the **incoming machine (~mid-July, memory `new-machine-incoming`)**. Commits `8eab75d` + `3734cca`. **(2) Coupling audit** (measured from the real import graph: 55 pkgs, 185 internal edges, per-pkg instability I / abstractness A / churn). Tree is healthy; one outlier: **`internal/api` Ce=22 god-package** (a single `Server` god-struct, an 11-arg `New`, ~40 handler files fused into one package). Confirmed the intended seams hold (`storage`/`forwarder` ⊥ `bridge`; `ft8` ⊥ `qsoservice`/`bridge`). Verified **`types.Qso` churn is strictly additive** (only `SessionID`/`CountryDetails` ever removed, both unused, both right after the v2 restructure) → not a refactor target, guardrail only. **(3) ADR 0043** (Proposed) — **seven coupling principles** (hide-info-behind-APIs, guard-boundaries, translate/validate-at-edge, announce-don't-command, consume-parsimoniously/produce-generously, messages-unless-semantics-fixed) unified by the meta-rule **tighten stable / loosen uncertain** (= the instability axis), applied first to splitting `internal/api` by surface behind **consumer-defined ports** + boundary guards + edge validation. Commit `9f746a7`; **corrected `f9fb2ae`** — its Context wrongly claimed "qsoservice does not announce": it **already publishes** `qso.stored`/`updated`/`deleted` on the main `events.Hub` (minimal `{qso_id, logbook_id}`, deliberately, and currently with **no SPA consumer** — the SPA subscribes only to `/v1/rig/events` + `/v1/ft8/events`; Phone/CW session rows come from the `POST /v1/qso` response, FT8 from `ft8-logged`). **(4) First api-split steps SHIPPED:** **`internal/api/httpkit`** (commit `e3b57cb`) — the JSON error envelope + response writers + size-capped body readers extracted to a leaf `Kit`; `Server` keeps thin delegate methods so the ~180 handler call sites are untouched; one 1-method `httpkit.ErrorNoter` is the only package-boundary seam (the access-log `responseRecorder` satisfies it via an exported `NoteError`). **Import ratchet** `internal/api/boundary_test.go` (commit `a7948e5`) — AST-scans api's non-test imports and **fails on any new module-internal import outside the frozen 23-set** (subset semantics: removals always pass so the split can shed freely, only growth fails; unused entries reported via `t.Log`); same idiom as `internal/bridge/boundary_test.go`; negative-tested (a probe import trips it with the ADR-0043 message). **(5) Deliberately NOT built** (doctrine applied to itself): a `qso-logged` event (spine already exists + is correctly minimal for future live-sync) and the bulk per-surface `api` split — both **deferred until smcloud (a real second consumer) pulls the seams**; ADR-0040 P1 is **backup/restore**, which doesn't consume the event stream, so there was no near-term prerequisite. Memories: `dev-host-fedora41`, `new-machine-incoming`.

### Session 196 (2026-07-01→02) — **HH:MM:SS time precision (ADR 0041) + forwarder-backfill SPA + SM Cloud P1 design (ADR 0040) + FOUR code-review passes fixed + guaranteed-stop safety arc (ADR 0042).** A two-day data-integrity + safety-hardening arc; all green throughout (gofmt, `go test -race` on touched packages, three SPA suites). **(1) HH:MM:SS time precision (ADR 0041, migration 0003).** Store + export QSO times at native `HHMM`/`HHMMSS` (was truncated to HHMM); **dedupe stays minute-precision** (`ComputeDedupeKey` fed `utils.TimeToHHMM`, so a seconds-stripped QRZ re-import still dedupes → never overwrites the stored seconds); **display stays HH:MM** (SPA `qsoDraft.timeOnFull` pins the real start second invisibly; `submitTimeOn/Off`). FT8 `BuildQso` formats `150405`. Driver: a QRZ round-trip destroyed local seconds that QSL manager **M0URX**'s OQRS matches on — so **never reconcile M0URX from a QRZ export**. `QSO_DATE_OFF` **always populated** (both paths: `BuildQso` + SPA `submitQsoDateOff`) — fixes a **midnight-crossing QSO being rejected/dropped**. Memory `project_sm_time_precision`. **(2) Forwarder-backfill SPA + config toggles (ADR 0039 SPA half).** Logbook SPA: a **tri-state "uploaded?" callsign colour** (green all / amber partial / red none) keyed on the durable ADIF `<prefix>_qso_upload_status` stamp, a **destination picker + manual upload** (`POST /v1/forwarder/{name}/uploads {uuids, force}` → `qsoservice.EnqueueUploads`), a server-side **`missing_from`** filter (`AdifPrefixForType`), an Emailed column + not-emailed toggle. Config SPA: **non-sparse Forwarders tab** (fixed toggle list, no add/remove). **Cross-SPA nav** (logging↔config↔logbook via a `navLink` snippet, **same-tab** — new-tab accumulated tabs × long-lived SSE → browser 6-conn-per-host starvation → hang) + a **DEV/release pill** (`internal/buildinfo.Env` → `/v1/version` `env`; RPM stamps `release`). **(3) SM Cloud P1 DESIGNED (ADR 0040 + `docs/v2-design/sm-cloud-p1.md`; NOT built).** Durable full-fidelity off-site backup + restore: a new **`smcloud` forwarder** (upsert-by-UUID, full `types.Qso` JSON, ADR-0038 forever-retry) → **Postgres**, **same-repo `cmd/smcloud`** (shared `internal/` types), **split-ownership** (content up / confirmation down at P3), **soft-delete tombstone** superset, **reconcile on `(UUID, modified_at)`** (reuses the `modified_at` trigger + `qso_history`), **full-JSON restore** (`smd import` JSON mode). Phase arc P1 my-backup → P2 onboard 7Q8AC → P3 auto-confirm → P4 community; phased auth (admin token → trust-on-provisioning → TQSL). Reactivates ADR 0016; security assessment gates tenant #2. Memory `project_sm_online_db_community`. **(4) FOUR operator-supplied code reviews — all confirmed + fixed + tested.** **qsoservice F1–F6:** F1 `"freq":""` PATCH → clean 400 not 500; **F2** a PATCH could forge the ClubLog upload stamp (the ADR-0039 backfill signal) + `QrzlogLogid` → added to the immutable-restore block; **F3** an import UUID collision (re-import an edited export — same UUID, different dedupe) aborted the whole run → classified as a per-record `uuid_conflict` that reports + continues (**this is the SM Cloud P1 restore path**); F4 malformed `QSO_DATE_OFF` silently dropped → reject; F5 nil-guard, F6 comment rot. **bridge F1(a)/F2/F5 + F1(b) — guaranteed-stop teardown/reconnect, ADR 0042:** **F1(a)** the daemon's OWN shutdown never unkeyed a keyed rig (healthy rig, port open, auto-off timer dies with the process → transmits until TOT); teardown now best-effort `tx_off` before `Close` — **bench-validated on the FTdx10 (`systemctl restart` drops PTT)**. **F1(b)** flag-based reconnect unkey (`strandedKeyed` set when F1(a)'s write fails; `defensiveUnkeyIfStranded` fires one `tx_off` after identity-confirm, one-shot). **F2** `keyMu` around `SendCommands`' busy-check+write (was check-then-act, could put an Exposed `set_power` on the wire mid-TX). F5 comment nits; F3/F4 deferred (Low). **FT8 F1/F2 + Stop:** **F1** a reconnect mid-release started a 2nd capture session (double mic → deadlock); `if s.capturing { return }`. **F2** `releaseCaptureLocked` drained `s.wg` under `s.mu`, deadlocking a self-dying loop (USB unplug closes the source → `onCaptureLoopExit` blocks on `s.mu` the releaser holds while it waits on that goroutine); drain now runs with `s.mu` dropped (a `releasing` guard + end-of-drain re-acquire). **Stop()** now `s.wg.Wait()`s after unlock (a fresh-eyes follow-up caught it returning before the drain when racing a linger-release). **(5)** station-manager.org **LIVE** (separate repo `station-manager-www`, Hugo + GitHub Pages); **crash-loop guards** (`StartLimitIntervalSec`/`Burst` on smd.service + a deploy preflight) after two smd on `:8080` hard-crashed the box; the name-clash-with-"Station Master" detour closed (keep the name, bought station-manager.org). ADRs 0040/0041/0042; memory `project_sm_online_db_community` / `_time_precision` / `_serial_bridge` / `_website` / `_dev_dogfood_and_crashloop` / `_name_decision`.

### Session 195 (2026-06-29→30) — **Forwarder durability (ADR 0038) + reference.db/log-db split (deployed+validated) + forwarder enqueue rework (ADR 0039, daemon side deployed) + a 4-finding code review.** A daemon-heavy storage+forwarding arc. All green throughout (`go build`/`vet`/`go test -p2 ./...`, gofmt); deploys validated on the live dogfood station. **(0) Opener:** finished the FD RST polish from the prior arc — RST_SENT = decoded SNR, RST_RCVD = config `ft8.field_day.default_rst_rcvd`; backfilled the 5 contest QSOs (also reached QRZ via the `qrz: update` forwarder path — edits DO forward upstream). FD shelved till the next Field Day. **(1) ADR 0038 — forwarder retries connectivity outages INDEFINITELY (SHIPPED).** New `OutcomeUnreachable` (the HTTP client got NO response → host unreachable): worker `markUnreachable` keeps the row `pending`, backoff saturates at the cap, NEVER counts toward `MaxAttempts`, NEVER → `failed`; `failed` reserved for host-up rejections. Fixes the ~15-min give-up (5×attempts/60→1800s) that silently stranded QSOs — wrong for offline-first ops (7Q8AC no-bundle, nationwide outage, DXpedition). qrz+clublog `Do`-error → `Unreachable`; tests + 2 forwarder-test flips. **(2) reference.db / log-db split (SHIPPED + DEPLOYED + VALIDATED).** Pulled the enrichment caches (`country`+`contacted_station`) into a shared `reference.db`, leaving `logbook/qso/qso_upload/qso_history` in the log DB — the DB-manager-SPA prerequisite + the multi-DB-file foundation. **2a** migrations split into `migrations/log/` (0001 four tables + 0002 rst-relax) + `migrations/reference/` (0001 caches), per-set golang-migrate tracking tables (`schema_migrations_log`/`_reference`), set-aware `Service.SetMigrationSets`/`missingCoreTables`/`SchemaVersion`. **2b/2c** two `*sqlite.Service` beans (`SqliteServiceName`=log + `types.ReferenceDBServiceName`=referencedb), `SetDatabasePath`→`reference.db`; routing via **nil-fallback** (`qsoservice.RefDB`/`refCacheDB`, `orchestrator.LogDB`/`logCheckDB` fall back to the primary when unwired = zero test churn); iocdi resolves two same-type beans by name. **2d** `BootstrapReferenceSplit` — idempotent, backup-first (`VACUUM INTO`), `ATTACH`+copy caches → `reference.db`, drop from log, **rename `schema_migrations`→`schema_migrations_log`** (version-identical lineage → no destructive 0002 re-run). Verified no JOIN/tx crosses the two domains. Deployed on dogfood (12 MB) + dev (302 MB); validated **bootstrap** (clean split, backups, populated `reference.db`), **read** (enrichment flag+worked-before), **write** (logged a QSO → log DB + contacted_station→reference.db). DB lives in `~/.local/share/station-manager/db/`. **DB-manager SPA spine DESIGNED** (4th SPA; two apps split structure-vs-content; Model A files⊇logbooks⊇QSOs, contest=own file; one active file, restart-to-switch via config pointer; NO schema editing; logbook SPA owns QSO content) — build deferred (memory `project_sm_db_manager_and_multifile`). **(3) ADR 0039 — `enabled` gates enqueue; non-sparse, config-driven forwarders (daemon side SHIPPED + DEPLOYED).** Supersedes ADR 0022's enqueue rule (ADR 0038's forever-retry killed the "disable to pause an outage" use case, so the queued-but-not-uploaded "suspended" state is gone). `enabled` now GATES enqueue (`shouldEnqueue` re-adds the check); disabled = don't queue + **startup discard** of its pending/failed/in_progress rows (`DiscardQueuedUploadsForForwarder`, keeps `uploaded` for `upstream_id` provenance, loud WARN); `import --forward` requires the target enabled; **endpoints → config** (`types.ForwarderConfig.Endpoints` action-keyed map, `RegisterDefaultEndpoints`/`ResolveEndpoint`, qrz/clublog resolve-from-config-with-const-fallback); **non-sparse seed** (`DefaultForwarderConfigs` + `applyDefaults` add-missing-by-type, seeding only types with a registered endpoint → the dev `//go:build dev` stub excluded). Backfill stays operator-driven (logbook SPA). Deployed: QRZ (enabled) unaffected, `clublog` seeded-disabled-skipped; the one persisted `stub` entry (a PUT between deploys) cleaned from the dogfood config (stop→strip→start, backup kept). ADR 0022 marked superseded-in-part; `config.md` updated. **SPA side PENDING:** logbook "uploaded to X?" column + manual upload (the backfill path), config-SPA Forwarders toggles. **(4) Code review (operator-supplied, all 4 confirmed + fixed):** **High** — stale upload workers could clobber a re-armed row: the four completion paths now do an atomic `UPDATE … WHERE id=? AND status='in_progress'`; 0 rows → `classifyZeroRowCompletion` (missing=ErrNotFound, re-armed=no-op); regression test + 3 tests updated to claim-before-mark. **Medium** — `CountryModelToType` dropped the PK (fetched countries couldn't UpdateCountry). **Low** — ORDER BY now whitelisted ASC/DESC (`Ordering.sqlDir`); `BeginTxContext(nil)` nil-guarded. **(5) Backlog:** operator **log viewer** (daemon diagnostics — a DB-manager tab) filed (the "loud startup log line" is worthless to ops who won't `tail smd.log`). Memory: `project_sm_forwarder_durability`, `project_sm_db_manager_and_multifile`, `project_sm_forwarder_enqueue_policy` (0022→0039).

### Session 194 (2026-06-27) — **Config-SPA `General` tab (About/version + mode-switch knob) + Logbook SPA first real surface (QSO browse).** All SPA-only, no daemon/Go change. All green: config SPA lint/check + 5 vitest; logging SPA lint/check + 851 vitest; logbook SPA lint/check + 1 vitest. **(1) Config-SPA `General` tab** — the cross-cutting-prefs home decided session 193, built as the last tab in the config SPA strip. Two occupants: the **`restore_rig_on_mode_switch` toggle** (a checkbox → presence-aware `/v1/config` PUT via new `configState.saveGeneral`/`generalDirty`/`cancelGeneral`; `restore_rig_on_mode_switch?: boolean` added to `config.ts` GET+PUT shapes, hydrated `?? true`) — so the mode-switch CAT-live re-tune knob (shipped session 193, daemon `*bool` default ON) now has a UI instead of being config.json-only; and the **About/version diagnostics** (new config-SPA `api/version.ts` mirroring the logging one + inline lazy fetch with Refresh, showing daemon version / Go runtime / DB schema). `GeneralTab.svelte` + app.svelte registration. **The About sub-tab was REMOVED from the logging SPA's My Station** (now 4 sub-tabs: identity/location/equipment/qso); the orphaned logging `version.svelte.ts` / `api/version.ts` / `version.test.ts` were deleted, and the two MyStationPanel keyboard-nav tests that expected About as the last tab updated to `qso`. **(2) Logbook SPA — QSO browse (first real surface; the SPA was a blank scaffold).** Operator pointed at the v1 reference `7Q-Station-Manager.20250823/logbook-app` (Wails, full management surface) as a UX guide — built the MVP: `LogbookView.svelte` = a **logbook selector** (`GET /v1/logbook`) + a **cursor-paged read-only QSO table** (Date · Time · Callsign · Band · Freq · Mode · Country · Name · Comment; callsign tinted red/green by forward/upload status, from the reference) + total count (`/count`), with a **page-size selector** + **« First / ‹ Prev / showing X–Y of N / Next ›** pager. **Paging is cursor-based** (operator chose Next/Prev over page-number via AskUserQuestion — the daemon's `/v1/logbook/{id}/qso` is forward-cursor `?limit&after`→`{items,next_cursor}`, no offset endpoint): `lib/states/logbook.svelte.ts` keeps a per-page cursor stack to walk Next/Prev/First. **No daemon change** — reused existing endpoints; confirmed `types.Qso` serialises FLAT (Go promotes the embedded QsoDetails/ContactedStation/LoggingStation into one JSON object), so the reference's flat field names map directly. Layers: `lib/api/logbooks.ts` (+ shared `_helpers.ts`), the state, `lib/utils/format.ts` (ADIF date/time/freq/mode formatters), app.svelte mount + smoke test. **The heavier management surface deferred** (per-row edit, multi-select + export/email/upload-selected, search/filter, QSL-awaiting, edit-history, logbook CRUD) — captured as a structured backlog item ("Logbook SPA — the management surface (beyond browse)") noting which features have daemon support vs need new endpoints, a rough build order (edit first), and that the reference is a UX guide not a port. **Dev loop:** `task frontend:logbook:dev` (:5175, HMR); ship via `task deploy:local:dev`. **(3) Docs:** `frontend-spa.md` (logbook "QSO browse shipped" section), `backlog.md` (General-tab marked SHIPPED, new "version-in-tab-title" + "logbook management-surface" items), `ft8.md` + this handoff (knob now editable on the General tab), dogfood-inbox strikes. **(4) Inbox:** logged the password-field eye-glyph idea (capture-only, untriaged). Next session openers unchanged: Spectrum colour revision; logbook per-row edit; cross-SPA nav/themes cluster.

### Session 193 (2026-06-26) — **FT8 Tx even/odd parity selector + switchable Spectrum occupancy view + new-DXCC on the enrichment card + backlog staleness audit.** All green: `go build`/`go vet`/`go test ./...`, gofmt clean; logging SPA lint/check + 852 vitest, config SPA unaffected. **(1) Tx even/odd parity (WSJT-X "Tx even/1st") for Call CQ.** Daemon: `tx_parity` ("even"|"odd"|"") threaded `handler_ft8_qso.go` → `Service.StartCallCq` → `Sequencer.StartCallCq`, where it picks the CQ slot parity via `s.theirPeriod = oppositePeriod(ourPeriod)` ("" / unknown → next-slot default). SPA: a **3-state** selector (**Next** = fire on the next slot, the fast default; **Even** :00/:30; **Odd** :15/:45 — operator chose 3-state over WSJT-X's binary to keep the fast default) in `Ft8MsgPanel`, bound to `ft8State.txParity` (operating state, localStorage `sm.ft8.tx.parity`), locked while a session is active, sent on `cq/start`. **Settled the open config-vs-operating-state question as operating state** (no config field). Caller-side only (answering forces opposite parity). Test `TestCallerSequencer_TxParityChoice`; docs api-endpoints.md + ft8.md. **(2) Switchable Spectrum occupancy view.** New `Ft8OccupancySpectrum.svelte` + a **Channels | Spectrum** toggle in `Ft8OccupancyPanel` (`ft8State.occupancyView`, localStorage `sm.ft8.occupancy.view`, default Channels). Same per-slot occupancy snapshot, *continuous* presentation: signals as soft slate shading at their true `low_hz`→`high_hz` positions (no cells), daemon clear offsets as ▾/★ ticks at real positions (aligned with the Clear Offsets list), **click-anywhere** continuous offset (clamped; arrow keys nudge, Home/End to edges, `role=slider`), and a **graded clear/near/sharing** status with soft wording instead of binary red. **Rationale (operator's insight, captured in the backlog waterfall item):** SM is the only FT8 app that channelises → over-reports "full" + manufactures TX guilt; FT8 is continuous + overlap-tolerant (close/overlapping signals routinely both decode), so the continuous view + soft grading matches reality. Grading is **position-only** (`Ft8Band` has no strength — loud-vs-weak needs the waterfall's FFT magnitudes). Pure logic `lib/utils/ft8Spectrum.ts` (`signalProximity`/`offsetFromFraction`/`clampOffset`, 7 tests). Both views write the one `selectedOffset`. **(3) new-DXCC `*` on the worked-station enrichment card** (`Ft8EnrichmentBox` — green `*` after the country when `info.isNewEntity`, matching Band Activity; rides the existing enrich lookup, no extra fetch; `?ft8demo` toggle added). Closed the "Rx-pane cap + enrichment card" backlog item (card + height-cap already shipped; richer fields deferred). **(4) Backlog staleness audit (operator: "some items are stale"):** cleaned 5 — `ft8.device` name-matching **DONE** (`resolveAudioDevice` name-matches + ADR 0028 audio.rx/tx by name), fresh-install config-shape **RESOLVED** (all sub-issues closed), FT8 caller-side sequencing retitled **"both flows shipped; only on-air validation remains"** (SPA pile-up stack + ↑ reorder superseded the daemon `operator_pick` mode), `PUT /v1/config` updated to **partial** (`default_rig_id` wired; M5 + `default_logbook.id` remain), Rx-pane **idle-state resolved WAI** (the offset-decode-when-idle behaviour is preferred over the old "go blank" decision), stray **Tune button DONE** (gone from `QsoEditOverlay`). **(5) Backlog captures (no code):** the waterfall item rewritten with the continuous-vs-channelised rationale + multiple-switchable-views decision + Canvas-not-DOM/FFT-stays-in-Go feasibility + spike-the-render-first; plus **Spectrum colour revision** and **Spectrum drag-to-set the offset** (Pointer Events + setPointerCapture, persist-on-release, live proximity feedback) — both deferred to next session by operator. Inbox: **About → config SPA + version-in-tab-title** logged (untriaged). Docs updated: ft8.md (Spectrum sub-section + Tx parity), CLAUDE.md FT8 bullet, api-endpoints.md, backlog.

### Session 192 (2026-06-25) — **New-entity DXCC-match fix + FT8 Band-Activity new-entity `*` marker + pile-up up-arrow reorder + FT8 logbook-count bump fix + config-SPA decode-log toggle + LSPA phase-2 cleanup + setup→config hand-off.** All green: full `go build`/`go vet`/`go test ./...`, gofmt clean; logging SPA lint/check + 845 vitest, config SPA lint/check + 5 vitest. **New-entity correctness (daemon):** the "new DXCC" flag (`country.is_new_entity`, `lookup/orchestrator.go` — drives the FT8 `*` and the Phone/CW Country-panel `*`) was false-positiving on worked entities (European Russia, Germany). Root cause: an **exact country-NAME string match** against the log, but hamnut's names ("Fed. Rep. of Germany", "European Russia") differ from QRZ-imported QSOs' stored names ("Germany", "Russia"), and name-match can't split European (54) vs Asiatic (15) Russia. Fixed to match the **numeric ADIF DXCC code**: new `internal/enums/dxcc` (embedded `dxcc-entities.json`, 154 entities, mirrors `enums/modes` embed + `LoadOverride` override) maps hamnut `primaryDXCCPrefix → DXCC number`; new `sqlite.HasQsoForDxccWithContext` (`json_extract(additional_data,'$.dxcc')` — the numeric DXCC lives in the blob, no column); the orchestrator derives the number and **falls back to the old name-match when a prefix isn't mapped** (partial coverage safe). Table built by `scripts/gen-dxcc-entities.py` (seed = the log's distinct DXCC numbers + a sample call → hamnut prefix; prefix collisions resolved by preferring the candidate whose stored name matches hamnut's, which rejected a log misfile — an HK4 Colombian call logged as Dominican Republic → kept HK→116). `dxcc.LoadOverride` wired in `cmd/smd`; operator override `$SM_WORKING_DIR/dxcc-entities.json`. Confirmed hamnut returns **no** numeric DXCC (only `primaryDXCCPrefix`); HamQTH `dxcc.php` does, but a live per-lookup call was rejected (a 2nd network dep in a fail-soft display path). **Known limit:** the table covers only the ~154 entities in the dogfood log — an unmapped worked entity with a name mismatch can still false-positive until its prefix is added (override / regenerate). Tests: `enums/dxcc`, `HasQsoForDxcc`, `IsNewEntity_MatchedByDxccDespiteNameMismatch` / `_DxccPath_NoPriorQso` (existing name-match tests still pass — fallback preserved). Memory `project_sm_new_entity_dxcc`. **FT8 SPA:** (1) **new-entity `*` marker** — `Ft8Panel` `decodeRow` shows a far-right green `*` when `info.isNewEntity` (rides the existing `enrichCallsign` flag lookup — no extra fetch; `ft8Enrich.svelte.ts` now carries `isNewEntity`), coexisting with the pile-up `✓` via `ml-auto`. (2) **pile-up up-arrow reorder** — `ft8PileupStack.moveUp(index)` (swap toward head; head/OOB no-op) + a left-side `↑` per `Ft8PileupDrawer` row (spacer on the head row) so a caller can be prioritised without clearing the FIFO. (3) **FT8 logbook-count fix** — the `ft8-logged` SSE handler now calls `configState.refreshLogbookCount()`; the LoggingCard `(N)` wasn't bumping for FT8 QSOs (the bump only lived in the Phone/CW submit path). **Config-SPA FT8 decode-log toggle:** `ft8_decode_log` added to `/v1/config` GET/PUT (reuses `types.Ft8DecodeLogConfig`, presence-aware; nil-in-config served as a disabled zero block so the form binds) + a Decode-log section (enable + path, default-path placeholder) on the FT8 tab, folded into the FT8 save + restart banner — surfaces the shipped-but-config-only `ft8.decode_log` (JTDX ALL.TXT) so the operator can enable it for enquiry backup. Tests: `handler_config_decodelog_test.go`; `api-endpoints.md` updated. **LSPA phase-2 cleanup (My Station):** removed the **Mode Mappings** + **CW** sub-tabs (now config-SPA domain — Rigs-tab `ModeMappingsEditor` + Station-tab morse fields) and **trimmed the Location sub-tab** to Grid Square / Altitude / Lat / Lon (CQ/ITU/DXCC + postal address live in the CSPA Station tab). All removed fields stay in the LSPA's PUT payload (the daemon full-replaces `logging_station`, so dropping them would clear them). My Station is now **5 sub-tabs** (identity / location / equipment / qso / About). CLAUDE.md + config.md mode-mapping pointers updated to the config SPA. **Setup→config hand-off:** the first-run callsign save now shows a "✓ Setup complete" interstitial (`app.svelte` `setup_done` snippet, local `justCompleted`) offering **Open the Config app →** (`<a href="/config/">`) + secondary **Start logging →**, shown once per install. **Install docs:** "power the rig on first" callout in `install.md` §4 step 1 (a USB-CAT serial port only enumerates with the rig powered on). **Dogfood triage:** new captures routed — FT8 occupancy **waterfall** → backlog (the ~10fps trigger to revisit PocketFFT for the occupancy FFT), decode-log toggle + LSPA Location trim → built this session, cross-SPA nav + themes/dark mode struck → backlog (already tracked). Backlog marked SHIPPED: new-entity `*` + DXCC fix, decode-log toggle, pile-up reorder, setup→config link, LSPA mode-mappings/CW removal.

### Session 191 (2026-06-25) — **Config-SPA build-out (Email / PSK Reporter / Station identity / QSL) + PasswordField + favicon + bulk import + 3-SPA build fix; full dogfood triage.** All green: `go test ./internal/api/ ./internal/config/`, gofmt/vet; config-SPA lint/check/format/build. **Daemon:** (1) **Bulk import** — new `qsoservice.SubmitImportBatch` (`submit_batch.go`): two-phase per batch (validate+dedupe with no open tx → one-tx write of survivors), hoisted logbook-callsign lookup, deduped `contacted_station` upserts, per-record fallback (re-run via `submit()`) on a batch-write failure; `cmd/smd/import.go` rewired to it. ~9× faster (4509 recs 11.7s→2.3s); **NO schema change** — reuses existing methods + the `(logbook_id,dedupe_key)` unique index. (2) **Serial by-id fix** — `hardware.SerialPorts()` now stores the stable `/dev/serial/by-id/…` symlink (`stableSerialID`/`EvalSymlinks`) instead of the renumber-prone `/dev/ttyUSBn`, so a saved rig port survives reboots. (3) **Email/SMTP edit surface** — masked `SmtpInfo` on `/v1/config` (`password_set` reports set/unset, merge-on-PUT keeps the stored secret; `smtpInfoFrom`/`mergeSmtp`); the read-only `mailer` projection (logging SPA) is untouched. (4) **PSK Reporter** — `psk_reporter` on `/v1/config` reusing `types.PskReporterConfig` unmasked, served raw/sparse (empty host/port → production default), `validatePskReporter` (port 0..65535). (5) **CAT/FT8 master toggles** — `bridge_enabled`/`ft8_enabled` presence-aware. New tests: `handler_config_smtp_test.go`, `handler_config_psk_test.go`, enable-toggle test. **Config SPA** (wired into the existing per-tab save model): **Email tab** (full SMTP form), **PSK Reporter** section on the FT8 tab (folded into the FT8 save + a new `ft8RestartRequired` banner — also closes the latent "no restart hint for the FT8-enable toggle" gap), **Station identity** section (identity now editable in BOTH SPAs onto one daemon source of truth, ADR 0003 — daemon `Normalize` re-derives lat/lon from grid on PUT, fixing the lat/lon-on-save report; first-run setup now self-sufficient in the CSPA), **QSL defaults** section (folded into the Station save). Reusable **`PasswordField.svelte`** (eye/eye-slash, controlled value+oninput to preserve masked-on-GET) swapped into Email/Enrichment/Forwarding. **Favicon:** `assets/logo.png` (radio tower) → each SPA's `public/logo.png`; `<link rel=icon>` base-rewritten per mount (`/logo.png`, `/config/logo.png`, `/logbook/logo.png`); rebuilt + committed all three dist seeds. **Build-script fix:** `dev-rpm.sh`/`release-rpm.sh`/`release.sh` now loop `logging config logbook` (were logging-only → silently shipping stale config/logbook bundles, incl. the new tabs). **Dogfood triage:** inbox fully worked — fixed favicon, password-glyph, QSL, PSK, and the operator/grid/lat-lon identity cluster; verified #1 (fresh-install default rig) working-as-intended; filed cross-SPA nav links, UI themes/dark mode, and setup→config hand-off to `backlog.md`. New captures pending triage: **remove mode-mappings + CW settings from the logging SPA** (now the config SPA's domain), and an **install-docs note** (power the rig on before running the config SPA so its serial port enumerates). Docs updated: `api-endpoints.md` (smtp/psk_reporter on GET+PUT), `types/pskreporter.go` comment.

### Session 190 (2026-06-24) — **Reviewed + hardened Codex's RST-length fix; validated fresh-install + 4509-QSO import end-to-end.** While the operator was offline (2026-06-23) Codex shipped the crucial **RST-length relaxation** (migration `0002_relax_rst_length`, commits `8ae65fc1` + `39fb7e10`): the `qso.rst_sent`/`rst_rcvd` `CHECK (length ≤ 3)` from `0001_init` was rejecting legitimate imported QSOs with wider RST values (e.g. `SP5VYF rst_rcvd=4657` in the operator's real 7Q5MLV export), surfaced by `runImport` as errored records. **Review verdict: correct, FK-safe, well-tested.** The SQLite table-rebuild (rename→recreate→copy→drop, mandatory since you can't `ALTER` a CHECK) reproduces every column/index/trigger/constraint faithfully — only the two RST CHECKs drop — and rebuilds the child tables (`qso_upload`, `qso_history`) *before* dropping `qso_old`, so FK enforcement (ON via DSN + runtime PRAGMA) never breaks; both migration tests assert upload + history rows survive. Two minor fixes applied this session: **(1)** restored the em-dash `—` in `0002`'s `qso_history` append-only RAISE messages (Codex had drifted to ASCII `-`, diverging from `0001`); **(2)** added `CHECK (length(rst_*) ≤ 10)` to the up-migration's relaxed columns (unbounded → generous cap that still admits real wide values but catches garbage). Down migration keeps its `≤ 3` restore. **Also struck a stale backlog entry:** the WSJT-X-style `ALL.TXT` decode log was filed as "not done" on 2026-06-22 but had since shipped (`ft8.decode_log` / `internal/ft8/decodelog.go`, commits `46f207ba` + `037e9aef`) — a fail-soft queued writer logging RX decodes + our own TX rungs, off by default, default path `$SM_WORKING_DIR/log/ft8-all.txt`; marked SHIPPED 2026-06-23 with pointers. **Fresh-install dogfood validation (operator-run):** first `smctl import` of the 4509-record export hit `target logbook id=1 does not exist` — a **fresh-install ordering gotcha**: the default logbook is only seeded on the first config PUT carrying a station callsign (`seedDefaultLogbook`, the My Station setup transition). After saving the callsign in My Station, import ran clean: **4509 stored, 0 errors, 0 dupes**; DB spot-check confirmed schema at migration **v2 (dirty=0)**, the single wide-RST `SP5VYF` row stored intact, and the `≤ 10` cap exercised on a genuinely-fresh DB. Re-import was **idempotent** — 4509 duplicates, `stored=0`, and `MAX(id)` unchanged at 4509 (dedupe short-circuits *before* INSERT, burning no row ids); `distinct dedupe_keys = total = 4509`. Operator chose to leave the malformed `4657` RST as faithful-import (SM stores, doesn't mangle). Open (non-blocking): friendlier "complete first-run setup before importing" message in `smd import` (vs raw not-found), and `/log` the fresh-install ordering gotcha for the onboarding arc. All green: `go test ./internal/database/sqlite/ ./cmd/smd/` after both edits.

### Session 189 (2026-06-23) — **Fresh-install config fixes: dangling `default_rig_id` + bridge timeouts now visible (sparse-but-served).** Continued the clean-DB dogfood config review. **(1) Dangling `default_rig_id`:** `applyDefaults` stamped `default_rig_id = 1` unconditionally, so a rig-less fresh install pointed at a non-existent rig (→ "Rig:" blank in the Phone/CW header). Fixed: `applyDefaults` only sets it when a rig catalogue exists (else stays 0 = "no active rig"); `validate.go` now rejects any non-resolving id except 0-with-no-rigs; `LoggingCard.svelte` shows **"Rig: not set"** when unset. Tests: `TestValidate_DefaultRigID`, `TestLoad_DefaultsApplied`, `TestHandleGetConfig_PreSetup`. **(2) Persistence-shape decision (and a course-correction):** I initially recommended "go sparse," then found `config.md §15.2` already settled it (sparse-on-disk rejected, filled-on-disk kept, upgrade-drift handled by the §13 migration guard) — my rec reversed a decision without checking. Re-investigating revealed the codebase actually uses **three** models, and `ft8`/`psk_reporter` are deliberately **sparse-but-served-resolved** (file empty, `/v1/config` GET serves resolved defaults via `Resolve*`). `bridge.timeouts`/`tune` were the only genuinely *invisible* block (not in file, not in GET). Operator chose to bring bridge onto the sparse-but-served pattern: added `bridge.ResolveTimeouts`/`ResolveTune` (reuse the exact `Service.New` helpers, so served == runtime; tune ceilings stay non-overridable) → served on GET as `bridge_timeouts`/`bridge_tune`. `config.json` stays sparse; SPA reads effective values; no default-freeze, no constant duplication. `qsl: {}` correctly stays empty (operator data). **`forwarders: null` confirmed correct** (don't pre-list — ADR 0022 enqueues by presence). Tests: `TestResolveTimeouts`/`TestResolveTune`. Docs: new `config.md §15.5`, api-endpoints.md GET/PUT `/v1/config`, backlog item resolved. Lesson reinforced: **read the settled design (config.md is Tier-1) before recommending a reversal.** All green: full `go build`/`vet`/`go test ./...`; SPA check/lint/format on `LoggingCard`.

### Session 188 (2026-06-23) — **Importer redesign: default NO-UPLOAD + `smctl import` wrapper (clean-DB dogfood findings).** During a clean-slate dogfood deploy the operator hit "import tries to upload to QRZ." Root cause: `SubmitImport` shared the public enqueue path, so importing a historical log queued `pending` upload rows for every configured forwarder; the old `--uploaded` flag + QRZ-LOGID stamp only neutralised *some* of them (footgun: forget the flag → mass re-upload). Redesigned per operator direction: **import uploads nothing by default**; `SubmitImport` gained `forwardTo []string` and enqueues only forwarders the operator explicitly names via the new `smd import --forward qrz,…` (validated against config, case-insensitive). The QRZ per-QSO `app_qrzlog_logid` (present on all 4509 records of the operator's real export) is now **preserved as QSO provenance** in `additional_data` (`types.Qso.QrzlogLogid` + `adif` round-trip both directions) instead of only as an upload-row `upstream_id` — so it survives even though import queues no row. Removed the now-dead `--uploaded` flag + `stampQrzUpload`/`markUploadedForForwarders` (bulk-forward of an already-stored log is the future logbook-SPA's job). New **`smctl import`** wraps the single-writer dance (stop→import→restart-if-was-running). Mode-normalization verified against the real export (USB/LSB→SSB+SUBMODE; FT8/SSB/CW are main modes, pass through) + pinned by `TestNormalizeImportedMode`. Docs updated: `install.md` §5, manual `importing.md`/`appendix-cli.md`. Dogfood inbox triaged → `backlog.md` (1 bug + 5 enh). All green: full `go build ./...`, `go vet ./...`, `go test -short ./...`; dry-run of the 4509-record real export parses clean. `preserveUUID`→`isImport` rename throughout `submit()`.

### Session 187 (2026-06-22) — **ClubLog forwarder SHIPPED.** New `internal/forwarding/clublog/` destination following the QRZ template (registered `"clublog"` + default retry via `init()`; named import + `clublog.UserAgent` override in `cmd/smd`). All green: gofmt clean, `go vet`, `go test ./internal/forwarding/... ./cmd/smd/...`, `-race` on the package, static `CGO_ENABLED=0` build.
- **API** (from the ClubLog freshdesk docs): insert = `POST realtime.php` with the QSO as one ADIF record + `email`/`password`/`callsign`/`api`; delete = `POST delete.php` matched by `dxcall`+`datetime`(`YYYY-MM-DD HH:MM:SS`)+`bandid`(numeric, via a `band→bandid` map). Classification is **status-code-driven** (2xx → success incl. "QSO OK/Modified/Duplicate" and delete "Not Deleted"; 400 → terminal; 408/429/5xx → transient).
- **Key design points** (captured in the package doc comment): (1) **API key always operator-supplied** in `credentials.{email,password,callsign,api}` — ClubLog issues one *application* key and auto-deletes keys found in public repos, so nothing is embedded in source. (2) **No `update` path** — ClubLog real-time can't edit fields (a re-upload is just a duplicate), so `action=update` → terminal with a clear message; sensible default `action_filter: ["insert","delete"]`. (3) **No upstream record id** → `UpstreamID` always empty, `priorUpstreamID` unused (delete built from the QSO's own fields). (4) **403 circuit breaker** — ClubLog mandates "STOP sending immediately or the IP is firewalled"; the first 403 trips an internal `atomic.Bool` so every later `Submit` short-circuits to terminal *without* a network call until daemon restart (the worker processes a batch per tick, so this prevents a rapid burst of 403s). No new HTTP route / config-shape change; `forwarding.md` stays the frozen Tier-2 record of intent.
- **Code-review follow-ups (same session, all green):**
  - **High — ClubLog deletes were unreachable.** The worker marked any delete `failed` when the prior `upstream_id` was empty *before* `Submit` ran (`worker.go` `resolvePriorUpstreamID`), and ClubLog inserts return no id — so every ClubLog delete died there. Fix (Option A): the worker no longer gates on an empty id; it fetches + passes it through and the forwarder decides (QRZ self-rejects empty in `buildForm`; ClubLog deletes by QSO fields). Replaced the worker-side short-circuit test with two integration tests (`TestWorker_Delete_NoPriorInsert_IdLessForwarderReaches` + `_IdRequiringForwarderFails`).
  - **Medium — ClubLog upload stamps weren't typed.** Added `ClubLogUploadDate`/`ClubLogUploadStatus` to `types.Qso` + the `adif` Record (both map directions) + `spec_validation_test.go` classification, verified against ADIF 3.1.7 (`CLUBLOG_QSO_UPLOAD_STATUS`/`_DATE` are official fields). Previously the `$.clublog_qso_upload_*` keys the worker stamps were dropped on the next typed round-trip (`type_to_model.go` marshals `types.Qso`).
  - **Medium — empty `action_filter` queued doomed update rows.** Root fix: forwarders register their **supported actions** (`forwarding.RegisterSupportedActions`, mirroring `RegisterDefaultRetry`) — qrz=insert/update/delete, clublog=insert/delete, stub=all. `applyDefaults` defaults an omitted filter to the supported set (fallback all-three for unregistered types); `validateForwarders` rejects an explicit unsupported action at load. `config` now imports `forwarding` (acyclic). Registry + config tests added.

### Session 186 (2026-06-19) — **Full-codebase code-review sweep: 9 packages reviewed, every finding fixed or triaged.** All green per package (build/vet/`-race`/suites; SPA prettier/vitest where touched). Each package committed + pushed before the next; resolution sections appended to `docs/reviews/archive/internal-<pkg>-2026-06-19.md`.
- **Pre-review small fixes (same day):** session-email send-progress toast wording (`Sending email…`); **ANT_AZ rounded to whole degrees** — `enrichment.svelte.ts` `activeBearing` + FT8 daemon `qsolog.go` (`FormatFloat` prec 0) + the CountryPanel bearing display (it's a rotator heading; sub-degree is spurious). Confirmed Notes is a fresh per-QSO field (not enrichment- or prior-QSO-populated; visible in Worked panel only).
- **Review fixes by package (order received):**
  - **adif** — H1: `<TAG:n>` oversized-length parser panic (Atoi error ignored → overflow → bad slice bound) folded into the tolerant clamp. M1: received-QSL fields (`QSLMSG_RCVD`/`QSL_RCVD_VIA`/`QSL_RCVD_NOTES`) now round-trip through `QslSection`. M2: kept the deliberate value right-trim (padded-export defence), documented; L1 stale comments fixed.
  - **api** — M1: session-email `to` parsed with `net/mail.ParseAddress` BEFORE any side effect (rejects CRLF header-injection + multi-address; display-name normalized). M2: FT8 handlers validate `slot_utc` + route unknown service errors to a generic logged 500 (`writeServerError`, no `err.Error()` leak). L1: dedupe request UUIDs; L2/L3 tests + stale freq-doc fixes.
  - **audio** — M1: `ReadWAV` rejects a short `data` chunk (`len != chunkSize`). L1: `playback.Player` made terminal-after-`Close` (mirrors capture, honours the dead `ErrClosed`) + new non-integration lifecycle tests. L3 comment refresh.
  - **bridge** — M1: an ACKed CI-V commanded freq now refreshes `CurrentDialMHz` (the dial FT8/PSK logging reads), not just SSE. M2: all CI-V snapshot writers (startup/bootstrap/poll/liveness) serialise behind `cmdMu` via a new `underCmdMuCIV` helper. L1: tune↔FT8 mutual-exclusion typed as `ErrTxActive` → 409 `rig_tx_active`. L2 stale comments.
  - **cat** — M1 (safety): CI-V ACK now requires the frame be addressed TO the controller (E0) FROM the rig + be exactly 5 bytes, so a shared-bus ACK can't complete a `tx_on`/`tx_off` confirm. M2 (operator chose **option 1**): data-driven `Command.Min`/`Max` numeric range metadata (Yaesu `FA/FB` 30k–75M Hz, `PC` 5–100 W) enforced in both protocol paths + `validateCommandRange`; CI-V `EncodingBCDPower` now **rejects** over-`ScaleMaxWatts` watts instead of clamping to full power. L2: `CloneRigDefinition` (deep copy; `Lookup` left zero-cost). **L1 (sets_state value-compat) DEFERRED to backlog.**
  - **config** — M1 (security): `WriteJSON` writes `0600` (config holds plaintext SMTP/lookup/forwarder secrets), tightens legacy `0644`, preserves stricter. M2: rig `Model` validated against `cat.Lookup` at the config boundary. M3: `Update`/`UpdateInMemoryThenPersist` deep-`Clone` so an aborted closure's nested mutation can't leak. L1 (operator chose **option 1**): `UnknownKeys` logs hand-edit typos at startup (Load behaviour unchanged). L2 doc refresh.
  - **database** — M1: `UpdateContactedStation`/`UpdateCountry` rewritten to the active-row pattern (`UpdateAll WHERE id AND deleted_at IS NULL` + `ErrNotFound`); **`UpsertLogbook` REMOVED** (dead + caller-supplied-PK upsert is nonsensical for an autoincrement key — operator: "drop it"). M2: migration verification now covers all 6 runtime tables. L1 doc fixes. Closed the 2026-06-14 contacted-station backlog item.
  - **email** — M1: STARTTLS now has coverage — a self-signed-cert TLS-upgrade fake (happy path) + a fail-closed test (no plaintext fallback); minimal `tlsRoots` test hook. M2: `Send` owns address/subject/attachment validation (`ErrInvalidMessage` before any I/O) + MIME params via `mime.FormatMediaType`; config validates `smtp.from`. L1: `smtp.enabled` kill-switch docs across email/config/cmd-smd/handler/endpoint-ref.
  - **errors** — M1 (observability): added `DetailedFrame` (direct type assertion) and switched `logging.buildErrorChain` to it — a mixed `DetailedError → fmt.Errorf("%w") → DetailedError` chain no longer drops the stdlib-wrapper frame or double-counts the nested error. L1 doc fixes (`LocalMsg`/`Cause()` removed, links → `archive/`).

### Session 185 (2026-06-18) — **PSK Reporter reception-report upload SHIPPED + live-validated; FT8 Next-drain & ladder-highlight bugs fixed; port-8080 restart-flap fix + Taskfile dev-daemon guard; go-ft8 v0.3.5 `/P`.** All green throughout (Go build/vet/`-race`/suites; SPA check/lint/build + 835 vitest).
- **go-ft8 v0.3.4 → v0.3.5: standard `/P` now encodes.** `EncodeStandardMessage` accepts the standard `/P` variant, so SM works `/P` stations **end-to-end with NO SM code change** — every TX guard decides by trying the encode + skipping on error, so the upstream gain flows straight through. **Type-4 compound (`PJ4/K1ABC`, `/MM`, …) + free text still skipped** (need WSJT-X hashed-callsign encode, gated on go-ft8). Proven offline (`internal/ft8/modulate_test.go`: `TestEncodeStandardMessage_Portable` + round-trip). Committed.
- **PSK Reporter reception-report upload — new `internal/pskreporter` (report/upload side only).** The FT8 "who's hearing me" / propagation-map feed. **IPFIX/UDP encoder, byte-exact vs the spec's worked example** (`ipfix_test.go`); a buffer → dedup (best SNR per call) → flush service: ~5 min cadence (program-relative timer + jitter, NOT clock-synced), descriptors first-3-datagrams + hourly, one long-lived UDP socket (constant source port). **Fed by FT8 decodes via `ft8.Service.SetDecodeSink`** (one-way DI, same pattern as `SetQsoLogger` — `internal/ft8` stays narrow); `cmd/smd` extracts a spot per decode with **`ft8.SpotFrom`** (reuses the sequencer `parseMessage`; CQ→caller+grid, directed→sender, hashed/free-text skipped), reporting **freq = dial + audio offset** (the real RF, via `bridge.CurrentDialMHz`), SNR, mode FT8, slot time, `informationSource=1`. Receiver identity from `logging_station` (call/grid) + `StationManager <ver>` + **antenna from `MY_ANTENNA`** (sourced from the station config, NOT a separate `psk_reporter` key). Config block `psk_reporter` (`enabled` default **OFF** — opt-in/public; `host`/`port`), set-once like SMTP (not on `/v1/config`). **Collector host = `report.pskreporter.info` (NOT `pskreporter.info` — that hostname is the Cloudflare website and silently drops UDP); port 4739 = production, 14739 = test** (same host, parses without writing the live DB). `cmd/ft8-psk-probe` = a dev/test CLI (flags; defaults to the test port; `-dry`). **Live-validated against the real collector** (`/cgi-bin/psk-analysis.pl` showed our datagram received + every field parsed) AND **running in production on the dogfood daemon** (`pskreporter: uploaded spots` 80–81/flush). **3 review findings fixed:** (1) optional startup made non-fatal (cmd/smd logs+continues; `AddSpot` guards `conn==nil` so a failed start can't grow the buffer unbounded); (2) a full-buffer flush no longer does UDP I/O on the FT8 decode goroutine — `AddSpot` signals the flush loop (non-blocking); (3) the receiver template is a **fixed 4-field shape** (antenna always present, empty when unset) so a runtime `SetReceiver` can't desync the cached template. Committed + deployed.
- **FT8 "Next" drain bug fixed (operator-reported).** Clicking Next on a pile-up no-show *stopped* the auto-drain with no way to restart. **Root-caused from the dogfood logs:** Next → `qso/abandon`, then the drain's immediate `qso/work` hit a transient **`503 rig_not_ready`** (the TX→RX settle right after cancelling the in-flight TX); the drain treated it as terminal — cleared its latch, **lost the already-dequeued head**, and never retried (nothing reactive re-fired the `$effect`). Fix (`Ft8Panel.svelte` drain `$effect`): don't dequeue until the start **succeeds**; on a transient (`rig_not_ready`/network) keep the head + **retry** (~1.5 s, up to ~9 s) via a reactive tick; if the rig's genuinely down after the retries, **pause** (queue kept, Resume restarts); drop only on a hard per-entry rejection. Committed + deployed.
- **FT8 ladder-highlight bug fixed (operator-reported, cosmetic).** Starting a work-a-caller (or answer-a-CQ) exchange briefly highlighted the **RX reply row** instead of the opening **TX rung**, then snapped back when TX keyed. Root cause: `tx.transmitting ? txRow : txRow+1` reads "not transmitting" as "waiting for reply" — but at the open the daemon publishes the rung with `repeats=0` + `transmitting=false` **before** the first key (`work_sequencer.go` 69→75→78). Fix: a `rowFor(txRow,len)` helper keys off `qso.repeats` — TX row when transmitting OR `repeats===0` (about to send), RX row only once sent + waiting (`repeats>0` && !transmitting). Applied to all three ladders (work/answer/caller). The daemon always *sent* the right messages — display-only. Uncommitted (latest).
- **Port-8080 restart-flap.** (1) **`server.StopAccepting()`** closes the listener at the START of shutdown — it was held until the final `server.Shutdown`, behind the multi-second bridge/ft8/psk teardown (`ft8.Stop` waits for an in-flight decode), so a replacement process racing the old daemon's teardown got `address already in use`. Now `:8080` frees immediately; the later `Shutdown` still drains connections. Race-guarded listener field, `net.ErrClosed` treated as a clean stop; regression test `TestServer_StopAccepting_ReleasesPort`. Committed + deployed. (2) **The actual recurring cause = a SECOND smd** — the dogfood `task run:smd` (or a stray `build/bin/smd`) overlapping the systemd daemon on `:8080`. Proven from the journal: systemd smd cleanly Stopped at 16:44:15 yet `:8080` was still in use at 16:44:25 → a freed listen port is instantly rebindable (`SO_REUSEADDR`), so another process held it. Fix: **Taskfile `run`/`run:smd` now auto-stop the systemd smd first** (tolerant of not-running/not-installed) + an echo reminder to restart it after. ⚠️ **Validate Taskfile edits with `task --list`, not `python yaml.safe_load`** — a `: ` (colon-space) inside a cmd string parses as a valid-but-wrong YAML *map*, which `safe_load` accepts but Task rejects ("invalid keys in command"); my first guard shipped broken and was caught on the next `task deploy:local:dev`. Taskfile guard committed (after the YAML fix).
- **Dogfood-inbox:** "footer for Band activity needs to be bigger text" (capture-only).
- **Roll-off due:** the live `### Session N` list is past the ~15 cap; roll the oldest entries to `session-handoff-archive.md` next maintenance pass. (Done in session 186: rolled 171 and older down, leaving 172–186 live.)

### Session 184 (2026-06-17) — **Session-email subject = logbook-callsign-prefixed + body QSO-count line (daemon-only).** All green: `go test ./internal/api`, build.
- **Why:** as multi-operator interest grows, a QSL manager receiving logs from several operators got identical-looking mail (the logging callsign was only inside the ADIF). Now the **subject is prefixed with the logbook callsign** (e.g. `G4ABC Station Manager session ADIF — …`) and the **body adds `Contains N QSOs.`** under "ADIF for this session attached." (singular `QSO` for 1).
- **Where:** `internal/api/handler_session_email.go` — extracted pure helpers `sessionEmailSubject(callsign, supplied, now)` (callsign comes from `db.LogbookCallsignByIDWithContext(qsos[0].LogbookID)` — the literal logbook callsign the QSOs were logged under; best-effort, a fetch failure just omits the prefix, never fails the send) + `sessionEmailBody(n, now)`. Unit-tested (`TestSessionEmailSubject_CallsignPrefix`, `TestSessionEmailBody_QsoCountAndPluralisation`). Hardcoded shape for now.
- **Backlog:** added "Configurable session-email subject + body (formatting tags)" — operator-editable templates (`{callsign}`/`{count}`/`{date}`/…) in config.json, defaults = today's hardcoded strings; this shipped as the first step. **Uncommitted.**

### Session 183 (2026-06-17) — **FT8 pile-up callsign stacking SHIPPED (SPA-only).** All green: SPA check/lint/format + 833 vitest; no daemon change.
- **The feature (operator-designed workflow):** during a pile-up, stations call you but are only *clickable* (work-now) when armed+idle, so callers spotted mid-QSO vanish before you can act. Now **Ctrl/Cmd+click** a calling-you decode to push it onto a **FIFO pile-up stack** (worked oldest-first); the Operate view **drains** it via the existing work-a-caller path whenever armed+idle, advancing as each contact completes, while the operator keeps adding. Capture (Ctrl+click) works in **any** state (mid-QSO, disarmed — pure capture, no TX), which is the whole point (callers are only visible in your RX parity).
- **Architecture (operator's call):** SPA owns the queue, daemon untouched. New `ft8PileupStack.svelte.ts` (in-memory FIFO, dedup-by-call refresh-in-place, push/peek/dequeue/remove/clear + `enabled` drain flag; erased on tab close, like `callsignStack`). `Ft8Panel`: `onCallerClick` (Ctrl→enqueue, plain→work-now), a `✓` marks stacked rows, and a drain `$effect` (armed+idle+freqKnown+offset+enabled+non-empty → `startFt8WorkCaller(head)`, re-entry latch until `qso.active` confirms). `Ft8PileupDrawer.svelte` in the Operate tab (call·grid·SNR, per-entry remove, Clear-all, Resume-when-paused) + a depth badge on the Operate tab. **Abandon pauses** the drain (queue kept; Resume restarts).
- **Supersedes** the daemon `caller_answer_mode: operator_pick` Call-CQ mode (still `501`-rejected, now unlikely to be built — the stack gives operator-chosen working for anyone calling you). `auto_first` Call CQ stays as the hands-off loop. Attended-only preserved (operator Ctrl+clicks every station — *more* attended than `auto_first`).
- Tests: `ft8PileupStack.test.ts` (7). Docs: ADR 0033 amendment, `ft8.md`, `backlog.md` (item closed), `keyboard-shortcuts.md`. **Uncommitted.**

### Session 182 (2026-06-17) — **FT8 Settings toggle + IC-7300 operating-freq POLL fix (the real cause of the wrong-band logging) + backlog adds + filter design pending.** All green: Go cat/bridge/types tests, SPA check/lint/format + 826 vitest.
- **IC-7300 POLL was missing the operating-freq read (root cause of the wrong-band logging + "waiting for rig").** ADR 0035 kept VFO-A push-only (Transceive), but push fires only on a freq **change** — parked, VFO-A is never re-sent, so a fresh SPA tab/reconnect misses it and `catState.vfoA` sits at the 14.250 placeholder. Confirmed by tapping `curl -sN :8080/v1/rig/events`: `vfoA` came once, then the poll cycle repeated `vfoB/mode/split/power` with no `vfoA`. **Fix: added `2500` (operating-freq read) to the IC-7300 rigdef POLL** (`icom-ic7300.json` POLL = `["2500","2501","2600","0F","140A"]`, mirroring READ); push stays as the real-time-on-change layer, poll is the steady-state backstop. `civ_test.go` `TestEmbeddedIC7300` POLL golden updated; ADR 0035 revised; memory `project_sm_ic7300_borrowed` updated. **⇒ Embedded rigdef → needs a redeploy + bench re-confirm of poll cadence with the extra read.** This is the proper fix behind the session-181 SPA freq guard (which stays as belt-and-braces). NB diagnosis: the live daemon was the **systemd/dogfood** `/usr/bin/smd` (holds the IC-7300 ttyUSB, logs to `~/.local/share/station-manager/log/`), not `task run:smd` (logs `build/log/smd.log`) — `deploy:local:dev` runs dev code on systemd.

- **Float-CQ-to-top toggle (shipped, backlog item closed).** New daemon-backed `ft8.display.cq_to_top` (bool, default off): `types.Ft8DisplayConfig.CqToTop` + `ResolveFt8Display` passthrough; SPA mirror (`config.svelte.ts` `Ft8DisplayView.cqToTop`, `api/config.ts` `Ft8DisplayFields.cq_to_top`); Settings-tab checkbox; `Ft8Panel` `orderedDecodes` `$derived` stable-partitions CQ rows above the rest (gated on `cqToTop`, applies in BOTH feed modes), and **suppresses per-slot separators** while on (the list is no longer slot-ordered). Tests: Go `cq_to_top` resolve case, SPA `config.ft8.test.ts` hydrate/default. Docs: `ft8.md`.
- **Backlog adds (operator-flagged on air):** (1) **work compound/portable (`/P`,`/MM`,…) callsigns + free text** — `EncodeStandardMessage` rejects them so the sequencer skips such answerers/callers; needs WSJT-X hashed-callsign (type-2) support (likely a `go-ft8` API addition) + free-text encode + UX. Real protocol work. (2) **Band Activity display filter** — design pending (below).
- **⇒ OPEN DESIGN (this session, not built): Band Activity prefix/substring filter.** Session-scoped (in-memory like the selected offset, NOT config). Open points: match target (callsign vs whole text), prefix vs substring, placement, interaction with float-CQ-to-top. Pin design → then build.
- **Uncommitted** (toggle + docs + backlog).

## Active cycle (the 1–3 things in flight now)

> **The full ranked queue lives in `docs/backlog.md` → "Worklist index".** This
> section is ONLY what's actively in flight — it does **not** re-rank the backlog
> (that's the backlog's job; this doc points at it).
>
> **▶ Focus (set 2026-07-04): _Next shippable state for 7Q8AC._** The goal is a
> release the external operator (7Q8AC, Malawi, offline-first) can run; "stabilise &
> finish in-flight" is the means. The P0/P1 items below ARE the ship gate — clear them
> before opening any new P2 workstream (theming included):
> - ~~**P0** — `PUT /v1/config` omitted blocks zeroed~~ **FIXED 2026-07-04** (→ archive);
>   `default_logbook.id` stays a **P3** residual (no logbook-switch consumer yet). **P0 now clear.**
> - ~~**P1** — FT8 caller-side sequencing (Call CQ pile-up): on-air validation~~ **PASSED
>   2026-07-04** — 33 QSOs / ~74 min on 17 m, full ladder + auto-resume + enrichment;
>   guaranteed-stop confirmed (rig off → warn + TX stop). One bug found + fixed same session:
>   **FT8 self-decode** (`dropOwnTransmissions`, `TestDropOwnTransmissions`). See archive.
>   (FT8 **Field Day** UI + further FD validation remain **PARKED** — testable only during a
>   Field Day contest; ARRL/RAC-only, so not a 7Q8AC concern. See backlog Parked tier.)
> - ~~**P1** — multi-tab rig hazard~~ **awareness banner SHIPPED 2026-07-04** (daemon
>   `rig-clients` SSE + logging-SPA banner; `TestSubscribe_BroadcastsClientCount`). Full
>   operating-lock (ownership/take-over) → **P2** — not a single-op 7Q8AC blocker.
> - ~~**P1** — bridge review F3/F4~~ **DONE 2026-07-04** (see backlog-archive): F3 tune-restore
>   detached from the request ctx (regression test); F4 `deliverAck` accepted-limitation comment.
> - ~~**P1** — SPA fetch timeouts (flaky-link ship risk)~~ **SHIPPED 2026-07-05 (session 200)** —
>   `safeFetch` default 15 s / 30 s write timeout; a fired timeout → retriable `'network'`. See backlog-archive.
> - **P1** — behavioural retest of the shipped session-192/193 daemon changes on the dogfood daemon
>   (detail: items 1–2 below). **← the one P1 left; needs operator hardware.**
>
> **Parked big workstreams (built on go-ahead, NOT this cycle — see backlog):**
> `internal/api` split (ADR 0043, opportunistic), SM Cloud P1 (ADR 0040), DB-manager SPA.
>
> The numbered items below are the **detail / trail** behind the above (some
> superseded — operator_pick, IC-7300 arc — kept for history):
> 0aa. **`internal/api` split — continue opportunistically (ADR 0043; NOT a standalone project).**
>    Session 197 shipped `httpkit` + the import-freeze ratchet. The bulk per-surface split (ports,
>    per-surface packages, sibling-isolation boundary tests) is **deferred until smcloud pulls the
>    seams** — peel a surface only when cloud work touches it. Do NOT big-bang it (the ~9k-line api
>    test suite is the cost). The `qso-logged` consumer-unification is likewise deferred (spine
>    exists; keep `qso.stored` minimal). ADR 0043 is the map.
> 0a. **SM Cloud P1 — build (on go-ahead; DESIGNED session 196, NOT started).** Per ADR 0040 +
>    `docs/v2-design/sm-cloud-p1.md`, sequence **S1–S6**: Postgres store → `cmd/smcloud` HTTP
>    API (upsert-by-UUID + reconcile + export) → `smcloud` forwarder → daemon reconcile → `smd`
>    JSON-restore. P1 single-tenant; onboarding 7Q8AC (tenant #2) is gated on the security
>    assessment. Memory `project_sm_online_db_community`.
> 0a2. ~~ADR 0039 SPA side~~ **DONE session 196** — logbook "uploaded?" tri-state column +
>    manual upload + `missing_from` filter + config-SPA Forwarders toggles all shipped.
> 0a3. **Bridge review F3/F4 (Low, deferred session 196):** F3 — post-unkey restore skipped on a
>    dead request ctx (detached-ctx fix, mirrors the qsoservice dedupe pattern); F4 — a late CI-V
>    ACK can bleed into the next command's wait (protocol-inherent; a `deliverAck` comment is the
>    fix). **FT8 Band-Activity slot divider (todo-next):** the accumulate divider already shows
>    time+band; add **parity** (`slotParity`), check the `cqToTop` suppression, dogfood, then
>    decide whether the Rx-Frequency pane needs its own grouping (backlog "FT8 accumulate-mode
>    duplicate rows").
> 0b. **DB-manager SPA (spine designed session 195, build pending):** the 4th SPA — files +
>    logbook CRUD + forwarding-queue health + cache inspect + backup/restore + ADIF import;
>    NO schema editing. The reference.db/log-db split (its prerequisite) is shipped+validated.
>    Multi-file switching (active-file pointer, restart-to-switch) + a **log-viewer
>    diagnostics tab** (backlogged) ride with it. Memory `project_sm_db_manager_and_multifile`.
>    *(Session-195 daemon work — ADR 0038/0039, the DB split — is already deploy-validated on
>    the live station, so it does NOT need the behavioural retest in item 1.)*
> 0. **FT8 Spectrum view follow-ups (operator-set opener, still open):** the two **FT8 Spectrum view** follow-ups
>    captured in the backlog — **(1) colour revision** (the first-pass slate/green/amber/
>    orange-red palette wants reworking, reconcile with the shared-theme/dark-mode work) and
>    **(2) drag-to-set the offset indicator** (Pointer Events + `setPointerCapture`, reuse
>    `offsetFromFraction`, persist-on-release, `touch-action:none`, live proximity-colour
>    feedback). Both deferred from session 193 to here. See the backlog "FT8 Spectrum view"
>    items.
> 1. **Behavioural retest on the dogfood daemon** (`task deploy:local:dev` — embeds all three
>    SPAs). Still the biggest unvalidated batch: session-192/193 **daemon** changes —
>    **new-entity DXCC matching** (confirm European Russia + Germany no longer show the `*`),
>    the **config-SPA decode-log toggle** (enable → restart → `ft8.decode_log` writes ALL.TXT),
>    and the **Tx even/odd parity** (pick Even/Odd → first CQ lands on that parity). Plus the
>    FT8 SPA surfaces (`*` marker, pile-up ↑, **Spectrum view** click-anywhere + grading,
>    logbook-count bump), the LSPA trims, and still-unconfirmed session-191 surfaces
>    (Email/PSK/Station/QSL, favicon, eye-glyph, CAT/FT8 toggles).
> 2. **New-entity DXCC table coverage:** the embedded table covers the ~154 entities in the
>    dogfood log. If a known-worked entity shows a stray `*`, add its `primaryDXCCPrefix` via
>    `$SM_WORKING_DIR/dxcc-entities.json` or regenerate (`scripts/gen-dxcc-entities.py`).
>    Memory `project_sm_new_entity_dxcc`.
> 3. **UI themes / dark mode + shared-theme layer (filed 2026-06-24):** the largest UI item —
>    a colour-token refactor across all three SPAs first. (**Cross-SPA nav links + a DEV/version
>    tab-title marker SHIPPED session 196**; the SSE-consolidation follow-up + FT8 settings
>    tooltips/beginner-expert are new backlog items filed session 196.)
> 4. **FT8 occupancy waterfall** — the rendered scrolling-waterfall view (backlog, now with
>    full rationale + feasibility); the soften-the-red strand shipped as the Spectrum view
>    (session 193). The ~10fps cadence is the trigger to revisit PocketFFT for the occupancy
>    FFT (memory `project_sm_realfft_stays_pure_go`).
> 5. **PSK Reporter follow-ups (future, in backlog):** the **retrieve/query side** (who heard
>    *you*) and **generalize to a spot-submitter registry only when a 2nd destination (DX
>    cluster) lands**.
>
> *(Maintenance: rolled Session 181 → archive 2026-07-02 when adding 196; live list is now
> 182–196 = 15 entries, at the ~15 threshold.)*
>
> The FT8-TX items further below are STALE — TX (a)–(e) + answer-a-CQ + caller-side +
> work-a-caller + pile-up stacking all shipped; "auto-sequence" is OUT OF SCOPE /
> QEX-forbidden (attended-only). Read the top `### Session N` entries for true state.

### Near-term goal: Icom IC-7300 CAT (borrowed rig) — ENGINE + RIGDEF SHIPPED & VALIDATED; finishing the rough edges

**IC-7300 CAT is now full-featured & on-rig validated** (Sessions 172–175): CI-V
engine + rigdef, inbound commands via **wait-for-ACK** (ADR 0034 rev), **full
state-mirror polling** for VFO-B/USB-D/split → display parity with Yaesu (ADR
0035), VFO swap (+ optimistic mirror), FT8 band buttons assert USB-D, and **FT8 RX
working** (codec = PCM2901: capture index 4 / playback index 2). FT8 **TX keying
added to the rigdef** (`tx_on`/`tx_off`, unexposed) — bench not yet run.

**⇒ The IC-7300 arc is CLOSED (Session 176, 2026-06-16):** first Icom on-air FT8 TX
validated end-to-end (`-key` bench — keyed on slot, USB-D, clean self-unkey), and the
ADR 0036 cleanup is done (deleted; folded into `config.md` §10.4 #1). No IC-7300
next-action remains.

**Diagnosed, parked — not bugs:** split **control** (a `set_split` toggle;
split *display* already works via the poll); **band-jump `Ctrl+Shift+5–9`** on Icom
(no `BS` equivalent — needs band-stacking register `1A 01` or `set_freq`-to-default,
a design call); **band highlight** (SPA derive current band from freq). The **per-rig
audio model daemon side SHIPPED 2026-06-16** (per-direction name-based `RigConfig.Audio.{rx,tx}`,
config.md §10.4 #1, Session 177) — only the **by-name picker UI** (config-SPA rig-profile editor)
remains. **Commit** any uncommitted arc.

> **The detailed sub-items below (wait-for-ACK fork, USB-D differentiation, freq
> up/down shortcuts) are now DONE** — wait-for-ACK shipped (ADR 0034 rev), USB-D is
> solved by the `26 00` poll (ADR 0035), freq shortcuts work on the rig. Kept for
> history; Sessions 174–175 are the current state.

**⇒ NEXT ACTION (resume here): operator decides the wait-for-ACK fork, then build it.**
Session 173 re-validated the command path standalone and designed the fix —
**"adopt-on-ACK" supersedes the earlier "read-after-write"** (better: no second
round-trip, sidesteps the half-duplex read collision, resolves USB-vs-USB-D on
the command path). The IC-7300 ACKs a commanded change with `FB`/`FA` (~20 ms) and
sends NO broadcast, so adopt-on-ACK is the only way the SPA learns the command
landed. **The full design is in ADR 0034 → "Command path: wait-for-ACK".** Before
coding, the operator picks: **synchronous** (recommended — `SendCommands` waits
~20 ms, `FA`→HTTP error) **vs async pending-queue** (non-blocking, `FA`→SSE error
event); and confirms the data-driven `Command.sets_state` op→state approach. Then
build the chosen variant (classifier + readLoop routing + `cmdMu`/`pendingAck`
waiter + per-op synthesize via `mapStatusToPayload` + `civ_ack_ms` knob; Kenwood
path untouched). Possible refinement: coalesce freq-step key-repeat. Then, in order:
1. **USB-D differentiation** — DECISION PENDING. Accept (documented: the `04` read
   gives base mode, `1A 06` data flag never broadcast) vs build the `1A 06`
   snapshot read + stateful base+flag mode-assembly (options 1+2). Operator made
   the operational case (FT8 leaves USB-D → phone TX in a data slot). Note even
   1+2 goes stale on a silent front-panel data toggle — only polling (rejected)
   fully closes it.
2. **Freq up/down keyboard shortcuts** broken for the IC-7300 — diagnose (not yet
   looked at).
3. **No band highlight** — SPA band buttons don't derive current band from freq
   (SPA-side).
4. **Doc pass** — ADR 0034 (spacing/identity/FB-ACK/wait-for-ACK findings now
   documented), memory `project_sm_ic7300_borrowed` (done), the
   `bridge.timeouts.civ_read_gap_ms`/`civ_ack_ms` knobs, and `install.md`
   prerequisites (Transceive ON, USB Port = Link to [REMOTE], baud = CI-V Baud
   Rate, USB SEND OFF) when shipping. CLAUDE.md serial-bridge bullet when the
   command path lands.

Read strategy (REVISED for Icom by ADR 0035): push for the fast operating
freq/mode **plus** a targeted, collision-aware **poll** of the un-pushed fields
(VFO-B/mode+data/split) — Yaesu stays push-only. Commands use **wait-for-ACK**
(adopt-on-`FB`), not the old read-after-write framing. Validated facts + gotchas
live in ADR 0034 + ADR 0035 + memory `project_sm_ic7300_borrowed`.

### Parked follow-ups (named, deliberate defer)

- **Contest logging not built.** Flagged session 66 (2026-05-16). The SPA today is steady-state casual-QSO logging — no contest mode, no macro keys (though F1, F4–F12 are already reserved by ADR 0007 for this), no exchange-field handling (serial numbers, RST+state, etc.), no real-time dupe checking, no multiplier tracking, no Cabrillo export, no contest-specific ADIF fields (`STX`, `STX_STRING`, `SRX`, `SRX_STRING`, `CONTEST_ID`). Scope question to settle when it's picked up: separate client (e.g. `frontend/contest/`) versus a mode switch inside `frontend/logging/`. Contest logging has different UX rhythm (high rate, keyboard-first, minimal panels) and different field shape (per-contest exchange template) — likely warrants its own SPA in line with the logging-vs-logbook split per `feedback_logging_vs_logbook_scope`, but pin that decision when an operator-driven need surfaces (likely the next CQ WW or similar contest the operator wants to enter). Daemon side is largely already there — `types.Qso` follows ADIF (so contest fields slot in via existing `additional_data` pattern), multi-rig API-aware for SO2R contests, UUIDv7 for sync.

- **FT8 SPA surface — BUILT (superseded the "holding scaffold / log-only" note).** The Operating Mode `<select>` in `LoggingCard` (Phone/CW ↔ FT8) renders `Ft8Panel`: live **Band Activity** decode feed (CQ flag + worked-before enrichment), **Rx Frequency** pane, **Clear Offsets** + the **Occupancy** picker strip, the **Operate** tab (Arm/Call-CQ/Abandon + the live role-aware message ladder), a **Settings** tab (daemon-backed display prefs), and a main-panel footer slot countdown. Decode→QSO is the e4 logging path (a completed *exchange* is a QSO; ADR 0024's integration point, realised via the injected `SetQsoLogger` sink — no `qsoservice` import). Remaining FT8 SPA work is tracked in `docs/backlog.md`: FT8 session-log tab, Rx-pane worked-station enrichment card, footer info-strip, CQ-to-top toggle, and the `operator_pick` answerer stack. `ft8.device` is still index-only (name-matching deferred).

- **FT8 AP-decode hints (ADR 0025, Accepted; pieces 2–4 deferred).** The next decode-recall lever after OSD: feed go-ft8's a-priori decoder a ranked, capped, deduped callsign hint set so it can hypothesise weak signals (the −14 dB tail OSD still misses). **go-ft8 v0.2.0 (session 124) shipped the API** (piece 1 done); the daemon doesn't use it yet. **Decision already shaped (ADR 0025):** SM builds the hint set in a storage-backed provider *outside* `internal/ft8` and injects it via an `APHintProvider` interface seam (mirrors `captureSource`); neither go-ft8 nor `internal/ft8` touches the logbook DB — preserves the ADR 0013/0024 import-graph invariant. Division of labour: SM ranks/caps (≈50–200, mix of heard-this-session + worked-on-band/mode + needed + watchlist); go-ft8 scores + tries top-K (≈2–4) AP hypotheses BP-only. go-ft8 copies/caps the hints + does cheap per-candidate known-bit scoring but **never ranks** — ranking is all SM. **Four separable pieces (ADR 0025):** (1) go-ft8 AP value API + scoring/top-K/diagnostics, (2) `internal/ft8` keeps a recent-heard set in-subsystem, fed as `APCallHints` (stateless; the long-lived `Decoder` is a later optimisation), (3) `internal/ft8hints` provider (blend recent-heard + worked-band/mode + watchlist/needed + later spots), (4) `cmd/smd` injection à la `captureSource`. **Piece 1 (go-ft8 API) shipped in v0.2.0; no in-place `SetAPCallHints` mutator, so AP works in the stateless path — the stateful decoder is now an optimisation, not a gate.** Smallest useful next increment = piece 2 (Service-held recent-heard set fed as `APCallHints` to the existing stateless decode; no DB, no decoder refactor), live-A/B-able like OSD was. Pieces 3–4 (logbook provider + injection) follow. Deferred — operator chose bump-only in session 124. See ADR 0025 + memory `project_sm_ft8_integration`.

- **FT8 TRANSMIT — DECIDED 2026-06-06 (ADR 0029, Accepted); steps (a)/(b)/(c) SHIPPED (2026-06-07), (d) NEXT.** Reverses the old "FT8 TX not in v1" stance (you can't complete an FT8 contact receive-only). **Design:** daemon-owned TX, layered **tones → GFSK audio → audio-output device → PTT → slot timing**, reusing the ADR 0027 guaranteed-stop discipline (`tx_on`/`tx_off` controller-only, never `exposed`). **Manual sequencing FIRST** — operator advances each rung of the fixed CQ→73 ladder; **auto-sequence deferred to a later ADR** (strict superset: same plumbing + an unattended state machine; manual de-risks the TX chain on real RF first). **Library seam:** go-ft8's `EncodeStandardMessage` returns the 79-symbol tone sequence and deliberately stops there (audio/scheduling/PTT/I-O are SM's), standard structured messages only (no free text / compound calls yet). **De-risking lever:** the encode→modulate chain is offline round-trip-verifiable against the shipped decoder (zero RF) before any audio device/PTT exists. **Invariant evolution:** "a decode is NOT a QSO" → "a completed *exchange* is a QSO"; `internal/ft8` imports `qsoservice` (never reverse) so narrow-daemon-scope (ADR 0013) holds by import graph. **TX-frequency selection** = a per-slot spectrum **occupancy / clear-offset picker** (one averaged FFT/slot via the retained CGO-free `internal/audio/realfft.go` + decode `FreqHz`), NOT a rendered waterfall — occupancy is data, not pixels. New audio-**OUTPUT** path mirrors the malgo capture seam (CGO-only, fail-soft, probe-listed device) → live TX needs a CGO build, like live decode. **Build order (RX-safe first):** **(a) per-slot occupancy detector (RX-only, useful immediately — the smallest first increment)** → (b) modulator + offline round-trip → (c) audio-output device → (d) PTT/slot controller → (e) manual sequencer + logging, SPA growing alongside; RF only enters at (c). Multi-ADR, multi-session — each layer may spawn its own ADR. See ADR 0029 + memory `project_sm_ft8_integration`.
  - **Step (a) PROGRESS (2026-06-07):** the per-slot occupancy **detector core is built + wired**. `internal/ft8/occupancy.go` — pure/CGO-free `Occupancy(slot, samples, decodes, cfg) → OccupancyReport`: Hann-windowed Welch FFT (`audio.NewRealPlan(3840)`, 3.125 Hz bins) → median-floor×factor threshold → energy bands; decode `FreqHz` → `[FreqHz, FreqHz+50]` upward-span bands (NOT ±25 — go-ft8's `FreqHz` is the base/sync tone per WSJT-X convention; ADR line corrected and confirmed go-ft8 is right to expose it that way, for TX symmetry); merge (overlap/touch → `both`, conservative) → ranked clear offsets (weights: margin / edge-distance / centeredness, capped at 8). Contract types `OccupancyReport`/`Band`/`SlotRef` + `SlotRefFromTime` (even/odd). Wired into `Service.decodeLoop` (computes per slot, publishes via `LatestOccupancy()` atomic slot) + config `ft8.tx.occupancy.*` (renamed from the ADR's `offset_ranking`; `types.Ft8TXConfig`/`Ft8OccupancyConfig`, pointer-wrapped, zero=default via `resolveOccupancyConfig`). Validated against the real `20m_slot1` corpus slot (26 decodes → 19 bands; both/decode-only/energy-only tiers all firing; suggestions land in real gaps). 14 unit tests + real-slot integration (gated `-short`). **#2 SSE SHIPPED (2026-06-07):** `GET /v1/ft8/events` streams `event: ft8-occupancy` (JSON `OccupancyReport` per slot). Owned by the ft8 subsystem mirroring the bridge: `internal/ft8/occupancy_hub.go` (`occHub` — fan-out + one-slot replay cache, slow-subscriber eviction, ADR 0009 late-subscriber-replay) + `internal/ft8/handler.go` (`HTTPHandler`/`Subscribe`/per-write deadline, no bootstrap poll — replay cache covers late tabs). `decodeLoop` publishes per slot; `LatestOccupancy()` now reads the hub cache; hub closed on `Stop`. Route registered in `api/server.go` only when `ft8Svc.Enabled()` (404→SPA fallthrough otherwise), wrapped in `limitEventSubscribers` (shares the SSE cap with `/v1/events` + `/v1/rig/events`); `api.New` gained an `*ft8.Service` param (cmd/smd + testServer updated). Hub + handler tests incl. `-race`. **SPA display SHIPPED (2026-06-07) — step (a) COMPLETE.** Chosen visual model (operator pick): **compact list, no spectrum strip**. `frontend/logging/src/lib/states/ft8.svelte.ts` — singleton `ft8State` (reactive `connected`/`slot`/`busyCount`/`suggested`/`occupied`), `EventSource('/v1/ft8/events')` listening `ft8-occupancy`, `startFt8()`/`stopFt8()`; null occupied/suggested coerced to `[]`. Stream lifecycle scoped to the FT8 view (`Ft8Panel` onMount/onDestroy — LoggingCard mounts it only when Operating Mode = FT8). `Ft8Panel.svelte`: Band Activity shows `HH:MM:SS · even/odd · N busy` (new `formatUtcClock` in `utils/time.ts`); TX Frequency lists the ranked clear offsets as read-only chips (empty/waiting states handled). 7 `ft8.test.ts` cases (FakeEventSource harness mirroring `bridge.test.ts`); lint/check/format/build all green. Read-only — clicking a clear offset to drive TX is **step (e)**; `occupied` bands carried in state but not rendered (reserved for step e / a future strip). install.md `tx.occupancy.*` knobs still deferred until the picker is interactive.
  - **Live-data validation + refinements (2026-06-07, dogfooding):** detector confirmed accurate against WSJT-X (855 occupied via decode@809; "2341 clear" was a weak decoded station at 2338 — decode tier protecting a station the waterfall barely shows). Added: **energy min-width gate** (`minEnergyBandHz`≈12 Hz, drops single-bin noise slivers; decode/both bands never gated) and a configurable **guard margin** (`ft8.tx.occupancy.guard_margin_hz`, default 10 Hz, 0=off, `*int` so explicit-0 survives resolve) so suggested offsets never sit flush against a neighbour. **Step-(e) picker decided:** a clickable occupancy **strip** (static per-slot, busy shaded / clear selectable — NOT a scrolling waterfall) **alongside** the Clear Slots list; daemon TX gate refuses/snaps overlapping offsets (good-practice enforcement vs WSJT-X's click-anywhere; best-effort at pick time). **New `docs/ft8.md`** captures the whole FT8 picture (enable/build/config/SPA/detector/TX roadmap). Build/workflow: **dev `task` builds (run/run:smd/build/build:smd) pinned CGO-on** (live FT8 without a deploy — `task run:smd` is the fast loop); `task build:smd:static` + CI's embed gate explicitly `CGO_ENABLED=0` (shipped static shape); operating-mode switch now persists to localStorage (survives reload). See `docs/ft8.md` + ADR 0029.
  - **Step (b) SHIPPED (2026-06-07) — GFSK modulator + offline round-trip, ZERO RF.** `internal/ft8/modulate.go`: `Modulate(tones []uint8, offsetHz) []float32` — continuous-phase GFSK (WSJT-X scheme: Gaussian freq pulse BT=2.0, h=1, 6.25 Hz spacing, 1920 samples/symbol, raised-cosine edge ramp), output `(nsym+2)*1920` normalised [-1,1]; `EncodeToSlot(text, offsetHz, dtSec) ([]int16, error)` calls `goft8.EncodeStandardMessage` → `Modulate` → lays into a 180000-sample slot. Tone geometry hardcoded (go-ft8's `ft8SamplesPerSymbol`/spacing are unexported — ADR 0029 export-later note stands). **Round-trip PROVEN:** `TestModulate_RoundTrip` encodes 6 messages across the CQ→73 ladder at 300–2900 Hz → modulate → `DecodeSlot` → text + freq (±2 Hz) recovered every time; `TestModulate_RoundTripOccupancy` confirms a generated signal marks its own slot busy in the step-(a) detector. Cheap shape/empty/length/reject tests un-gated; decode round-trips gated `-short`.
  - **Live DECODE FEED (Band Activity) SHIPPED (2026-06-07)** — RX-display, independent of the TX build order. Decodes were previously only logged; now published. Daemon: ft8 hub generalised from occupancy-only to a multi-event fan-out (`occupancy_hub.go`→`hub.go`, `hubEvent{name,payload}`, per-type replay cache — the bridge pattern), new **`ft8-decode`** SSE event on `/v1/ft8/events` carrying `DecodeReport{slot, decodes:[{text, freq_hz, dt_s, snr}]}` (`snr` added session 162 once go-ft8 v0.3.0 exposed it). `decodeLoop` publishes decode + occupancy per slot. SPA `ft8.svelte.ts`: `ft8State.decodes` rolling history (newest-slot-first, freq-ascending within slot, cap 100, monotonic-id keys), listens `ft8-decode`, cleared on stop. `Ft8Panel` Band Activity box renders the scrollable list (operator chose **accumulate/scrollback**, WSJT-X-like, over per-slot-replace); operator has restructured the panel (Main Freq / Band Activity / TX Frequency / Clear Slots columns). Go hub+handler tests incl. `-race`; 12 `ft8.test.ts` cases; lint/check/build green. **Temporary validation view (2026-06-07):** the (otherwise empty until step e) **TX Frequency** panel currently renders `ft8State.occupied` as an "Occupied (Hz)" list with each band's source+level (`both 0.91` / `energy 0.06` / `decode`) — added to debug an operator report that a known-clear freq (855 Hz, per live WSJT-X) wasn't in the suggestions. Diagnosis pending live comparison: if 855 isn't in any occupied band it's purely the ranked top-8 cap/edge-weighting crowding low-freq clear slots out (relax ranking); if it shows `energy 0.0x` it's a threshold false-positive (raise `threshold_factor`); if `decode`, it's real. Step (e) reclaims this panel for the TX picker.
  - **Step (c) SHIPPED (2026-06-07) — audio-OUTPUT device, AUDIO ONLY (no PTT, RF-safe).** `internal/audio/playback/` — the output mirror of `internal/audio/capture`: a malgo/miniaudio **S16, 12 kHz, mono** `Player` behind `//go:build cgo` (`playback.go`), with the pure callback core (`fillFrame` copy+silence-pad, `bytesAsInt16` zero-copy) in an **untagged** `buffer.go` so it's unit-tested in the CGO-free lane (`buffer_test.go`, 7 cases); `doc.go` carries the package clause on the static build. Lifecycle `New → Init → Play(samples) → <done channel> → Stop / Close`: `Play` is **non-blocking** and returns a channel closed when the whole waveform has been handed to the device (natural end); **the caller owns the stop** (`Stop` halts immediately) — exactly the guaranteed-stop discipline step (d)'s controller inherits. The int16 from `ft8.EncodeToSlot` streams straight in (no float conversion, unlike capture's f32→i16 seam). Integration tests gated `integration && cgo` (real hardware: init/list/play-to-completion/stop-mid-waveform). **Config:** `types.Ft8TXConfig.Device` (`ft8.tx.device`, string index, separate enumeration from capture `ft8.device`, system-default when empty). **Smoke tool `cmd/ft8-tx-probe`** (`//go:build cgo`): `-list` enumerates playback devices for `ft8.tx.device`; `-msg=… -offset=… -dt=… [-wav=…]` encodes a standard message and plays it (optionally writes the slot WAV for an A/B decode back through `ft8-decode-file`/`jt9`) — **drives a sound card, not the rig; no PTT, no RF.** All builds green: CGO-free helper tests + static build, CGO build of playback + probe + all `cmd/...`, gofmt/vet clean, full `internal/ft8` + `internal/audio/...` suites pass. **Actual RF first enters at step (d)** (the original "RF at (c)" framing refined — (c) is sound-card audio; PTT keying is (d)). **NEXT (TX): step (d)** — PTT + slot-timing controller (daemon-owned guaranteed stop: key TX via the controller-only `tx_on`/`tx_off`, start `Player.Play` aligned to the slot boundary at +0.5 s, hard-stop on slot end / disconnect / single-flight, mirroring ADR 0027's tune controller).

- **DX cluster integration — idea, needs a discussion (flagged session 123, 2026-06-02).** Receive spots (a telnet DX-cluster / DXSpider feed) and possibly send spots (self-spot / spot a worked station). Not yet scoped — the point of the note is to *have the conversation* before any design. Why it's on the list: (a) "spotted recently" is a named AP-hint source in ADR 0025, so a spot feed directly feeds FT8 AP recall; (b) spots are broadly useful to the logging UX (live band activity, DX/needed alerts). Open questions for the discussion: is this a **daemon subsystem** (a long-lived network connection emitting spots over SSE, shaped like the bridge — consumed by the SPA) or a client feature? How does it respect narrow-daemon-scope (ADR 0013) — spot *reception* is arguably ingest-like, but "needed/award" highlighting and self-spotting touch the logbook, which is logbook-app territory per `feedback_logging_vs_logbook_scope`. Protocol/auth (cluster login by callsign), spot filtering, and dedupe also need deciding. No ADR yet — discuss scope first, then decide whether it's one initiative or split (rx feed vs tx self-spot).

- **Inbound CAT command path — DAEMON-SIDE SHIPPED (ADR 0026 Accepted, session 126); SPA pending.** ⚠ See the **Session 126** entry above for the full state. Daemon-side is done + tested (data-driven `cat` commands, `bridge.SendCommand`, `POST /v1/rig/command`, `BridgeInfo.ops`); implementation committed in `5e8af9b7`, capability unit + docs pending commit. Remaining: `ft8.bands` config, SPA FT8 card, SPA i18n codes for the new HTTP error codes (confirm-by-push validated on the FTdx10 2026-06-04). The planning pass + new ADR this bullet used to ask for are **done** (ADR 0026). The rest of this bullet is the original framing, retained for context: Flagged session 66 (2026-05-16) when "Ctrl+\\ VFO swap" surfaced as a deferred polish item. Operator's mental model: keyboard shortcuts work consistently across manual AND CAT modes (no other shortcut is gated by CAT state). Implementing Ctrl+\\ as manual-only would be surprising UX. Implementing it for CAT mode opens the v1 inbound-command path that ADR 0019 explicitly deferred. Natural scope at that point isn't just VFO swap — it's the full v1 SPA-drives-rig surface: set selected VFO, set split on/off, set frequency, set mode. (PTT stays deferred per ADR 0019 — separate concerns: per-connection asserted state, disconnect-safety-release, future arbitration.) Requires: bridge command-write methods, daemon HTTP endpoint shape (`POST /v1/rig/cmd` or per-field), rigdef SET-command encoders (currently only INIT + READ are encoded), error handling for rig-rejected commands, multi-rig awareness from day one. **Deliberately parked** so dogfooding the existing read-only surface surfaces what actually needs SET-side support and in what order. ADR 0019's "Triggers to revisit — The SPA needs to drive the rig" already captures this. When this gets picked up, expect a planning pass + new ADR before code.

### The immediate next action (post-review, pick a phase)

QRZ port complete, review triage complete, Task #29 (cmd/smd/main.go
tests) complete in session 14, SSE event stream complete in session
14. The forwarding subsystem + its live notification surface is
**done** — the next session picks one of three directions below.

My standing recommendation is a **daemon-only alpha checkpoint**:
cut a tagged build, dogfood via curl + SSE + the existing HTTP
endpoints, and use the results to inform the next subsystem
choice (a second real forwarder vs. bridge/CAT vs. client work).
The forwarding + events surface is the minimum viable
daemon-side feature set; running it against real QSOs for a
week will surface gaps cheaper than guessing at the next
subsystem. If alpha feels premature, the second-best option is
a second real forwarder (ClubLog or LoTW) — it validates the
"prefix-agnostic plumbing" claim and gives the SSE stream more
to say. Bridge/CAT is a larger effort with its own design doc
still to write.

The 8-stage QRZ plan is retained below for historical context;
do **not** re-derive the design decisions captured in it.

**QRZ API reference** (from the operator's paste of QRZ's developer
guide — use this, not an inferred version):

- Endpoint: `https://logbook.qrz.com/api`, HTTP POST with
  `application/x-www-form-urlencoded`.
- User-Agent header required (≤128 chars, should include callsign
  + app name for identifiability).
- **INSERT**: `ACTION=INSERT`, `KEY=<apikey>`, `ADIF=<single-record>`.
  Response: `RESULT=OK|FAIL|REPLACE` + `LOGID` + `COUNT`.
- **UPDATE**: no native update — use `ACTION=INSERT` +
  `OPTION=REPLACE`. Response `RESULT=REPLACE` when it overwrote a
  duplicate. This is what v1 did.
- **DELETE**: `ACTION=DELETE`, `LOGIDS=<id>` (comma list for many).
  Response: `RESULT=OK|PARTIAL|FAIL` + `COUNT`.

**Resolved design decisions** (don't re-open):

- **`Forwarder.Submit` signature**: `(ctx, qso, action, priorUpstreamID string)`
  (stage 1). Worker populates `priorUpstreamID` from the prior
  insert row's `upstream_id` for delete actions only.
- **`Forwarder.AdifPrefix()`** (stage 1). QRZ returns `"QRZCOM"`.
  Worker stamps `QRZCOM_QSO_UPLOAD_STATUS="Y"` +
  `QRZCOM_QSO_UPLOAD_DATE=today` on success (insert/update, not
  delete — soft-deleted QSOs don't export). Failures/transients
  stamp nothing.
- **Delete LOGID wiring**: option A from the session-12 discussion.
  Worker does a DB lookup before `Submit`; forwarder receives LOGID
  via `priorUpstreamID`; empty lookup → terminal "no upstream id
  for delete".
- **QRZ credentials shape**: `{"api_key": "..."}` only — QRZ
  enforces the callsign/logbook match server-side, so a local
  `callsign` field would only introduce drift risk without a
  guarantee. (stage 2, landed)
- **QRZ response classification** (stage 3, landed): per-action
  matrix in `response.go` and `forwarding-implementation.md` §8.1.
  Short form: `RESULT=AUTH` → Terminal (global); `RESULT=OK` /
  `RESULT=REPLACE` → Success with `UpstreamID = LOGID`;
  `RESULT=FAIL` on delete → **Success** (idempotent);
  `RESULT=FAIL` elsewhere → Terminal; `RESULT=PARTIAL` / unknown
  on any action → Terminal; missing `LOGID` on claimed-OK insert →
  Terminal. Transport-level errors (HTTP 4xx/5xx, network, timeout)
  are classified at the `Submit` call site in stage 4 — network
  and 5xx/429 → Transient, 4xx → Terminal.
- **Retry-defaults ownership** (stage 7): each forwarder package
  exports `var DefaultRetry types.RetryConfig`.
  `spawnForwarderWorkers` in `cmd/smd/main.go` looks it up by type.
  Delete the `defaultForwarderRetry` temporary fallback.
- **Test creds**: operator has a QRZ test logbook with `USER` and
  API key in env vars. Used for manual integration verification
  after code lands — **not** for automated tests.
- **Automated tests**: `httptest.NewServer` everywhere, hermetic
  and CI-safe.

**Remaining stages** (each is a committable unit):

| # | Stage | Status |
|---|-------|--------|
| 1 | Extend `Forwarder` interface (`AdifPrefix`, `priorUpstreamID`) | **done** (session 12) |
| 2 | `internal/forwarding/qrz/` skeleton — credentials struct (`api_key` only), `New`, `Type()="qrz"`, `AdifPrefix()="QRZCOM"`, registry init, stubbed Submit, validation tests | **done** (session 13) |
| 3 | Response parser + classification function — `parseResponse` + `classifyResponse` with per-action helpers (`classifyInsert`/`Update`/`Delete`); `AUTH` global, single-LOGID-delete `FAIL` → Success; 26 unit tests | **done** (session 13) |
| 4 | Insert + update `Submit` — real HTTP, `buildForm` + `classifyHTTPStatus`, `DefaultEndpoint`/`DefaultHTTPTimeout`/`UserAgent`, package-internal `newWithEndpoint`; 18 httptest tests + live harness (`TestLive_InsertThenUpdate` quick, `TestLive_InteractiveFlow` with `/dev/tty` pauses); live-validated against real QRZ | **done** (session 13) |
| 5 | Delete `Submit` + worker LOGID lookup — `FetchInsertUpstreamIDWithContext` (defensive ORDER BY, UNIQUE-constraint-aware), worker `resolvePriorUpstreamID` short-circuit, QRZ `buildForm` delete branch; CI fix for `:memory:` + `-race` flake (DSN `cache=shared`); live harness delete via `Submit` | **done** (session 13) |
| 6 | ADIF-stamp wiring — `MarkUploadSuccessWithAdifStampWithContext` writes both the qso_upload transition and a `json_set` stamp on `qso.additional_data` in one tx (no new columns; matches the "additional_data absorbs ADIF spec evolution" invariant); worker `markSuccess` dispatch gates on AdifPrefix + action; prefix-agnostic so new forwarders land without sqlite/migration changes | **done** (session 13) |
| 7 | Retry-defaults ownership refactor — per-forwarder `DefaultRetry` vars, `forwarding.RegisterDefaultRetry` / `DefaultRetryFor` registry companions, `spawnForwarderWorkers` lookup-by-type + loud error for missing defaults, hardcoded `defaultForwarderRetry` deleted | **done** (session 13) |
| 8 | Import `internal/forwarding/qrz` in `cmd/smd/main.go` (regular import — main sets qrz.UserAgent); wired `qrz.UserAgent = "station-manager/" + Version` and `adif.ProgramVersion = Version` at the top of run(); flipped `adif.ProgramVersion` from const to var; ldflags smoke-check passes | **done** (session 13) |

### Follow-ups after the QRZ port

1. **Alpha checkpoint.** Tag a build, dogfood the daemon against
   real QSOs for a week: ingest via `POST /v1/qso` (curl or a
   disposable script), QRZ forwarding on, SSE stream tailed with
   `curl -N` or a browser `EventSource`. The forwarding +
   events surface is the smallest self-contained daemon-side
   feature set; real use will surface gaps cheaper than guessing.
   **My standing recommendation for the next phase.**

2. **A second real forwarder (ClubLog / LoTW / eQSL)**. Exercises
   the "prefix-agnostic generic plumbing" claim. Would validate
   the registry + `DefaultRetry` ownership pattern in anger. Also
   a good smoke test for whether the stage-6 ADIF-stamp json_set
   generalises as cleanly as we think it does.

3. **Bridge / CAT design — substantial progress session 15, now at a
   decision point.** Design is in `docs/v2-design/bridge.md`, rewritten
   in-session from a two-frontend shape to a much smaller Unix-socket-only
   SM-internal multiplexer. The live question is **§6 YAGNI: build now or
   defer?** User lean at session end is *defer*, with `internal/cat` given
   a pluggable transport abstraction (§8.3) so the deferred path costs
   nothing. Recommended next-session work order:

   **a. Answer §6.** Everything else depends on this.
   **b. If deferred:** settle §8.3 (`internal/cat` transport abstraction
      shape) as a design-only exercise. This unblocks the logging app for
      milestone 2 without foreclosing the bridge.
   **c. If built now:** sequence is (i) `internal/cat` transport abstraction,
      (ii) NDJSON schema (§8.1), (iii) bridge implementation, (iv) logging
      app wired through `SocketTransport`, (v) defer CAT control app to its
      own design session.

   My recommendation: **defer the bridge, but do §8.3 now.** Keeps the
   logging app on the fastest path (direct `SerialTransport`) and makes the
   eventual switch to a bridge mechanical.

### Parked follow-ups (low priority, not blockers)

- **Dead-method sweep (SQL audit item 3).** Several sqlite methods
  have only test callers today. The former forwarder-queue
  candidates (`FetchPendingUploads`, `UpdateQsoUploadStatus`) have
  already been deleted in session 11 — they were v1 worker code,
  replaced by the stage-6 purpose-built methods. The remaining
  low-signal methods
  (`FetchQsoSliceByLogbookId`,
  `FetchQsoByDedupeKey`'s no-context wrapper,
  `FetchContactedStationByCallsign`, `FetchCountryByCallsign`,
  `FetchCountryByName`) still need a specific "delete or keep"
  decision. Enrichment methods likely return in milestone 2; the
  QSO list helpers may be dead. Park until we know.
  `FetchQsoCountByLogbookId` removed from this list session 67
  (2026-05-17) — gained a real caller via the new
  `handler_logbook_count.go` for the LoggingCard header badge.
- **SQL audit item 4** — optional `(call, logbook_id) WHERE
  deleted_at IS NULL` composite for contact-history with
  `?logbook=` filter. Defer until a concrete performance
  complaint surfaces.

### v2 design work

- **Pick the ORM/generator approach** → `docs/v2-design/db-layer.md`.
  sqlboiler stays until there's a reason to change.
- **Multi-rig as first-class assumption** — bridge-side shape now
  captured in `docs/v2-design/bridge.md` (first-class from day one
  in the bridge). Data-model side (rig id on `types.Qso`, logbook
  schema impact) still open; address when rig control construction
  starts.

### Deferred features

- **Logging-app text-file fallback reconciliation** — milestone 2+.
- **Enrichment / contacted_station population** — milestone 2.
  Client-side concern; daemon submit path stays fast and network-free.
- **Daemon dashboard / monitoring UI** — post-milestone 2.

### v1 branch follow-ups

- Data race candidate fix (session 6) not yet verified on v1 branch.
- Hardcoded QRZ forwarder — v2 concern, unlikely to be fixed on v1.

### Maintenance

- Update this file at the end of every session.
- **Roll-off:** when the live `### Session N` list passes ~15 entries, move the
  oldest block into `session-handoff-archive.md` (newest-first, verbatim). Last
  roll-off: 2026-06-30 (Session 180 → archive; live kept 181–195). Prior:
  2026-06-24 (Sessions 172–175 → archive; live kept 176–190).
