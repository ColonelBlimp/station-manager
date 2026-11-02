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
  dossier and an ADR when selected; W-0013 carries the datastore-swap pointer. **Ruled 2026-09-19:
  files first** — ADR 0071 Accepted (dated decision); the archive programme is step 3 of the
  execution order recorded in W-0014 and the backlog, after alpha.3 acceptance and W-0010 outcome 9;
  it opens its own dossier when selected. Settings → Logbooks (logical logbooks inside the active
  archive) follows it as step 4.
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
  previous router and sidebar. The landing-view preference was **ruled 2026-09-19:** a browser-local
  preference (like the rail choice, no `config.json` schema change) with the option set Last used
  (default), Phone/CW, FT8 and FT4; the first-use fallback stays Phone/CW; it applies only to the bare
  root — a deep link or bookmark and the first-run welcome gate take precedence. Post-freeze slice; not
  part of the alpha.3 candidate.
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

- **Excel export of a session (inbox 2026-09-10; ruled 2026-09-19; post-freeze slice):** produce a
  genuine `.xlsx` from the same fully stored session QSOs the ADIF export uses, with stable columns,
  one QSO per row, and no forwarding or e-mail side effects. Acceptance criterion to ratify: the
  workbook opens in a spreadsheet application with one header row and one row per stored session QSO,
  and its row count equals the ADIF export's record count for the same session. Nearest confusable
  outcome: a CSV renamed `.xlsx`, or a sheet built from the browser's display list rather than the
  stored rows. Not part of the alpha.3 candidate.
- **Run-role header wording (inbox 2026-09-14; ruled 2026-09-19; post-freeze slice):** the Operate
  header's run role reads "CQ run" instead of "Calling CQ" (the role holds for the whole run, ADR
  0067, and read as "a CQ is being keyed now"); the active button keeps "Calling CQ…". Nearest
  confusable outcome: the header changing per rung. Not part of the alpha.3 candidate.

Routed by the inbox triage of 2026-10-06 (not selected; each still needs its acceptance criteria
stated before a build):

- **Contact details Notes field (inbox 2026-09-24):** the Notes textarea in the Phone/CW Contact
  details foldout should NOT allow resize.
