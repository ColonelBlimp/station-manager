# Dogfood inbox

Raw dogfooding notes for later triage.

## Notes
- [2026-09-15] Operator, during the FT8 soak run (20 m, build alpha.2-97): plans to shift to 30 m later and reports that 30 m "did cause some USB/serial issues (interference)" on an earlier occasion — RF into the USB/serial path on the DX Commander vertical (30 m is on that profile). Not yet seen in a log I watched; the watch now passes every serial/CAT/liveness line regardless of level so the band change is covered. If it recurs: capture the exact log sequence (rig quiet / reconnect / write errors) with times and the rung that was keying, before any triage.
- [2026-09-20] evidence database: how is it accessed and how to make use of its data? — **Answered 2026-10-06 (triage):** `evidence.db` is a local, station-global SQLite file, normally `<data_dir>/db/evidence.db`; an old external layout keeps a file already found beside `datastore.path` there, and a file in the old working-directory location is moved there at startup, or left in place if its WAL cannot be folded first (`internal/evidence/relocate.go`, `internal/archive/paths.go` `Paths.Evidence`, `cmd/smd/lifecycle_adapters.go` `initEvidence`). New slot observations come from FT8 while `evidence.capture` is on (default off) — each useful decode with its context, plus coverage and loss intervals — but the evidence service also writes at its own startup (schema migration, antenna-profile activation) and from its workers (retention purges and receipts, sync bookkeeping when `evidence.sync` is on) (`internal/evidence/service.go`, `schema.go`). Ways in today: (1) `GET /v1/evidence/status` — capture state, disk usage, row counts, sync and retention totals; counts only, never rows, and no SPA view calls it (api-endpoints.md, Evidence capture); (2) with `evidence.sync` on, rows travel to SM Cloud (`PUT /v1/evidence`), which stores them but serves no read endpoint yet — the spot-network design (Draft 3) plans the public "on air / heard / queued" page from them; (3) the file itself, read-only with `sqlite3` (it is a WAL database; SQLite allows readers beside a writer, https://sqlite.org/wal.html — not tried on this station). There is no query endpoint, export or in-app view of the observations. Whether capture is on at this station was not checked (config.json not read). Any UI for them (e.g. "who heard me", band-opening history) is a design question for the operator; nothing proposed.
- [2026-10-21] Settings → Forwarding is confusing: two "SM Cloud" sections on one page. The destination row "▶ SM Cloud [DISABLED]" (under "Where each logbook in archive Drill uploads", collapsed by default, holds the per-logbook enable checkbox) and the station account "▼ SM Cloud service and token" (under Station accounts, expanded). During drill A Part 2 (2026-10-21) the operator took the expanded account section for the destination and could not find the logbook checkbox; the collapsed destination rows also hide the only control the page exists for. Screenshot: ~/Pictures/Screenshots/Screenshot_20261021_090500.png — **Triage 2026-10-21 (agent): usability defect.** In the code, both disclosures start collapsed and open by themselves to show an unsaved edit, or, for a destination, a field a refused save marked (`frontend/app/src/lib/config/DestinationsSection.svelte:228-231`, `ForwardingSection.svelte:145-148`). The account card also opens from its summary or the destination card's account links (`ForwardingSection.svelte:76-80`). The trigger for its expanded state in the screenshot is not recorded. The cards share the leading words "SM Cloud", and the destination card hides the per-logbook enable control when collapsed.
  - **Design direction (Codex review, 2026-10-21; not implemented):** on arrival, an operator can find Drill's upload switch and distinguish upload selection from shared connection settings. Open every destination card by default. Use **"Upload to SM Cloud"** for the destination and **"Connection settings — SM Cloud"** for its station account. Keep account cards collapsed initially; retain explicit opening links and visibility of unsaved edits and validation errors. Carry this into Settings → Logbooks (W-0021, after the 5F checkpoint), which reshapes the same page; priority remains with the backlog. Implementation is not authorised.
- [2026-10-21] `smd` with an unknown first argument starts the daemon. `smd version` (the agent's error during C's preparation, recorded in W-0021) is not a subcommand: `dispatchSubcommand` returns `handled=false` (`cmd/smd/main.go:142-145`), and `run()` calls `flag.Parse()` without checking `flag.NArg()` (`cmd/smd/main.go:190-191`). The stray positional argument is ignored, and a full start follows on the default configuration: the live databases opened, forwarder workers and the reconciler started, and the rig's serial port tried. Only the occupied port 8080 stopped it. — **Triage 2026-10-21 (agent; the operator classified it as a CLI defect): defect, follow-up work, not authorised for implementation.** The likely fix, an inference: refuse any positional argument (`flag.NArg() > 0`) with "unknown command" and the subcommand list, before the config load or any open, and exit `ExitError`. This needs a RED test through `dispatchSubcommand` and `runDaemon`'s seam, showing that `smd version` exits non-zero without starting. Whether `smd version` or `--version` should exist is a separate operator decision. Priority goes to the backlog by ruling.
- [2026-10-22] the ui is saying 4 qsos but only 3 new qso were logged with this install - maybe browser cache? — **Triage (Codex review, 2026-10-22):** confirmed carry-over from the first test lane in the reused tab’s `sessionStorage`, not a fourth daemon QSO. The operator identified A2-06’s test QSO among the four Session rows; the second lane’s daemon and Logbook retain the same three QSO UUIDs across the upgrade. Both promised throwaway profiles were unused. A2-08’s migration/retention outcome passes; fresh-profile isolation is not claimed. The related archive-switch concern remains inferred from code and unexercised: carry it into B2 / W-0021 with Session rows/count and edit/export checks, distinguishing same-archive reload from an archive change. See [alpha.4 Finding #5](reports/dogfood-acceptance-v2.0.0-alpha.4.md#findings). No implementation authorised. **Follow-up ruling, 2026-10-23:** B2-09 passes; B2-10 and B2-11 confirm the archive-switch defect in a fresh profile. The mixed Session list and false-success incomplete export (Findings #5 and #6) hold alpha.4 before A3; fix both for a replacement candidate and rerun the affected gates. Country presentation and the stale-file explanation are lower-severity follow-ups (#7 and #8). No waiver or implementation authority is granted; see the acceptance record’s B2 review and release disposition.
- [2026-10-23] at the main shell there is nothing that indicates what callsign is being used as the logging station's callsign. With the advent of Archives and multiple logs, this might be an important feature to add so that the Operator can quickly determine they are using the right callsign
- [2026-10-28] cabrillo format session and logbook export
- [2026-10-31] the phone/cw panel should not be the std phone/cw panel so that we can modify the contest panel for each contest - indeed, I think each contest might need its own layout or the possibility of such
- [2026-10-31] Contest dates are hard-coded in the header dropdown's catalogue (frontend/app/src/lib/operate/contest.svelte.ts CONTESTS); they should not be — operator, to look at later.
- [2026-10-31] No Logbook → Export ADIF: the Logbook view has no export; only Session → Export… (the tab's session list) produces ADIF. The contest's Cabrillo needs every member of the occurrence, whatever tab or session logged it.
- [2026-10-31] contesting should support: http://scoredistributor.net/index.php — **Findings (agent, 2026-10-31, from http://scoredistributor.net/devinfo.php and instructions.php):** WA7BNM's relay that forwards real-time scores to the online scoreboards (COSB, Live Contest Score Server). A plain-HTTP POST of `<dynamicresults>` XML to http://scoredistributor.net, no authentication (N1MM leaves user/password blank); required: contest (Cabrillo name), call, class (Cabrillo category attributes), score, timestamp; optional per-band qso/point/mult breakdown; text replies (`OK-Forwarded`, `Invalid data-score`, …); posting interval not stated (Win-Test instructions suggest 1–2 min). The poster and an opt-in setting are small; the **live score is the work** — Station Manager computes none, and `score` is required — so it needs per-contest scoring in the contest catalogue, with the W-0023 UK/EI scoring facts re-verified first. Not for the 2026 SSB event.
- [2026-10-31] Contest config system (operator): contest definitions as JSON — an embedded `contests.json` plus an operator override in the working directory (the `modes.json` pattern), served by the daemon (`GET /v1/contests`) and loaded by the form instead of the hard-coded catalogue: occurrences (dates), mode, bands and frequency segments, exchange fields, Rcvd # default, district list, Cabrillo name. The UI warns when operating outside the contest's rules. Ruled 2026-10-31: the band/segment/mode warning is built for the UK/EI SSB event now from the existing catalogue; the JSON system follows the contest.
- [2026-10-31] Contest mode needs a more intuitive way in and out (operator): the shell-header Contest checkbox and dropdown are a stopgap for the UK/EI DX SSB 2026 event. Entering contest mode, choosing the event, and finishing it (including stopping early) should be one designed flow — with the per-contest panel and the `contests.json` catalogue. For this event: Contest switches itself off when the contest ends, and the Phone card warns when logging outside the contest's hours.
- [2026-10-31] FT8 view, Rig panel: the "not the FT8 data mode (DATA-U) — pick the band to set it" warning sits inside the Mode readout box, stretching it and misaligning the Band/Mode/VFO row (seen with the rig on USB after SSB contest use). Move it onto its own full-width line under the row. (Operator, 2026-10-31: after the contest.)
- [2026-10-31] FT8 view, Rig panel: the Mode readout has no way to set the FT8 data mode itself — the only route is picking the band. Add a "Set FT8 mode" action beside the warning that does what picking the band does. The read-only Mode (no USB/LSB dropdown on FT8) is by design: the FT8 profile owns the mode. (Operator, 2026-10-31: after the contest.)
- [2026-10-31] FT8 view: the card is titled "Rig Control" but there it only changes band (which sets the FT8 frequency and data mode), swaps VFOs and tunes — the mode is the FT8 profile's. The title promises more than the FT8 card does; rename it there, or show the controls it implies. (Operator, 2026-10-31: with the FT8 Rig panel items, after the contest.)
- [2026-10-31] Purge the non-real 7Q7EB contact (2026-08-06 06:32Z, 20m SSB, uuid 019fd5c5-efcc-7193-be4f-1fee532ee315) from Station Manager (operator: not a real contact). It was never uploaded to QRZ or ClubLog — the one-contact gap behind QRZ showing 8,130 against Station Manager's 8,131 — but SM Cloud holds it (reconcile, 2026-09-21), so the purge must reach SM Cloud too. After the contest. 2026-11-02: operator confirms it was logged by mistake but says leave it for now (harmless); it was correctly left out of the 2026-11-02 email to the QSL manager. When done: no SPA delete yet (alpha.5 Finding #3), so `DELETE /v1/qso/<uuid>` — soft-delete + audit row; queues deletes to clublog, qrz, smcloud; QRZ settles as a no-op (no LOGID, qrz.go), ClubLog sends delete.php and the code treats its 2xx "Not Deleted" as success (clublog.go comment, unverified against ClubLog docs); SM Cloud removes its copy.
- [2026-10-31] TN8GD (Republic of the Congo, 10m FT8) shows no flag in the Logbook: it was saved without a DXCC entity number. Cause: internal/enums/dxcc/dxcc-entities.json, the prefix → DXCC-number table, was generated from the operator's own log (scripts/gen-dxcc-entities.py) and covers only the 166 entities already worked; TN (DXCC 412) is missing, so every first-ever entity is saved without its number and the flag (keyed on DXCC) is blank. Country name, continent and zones are right; uploads unaffected (QRZ/ClubLog derive DXCC). Fix after the contest: fill the table with every current DXCC entity from an authoritative list, then Re-enrich TN8GD.
- [2026-11-01] "Contest is off — contacts you log now won't count in UK/EI DX SSB" sticky warning showed on the FT8 page after the operator unticked Contest to work FT8 on 80m during the contest's hours (operator had not seen it before; screenshot 04:22 local). By design (Header.svelte offWarning, 48af7b77), but misleading off the Phone/CW card: an FT8 contact can never count in an SSB contest, Contest on or off. Show it only where it can be acted on (Phone/CW view, or a contest whose mode matches the view), or reword. For after the contest.
- [2026-11-01] Rig Control doesn't remember previous settings, particularly mode — (operator, contest day 2, moving between FT8 on 80/40 and Phone on 20/15/10). Evidence (smd.log 02:50–03:16Z): FT8 band picks send set_freq to the FT8 dial + set_mode DATA-U (rig.svelte.ts ft8SelectBand); Phone/CW band picks send set_band and rely on the RIG's band-stack recall for mode (selectBand) — SM keeps no per-band mode of its own. So after FT8 on a band the rig's stack for that band holds DATA-U at the FT8 dial, and a Phone pick of that band brings it back (operator then set USB/LSB by hand: 02:57:56Z LSB, 03:15:13Z USB). Bursts of 6–7 set_band 20m in a few seconds (02:58:03Z, 03:00:25Z, 03:15:06Z) look like repeated presses trying to reach SSB — whether a repeated band select steps through the FTdx10's band-stack registers is UNVERIFIED (needs the FTdx10 manual/CAT reference). Scenario to confirm with the operator. Likely want: Phone/CW remembers its own last freq+mode per band (independent of the rig stack FT8 overwrites). For after the contest.
- [2026-11-02] ukeicc-cabrillo.py: when a contact errors (e.g. UK/EI with no district), its serial is also listed under "unused serials (gaps, expected)" — the gap list is computed from the written rows only, so 011/012/070/154 showed as gaps beside the real 004/020/058/161. Compute gaps from every member's STX, not just the rows that passed. Found generating the 7Q5MLV UK/EI DX SSB entry. For after the contest.
- [2026-11-02] changing from DIGI mode to PHONE/CW should remember the last settings (freq, mode, etc) or have defaults if now 'last settings' are available. It is a lot of friction to change from FT8 to Phone and find the mode is FT* and the freq is the FT8 freq.
- [2026-11-04] Lookup-cache names that are not valid UTF-8: the station's `reference.db` (W-0024 R4 backup copy, 2026-11-04) holds 15 `contacted_station.name` values stored as TEXT that are not valid UTF-8. Found when a row-snapshot helper failed to decode them. Their encoding and origin (which lookup source, and when) are unknown. Not yet triaged. Evidence: W-0024 acceptance record, entry 27.
- [2026-11-04] Support the NCCC FT4 Sprint (operator question; rules https://www.ncccsprint.com/ft4ns.html, read 2026-11-04): weekly, Thursdays 0100–0130 UTC, FT4 only on the standard FT4 dials (80 m 3.575, 40 m 7.0475, 20 m 14.080, 15 m 21.140, 10 m 28.180, 6 m 50.318, 160 m any), 100 W maximum. The exchange is the **grid square only**, NA VHF style; the runner sends "CQ TEST <call> <grid>". Scoring: QSOs × grid multipliers per band. Reporting: post the score to 3830 and upload to LoTW; the FT4 page names no Cabrillo file. A contact counts without the closing RR73/73 (WSJT-X Improved 2.7.0-rc2 has an "NCCC Sprint" short-protocol option; plain WSJT-X uses NA VHF). **Today (agent code reading, not tested):**
  - Supported: FT4, the dials, and a CQ modifier ("CQ TEST", `internal/ft8/caller_sequencer.go`).
  - Missing: an NA VHF grid-exchange sequence. Our ladders exchange signal reports, and Field Day is the only special exchange. Answering a "CQ TEST" parses it as a plain CQ (`internal/ft8/sequence.go`) and uses the report ladder, which is the wrong exchange; what happens when the runner then sends "R <grid>" is unverified.
  - Missing: the two-phase short protocol, a contest tag on the logged QSOs, and sprint scoring.
  - Needs a new sequencer mode for both calling CQ and answering (`internal/ft8/AGENTS.md` checklist), designed with W-0023's contest work. Until then, use WSJT-X.
- [2026-11-06] FT8 repeat hold, 12m Japan pile-up 2026-11-06: JE1TRF held as "already worked on 12m FT8. Held, not answered." (earlier QSO 2026-07-04 13:49Z, 12m FT8, -13/-17); operator pressed Ignore (log: "repeat declined (operator pressed Next)") at 06:50:59 local, re-held after a new Call CQ at 06:51:23 (declines last one run), Ignored again 06:52:09, then 6 calls skipped (declined_repeat). Also JA0OZZ x3 and JF1RYU x1 skipped as repeat_while_another_held (one hold at a time). Operator: "If a QSO was declined because of too many repeats attempts at a rung (it was a big pileup), then a later attempt should not be refused - the warning message was not clear about the situation." Facts: the hold is worked-before (band+mode), not rung stalls (stalled_this_round / stall_cooloff did not fire). Open with operator: (1) wording should say WHEN the earlier QSO was / that it is not this run; (2) should an Ignored repeat be allowed back later in the same run? Evidence: smd.log 2026-11-06 06:39-06:58 local; internal/ft8/caller_sequencer.go:683-711; frontend/app/src/lib/operate/Ft8Operate.svelte:395-431.
  - Operator ruling 2026-11-06: (1) "The hold does not need to give the time, the operator should know if a QSO was logged or not with the station." (2) "An ignored repeat should be allowed back, IF there is no QSO in that session." Open: what "allowed back" means (held again for Answer anyway / Ignore, or answered without a hold), and which "session" (the FT8 run, the operating session shown in the Session panel, or the day).
  - Operator 2026-11-06: "session" = the operating session shown in the Session panel. On "allowed back": "The problem is the wording. If a contact is started but repeats x3 (and the setting is x5), the Op abandons. The next contact is made. The previous Abandoned contact ties again at this point is should be allowed?" Code facts (agent): that case is already allowed: an abandoned/parked answerer is excluded only for the current CQ round (`stalledCalls`, internal/ft8/caller_sequencer.go:922) and the exclusion clears when the next contact completes (:366) or nobody else is calling (:946); operator_pick never excludes (:907-918). JE1TRF was not that case: no contact was started with it today; it was a worked-before hold from the whole logbook (QSO 2026-07-04), so "already worked on 12m FT8" read as "worked in this session". Direction so far: no date in the hold; the hold's wording must not suggest this session; an Ignored repeat with no QSO in the Session panel's session should be allowed back.
  - Operator ruling 2026-11-06: an Ignored repeat that is allowed back (no QSO with it in the Session panel's session) is **answered directly**, without a hold. Not yet designed or built; triage owns priority.
- [2026-11-06] Operator 2026-11-06: "We need a way of rebuilding/backup/restore". Today's gap (agent, code reading, not exercised): SM Cloud rebuilds QSO data only — `smd provision` recreates a cloud archive as a NEW managed file (archive + logbook UUIDs, metadata, QSO rows incl. tombstones, default; internal/archive/provision.go) and `smd restore` fills an existing logbook by identity (cmd/smd/restore.go). NOT rebuilt from the cloud: config.json (settings, rigs, lookups, all credentials incl. the SM Cloud token restore needs), destination bindings (logbook_destination), the upload queue (qso_upload), qso_history, operator_event, contest_serial_allocation, evidence.db; reference.db is a refillable cache. Only logbooks with an enabled SM Cloud binding are in the cloud. Unverified: recreating a lost LEGACY Home (provision builds managed files and never writes over an existing one). Full recovery today relies on the stopped-daemon tarball backups taken by hand (~/sm-backup). Wanted: a supported, operator-facing backup / restore / rebuild path covering everything above.
- [2026-11-06] Session email 2026-11-06 09:00 local: "session email: some rows changed or were removed between compose and stamp; not marked" (stamped 137, sent 138). Cause (agent, from smd.log + code): the email composed at 09:00:13 with TJ1GD's QSO (id 8566, 06:58:46Z) at its then revision; the once-a-minute ClubLog/QRZ uploads at 09:00:19 wrote their upload stamps on that row, bumping its revision ("stamp sync: re-enqueued revision-bumped rows"); the stamp at 09:00:21 matches by revision (internal/api/handler_session_email.go:270-306), so TJ1GD was sent but left unmarked (no sm_fwrd_by_email_status) and would be re-sent by a later session email. Operator wants it looked at: upload-status stamps should not make a row count as "changed" for the session-email mark. Likely to recur whenever a session email is sent within ~1 min of the last QSO.
- [2026-11-06] Map vs Session panel (2026-11-06, 4-window check): a newly opened Map window still showed this morning's contacts while the Session panel in the new windows was empty. By design today (agent, code reading): the Map plots QSOs logged in the last N hours (operator-picked window, default 6 h, persisted as `sm-map-window`; frontend/app/src/lib/map/mapData.svelte.ts:1-16 — "no session entity … 'this sitting' is just 'QSOs in the last N minutes'", ADR 0049 rejection), while the Session list is per tab and per archive (ADR 0094), so a new window starts empty. Operator: "We either change the design or log it in the manual. That I didn't expect it suggest we need to change this." Leaning: change the design (revisit the ADR 0049 decision) rather than only documenting it.
- [2026-11-07] FT8 compound-call answer has only 3 rungs (operator: "There is only 3 rungs?", screenshot 07:29 local). Answering `CQ 6W/IK2HKT` ran ADR 0048's reduced type-4 ladder: `6W/IK2HKT 7Q5MLV` (sent 07:29:15, 07:29:45, 07:30:15) → their RR73 → our 73, no grid or report; no reply, abandoned 07:30:45 ("type-4 no answer after max repeats"). Agent findings: (1) ADR 0048's premise "no room for a grid or a signal report… a protocol consequence" holds only for type 4. QEX Jul/Aug 2020 (Franke/Somerville/Taylor, the ADR's own citation, https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf): "message type 1 conveys any call enclosed in angle brackets as a 22-bit hash code"; its c28 field is "Standard callsign, CQ, DE, QRZ, or 22-bit hash". So a standard message such as `<6W/IK2HKT> 7Q5MLV -12` is valid, and reports could be exchanged. (2) The real limit is our encoder: go-ft8 v0.9.0 `pack28` (`ft8/pack.go`) has no 22-bit-hash path for a bracketed call in a standard message. ADR 0048's revisit trigger ("go-ft8 adds type-4 grid/report forms") names the wrong capability. (3) Possible cause of non-completion: in our type-4 opening OUR call goes out as a 12-bit hash, which the far station resolves only if it has recently decoded 7Q5MLV in full (ADR 0048 caveat); a type-1 message would spell our call and send theirs hashed, which their software shows as their own call (QEX: "or perhaps 'mycall', if it maps to that code"). UNVERIFIED: which message sequence WSJT-X actually uses for a nonstandard-call QSO (believed to exchange reports with hashed calls, from memory only); the WSJT-X User Guide §7.5 could not be read. Verify against §7.5 or the WSJT-X source before any design. Also seen: the run line reads "Run live — working 6W/IK2HKT (first come)" for a contact the operator chose (RunSurface.svelte shows `orderWord` whenever an auto run is armed and a contact is active).
- [2026-11-07] UI framework question (operator: "I am still thinking if there is an alternative UI framework - what about using Electron? Keeping the daemon, but using Electron to be the frontend?"). Agent notes, not a decision: the only sensible form keeps `smd` and the existing Svelte 5 SPA, with an Electron shell loading `http://127.0.0.1:8080`; no rewrite, no new API. Would address browser-shell issues: Firefox-specific findings (F4 disabled-button reason invisible, F6 refused reload), stale tabs after upgrade, browser shortcut clashes; adds window lifecycle control (confirm on close, single instance, restore windows, reload after upgrade), native menus, notifications, tray, autostart, file dialogs. Would NOT address shared state across windows (ADR 0100 fixes that in the daemon) or anything in FT8, rig, contest or logging. Costs (general knowledge, unverified for this project): bundles Chromium + Node (very roughly 100–200 MB), fast release cadence with Chromium security fixes to ship, second package, hardening (contextIsolation, no Node integration, local URLs only); the browser stays for remote access. Alternatives: Tauri or Wails (system web engine; on Linux WebKitGTK, with its own quirks), or stay in the browser and fix quirks. Suggested path: name the problems first, decide by ADR; a one-day spike (Electron shell loading the existing SPA, no app changes) if still attractive. Operator also: "I am getting a sense that session management is becoming rather complicated..."

## Triage proposal — 2026-11-06 notes (agent; reviewer-corrected 2026-11-06; PROPOSAL, backlog unchanged)

Scope was checked against the code; nothing is implemented. The order is by operator impact, not
by size. The reviewer supports the order and the homes. These are review recommendations, not
implementation authorisation; the operator's decisions are marked under each item.

- **Cross-cutting: one operating session, owned by the daemon (operator direction 2026-11-06).**
  - **The connection (agent):** items 2 (FT8 repeat hold after Ignore) and 5 (Map versus Session)
    share a root cause. Station Manager has no single "operating session":
    - the **Session panel** uses this tab's sitting (`sessionStorage`, ADR 0094);
    - the **Map** uses the last N hours of the logbook (ADR 0049 rejected a session entity);
    - the **FT8 repeat hold** uses the whole logbook, same band and mode;
    - the **daemon** knows no session at all.

    The Map surprise and most of the FT8 draft's machinery (deciding-tab verdicts, hold ids,
    stale-intent refusal, single-use bypasses) follow from that.
  - **Operator direction:** "The daemon session is the cleanest and gives us one authority on the
    matter." One daemon-owned operating session that the Session panel, the Map and FT8
    eligibility all follow.
  - **Consequences:**
    - this needs a **new ADR** that revisits ADR 0049's rejection of a session entity, and ADR
      0094's per-tab Session list;
    - it must define when a session starts and ends (manual, an activity gap, the UTC day, or
      another rule), and how it relates to archives and logbooks;
    - item 2's deciding-tab draft is **on hold**: it is expected to be replaced by a daemon-side
      check against the daemon session;
    - item 5 is expected to resolve as "the Map follows the daemon session".
  - **Status:** direction only. No ADR is written and nothing is authorised. It is to be designed
    before items 2 and 5 are approved.
  - **Operator inputs on starting and stopping (2026-11-06, for the ADR):**
    - **"Mixed plus extended":** sessions start and end automatically by activity, plus a manual
      "New session". **Extended:** the operator can **restart** a session.
    - **On browser close:** the operator could be prompted "Close session?".
    - **Changing archive or logbook mid-session closes the session.** The operator considers this
      probably the easiest rule. Agent: it aligns with today's per-archive Session lists. A
      logbook change is a default change, which already restarts the daemon. A plain restart
      (power cut, deploy) would **not** close the session.
  - **To settle (agent questions):**
    - **(a) "Restart" a session:** does it mean resuming a session already closed (continue adding
      to it), or starting a fresh one?
    - **(b) The browser-close prompt.**
      - **The limit:** a page cannot show its own question on close. Firefox shows only its
        generic leave-page dialog. Observed in this record's station run (acceptance entry 21):
        "This page is asking you to confirm that you want to leave…". That dialog cannot tell a
        close from a reload, and the page cannot act on the answer after leaving.
      - **More:** with several windows open, closing one should probably not close the session.
      - **Possible alternative:** ask on the **next open** instead: "Continue the previous
        session or start a new one?"
    - **(c) The activity gap:** how long without a QSO ends a session automatically. The operator
      sets the threshold.
  - **Operator answers (2026-11-06):**
    - **(a) "Restart" = pick up:** a closed session can be resumed, and new QSOs are added to it.
    - **(b) Ask on opening, not on close:** "Continue the previous session or start a new one?"
    - **(c) The gap is configurable.** Reasoning: contests often require minimum off-times
      (around 1 h), and the session should not close during a contest.
  - **Agent notes for the ADR:**
    - **Pick-up makes an early automatic close cheap to recover from.** That lowers the cost of
      choosing the gap wrongly.
    - **During a contest, two options:**
      - the gap's default exceeds typical contest off-times;
      - or automatic closing is suspended while a contest is selected (the Phone form's contest
        toggle or the header's contest selection, W-0023).

      Which to use, and the default gap, are the operator's numbers.
    - **Still to define:** what "activity" is (a QSO logged, or any operator action), and whether
      FT8 decoding alone keeps a session open.
  - **Today's baseline (agent, 2026-11-06):**
    - The **Session panel** list spans the life of the browser tab (`sessionStorage`). It
      survives reloads (including the automatic reloads after a default change or archive
      switch), daemon restarts and every new FT8 run, and ends when the tab closes.
    - It also feeds the session email, which receives the UUIDs from the tab
      (`internal/api/handler_session_email.go:44`).
    - **Its flaws:** it belongs to one tab (new windows start empty; the Map's time window
      disagrees), and nothing outside the tab can see it (not the daemon, FT8 eligibility, the Map
      or other windows).
    - FT8 **runs** are shorter: from Call CQ, or answering a CQ or starting auto-work, until Stop
      or abandon. Each has a `RunID` (`internal/ft8/sequencer.go:2356-2370`), and its QSOs carry
      `APP_SM_RUN_ID`. A daemon session would sit above runs.
    - **`smd` keeps no session list:** no session table, no session id on QSOs.
      `session.terminated` is a Station Events kind for an FT8 contact ending
      (`internal/stationevents/stationevents.go:42`).
  - **Operator direction (2026-11-06):** "this is what we need to design — something that looks
    and feels the same" as today's Session panel, but daemon-owned, without its flaws. Session
    controls (buttons; "I suspect they will be needed") go on the Session panel.
  - **ADR drafted (2026-11-06):** [ADR 0100](decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md),
    Proposed, design only. Activity, the default gap and contest policy are left for operator
    ruling; item 1 stays independent. Nothing is authorised.

1. **Session-email mark skipped after an upload stamp** → home: W-0010 (forwarding data and sync
   reliability; session email is forwarding by email). **Not selected:** W-0010's earlier selection
   was outcome 9, now closed.
   - **Observed:** a QSO sent in the email but not marked, because the ClubLog/QRZ upload stamps
     moved its `revision` between compose and mark. It would be re-sent by a later session email.
   - **Intended:** a row whose contact content, as represented in the attachment, is unchanged is
     marked. A real content change or a deletion leaves it unmarked.
   - **Evidence:** `smd.log` 09:00:13–21 local; the mark matches `(id, revision)`
     (`internal/database/sqlite/api_context.go:2433-2480`); every `qso` update bumps `revision`
     through the trigger (`log/0005_qso_revision.up.sql`, ADR 0050).
   - **Scope, not small:** `revision` is the SM Cloud sync marker, so stamps must keep bumping it.
     The mark needs its own content comparison.
   - **Reviewer recommendation:**
     - compare the contact content represented in the attachment, excluding delivery bookkeeping
       and storage metadata such as `revision` and `modified_at`;
     - **enumerate the excluded fields before implementation**;
     - enrichment changes that alter the exported contact content **count** as changes;
     - keep comparison and marking **atomic**, keep rejecting deleted rows, and keep the
       `revision` bumps and their cloud propagation.
   - **Impact:** a repeat email to the recipient. **Reproduced when an upload stamp overlaps
     compose-to-mark.** The evidence establishes the race, not its frequency.
   - **Acceptance:** separate cases for:
     - a stamp-only change (marked);
     - an actual content change (unmarked);
     - a deletion (unmarked);
     - a mixed batch (each row judged on its own);
     - SM Cloud still receiving every revision.

     This fixes one cause of repeat email, not general email idempotency.
   - **Operator decision (2026-11-06, "agreed", following the reviewer's recommendation):**
     compare exported contact content, excluding explicitly listed delivery bookkeeping and
     storage metadata; enrichment changes that alter that content count. The exact exclusion
     list is reviewed before implementation. **Not authorised for implementation.**
   - **Draft exclusion list (agent, 2026-11-06; revised after review; PROPOSAL, not authorised):**
     - **The comparison:** the fields **actually emitted** for each record by the attachment path
       (`adif.QsoToRecord`, `internal/adif/adif.go:114`, written by `adif.ComposeToAdifString`,
       `internal/api/handler_session_email.go:176`). It is not every member of `adif.Record`:
       embedded structs also carry non-exported metadata.
       - The composed record's emitted fields are compared with the current row's, field by
         field, with the excluded fields removed from both.
       - It runs in one transaction with the mark, so comparison and marking stay atomic.
       - Deleted rows stay unmarked.
     - **Excluded: delivery bookkeeping emitted by this path:**
       1. `clublog_qso_upload_date`, `clublog_qso_upload_status`: the ClubLog forwarder's stamp
          (`internal/forwarding/clublog/clublog.go:315`). These caused the observed race.
       2. `qrzcom_qso_upload_date`, `qrzcom_qso_upload_status`: the QRZ forwarder's stamp
          (`internal/forwarding/qrz/qrz.go:203`). These caused the observed race.
       3. `app_qrzlog_logid`: QRZ's per-QSO log id, provenance that is not client-editable
          (`internal/qsoservice/update.go:102-103`).
       4. `sm_qso_upload_date`, `sm_qso_upload_status`: the SM upload stamp.
       5. `sm_fwrd_by_email_date`, `sm_fwrd_by_email_status`: the email mark itself. A re-send of
          an already-emailed row emits it.
     - **Excluded as prospective policy:** `qrzcom_qso_download_date` and
       `qrzcom_qso_download_status` exist in `adif.Record`, but the current `QsoToRecord` does not
       populate them, so this attachment path does not carry them today. They are excluded in case
       a later path does.
     - **Not compared, because this path does not emit them** (storage metadata): `id`,
       `logbook_id`, `dedupe_key`, `revision`, `modified_at`, `created_at`, `deleted_at`
       (deletion is rejected separately), `country_details`, `contact_history`.
     - **Counted as contact content:** every other emitted field, including:
       - `APP_SM_QSO_ID`. The QSO's UUID **is** exported under this name and stays in the
         comparison;
       - `APP_SM_RUN_ID`, `APP_SM_CONTEST_START`, and `APP_SM_REQUEST_QSL` (the request-QSL
         flag);
       - **the exported QSL fields** (`qsl_sent`, `qsl_sent_via`, `qslsdate`, `qsl_rcvd`,
         `qsl_rcvd_via`, `qslrdate`, `qsl_rcvd_notes`, `qslmsg`, `qslmsg_rcvd`, `qsl_via`). Per
         the reviewer's recommendation, they affect what the recipient needs to know;
       - the exported enriched values, held in the contacted-station fields (`COUNTRY`, `DXCC`,
         `CQZ`, `ITUZ`, `CONT`, `NAME`, `QTH`, `ADDRESS`, `EMAIL`, `GRIDSQUARE`, `LAT`, `LON`,
         `IOTA`, `DISTANCE`, `ANT_AZ`, `ANT_PATH`). Per the decision, an enrichment change that
         alters them counts;
       - the logging-station `MY_*` fields, `OPERATOR`, `STATION_CALLSIGN`, `OWNER_CALLSIGN`.
     - **Not in the model:** no LoTW or eQSL fields exist.
     - **Selected 2026-11-06:** the operator authorised the fix, design first. The transaction design
       is W-0010 outcome 11 ([dossier](work/W-0010-forwarding-data-and-sync-reliability.md)).
     - **Coverage of today's attachment:** it carried 49 distinct fields
       (`sent-adif/session-20261106-070013.adi`). Excluded among them: only the four `CLUBLOG_*`
       and `QRZCOM_*` upload fields.
2. **FT8 repeat hold after Ignore** → home: W-0011 (Deferred until a recognised operator problem
   is chosen; this is one), not W-0019 (closed).
   - **Observed:** a station Ignored on a worked-before hold stays excluded for the rest of the
     run.
   - **Intended (operator ruling 2026-11-06, already recorded):** after an Ignore, a station with
     no QSO in the relevant Session-panel session is answered directly, with no hold. The hold
     shows no date.
     - This is recorded as a **scoped exception** to the 2026-09-13 ruling "never auto-skip and
       never auto-answer it" (`internal/ft8/repeathold.go:1-20`).
     - It does **not** extend to the first worked-before hold, which stays as it is; that broader
       change is not authorised.
   - **Scope, not small:** the daemon, where the sequencer runs, has no notion of the Session
     panel's session. It lives per tab in `sessionStorage`
     (`frontend/app/src/lib/operate/session.svelte.ts`, ADR 0094), and several tabs mean several
     sessions. The hold's check reads the run's whole logbook (`repeathold.go:70-87`).
   - **Unresolved: session ownership.** Reviewer recommendation for the design proposal: the
     session belonging to the tab that starts the run, with an explicit definition of what
     happens on reload, session clearing, tab closure, or another tab taking control. Neither the
     FT8 run nor the calendar day is substituted silently.
   - **Impact:** callers excluded or delayed in a pile-up, as in today's 12m trace. Completed
     contacts lost are not established by that trace.
   - **Operator decision (2026-11-06, "agreed", following the reviewer's recommendation):** the
     initiating tab's session is the basis for the design. Ownership stays **provisional** until
     the design resolves reload, session clearing, tab closure and control from another tab,
     particularly how each affects eligibility to answer an Ignored caller.
   - **Acceptance:** to be written with that design. **Not authorised for implementation.**
   - **Draft session design (agent, 2026-11-06; revised after review; PROPOSAL, NOT APPROVED, not
     authorised):**
     - **Facts it rests on:**
       - The Session list is per tab in `sessionStorage`: it survives a reload and ends when the
         tab closes (`frontend/app/src/lib/operate/session.svelte.ts:1-12`).
       - There is no operator action to clear a session; the only reset is a test seam (`:145`).
       - FT8 teardown starts **only when the last** `/v1/ft8/events` subscriber leaves
         (`onSubscriberRemoved`, `internal/ft8/service.go:463-474`). The capture then lingers 5 s
         (`:21`), and TX is disarmed `unattended` (`internal/ft8/servicetx.go:319`).
       - A run's `declinedRepeats` is cleared when the run ends (`internal/ft8/repeathold.go:140`).
       - Each run has a `RunID`, a UUIDv7 (`internal/ft8/sequencer.go:288-294`).
       - Holds have no identity of their own today.
       - Runs have no tab owner: `POST /v1/ft8/claim` selects the profile only, and any FT8 tab
         may send Answer anyway or Ignore.
     - **Policy change, stated plainly:** eligibility is decided from **the session of the tab
       that presses Ignore** (the deciding tab), not the initiating tab. This avoids a new
       run-owner concept, but two FT8 tabs with different sessions may reach **different
       answers** for the same caller. The operator rules on this.
     - **Scope of "no QSO in the session" (operator choice):**
       - **Recommended:** the existing same-call, same-band, same-mode/profile scope, consistent
         with the worked-before hold.
       - **Alternative:** "any QSO with this call", which is broader and needs an explicit
         operator choice.
     - **Mechanism, attributed and staleness-safe:**
       1. **Hold identity.** Each hold gets an id (a per-run counter), published with the hold.
          The Ignore intent names `run_id`, `hold_id` and the held station's call, band and
          mode, and carries the deciding tab's verdict "no qualifying QSO in my session".
       2. **Stale intents.** The daemon applies an Ignore only if `run_id` is the live run and
          `hold_id` is the hold still open for that station. Otherwise it refuses with a distinct
          stale-intent code and changes nothing. A delayed Ignore can never grant eligibility to a
          different held caller or to a replacement run. The tab shows the refusal and the current
          hold.
       3. **What is granted.** An eligible Ignore records a **single-use bypass** for that call,
          band and mode **in that run**. It is not a stored "eligible" flag that is trusted later.
       4. **Invalidation.** The bypass is void as soon as any QSO with that call, band and mode is
          stored after the Ignore, by any tab or path. The daemon sees every stored QSO, so it
          rechecks the worked-since-Ignore condition when the bypass would be used, and a
          completed contact always voids it. It is also void when the run ends.
       5. **Consumption.** The bypass is consumed when the station is next picked and answered
          directly. After that, the station is under the normal rules again.
       6. **An unsuccessful direct answer.** If that answer reaches no completed contact (no
          answer after the repeat cap, abandoned, stalled), the bypass is already spent. The
          ordinary stall rules apply (CQ-round exclusion, cooloff), and a later call meets the
          normal worked-before hold. Nothing is re-granted automatically.
       7. **A not-eligible Ignore:** declined for the run, as today.
       8. **Unchanged:** the first worked-before hold.
     - **Lifecycle (corrected):**
       - **Reload:** the session survives. A pending bypass belongs to the run, not to the tab,
         and is unaffected. On reconnect the tab sees the hold state and any bypass state.
       - **Tab closure:** the run continues while **any** FT8 tab stays subscribed. Bypasses and
         declines already granted survive with the run, including those granted by the closed
         tab. Only when the last FT8 subscriber leaves does teardown start (5 s linger, TX
         disarmed). The run ends, and every bypass and decline with it.
       - **Session clearing:** no such action exists. If one is added, bypasses already granted
         stay void-on-QSO as above, and later Ignores use the cleared session.
       - **Another tab acting:** any FT8 tab may Ignore the current hold. Its decision applies
         only through the attributed intent (item 2), so a stale or delayed intent from another
         tab is refused.
     - **Feedback (recommended):** Ignore says which it did: "skipped for now — eligible to answer
       a later call", or "ignored for this run". The two lead to different later transmissions.
     - **Acceptance, draft:**
       - An eligible Ignore, then the station calls: answered directly, once.
       - A QSO with that station logged before its next call voids the bypass: the normal hold
         returns.
       - An unsuccessful direct answer: no new bypass; normal rules apply.
       - A not-eligible Ignore: declined for the run.
       - A stale Ignore (wrong run, or a superseded hold): refused, nothing changed.
       - Two tabs with different sessions: each decision applies as attributed.
       - Each lifecycle event behaves as above.
       - The first hold is unchanged.
       - Each decision and its basis are logged.
     - **Open for the operator:** the deciding-tab policy, the scope, and the feedback wording.
3. **O-a: "The request timed out…" shown for every unknown outcome** → home: W-0012 (Open;
   diagnostics).
   - **Observed:** the shared lead sentence (`frontend/app/src/lib/api/_helpers.ts:69-70`; 32
     call sites in 14 files) says "timed out" after an immediate network error or an uncoded 502
     (entry 30).
   - **Intended:** neutral wording. Reviewer recommendation: "Station Manager could not confirm
     the result, so the outcome is unknown."
   - **Impact:** misdiagnosis at exactly the moments the operator must judge an uncertain outcome.
     It ranks above polish, per the false-alarm ruling.
   - **Acceptance:** every unknown-outcome path uses the neutral sentence, and each operation keeps
     its existing recovery guidance. Distinguishing every transport failure is not needed for this
     correction.
4. **Backup / restore / rebuild path (design question)** → home: W-0014's parked "DB
   backup/restore/integrity UI". Proposed: promote it to its own work item once its outcome is
   set.
   - **Observed:** SM Cloud rebuilds QSO data only; full recovery relies on hand-made tarballs.
   - **Impact:** high consequence, low frequency; no incident today.
   - **Decisions:** what must be recoverable (config, credentials, bindings, queue, evidence),
     from where (local, cloud), the operator flow, and how often.
   - **Acceptance:** defined after those decisions.
5. **Map versus Session panel (design question)** → home: W-0012 (map).
   - **Observed:** the Map shows the last N hours of the logbook; the Session panel shows this
     tab's sitting (ADR 0094). These are separate concepts, by design (ADR 0049).
   - **Operator leaning:** change the design.
   - **Decisions:** the intended outcome (the Map follows the tab's session; or a time window,
     clearly labelled; or both), and whether ADR 0049's rejection of a session entity is
     revisited (a new ADR).
6. **O-b: internal path in the write-failed toast** → home: W-0012.
   - **Observed:** `default_change_write_failed` carries the raw write error, including the
     temporary-file path, to the browser.
   - **Intended:** the operator sees "config.json could not be written"; the path and the error
     chain stay in `smd.log`, as the archive routes already promise.
   - **Impact:** low; it is not a secret.
   - **Acceptance:** no file path in the default-change error bodies; the path still logged.
- [2026-11-12] FT4/FT8 "Tx 1 disabled" contest callers (RSGB FT4 series). Operator: "I want to support this for the RSGB FT4 contesting." The 2026 RSGB FT4 rules say to use standard FT4 (not the contest formats) and allow disabling Tx 1, since grids are not needed. Code reading (agent, 2026-11-12):
   - **Calling CQ (the gap):** a caller with Tx 1 disabled answers our CQ with a bare report (`US THEM -12`). We list and pick only grid answers (`internal/ft8/caller_sequencer.go:588` `collectAnswerersLocked`; `:665` the auto pick, both `pm.kind != msgGrid`), so such callers are silently ignored.
   - **Answering a CQ:** we always open with Tx 1 (`THEM US GRID`; `internal/ft8/sequence.go` `NewExchange`, `txCalling`). There is no option to open with a report. This is allowed by the rules ("may disable"), but costs a cycle.
   - **Wanted:** (1) accept a report-first answer to our CQ, listing it and replying `R-report`; (2) optionally, a "skip Tx 1" setting when answering. A sequencer change under `internal/ft8/AGENTS.md` (rungs, final-rung policy, logging with RST_RCVD from their first report and no grid).
   - **Acceptance:** in a run, a report-first caller is listed and worked to a logged QSO, with no grid and both reports; with the setting on, answering a CQ opens with our report.
- [2026-11-12] FT8/FT4 logging must never wait on enrichment. Operator ruling: "FT8/4 should go through the ladder and not be dependant on enrichment. If enrichment occurs with the boundaries of the ladder - great, otherwise log and move on. The QSO can always be edited later and enriched."
   - **Today (source reading, not tested):** the completion job looks the call up *after* the last rung and then writes under the same 30 s context (`launchFt8QsoLog`, `cmd/smd/main.go:356`; `Enrich` then `Submit`, `cmd/smd/lifecycle_adapters.go:756`, `:778`). A cache hit is instant (Band Activity pre-looks-up CQ callers), but a cold miss waits on the sequential provider chain (`internal/lookup/orchestrator.go` `runChain`; 10 s per request by default). If the lookup uses the whole 30 s, the write starts on an expired context and the contact is expected **not** to be stored. That breaks the "enrichment never blocks logging" invariant. Reported to Codex as claude-178.
   - **Wanted:** start the lookup when the partner's call is known at the start of the ladder. At completion, use whatever has arrived and write at once, under its own context; never wait for a lookup at completion. Missing details are added later by editing or re-enriching the QSO.
   - **Acceptance:** with a lookup that hangs, a completed FT8/FT4 exchange is stored immediately without lookup details. With a lookup that finishes during the ladder, the stored QSO carries its details. A test whose enricher blocks until its context expires still finds the contact stored, and fails against today's shared context.
   - **Knock-on:** ADR 0100's archive-switch drain then waits only for the write, not a lookup.
- [2026-11-13] FT8 answer mode indicates that it can be selected - the pointer changes - (when in a ladder), but nothing can actually be selected
- [2026-11-13] Session panel - scrollbars are too big and horizonal scrolling should not happen: [Image #1]
   - **Scrollbar size (2026-11-14):** dealt with app-wide in `fccaba89` (narrow, no arrow buttons; deployed; operator: "scrollbars look good"). **Still open:** the Session panel's horizontal scrolling, which that change does not address. The manual and the waiting page still have Chromium's default scrollbars; whether they should match is undecided.
- [2026-11-13] Station Events are going to be retired for something more useful. If you look at the current list of events, they are not useful in any way
   - **What the station's list held when this was logged (read-only look, 2026-11-13):** seven events between 2026-10-21 and 2026-11-07 and none since. Five are `tx.disarmed` (four with the cause `dial_moved`, which a band change produces; one `cat_lost`), one `forward.failed` with no outcome, one `archive.activated`. Of the eleven kinds in the catalogue, three have ever appeared. Nothing records a daemon start or stop, a failed deploy or a lookup outage.
   - **Standing:** `docs/backlog.md` still lists W-0020 (Station Events) as selected work. W-0027 C2 adds no Station Event (its dossier, C2a section).
