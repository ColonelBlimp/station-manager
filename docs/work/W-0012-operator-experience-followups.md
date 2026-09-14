# W-0012 — Complete routed operator-experience follow-ups

**Status:** Open — staged after release correctness gates
**Selected:** 2026-09-12 — the "Rig Control mode control in the FT views" slice only; the rest stays staged
**Outcome:** Remaining UI, map, onboarding, and diagnostic improvements have one routed home and do
not compete with the app-shell, notification-history, or UI-cohesion dossiers.

## Routed outcomes

- [W-0001](../archive/work/W-0001-durable-notifications.md) owns durable notification history; ADR 0060 owns alert
  placement and ADR 0061 the proposed structured event store. Never expose `smd.log` as the event
  history.
- [W-0003](../archive/work/W-0003-retire-legacy-operator-spas.md) owns canonical app routing and remaining shell
  consolidation; [W-0004](../archive/work/W-0004-complete-app-ui-cohesion.md) owns themes, occupancy colors, and
  ambient build identity.
- **Operate workflow:** reorganize the contact view; decide whether QTH belongs on the Phone/CW
  card; surface active-versus-configured rig state; revive only dead SSE clients on visibility
  changes; retain focused fixes for session email/filter/toolbar feedback and card layout.
- **Start a new session (operator idea, 2026-09-12; not selected):** the session is a per-tab browser
  list, not a daemon entity (ADR 0049 rejected daemon-owned sessions): `session.svelte.ts` keeps
  `session.qsos` in `sessionStorage` under `sm.session.qsos`, fed by the Phone/CW submit response and
  the FT8 logged event; the timer keeps its own `sm.session.startedAt`; the email and export routes
  take the SPA's UUID list. A new tab therefore starts a fresh session and a daemon restart does not
  end one. Today's operator path: open a brand-new tab by typing the URL (not a link from the app, and
  not a reopened tab — Firefox restores `sessionStorage` on undo-close and session restore), confirm
  the Session panel count is 0 and the timer reads 00:00:00, then close the old tab (one tab only).
  The feature: a "New session" action in the Session panel that clears the list and the timer in
  place, SPA-only, no daemon change, no data-derivable-from-QSOs trigger for ADR 0049's revisit.
  Acceptance: the list, its rail badge and the timer reset together and stay reset across a reload
  (the stored copy is removed, as `_resetSessionForTests` does); the logbook is untouched and the
  wording says so; a session holding not-yet-emailed QSOs asks first (email or start anyway) — the
  nearest confusable outcomes are a reset that reads as deleting QSOs, a reload that resurrects the
  old list, a timer that keeps counting because it read its start once at mount, and the FT8 Band
  Activity "worked this session" mute silently emptying (`Ft8BandActivity.svelte` reads
  `session.qsos`; expected, and to be stated). Explicit operator action only — no idle or day-change
  rollover. The unwritten session-log manual chapter (W-0018) must carry the per-tab rule.