- **"Daemon" in operator-facing wording (inbox 2026-09-28; partly done):** ruled — operator-facing
  text stops saying "daemon" and says plainly what is needed. Done so far: the rig toasts only ("Rig
  added." and "Default rig set — restart needed.", `4ffc727a`). Not started: the wider sweep of
  toasts, notices, button labels such as "Restart daemon", and the manual; it needs its own pass and
  a ruling on the replacement words (for example "Restart" or "Station Manager").
- **Search and filter a logbook (inbox 2026-09-23):** there is no way to find a callsign or
  otherwise filter a logbook.
- **Header identity block is cramped (inbox 2026-09-24):** Archive, Logbook and Rig stack in a
  column; re-lay them on one row or as a compact chip. ADR 0087 removed the archive selector, but
  the three stacked lines remain (`Header.svelte`).
- **Settings tab survives a reload (inbox 2026-09-24):** the active Settings tab is component
  state, so any reload — including an archive switch's — lands on Station, not where the operator
  was. Carry the section in the URL (hash or path) so a reload and a deep link land on the same tab.
- **Styled confirm dialog (inbox 2026-09-26):** replace the browser's `window.confirm` with one
  styled in-app ConfirmDialog (native `<dialog>`, no new dependency) for the Settings leave guard,
  Restart daemon, Activate archive (ADR 0087's prompt), destination Clear queue, and Rigs
  duplicate-model and delete. The leave guard becomes asynchronous, so the router guard, including
  browser Back after the address bar has moved, is reworked with its suite and reversion proofs. Tab
  close or reload keeps the browser's own `beforeunload` warning (browsers forbid custom dialogs
  there).
- **Settings tab widths (inbox 2026-10-05):** standardise the content width across the Settings
  tabs; many differ today.
- **Logbook column headings overlap (alpha.5 Finding #2, 2026-10-24):** the last two headings render
  overlapped, "EMAIL" over "COMMENT" (alpha.5 acceptance record, B2-08 screenshot). Cause and wider
  impact not investigated.
- **No individual-QSO delete in the SPA (alpha.5 Finding #3, 2026-10-24):** the daemon supports
  `DELETE /v1/qso/{uuid}` (soft delete, audited, archive-guarded) but the SPA offers no delete action
  for a QSO; no recorded decision excludes it. A product gap for separate prioritisation.
- **Stale tab after an upgrade (alpha.5 Finding #4, 2026-10-28):** an open tab that reconnects after
  the daemon restarts keeps running the previous build's code while its badge already shows the new
  daemon version; its first lazily loaded page then fails (Settings blank: the old chunk name is
  answered with the SPA's HTML fallback, 200 `text/html`, not a 404). A reload fixes it.
- **Country for an unenriched QSO (inbox 2026-09-30):** there is no place to enter the remote
  station's country when the QSO is not enriched over the internet.
- **Re-enrich with no source enabled (inbox 2026-10-04):** on the Edit QSO card, "Re-enrich" is
  enabled although no enrichment services are enabled. The condition is the station's enabled
  services, not the QSO's missing enrichment — a QSO without enrichment is exactly when
  re-enrichment helps.
- **Settings "Restart daemon" button (inbox 2026-09-24; triage 2026-10-06):** keep it. It is the
  config-apply restart for changes that need one; no move or per-section replacement is planned.

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
widen ADR 0079 without a ruling. Baseline rekeyed again (`line@494` → `line@500`). Flake fixed
2026-09-20: CI run 35516449552 (a Go-only commit) failed this file with an
`EnvironmentTeardownError` from d3-geo — the map route's lazy MapView chunk was still importing
when the file's only test finished and jsdom was torn down (seen once in four runs, never
locally in five). The pin now also waits for MapView to mount (`h1` "Contacts Map") before the
test runs; reversion proof: a wrong marker fails the wait against the real heading.
On air 2026-09-14 (`2.0.0-alpha.2-90-g7126ab64`, 15 m FT8 Call-CQ run, Map tab open beside the
FT8 view): the operator reports the map working — the passive check the build was waiting on. The
log agrees: the Map tab took the daemon's global subscriber count to four (entry 36 had six), no
request starved, and the map stayed up for the 88-QSO session (acceptance record entry 38).

**AC2c build (2026-09-14):** daemon `TuneStatePayload.RestoreMode` (`restore_mode`, active push only,
the `StartTune` snapshot read under `mu`); `tune_test.go` pins it on the active push and its absence on
the inactive one. SPA: `rig-sse.ts` accepts an optional string (a non-string drops the frame),
`rig.tuneRestoreMode` set from the push and cleared with the carrier and on `resetCatLink`;
`RigPanel.svelte` derives `shownLiteral` (restore mode while `tuneActive`, else the live literal) and
feeds it to the readout, the border, the live selector and its option list; the `ftTuning` two-line
branch is gone, the tooltip alone names the carrier. Rendered tests (`RigPanel.svelte.test.ts`
AC2c, five cases): identical readout snapshot before/during/after with tune-state first and with the
rig's mode push first; the off-data-mode note held unchanged; a mid-tune mount shows the restore
mode; the live selector's value held. Reversion proof: the Go assertions fail to compile without the
field; the eight SPA assertions fail on the previous panel and store. jsdom cannot measure layout, so
the "same rectangle" criterion is pinned structurally (identical text and class) rather than by
geometry. Committed `2544b3da`. Review of that commit, P2 fixed: the daemon publishes the inactive
push right after WRITING the restore and the rig reports the restored mode a moment later, so
clearing the hold on the inactive push showed the cached RTTY-U in between. The hold now outlives
the inactive push and is released by the first mode report after the tune (or an operator mode
pick, which outranks it); a report during the tune is the carrier's and leaves it alone. Rendered
between the inactive push and the report, the field is identical; a restore that did not take
(the rig still answering RTTY-U) shows honestly once reported. The start side's other order (mode
report before the active push) has no SPA-side cover and none is built: `StartTune` publishes
tune-state straight after the tune-on write returns, before the rig can answer over serial — a
reading of the daemon's code, not a measurement — so the rendered test for that order pins the
settled state only and says so. Committed `f4c46e81`; CI red on it (an `async` test with no
`await`, masked locally by piping lint through `tail`) and its review's two P2s fixed together:
(1) the daemon now closes every stop the rig is connected for with the rigdef READ snapshot
(`reconcileAfterTune`), so the rig reports the mode the stop left it in and the hold always has a
release — including the skipped restore of an unconfirmed unkey, where the field would otherwise
have shown the data mode over a rig still in RTTY; (2) a mode pick DURING the tune no longer drops
the hold (the daemon refuses the write and the rollback keeps the literal); losing the rig drops
the hold with the link. Go tests pin the READ after the restored and the skipped-restore stops.
Committed by the operator as `c0bbd80a`, with the auto-off test waiting for the fake serial's READ
before it inspects the stop sequence (the inactive push precedes the READ, a flake the first version
had); review clean. **Deployed 2026-09-15 as `2.0.0-alpha.2-97-gc0bbd80a`.** Open: the operator's
on-screen check — start a tune from the FT8 view and stop it without looking at the screen.

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
| AC2b | *Superseded 2026-09-14 by AC2c.* Was: the daemon's own tune carrier keyed (rig reports RTTY-U) → the readout named the carrier on two lines, neutral border, no band-pick hint (inbox 2026-09-13; codex 13d95084 P2). | — |
| AC2c | The daemon's own tune carrier is keyed: the Mode field is identical before, during and after the tune — text, border and width — nothing added, nothing taken away (operator ruling 2026-09-14). The readout and the live selector show the pre-tune literal the daemon names on the `tune-state` push (`restore_mode`); the carrier is named only in the tooltip. The Tune button keeps its rectangle from the click that starts a tune to the click that stops it. | The two-line carrier note of AC2b, which widened the readout and moved the Tune button; a readout that captures the carrier's RTTY-U because the rig-state push landed before the tune-state push; a tab opened mid-tune showing RTTY-U. |

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

## Slice — unrecognised-callsign warning (Phone/CW)

**Selected:** 2026-11-02, first of the post-contest order (operator).
**Origin:** dogfood inbox 2026-10-31: `0H5RW` (zero-H, for OH5RW) was logged in the UK/EI DX SSB
contest; the lookup found no country, ClubLog refused the upload (HTTP 400, terminal) and QRZ took the
call as typed. Nothing on the Phone form said the prefix was not recognised.

**Found (2026-11-02):** HamNut answers an unknown prefix with `200 {"found": false}` (and, per the
service's own comment, 404), mapped to `errors.ErrNotFound` (`internal/lookup/hamnut/service.go`,
`internal.go`). The orchestrator collapses that with transport failure, a disabled provider and other
errors into `country_source: "none"` (`internal/lookup/orchestrator.go` `readCountry`), so the SPA
could not tell "not recognised" from "could not ask".

**Rulings (operator, 2026-11-02):**

- **Trigger:** only an explicit "not recognised" result. Offline, timeout, disabled lookup,
  cancellation and malformed responses produce no callsign warning.
- **Warn only:** no confirmation step, no logging block (enrichment never blocks logging).
- **AC1:** a typed Phone/CW callsign whose lookup says the prefix is not recognised shows, under the
  country panel, "No country found for <CALL> — check the callsign". The wording describes the lookup
  result; it does not claim the call is invalid.
- **AC2 (nearest confusable):** a lookup that could not run or could not be read shows no warning.
- **AC3:** logging succeeds while the warning is visible; the warning can coexist with the contest
  dupe warning.
- **AC4:** the warning hides immediately when the normalised callsign changes or the contact is
  cleared; a late response for the previous call never restores it, including within the debounce.
- **Scope:** Phone/CW only; FT8 never shows it (its calls come from decodes).
- **Prerequisite:** `PrefixLookupResponse.Found` is a plain bool, so `{}`, `null` or a missing or null
  `found` decode as "not found". Tests must separate an explicit `found: false` from these unusable
  responses, which must become ordinary errors.
- **Contract:** add `country_source: "not_found"`; HTTP 200 with the `country` object omitted
  (`Result.MarshalJSON` today assumes every source except `none` carries country data). The SPA seam
  (`apiEnrich`, `frontend/app/src/lib/api/seams.ts`) must keep the negative result even when both the
  country and station objects are absent. A cache hit stays a country result, never evidence of an
  unrecognised prefix; document that boundary with the new value in `api-endpoints.md`.

**Agent's reading of "explicit" (open to the operator's correction):** only the `200 {"found":
false}` body counts. An HTTP 404 keeps today's no-data handling but raises no warning, because a 404
can also come from a wrong URL or path.

**Limit (stated to the operator 2026-11-02):** the local country table answers first, by the
longest prefix HamNut has already confirmed, so a call with a known prefix and a mistyped suffix
(`OH5RWW`, `G0KHX`) reads as that country and is not flagged. The warning catches an unrecognised
prefix, not an invalid callsign.

**Built 2026-11-02 (uncommitted at writing):** `lookup.ErrPrefixNotRecognised` (wraps
`errors.ErrNotFound`) and `SourceNotFound`; HamNut's `found` is a `*bool`, so an absent or null field
is an unusable-response error; `readCountry` reports `not_found` only for the explicit verdict;
`Result.MarshalJSON` omits `country` for it; `api-endpoints.md` documents the value and the
cache-hit boundary. SPA: `Enrichment.countryNotFound`, the seam keeps the verdict
(`nothingKnown`), `callsignNotRecognised(call)` ties the warning to the settled lookup of exactly
the current normalised call, and the Phone/CW card shows it under the enrichment panel. The shared
`EnrichmentCard` reads a verdict-only result (no country, no station facts: `verdictOnly`) as
"No match", as it did when the seam returned null; a country miss that still carries station data
keeps the station-derived display — grid, bearing and distance, path controls, local time — in
both views (review 2026-11-02, P2), and only Phone/CW adds the country warning. Tests:
`internal/lookup/hamnut/service_test.go`, `internal/lookup/orchestrator_test.go`,
`frontend/app/src/lib/api/seams.enrich.test.ts`,
`frontend/app/src/lib/operate/LoggingCard.callsignWarning.svelte.test.ts` (AC1–AC4, FT8 card).

## Proposed slice — Phone/CW keeps its own frequency and mode per band

**Status:** ruled 2026-11-02; AC1–AC7 approved with amendments (revised below); AC6 interaction
ruled (option a). Ready for RED. No code.
**Origin:** dogfood inbox 2026-11-01 ("Rig Control doesn't remember previous settings, particularly
mode") and 2026-11-02 ("changing from DIGI mode to PHONE/CW should remember the last settings …
or have defaults"), from the UK/EI DX SSB weekend.

**Found (2026-11-02):**

- `modeRestore.svelte.ts` already snapshots the outgoing view's operating point on every Phone/CW ↔
  FT switch and restores the incoming one (config `restore_rig_on_mode_switch`, live: on). It holds
  ONE operating point per view, in memory only; a reload starts empty except the boot-into-FT8
  capture (A26). Phone/CW has no default ("phone has no canonical home").
- The restore worked on 2026-11-01: 02:57:56Z `set_freq 7145000` then `set_mode LSB` after leaving
  FT8. The friction came next: 02:58:03Z onward, six `set_band 20m` in eight seconds. A Phone/CW band
  pick sends `set_band`, so the rig recalls its own band-stack entry for 20m — which FT8 had left at
  14.074 DATA-U (`rig.svelte.ts` `selectBand`).
- The Operate-wide band keys (Ctrl+Shift+digit, `[`/`]`, `RigKeys.svelte`) call the same Phone/CW
  `selectBand` even in the FT8 view, so a key band change there also recalls the rig's band stack
  rather than the FT8 dial; the FT8 band buttons use `ft8SelectBand`.
- `BAND_DEFAULT_HZ` (`rig.svelte.ts`) already holds a default frequency per band, used only CAT-off.

**Proposed behaviour (for ruling):**

- **B1:** a Phone/CW band change (button, digit key, next/previous band) goes to Phone/CW's own last
  frequency and mode on that band, not the rig's band stack.
- **B2:** a band with no Phone/CW memory goes to a default frequency and mode for that band.
- **B3:** returning from FT8 with no Phone/CW memory at all (e.g. after a reload) goes to the default
  for the rig's current band instead of staying on the FT dial in the data mode.
- **B4:** the memory is written whenever Phone/CW leaves a band or the view — including a frequency or
  mode set by hand on the rig — and never from the FT views.
- **B5 (kept):** nothing re-tunes while the rig may be keyed; the existing restore guards stand.
- **Nearest confusable:** FT8/FT4 band picks keep going to the configured FT dial in the data mode.

**Questions for the operator:** (1) does the per-band memory survive a reload and a daemon restart,
or only the browser session (today's restore deliberately forgets on reload); (2) the defaults —
`BAND_DEFAULT_HZ` frequencies, with LSB below 10 MHz and USB above, or a table the operator gives;
(3) should the band keys in the FT8 view go to the FT dial like the FT8 band buttons (a separate
inconsistency found here).

**Rulings (operator, 2026-11-02):**

1. **Persistent memory.** Phone/CW memories survive reloads and daemon restarts, kept in this browser
   and scoped to its station and rig. Loading them never retunes: recall happens only on an
   operator's band selection or a return to Phone/CW, and the return respects
   `restore_rig_on_mode_switch`. This deliberately revises the "forget on reload" choice in
   `modeRestore.svelte.ts`: the protection against surprise tuning becomes a rule about *when* recall
   happens, not a requirement that memories vanish.
2. **Explicit defaults.** Frequencies from `BAND_DEFAULT_HZ`; modes: 160m, 80m, 40m LSB; 30m CW at
   10.125 MHz; 20m, 17m, 15m, 12m, 10m, 6m USB. **60m: see ruling 6** (USB is the IARU Region 1 voice
   recommendation below 10 MHz; the existing 5.357 MHz default matches the app's FT8 dial; the
   station-specific frequency is the operator's choice). Source given with the ruling: IARU Region 1
   HF band plan, <https://www.iaru-r1.org/wp-content/uploads/2019/08/hf_r1_bandplan.pdf> (60m USB
   exception; 10.125 MHz in the CW segment). A remembered frequency and mode always win over a
   default: a saved 20m CW position returns as CW.
3. **One band action per view.** Buttons, digit keys and next/previous use the same view-aware band
   selection. FT8 selects its configured FT8 dial and FT4 its own FT4 dial (separate tables,
   `config.md` §4.2), each with the configured data mode; next/previous visits only bands that profile
   carries, in configured order. Fixed in this slice.
4. **Tightened B4:** inherited or partially restored FT state never becomes Phone/CW memory — the
   boot capture (`noteRigReport`, a report received in an FT view) cannot stay an unconditional
   source, and a refused restore must not overwrite good memory with the rig's unchanged FT position.
5. **Tightened B3:** returning from FT still restores Phone/CW's last band and position; the
   current-band default is only the fallback when no return position exists. B5 (no re-tune while
   the rig may be keyed) is exercised on every entry path, including rapid selections and partial
   command failures.

6. **60m (second ruling, 2026-11-02).** Mode USB; **no default frequency in this slice**. With no
   saved 60m position, a Phone/CW 60m selection leaves frequency and mode unchanged and says "No
   Phone/CW default configured for 60m." The same applies to the AC3 fallback and to CAT-off display.
   Falling back to the rig's stored setting would contradict AC1. A saved 60m position recalls as usual.

**Acceptance criteria (approved with amendments, 2026-11-02 — revised text):**

The reviewer approved AC1–AC7 subject to amendments to AC3–AC7, folded in below. Two definitions are
used throughout:

- **Confirmed position.** CAT live: a frequency and mode the rig itself reported. CAT off: a valid
  frequency and mode the operator entered. A commanded value the rig has not reported is never
  confirmed.
- **Phone/CW owns the rig** from the moment a return to Phone/CW (or a Phone/CW band selection)
  completes successfully — every command in its sequence confirmed — or an attributable manual
  operation establishes a position, until the operator leaves the view or starts another selection.
  Displaying Phone/CW alone is not ownership. With `restore_rig_on_mode_switch` off, entering the view
  does **not** establish ownership: the rig is untouched and may still show FT's position and delayed
  reports, so existing Phone/CW memory is preserved until a Phone/CW selection or an attributable
  manual operation establishes a position.
- **Attributable manual operation.** A confirmed position the operator set by hand, with no Phone/CW
  command outstanding whose outcome is unknown. A report that merely differs from the last known
  position is not proof of hand-tuning: it may be the delayed result of an earlier command.

Criteria:

- **AC1 — Phone/CW band selection.** Button, digit key and next/previous in Phone/CW go to the
  remembered frequency and mode for that band when one exists, otherwise to the band default
  (ruling 2; 60m per ruling 6); never to the rig's band stack. Nearest confusable: today's `set_band`
  recall, which must no longer occur.
- **AC2 — FT band selection.** In FT8/FT4, the same three inputs go to that profile's configured dial
  in the configured data mode; next/previous skips bands the profile has no dial for, keeping the
  configured order. Phone/CW memory is neither read nor written.
- **AC3 — return to Phone/CW.** Leaving FT restores Phone/CW's last return position (band, frequency,
  mode); with none, the default for the rig's current band (60m: unchanged plus the ruling-6
  message). `restore_rig_on_mode_switch` keeps its existing scope (`modeRestore.svelte.ts`
  `setRestoreOnModeSwitch`): off prevents the live re-tune only; the CAT-off displayed context is still
  restored. Band selections still apply AC1.
- **AC4 — persistence without surprise.** Memories survive a reload and a daemon restart, keyed to
  station and rig; a reload, a restart or opening the page sends no rig command. **Required case:**
  hand-tune on the rig, receive the rig's confirmation, reload without navigating away; a later
  selection of that band recalls that exact frequency and mode.
- **AC5 — what writes memory.** Every confirmed position (CAT live or CAT off, per the definition
  above) received while Phone/CW owns the rig is persisted at once — not only on leaving a band or the
  view — so the latest knob change survives a reload. Never written: a report received in an FT view
  (the boot capture stops being a memory source); a late FT confirmation that arrives after navigating
  to Phone/CW but before Phone/CW owns the rig; the position left by a refused, failed or partial
  restore or selection; an unconfirmed commanded value; and any stale rig data replayed or re-read
  across a connection loss — after a reconnect, only a fresh report counts. **Resumption:** after an
  unsuccessful restore or selection, recording resumes when the operator next completes a selection
  successfully, or recovers by hand — an attributable manual operation (definition above). While any
  command's outcome is unresolved, a differing report is not relabelled as hand-tuning; a test covers
  a delayed result from an earlier unknown-outcome command arriving after the failure.
- **AC6 — safety on every path.** Every entry (buttons, keys, next/previous, return from FT) is
  serialised. Each sequence is two or more commands (frequency, then mode); TX eligibility is
  rechecked before each command, not once per sequence. Refusal, transport failure, unknown outcome
  and supersession each stop the remaining sequence: no data mode on a phone frequency, no memory
  written from the half-applied state, and the failure stays visible to the operator. Rapid
  next/previous presses accumulate from the pending destination, not the displayed band: two presses
  while the first is pending advance two eligible bands. "Last wins" applies when that last request
  succeeds; a superseded or failed earlier request is reported, not hidden. Band selection while FT
  is armed: see the ruling below.
- **AC7 — CAT off.** The same memory and defaults apply to the displayed context (band, mode,
  frequency); no rig is touched. Memory accepts valid operator-entered frequency and mode — no rig
  report is required. 60m without memory: unchanged plus the ruling-6 message. **FT8/FT4 with CAT
  off** keep today's behaviour (operator ruling 2026-11-02): band and dial change, the displayed mode
  is preserved. An FT dial that is not on its band (config accepts any positive dial) is refused
  with an explanation and the display is left unchanged (review 2026-11-02, P2).

**Ruling — band selection while FT is armed (operator, 2026-11-02): option (a), stop-then-retune.**
Background: today a retuning command is not refused while SM is keyed; `handleRigCommand`
(`internal/api/handler_rig_command.go`) first performs the dial guard's teardown — PTT down, session
ended, TX disarmed — and writes only if that stop is confirmed (otherwise 409 `rig_tx_stop_failed`,
rig not retuned). The alternative, (b) refuse the selection while armed, was weighed and not chosen.

- Applies to explicit band selections — buttons, digits and next/previous — consistently in Phone/CW,
  FT8 and FT4.
- Sequence: resolve a valid destination; stop/disarm through the existing daemon path; then apply the
  destination subject to the existing TX guards.
- A failed stop or unresolved TX state prevents every further write and every memory update.
- Never re-arms automatically afterwards.
- A 60m selection with no destination only shows the ruling-6 message: no stop, no write.
- Returning between views is not covered: it keeps its existing restore guard and
  `restore_rig_on_mode_switch` behaviour.

**Implementation boundaries the tests must cover (review, 2026-11-02):** the stop trigger
`retunesTheRig` lists only `set_band` and `set_freq`. It does not cover `set_freq_b` (VFO-B
selection) or a mode-only sequence (destination frequency already matches, mode differs). Do not
assume the existing hook covers every new path; tests exercise VFO-B selection and the
same-frequency/different-mode destination under an armed FT session.

**Ruling — next/previous band (operator, 2026-11-02): wrap, nearest eligible when off-list.**
Eligible bands: Phone/CW — the operating bands with a usable destination (valid memory or a
default); FT8/FT4 — operating bands whose configured dial lies on that band. A band with no usable
destination is skipped by next/previous (second ruling, option (a), 2026-11-02); selecting it
directly still gives its message. When no configured band has a destination the explanation says
so, rather than that no bands are configured.

- On-list: next/previous follows the configured order and wraps.
- Off-list (a known band not eligible): canonical frequency order — next selects the nearest higher
  eligible band, previous the nearest lower, wrapping to the lowest/highest eligible band when none
  lies that way. Example: from excluded 60m, next → 40m, previous → 80m.
- Unknown current band: next → the first configured eligible band; previous → the last.
- No eligible bands: an explanation only; no stop/disarm, no rig command.

**Slice 2 built (2026-11-02):** D1/D2 in `internal/api/handler_rig_command.go`; the sequence in
`frontend/app/src/lib/operate/bandSequence.svelte.ts` (unwired); confirmed rig state and a report
listener in `rig.svelte.ts`.

**Ruling — the recheck race (operator, 2026-11-02): option (a).** A credible, code-derived race (not
observed): the frequency report and the FT8 disarm arrive on independent streams, so a recheck
straight after the frequency report could see the daemon's own stop as still "armed". Rejected: a
"stopped" field in the 202 — the 202 already follows the stop hook and guarded write, and a historical
acknowledgement cannot show whether a new transmission started afterwards. Boundaries:

- The transmit state is observed from before the first write, so events during the POST count.
- One absolute deadline: the first write's HTTP 202 plus 2 s. The frequency confirmation and the
  stop observation wait concurrently; no second allowance after the frequency report.
- The complete stopped state is required: FT8 disarmed, not transmitting, no active contact (and no
  tune carrier). `armed: false` alone does not count.
- Once stopped — or idle from the start — a newly observed arm, contact or transmission invalidates
  the sequence, even if it has ended again.
- Supersession, connection loss or a TX alarm ends the wait. Immediately before the mode is
  dispatched: recheck the VFO, the frequency and the TX guards.
- Expiry sends no mode. With the frequency confirmed: "Frequency changed, but transmit-stop
  confirmation did not arrive; mode was not set." A late event does not resume the sequence.
- The true no-op writes, stops and waits for nothing.
- Built as one recording mechanism: a reactive effect, active for the whole selection — including
  while a POST is pending and nothing is awaited (review 2026-11-02 gap: disarm → re-arm → disarm
  during the POST) — latches stopped / keyed-again and wakes the waits; the guard reads the latch.
  Each SSE message is its own task, so the effect has run before the next report is handled.

**Ruling — unknown selected VFO (operator, 2026-11-02):** refused with zero commands — neither the
default A nor an optimistic selection establishes which physical VFO to write — at startup and after
a reconnect, until a fresh selected-VFO report arrives.

**Ruling — mode-only selection while keyed (operator, 2026-11-02):** completion requires both the
reported mode and the observed stop, within 2 s of that write's 202. "Mode changed, but
transmit-stop confirmation did not arrive." only when the mode was confirmed; otherwise the generic
unknown-outcome message. A late stop does not resume the sequence.

**Build order and test discipline (review, 2026-11-02):** AC6 is a prerequisite of every new live
band-selection path — no new live path becomes reachable before its safety sequence is built and
tested. Separate commits, in this order:

1. Pure destination selection and CAT-off behaviour (AC1, AC2, AC7).
2. The live selection sequence, including AC6's stop, failure, supersession and ordering cases.
3. Memory ownership, persistence and return behaviour (AC3–AC5), then wiring the controls to the
   completed action.

Standalone `set_mode` stays excluded from stop-then-retune
(`internal/api/handler_rig_retune_test.go`, `TestRigRetune_OnlyRetuningCommandsStopTransmit`). The
same-frequency/different-mode case is fixed in the band-selection action — it carries
stop-then-retune semantics even when its only necessary write is a mode change — never by making
every mode change stop FT. Distinct tests: VFO-B frequency selection invokes stop-then-retune;
same-frequency/different-mode band selection invokes it; standalone mode changes keep their existing
behaviour; an unresolved stop prevents destination writes and memory updates. Each AC carries a case
matrix; no single failing test is taken to prove a whole criterion.

**Slice 1 committed** `01a75a9a` (destinations, eligibility, CAT-off; no production caller).

**Slice 2 — live selection sequence: proposed design (for ruling, 2026-11-02).** Found in the tree:

- Daemon: `handleRigCommand` calls `stopTxForRetune` — composed from `bridge.StopTune` and
  `ft8.Service.StopForRetune` (`internal/api/server.go` `retuneStopper`) — only when
  `retunesTheRig` sees `set_band`/`set_freq`. `StopForRetune` is a no-op when nothing is armed and
  otherwise disarms with `EndReasonBandChange`; it never re-arms.
- SPA (corrected by review, 2026-11-02): `confirmWrite` arms a per-field watch (`band`,
  `modeLiteral`, `selectedVfo`, `tune`) but **does not wait for the rig after HTTP 202** — its
  `accepted` branch returns at once; the watch is awaited only after a fired timeout, within the
  2 s grace. Absolute frequency writes (`setFreq`) use `driveRig`, with no watch at all. A newer
  write on a lane marks the older one `superseded`, today silently. So no existing path proves the
  rig reached a value before the next write.
  `modeRestore.svelte.ts` `transmitting()` is the existing "rig may be keyed" test (FT armed or
  transmitting, an FT contact active, tune carrier up); `rig.txAlarmActive` is the daemon's "cannot
  confirm unkeyed".
- FTdx10 literals: `CW-U` and `CW-L` both map to CW (`internal/cat/rigs/yaesu-ftdx10.json`).

Proposed:

- **D1 (daemon):** add `set_freq_b` to `retunesTheRig`, for every caller — a VFO-B frequency write
  moves the operating dial when B is selected (and the transmit VFO in split), exactly the case
  `set_freq` is already stopped for. This also makes ordinary VFO-B frequency steps stop FT, as
  VFO-A steps already do. `set_mode` stays excluded.
- **D2 (daemon):** `POST /v1/rig/command` gains an optional `"band_selection": true`. When set, the
  stop hook runs before writing whatever the batch holds — a mode-only band selection included. No
  other request changes; standalone `set_mode` keeps its behaviour. Documented in
  `api-endpoints.md`.
- **D3 (sequence):** resolve the destination (slice 1); with `rig.txAlarmActive`, refuse before any
  write ("TX state unresolved"). Write 1 is the frequency (`set_freq` or `set_freq_b` by the selected
  VFO); if the frequency already matches, write 1 is the mode; if both match, nothing is written and
  nothing is stopped. Every write in the sequence carries `band_selection`. Each write must be
  confirmed by push before the next; any other outcome — refused (including 409
  `rig_tx_stop_failed`), transport, unknown, superseded — ends the sequence and is returned for
  display. Before write 2, recheck `transmitting()` and the TX alarm; either aborts. Nothing ever
  re-arms.
- **D4 (modes):** FT8/FT4 use the configured FT data literal (empty = leave the mode). Phone/CW maps
  its friendly mode to the first rig literal in `rigModes` order that maps to it (FTdx10 CW →
  `CW-U`); no literal → refused before any write.
- **D5 (ordering):** selections are serialised; a new selection supersedes the pending one, whose
  remaining writes are abandoned and whose supersession is reported, not silent. Next/previous
  steps from the pending destination's band, else the displayed band.
- **D6:** an unknown-outcome write is recorded as unresolved, for slice 3's attributable-manual rule.
- Slice 2 leaves `selectBandLive` unwired; D1 is reachable at once through the existing VFO-B
  frequency step.

**Slice 2 rulings (operator, 2026-11-02):**

1. **D1 approved:** every `set_freq_b` request stops TX first, regardless of `band_selection` — the
   existing frequency-write policy applied consistently. The effect on ordinary VFO-B steps is
   accepted. Standalone mode changes are unchanged. **D2** and leaving the sequence unwired are
   approved.
2. **True no-op stays a no-op:** both targets already satisfied → no write, no stop; a redundant
   band selection does not end FT8. "Already satisfied" means **confirmed rig state** — never
   optimistic display values, nor a stale baseline while an earlier write is outstanding. The
   TX-alarm refusal takes precedence.
3. **CW → `CW-U` approved** as deterministic canonicalisation, using the merged mode mappings and
   the advertised `rigModes` order (FTdx10 rigdef lists `CW-U` before `CW-L`; both map to CW). A
   friendly CW memory deliberately recalls as `CW-U`; a previous `CW-L` choice is not preserved.
4. **Cancellation wording:** "Band selection to 40m replaced by a newer selection." If a step had
   already completed, the report says so too. No wording may imply that nothing reached the rig or
   that the replacement succeeded.

**D3 correction (review, 2026-11-02):** the sequence needs an explicit confirmation wait, armed
before sending, for **every** issued step — including the final mode write before success is
declared. First decisive failing test: HTTP 202 without a matching frequency report does not release
the mode write; nor does an unrelated report, nor a report for the other VFO.

Tightened boundaries:

- **Before the mode write:** recheck the selected VFO and the confirmed destination frequency,
  alongside TX eligibility. A VFO switch or a dial change invalidates the remaining sequence.
- **D5/D6:** cancellation abandons unsent work; it cannot retract an issued write. Unresolved writes
  are retained across supersession and ambiguous transport failures. Required test: 20m → pending
  40m → 20m — the replacement cannot claim "already satisfied" from the old 20m report while the
  40m write could still land.

**Confirmation deadline (operator, 2026-11-02): 2 seconds** after HTTP 202 for the rig's own report
of each step's value; past it the outcome is unknown and the sequence stops. A separate, named
deadline — the existing 2 s timeout grace is a different rule and does not define it. No measured
report latency exists (smd.log records "rig command applied", not the arrival of the rig's report).

## Verification boundary

Every slice states its operator-visible outcome and nearest confusable state first. Frontend work
runs the affected SPA's lint, format check, Svelte check, and Vitest suite. Layout fixtures must make
overflow, hidden-tab recovery, focus ownership, or stale state observable; screenshots alone are not
the acceptance test.

## References

- [`ADR 0060`](../decisions/0060-operator-alert-surfaces-and-stuck-tx-overlay.md)
- [`ADR 0061`](../decisions/0061-consolidated-operator-event-log.md)
- Expanded pre-decomposition inventory: `d0391ed7:docs/backlog.md`.