- **Selectable and creatable data files — physical log databases on disk (operator requirement,
  2026-09-12; not selected):** trigger: a UI test on the live station logged a real contact (7Q7EB,
  2026-08-06, alpha.2 Finding #19) into the only log and mirrored it to SM Cloud. The operator wants to
  create separate database files and choose which one is open — a test file, a contest file, the
  everyday log — the way v1's data files worked; logical logbooks inside one file are not the ask.
  Baseline: one log database at `<data_dir>/db/station-manager.db` holding QSOs, logbooks, the upload
  queue and the audit history, beside a shared `reference.db` for the enrichment caches (the split
  already exists, `sqlite/bootstrap.go`); `datastore` is a restart-required setting (config.md §11.4)
  and hot-swap is unimplemented even for rigs, where ADR 0028's catalogue-plus-active shape is the
  precedent; the handle is held by the API server, `qsoservice`, the forwarder worker, the SM Cloud
  reconciler and the notification history. Inside a file the logical layer is already there: logbook
  CRUD on `/v1/logbook` (name and callsign required, delete refused while it holds QSOs or is the
  default), `logbook_id` on every QSO, ADR 0055's callsign-per-logbook, `smd import`/`restore
  --logbook`; what that layer lacks is a UI, a writable active logbook (`default_logbook.id` is a hand
  edit plus restart) and the ADR 0056 per-logbook forwarder bindings, designed but unbuilt.
  Operator-observable outcomes for files: Settings → Data files lists the files under `<data_dir>/db/`
  and marks the open one; "New" creates an empty file from a validated name, migrated to the current
  schema and seeded with its default logbook and callsign by the first-run rules; "Open" switches the
  daemon to it, refused while FT8 is armed, a session is active or a transmission is in flight, and
  while uploads are in flight; after the switch the header names the file, its active logbook and
  count, the tab starts a new session (see the new-session entry), the upload queues are the file's
  own, and forwarding for the new file is OFF until the operator binds it — SM Cloud in particular must
  name its own cloud logbook or stay disabled, never inherit the everyday binding (the reconciler
  treats cloud rows unknown locally as a retentive superset and warns every tick; a later restore
  would pull the everyday log into the test file); backups, `restore` and `import` address a named
  file; the enrichment cache stays shared in `reference.db`. Mechanism, two alternatives for the ADR:
  (A) write `datastore.path` through the config PUT and restart the daemon under systemd — the
  restart-required class the setting already has, the reconnect path every deploy exercises (ADR
  0079); (B) an in-process swap through the ADR 0070 lifecycle graph — drain the dependents, close,
  open and migrate, restart them — which makes W-0009's LC-5 (concurrent SQLite open/close) live.
  Recommended: A first; a switch is rare and B is a lifecycle framework change for a convenience.
  Nearest confusable outcomes: a switch mid-FT8-run filing a contact into the wrong file; a new file
  silently bound to the everyday SM Cloud logbook; per-file copies of the enrichment cache; a file
  deleted or renamed while open; `config-check` validating a path that is not the open file; two
  writers on one file (`smd import` against the open file is already possible today). Decisions for
  the operator: A or B; files confined to `<data_dir>/db/` with validated names (recommended) versus
  arbitrary paths; whether a new file copies the current station identity; forwarding off by default
  for a new file (recommended); whether the logical-logbook UI is still wanted inside a file (files
  give isolation and portability, logbooks give callsign identity — both, files first). Its own
  dossier and an ADR when selected; W-0013 carries the datastore-swap pointer.
- **Cap Band Activity's enrichment lookup concurrency (inbox 2026-09-11 follow-up (b); written up
  2026-09-12; not selected):** the incident: two SPA tabs held six event streams (log and rig per tab,
  the FT8 stream, the Map view's own second log stream), which is the browsers' default of six
  persistent HTTP/1.1 connections per host (the daemon serves plain HTTP, so no HTTP/2), and every
  ordinary request queued behind them — inference from the incident, with instant recovery once a tab
  closed. Today `ft8Enrich.svelte.ts` fires TWO requests the moment a callsign is first seen on a
  band under a profile — `GET /v1/enrich/callsign` (a country-cache hit is local; a station-cache miss
  goes upstream to hamnut or QRZ, over a second each in the 2026-09-11 log) and the worked-before
  `GET /v1/contest-dupe` (a local read) — deduplicated per key but with no queue and no limit, so a
  busy FT4 slot with fifteen new calls fires thirty requests at once and occupies whatever connections
  the streams left. The change, SPA-only: a scheduler in that module with at most N lookups in
  flight; newest slot first, stations calling us or listed as answerers ahead of plain CQ rows, and a
  lookup for a row that has scrolled off unheard is dropped; the pending queue is aborted when the FT
  view closes, the profile switches or the tab closes (the route already honours the browser's abort
  signal); the worked-before check stays outside the budget (cheap, and it drives the grey-out); the
  Phone/CW card's own Tab-out lookup never routes through the limiter. Operator-observable acceptance:
  during a busy slot an operator action (answer, work, map, count) is not delayed by decoration; the
  daemon access log shows no more than N concurrent enrich requests from the tab; flags fill for the
  current slot before older ones; no enrich request lands after the FT stream closed; the grey-out
  still appears within the slot. Nearest confusable outcomes: a cap that also throttles the
  worked-before check; a queue that never drops, so stale rows still consume budget minutes later;
  lookups continuing after the view closed; the logging card's lookup queued behind decoration.
  Decisions for the operator: N (suggested 2); the queue cap before old entries drop; whether
  answerers jump the queue. **Ruled and built 2026-09-13:** N = 2 (`FT8_ENRICH_CONCURRENCY`); no
  numeric queue cap — a pending lookup not observed in the next Band Activity pass has scrolled off and
  is dropped; stations calling us jump the queue, then newer slots; the worked-before check stays
  outside the budget; `clear()` on view close aborts in-flight lookups (AbortSignal through
  `apiEnrich`) and discards late answers. `ft8Enrich.svelte.ts` scheduler + `Ft8BandActivity.svelte`
  pass brackets; tests on a controllable enricher (cap, priority, drop, abort). Codex review of `1fe16e2b` (P2,
  fixed 2026-09-13): dispatch ran mid-pass, so the first rows observed filled the cap before a caller later in
  the same pass (cq_to_top lists CQ rows first) was seen — a pass now only enqueues and `endPass()` dispatches
  over the whole of it; outside a pass (markWorked's re-kick) dispatch is immediate. Tests on a fake enricher with controllable promises in the module's
  existing test file: never more than N in flight, the order, abort on close. Alternative, larger:
  the daemon stamps the cached country onto each decode line as it publishes the slot (cache read
  only, never upstream) so Band Activity makes no per-decode request and only misses are warmed in
  the background — a decode-frame contract change. Neither raises the connection budget: with the
  Map reuse (follow-up (a), ADR 0079) a second tab still leaves one spare connection, so one tab
  stands until HTTP/2 over TLS (follow-up (c), to verify).
- **Dashboard review and a configurable landing view (operator, 2026-09-14; not selected):** the
  operator: "review the Dashboard and what is the SPA's default landing page — Phone/CW, FT8, FT4,
  maybe even make this configurable — different users default to different modes of operating and we
  should accommodate that." Today: a bare `/app/` lands on the Dashboard, which is the unbuilt dashed
  placeholder (`App.svelte`); ADR 0044 endorsed it as a lean status home (rig state, forwarding-queue
  health, today's QSO count, quick-nav cards, no `GET /v1/dashboard`) and left `startup_view` as an
  open finer point (`dashboard` | `operate-ft8` | `operate-phone` | `last-used`). The router
  (`router.svelte.ts`) already remembers the last-used Operate mode in browser storage for a bare
  `/operate`, and the sidebar's rail choice is a browser preference (`sm-util`), so "last-used" is
  half built. Acceptance criterion to ratify: opening `/app/` lands on the view the operator chose in
  Settings; a deep link or bookmark still wins; a first run lands on the welcome gate regardless.
  Nearest confusable outcome: a landing preference that fights the remembered last-used mode (two
  memories, one URL) — one of them must own the bare path. Rulings wanted: (1) the option set —
  Dashboard, Phone/CW, FT8, FT4, last-used; (2) where it lives — `config.json` (daemon-owned, follows
  the station across browsers, the ADR 0044 sketch) or a browser preference like the rail (per device,
  no config schema change); (3) whether the Dashboard is built first, stays a placeholder, or gains
  the map as its centrepiece (the 2026-09-14 inbox exchange). Own slice when selected; the Dashboard
  itself may warrant a short ADR update to 0044 with the tile set decided. Same day the operator
  challenged the Dashboard's existence and usefulness. Assessment: every tile ADR 0044 sketched is
  already surfaced somewhere the operator sees more often — rig connection state and dial in the
  header chip, the logbook count in the header, forwarding-queue health under Settings → Forwarding
  with terminal failures in the notification rail, today's QSOs in the Logbook, quick-nav in the
  sidebar — so a status home would restate the shell. What the Dashboard uniquely provides today is a
  landing view that starts nothing: no FT profile claim, no FT8 stream, no audio capture. Phone/CW is
  equally inert, so that role does not need a dedicated view. Footprint if retired: the `View` union
  and the bare-path fallback in `router.svelte.ts`, the sidebar entry and its icon, the placeholder
  branch and title in `App.svelte`, three tests, two code comments that use it as an example of a
  non-FT view; no manual page, no daemon surface, nothing in `config.json`. Recommendation: retire
  the Dashboard (dated update to ADR 0044, which endorsed it "in principle"), land a bare `/app/` on
  the last-used Operate mode (Phone/CW on first use), and reduce the landing-preference option set to
  Phone/CW, FT8, FT4 and last-used. Not a ruling yet — the operator's call. **Ruled 2026-09-14:
  retire the Dashboard; land on the last-used Operate mode for now.** Built the same day: ADR 0044
  amendment (2026-09-14); `router.svelte.ts` (no `dashboard` view; the bare root and unknown paths
  parse to Operate with the remembered mode, URL normalised), `Sidebar.svelte` (entry and icon gone),
  `App.svelte` (placeholder branch and title gone); comments and `docs/ft8.md` that used "a trip to
  the Dashboard" as the leave-the-workspace example now say the Logbook. Tests: router landing (bare
  root → last-used mode; unknown path → Operate), sidebar nav without a Dashboard; the boot boundary
  and first-run title tests unchanged and green. Reversion proof: the three new tests fail on the
  previous router and sidebar. The landing-view preference (option set Phone/CW, FT8, FT4,
  last-used; config versus browser) stays open.
- **The contacts map inside the shell, with a pop-out (operator question, 2026-09-14; not
  selected):** "can the map be a part of the SPA rather than its own tab and allow FT8, FT4 to
  continue working — indeed, Phone/CW to retain its settings — i.e. the Op is running Phone/CW,
  checks the map, and comes back and continues; and could it be forked off into a tab should the Op
  want to operate and view the map at the same time?" Facts: Phone/CW already keeps its draft and
  QSO clock across any view change (module state in `qso.svelte.ts`, the 2026-09-13 draft-age
  build). FT8/FT4 do NOT survive leaving the Operate view: `Ft8View.svelte` closes its stream on
  unmount (`stopFt8` in the mount cleanup), and the daemon disarms TX and abandons any active QSO
  when the last `/v1/ft8/events` subscriber is gone for `captureLinger` (5 s,
  `internal/ft8/service.go`), so an in-shell Map that REPLACES the Operate view in the content area
  would end a run after five seconds, exactly as the Logbook does today. Design that meets the ask:
  a Map entry in the sidebar that renders the map in the shell's content area while the Operate
  workspace stays MOUNTED but hidden (`hidden`, not unmounted), so the FT stream, capture and run
  continue — the presence signal keeps its meaning, the operator is at the same visible tab — and
  the shell-level TX alarm banner and toasts stay visible over the map; plus a pop-out control on the
  in-shell map that opens the existing full-window `/app/map` in a new tab for the second-monitor
  case (that tab now holds two streams after the 2026-09-14 transport change). Nearest confusable
  outcome: an FT8 run continuing unseen behind the map — the header rig chip and the alarm banner
  are the visible signals; the hold, queue and decode surfaces are not. Rulings wanted: (1) keep the
  FT run alive while the Map is shown in-shell (a change to what "leaving the FT view" means, for
  the Map only or for the Logbook and Settings too); (2) the pop-out stays as the second-monitor
  path; (3) whether the sidebar's "Map ↗" becomes the in-shell entry with the pop-out inside the
  map, or both remain side by side. **Ruled 2026-09-14, DECLINED:** "leave the map as a separate tab
  and manage the internal browser connections. I don't want to disturb the FT side of things — it
  works well and this idea may cause more issues." The map stays the full-window second tab; the
  stream budget is the lever (the 2026-09-14 shared `/v1/events` per tab, and proposal (e) on the rig
  stream still awaiting its ruling). Not to be reopened without a new operator ask.
- **Show the picked station's own offset on the Occupancy panel (inbox 2026-09-12; not selected):**
  trigger: FT4 before the contest, A61DD calling CQ at 874 Hz in every odd slot, plain in Band
  Activity and absent from the Spectrum view — read as a missing signal. Not a defect: the panel
  (Channels and Spectrum alike) is the occupancy of the slot the operator TRANSMITS in — idle, the
  Even/Odd "TX slot" toggle; in a QSO, the opposite of the worked station — so the station being
  answered is never in it by construction, and decode-derived bands are never gated (`occupancy.go`
  `decodeBands`). The confusable is real: "Spectrum" reads as a waterfall of the band. The feature,
  SPA-only: when a Band Activity row is picked (an answer, a work, or the active QSO's station), draw
  its footprint `[freqHz, freqHz + width]` on both views as a hollow outline labelled "their signal ·
  other slot" — distinct from the shaded occupants (which are in OUR slot) and from our own tinted
  footprint; in Channels, an outline on the nearest cell. Their offset lives only on the row
  (`DecodeEntry.freqHz`): the answer request carries OUR `offset_hz`, the sequencer does not track
  their audio frequency and the `ft8-qso` frame does not echo it — so the SPA remembers the picked
  row's offset with the pick, or the frame gains `their_freq_hz` (a daemon change, durable across
  tabs and reloads; the `cq_message` precedent). Operator-observable acceptance: with a station
  picked, both views show its outline at its decoded offset while the QSO is active; the outline
  changes no clear/near/sharing grading and no ★ ranking (their signal occupies the other slot —
  answering on their frequency is normal); it disappears when the QSO ends or the pick is cleared,
  and it is not drawn when the panel's snapshot is stale for the band. Nearest confusable outcomes:
  the outline read as an occupant, so a clear offset is avoided; the outline outliving the QSO or
  surviving a band change; a station calling in OUR slot drawn twice (it is a genuine occupant and
  already shaded — same-parity rows get no outline, or the outline sits on the shading and says so);
  the outline drawn from a row that has since moved frequency. Decisions for the operator: which pick
  drives it (the active QSO only, recommended, or any clicked or hovered row); the label; whether the
  Channels view gets it too. Smaller alternative: a one-line cue in the panel header when idle — "the
  slot you transmit in; the station you answer is in the other slot" — and no marker.
- **Maps and tables:** dogfood-validate shipped map catch-up/zoom behavior; decide solar-time overlay
  versus a world-time widget, map band-source policy, and session column resizing/sorting before
  implementation. The whole-log Dashboard map remains separate from the shipped time-window map.
- **Settings and sidebar (alpha.2 dogfood Findings #3, #4, #18):** Delete is disabled on the default rig with
  the reason in its tooltip alone, and the operator sets another rig as default first
  (shipped `a364c21a`, 2026-09-08; deployed and accepted 2026-09-09 — the record's entry 31; the SPA's
  earlier silent repoint of the default to the first survivor is gone; the panel line under the header
  removed by ruling in `17fc6a7e`, deployed and accepted 2026-09-09, entry 33); the rig detail's redundant
  manufacturer · model subtitle is gone (shipped `5e763dc8`, deployed and accepted 2026-09-09, entry 32);
  the sidebar Manual link opens in a new tab (shipped `f0b8e6eb`, 2026-09-07, operator-verified on the
  station).
- **First-run and no-rig defaults (alpha.2 dogfood Findings #8, #11, #12, #16):** the welcome surface's tab
  title reads "Welcome · Station Manager" (shipped `c1045efa`, 2026-09-07, deployed and accepted); the Rig Control card opened from Phone/CW or FT8 sets both
  frequency and mode for that context (no-rig operating-context policy); the header's logbook count
  refreshes on reconnect after a daemon restart or import (shipped `33e975bc`, ADR 0079, and `9b60b65e`, the initial `: connected` comment on every SSE
  stream, 2026-09-07; deployed and verified immediate at the reconnect — the record's entries 29–30). Both rails already default to expanded:
  Finding #12 was closed working-as-designed on 2026-09-07 — the collapsed rail was the browser's
  remembered `sm-util` preference.
- **Onboarding/preferences:** reduce non-Linux first-run friction; add download-site install content
  from the canonical install guide; keep beginner help, profiles, and `default_logbook.id` wiring
  deferred until their consuming workflow exists.

## Built follow-up — the contacts map rides the shell's event stream (inbox 2026-09-11 follow-up (a), built 2026-09-14)

Acceptance criterion: with the Operate tab on FT8 and the map open in a second tab, the map loads
and stays live, and the daemon's SSE subscriber count reads five on opening the Map tab, not six
(the log shows the count per subscribe). Nearest confusable outcome: a map that loads only because
the Operate tab is idle — the count is the observable, not the load. Mechanism: `openLogEvents`
(`frontend/app/src/lib/api/log-events.ts`) shares one `/v1/events` connection per tab behind a
ref-counted subscriber list; the shell opens it at boot and never closes it, the Map view joins it
and leaves it on unmount, and only the last subscriber closes the connection. Joining an open stream
fires `onOpen` at once so the map's stream-then-fetch contract and its catch-up refetch are
unchanged; the reconnection transition is tracked per subscriber (the shell that saw the drop
re-fetches its count, a newcomer that did not is not told to). No call-site change in `main.ts` or
`mapData.svelte.ts`. ADR 0079 carries the dated update with the alternatives weighed. Tests:
`log-events.test.ts` (one EventSource for two subscribers, fan-out, late join open/down, ref-counted
close); `main.boot.test.ts` and the MapView tests unchanged and green. Reversion proof: the four new
tests fail on the previous transport (a second EventSource per subscriber). Proposal (e) — no rig
stream in a Map tab — was ruled and built the same day: `main.ts` opens `/v1/rig/events` only when
the tab did not boot on the map route (the full-window map renders no header chip, Rig panel or
alarm banner, and a Map tab never routes elsewhere without a reload). Budget now: Map tab one
stream, Operate on FT8 three, four across both against the cap of six. Tests: `main.mapboot.test.ts`
(CAT on, map route → log stream only) with `main.rigboot.test.ts` as the positive control (CAT on,
Operate route → both), each importing the real `main.ts`; reversion proof: the map-route pin fails
on the previous shell while the control still passes. The observatory's line-keyed `main.ts`
baseline moved with the comment (`line@489` → `line@494`) in the same commit. Deployed 2026-09-14
(`2.0.0-alpha.2-90-g7126ab64`). Review of that commit: P2 fixed — a tab that boots on `/map` while
first-run setup is still needed is not a settled Map tab (the welcome card covers the route and its
"Open Settings" exit renders the shell without a reload), so the skip now waits for
`setup_complete`; `main.mapsetupboot.test.ts` pins it (map route, setup needed, CAT on → the rig
stream opens). P3 refuted in a code comment: build identity rides the rig stream by W-0004 AC3's
ratified placement, so a Map tab is now the no-CAT case for it — the tab title's DEV marker
refreshes on the next reload, the accepted AC3 behaviour; routing it through the log stream would
widen ADR 0079 without a ruling. Baseline rekeyed again (`line@494` → `line@500`).

## Built follow-up — run surface idle line retired (operator ruling 2026-09-13, on air)

ADR 0067's ratified table gave the auto-mode idle state the line "Your next contact starts a run —
callers worked first come / strongest first". The operator, operating on the deployed build, ruled it
wasted space: the Answer mode selector in the row above already states the mode. The idle auto state now
renders no text; the row keeps its dot and a held height so the fixed three-row structure never reflows.
Every other ratified string is unchanged. `RunSurface.svelte`, tests updated.
The same session: the role label ("Calling CQ", "Working a caller") under the worked call in the
enrichment column duplicated the one on the panel's header bar a few lines above — removed; the header
bar keeps it (`Ft8Operate.svelte`, test pins a single occurrence).

Follow-up 2026-09-14, on the deployed build: with the idle text gone, the row's grey state dot stood
alone and read as a stray bullet in the enrichment section. The dot now renders only beside a state
line (the idle auto state shows neither), so the row still holds its height and the dot returns with
the text it annotates. Test: idle asserts no dot, an armed run asserts it is back; reversion proof by
removing the guard alone (idle test fails on the dot being present).

## Built follow-up — Phone/CW draft kept across a mode switch, age shown (inbox 2026-09-12, ruled and built 2026-09-13)

Ruling: keep the draft across mode switches and show its age; preserve the original Time On; never
silently clear or retimestamp; show the age whenever the draft survives a switch; no expiry threshold.

| | Outcome | Nearest confusable outcome |
|---|---|---|
| AC1 | A draft whose callsign was committed (QSO clock started) survives a trip to FT8/FT4 and back with every field and its original Time On; the card shows "Draft started N min ago — Time On HH:MM:SSZ kept across the mode switch", ticking, however old. | The draft silently cleared, or Time On re-stamped on return. |
| AC2 | A typed but uncommitted callsign (no Time On) survives too and shows no age line — there is nothing to age. | An age line with no Time On behind it. |
| AC3 | Log or Clear ends the draft and the age line with it. | A stale age line on the next QSO. |

`qso.svelte.ts` (`qsoClock.startedAtMs`, `survivedSwitch`, `noteModeSwitchForDraft`, `draftAgeText`),
`modeRestore.svelte.ts` (the router's single mode-change hook calls it, synchronously, before the rig
work), `LoggingCard.svelte` (the age line under Time On). Tests: `qso.svelte.test.ts`,
`LoggingCard.svelte.test.ts` (fake timers).

## Built follow-up — Band Activity typed filter announced (inbox 2026-09-12, built 2026-09-13)

| | Outcome | Nearest confusable outcome |
|---|---|---|
| AC1 | Typed filter set: the header shows "Filter: ZS · N hidden" beside the funnel with its own clear (×); clearing it restores every row without opening the popover. | The funnel's tint as the only cue (what shipped). |
| AC2 | The filter hides every decode: the empty state reads "No decodes match “ZS” — N hidden by the filter". | The quiet-band text "Decodes appear here as slots are received" — which stays for a band with no decodes at all. |
| AC3 | A station calling you is never hidden and never counted. | A caller counted as hidden while it is on screen. |

`Ft8BandActivity.svelte`: the funnel predicate now yields `{rows, hidden}`; the chip and the empty state
read it. Tests in `Ft8BandActivity.svelte.test.ts` (chip + count + clear; all-hidden vs quiet band).
Hide-hashed alone (config) keeps the tint; the chip is for the typed narrowing the operator set.
Codex review of `5cbc6be2` (two P2s, fixed 2026-09-13): the empty state keyed on `groups.length`, which
`cq_to_top` never empties, so the explanation was unreachable there — it now keys on visible rows and the
cq-top branch yields no group for none; and the hidden count conflated hide-hashed removals with the typed
filter — each exclusion is now counted against the filter that made it, the chip carries only the typed
count, and hide-hashed removals are named separately ("N unidentifiable hidden"), also when they alone
empty the feed. Tests for both; reversion proof: fix stashed, both fail.

## Slice — Rig Control mode control in the FT views

**Selected:** 2026-09-12, build after the Africa FT4 DX Contest (ends 2026-09-12 18:00Z); no deploy before.
**Origin:** dogfood inbox 2026-09-11 ("the mode selector is polluted") and the operator's screenshot of
2026-09-12 08:26: the FTdx10's fifteen literals in CAT-code order, eleven carrying a "· mapped name"
suffix, DATA-L and DATA-U both labelled FT4 during the FT4 run, the closed control truncated to
"DATA-U · F".

**Rulings (operator, 2026-09-12):**

- In the FT8 and FT4 views the mode is owned by the profile. The bridge switches the rig to the per-rig
  `ft8_mode` literal before every keyed rung and restores the prior mode after unkey
  (`internal/bridge/ft8tx.go`); first entry and every FT band button assert dial then data mode
  (`modeRestore.svelte.ts` seed, `ft8SelectBand`); no arm gate on the rig's reported literal was found in
  the SPA (search 2026-09-12). A live selector there can only fight the profile, so the Rig Control's
  Mode control becomes a readout in those views.
- FT4 uses the FT8 data literal. No separate FT4 literal and no `DIGI` mapping sentinel: mapping values
  are validated as ADIF modes (`config/validate.go`), the daemon never consults the mapping for an FT
  contact (the profile carries FT8, or MFSK/FT4, into the QSO — `ft8/qsolog.go`), and a sentinel would
  need validation, editor, rigdef-default and config-migration work plus a rule for the no-profile case.
  Declined.
- Settings → Rigs → Advanced → Mode Mappings is untouched by this slice.

**Operator-observable acceptance:**

| | Outcome | Nearest confusable outcome |
|---|---|---|
| AC1 | FT8 or FT4 view, CAT connected, the rig reporting the configured data literal: Mode shows the profile with the literal, e.g. "DATA-U · FT4", in full; nothing opens on click. | A disabled select still reading "DATA-U · F"; a readout naming the mapping's FT8 while FT4 is open. |
| AC2 | Same view, the rig reporting any other literal (seed refused while transmitting, the no-move knob off, a hand change on the rig): the readout shows the reported literal and says it is not the profile's data mode; clicking the current band's button puts the rig on that band's dial and the data mode. | A readout that says FT4 while the rig reports USB; a band button that is a no-op on the band already selected (read-verified 2026-09-12: `setFreq` always writes and `ft8SelectBand` then asserts the mode — pin it with a test). |
| AC3 | The per-rig `ft8_mode` set to `""` (leave the rig's mode alone, config.md §10): the FT views keep the live selector. | A readout everywhere, leaving no in-view way to set the mode for that configuration. |
| AC4 | CAT off or lost in an FT view: the readout names the profile; an FT contact's logged mode is unchanged (it comes from the daemon's profile). | The manual nine-mode select shown as if it drove the FT log's mode. |
| AC5 | The Phone/CW view is unchanged by this slice: live and manual selectors as shipped. | The readout leaking into Phone/CW because the FT profile label persists across navigation. |
| AC2b | The daemon's own tune carrier is keyed (rig reports RTTY-U): the readout reads "RTTY-U · tune carrier — the mode from before the tune returns when it stops", neutral border, no band-pick hint (inbox 2026-09-13, built the same day; codex 13d95084 P2: the tune restores the daemon's pre-tune snapshot, not necessarily the data mode, so no specific mode is promised). | The mismatch wording and hint during a tune, inviting a freq + mode write under a keyed carrier. |

**Mechanism (for the builder):** `RigPanel.svelte` already receives `ftMode` and `modeLabel` from
`Operate.svelte`; render the readout when `ftMode` is set, `rig.cat === 'connected'` and
`ft8ModeLiteral() !== ''`, comparing `rig.modeLiteral` with `ft8ModeLiteral()`. Rendered tests in
`RigPanel.svelte.test.ts` for every row above, each with a reversion proof; no daemon change; deploy
with `task deploy:local:dev` when the operator directs.

**Built 2026-09-13** (`RigPanel.svelte`, `rig.svelte.ts`): `ftReadout = ftMode set && (CAT not live ||
ft8_mode literal configured)` renders a `role="status"` readout in place of the select — CAT live on the
literal: "DATA-U · FT4" from the VIEW's label (never the mapping's FT8 while FT4 is open); another literal:
the literal plus "not the FT4 data mode (DATA-U) — pick the band to set it" on an amber border; CAT off or
lost: the profile name; `ft8_mode` "" keeps the live selector. The configured literal became `$state` so
config landing after mount flips the selector into the readout. Tests: AC1–AC5 rows plus the late-config
case in `RigPanel.svelte.test.ts`; the same-band pin in `rig.svelte.test.ts` (`ft8SelectBand` on the band
already selected still writes set_freq then set_mode); `ambientPanels.test.ts` FT8/CAT-off case updated to
the readout. Reversion proof: component change stashed, AC1/AC2/AC4 and the late-config test fail. Frontend
gates: lint, Prettier, svelte-check, full Vitest. Not yet deployed (one deploy with the W-0011 change).

**Follow-on, awaiting rulings (Phone/CW selector tidy, a separate slice):** the list is the rigdef's
MAINMODE table in CAT-code order (`cat.RigModes`) and `modeOptionLabel` suffixes every literal whose
mapped name differs. Open choices: (1) suffix only on the data literal (recommended) or only when
neither string prefixes the other; (2) group by family (SSB, CW, RTTY, DATA, FM, AM, PSK) in the SPA;
(3) a closed control wide enough for its label; (4) whether the FT4 relabel of the data literal
persists into Phone/CW after an FT4 session (ruled 2026-09-11, `c12a8901`) or is confined to the FT
readout and the header chip; (5) showing the data literal as "FT8/FT4" in Phone/CW when no profile
label stands — recommended hold. Hiding rarely used variants would need a per-rig configured mode
list, like operating bands, and is not proposed.

## Verification boundary

Every slice states its operator-visible outcome and nearest confusable state first. Frontend work
runs the affected SPA's lint, format check, Svelte check, and Vitest suite. Layout fixtures must make
overflow, hidden-tab recovery, focus ownership, or stale state observable; screenshots alone are not
the acceptance test.

## References

- [`ADR 0060`](../decisions/0060-operator-alert-surfaces-and-stuck-tx-overlay.md)
- [`ADR 0061`](../decisions/0061-consolidated-operator-event-log.md)
- Expanded pre-decomposition inventory: `d0391ed7:docs/backlog.md`.
