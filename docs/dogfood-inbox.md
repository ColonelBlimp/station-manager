# Dogfooding inbox — raw capture

Quick, unsorted notes jotted while operating, via the `/log` command. **Raw
capture only** — items here are NOT triaged, deduped, or acted on. They may be
bugs, enhancements, half-thoughts, or non-issues.

**Triage later:** in a dedicated pass we go through these together and move each
to its real home — a fix, a `docs/backlog.md` entry, or struck through / deleted
as a non-issue. Don't let this file become the backlog; it's the in-tray.

Format: one bullet per note, newest at the bottom, date-stamped `[YYYY-MM-DD]`.

## Notes

<!-- /log appends below this line -->
- ~~[2026-07-04] FT8 occupancy self-TX false-busy (SIBLING of the self-decode fix)~~ **FIXED 2026-07-04, review-hardened 2026-07-05.** The occupancy readout for the operator's own offset flickered busy↔clear in lockstep with TX/RX slots because `Occupancy()` derives bands from raw audio energy (`detectEnergyBands`) as well as the (self-filtered) decodes, so a TX slot's rig-audio bleed showed a band at the TX offset. Fix: `TxController.onTransmit` records each keyed slot boundary (`Service.markTxSlot`) and `decodeLoop` skips **decode + occupancy** for a slot it transmitted in (`wasTxSlot`) — the prior RX-slot report persists on the SPA. Review round hardened it: (a) `markTxSlot` is a 3-entry **ring** so a second TX keyed close behind can't evict the first before decodeLoop reads it; (b) the mark fires **only after `KeyTx` succeeds** (a failed key = genuine RX slot, must not be skipped); (c) decode itself is now skipped on TX slots (saves ~1 s, kills garbled-bleed ghost rows); (d) the SPA slot clock is driven from `ft8-decode` (fires every slot) since `ft8-occupancy` no longer does on TX slots; (e) `SlotRefFromTime` floors to the 15 s lattice. Tests `TestTxSlotTracking` (ring + boundary/capture-ref match). Surfaces: `internal/ft8/{txcontroller.go, service.go, servicetx.go, occupancy.go}`, `frontend/logging/.../ft8.svelte.ts`, `docs/ft8.md`.
- ~~[2026-07-05]~~ **→ backlog P3 (triage 2026-07-08, pending HW check).** FT8 occupancy — tune carrier not marked (review follow-up, DEFERRED pending hardware check). The self-TX occupancy skip covers FT8 TX only; the ADR 0027 **tune carrier** keys the same rig for up to 30 s (1–2 FT8 slots) without marking those slots, so tuning with the FT8 view open *could* reproduce the false-busy readout. **Check first on the FTdx10 whether the RTTY tune tone actually routes into USB RX audio** — if it doesn't, there's nothing to fix. If it does: the tune controller would need to mark the affected slot(s) into the same `txSlots` ring (a 2-slot tune needs per-boundary marks or an interval, which the single-string design couldn't represent — the ring already helps). Low priority: tuning while operating FT8 is uncommon.
- ~~[2026-07-04]~~ **→ backlog P3 (triage 2026-07-08).** QSO world map — great-circle lines from the Op's QTH to each contact's gridsquare. A loved ham feature (WSJT-X/QRZ/Log4OM all have it), not just eye candy. Cheaper than it looks: data already exists (`my_gridsquare` + per-QSO `gridsquare`), and the geodesic math is already in `lib/utils/bearing.ts` `pathInfo` (drawing the arc = sampling points along that geodesic). **Decisions:** (1) **smd, NOT smcloud** — the data is local, offline-first requires it (a cloud map goes blank at a no-internet DXpedition, the exact 7Q8AC case), and smcloud P1 is backup, not a display surface (ADR 0040). Home = the **logbook SPA** (a session mini-map could live in logging later); a *public/community* map is a P4 cloud/website feature (same bucket as MQTT). (2) **Basemap = Natural Earth**, public domain (GPL-clean, no attribution/share-alike entanglement), via the `world-atlas` TopoJSON `countries-110m.json` (~100 KB), **bundled/embedded not fetched** (offline-first, same posture as the flag-icons). AVOID OSM/ODbL-derived data (share-alike). (3) **Render:** equirectangular, dep-free first (lon/lat→x/y is trivial; hand-roll the antimeridian wrap), or `d3-geo` for nicer projection + `geoInterpolate` great circles + antimeridian clipping at the cost of one dep. Priority P2/P3 UI — rides with the UI-cohesion/theming arc; not ship-gate, not smcloud.
- ~~[2026-07-04] Re-enrich a logged QSO from the QsoEditOverlay.~~ **→ backlog, BUMPED 2026-07-05** (P2, next-session, target = **logbook SPA**). Second flaky-link occurrence confirmed it (2026-07-05: 3/52 QSOs logged nameless — RG6S/R2BNC/SP9SOF — during QRZ timeout windows; enrichment recovered but those slipped through with no backfill path). Original note: no UI way to re-run enrichment on an already-logged QSO; the **repair path** for stale/missing enrichment. See backlog "Re-enrich a logged QSO (logbook SPA)".
- ~~[2026-07-04]~~ **→ backlog P3 (triage 2026-07-08).** FT8 auto band-hop / "run the bands" — when a band is worked out, auto-QSY to the next band in a **configured band list** and resume calling CQ. **Trigger correction (from the code):** the natural trigger is a NEW **CQ-idle timeout** (N unanswered CQ cycles = band gone quiet), NOT the per-contact `max_repeats` — `max_repeats` is the contact off-ramp, and **CQ itself is uncapped by design** (`caller_sequencer.go`: "calling CQ is the operator's standing intent"). So this feature *adds* a CQ-level give-up that doesn't exist today. **Pieces mostly exist:** per-band FT8 dial freqs (`ft8.frequencies`), CAT band/freq change (`set_freq`), the CQ sequencer — so it's [band list] + [CQ-idle timeout] + [auto-QSY] + [resume CQ on the new band]. **Real-world caveats:** antenna/amp — hopping assumes the antenna covers every listed band (or an antenna switch / auto-tuner), else you CQ into a mismatched antenna + amp; mode/power may differ per band. **Two trigger modes (2026-07-04 refinement — offer both, hop on whichever fires first):** (a) **idle** — N unanswered CQs (e.g. 10) = band dry; (b) **timer** — fixed time per band (e.g. 2 h) then rotate, à la a DXpedition band schedule. Config sketch: `bands: [20,17,15,12,10]` (= the bands THIS antenna works), `hop_after_unanswered_cqs`, `max_time_per_band`; loop the list. **Amp/tuner integration:** each hop may need a retune — could **auto-run the ADR 0027 tune controller** on the new band before CQing (clean reuse); but some amps need a manual band-switch, so "auto" assumes an all-band antenna + auto/no amp. **Automation posture:** more autonomous (machine picks band + freq + CQs) — still attended + Abandon-able (DXpeditions have an op; this just kills manual-QSY toil), but the timer mode is the most autonomy-forward → flag vs attended-only. **Fancier future:** propagation-aware scheduling (band by UTC / grayline) instead of round-robin. P2/P3, not ship-gate, not smcloud. (Pairs with the `max_repeats` infinite option — opposites: infinite = never leave a *contact*; band-hop = leave a whole *band* when it's dry.)
- ~~[2026-07-04]~~ **→ backlog P3 (triage 2026-07-08, post-ship).** Voice keyer + phone/CW auto-CQ + QSO copilot (multi-mode operating cockpit; pairs with DX-cluster consume). **Crosses the explicit v1 "no phone/CW PTT-for-operating" boundary → deliberate P2/P3, post-7Q8AC-ship, not smcloud.** Pieces, in risk order: (1) **QSO audio recording** — lowest risk, doesn't transmit (RX capture → WAV tied to the QSO UUID); `internal/audio` (retained CGO-free WAV) + `internal/audio/capture` already exist. Could land first/independently. (2) **Phone voice keyer** (record CQ, transmit, auto-repeat) — either reuse the **FT8 TX path** (`internal/audio/playback` + guaranteed-stop machinery) OR, lighter, **orchestrate the rig's native voice keyer/DVK over CAT** (trigger memory 1=CQ, 2=report; same pattern as the CW idea). First non-FT8 thing that transmits → full FT8-TX rigor: **attended-only, controller-only keying, shared `keyMu` single-flight with tune/FT8-TX, guaranteed-stop** (`tx_on`/`tx_off` stay unexposed). (3) **CW CQ (later)** — different mechanism: **CAT-driven rig keyer / message memories**, not audio (proper timing/QSK). **(4) Full-auto phone QSO via AI — hear callsign+RST → send RST → log — is NOT feasible as autopilot; only as an operator-in-the-loop COPILOT.** Why callsigns are structurally hard for ASR: they strip the language-model prior that makes ASR work (no linguistic predictability), spoken letters are acoustically confusable in noise (hence the phonetic alphabet), even expert humans need repeats, and a pile-up = overlapping speech. Contrast: **FT8 auto-works because it error-corrects the callsign into a deterministic decode; phone has no such redundancy** (leaves copy to the human ear). AI's real value = re-supply the missing prior: constrain output to valid callsign syntax + cross-check against real issued calls (QRZ/hamnut — **SM's edge**) + confidence-and-confirm (narrow to 2–3, human picks in a keystroke). Rigs' "automated voice" (voice synthesizer announcing freq/mode; voice keyer replay) is the *transmit/announce* side only — no rig *comprehends* an incoming call. **Practical blocker:** offline-first (7Q8AC flaky internet) forces **local ASR** (Whisper on-device) — heavy CGO/binary/GPU weight, worst at a field station. Copilot is also the *responsible* shape (your licence + callsign on the air; a mis-copied auto-transmit confirms a wrong call + logs garbage).
- ~~[2026-07-04]~~ **DECIDED — scope note, not backlog (triage 2026-07-08): don't build a DX-cluster node.** DX cluster — **don't build a node** (mature interconnected mesh already exists: DXSpider / AR-Cluster / CC Cluster + RBN; SM is logging + CAT, not spotting infrastructure — building one = scope creep + sysop duties for zero benefit). **Integrate instead, two independent directions:** (a) **Consume + click-to-tune** — a DX-spots panel (filter to needed-DXCC/band) where clicking a spot QSYs the rig via the existing CAT control. Architecture mirrors the bridge exactly: a persistent telnet/TCP stream is a **daemon subsystem** that ingests → filters → SSE → SPA. **Priority catch:** it's mostly a **phone/CW** feature — for **FT8, SM already self-spots** via the live Band Activity decode feed (worked-before / new-DXCC tint, beam headings), so a telnet cluster adds ~nothing to the FT8 workflow; value tracks how much phone/CW operating grows. Internet-dependent (offline-first: degrades to "no spots", fine). (b) **Submit** — already captured in the backlog "spot-submitter registry" (P3): a cluster is the natural 2nd destination after PSK Reporter (cheaper, extends the existing spot pattern), but needs a self-spotting-etiquette policy (what/when to spot). Both P2/P3, not ship-gate, not smcloud.
- ~~[2026-07-04]~~ **DECIDED — scope note, not backlog (triage 2026-07-08): revisit only at the P4 community phase.** MQTT — revisit for the P4 community/live-multi-device-sync phase only; NOT warranted now. Considered for SM's transports and rejected: local real-time is already SSE (browser clients on localhost); daemon→smcloud sync is HTTP forwarder + forever-retry queue (ADR 0038) with request/response manifest reconcile (MQTT is fire-and-forget pub/sub, wrong shape); no IoT fleet (one local rig over serial); spotting is telnet/UDP. MQTT's strengths (many-to-many pub/sub, retained messages, offline buffering over flaky links) only pay off at ADR 0040 P4 (many stations publishing/subscribing to a live feed). Adopting it now = a broker (Mosquitto/EMQX) + client dep + ops to do what SSE/HTTP already do lighter — against minimize-deps / build-specific. Trigger to reconsider: P4 community platform or live multi-device sync becomes real.
- ~~[2026-07-04]~~ **→ backlog P3 (triage 2026-07-08).** Movable/dockable nav (ManualLink) — nice-to-have. The fixed top-right nav overlapped the FT8 pile-up/stacking drawers (logging SPA only); fixed for the ship by nudging the drawers down. Idea: let the operator grab-and-move the nav wherever they want. Deferred (post-ship): it's a new P2 interaction feature (pointer-drag + viewport clamp + resize re-clamp + position persistence [config.json, not localStorage] + mirror to 3 SPAs), it erodes the nav's "always in the same corner" value, and left-side isn't free anyway (config/logbook hold the page title there). If it graduates, build the drag layer once and share it with the backlogged FT8 Spectrum drag-to-set-offset work (same Pointer Events machinery).
- ~~[2026-07-04] FT8: SM self-decodes its own TX slot~~ **FIXED 2026-07-04** (`dropOwnTransmissions` in `internal/ft8/decode.go` — the decode loop drops any decode whose sender/DE == the operator's own call, wired via `Service.SetStationCall` from live config; test `TestDropOwnTransmissions`). Filters at source so Band Activity, occupancy, AND the sequencer never see our own signal. Was: during a Call-CQ pile-up the operator's own TX (e.g. `JA2ICB 7Q5MLV RR73`) decoded off the rig's TX-audio bleed at the TX offset and showed as a phantom station.
- ~~[2026-06-23] default_rig_id is 0 on fresh install, Rig shows not set~~ **RESOLVED/WAI 2026-06-25** — daemon migrates a loose bridge/cat config into a one-entry catalogue + sets default_rig_id; a genuinely rig-less config correctly shows "not set"; commit 8c2fa625 makes the first rig added in the config SPA active. No bug remaining.
- ~~[2026-06-24] browser tab icon is the default. Should be the radio tower.~~ **FIXED 2026-06-25** — `assets/logo.png` (the tower mark) wired as the favicon for all three SPAs via each `public/logo.png`; Vite base-rewrites the href per SPA mount.
- ~~[2026-06-24] we need some links in the logging SPA (and the other SPAs) to other URIs: config, logbook, db manager, etc~~ **→ backlog** (tracked: "Cross-SPA navigation links (all SPAs)") — awaits the theming/shared-nav workstream.
- ~~[2026-06-24] we need to implement UI themes as well as dark mode~~ **→ backlog** (tracked: "UI themes + dark mode (all SPAs)" + "UI consistency across SPAs — shared theme layer") — largest UI item; colour-token refactor first.
- ~~[2026-06-24] the initial setup page should say something like, 'if you want to continue configuring Station Manager, click here [link]' which should go to the config SPA~~ **FIXED 2026-06-25** — after the first-run callsign save, the logging SPA now shows a "✓ Setup complete" interstitial (`app.svelte` `setup_done` snippet) offering **Open the Config app →** (`<a href="/config/">`) plus a secondary **Start logging →**; shown once per install (local `justCompleted`, never seen by a returning operator).
- ~~[2026-06-24] all password/secret input field should be able to 'display' the password (the little eye glyph)~~ **FIXED 2026-06-25** — new reusable `PasswordField` (eye/eye-slash toggle) swapped into all config-SPA password inputs: Email (SMTP), Enrichment (QRZ), Forwarding (creds).
- ~~[2026-06-25] psk reporter config not seen~~ **FIXED 2026-06-25** — added a PSK Reporter section to the config SPA's FT8 tab (enable + host/port, daemon-validated), folded into the FT8 save with a restart-required banner.
- ~~[2026-06-25] qsl fields not seen~~ **FIXED 2026-06-25** — added a QSL defaults section (QSL_VIA / QSLMSG / QSL_SENT_VIA) to the config SPA's Station tab, folded into the Station save (the daemon already handled the `qsl` block presence-aware).
- ~~[2026-06-25] lat and lon are no calculated when the config SPA is saved, but it is updated in My Station LSPA~~ **FIXED 2026-06-25** — root cause was no gridsquare field in the CSPA; the daemon already re-derives lat/lon from grid on every PUT.
- ~~[2026-06-25] lat and lon needs gridsquare which is not edited in the config spa~~ **FIXED 2026-06-25** — added Station identity section (callsign/operator/owner/name/grid) to the CSPA Station tab.
- ~~[2026-06-25] operator name cannot be edited via CSPA~~ **FIXED 2026-06-25** — same Station identity section; identity now editable in BOTH SPAs (one daemon source of truth, ADR 0003).
- ~~[2026-06-25] mode-mappings should be removed from the LSPA~~ **FIXED 2026-06-25** — removed the Mode Mappings sub-tab (+ its `editingModes`/`snapModeMappings`/`$effect.pre` snap machinery and the mode-mapping PUT payload) from the logging SPA's My Station panel; mode-mapping editing now lives only in the config SPA (Rigs tab → `ModeMappingsEditor`).
- ~~[2026-06-25] CW setting should be removed from the LSPA~~ **FIXED 2026-06-25** — removed the CW sub-tab (Morse Key Type/Info) from the logging SPA's My Station panel; those fields now live only in the config SPA's Station tab. The logging SPA still sends `my_morse_key_*` unchanged on PUT (the daemon full-replaces `logging_station`), so the values aren't cleared.
- ~~[2026-06-25] install/setup docs: rig should be on BEFORE running the config SPA otherwise rig's serial port may not show up in the list~~ **FIXED 2026-06-25** — added a "Power the rig on first" callout to `docs/install.md` §4 step 1 (device listing): a USB-CAT rig's serial port only enumerates once the rig is powered on; covers both the `/v1/hardware` probe and the config SPA's rig picker.
- ~~[2026-06-25] LSPA->My Station->Location: everything can be removed other than: Grid Square, Altitude, Lat, Lon - future add POTA fields~~ **FIXED 2026-06-25** — trimmed the logging SPA's My Station → Location sub-tab to Grid Square / Altitude / Latitude / Longitude; removed the CQ Zone / ITU Zone / DXCC editors + the postal-address block (Street/City/Postal Code/Country), all of which live in the config SPA's Station tab. The fields stay in the LSPA PUT payload (sourced from `configState`) so the daemon's full-replace of `logging_station` doesn't clear them. Future POTA fields → backlog.
- ~~[2026-06-25] investigate adding waterfall option for occupancy~~ **→ backlog** (tracked: "FT8 occupancy — investigate a rendered waterfall option") — investigation, not a build commitment; the ~10fps waterfall is the trigger to revisit PocketFFT for the occupancy FFT.
- ~~[2026-06-25] add a radio to config SPA for enabling special ft8 logging~~ **FIXED 2026-06-25** — clarified = a toggle in the config SPA FT8 tab for the `ft8.decode_log` JTDX ALL.TXT decode log. Built same day: daemon `ft8_decode_log` GET/PUT surfacing (`handler_config.go` + tests) + SPA decode-log section (toggle + path) in the FT8 tab, folded into the FT8 save with the restart banner. Restart-required (log opens at FT8 service start).
- ~~[2026-06-26] LSPA->My Station->About should be moved to config - it's just info. I could be useful to find a more permanent place for the the version to be displayed in all the SPAs (tab title?)~~ **FIXED 2026-06-26** — added a **General** tab to the config SPA (the cross-cutting-prefs home decided 2026-06-26) and moved the About/version diagnostics there (new config-SPA `api/version.ts` + inline fetch); removed the About sub-tab from the logging SPA's My Station (now 4 sub-tabs; orphaned logging `version.svelte.ts`/`api/version.ts`/test deleted). The General tab also hosts the **mode-switch rig-restore toggle**. The "version somewhere more permanent (tab title across all SPAs)" sub-idea → backlog (separate from the move).
- ~~[2026-06-26] warning message too technical: Warning: Lost the serial connection to the rig (serial.ReadResponseBytes: serial: port closed)~~ **FIXED 2026-06-26** — the `serial_port_error` disconnect toast was interpolating the raw Go error (`{error}`) into the operator message. Reworded the `bridge.disconnected.serial_port_error` i18n template (`lib/i18n/en.ts`) to a friendly, actionable line — "Lost the connection to the rig — check it is powered on and the cable is connected." — matching the `rig_no_data` house style; the raw error stays in `smd.log` for debugging. SPA-only; tests updated.
- ~~[2026-06-26] When moving back to Phone/CW from ft8, SM should 'remember' the last setting for the Phone/CW (Band, Freq A & B, Mode)~~ **FIXED 2026-06-26** — the operating-mode switch (`LoggingCard`) now remembers operating state **both ways**: it snapshots the outgoing mode (VFO A/B, mode, selection) and restores the incoming mode's last snapshot on every Phone/CW ↔ FT8 switch (`rigControl.snapshotOperatingState`/`restoreOperatingState`). CAT-off rewrites `manualState`; **CAT-live auto re-tunes the rig** — `set_freq`/`set_freq_b`/`set_mode` for only the values that changed, each capability-gated (no VFO-swap: it exchanges contents + selection is preserved across an excursion). In-memory snapshots (lost on reload → no surprise re-tune). The CAT-live re-tune is **opt-out** via daemon config `restore_rig_on_mode_switch` (default ON; explicit `false` disables it — CAT-off restore unaffected). Tests: `rigControl.test.ts` + `handler_config_modeswitch_test.go`. **Live multi-command re-tune wants on-rig validation** (couldn't exercise rig commands locally). Config-SPA toggle UI for the knob → backlog (config.json-only for now).
- ~~[2026-06-27] all password/secret input field should be able to 'display' the password (the little eye glyph)~~ **DUPLICATE — already FIXED 2026-06-25 (verified 2026-07-03).** Every genuine secret across all three SPAs (QRZ `api_key`, ClubLog `password` + application `api`, SMTP password) is declared `kind:"password"` and rendered through the reusable `PasswordField` eye-toggle; no raw `type="password"` inputs and no secret shown as plain text remain. Stale re-log of the 2026-06-25 fix.
- ~~[2026-06-27] when calling cq on ft8, the Calling.. CQ button should be red~~ **DONE (stale — verified 2026-07-03).** `Ft8MsgPanel` already turns the Call CQ button `bg-red-600` with a "Calling CQ…" label while a caller session runs (`callerActive`).
- ~~[2026-06-27] when answering a call (ft8) mid-way through the TX Op can click Next and it move on and continue transmitting (looks like for the new callsign) - real bug~~ **FIXED (verified 2026-07-03).** The Next button is now `disabled` while `tx.transmitting` and `onNext` early-returns mid-transmission — it acts only *between* slots, so it can no longer switch callsign mid-TX.
- ~~[2026-06-27] Clicking Next shoudl either stop and wait for the next appropriate slot, or finish TX and then move to the next~~ **RESOLVED (verified 2026-07-03).** Current behaviour = the "finish TX then move" option: Next is inert during transmit and advances the drain to the next queued station between slots.
- ~~[2026-06-27] ft8 should have a session ability to adjust the number of calls before moving to the Next~~ **→ backlog** ("FT8: operator-adjustable attempt limit before Next" — one item with line 43). No such control today; `maxRepeats` is daemon-set per rung.
- ~~[2026-06-27] or only be able to do Next when not in TX~~ **DONE (verified 2026-07-03).** Next is `disabled` while transmitting (same fix as the line-38 item).
- ~~[2026-06-27] ft8: the station currently being worked, should either be coloured differently in the Band Activity and click disabled, or hidden from view~~ **DONE (verified 2026-07-03).** The in-flight station's row is tinted (`bg-indigo-50 ring-indigo-200`) and effectively inert — a plain click no-ops while a QSO is active, and a ctrl/cmd-click is blocked with an "Already working {call}" toast.
- ~~[2026-06-27] big pile-up, lots of callsigns in the plie-up stack, if a station stops listening (or just no longer hears us) it takes 5 attempts before moving to the Next - it would be useful to have an override field (number) to the right of the Next button to adjust down the number of attemps to stop wasting time on stations which are not responding.~~ **→ backlog** ("FT8: operator-adjustable attempt limit before Next" — same item as line 40).
- ~~[2026-06-27] it would be good to have some form of visual indicator when in TX for ft8~~ **DONE (verified 2026-07-03).** A pulsing red "ON AIR" badge shows whenever the rig is keyed, plus the Enable-Tx and Call-CQ buttons go red.
- ~~[2026-06-27] can add the station currently being worked (ft8) to the pile-up list~~ **FIXED (verified 2026-07-03).** A ctrl/cmd-click on the in-flight station is blocked with an "Already working {call}" toast — it can no longer be enqueued.
- ~~[2026-06-27] I am thinking that ft8 should only hold the mic when TX is enabled (via the Enable TX button) and it should be released when that button is toggled, OR the Op shifts to Phone/CW?~~ **DECIDED — leave as-is (2026-06-27).** Triaged: gating the mic on Enable-TX would couple RX to TX-arm (no decodes / empty Band Activity until armed), but the operator wants to **monitor the band (decode) without arming TX** — so the current view-open capture model (mic held while the FT8 view is open, CAT-gated, released on shift to Phone/CW) stays. The Enable-TX-as-gate model is rejected; see the Monitor-toggle backlog discussion-point.
- ~~[2026-06-27] the O/E is fine, but there is no header for it so the header's label are not aligned properly~~ **FIXED (verified 2026-07-03).** Band Activity now has an aligned "E/O" column header (tooltip "Slot parity — even (:00/:30) / odd (:15/:45)"). The remaining flag-column offset (the "Message" label sitting slightly left of flagged rows) was fixed by the operator 2026-07-03 while dogfooding — full alignment confirmed on-screen.
- ~~[2026-06-27] heading of pile-up . even needs to be in one line not wrapped~~ **FIXED 2026-07-03 (operator).** The drawer header `<h2>` is now `flex flex-col` with two spans — "Pile-up (N) · parity" on line 1, "Paused" on its own line 2 — so the count+parity never wraps mid-phrase. (An earlier `whitespace-nowrap`+`bg-gray-500` attempt was reverted; this is the kept solution.)
- ~~[2026-06-27] can add to the pile-up queue a station already in the queue~~ **FIXED (verified 2026-07-03).** `ft8PileupStack.push` is keyed by normalised call — an already-queued call is refreshed in place (returns `false`), never appended twice.
- ~~[2026-06-27] if a station is in the pile-up queue, it can't be double-added, but it should not be allowed to be ctrl+click'ed - or remove the hover~~ **FIXED 2026-07-03.** A queued Band Activity row now drops its `hover:underline` "add" cue and its tooltip reads "already queued in the pile-up"; a ctrl/cmd-click on it is a no-op with an "X is already in the pile-up" toast (was a silent in-place refresh). `Ft8Panel.svelte` — `onCallerClick` guard + conditional row class/title.
- ~~[2026-06-27] still I can add calling stations from both odd and even~~ **FIXED (verified 2026-07-03).** Wrong-parity enqueues are rejected against the run's `workableParity` (locked by the first add); wrong-parity rows are also greyed in the feed.
- ~~[2026-06-27] let's revisit the need for the E/O in the band activity now that the display lock to whatever parity you are TX'ing on~~ **DEFERRED — lean keep (2026-07-03).** The E/O column is still useful for reading the band at a glance; parity is now enforced at enqueue (wrong-parity greyed) so it's informational, not load-bearing. Revisit after more on-air time — matches the "revisit after on-air evaluation" note on the shipped parity-badge backlog item.
- ~~[2026-06-28] correct the ladder display for the ARRL FD~~ **→ backlog** ("FT8 Field Day — FD-aware Operate ladder render", with the sibling FD remainders: FD pile-up Ctrl-click, config-SPA section dropdown).
- ~~[2026-06-29] logbook - need to able to edit more fields of a QSO: notes, LP/SP~~ **→ backlog** (folded into "Logbook SPA — the management surface": extend the per-row edit form with LP/SP + a notes field beyond `comment`).
- ~~[2026-06-29] logbook: date_off = should it be populated always?~~ **RESOLVED 2026-07-02 — yes, always.** `QSO_DATE_OFF` (the date at `TIME_OFF`) is now populated on both logging paths: FT8 `BuildQso` sets it unconditionally, and the logging SPA's `submitQsoDateOff` sends it (= `QSO_DATE` same-day, next day on a UTC-midnight rollover). This also fixes a midnight-crossing QSO being rejected `invalid_time_range` (Phone/CW) or silently dropped (FT8). New QSOs only; imports carry the source's value.
- ~~[2026-06-29] logbook - qso list should indicate if it has been sent via email~~ **→ backlog** (folded into "Logbook SPA — the management surface": an "Emailed" column mirroring SessionPanel).
- ~~[2026-06-29] logbook - need to able to export selected/all~~ **→ backlog** (already tracked under "Logbook SPA — the management surface" → bulk actions → Export selected/all as ADIF).
- ~~[2026-06-29] logbook - maybe move the email option to a dialog overlay - same with export?~~ **→ backlog** (folded into "Logbook SPA — the management surface": bulk email/export as a dialog-overlay UX).
- ~~[2026-06-30]~~ **RESOLVED — scope note, not backlog (triage 2026-07-08): keeping the name, no rename (see note).** App name "Station Manager" is confusingly similar to the third-party online service "Station Master" (a potential forwarder destination). Clash only bites in the forwarder context (app forwarding to that service). Decision (this session): don't rename — disambiguate in-context via the forwarder's own label/logo. Durable cost of rename: RPM package name, repo, Hugo manual, all docs/ADRs/memory, and ADIF PROGRAMVERSION already stamped on forwarded QSOs in QRZ/ClubLog (irreversible outbound data). Revisit-window if it ever nags: BEFORE a v2.0.0 stable tag + wider user base, not mid-alpha. **RESOLVED 2026-06-30: keeping the name — no rename.** Reasoning held up under scrutiny: Station Master isn't in the ADIF spec (no `APP_` namespace collision), only two users (one is the author), and it's a paid service vs this free GPL one (no commercial poaching). Differentiate via own identity instead: purchased `station-manager.org` (10yr), own logo + callsign + "free & open source" positioning, label Station Master distinctly on the forwarder screen. `smd`/`SM` prefix/module path untouched.
- ~~[2026-07-02] think about adding larger explanation tooltips to some of the ft8 (and other) settings knobs, etc. It would be nice if the user could switch this off (begginner/expert) mode~~ **→ backlog** ("Settings help tooltips + beginner/expert mode (all SPAs)").
- ~~[2026-07-03] the shift+ctrl+Fn should operate in ft8 the same as it operates in phone/cw~~ **→ backlog** 2026-07-04 (P2 · FT8: "shift+ctrl freq-step key parity in FT8").
- ~~[2026-07-03] ft8 session = no Op name viewable in the list~~ **FIXED 2026-07-04** — `LoggedQso.Name` plumbed through the `ft8-logged` SSE + SPA listener (was hard-coded blank); the "list too wide" half is the column-width fix below (63).
- ~~[2026-07-03] name is in the SSB entry, but needs overflow hidden and text ellipsis~~ **FIXED (verified in code 2026-07-17).** Both Session panels constrain the Name column: the shipping logging SPA's name cell carries `overflow-x-hidden text-nowrap text-ellipsis` (under a `w-32` header), and frontend/app's carries `w-32 truncate` — a long name can no longer grow the table.
- ~~[2026-07-05] ft8 abandon (while Tx) click 'resume' immediately starts Tx - but this is too late into the slot?~~ **WAI (confirmed 2026-07-05).** Gated by `fireOpening`'s 4.5 s late-window guard — immediate TX only within the first 4.5 s of an our-parity slot (ADR 0032 truncation keeps it decodable); past that it defers to the next slot. **→ backlog** as a TX-quality enhancement (work-path opening: prefer a clean next-slot start over a truncated immediate fire, since a station you're working keeps calling — no reply-window pressure like a CQ answer).
- ~~[2026-07-06]~~ **→ backlog P3 (triage 2026-07-08; already tracked as "operator / user profiles", contest lens folded in).** Operator profiles — reconsider with a contesting lens. Selectable operator-identity bundles (Operator callsign + Operator Name, possibly Owner's Callsign) the op picks at session start instead of retyping. The contesting angle is the strong motivator: multi-op contests rotate operators mid-event (each needs their own operator identity on the log), and contests often want operator/contest-specific settings (exchange, CONTEST_ID, class/section like the FD fields). Profiles could bundle op-identity + contest params and be swapped fast during an event. Relates to the existing backlog item "Operator / user profiles — selectable op-identity bundles" (filed 2026-06-24) — this adds the contesting justification + the contest-params bundling idea. Daemon owns the profile list (config.json), SPA picks.
- ~~[2026-07-06]~~ **→ backlog P3 (triage 2026-07-08; pairs with the QSO world map).** Dashboard: add a world map at the bottom (Natural Earth, public-domain vector — license-clean for the GPL tree) showing worked grids / grayline / beam headings / live FT8 spots. Natural full-width card slot below the stat-tile grid. Render SVG-side, demand-loaded so it doesn't weigh on the flaky link.
- ~~[2026-07-06]~~ **→ backlog P3 (triage 2026-07-08, needs-trigger).** Mobile layout — deferred, no pressure now. NOT a responsive-CSS pass on the desktop app; it is a separate design effort (different interaction model, not just narrower CSS). Trigger: if smcloud (ADR 0040) grows from backup/sync into online/remote access to the log (view spots / quick-log from a phone away from the shack), a phone-shaped consumer appears and mobile matters. Mobile is coupled to a remote-access surface, not to the local app (today SM is a shack-machine daemon reached at localhost from a desktop browser). Until that trigger exists, desktop-only stands (shell mock has a 64rem min-width floor, no mobile/tablet state). Note: a tablet cannot host the deployment anyway (serial CAT / audio / CGO need a real machine).
- ~~[2026-07-06] Draggable / pinnable cards (Operate content area)~~ **→ DECIDED (ADR 0046) + IMPLEMENTED in frontend/app 2026-07-08 (session 209), browser-validated.** The fork resolved toward a *tiling* model (fixed-size, no-overlap, reflow) with a *global* pin (not the per-card {x,y,pinned} sketched below), non-destructive Default, cards unchanged in size/content; validated by the pointer-drag POC at `docs/v2-design/tile-layout-poc/`. Original note kept for the reasoning trail: operator arranges cards freely within the content area, PIN to persist a position; unpinned cards return to DEFAULT on a new window/restart. Reconciliation model (the elegant bit): PINNED = absolute-positioned override (honoured as-is, does NOT reflow); UNPINNED = default responsive flow (the auto-centre / auto-collapse / scroll layout). New window, nothing pinned -> defaults. Persist per-card {x,y,pinned} (localStorage, or op-profile config if layout should follow the operator). THIS IS AN ADR-LEVEL FORK, not just a feature: does the content area BECOME a free canvas (dashboard widgets) or stay a responsive document flow? Free-canvas would largely supersede the responsive/stationary/scroll layout just built. Decide deliberately once the surface has REAL content (CAT/Rig panel, info panels, FT8 surface) — dragging placeholders is premature. WHY raised now (operator, leans yes to doing it): the complexity is real once there is real content, so build the frontend to NOT block it — DRY, self-contained, decoupled components from the start: presentation decoupled from state/logic, no positioning assumptions baked into a card, and backend logic / state machines INJECTED or SUBSCRIBED (the daemon-side seam-injection pattern), never wired into the presentation component — a card that reaches into a state machine or assumes where it lives cannot be picked up and moved. i.e. the feature is a forcing function for clean component architecture NOW. Cheaper stepping stone if appetite is just "let ops tailor it": a few layout PRESETS may cover ~80% for far less than a drag engine. Prototype drag in the throwaway shell mock, not the Svelte impl (would fight the responsive layout), if feeling it before committing. Related: operator profiles idea, [[project]] responsive Operate layout.
- ~~[2026-07-07]~~ **→ backlog P2 (triage 2026-07-08).** Serve the DXCC entity number in the enrichment country payload — **CORRECTED same day: the mapping already EXISTS** (`internal/enums/dxcc`, embedded `dxcc-entities.json` prefix→number + `$SM_WORKING_DIR` override, built for the new-entity check) — the gap is only that the enrichment handler doesn't FILL `country.dxcc` from it. So this is a small handler-level fill (DXCCForPrefix on the country's dxcc_prefix), not a new reference dataset. Why it matters: the number otherwise exists only via QRZ's per-station `station.dxcc` (3,577/5,748 cached stations = 62%; hamnut returns prefixes only), so e.g. G4ZWX enriches with entity "G" but no 223 — and 38% of logged QSOs carry no DXCC number for awards tracking. Surfaced by the frontend/app EnrichmentCard showing "DXCC G"; the SPA displays the number whenever the wire provides it. Consider filling the logged-QSO ADIF DXCC the same way.
- ~~[2026-07-07]~~ **→ backlog P3 (triage 2026-07-08).** Band data — single-source the freq→band table + regional band-plan design consideration. TODAY there are THREE hand-synced copies of the ADIF band set: the logging SPA's `utils/frequency.ts` IARU-widest table (now also copied into frontend/app), the daemon's `internal/utils` FrequencyToBand, and `internal/enums/bands` (whose own comment says the sets "must match" — the mirror-drift smell, same pattern as the SPA's SUBMODE_TO_MODE mirror of adif-modes.json). For LOGGING this is not regionally wrong (ADIF BAND boundaries are the region-agnostic widest envelope by spec), but it should be ONE embedded data-driven table daemon-side (adif-modes.json pattern), with the SPA mirror either served or pinned by a contract test. SEPARATELY (operator design catch 2026-07-07): the moment SM grows operating aids — band-edge/TX-legality warnings, CW/phone segment hints, FT8 auto band-hop deciding if a band is permitted in-jurisdiction — REGIONAL band plans (IARU region + national licensing, e.g. Malawi) become the required dataset, and the IARU-widest logging table is the WRONG data to reach for. Design those features against a per-region/jurisdiction band-plan source from the start.
- ~~[2026-07-07] Draggable/pinnable cards — chrome decision~~ **→ folded into ADR 0046 + IMPLEMENTED 2026-07-08 (session 209)** (CardFrame wrapper, arrange-mode-only chrome): NO permanent titlebars baked into cards. Drag/pin chrome is supplied by a uniform WRAPPER frame (`CardFrame`-style) the layout layer puts around any card — title, grip, pin glyph live there once; the card contributes only a display name (ADR 0045: cards stay position/chrome-ignorant). Frames are hidden during operation and revealed in an explicit "arrange layout" mode (Grafana/HA/iOS-widgets pattern) — avoids accidental drags mid-pile-up (the Operate surface is dense with inputs, so drag-anywhere is out) and costs zero vertical pixels on the fast path (protects the compact above-the-fold card work). Cards that genuinely need a header for their own content (e.g. a scrolling Session log's sticky title/count bar) make that call themselves — content decision, not drag chrome.
- ~~[2026-07-08]~~ **→ backlog P2 (triage 2026-07-08, with rig-control).** Rename the util-rail "Rig" panel to "Rig Control" WHEN rig control lands in frontend/app (ADR 0026 ops: VFO click-to-swap, band step, set_mode). Agreed 2026-07-08: keep "Rig" while the panel is read-outs + manual entry — the label should not promise commands it can't send; rename in the same change that adds them (titles map in InfoPanel.svelte + UtilRail panels list).
- ~~[2026-07-08]~~ **→ backlog P2 (triage 2026-07-08).** Backport the tightened RST validators from frontend/app to the shipping frontend/logging SPA. Shipping's isValidRst is shape-only (/^[0-9]{2,3}$/) — it accepts 77, 000, and 599-on-USB. frontend/app now enforces the scale (R 1-5, S 1-9, T 1-9, zero invalid everywhere) AND mode-aware digit count (tone is CW-only: isValidRst for CW with optional tone, new isValidRs exactly-two-digits for voice/RTTY/PSK31; WSJT-X signed-dB unchanged). Port rst.ts + rst.test.ts + the draftProblems/canSubmit validator pick (shipping keys on displayedState.subMode||mode). Low urgency — the daemon stores whatever the SPA sends either way; this is entry-error protection for the daily logger.
- ~~[2026-07-08]~~ **→ backlog P2 (triage 2026-07-08).** Daemon log noise: client-aborted enrichments log as WARN with full error chains. The SPA deliberately aborts superseded /v1/enrich/callsign requests (typing); the daemon logs each as "new-entity check (dxcc) failed: context canceled" + "callsign provider error: qrzlookupservice: context canceled" at warn level with error_chain/error_history. Expected behaviour masquerading as faults — drowns real warns during operation. Fix: where the cause is context.Canceled from the request context, log at debug (internal/lookup orchestrator + qso new-entity check). Surfaced during the 2026-07-08 clean-DB QA run.
- ~~[2026-07-08]~~ **→ backlog P3 (triage 2026-07-08).** MY_RIG design question: should it follow the CAT-identified rig when connected? The stored QSO blob carries my_rig from static config (build config said "Yaesu FT-710" while the FTdx10 was on CAT — stale config produced a wrong my_rig on a fresh-DB QA QSO). When the bridge is live it KNOWS the rig identity; consider rig-identity-wins-when-connected (config as the CAT-off fallback). Touches daemon submit enrichment of station fields, not the SPA.
- ~~[2026-07-08]~~ **→ backlog P1 (triage 2026-07-08).** Stale test: internal/api TestVersion_HappyPath expects schema {version:3} but the DB now migrates to version 4 (a log migration was added — migrations/log/0004_utc_timestamps). Pre-existing failure, unrelated to session work; the test's expected schema version needs bumping to 4 (handler_version_test.go). Surfaced running the full api suite during the session-export build.
- ~~[2026-07-08]~~ **→ backlog P2 (triage 2026-07-08, needs investigation).** ADIF export does not contain all the populated MY_ fields
- ~~[2026-07-08] can start a QSO... but cannot log it (rig not confirmed) — want a warning~~ **FIXED 2026-07-08 (session 209):** Tab-to-start now warns via toast when the rig gate is unconfirmed/lost ("confirm the band in the Rig panel before you can log this QSO"); the QSO still starts (clock runs), the warning explains why the Log button is disabled.
- ~~[2026-07-08] when closing a card (worked, session, etc), focus should return to the callsign input~~ **FIXED 2026-07-08 (session 209):** each info-card's hide (X) now calls `focusCallsign()` after `hideTile`, so focus returns to the callsign field.
- ~~[2026-07-08]~~ **→ backlog P2 (triage 2026-07-14; frontend/app Operate UI).** contact view (from working panel) needs to be re-organized
- ~~[2026-07-09]~~ **→ backlog P2 (triage 2026-07-14; build BEFORE/WITH rig-control band-jump).** Configurable operating bands — a station-level `operating_bands` list in config.json (antenna coverage varies; many ops don't work all bands — e.g. 7Q5MLV skips 160/60/30). Drives ALL band surfaces from ONE source for consistency: the Phone/CW band-button grid, the FT8 band buttons, and the manual band dropdown. Default when unset = full 160m..6m (additive — today's behaviour); render canonical low→high; keep DISTINCT from `ft8_frequencies` (FT8 buttons = operating_bands ∩ ft8-freq bands). Editor's home is the Settings card (config SPA — not yet built in frontend/app); dogfood shortcut = add the daemon field + wire the grid consumer FIRST so config.json is hand-editable, Settings checkboxes follow as polish. ALSO (impacts rig-control Slice 4 / keyboard): the shipping SPA's Ctrl+Shift+[digit] band-jump maps digits 1-0 → 160m..6m as a FIXED table; with configurable bands the digit→band mapping must become user-configurable too. Simplest = digits follow operating_bands order (digit 1 = first configured band …); fuller = an explicit digit→band map in config. Build the Slice-4 band-jump against the configured list rather than a hardcoded table — so do the configurable-bands feature BEFORE/WITH Slice 4, not hardcode-then-rework.
- ~~[2026-07-09]~~ **→ backlog P3 (triage 2026-07-14; needs-trigger — external/online data source).** I nice to have feature would be somthing like a propagation report: ¬/Pictures/Screenshot at 2026-07-09 15-47-00.png
- ~~[2026-07-09] ft8 unable to call a station already in qso with another station - the remote station called cq->another remote station answered->many are now calling that station, I want to call that station - no way for me to do this currently.~~ **→ LIKELY FIXED by the FT8 DIRECTED-CALL feature (double-click any plain Band Activity row to call that station regardless of what they're doing), shipped 2026-07-13. Already on the active-cycle list for on-air validation — confirm it covers this "call a station mid-QSO with someone else" case when validating.**
- ~~[2026-07-10]~~ **→ backlog P2 (triage 2026-07-14; the light-mode half of "FT8 Spectrum view — colour revision").** Occupancy colours (frontend/app FT8 Occupancy pane — Ft8OccupancyStrip / Ft8OccupancySpectrum) don't work in light mode, only dark. Introduced with the Occupancy pane increment (uncommitted). Likely the red-500/green-700 fill opacities + amber recommendation markers read fine on the dark canvas but wash out / look wrong on the light surface — needs a light-mode pass on the busy/clear cell fills and the spectrum tints.
- ~~[2026-07-11] One-off backfill: stamp the numeric DXCC entity onto existing QSOs logged with blank DXCC (before the 2026-07-11 orchestrator change that now populates ContactedStation.DXCC from the enums/dxcc prefix→entity map). New QSOs already get it; this is a retroactive pass over historical rows (map each row's country/prefix → entity number, skip rows that already have a numeric DXCC). Deferred — raised while live-operating FT8.~~ **DONE 2026-07-13 — SUPERSEDED by the whole-logbook blank-DXCC backfill run (see the 2026-07-13 "dxcc-entities.json coverage gaps" item below: all 716 pipeline blank-dxcc rows repaired via re-enrich + PATCH; 0 pipeline blank-dxcc remain). No separate pass needed.**
- ~~[2026-07-11]~~ **→ backlog P3 (triage 2026-07-14; smcloud community-phase feature, capture-don't-build — sits with ADR 0040 SM Cloud + the P4 community bucket).** Pile-up "am I being heard?" status site (smcloud). When running a pile-up, SM (local) publishes to a PUBLIC website; a caller just opens the page and types their callsign to see their status — no SM install needed on the caller side (this removes the cold-start barrier: one publisher = you, any number of web viewers). CRITICAL reframe from the original "position in the pile-up" idea (discussed 2026-07-11): publish STATUS, not queue rank. (a) Data source = the DECODE FEED (everyone SM has decoded calling the op this session), NOT the operator's curated Ctrl-click pile-up stack — most callers aren't in that stack, so a stack-based lookup returns "not found" for the common case, which is worse than nothing. (b) States: **worked ✓** (in the log) / **heard — not yet worked** (decoded this session, hang in there) / **not heard**. "7Q8AC has decoded you 3× this session, not yet worked" is the message callers are desperate for. Avoid a "#7" position number — FT8 pile-ups aren't ordered queues (op picks who they hear), so a rank promises fairness the op won't honour and invites complaints. UNIQUE NICHE vs prior art: ClubLog Live Stream already shows the DX's LOG (the worked-✓ half); PSK Reporter shows where MONITORS heard you (not the DX); NEITHER shows "the DX's own receiver is hearing you" — that middle state is the unfilled gap SM can own because it already has the decode feed + session log locally. COST: local side is mostly already there; new work = smcloud (a small endpoint taking per-slot snapshots `{dx_call, decoded[], worked[]}` + a dead-simple lookup page `?dx=7Q8AC&me=G4XYZ`) — bounded, ~weekend backend MVP, not a platform. CAVEATS: best-effort publish (enrichment-never-blocks-logging discipline — a failed push never touches the QSO); low volume (~one small JSON per 15s slot); FLAKY-LINK staleness is the real risk — if the op's link drops mid-pile-up the page goes stale and MISLEADS ("decoded you 5 min ago" when the op has moved on/gone), so a prominent "updated Ns ago / STALE" stamp is mandatory so a dead link reads as dead. DISTRIBUTION (added 2026-07-11): embed the widget on the op's QRZ.com page as an iframe — a caller who just heard 7Q8AC reflexively opens qrz.com/db/7Q8AC, so the widget sits exactly where they already go (solves discovery — no URL to know). Marginal cost is small once smcloud exists: make the widget iframe-embeddable (permissive `frame-ancestors`, no `X-Frame-Options: DENY`) + a compact/responsive read-only mode; the same embed then works on any site / DXpedition page. MAKE-OR-BREAK UNKNOWN: verify QRZ actually permits `<iframe>` in bio pages (many sites strip iframes for XSS) AND that HTML-bio editing needs QRZ XP (paid); fallback if blocked = a prominent LINK/button on the QRZ page ("Am I in 7Q8AC's pile-up? →") — one click, still solves discovery. Idle-state handling matters MORE on a permanent QRZ page: needs an explicit active-vs-idle concept so an idle op shows "not currently active — last operated Xh ago" rather than stale decodes. ON-AIR + FREQUENCY (added 2026-07-13, operator): the page/widget must also show that the op is ON-AIR RIGHT NOW and on WHAT FREQUENCY — "7Q8AC is on-air: 14.074 MHz (20m FT8)". This is the concrete form of the active-vs-idle concept (on-air = live snapshots carrying freq; idle = "last operated Xh ago"), and it upgrades the widget from status-check to DISCOVERY ("where do I find them") — a caller lands on the QRZ page and knows the band/freq to tune to BEFORE calling. Data is already local: CAT dial freq (`rig-state`) + FT8 band/mode; add `{on_air, freq, band, mode}` to the per-slot snapshot `{dx_call, decoded[], worked[]}`. The staleness stamp guards this too — a stale freq misleads worse than a stale decode list (callers tune to a band the op has left), so the freq display inherits the same "updated Ns ago / STALE" treatment. NOTIFICATIONS guardrail (discussed 2026-07-11): do NOT auto-email everyone who calls (even if enrichment/QRZ yields an address) — that is unsolicited bulk email = spam: GDPR/PECR (EU/UK callers, QRZ email = personal data, no consent) + CAN-SPAM (US), a QRZ ToS violation (their data must not be used for bulk mail — risks losing the enrichment lookup), deliverability suicide (a burst of cold addresses per pile-up → blacklisted domain), and reputation harm. A LINK-ONLY email (just delivering the smcloud URL, no message) does NOT fix this — the unsolicited SEND is the problem, not the content (a link to your service still counts under CAN-SPAM, and GDPR is about processing their address to contact them without consent) — AND it's redundant with the QRZ-embed, which already puts the link where callers go, so no email is needed for discovery. The consent-flip that IS legitimate: a CALLER-INITIATED opt-in ON smcloud — "notify me when 7Q8AC works me" (email/push), 1:1, transactional-after-a-real-QSO, with unsubscribe (same footing as OQRS/LoTW). Rule: the caller PULLS the notification; the op never PUSHES to a harvested list. Scales with being WANTED — a rare-station/DXpedition feature (fits 7Q; pointless for a common station). Status: v-next / roadmap, orthogonal to the frontend/app daily-driver work — capture, don't build now.
- ~~[2026-07-13] BUG (data-corrupting): country-cache poison row `prefix='U'`~~ **FIXED + DATA REPAIRED 2026-07-13.** Mechanism: `validateCountryPrefix` (all country writers) + exported `sqlite.IsCacheableCountryPrefix` now reject one-char prefixes (the orchestrator skips the writeback QUIETLY — G/M/U/R-block calls are common, a warn per cold miss would be steady-state noise; the per-call result still reaches the caller, only the cache row is forgone); reference migration `0002_drop_single_char_prefixes` purged the existing `'U'` AND `'R'` rows (the `'R'` row was the same class — it over-matched Asiatic Russia / Kaliningrad R-calls). Tests: `TestUpsertCountry_RejectsSingleCharPrefix`, `TestMigrate0002Reference_PurgesSingleCharPrefixes`, `TestEnrich_ColdMiss_OneCharPrefix_ReturnsButNeverCaches`. Data: all 26 QSOs re-enriched (fresh hamnut per call, `refresh=true`) + repaired via `PATCH /v1/qso/{uuid}` (country=Ukraine, dxcc=288, zones/cont asserted) → 26 QRZ `update` uploads queued through the normal one-fails-all-fail path. Original finding: Found by the new `scripts/qso-audit.py` (skill `qso-audit`). The `reference.db` `country` row created 2026-06-25 14:41 has `prefix='U'`, name "European Russia", dxcc_prefix UA (hamnut's prefix for some Russian call cached verbatim); `FetchCountryByCallsign` longest-prefix-matches (`callsign LIKE prefix || '%'`), and with no longer `U*` row cached, EVERY U-call — Ukrainian UR–UZ, also Uzbek UJ–UM / Kazakh UN–UQ — hits it. **26 QSOs with Ukrainian calls stored (and uploaded to QRZ) as European Russia** (dxcc 54 or blank) between 2026-06-26 and 2026-07-11; all 42 U-calls before the row's creation are correct ("Ukraine"). The ITU U-block spans four DXCC entities, so a bare-'U' cache row can never be right. Fix has two halves: (a) mechanism — don't cache/match hamnut's raw group prefix when it's a known multi-entity block (or key the cache differently); purge the poison row; (b) data repair — re-enrich + correct the 26 QSOs and re-upload to QRZ (action='update' path).
- ~~[2026-07-13] dxcc-entities.json coverage gaps → blank numeric DXCC~~ **DONE 2026-07-13 — BACKFILL RUN + BASELINE EXTENDED.** All **716** blank-dxcc pipeline QSOs (ids 4522–5255 + gap rows) repaired via per-call re-enrich + `PATCH /v1/qso/{uuid}` (dxcc always; country/zones/cont ONLY where fresh enrichment disagreed — RST/time untouched by construction). Drift log caught MORE one-char-poison damage beyond Ukraine: **12× European→Asiatic Russia, 2× →Kazakhstan, UK7AL→Uzbekistan, RN2F→Kaliningrad, 4× England→Scotland/Wales/NI, GD7DUZ England→Isle of Man, 11× Italy→Sicily (dxcc 248 — no separate entity), US K/W zone defaults (cqz 5/ituz 8 → real zones)** — ~51+16 rows with corrected country data. **Baseline extended:** 12 entities added to `internal/enums/dxcc/dxcc-entities.json` (154→166: GD 114, UA2 126, YV 148, IT9 248, 3A 260, T2 282, UK 292, A9 304, XU 312, 5N 450, 9J 482, HI 72 — numbers verified vs ADIF 3.1.5 + log-authoritative import rows) AND mirrored as a runtime override at `~/.local/share/station-manager/dxcc-entities.json` (LoadOverride, so the INSTALLED daemon resolves them now; redundant once a release embeds the new baseline — delete then). Verified: **0 pipeline blank-dxcc remain** (1 import-era row out of scope); RST empty-counts unchanged (13/7). 742 QRZ `update` uploads queued; drain ≈ 4.6 h at tick 120 s × batch 5. Original finding: Audit of the last 90 (all FT8, 2026-07-11): XU7O, T22TT, 5N0YEN×2 logged AFTER the fix deployed still have empty `dxcc` because XU (Cambodia), T2 (Tuvalu), 5N (Nigeria) are missing from the embedded baseline (154 entities). Consider regenerating a fuller table via `scripts/gen-dxcc-entities.py` (~340 entities). Whole-logbook blank-dxcc count (CORRECTED by the 2026-07-13 full audit): **716 rows, and they are NOT historical — they're live-pipeline QSOs ids 4522–5255 (2026-06-25 → 07-11), every QSO logged between the enrichment rework and the populate-everywhere fix** (the 2026-06-25 bulk-import rows 1–4521 carry dxcc from QRZ, only 1 blank); plus the 4 baseline-gap rows. Well-bounded mechanical backfill — same re-enrich + `PATCH /v1/qso/{uuid}` pattern validated on the 26 Ukrainian repairs (QRZ `update` uploads drain automatically). (The other 4 blank-dxcc in the window — SP3HUU/VU3SPD/9A4ZM/UY5QJ, 04:37–04:42 — were logged before that morning's deploy, i.e. plain backfill cases.)
- ~~[2026-07-13] FT8 QSOs #5257 (YO4SX) + #5147 (SV1NQT) stored with EMPTY `rst_rcvd`~~ **CLOSED 2026-07-13 — NOT A BUG (operator triage).** The other station never sent the report ("something went wrong their end" — e.g. skipped straight to RR73), so SM correctly logged what was actually exchanged. Empty `rst_rcvd` on an occasional FT8 QSO is on-air reality, not a sequencer defect; no action. Rate supports it: 2 instances in ~820 pipeline QSOs — an SM bug on a sequencer path would stamp every QSO taking that path. **Re-open trigger (operator): if the audit surfaces this again at a higher rate, investigate then.** (13+5 more empty-RST FT8 rows are import-era, ids ≤4521 — source-data gaps, also no action.)
- ~~[2026-07-13]~~ **→ backlog: folded into the DB-manager SPA workstream (a data-validation surface — a DXCC/enrichment consistency checker, mirroring `scripts/qso-audit.py`'s FAIL checks).** we should think about adding some validation to the database admin SPA (when we finally get to do it), such things as DXCC checker which checks for and corrects these kinds of errors
- ~~[2026-07-13]~~ **→ backlog P3 (triage 2026-07-14; enrichment robustness — a 2nd callsign provider as a fallback chain link; flaky-link/QRZ-gap relevant for 7Q5MLV/Malawi).** **Re-enrich flow VALIDATED live** (logbook page, first day) — but RG6S can't be name-repaired: **the callsign is not on QRZ.com, and QRZ is the only callsign-class lookup configured**. Not a flow bug — no source has the data (the country layer still repairs country/dxcc/zones via hamnut). Feature seed: the enrichment orchestrator already runs a provider CHAIN (`o.Chain`) with QRZ as its only link — a **second callsign provider (e.g. HamQTH, free tier)** as a fallback link would catch some QRZ-absent calls (Russian/CIS calls are a common QRZ gap). Needs: provider client + chain config + the ADR 0017 cache semantics it already gets for free. Untriaged.
- ~~[2026-07-17] ft8 calling cq, when the sequencer maxs out its retries and abandons the contact, it does not pick up any of the other responding stations, but returns to calling cq~~ **→ backlog P2 (triage 2026-07-17; real gap, confirmed in `caller_sequencer.go`).** The answerer scan (phase 1 of `onSlotCalling`) runs only when no contact is in flight at the top of a slot, so on a max-repeats abandon (a) the abandon slot's own decodes are never scanned — the drop happens after the scan point, and that slot transmits CQ — and (b) nothing remembers the stations that answered during the failed contact's retry cycles, so callers who gave up waiting are lost entirely. (The RR73 completion path already picks a next-slot answerer without an extra CQ; abandon is the gap.) Tracked as backlog "FT8 Call-CQ: on contact abandon, work a live answerer instead of resuming CQ". **Layer 1 (same-slot rescan) BUILT same day** — the max-repeats drop now re-runs the answerer pick over the abandon slot's decodes and replies immediately, CQing only when nobody else is calling (`pickAnswererLocked`; test `TestCallerSequencer_AbandonWorksLiveAnswererSameSlot`; needs on-air validation). Layer 2 (recency-bounded answerer pool, also feeds Call-CQ waiting feedback) stays open in the backlog.
- ~~[2026-07-17] check the dogfood logs. There are some errors and the Occupancy refused to update~~ **CLOSED 2026-07-23 (triage) — no recurrence in the six days since; all three sub-findings resolved or explained.** Re-open ONLY on a fresh occurrence, capturing the observations the original note asked for (clock time · was Band Activity still updating · had a tune just run). Original triage (2026-07-17):**TRIAGED same day (occupancy self-cleared; watching for recurrence).** Log errors explained: (a) 5× `serial open failed` 2026-07-16 18:50–18:51 — the configured port was the **FT-710's adapter (`01ABC53F`), which isn't plugged in** (only the FTdx10's `01817BF4` enumerates); supervisor retried with backoff, stopped once the active rig was back to the FTdx10. Config/hardware mismatch, not a code bug. (b) `session email send failed` 06:22 — the SMTP **server** returned 451 "4.3.2 Internal server error" at AUTH (provider-side, transient); the session ADIF was archived first (`exports/sent-adif/session-20260717-042201.adi`), so nothing lost — re-send when the provider recovers. (c) Occupancy: at check time (06:50) the daemon was emitting `ft8-occupancy` on every RX slot (verified live on `/v1/ft8/events`; TX slots skip by design, so updates halve to ~30 s while running a pile-up). No hub evictions logged; daemon restarted 05:32 (the deploy). **Cause of the stall window not identified from logs** (occupancy logs nothing at info). If it recurs: note the clock time + whether Band Activity was still updating (separates decode-stall from occupancy-only) + whether a tune had just run (the tune-carrier-not-marked backlog item is adjacent).
- ~~[2026-07-17] zoom function for the map~~ **→ backlog P2 (triage 2026-07-18).** Folded with the hover-tooltip note below into ONE item — "Contacts map — zoom/pan + station hover tooltip" — because they share coordinate machinery (the tooltip's hit-test must run in zoom-transformed screen space, so building them separately means rework). Zoom/pan via `d3-zoom` transform or projection scale/translate on the existing engine; reset control; all layers ride the transform.
- ~~[2026-07-17] hover over remote station = tooltip with some details~~ **→ backlog P2 (triage 2026-07-18; same item as the zoom note above).** Hover an arc endpoint → tooltip with callsign, band/mode, time, grid, distance + bearing — all already computed/available SPA-side (`pathInfo` + the plotted QSO set); needs pointermove nearest-endpoint hit-testing + a positioned tooltip component. Overlapping endpoints at low zoom show a count/list — zoom disambiguates (the other reason the two pair).
- ~~[2026-07-18] map bug: The session timer says 14:58, but the map lists QSOs from 1 hour ago+~~ **NOT A BUG — WAI per ADR 0049 (triage 2026-07-18).** The map is deliberately session-blind: its window is "QSOs in the last N minutes" (the Window selector, default **6 h**), not "this session" — ADR 0049 rejected a session entity for the map, and `mapData` consumes no session state by design. A 15-min session with a 6-h window correctly shows the previous hours' QSOs. Remedy today = pick a shorter window (15/30 min). **Revisit trigger:** if the mismatch keeps chafing while operating, the enhancement would be a "since session start" window preset — but that re-couples the map to a session signal ADR 0049 deliberately excluded, so it needs a conscious ADR revisit, not a quick patch.
- ~~[2026-07-18] editing a QSO while running FT8 takes the station off-air~~ **FIXED same day (option a — in-place session edit).** Diagnosis: mechanically WAI but a real UX trap — SessionPanel had no in-place edit, so editing navigated to the Logbook ROUTE → FT8 view unmounts → SSE subscriber drops → 5 s linger → mic released (demand-driven capture, by design) → no scheduler slots → the CQ run goes silent. Fix: `EditQsoModal` decoupled to injected props (ADR 0045) + new `sessionEdit` controller (hydrate `GET /v1/qso/{uuid}` → modal in place → PATCH → canonical write-back onto the session row) + the Session-card callsign as the edit button (fixed column widths untouched). No route change ⇒ capture never released ⇒ the run stays on air; Re-enrich included for free. Options (b) capture-across-routes and (c) navigation warning were NOT taken — (a) removes the trap at the root without touching the capture design. Needs dogfood validation after redeploy (edit a session QSO mid-CQ-run; confirm TX cadence never breaks).
- ~~[2026-07-18] map bug: map open in another tab, but not active - does not update - 6 qsos, 4 80m arcs~~ **→ backlog P2 (triage 2026-07-18; real gap).** Background-tab staleness: a hidden tab gets throttled timers (the 300 ms `scheduleRefresh` debounce) and possibly a silently-dead SSE, and the map has NO `visibilitychange` handling (verified — neither `mapData` nor `api/log-events.ts`), so nothing forces a catch-up refetch when the tab becomes visible again. NB the second-monitor posture (own visible window) is NOT affected — browsers throttle on visibility, not focus; this bites the same-window hidden-tab case. Fix direction in the backlog item ("Contacts map — background-tab staleness"): refresh on `visibilitychange`→visible (+ treat it as a reconnect check), which heals every root cause at the moment it matters.
- ~~[2026-07-18] FT8 capture has no dead-stream detection: KDE Plasma device fiddling destroyed+recreated the rig codec's PipeWire nodes mid-capture, leaving smd's source-output DANGLING (Source: 4294967295 = no source) — the daemon stayed in "decoding live slots" capturing pure silence with ZERO errors logged~~ **BUILT same day (dead-stream watchdog).** `internal/ft8/deadsource.go`: a scheduler-side monitor closes a window at every 15 s boundary (the timer fires even when the source delivers NOTHING — the incident's shape, where the ring never filled so no Slot was ever emitted and a decode-side check would never have run) and marks it dead when starved (< quarter-slot delivered) or silent (all literal zeros; an analog input always carries ADC noise). Two consecutive dead windows (the CAT `noDataStrikeLimit` pattern) → warn log + async release + reacquire — the release's tail re-acquires for the still-present subscriber, creating a fresh OS stream that links to the current device nodes: the manual close/reopen fix, automated. Fires once per session (fresh session = fresh monitor); reacquire-fails falls back to the proven CAT-reconcile retry loop. Worst case ~45 s outage instead of silent-forever. 7 new tests (pure strike-policy + service release/reacquire plumbing); needs dogfood validation at next Plasma fiddle (or `pw-cli destroy` on the codec node mid-capture). Original incident detail: pactl move-source-output refuses a dangling stream ("Invalid argument"); the "device not found" boot-retry loop worked perfectly — the gap was only the established-stream-goes-dead case.
- ~~[2026-07-18] P0-class TX-safety gap: rig stuck in CONTINUOUS TRANSMIT after a USB write-endpoint stall~~ **CLOSED 2026-07-23 (triage) — all three mitigations landed.** (1) **HW** — the failing hub is out of the path; the rig now runs on a direct root port. (2) **Rig-side** — the FTdx10 TX time-out timer is ENABLED (the one true guaranteed stop when the wire dies). (3) **SM stuck-TX alarm — BUILT**, and *proven in the field*: it fired correctly during the 2026-07-21 second incident (ERROR log + `tx-alarm {active:true, code:"tx_still_keyed"}` SSE + persistent SPA banner), which is exactly what this note asked for. Residual work moved to the 2026-07-21 item below (the alarm's clear/ack gap) — that is now the only open half of this subsystem. Original note follows. — found live 2026-07-18 ~15:42: rig stuck in CONTINUOUS TRANSMIT after the USB hub (port 5-2.3, carries both the CP2105 CAT bridge AND likely the PCM2903C codec — same hub behind the morning "Plasma" audio incident) stalled its serial write endpoint mid-transmission (kernel: urb stopped -32 on every write from ~15:42; clear-tt EPROTO errors from 15:35:32). TX1+audio for the RR73 keyed the rig BEFORE the stall; the unkey TX0 at 15:42:13 and EVERY backstop after it (18s auto-off, disarm, release-on-disconnect) were written into the dead pipe and "succeeded" daemon-side — log looks perfectly clean while the rig sat keyed. Liveness correctly flagged rig/CAT dropped at 15:42:24 but no alarm reached the operator; recovery was manual (kill daemon + unkey at rig, 15:44:19). NOT a tab-close bug (coincident timing only — session abandon/disarm ran cleanly). Mitigations: (1) HW — replace the failing hub before next TX; (2) rig-side — enable the FTdx10 TX time-out timer (the only true guaranteed stop when the wire dies; consider documenting as an operator prerequisite for TX features, like CI-V Transceive); (3) SM — a stuck-TX alarm: when TX was keyed within the last ~20s AND (unkey write errors OR CAT liveness drops OR serial teardown fires), emit ERROR + a dedicated SSE alarm event the SPA renders as a persistent "CHECK YOUR RADIO — possibly stuck in TX" banner (+ maybe attempt a port reopen + best-effort TX0 re-send, and consider USBDEVFS_RESET escalation). Detection is cheap; the silent clean-looking teardown was the failure. Related verified-open backlog item: bridge auto-off retry generation counter (different bug, same subsystem).
- ~~[2026-07-18] ClubLog API key — decide HOW SM distributes it (embed in the GPL repo vs paste-in step)~~ **RESOLVED 2026-07-19/20 by ADR 0054 — NEITHER (a) nor (b): the key is BUILD-INJECTED.** `-ldflags -X …/forwarding/clublog.InjectedAPIKey` reads it from the gitignored `.env` at build time, so it is not a config credential, not in source, and not a paste-in step for the operator — releases carry it, the repo does not. The ClubLog grant condition (realtime.php must never carry catch-up batches) is recorded in the note below and honoured: enabling the forwarder only queues NEW QSOs (ADR 0039), and historical catch-up goes via a manual ADIF upload / putlogs.php. The in-app putlogs.php bulk path stays a backlog item. Remaining action is operational, not a decision: **enable the forwarder at the next on-air test** (already on the active-cycle list). Original note follows. — requested 2026-07-18 (via helpdesk; form answers drafted from the actual clublog.go behaviour — 403 breaker, 60s→30min×5 retry, 120s/5 batch pacing, stdlib net/url encoding). The key is APPLICATION-specific (operator-confirmed): it will be Station Manager's key, shared by every SM install; operators authenticate individually with email + application password + callsign. Decision needed when the key arrives: HOW SM distributes it — (a) embed as the clublog forwarder's default `api` credential (public in the GPL repo; standard practice among OSS loggers, tolerated by ClubLog) so operators (7Q8AC!) configure only their three personal fields, vs (b) keep it out of the repo and document a paste-in step (friction, but the key stays semi-private). Lean (a) for onboarding; check any conditions ClubLog attaches to the grant first. **ClubLog condition (helpdesk reply, 2026-07-19): realtime.php must NEVER carry catch-up batches of pre-existing QSOs — that anti-pattern gets the key blocked; bulk goes via putlogs.php only. Confirmed back to them same day.** Consequence: the backfill plan is REVISED — SM's Logbook backfill button (which rides the worker → realtime.php one QSO at a time) must NOT be pointed at ClubLog for historical sets. When the key arrives: enable forwarder + restart (new QSOs flow via realtime.php as they're logged, as designed — enabling can't flood, since QSOs logged while disabled are never auto-queued per ADR 0039), and do the historical catch-up (the 3 failed rows ZR1ADI/JA1IST/JG8FWH + the disabled-window gap + the 5.6k history) as ONE ADIF export uploaded manually on clublog.org (or via putlogs.php) — zero code needed. A putlogs.php bulk path in the forwarder is logged in the backlog as the longer-term fix so 7Q8AC-style operators get in-app backfill without the manual step.
- ~~[2026-07-18] frontend/app: the header CAT chip should TOGGLE the Rig panel, not just open it~~ **BUILT same day (+ the paired rename).** `Header.svelte` `toggleRigPanel`: on Operate the chip toggles the panel (second click dismisses); from any OTHER view it navigates to Operate and REVEALS (show, not toggle — never a blind toggle-off of a panel the operator can't see, and arriving keeps an already-open panel open). Works in both hosts via the shared tile state (Phone tile board + FT8 overlay stack). The **"Rig → Rig Control" rename** landed with it (RigPanel `<h3>` + the `TILES` registry name, so the rail label matches). 2 new Header tests pin both behaviours; full suite 635 green + lint/check/format. Backlog's rename one-liner is thereby done too.
- ~~[2026-07-18] tailwindcss truncate not working in the Session panel Name column (frontend/app)~~ **FIXED same day.** Root cause: the table used AUTO layout, where `truncate`/ellipsis on a `<td>` is inert — cells grow to fit content and `w-*` is only a suggestion (the tds already carried the ellipsis classes from an earlier attempt). Fix: `w-full table-fixed` on the SessionPanel table so the header row's widths BIND and the existing ellipsis classes engage on Name + Country.
- ~~[2026-07-19] FT8: on a band change, Band Activity should clear all decodes from the previous band~~ **BUILT same day (frontend/app port gap).** The logging SPA already had the band-change clear (Ft8Panel effect, with intra-band-nudge + transient-unknown guards and the pile-up drop); frontend/app had `clearDecodes()` defined with the exact "band change makes prior rows misleading" comment — and nothing calling it. Ported as `ft8State.noteOperatingBand(band)` (unit-testable transition logic: first sighting records; same band = no-op; genuine band-to-band change clears decodes AND the pile-up queue — its callers were the old band's watering hole; empty band ignored) + a one-line `$effect` in Ft8View feeding it `rig.band`. `lastSeenBand` deliberately survives view close, so a band change made while the FT8 view is closed still drops the persistent pile-up on reopen. 4 new state tests; app suite 639 green + lint/check/format/build.
- ~~[2026-07-21] SECOND stuck-TX incident — the tx-alarm is slow + opaque to clear, with no operator ack~~ **→ backlog P1 (triage 2026-07-23; finding (A) mechanism VERIFIED IN CODE and sharper than this note assumed — finding (B) is operator/hardware).** The note guessed the alarm's ~13-minute persistence came from an unidentified re-trigger. It is worse and fully deterministic: **the alarm latches itself out of every clear path.** `confirmTxIdle` (the only clear) runs on an observed TXSTATUS, and the only code that ever *asks* for one is `beginTxConfirm`, reached solely from an FT8 or tune unkey — but `ft8tx.go:105`, `tune.go:152` AND the generic `command.go:126` all refuse while `txUncertain`, which the alarm sets and holds. `read_tx_status` is NOT in the FTdx10 rigdef's periodic `READ` set (`ID;FA;FB;ST;VS;MD0;MD1;PC;`), and the liveness fallback (`observeRigData`) is deliberately disabled for rigs that HAVE a status query. So for the FTdx10/FT-710 the only clear paths are an **unsolicited AI push** (what actually cleared it at 05:06:54, once the power-cycled rig resumed pushing) or a **pipeline reconnect** — neither of which the operator can invoke, and the CP2105 staying enumerated across a rig power-cycle means the reconnect never fires. The remedy is an **alarm-driven one-shot `TX;` re-poll** that bypasses the `txUncertain` gate (it is a READ — it cannot key anything), plus an operator-triggered **"re-check the rig"** action so the operator can ask rather than only wait. **CORRECTION to this note's own ask (2026-07-23, after review):** do NOT build the daemon-side "clear alarm" endpoint it proposes. Clearing `txUncertain` would re-enable keying and generic commands while the rig may still be transmitting — the exact ADR 0051 guarantee — and publishing `tx-alarm {active:false}` without positive RX evidence would retire the only standing warning for every tab and every late subscriber (the hub caches it). Acknowledgement must stay LOCAL/UI-only, which `frontend/app`'s `TxAlarmBanner` already does (`dismissTxAlarm` sets a client-side flag, a new alarm re-shows the banner, daemon state untouched) — so what is missing next to that Dismiss is a RECHECK button, not a clear. Only `confirmTxIdle`, on positive evidence, may publish an inactive alarm. Tracked as backlog "Bridge: tx-alarm cannot self-clear — alarm-driven re-poll + operator recheck". Original note follows. — live 2026-07-21 ~04:53 — genuine stuck transmit (operator SAW the rig TXing, confirmed), but a DIFFERENT failure class from the 2026-07-18 P0 (that was a USB write-endpoint stall; **this one's kernel USB log was CLEAN**). Context: Call-CQ caller pile-up on **30m** (10.136, offset 2750), working US0QV (KN77). Timeline (smd.log): 04:53:00 transmitted the reporting rung `US0QV 7Q5MLV -16` → 04:53:43 ALARM `bridge: rig reports CAT TX still keyed after unkey — CHECK YOUR RADIO` + SSE `tx-alarm {active:true, code:"tx_still_keyed"}` + `skipping ft8 mode restore — unkey unconfirmed` → 04:54:00 next rung `caller rung transmit failed: "ft8: rig not ready to transmit"` (the TxReady interlock correctly REFUSED to re-key on top of the stuck carrier) → 04:54:35 `ft8 tx: disarmed`. Recovery = operator **power-cycled the rig** → normal. **Diagnosis (verified):** `journalctl -k` for the window = ZERO USB errors (no urb/-32/EPROTO/disconnect) — unlike 224. tx-status `confirmed idle` had polled clean every 30 s right up to 04:53:13; rig-state SSE streamed normally before AND after; smd never restarted (PID 9080 since 04:23:18). So the USB transport was electrically healthy and the confirm-READ was accurate (the rig really was still keyed) — the rig simply **did not act on the `TX0;` unkey while transmitting on 30m**. Prime suspect = **30m RF ingress corrupting the CAT serial content / locking the rig's CAT-PTT under RF** (operator's own hypothesis; same band + the DX Commander vertical was the suspected RFI source flagged in the 224 note). Upshot: **the CAT-based guaranteed-stop (`TX0;`) is not reliable on 30m under this RFI**, independent of the USB layer being fine. What held the line: (1) the stuck-TX **alarm — the 224 gap-fix — fired correctly** (ERROR + SSE banner; the daemon KNEW and told the operator, exactly the 224 ask); (2) the **TxReady interlock refused to re-key**; (3) the **FTdx10 TOT (3 min)** is the true hardware backstop and would have force-dropped it (operator beat it manually). **SM findings for triage:** **(A) P1 — the tx_still_keyed alarm is SLOW + OPAQUE to clear, with no operator ack.** (CORRECTED on the recovery, verified in the log:) an auto-clear path DOES exist — at 05:06:54 the bridge logged `bridge: tx alarm cleared (transmitter confirmed idle)` on the next `tx-status` poll that confirmed idle, and this fired on the OLD daemon (PID 9080) ~1 min BEFORE the `systemctl --user restart smd`, so a restart was NOT actually required. The real gap: after the alarm the periodic tx-status poll went SILENT for ~13 min (heartbeats stopped 04:53:13 → resumed 05:06:54; the exact re-trigger is unconfirmed — no tune/arm/command POST appears in the 05:04–05:07 window), so the operator sat in front of a persistent "CHECK YOUR RADIO" banner for ~13 min with no way to dismiss it and no signal of when it would self-clear. The hub also caches `tx-alarm {active:true}` for late SSE subscribers, and a rig front-panel power-cycle doesn't clear it (the CP2105 stays USB-enumerated → no pipeline reconnect to reset daemon state), so the banner persisted through the physical recovery until the internal clear fired. Needs: a PROMPT one-shot tx-status re-poll driven BY the alarm itself (confirm idle within a second or two → auto-clear), AND an explicit operator clear/ack (SSE clear event + SPA "acknowledge / clear alarm" button + endpoint) for the case where the rig genuinely can't be re-probed. (NB the clean shape is already proven on reconnect: the 05:07:56 restart pipeline sent the ADR 0051 defensive `tx_off` on confirmed connection and immediately read `tx-status 0` idle — a fresh connect always resets cleanly; the gap is only the no-reconnect, same-process case.) **(B) RF/operational — 30m RFI defeats the CAT unkey:** RF-side mitigations (common-mode choke on the USB lead, more ferrites, lower power on 30m, feedline/vertical work); TOT is the net. Bigger question worth weighing: a **hardware PTT line (RTS/DTR) unkey path** would be more RFI-robust than CAT `TX0;` — the guaranteed-stop currently assumes CAT is reliable, which 30m RFI just disproved twice-adjacent. Pairs with the 224 note (same TX-safety subsystem, complementary failure modes: USB-stall vs RFI-corrupted-CAT).
- ~~[2026-07-21] add Notes field to the phone/cw panel~~ **DONE — already BUILT 2026-07-21** (same day as the note; likely logged just before the build landed). `LoggingCard.svelte` has a collapsible **Contact details** disclosure holding a Notes `<textarea>` (`draft.notes`), kept off the fast path by design.
- ~~[2026-07-21] add remote rig, pwr, etc to phone/cw panel~~ **DONE — already BUILT 2026-07-21** (same disclosure as the Notes item). Contact details holds the contacted-station **Rig** + **RX Power (W)** inputs (`draft.rig` / `draft.rxPwr`, the latter ADIF-validated — commits `db053f3b`→`29910fe9`), plus read-only **QRZ page link** and **looked-up email**.
- ~~[2026-07-21] click on VFO-A and VFO-B VFO-B retains the swapped freq - not the actual frequency.~~ **→ backlog P2 (triage 2026-07-23; folded with the two VFO notes below into ONE item — they are the same surface and the same fix pass).** Verified as DESIGNED-not-broken, but the design is the complaint: clicking the non-selected VFO box calls `selectVfo` → `swapVfoLive` → `swap_vfo` (`SV;`), which **exchanges A↔B contents** rather than selecting a VFO. `rig.svelte.ts` documents why — the FTdx10 has no CAT command that moves the operating frequency onto a named VFO (`VS` toggles a flag only) — so "select the other" was equated with "swap". The operator's expectation (select B, frequencies stay put) is not reachable with the current rigdef; changing it needs either a `VS`-based select op added to the rigdef (and a decision about what "selected" then means for the freq-step ops) or a relabel of the boxes so the swap semantic is explicit. **Decision needed before building.**
- ~~[2026-07-21] change between VOF-A/B should also action on clicking the label as well as the input field~~ **→ backlog P2 (triage 2026-07-23; same item as above).** Confirmed trivial and real: in `RigPanel.svelte` the `VFO-{v}` label is a `<span>` OUTSIDE the `<button>`, so only the frequency box is a click target. The whole label+box group should be one control (mind the a11y: one button, one accessible name — not a nested-interactive).
- ~~[2026-07-21] moving between VFOs - only the main freq is displayed, the sub is not rendered~~ **→ backlog P2 (triage 2026-07-23; same item as above) — NEEDS ON-RIG OBSERVATION.** Not reproducible from the code alone: `RigPanel` renders BOTH VFOs from a `{#each VFOS}` and shows `—` only when `rig.vfoA/vfoB` is `null`; `FB;` IS in the FTdx10 rigdef `READ` set, and `swapVfoLive` optimistically mirrors `vfoB = vfoA` before the command so the post-swap value should be right even if the daemon re-reads only the operating VFO. So the sub going blank points at a *refresh* path (a `null` reset — `rig.svelte.ts:703` — or a missing re-READ after some transition) rather than the render. **Capture next time: which VFO went blank, after exactly what action, and whether it recovered on its own or needed a reconnect.**
- ~~[2026-07-21] session email resend sends ALL session QSOs, not just unsent ones~~ **→ backlog P2 (triage 2026-07-23; CONFIRMED exactly as written).** `ExportDialog.svelte:35` — `uuids = session.qsos.map(q => q.uuid).filter(...)`, the whole session with no `sm_fwrd_by_email_status` filter, and that array feeds BOTH `exportSessionAdif` and `sendSessionEmail`. Tracked as backlog "Session email — send-only-not-yet-emailed delta option". Pairs with the logbook 'not emailed only' item below (same status field, same operator intent — worth one pass).
- ~~[2026-07-21] logbook. When selecting row, the table jumps down because of the header appearing.~~ **→ backlog P2 (triage 2026-07-23; CONFIRMED, and the fix is trivial).** `Logbook.svelte:110` — the selection toolbar (count · Upload · Email · Clear) is inside the controls row under `{#if logbookState.selectedCount > 0}`, so selecting the first row grows that row and shoves the table down; clearing the selection snaps it back. Fix = reserve the space (render the bar always, contents hidden/disabled at zero selection, or give the row a fixed min-height) so selection never reflows the table.
- ~~[2026-07-21] logbook: when sending an email no toast is show for sending, or sent~~ **→ backlog P3 (triage 2026-07-23; CONFIRMED — accurate description of the code).** `LogbookEmailControls.svelte` reports entirely through local state: the button label flips to "Sending…" and the outcome lands in an inline `result` object rendered in the controls row — no `toasts.*` call anywhere in the component, unlike the rest of the app. Note the inline text is deliberately verbose for the ambiguous-network case ("the email may still have gone out"), so a fix should ADD toasts for the send/sent transitions while KEEPING that inline warning — do not swap a 3-second toast in for it.
- ~~[2026-07-21] logbook 'not emailed only' doesn't work~~ **→ backlog P2 (triage 2026-07-23; CONFIRMED real, root cause identified — it is PAGE-LOCAL, not broken).** `logbook.svelte.ts:150` filters `this.rows` (the CURRENTLY LOADED page) by `sm_fwrd_by_email_status !== 'Y'`, and its own doc comment says "No reload — it just hides emailed rows from the loaded page." The daemon's list endpoint (`GET /v1/logbook/{id}/qso`) accepts only `limit` / `after` / `missing_from` — there is NO email-status filter server-side. So on a multi-thousand-QSO logbook the toggle hides a few rows of the current page and looks inert, which is exactly the reported symptom. The field itself is plumbed correctly end to end (`types.Qso.SmFwrdByEmailStatus` → top-level JSON → `LogbookQso.sm_fwrd_by_email_status`), so this is a SCOPE fix: add a server-side not-emailed filter param (mirroring `missing_from`) and drive the toggle off it, or relabel the control to say it applies to this page.
- ~~[2026-07-21] check if we need to update the ADIF field for CLUBLOG_QSO_UPLOAD_DATE~~ **CHECKED — NO ACTION (triage 2026-07-23).** Already handled: `adif.ClubLogQsoUploadDate` maps the field in both directions (`adif.go:81/128/187`), the spec-validation test types it as an ADIF Date, and `clublog.AdifPrefix` returns "CLUBLOG" so the forwarding worker stamps `CLUBLOG_QSO_UPLOAD_STATUS`/`_DATE` on the row after a successful upload — the same stamp mechanism QRZ uses. Nothing to add; the values simply won't appear until the ClubLog forwarder is enabled and QSOs actually upload.
- ~~[2026-07-22] the phone/cw panel need QTH field~~ **WAI / placement decision, not a missing field (triage 2026-07-23).** QTH IS editable — it lives in the **ContactDialog** overlay alongside Gridsquare (`ContactDialog.svelte:198-205`), and `LoggingCard`'s header comment records the deliberate split: "QTH / grid live in the ContactDialog overlay; the grid is enrichment-filled, not typed here." So the ask is really "promote QTH onto the card", which competes with the reason the fast path is kept short. **If you still want it on the card**, the cheap version is to add it to the existing Contact-details disclosure (where Rig/RX-power/Notes already are) rather than the always-visible row — say so and it is a small build; otherwise this closes as WAI.
- ~~[2026-07-23]~~ **→ stuck-TX subsystem tracking (triaged 2026-07-24): investigation, not a build. Superseded by the reproduction results (next item) — the alarm re-probe + Re-check are already BUILT (`txrecheck.go`); RF ingress into the antenna is now the lead. Operational carry-over: deploy current main before the next TX session (the incident build predated the re-probe fix).** **THIRD stuck-TX incident — live 06:36, and this one is the TUNE path, not FT8.** Operator finished an FT8 CQ run, moved to SSB on **20m**, found a frequency and tuned at **30 W**; the carrier would not stop and the operator **switched the radio off** to kill it. Timeline from `smd.log`: `06:36:14` ft8 tx disarmed + capture released → `06:36:27` `POST /v1/rig/tune` (ON, 202) → `06:36:29` tune OFF, and the rig **ANSWERED the status query with `TX1;`** → `bridge: rig reports CAT TX still keyed after unkey — CHECK YOUR RADIO` + `tx_still_keyed` → `06:36:32/33/37` three operator re-tune attempts, all correctly refused `409 rig_tx_unconfirmed` → `06:38:33` rig finally reported idle, alarm auto-cleared. **~2 minutes of unintended transmission.** **DISTINCT from the two prior incidents:** 2026-07-18 was a USB write-endpoint stall (kernel urb errors); 2026-07-21 was 30m with a clean kernel log (RFI-corrupted CAT hypothesis). This one has **NO identity error, NO CAT loss, NO liveness event anywhere in today's log** — CAT was demonstrably healthy throughout, since the rig both raised the alarm (answered `TX1;`) and cleared it (answered again). 20m at 30 W also does not fit the 30m-RFI theory. (The operator recalled "rig id was wrong / lost CAT" as part of this event — the log shows that was a SEPARATE earlier problem at `04:58`: three `rig_identity_unverified` 409s, resolved by the `smd` restart at 04:58:57.) **PRIME HYPOTHESIS — the tune was 2 SECONDS long** (on 06:36:27, off 06:36:29). A key/unkey that fast may hit the FTdx10 mid-TX-ramp, where it appears to drop the unkey; that would also explain why the 15 s FT8 cadence never shows it. **Test on a dummy load at low power: does a 5+ second tune stop cleanly while a ~2 second tune sticks?** **SECOND tune anomaly the same session:** `05:10:00` `bridge: tune auto-off fired; carrier dropped` — an earlier tune ran to the 15 s hard backstop instead of being stopped. Two tune anomalies, zero FT8 anomalies, in one session. **WHAT HELD:** the power/mode restore was correctly **SKIPPED** (`skipping tune mode/power restore — unkey unconfirmed`) — the 2026-07-19 review P1 fix working exactly as designed, so no full-power `PC` write went into a possibly-keyed rig; and the TxReady interlock refused all three re-key attempts. **WHAT DIDN'T:** the deployed build was `00921ce9`, which predates the 2026-07-23 alarm re-probe loop + Re-check endpoint — so nothing re-asked the rig and the operator had no action available; the 06:38:33 clear came only from an unsolicited AI push once the rig's state actually changed. **→ deploy current main before the next TX session.** **SIDE OBSERVATION worth chasing:** every FT8 unkey in the preceding 12 minutes confirmed with `tx-status 2` = "TX by OTHER means", not `0` = RX (every 30 s, matching the every-other-slot CQ cadence) — something besides CAT is asserting PTT, most likely audio/VOX data-mode keying. Check the rig's data-mode PTT setting; it may also be relevant to why a CAT unkey alone did not drop the tune carrier.
- ~~[2026-07-23] **ClubLog forwarder ENABLED and confirmed working on air** (closes the long-standing "enable at next on-air test" item).~~ **CLOSED (triaged 2026-07-24) — the standing "enable ClubLog at next on-air test" action is done; nothing to build.** Three successful `realtime.php` uploads: K1OFO 06:46:58, WA7M 06:52:57, ZS6RF 07:08:57 — all also forwarded cleanly to QRZ and smcloud. Empty `upstream_id` is normal for realtime.php (it returns no record id). The key is build-injected per ADR 0054, so nothing operator-facing to configure.
- ~~[2026-07-23]~~ **→ stuck-TX subsystem tracking (triaged 2026-07-24): investigation, not a build. Duration + FT8-residue hypotheses both refuted on a dummy load; RF ingress into the antenna is the remaining lead. Next step is an operator RF experiment (2 s tune into the antenna, 20 m) — if it reproduces, the fix direction is RF mitigation + the hardware-PTT avenue parked in ADR 0057, NOT more CAT detection logic.** **Stuck-tune reproduction attempts — BOTH leading hypotheses DEAD.** All trials into a **dummy load**, via `scripts/tune-duration-probe.sh`, on the pre-fix build (deliberately: the new re-unkey retry would have fought a stuck carrier and masked the raw rig behaviour). **(1) Duration — 0 of 3 stuck at 5 s, 0 of 3 at 2 s.** The "a ~2-second tune catches the FTdx10 mid-TX-ramp" hypothesis is refuted. **(2) FT8 residue — 0 of 3 stuck.** A faithful replay of the incident shape: FT8 CQ transmitting `08:09:00` → session abandoned + `ft8 tx: disarmed` `08:09:15` → tune ON `08:09:27` (12 s later; the incident was 13 s) → three 2 s tune cycles, all `confirmed idle`. **Zero alarms logged since 08:00.** **CORRECTION to the earlier `tx-status 2` lead:** "TX by other means" appears on these CLEAN tune confirmations too, so it is simply how this FTdx10 reports post-unkey state in these modes — NOT evidence that something else is asserting PTT, and not the thread it looked like. **REMAINING VARIABLE: the antenna.** Every test today was into a dummy load; the incident was into the DX Commander vertical at 30 W on 20 m. RF ingress on the CAT/USB path is therefore back as the leading candidate (and it was already the 2026-07-21 hypothesis on 30 m). Next experiment, operator's call on RF exposure: the same 2 s trials **into the antenna** on 20 m. If that reproduces, the fix direction is RF mitigation (common-mode choke on the USB lead, ferrites) plus the hardware-PTT-line avenue parked in ADR 0057 — NOT more CAT-level detection logic. If it does not reproduce, the fault is intermittent and we wait for the next occurrence with a timestamp. **Tooling caveat worth knowing:** the probe script's alarm counter was buggy on first use (`grep -c || echo 0` yields `0\n0` on a clean log, breaking the comparison so a stuck trial would print "clean"). These results are unaffected — the log already held the 06:36 alarm, so the count returned a clean integer — but it was luck, not design. Fixed, along with unvalidated HTTP statuses (a refused stop used to print "clean" while the carrier was potentially still up) and an off-by-one that reported "0 of 4" for a 3-trial run.
- ~~[2026-07-23]~~ **→ backlog (triaged 2026-07-24): folds into the existing "Operator log viewer (daemon diagnostics) — DB-manager tab" item (backlog); the user-manual "how to read `smd.log` after a stuck-TX" note is the concrete first use case. No new entry needed.** at some point we need to build a logviewer - notice in the notes you added to the user manual: **How to tell.** In `smd.log`, look at what the rig answered after the stop:
- ~~[2026-07-24] https://qrz.digital/api/swagger-ui/index.html - for further investigation~~ **→ backlog (triaged 2026-07-24): candidate for the "2nd callsign-enrichment provider" item (a REST/OpenAPI service); folded there with the two below as providers to evaluate. Role — enrichment lookup vs logbook forwarder — is part of that deferred investigation.**
- ~~[2026-07-24] https://www.qrzcq.com/page/developers - for further investigation~~ **→ backlog (triaged 2026-07-24): candidate provider for the "2nd callsign-enrichment provider" item; QRZCQ also hosts a logbook, so possibly a forwarder destination too. Folded (capture-for-evaluation).**
- ~~[2026-07-24] https://www.hamqth.com/developers.php - for further investigation~~ **→ backlog (triaged 2026-07-24): the API docs for HamQTH, already the NAMED candidate in the "2nd callsign-enrichment provider" backlog item; URL attached there.**
- ~~[2026-07-25] the worked panel for phone/cw needs the table to be set to table-fixed as the columns are bleeding into each other and a large field (note) overscolls.~~ **→ DONE 2026-07-26.** `table-fixed` was already there — the missing piece was **`w-full`**. Per CSS 2.1 §17.5.2 a table with `width: auto` uses the AUTOMATIC layout algorithm and `table-layout: fixed` is ignored, so the `w-*` on the th row never bound: cells grew to fit content (the bleeding) and a long Notes value stretched the table into a horizontal overscroll instead of ellipsizing. Exactly the SessionPanel bug fixed 2026-07-18, same one-word fix. A survey of every `<table>` in the app confirms WorkedPanel was the ONLY one missing `w-full`; the other three already had it.
- ~~[2026-07-25] session panel: map button is redundant~~ **→ DONE 2026-07-26.** Removed. The sidebar's bottom-utilities Map link is always on screen and opens the identical new tab (same href/target/rel), so the tile button was pure duplication. Its test coverage MOVED rather than vanished — the map link was previously tested only through SessionPanel, so a new `Sidebar.svelte.test.ts` now guards it (the `target="_blank"` matters: opening in-tab would unmount a live FT8 run).
- ~~[2026-07-26] ftp-all.txt should be enhanced to do archiving (gzip) and log rotation.~~ **→ DONE 2026-07-26: `ft8-all.txt` now rotates via lumberjack (10 MB × 5 gzipped backups) and is created 0600 (was 0644, and a legacy log is tightened on open). `smd.log` already had rotation + gzip (100 MB × 5 / 30 days); the survey also found `cmd/smd/startuplog.go` creating smd.log 0644 — fixed to 0600.**
- ~~[2026-07-26] add a world time widget~~ **→ backlog P3 (triage 2026-08-07): decide together with the map time-zone overlay below — they answer different questions and one decision should cover both.** The 2026-08-01 analysis below stands (open questions remain the operator's).
  — **TRIAGED 2026-08-01, not started.** Nothing exists: no clock component anywhere
  in `frontend/app/src` (the UTC references are all timestamp FORMATTING inside
  Ft8/Logging cards, not a clock). SPA-only, no daemon change, no rig interaction —
  so it is deployable without touching the running daemon, which makes it unusually
  cheap to land. **Open questions for the operator, none to be invented:** which
  zones (UTC only? UTC + local? a chosen set for skeds?), where it lives (shell
  header beside the session timer, or a tile), and whether it needs to be
  second-accurate — FT8 already depends on system time being right, so a clock that
  disagreed with the slot clock would be actively misleading, and that argues for
  driving it from the same source rather than a fresh `new Date()`.
- ~~[2026-08-04] Map: overlay the time zones — or the CURRENT TIME along each zone
  line.~~ **→ backlog P3 (triage 2026-08-07): one decision with the world-time widget
  above; recommendation (A) solar stands.** Raised while reviewing what is left on
  the map. **Analysed, not started; the analysis is here so it is not re-derived.**
  — **RELATED TO THE WORLD-TIME WIDGET ABOVE, and the first thing to settle:**
  they answer different questions. A widget says what time it is in Tokyo; the
  overlay says what time it is WHERE THIS ARC LANDS. Decide whether the overlay
  supersedes the widget, or they are complementary, before building either.
  — **TWO DIFFERENT FEATURES SHARE THE NAME.** (A) SOLAR / nautical zones: 24
  bands of 15°, offset = round(lon/15). No dataset at all — pure geometry plus
  arithmetic, exact, never stale, no licensing question. (B) POLITICAL zones:
  real boundaries, half-hour offsets, DST. Needs a boundary dataset, and the
  usual one (timezone-boundary-builder) is OSM-derived → **ODbL, which the
  basemap decision explicitly excludes** ("AVOID OSM/ODbL-derived data
  (share-alike)", dogfood 2026-07-04). Natural Earth publishes a public-domain
  time-zones layer that would sidestep that — UNVERIFIED whether it is available
  in the TopoJSON shape we use, and at what size; `countries-50m` is already
  756 kB and the chunk-size warning limit was raised to 800 kB for it.
  — **RECOMMENDATION: (A), and not merely because it is cheaper.** The map's
  headline overlay is the GREY LINE, a solar phenomenon, and everything an
  operator reads this map for tracks solar time — grey-line enhancement, MUF,
  sunrise/sunset. Political time is LESS useful for that decision and far more
  expensive: China spans five solar zones on one clock, India is +5:30, DST
  shifts twice a year. Label it **solar time**, honestly. Labelling nautical
  bands "local time" is the one genuinely wrong option.
  — **CHEAPER THAN IT LOOKS, because the parts exist:** the 60 s clock already
  ticks for the terminator (`MapView.svelte`), `geoPath` already draws arbitrary
  geometry through the projection, the overlay pattern IS the terminator, and the
  toggle+persistence pattern is Grey line. It SHOULD persist, by the same
  reasoning that says the band filter should not: this ADDS an overlay, so a
  remembered setting cannot make the map look broken.
  — **CORRECTION TO THE OBVIOUS MENTAL PICTURE:** the projection is
  `geoNaturalEarth1()`, NOT equirectangular, so meridians are CURVES, not
  vertical lines. Not extra work (d3-geo projects a meridian LineString exactly
  like the country outlines) but it changes label placement.
  — **Open for the operator, none to be invented:** labels at the top edge or
  along a latitude; every band or thinned on a narrow window; and whether the
  lines show at all zoom levels.
- ~~[2026-07-27] FT8 TX drive collapsed mid-session — rig keyed but made almost no
  power~~ **TRIAGED 2026-08-07 — detection SHIPPED, text kept as the reasoning
  trail.** The "read the rig's PO meter" fix this entry proposed was built as the
  drive alarm (2026-07-29/30) and extended by ADR 0064 continuous ALC/PO polling
  (2026-08-07). Of the two "either way" follow-ups: (a) `ft8.tx.amplitude` is
  tracked in backlog P2 "FT8 audio levels" (`txAmplitude` Pwr control); (b)
  arm-time sink-name+volume logging → backlog (triage 2026-08-07). Original:
  rig keyed but made almost no power, so the amp (needs ~10 W) never came up.
  ROOT CAUSE UNKNOWN. Everything
  upstream was verified healthy: CAT ok, PTT asserted (ft8-all.txt "Transmitting"
  lines prove KeyTx succeeded), mode DATA-U, correct frequency, correct default
  sink (the rig's PCM2903C codec), and a full QSO had completed on the same build
  20 minutes earlier. It was fixed by CYCLING the PipeWire sink volume
  (0.40 -> 1.00 -> 0.60 -> 0.35 -> 0.39); the operator did NOT touch the rig's USB
  LEVEL, and the working value (0.39) is essentially the 0.40 it was broken at — so
  the volume VALUE was never the fault. The plausible mechanism is that changing it
  forced PipeWire/WirePlumber to re-apply routing+gain to smd's playback stream,
  which had somehow got into a bad state. If it recurs: cycle the sink volume
  first, and capture `pactl list sink-inputs` DURING a transmit slot before
  changing anything — that is the one observation we never got, and it would show
  whether smd's stream exists and where it is routed.
  Follow-ups worth building either way: (a) an `ft8.tx.amplitude` config field so
  drive is SM's to set rather than a desktop mixer's, and (b) log the output sink's
  name AND volume at FT8 arm time, so this is one grep instead of an hour.
  **THIS RECURRED 2026-07-28 — see the stuck-TX entry below. Same rig, same setup,
  one day later; that time it was NOT cycled and it ended with the radio latched
  and hung. The 07-28 entry establishes the collapse is silently detectable from
  the decode log (answers stop while decodes continue) and proposes the real fix:
  read the rig's PO meter while keyed.**
- ~~[2026-07-28] SPA shows **"Cannot reach the daemon"** on a tab that was backgrounded
  while the screen blanked~~ **→ backlog P2 infra (triage 2026-08-07): fix ONCE at
  the SSE-client layer, only-when-dead (a healthy `/v1/ft8/events` stream must never
  be torn down — closing it starts the TX-disarm linger). The 2026-08-02 narrowing
  stands: stale-display fix, not TX-safety.** Original: operator had to log back in. The daemon was healthy
  throughout — `smd` active, no restart, answering HTTP 200, FT8 still transmitting
  and decoding, and a QSO (IK6DLK) logged after the unlock. So the browser suspended
  the backgrounded tab, its SSE `EventSource` dropped, and **it did not
  re-establish on refocus** — a reload was needed. Minor, but misleading at exactly
  the wrong moment: the operator came back to what looked like a dead daemon in the
  middle of an unattended TX run, on the same morning as a real stuck-TX incident.
  An SSE client that loses its connection while hidden should retry when the tab
  becomes visible again.
  — **RE-VERIFIED 2026-08-01: STILL OPEN, and the banner was a red herring.**
  "Cannot reach the daemon" is a FETCH failure string (`api/logbooks.ts:78`,
  `setup.ts:31`, `qso-patch.ts:55`) — it is not emitted by any SSE path, so what the
  operator saw was a request failing on refocus, with the dead stream as the
  separate symptom. All three SSE clients (`log-events.ts`, `ft8-sse.ts`,
  `rig-sse.ts`) still state the same contract — *"the browser owns reconnect on
  transient drops — no retry loop here"* — and **nothing anywhere recreates an
  EventSource on `visibilitychange`.**
  **The map already carries a partial answer, from an independent dogfood report
  (2026-07-18):** `mapData.svelte.ts:310` installs a `visibilitychange` handler whose
  comment names this exact cause — *"Hidden tabs get throttled timers and possibly a
  silently-dead stream, so a backgrounded map goes stale and NOTHING forces a
  catch-up"*. But it heals DATA by refetching; it does not revive the stream, so
  `mapData.live` stays false and every other surface has neither. **Two reports, two
  surfaces, one root cause** — worth fixing once at the SSE layer rather than a third
  time per-view. Open for the operator: on becoming visible, always recreate, or only
  when the stream is known dead (which needs a liveness signal the clients do not
  currently keep).
  — **NARROWED 2026-08-02, against `smd.log` rather than reasoning.** Two things
  changed the scope, and both SHRINK it:
  **(1) The screen-blank half is already closed.** Idle inhibition landed the same
  day as this report (`65dbcee5`, `internal/ft8/idleinhibit.go`): a logind
  `idle:sleep` block + `org.freedesktop.ScreenSaver` held for as long as FT8 TX is
  ARMED, proven by controlled A/B on KDE Plasma (44 min untouched with the lock
  held, vs a lock within 5 min once disarmed). So "screen blanks mid-run and the
  stream dies" should not recur. It does NOT cover TX-unarmed monitoring, Phone/CW,
  Logbook, or the map — and it does not stop a TAB being backgrounded, which
  browsers throttle/freeze regardless of whether the screen is on.
  **(2) A dropped stream has NEVER silently ended a run — checked, not assumed.**
  The suspicion was that a dead SSE → subCount 0 → `captureLinger` → `onLingerExpired`
  disarms TX and abandons the QSO. On 2026-07-28 the sequence `06:16:47 session
  abandoned / 06:17:08 session abandoned / 06:17:55 ft8 tx: disarmed` looked exactly
  like that. It was not: every one carries an HTTP request, i.e. a click —
  `POST /v1/ft8/qso/abandon` 06:16:47, `POST /v1/ft8/cq/start` 06:17:06, `POST
  /v1/ft8/qso/abandon` 06:17:08, `POST /v1/ft8/tx/arm` 06:17:55 (an arm re-runs the
  disarm path, which is why the "disarmed" line sits there). Timing disconfirms it
  too: last SSE disconnect 06:16:55 + 5 s linger = ~06:17:00, a full minute earlier.
  And both "disconnects" that day were RELOADS — `/app/operate/ft8` + `index.js` +
  `index.css` + `logo.svg` fetched in the same second, with the SSE reopening
  immediately.
  **Consequence: this is a STALE-MAP fix, not a TX-safety fix — rank it accordingly.**
  The live target is the 2026-07-18 background-tab report. The argument for
  only-when-dead over always-recreate still holds, but for TAB SWITCHING rather than
  screen blanking: closing `/v1/ft8/events` starts the 5 s linger, and a reopen that
  fails would disarm TX mid-run, so a healthy stream must never be torn down.
- ~~[2026-07-28] **STUCK TX — 4th incident, and the first where the RIG stopped
  answering CAT while still keyed.**~~ **TRIAGED 2026-08-07 — the build follow-ups
  are all dispatched; text kept as the reasoning trail.** Closed: (d) meter reads →
  drive alarm + ADR 0064 · (b) alarm-window probe logging → `txrecheck.go` · (g)
  idle inhibitor → built + A/B-proven. Moved: (a) mode-scoped TOT → appended to the
  backlog P3 TOT entry · (e)/(f) playback reopen-on-collapse → backlog P3
  (needs-trigger; the disarm/re-arm recovery is still UNVERIFIED — try it at the
  next occurrence). Operator-side (not code): (c) choke/ferrites · the 80 m power
  ladder · deliberate mic unplug/replug test. Original entry: 80m FT8 CQ run (3.573 MHz, +2750 Hz), operator
  in the shack 2 m away and NOT touching the rig; no rig settings changed since the
  2026-07-23 RTS fix. Ended when the operator switched the radio off at ~04:15:30.
  **Trace** (local time, `smd.log`): 50 clean CQ cycles, every one keying
  `tx-status 1` (TX by CAT) → `2` (tail) → `0`. Cycle 51 at **04:10:30** reported
  **`2` — "TX by other means" — at the moment of keying, never `1`, and never
  returned to `0`.** `TX0;` written 04:10:43 on schedule; alarm `tx_unconfirmed`
  04:10:46 (the 3 s confirm timeout); three following slots refused
  (`rig not ready to transmit`); **~04:11:50 the rig stopped answering CAT
  altogether** (derived: the FT8 capture gate needs 2 consecutive 5 s silent
  windows and dropped at 04:12:02); alarm re-probe loop exhausted 04:15:41. Only
  the power cycle cleared it. Across today 95 keyings read `1` and exactly one read
  `2`; since the RTS fix, `2`-while-idle otherwise appears only in the 2026-07-25
  06:00–07:30 block, which is the operator working phone by hand.
  **Operator observation at ~04:15: rig displayed TX, amp keyed, but NO POWER
  OUTPUT** — which is the single most useful fact and the reason to read this
  alongside the 2026-07-27 drive-collapse note above.
  **Eliminated:** RF ingress as the SUSTAINING cause (no drive after 04:10:43 =
  no RF, so nothing to get back into the CAT lead while it died); operator PTT
  (mic/MOX/footswitch — he was 2 m away); `RPTT SELECT` / settings drift (unchanged
  since 07-23, and it is DAKY); USB/CP2105 (kernel log completely clean through the
  window — this is NOT the 2026-07-18 shape); SM's audio path timing (the
  transmission ran its normal 12.6 s and the unkey went out on time); SM command
  set (`ft8.tx.mode` is unset, so SM sends this rig nothing but `TX1;`/`TX0;`, and
  both went out). **RF ingress is also OUT as the trigger** — see the finding
  below; no RF had left the shack for 24 minutes before the latch. (Recorded
  because it was the leading hypothesis for most of the investigation and was
  wrong twice: RF was ruled out as the SUSTAINING cause on the "no power"
  observation, readmitted as a possible TRIGGER, then ruled out entirely by the
  decode-log timing. The operator's own report of 80/40/30 m interfering with
  household equipment is real and is the common-mode signature — it just is not
  what happened here.)
  **THE ACTUAL FINDING — THE DRIVE HAD ALREADY COLLAPSED, 24 MINUTES EARLIER.**
  From `ft8-all.txt`, last station to answer us: **UR4LBG at 01:46:15 UTC**. In the
  window 01:46:30 → 02:10:30 we made **48 CQ transmissions and got 0 answers**,
  while **decoding 25 signals from 6 distinct stations** (DO5PY, MM0HVU, NF6P,
  RN3YN, ZS6NL) — so the band was open and RX was healthy the whole time. In the
  seven minutes BEFORE that we worked VE7SV (British Columbia, on 80m!), SP9UPH and
  UR4LBG back to back. **Confirmation from the recovery:** after the power cycle,
  DM2BPG answered within 2 minutes of resuming and KU1CW right after — at 250 W,
  LESS power than before. Propagation does not recover the instant a radio is
  power-cycled. So TX drive collapsed ~01:46:30 UTC, we called CQ into nothing for
  24 minutes, and the rig latched at 02:10:30 — the latch came AFTER the collapse,
  not before, and is probably a second-order consequence rather than the primary
  fault. The `ft8-all.txt` "Transmitting" lines prove only that KeyTx succeeded,
  never that audio flowed — same caveat as 07-27, and this is exactly how 24
  minutes of dead air looked normal in every log we have.
  **The cheap detector that already exists in the data:** answers stop while
  decodes continue. Worth knowing by hand; better as (d) below.
  **BAND CORRELATION — operator's observation, and the log supports it.** He
  reports trouble only on **80m and 30m**, never on the high bands. Sweeping every
  answer-less run of ≥15 consecutive TX slots across the whole decode log finds
  exactly ONE genuine collapse: today's, on 80m. The high bands are demonstrably
  clean over ~1,070 transmissions — 17m 478 TX/377 answered (the 07-27 soak), 15m
  413/339, 12m 181/166, all with no answer-less run at all. (The one other flagged
  run, 20m 07-27 04:53→07:41, is 31 slots spread over 2h48m of intermittent
  operation, not a continuous collapse.) The earlier 30m sighting is the 2026-07-21
  incident recorded in `session-handoff.md`, outside this log window. **This puts RF
  ingress back as the leading cause OF THE DRIVE COLLAPSE — which is NOT a
  contradiction of the elimination above, because that ruled RF out for the LATCH
  and the CAT death, both of which happened 24 min after any RF had stopped. RF was
  present at 01:46:30 when the drive collapsed.**
  **AND IT IS NOT A FREQUENCY CORRELATION AT ALL — IT IS AN ANTENNA ONE (operator,
  2026-07-28).** 20m–6m are on a **hex beam**; 80/40/30m are on the **DX Commander
  vertical**. Re-sorted that way the split is exact: every clean band above — 20m,
  17m, 15m, 12m — is the HEX BEAM, and both trouble bands — 80m, 30m — are the
  VERTICAL. That is a far stronger hypothesis than "low bands are worse", because it
  names a mechanism instead of a tendency: a ground-mounted multiband vertical works
  against its radial field and readily puts common-mode current on the feedline
  shield, which carries RF straight into the shack; an elevated, balanced hex beam
  normally does not. **Check against the earlier incidents:** 2026-07-21 was 30m =
  vertical ✓; 2026-07-28 is 80m = vertical ✓; 2026-07-23 was 20m = hex beam, which
  looks like a counter-example but is NOT evidence either way — that one has a known
  and fixed root cause (the RTS rigdef bug). **FALSIFIABLE PREDICTION worth running:
  40m is also on the DX Commander and has only ~7 logged transmissions, so it is
  effectively untested. If the antenna hypothesis is right, 40m should show the
  fault and 20m should never.** That single experiment separates antenna from
  frequency.
  **THE 40m EXPERIMENT WAS RUN THE SAME MORNING — AND THE PREDICTION DID NOT HOLD.**
  2026-07-28, T0 03:08:00 → 03:39 UTC, 7.074 MHz, DX Commander, **choke NOT yet
  fitted**, 250 W: **64 TX slots, 144 decodes, 4 QSOs** — SP9UPH (Poland) 03:09,
  W3GQ (USA) 03:13, F4JTM (France) 03:21, IK6DLK (Italy) 03:28. Longest answer-less
  run **22 slots / 11 min**, against 48 slots / 24 min for the morning's confirmed
  80m collapse. Ended by band noise, not by any fault. **So a DX Commander band ran
  ~31 min clean — longer than the 80m session survived before it broke (~25 min) —
  and the simple "it is the vertical" story is WEAKENED.**
  **POWER IS THE VARIABLE THAT ACTUALLY TRACKS THE FAULT (operator supplied the
  figure 2026-07-28: the failing 80m run was at 350 W).** Tabulated, every run this
  morning on the same unchoked DX Commander:
  | band | power | duration | result |
  |------|-------|----------|--------|
  | 80m  | **350 W** | ~25 min | **COLLAPSED**, then the rig latched |
  | 80m  | 250 W | ~30 min, 4 QSOs | clean |
  | 40m  | 250 W | ~31 min, 4 QSOs | clean |
  | 30m  | 300 W | in progress | — |
  **The 80m 350 W vs 80m 250 W pair is a SAME-BAND, SAME-ANTENNA, SAME-MORNING
  comparison in which only the power differed — and only the high-power run failed.**
  That is much stronger than any of the cross-band comparisons, and it is a
  threshold signature: consistent with common-mode ingress that only becomes
  destructive above some level between 250 W and 350 W. **It also revives the
  antenna hypothesis in a REFINED form** — not "the vertical always fails" (40m and
  80m both ran clean at 250 W) but "the vertical couples enough common-mode current
  back into the shack to break the USB audio ABOVE a power threshold". The choke
  remains the right fix; it just is not needed at 250 W.
  **CORRECTION to an earlier draft of this entry**, which called "80m at 250 W" the
  clean next test that neither session had made: it HAD been made, immediately after
  the power cycle the same morning — ~30 min, 4 QSOs, clean. That run is the control,
  and it is what makes the power comparison work.
  **The remaining useful experiment is therefore a POWER LADDER on 80m** (250 → 300
  → 350 W, ~30 min each), run BEFORE any RF changes, to find where it breaks.
  Changing the RF path first destroys the baseline. Note 40m/80m also do not couple
  common-mode alike even on one antenna, so keep the ladder on ONE band.
  **STATION RF LAYOUT (operator, 2026-07-28) — there is ALREADY a common-mode
  choke, which reframes the fix:** `rig → coax switch → amp → [Palomar common-mode
  choke] → Rat Pak remote switch → DX Commander or hex beam`. So the main line out
  is already choked, and RF is still getting in. Three readings, in order of how
  cheap they are to test:
  (i) ~~The Rat Pak control cable bypasses the choke.~~ **WRONG — operator
  confirms NOTHING bypasses it: a single high-quality coax leaves the shack, and the
  choke sits before it exits.** So the conducted path out of the shack is properly
  choked, and it was choked during every incident. Recorded because it eliminates
  the obvious answer and forces the next one.
  (i-revised) **THE INGRESS IS PROBABLY NOT CONDUCTED AT ALL — IT IS DIRECT FIELD
  COUPLING.** The operator independently reports that 80/40/30m "produce
  interference with household equipment and vice-versa". **Household equipment is
  not connected to his coax**, so that interference cannot be travelling by the
  choked path — it is the antenna's near field reaching into the house, plus RF on
  the mains. Once that is granted, the shack's own USB leads, PC and rig are sitting
  in the same field, and the coax choke is irrelevant to them. This explains why a
  correctly-fitted choke did not prevent any of this, and it re-orders the fixes:
  **work at the VICTIM end, not the source.** Ferrite the USB leads (rig end AND PC
  end), keep them short, route them away from the antenna side of the room, and
  consider a ferrite/filter on the mains feeds to the PC and rig — the mains is the
  other conductor running through the whole house that the household-equipment
  symptom implicates. Antenna-to-shack distance is the underlying variable and the
  expensive one.
  (ii) **A broadband choke is usually WEAKEST at 3.5 MHz.** If the Palomar model's
  common-mode impedance falls off at the low end, the observed pattern falls straight
  out: at 80m the choke is least effective AND the vertical produces the most
  common-mode current AND 350 W makes the absolute current highest — three factors
  compounding on exactly the one band that failed, while 20–6m on the hex beam
  (better-behaved antenna, choke in its strong region) stays clean. **Check the
  model's published impedance at 3.5 MHz**; it may simply be out of its depth there
  rather than faulty.
  (iii) **Ferrite the USB leads at the rig regardless.** (i) and (ii) attack the
  source; this protects the actual victim, and the victim is known — the PCM2903C
  audio stream, not the CAT link.
  **Cheap diagnostic while running the power ladder: feel the choke.** A
  common-mode choke that is being overwhelmed gets warm; a cold one at 350 W on 80m
  argues the current is arriving by another path — i.e. (i).
  **THE MECHANISM THIS SUGGESTS, AND THE GAP IT EXPOSES:** common-mode RF disturbs
  the **PCM2903C USB audio codec's stream** — a DIFFERENT USB device from the CP2105
  serial bridge, which is why CAT sails on untouched while audio dies. **We have
  already been bitten by exactly this shape in the other direction and fixed only
  that side:** 2026-07-18, the CAPTURE stream went dead and the daemon decoded pure
  silence with zero errors logged → `internal/ft8/deadsource.go` was built the same
  day (starved/silent strike detection → release + reacquire). **There is NO
  playback-side equivalent** — confirmed, nothing in `internal/audio/playback/` or
  `txcontroller.go` monitors stream health — so when the PLAYBACK stream died on
  07-27 and again on 07-28 the daemon transmitted silence with zero errors, twice.
  Same failure shape, opposite direction, mitigation applied to one side only.
  **Caveat that matters for the fix choice:** a playback watchdog may be BLIND to
  this. On 07-27 the stream appeared to be running and was routed nowhere useful —
  if malgo keeps pulling frames, a callback-counting watchdog sees a healthy stream.
  The meter read in (d) measures at the RIG, past every software layer, so it
  catches the fault wherever in the chain it happened. Build (d) first.
  **THE PLAYBACK DEVICE LIFETIME IS THE LIKELY MECHANISM — AND IT SUGGESTS A MUCH
  CHEAPER FIX (2026-07-28).** `txplayer_cgo.go`: "The Service Init's it on **arm**
  and Close's it on **disarm**; the TxController drives Play/Stop per slot." So ONE
  playback device handle is held open for the WHOLE armed session — potentially an
  hour — and every slot's `Play()` writes into it. **If that single handle goes bad
  (RF glitch, a WirePlumber re-route, a device suspend), it stays bad for the entire
  session and every subsequent transmission is silent.** That fits every observation
  better than a per-stream fault does: 48 consecutive silent transmissions from ONE
  bad handle rather than 48 independently-bad streams; cycling the sink volume
  fixing it (forces PipeWire to re-apply routing to the existing stream); zero
  PipeWire errors (the stream object still exists, it is just going nowhere — and
  PipeWire/WirePlumber DO log to the user journal, they logged startup fine at
  03:20:17, so the silence during the collapse is meaningful rather than absent
  logging).
  **IMMEDIATE OPERATOR RECOVERY, easier than cycling PipeWire volumes: DISARM and
  RE-ARM TX.** That closes and re-opens the device, so it should clear the collapse
  from inside SM with no desktop fiddling. **UNVERIFIED — try it at the next
  occurrence before reaching for `pactl`**, and if it works that also CONFIRMS this
  mechanism, because nothing else in the disarm/arm path touches the audio route.
  **OPERATOR HYPOTHESIS, 2026-07-28, AND IT MAY DISPLACE THE WHOLE RF THREAD: was
  the PC idle/screen-blanked when the drive collapsed?** He was **lying on his bed
  2 m away, not at the machine**, for the entire 80m run. Checked and PARTLY
  supported:
  - **USB autosuspend is RULED OUT.** All three relevant devices read
    `power/control=on` with `runtime_status=active` (USB AUDIO CODEC 7-1.2, CP2105
    7-1.1, USB Audio 1-5). No USB-level power management is touching them.
  - **But something power-related DID happen 7 minutes before the collapse:**
    `03:39:43 Lockdown: systemd-logind: hibernation is restricted` ×5 with
    "5 callbacks suppressed" — logind evaluating hibernation capability. Collapse
    was 03:46:30. Suggestive, not conclusive: that message can also come from a
    desktop applet polling `CanHibernate`. No lock/screensaver/DPMS event is
    journalled (the compositor handles blanking, not logind), so absence of a lock
    record is NOT evidence there was no blank.
  - **The session-event mechanism has PRECEDENT IN THIS STATION:** the 2026-07-18
    capture-side incident was *"KDE Plasma device fiddling destroyed+recreated the
    rig codec's PipeWire nodes mid-capture"*. A session/compositor event causing
    WirePlumber to re-evaluate routing would leave SM's held-open playback device
    (see the lifetime note above) pointing at a stale node — no USB error, no
    PipeWire error, silence until something forces a re-route. That is every
    observation, with no RF required.
  - **AND IT RE-EXPLAINS THE "BAND" CORRELATION AS A TIME-OF-DAY ONE.** The clean
    high-band sessions were DAYTIME with the operator working at the PC (07-27 17m
    soak 11:41–16:41 local; 07-26 15m/12m 11:18–16:17 local). The 80m failure was
    **03:23–04:59 local with the operator on his bed**. Screen blanking needs an
    idle machine, and only the low-band sessions had one. **The 80m/30m pattern may
    be nothing to do with the vertical at all** — it may simply be when he operates
    unattended. This ALSO fits the 07-28 SPA "cannot reach the daemon" entry above,
    which is a confirmed screen-blank casualty on the same machine the same morning.
  **THE TEST THAT SEPARATES THIS FROM RF, and it is cheaper than the power ladder:
  run 80m at 350 W while ACTIVELY USING the PC (or with blanking/idle disabled).**
  Clean ⇒ idle/session event, not RF, and the choke work is unnecessary. Collapses
  ⇒ RF is back and the power ladder is the next step. Run this BEFORE the ladder.
  **(g) SM holds an IDLE INHIBITOR while TX is armed — BUILT 2026-07-28.**
  ("Don't idle/blank/suspend while the station is transmitting.") Correct
  regardless of which hypothesis wins, since an unattended FT8 run is exactly when
  the machine looks idle to the desktop and exactly when nobody is watching.
  **TWO surfaces, because neither is universal:** logind
  (`org.freedesktop.login1`, SYSTEM bus, `idle:sleep` in mode `block`) is on every
  systemd distro and on the non-systemd ones shipping elogind, and works headless;
  `org.freedesktop.ScreenSaver` (SESSION bus) is provided by KDE/GNOME/XFCE/MATE/
  Cinnamon but NOT by bare wlroots compositors or without a session. **logind alone
  does not reliably stop a desktop BLANKING** — on KDE that is kscreenlocker's
  business and answers to the ScreenSaver interface — so holding only logind would
  likely have missed the very event suspected here. Partial success is success;
  an error means the host granted nothing, and then the daemon logs once and
  transmits anyway (inhibition is a courtesy, TX is not).
  New package `internal/inhibit` (+`github.com/godbus/dbus/v5`, BSD-2, GPL-
  compatible, pure Go, no transitive deps); injected into `internal/ft8` behind an
  `IdleInhibitor` interface exactly as `TxKeyer` is, so the FT8 package takes no
  D-Bus dependency and stays testable without a bus. Config `ft8.tx.inhibit_idle`,
  nil→true, explicit false honoured — as a RESOLVER (`types.ResolveFt8InhibitIdle`),
  not an `applyDefaults` entry, because `ActiveFt8()` leaves the whole TX block nil
  when there is no `ft8.tx`, no `ft8.tx.mode` and no rig TX-audio device, and a
  default cannot be written into a block that does not exist. **That was a real
  defect caught before commit**: the first cut defaulted inside `applyDefaults` and
  read the field off the block, so a minimal config resolved to OFF while the docs
  said ON — silently, discoverable only by the machine blanking mid-transmission.
  13 tests across three packages, all written BEFORE their implementations, and
  five reversions proven: drop the acquire → "want 1 held, got 0"; drop the release
  → "want 0 held, got 1"; drop `sync.Once` → 3 underlying releases; release only the
  first surface → the second leaks; restore the nil-block defect → "want true".
  The two rules worth knowing, because they pin states this change CREATES: a
  REFUSED arm must hold nothing (arms are refused routinely on a CAT blink, and a
  leaked inhibition never releases — the desktop would stop idling for the life of
  the process, a worse fault than the one being mitigated), and arming twice must
  hold exactly ONE (`ArmTx` is idempotent and the SPA re-sends it).
  **NOT YET VALIDATED ON AIR.** It mitigates a cause that is still unproven — the
  confirming experiment (80m at 350 W with the operator actively at the PC) has not
  been run, and (g) will now MASK that experiment on the machine it is deployed to.
  Run the experiment on a build without it, or with `inhibit_idle: false`.
  **The fix this implies (f): re-open the playback device on a detected collapse** —
  the exact analogue of what `deadsource.go` already does on the capture side
  (strike → release → reacquire). Note this makes (e) and (f) the same work, and it
  raises the value of (d) further: the meter read is what would DETECT the condition
  that (f) then recovers from.
  **TOT was armed and would have worked:** set to 8 min (kept long because a short
  TOT cuts out phone), so it was due to fire 04:18:30 — the operator stopped it
  ~3 min short. Not a gap; but 8 min is pure exposure for a mode whose transmissions
  are 12.6 s.
  **OPERATOR CHANGES ON RESUME (04:37 local):** (1) power reduced to **250 W**;
  (2) **the microphone was UNPLUGGED — and PLUGGED BACK IN at ~04:52**, so the
  mic-out window was only ~15 min and covered 3 QSOs (DM2BPG 04:40, KU1CW 04:42,
  DL1STG 04:50) with no fault. **That proves nothing**: the previous run went ~25
  min before the drive collapsed and ~49 min before the latch, so a clean 15 min is
  well inside the noise. From ~04:52 the only variable still changed is the power.
  (2) is a real candidate for the LATCH
  specifically: a stuck or intermittent mic PTT keys the rig "by other means"
  (`tx-status 2`, exactly what was observed), no CAT `TX0;` can release it, and in
  DATA-U the mic audio is not routed — which would key the rig with NO power out,
  matching the operator's observation precisely. It does NOT explain the CAT link
  going silent at 02:11:50, and it does NOT explain the drive collapse at 01:46:30,
  which is the primary fault and came first. If the latch never recurs with the mic
  out, unplug/replug it deliberately to confirm rather than leaving it as a
  coincidence.
  **Follow-ups, HIGHEST VALUE FIRST:** (d) **read the rig's ALC *and* PO meters
  while keyed** — Yaesu exposes meter reads (`RM`; `RM5` ALC, `RM6` PO), and the
  FTdx10 rigdef has no meter command among its fourteen. Keyed with the meters at
  zero is a DIRECT, local measurement of "I am not radiating" — the fault that
  actually happened, twice in two days, and which every existing log made look
  normal. **Read BOTH, because together they LOCALISE the fault rather than merely
  detecting it:** ALC≈0 + PO≈0 ⇒ no audio is reaching the rig (the PipeWire/drive
  fault — our case); ALC normal + PO≈0 ⇒ audio is fine and the rig is not making RF
  (PA, ATU, antenna or rig fault); ALC≈0 + PO normal should be impossible and would
  indicate the reads themselves are wrong. The operator already does this by eye —
  watching ALC during a slot is exactly the manual version — so the check is
  known-good, it just needs to be something the daemon does every slot instead of
  something a human has to remember to look at. READ-only: no TX intent, no safety
  risk, no network. This supersedes (a) in priority: (a) shortens the damage after a
  hang, (d) catches the fault itself. Needs checking against the FTdx10 CAT
  reference, same as (a).
  (a) **mode-scoped TOT** — if the FTdx10 exposes the TOT menu item
  over CAT (Yaesu `EX` commands reach the menu on that generation; the rigdef has NO
  menu-command support today), SM could clamp it to the rig's **1 min minimum** for
  the life of an FT8 session and restore the operator's 8 min on exit. Same
  snapshot/clamp/restore shape as the ADR 0027 tune restore, so NOT a new safety
  mechanism — and it addresses the one failure class no CAT logic can, a radio that
  has stopped listening. 1 min is still ~5× the longest legitimate FT8 key-down
  (12.6 s), so the floor costs nothing here; against TODAY's incident it would have
  ended the carrier at ~04:11:30 instead of ~04:15:30, i.e. 4 minutes earlier and
  before the operator had to reach the radio. Needs checking against the FTdx10 CAT
  reference first. (b) **The alarm
  window logs almost nothing** — 04:10:46→04:15:41 produced ONE line, because
  `observeTxStatus` logs only on TRANSITION and a stuck state never transitions. So
  the log cannot distinguish "rig keeps answering still-transmitting" from "rig
  stopped answering" — the single most important fact in a stuck-TX incident, and
  the one that had to be inferred here from the FT8 capture gate's timing. Log the
  probe answer (or its absence) while an alarm stands. (c) **CHOKE THE COMMON-MODE
  PATH — PROMOTED from "cheap, do it anyway" to the leading candidate FIX, on the
  ANTENNA correlation above.** Two places, and the first matters more: a choke/current
  balun at the **DX Commander's feedpoint** stops RF getting onto the feedline shield
  and into the shack at all, and a clamp-on ferrite on the **USB leads at the rig end**
  protects the specific victim. The hex beam evidently does not need either, which is
  itself the argument for where to spend the effort. (d) detects the fault; (c) is the
  only item that attacks its cause. Cheap enough to just do and see. (e) A
  **playback-side dead-stream watchdog** mirroring `deadsource.go` — lower priority
  than it looks, per the blindness caveat above, but the asymmetry is real and worth
  closing once (d) can tell us whether a given collapse was visible from inside the
  audio layer at all.
  NOT worth building: escalating a persistent `2` to a re-unkey burst (as `1`
  already does) — it would have written into a radio that had stopped listening.
  **THE IDLE/SESSION TEST WAS RUN — 2026-07-28 midday, DUMMY LOAD. NO
  REPRODUCTION, and it forces two corrections above.** 5 W into a dummy load on
  **3.573 MHz — the incident's own band** — CQ run, blank/lock at 2 min, then
  KDE power management returned to defaults so the session also SUSPENDS.
  Instrumented at 2 s: the rig sink's PipeWire state, SM's playback and capture
  node presence, logind `IdleHint`/`LockedHint`, smd's PID; plus `pw-mon`,
  `smd.log`, `ft8-all.txt`. **Result: 107 of 107 transmissions had drive**, over
  ~55 min, two lock cycles and one full suspend/resume. The reconciliation is
  exact — 107 `Transmitting` lines, 12 of them before the recorder started, 95
  remaining, against 95 observed `txplay: ABSENT→running` bursts.
  **CORRECTION 1 — the `03:39:43 hibernation is restricted` ×5 line is NOT
  suggestive of anything.** It fired again today at 12:15:42, four seconds
  before an ordinary screen lock. It is the routine signature of a session
  locking. **But it is a good TIMESTAMP, and that is new: it dates the lock on
  the night of the incident, putting the drive collapse 6m47s after the screen
  locked** (03:39:43 → 03:46:30).
  **CORRECTION 2 — "idle alone is sufficient" is WEAKENED.** The test ran ~3×
  that 6m47s interval, locked, with zero failures. It does NOT clear idle as a
  co-factor and does NOT implicate RF; the variables still separating the test
  from the incident are power (5 W vs 350 W) and load (dummy vs DX Commander),
  i.e. precisely the RF ones. So the confirming experiment in the "NOT YET
  VALIDATED ON AIR" note above is only PARTLY discharged — the low-power,
  no-antenna half is done and null.
  **MECHANISM REFINEMENT, and it cuts against the "one bad handle" reading
  above.** The rig's PipeWire sink drops to `suspended` ~6 s after every
  transmission and must RESUME for the next one — ~120 resumes/hour on a CQ run.
  And SM's playback NODE does not exist at arm at all: PipeWire creates it per
  transmission and removes it after (verified — armed and idle for 22 s showed
  `txplay=ABSENT`, `sink=suspended` throughout). So whatever malgo holds open
  from arm to disarm, PipeWire tears the node down and rebuilds it every slot.
  **A stale-node theory must therefore explain why a FRESH node each slot still
  goes nowhere** — which points at the persistent device handle and its routing
  target rather than at the node. 107 consecutive clean resumes says the path is
  not fragile; the per-slot resume is nonetheless the operation to instrument.
  **SAFETY FINDING — SUSPEND CAUGHT A KEYED TRANSMISSION.** 13:02:30 transmission
  starts, `tx-status 1`; **13:02:34 `PM: suspend entry (deep)`, `user.slice`
  frozen** — the machine slept four seconds into a keyed transmission. SM was
  frozen for 134 s and could send nothing; its own 18 s `ft8TxMaxDuration`
  auto-off froze with it and fired **127 s late** on thaw (13:04:55, `ft8 tx
  auto-off fired; PTT dropped`). **Nothing on the computer could unkey the rig —
  only the rig's TOT could.** That is ADR 0057's premise meeting real hardware
  for the first time, but it is NOT proof the premise held: there is no rig-side
  observation across the freeze. The rig reported `0` within 7 s of resume, so it
  was unkeyed by then; TOT (at 1 min for this test) is the likely agent,
  unproven. **Cheap test that WOULD prove it: watch the PO meter through a
  deliberate suspend.** If it drops at 60 s, the premise is confirmed on
  hardware. At 5 W into a dummy load this was harmless; on the DX Commander at
  350 W it is a minute of unattended full-power carrier.
  **A RESUME USB ERROR WAS LOGGED — BUT NOT ON THE RIG'S CONTROLLER.** Resume
  logged `xhci_hcd 0000:0d:00.0: xHC error in resume, USBSTS 0x401, Reinit`,
  `root hub lost power or was reset` on usb1 and usb2, and a reset of every
  device on those two buses. **The rig is on NEITHER:** CP2105 `7-1.1` and USB
  AUDIO CODEC `7-1.2` are on bus 7 behind controller `0000:73:00.4`, and bus 7
  logged no reset at all — the rig's audio and CAT resumed cleanly.
  **CORRECTION to the first draft of this block, which claimed they shared the
  erroring controller:** that came from taking the first PCI address in the
  device path, which is the shared bridge `0000:00:08.1`, not the controller.
  The `7-1.2`/`7-1.1` numbering recorded earlier in this same entry would have
  caught it. So "a resume that half-recovers — CAT back, audio not" stays a
  THEORETICAL route to the incident's signature: no evidence it happened here,
  and no suspend occurred during the incident regardless. What is solid and still
  worth knowing: this machine has an xHCI controller that errors on every resume,
  which is an independent reason not to let it sleep.
  **The FT8 scheduler did the right thing under a fault it had never met:**
  `timer delay exceeded the lateness budget; skipping slot`, `late_ms=127172`. It
  refused a 127 s-late slot instead of transmitting into it.
  **(g) HAD A VALIDATION GAP, found while setting this up — CLOSED the same day;
  see the controlled A/B below.** The bench check —
  arm TX, confirm `systemd-inhibit --list` shows the lock — proves the lock is
  TAKEN, not that anything HONOURS it. On this host logind's `IdleAction` is
  `ignore`, so **the `idle` half of (g)'s lock is inert**: there is no logind idle
  action to inhibit. The suspend is driven by **PowerDevil**, so the work must be
  done by the `sleep` half (making PowerDevil's call into logind fail) and by the
  ScreenSaver lock. **The real proof is re-running exactly this test with (g)
  deployed and seeing whether the machine still suspends.** Also worth knowing
  for any future analysis: **logind's `IdleHint` is NOT tracked under KDE
  Wayland** — it read `no` throughout while `LockedHint` read `yes`. An analysis
  keyed on `IdleHint` would come out flat and read as "the machine never went
  idle".
  **GAP CLOSED — (g) WORKS ON THIS DESKTOP, PROVEN BY CONTROLLED A/B (2026-07-28,
  15:40).** Deployed, armed TX with NO CQ run (arming holds the inhibitor and
  transmits nothing — `armTx` opens the device and builds the controller, only
  `startTransmission` keys), then left untouched. The lock was visible and
  correct: `Station Manager | smd | sleep:idle | "Station Manager: FT8 transmit
  is armed" | block` — and the ONLY block-mode sleep lock on the box, since
  PowerDevil's `block` covers key handling alone and every other sleep lock is
  `delay`. Both surfaces granted: logind directly visible in the list; the
  ScreenSaver one inferred from the ABSENCE of `inhibit: some idle-inhibition
  surfaces unavailable`, which `internal/inhibit` emits whenever it gets fewer
  locks than surfaces.
  **The A/B — same machine, same session, same KDE defaults (`powerdevilrc` and
  `kscreenlockerrc` both reset to empty), one variable:**
  | inhibitor | window | result |
  |-----------|--------|--------|
  | **HELD** | 14:44:54 → 15:29:12, 44 min untouched | **NO LOCK** |
  | **RELEASED** (TX disarmed 15:29:12) | ~5 min after last input | **locked 15:36:33** |
  **PROVEN DIRECTLY: PowerDevil's idle timers are suppressed** — i.e. the
  ScreenSaver surface IS honoured, which was the actual open question, and the
  bench check could never have answered it.
  **STRONGLY SUPPORTED BUT NOT DIRECTLY CONTROLLED: suspend prevention.** The
  control ran only 11 min and suspend needs ~18, so no suspend was ever available
  for it to block. The inference rests on three things: the same PowerDevil idle
  mechanism the lock A/B just proved suppressed; an INDEPENDENT logind `sleep`
  block lock; and this machine demonstrably suspending at ~18 min idle (13:02:34)
  yet sitting 44 min inhibited without one. **To close it properly: disarm and
  leave the machine 25+ min.** Recorded as still open rather than claimed.
  **ALSO CONFIRMED IN THE FIELD: the inhibition RELEASES on disarm** — no
  `Station Manager` row after 15:29:12. That is one of the two rules called out
  above as pinning a state this change CREATES, and the more dangerous one: a
  leaked inhibition never releases, so the desktop would stop idling for the life
  of the process — worse than the fault being mitigated.
  **JUSTIFICATION HAS CHANGED — say so in the commit message.** (g) was built for
  the idle/drive-collapse hypothesis, and that hypothesis was WEAKENED the same
  day (see the dummy-load results above). What justifies it now is the MEASURED
  hazard at 13:02:34: a suspend landing four seconds into a keyed transmission,
  SM frozen and unable to unkey, with only the rig's TOT able to end it.
  **Do not read (g) as a drive-collapse fix.**
  **Set `inhibit_idle: false` for the dawn 350 W test**, or (g) masks the idle
  variable in the one experiment that still matters.
  **MANUAL UPDATED** (2026-07-28, operator-directed): new section *"Before you
  transmit: stop the computer sleeping"* in `manual/content/chapters/ft8.md`,
  next to the TOT section it depends on, stating that the results are
  unpredictable at best and can damage equipment at worst; plus a cause paragraph
  in the troubleshooting *"The rig transmits and won't stop"* entry.
- ~~[2026-07-28] **THE RIG INTERMITTENTLY IGNORES THE FIRST `TX0;` — 6 occurrences,
  and it is NOT RF, NOT power, and NOT the antenna.**~~ **→ backlog P3 (triage
  2026-08-07, parked on thin data): the recovery machinery works in the field
  (attempt 1, ~1 s, every time); open halves are the common-factor sweep (4 usable
  samples) and the persistent-`2` escalation re-look (needs a duration threshold —
  operator's call). Text kept as the reasoning trail.** Separate finding from the
  stuck-TX entry above, and deliberately NOT filed as part of it (see "two
  readings" below). Signature: after a normal unkey the FTdx10 answers **`1`** —
  CAT TX still keyed — to the confirmation query. SM's `case "1"` path raises
  `tx_still_keyed`, refuses the FT8 mode restore, and `retryUnkeyStillKeyed()`
  re-sends `tx_off`; the rig then confirms idle and the alarm auto-clears. Every
  occurrence so far has recovered **on attempt 1, within about one second**, and
  no run has been interrupted.
  **Every occurrence in `smd.log`:**
  | date | local | band | context | outcome |
  |------|-------|------|---------|---------|
  | 07-21 | 04:53:43 | — | — | (pre-dates `ft8-all.txt`) |
  | 07-23 | 06:36:29 | — | — | cleared |
  | 07-26 | 07:00:43 | 20m | `ZL1JRD 7Q5MLV R-17` | attempt 1 → cleared |
  | 07-26 | 13:07:13 | 15m | `BA4SEX 7Q5MLV RR73` | attempt 1 → cleared |
  | 07-28 | 12:25:43 | 80m | CQ, **5 W into a DUMMY LOAD** | attempt 1 → cleared |
  | 07-28 | 14:11:43 | 80m | CQ, **350 W into the DX Commander** | attempt 1 → cleared |
  (07-21 and 07-23 have no matching TX line only because `ft8-all.txt` starts
  20260726_043930 — that is NOT evidence they were outside an FT8 transmission.)
  **THE 07-28 PAIR IS THE DISCRIMINATING EVIDENCE.** Same rig, same day, ~1h45m
  apart: one at **5 W into a dummy load** (no antenna, essentially no RF in the
  shack) and one at **350 W into the DX Commander on 80m**. An order of magnitude
  in power, completely different loads. **That rules out RF ingress and power as
  the cause of THIS fault.** The band spread rules out the antenna correlation
  too: 20m and 15m are the HEX BEAM, 80m is the VERTICAL — it happens on both.
  **RATE — and there is NO trend, despite two today.** 4 events across the 1,707
  transmissions `ft8-all.txt` covers (07-26: 634, 07-27: 578, 07-28: 495) ≈ **1 in
  430**. 07-27 had 578 transmissions and **zero** events, which at that rate has
  ~26% probability — entirely unremarkable. So the data are consistent with a
  constant low rate, and "two today" is not an escalation. **Normalise per
  transmission before ever claiming this is getting worse.** Also not something we
  introduced: 07-21 and 07-23 pre-date all of this week's work.
  **WHAT IS CONFIRMED: the `case "1"` path works in the field.** Alarm raised →
  mode restore correctly refused → `tx_off` re-sent → rig confirmed idle → alarm
  auto-cleared → run continued. Four clean observations, the first ever outside a
  live incident. That machinery needs no changes.
  **TWO READINGS OF ITS RELATION TO THE STUCK-TX INCIDENTS, AND THE CONSERVATIVE
  ONE SAYS THEY ARE DIFFERENT FAULTS.** (A) *Same fault*: the rig's CAT handling
  occasionally drops or defers a write, and which code comes back is a matter of
  timing. (B) *Different faults*: `1` means **the unkey did not take** (command
  handling), `2` means **something non-CAT is keying the rig** (control line or
  hardware). Those are different rig states and conflating them would be an error.
  The 07-28 04:10:30 incident's `2` arrived with a latch AND the CAT link dying —
  a far larger event than a one-second blip. **Nothing currently separates A from
  B, so do NOT treat this finding as an explanation of the stuck-TX incidents.**
  **WHAT IT DOES BEAR ON — the recorded "NOT worth building" verdict deserves a
  re-look.** That verdict (escalating a persistent `2` to a re-unkey burst) was
  reached for a rig that had **stopped listening**. But in the 04:10:30 incident
  the rig answered `2` for roughly 80 seconds BEFORE CAT died, so there WAS a
  window in which it was still listening and an escalation could have written to
  it. Worth revisiting — with the constraint that any escalation needs a
  **duration threshold**, because `2` is also the normal ~1 s TX→RX tail and
  escalating on it immediately would fire on every clean transmission (see the
  `case "2"` comment in `txconfirm.go` and ADR 0057).
  **Next, if this is picked up:** sweep the occurrences for a common factor. Four
  usable samples is thin, and message type is already not it — the 07-26 pair were
  mid-QSO rungs (`R-17`, `RR73`), the 07-28 pair were both CQ.

---

## [2026-07-29] THE FT8 DRIVE COLLAPSE NOW HAS A MEASURABLE SIGNATURE — and it is the ABSENCE of meter data, not a low reading

**Status: MEASURED, on hardware, with a controlled drive sweep and a recovery
control. This is the first time the collapse has been made observable.**

**TRIAGED 2026-08-07 — follow-ups dispatched:** 1 (alarm on silence) BUILT →
`drivealarm.go` · 2 (`last` onset treatment) still open — carried to backlog P3
with the FT-710 item · 3 (finer sweep) operator/optional · 4 (push-rate
tolerance) honoured by every consumer built since · 5 CORRECTED by the 07-30
entry (CQ runs are unbounded) · 6 (FT-710 rigdef `RM`/`MS`/`METERPOLL`
unverified) still true → backlog P3. Text kept as the reasoning trail.

### What was built

`internal/bridge/meters.go` — the rig's own PO meter is now recorded per FT8
transmission and summarised in one log line at unkey
(`bridge: ft8 tx meters (raw 0-255 scale)`), carrying `max`, `min`, `last` and
`n` (the sample count). Nothing is written to the rig: the readings arrive as
unsolicited AUTO-mode pushes, so no CAT traffic is added to the key-down path.

### WHAT I GOT WRONG — the manual documented all of it, and so did our rigdef

The CAT reference (2308-F) states nothing false here, and every fact needed to
get this right was available from day one. Recorded this way deliberately: the
first draft of this section was headed "the three things the manual got wrong",
which was false and taught the wrong lesson — that documentation is unreliable
and the hardware must be probed. Probing costs keyed transmissions on a licensed
station, and two were spent on my wrong prefix.

1. **The rig pushes `RM` because SM tells it to.** `internal/cat/rigs/yaesu-ftdx10.json`
   sets `INIT` to `AI1;` on every connect; the reference's AI page reads
   *"0: Auto Information 'OFF' / 1: Auto Information 'ON'"* and *"This parameter
   is set to '0' (OFF) automatically when the transceiver is turned 'OFF'."* So
   the rig's own default is OFF and the stream exists only because the bridge
   arms it. I never opened that page — I read `AI=O` in the command index as if
   it were the mechanism. **One grep of our own rigdef settles it**, and was
   available in all four rounds.
2. **The manual documents the pushed frame.** The `RM` page carries a **`P1=0`**
   form with *"P2: Meter 0 - 255"* and *"P3: 000 (Fixed)"* — distinct from the
   Read form, whose selector list runs `1: S 3: COMP 4: ALC 5: PO 6: SWR
   7: IDD 8: VDD` and contains **no `0`**. That is `RM0nnn000`. The first
   implementation modelled `RM4`/`RM5`/`RM6` on my unstated assumption that the
   rig pushes the meter you asked for; those answer explicit queries only, and
   two real on-air transmissions reported "no meter data" while the rig pushed
   at ~26 Hz throughout both. Worse than not reading the page: **I had read it**
   — that is where the `RM4`=ALC / `RM5`=PO correction came from — and took only
   the legend I went looking for. The `P1=0` legend sits directly above it.
   (Caveat: the PDF's two-column layout converts badly, so which row the `P1=0`
   legend binds to is read from converted text, not the page image.)
3. **The pushed frame does not say WHICH meter it is** — not an error of mine, a
   design consequence, and `MS METER SW` covers it
   (`0:PO 1:COMP 2:ALC 3:VDD 4:ID 5:SWR`), so `MS;` was added to the rigdef's
   READ burst. Observed: the meter reads **S-meter during receive** and the
   **MS-selected meter while transmitting** — receive values sat at 103-132 and
   matched an explicit `RM1;` query (124); under mic modulation they dropped to
   a 0-33 band tracking the speech envelope.

The transferable lesson is narrower than "check the wire", and it is not about
the manual at all: **read the whole page, not the line you went looking for, and
grep our own tree before theorising about the rig's behaviour.** Both of the
above were answerable without transmitting.

### THE CONTROLLED TEST (dummy load, 5 W, 80 m, 24 transmissions)

Drive was swept by muting/attenuating the PipeWire sink feeding the rig
(sink 66, `PCM2903C Audio CODEC`), three transmissions per level.

| sink vol | dB     | max | n     | frames |
|----------|--------|-----|-------|--------|
| 0.39     | 0      | 34  | ~140  | flowing |
| 0.28     | -3     | 5   | ~33   | flowing |
| 0.20     | -6     | —   | —     | **SILENT** |
| 0.10     | -12    | —   | —     | **SILENT** |
| 0.05     | -18    | —   | —     | **SILENT** |
| 0.00     | mute   | —   | —     | **SILENT** |
| 0.39     | recovery | **34** | **158** | flowing |

**The recovery control is what makes this conclusive.** Fourteen consecutive
silent transmissions could have meant the rig simply stopped pushing at 09:20
for unrelated reasons; the return to exactly baseline (`max=34, n=158`) at
09:26:43, and immediate re-silencing when re-muted, rules that out. Silence
tracks drive, reversibly.

### THE DIAGNOSTIC MATRIX — all four states are distinguishable

| condition                        | max | n    |
|----------------------------------|-----|------|
| healthy                          | 34  | ~155 |
| **collapse mid-transmission**    | 34  | **23** |
| reduced drive throughout (-3 dB) | 5   | 33   |
| total collapse                   | —   | **silent** |

The mid-transmission case was produced deliberately: keyed 09:31:00, drive held
3 s, muted 09:31:03, transmission ended 09:31:13.

**`n` is the primary diagnostic, NOT `max`.** A mid-transmission collapse has an
*identical* `max` to a healthy transmission (34 in both) because drive genuinely
was present briefly; only the sample count betrays it — 23 vs ~155, and 23/155
≈ 15% against the ~24% of the window that had drive, so it tracks duration.
`max` is what then separates "collapsed after coming up" (34) from "weak
throughout" (5). **Both fields are needed; neither alone suffices.**

### WHY: the rig pushes ON CHANGE

A meter pinned at zero has nothing to report, so it sends nothing. `max=0` never
occurs. This is why silence — not a low number — is the collapse signature, and
it is the opposite of what the instrument was designed to detect. The
"rig pushed no meter data for this transmission" line was built as a *"does this
rig push at all?"* diagnostic and was twice dismissed as instrument failure when
it was in fact the fault being reported.

**It had already fired unnoticed**: three blank transmissions at 09:05:28,
09:05:58 and 09:06:28 were the collapse signature, from the audio still being
down after an earlier volume test.

### TWO FIELDS THAT LOOK DIAGNOSTIC AND ARE NOT

- **`min` was pure key-up ramp.** EVERY transmission logged `min=0`, healthy ones
  included, because PTT comes up a few samples before the waveform starts. Now
  computed from the first non-zero reading onward, which needs no invented
  settling interval — the rig says when drive arrived by reporting a non-zero
  value. Demoted but kept honest.
- **`last` is pure key-down tail** and is still unfixed. All three *clean* 5 W
  controls logged `last=0` because the final sample lands after the waveform
  stops but before PTT drops. **Do not read `last` as evidence of anything.**

### FOLLOW-UPS

1. **An alarm must key on silence / depressed `n` during a keyed transmission,
   not on a low reading.** The ambiguity to resolve first: "no meter data" today
   means BOTH "instrument broken (wrong rigdef, AI not armed)" AND "drive is
   zero". The discriminator already exists and needs no threshold — the S-meter
   pushes continuously at ~26 Hz in *receive*, so a working instrument is
   provably alive between transmissions.
2. **`last` needs the same onset-style treatment as `min`, or removal.**
3. **The sensitivity curve is steep between 0.28 and 0.39** and unmeasured there;
   5 W is evidently near the bottom of the rig's drive range. A finer sweep would
   locate the detection floor.
4. **Push rate is NOT constant** — ~26 Hz in receive, ~4 Hz under voice, ~12 Hz
   under FT8. Any consumer must tolerate the range; nothing may assume an
   interval.
5. **`ft8.tx.max_repeats = 6` caps a CQ session at six transmissions**, which
   shaped this test plan into 6-transmission blocks. Worth remembering for any
   future on-air experiment.
6. **The FT-710 rigdef is untouched.** Its `RM`/`MS` selectors were not verified
   against its own CAT manual and must not be assumed identical to the FTdx10's.

## [2026-07-30] DRIVE ALARM — on-air acceptance PASSED, and two findings that change what it proves

**TRIAGED 2026-08-07:** Finding 2 (banner time anchor) was BUILT same day.
Finding 1's evidence step (inter-frame gaps + value histogram in the keyed
window) is BUILT — `withMeterContext` gap fields + `_hist` buckets in
`meters.go` — so the value-aware-rule decision now waits on the next real
collapse being read from that logging. Finding 1b (overdrive invisible) is
PARTLY covered by ADR 0064: the ALC chip shows red on overdrive whenever the
FT8 view is open; an overdrive ALARM remains undecided. Text kept as the
reasoning trail.

Layer 3 of the ATDD plan, run on the air at operating power (not the 5 W dummy
load of the sweep) during a live CQ run. Both cases fired; no healthy slot fired.
Citations are `smd.log` timestamps from 2026-07-30.

| case                              | key-down | alarm    | latency               | n   | clean n |
|-----------------------------------|----------|----------|-----------------------|-----|---------|
| collapse mid-slot (muted at ~+6s) | 04:57:15 | 04:57:24 | +9 s = 3 s after last frame | 129 | 327–482 |
| silent from key-down (muted before) | 05:01:15 | 05:01:18 | **+3 s exactly**      | 35  | ~365    |

Healthy slots either side alarmed nothing: 04:58:58 `n=376`, 04:59:28 `n=349`,
05:02:28 `n=360`. Two QSOs completed through the run (R2EC KO82, UX7QV KN29).

**Process cost worth recording: the second test was run by muting into a live
QSO.** The rung was read at 05:00:27 (`calling-cq repeats 9`), the mute applied
at 05:00:58, and UX7QV had answered at 05:00:45 in the gap — so `UX7QV 7Q5MLV
-20` repeat 2 did not radiate. The QSO survived only because repeat 1 had already
gone out. Operator directive follows: **no transmit-path change without
per-instance prior agreement** (warning and asking are expected; acting is what
needs approval). Re-read the CURRENT rung immediately before any such action.

### FINDING 1 — absent drive does NOT produce pure silence, and the alarm's margin is thinner than designed

The design comment in `internal/bridge/drivealarm.go` claimed drive that is
absent "produces SILENCE rather than zero-valued frames, because the rig pushes
on change and a meter pinned at zero has nothing to report." **The 05:01:15 slot
disproves the absolute form of that claim.** It logged `min=0 max=109 last=6
n=35` while muted from before key-down:

- `max=109` can only have come from the unmute at 05:01:27.67, ~0.33 s before the
  slot ended — muted audio cannot produce full PO.
- At the measured 12–26 Hz that tail accounts for ~5–9 frames, leaving **~26–30
  frames that arrived while no RF was leaving the rig**, necessarily reading zero
  (`min=0`).
- Yet the alarm fired at exactly +3 s, so those frames were **not** in the first
  3 s: there was a complete gap from key-down to 05:01:18, then sparse
  zero-valued pushes at roughly 2–3 Hz for the remainder.

**Why this matters: it is a latent FALSE NEGATIVE.** The detector keys on
*gaps*, not values. Had those sparse zero-valued frames begun within 3 s of
key-down instead of after, the gap would never have opened and **no alarm would
have fired despite zero RF**. The mechanism was believed to be safe because
absent drive was silent; it is actually safe only because absent drive is silent
*for long enough*, and nothing measured says how reliably.

*The frame-count arithmetic above is an INFERENCE from a single summary line, not
a measurement.* The cheap way to settle it — no transmission required, no
threshold invented — is to log inter-frame gaps or a value histogram for the
keyed window and read one collapse slot. Do that before deciding whether the
detector needs a value-aware rule; a "frames are all zero" rule would need a
definition of zero, which is the operator's call, not an inference.

This also **narrows but does not close** the OPEN QUESTION in
`drivealarm_test.go`'s header. Frames that flow reading zero are no longer
hypothetical — the hardware produces them. Still unobserved is frames arriving
*continuously* (no 3 s gap) all reading zero, which today would not alarm.

### FINDING 1b — at operating power `max` carries no signal at all

The sweep's matrix (5 W, dummy load) had `max` separating "collapsed after coming
up" (34) from "weak throughout" (5). At operating power `max` was **identical
across every state observed**: healthy 109, mid-slot collapse 109, silent-from-
key-down 109 (from the unmute tail), and 112–113 during an accidental 3900% drive
overdrive. So the earlier claim that "both fields are needed" holds only at the
bottom of the drive range where it was measured. **`n` carried the entire signal
here.** Do not port the 5 W matrix's thresholds to operating power.

Related: `wpctl set-volume 66 39` — a `0.39` typo — put 3900% gain on the rig's
audio drive for two CQ slots (04:57:45, 04:58:15, `max=112`/`113`). An overdriven
FT8 signal splatters and is unlikely to decode. Nothing in SM can see this: the
drive alarm watches for the ABSENCE of output and an overdriven slot looks
healthier than healthy. Worth a thought about whether an upper bound belongs
anywhere.

### FINDING 2 — the banner has no time anchor, so a stale alarm is indistinguishable from a fresh one

Observed directly: at ~05:02 the operator asked "I have got the NO RF OUTPUT
banner again... ?" No second alarm had fired. It was the 05:01:18 banner still
up — correct behaviour (the alarm deliberately has no auto-clear, because "the
meter started reporting again" is not evidence the operator has checked the
radio), but the banner says nothing about *when* it fired, and by then three
minutes and four healthy transmissions had passed.

This is a miss in the criterion, not the code. The wording was made deliberately
tenseless after three review rounds flip-flopped over tense (`84ed3ffc`), and in
removing every claim about the rig's key state it also removed every anchor to
time. The nearest-confusable-state clause was written about no-RF vs dead
instrument and never asked about **fresh alarm vs stale alarm**.

**BUILT 2026-07-30.** Acceptance criterion, as answered by the operator:

> When the drive alarm has fired and I come back to the screen later, the banner
> tells me WHEN it fired as a clock time, and once one armed-and-silent
> transmission has completed it also tells me output has been normal since — and
> I can tell that apart from an alarm that has just fired for a transmission now
> in progress. Dismissal stays manual either way.

The three judgement calls and their answers:

1. **Absolute time** — "at 05:01:18", not "3 minutes ago". A relative label needs
   a refresh timer and goes stale silently, which is the exact fault being closed.
2. **Show recovery: yes.** Reported as a second clause on the same banner.
3. **Recovered after ONE healthy transmission.**

One thing the operator's wording left open, decided and stated rather than
buried: **"healthy" means the drive watch was ARMED and stayed silent**, not
merely that no alarm fired. A transmission where the watch never armed — no
instrument-alive evidence — says nothing about output, so reporting recovery from
it would claim a measurement never made, the same fault the banner's S8 rule
exists to prevent.

Mechanism: the daemon owns the recovery signal, because only it can tell a
healthy FT8 transmission from a tune carrier; the SPA watching `tx-status` go
1→0 cannot. It rides the existing `drive-alarm` event as `Active=false`, which is
what that field was reserved for. **It is NOT a clear** — the banner stays until
dismissed, because a rig whose output came back has still not been looked at.
That distinction was a live defect: the SPA handler assigned `p.active` straight
to `driveAlarmActive`, so a recovery would have made the banner vanish.

The states this change would create, each a rule to write BEFORE implementing:
alarm-raised-and-still-current; alarm-raised-then-recovered; alarm-raised-then-
raised-again (does the timestamp update, or list both?); alarm-raised-then-CAT-
dropped (recovery unknowable — `resetCatLink()` already clears the alarm state,
so check that path rather than assuming it).

### CORRECTION to the 2026-07-29 follow-up 5 above

"`ft8.tx.max_repeats = 6` caps a CQ session at six transmissions" is **wrong**,
and it shaped that day's test plan into 6-transmission blocks. Today's run
reached `repeats 9` while still calling CQ (05:00:15) and kept going. The cap
applies to repeats of a rung while working one answerer; **a CQ run is unbounded
until Abandon**, which is what `robustness-pass-position` and ADR 0033 already
said. Plan future on-air experiments accordingly.

- ~~[2026-07-31] Move the shell warning banners to **sticky toasts** for consistency.~~
  **→ backlog P3 (triage 2026-08-07): decide together with the notification history
  rail below and ADR 0060 (alert placement, itself blocked on observation). The
  S5/S6 don't-flatten constraint and the daemon-retractable-toast wrinkle stand.**
  There are now three stacked shell-level surfaces — `TxAlarmBanner`,
  `DriveAlarmBanner` and the new `DriveMonitorNotice` — and each one reflows the
  whole page downward, whereas toasts stack in a corner without moving the content
  being read. That layout cost is the real argument, and it gets worse with each
  surface added. **Do NOT flatten all three into one channel, though:**
  `DriveAlarmBanner.svelte.test.ts` rules S5/S6 exist specifically so a drive fault
  and a stuck transmitter never render as each other — they demand opposite
  responses ("your audio path died, carry on" vs "your rig may be keyed right now,
  go and look at it") — and a consistency pass that gives them the same visual
  weight erodes exactly that. Suggested split: drive alarm + drive-monitor notice →
  sticky toasts (the drive alarm already has a Dismiss contract that is
  toast-shaped); tx-alarm stays a banner, because being hard to ignore IS the
  feature for that one. **Design wrinkle to settle first:** the drive-monitor
  notice RETRACTS ITSELF when the rig's meter goes back to PO (the daemon knows
  when the condition ends), whereas toasts are normally dismissed rather than
  withdrawn — so this needs a keyed, daemon-retractable toast, which the toast
  system may not have today. Check before scoping. Open question for the operator:
  does a sticky toast that vanishes on its own read as "handled" or as "lost"?

- ~~[2026-07-31] **Notification history rail — transient warnings are unrecoverable
  today.**~~ **→ backlog P3 (triage 2026-08-07): decide with the sticky-toast note
  above; the substrate is ADR 0061's event store (alarms pilot slice) + ship-gate
  (c) — build the rail on that, never on `smd.log`. Retention/persistence/badge
  questions remain the operator's.** Original: Trigger: a red banner flashed and was gone before the operator could
  read it; answering "what was that?" needed a grep of a 14 MB `smd.log`, which no
  ordinary user can do. (It was real — `tx_still_keyed` at 05:09:13, cleared
  05:09:14 after one `tx_off` re-send. 7 such alarms in 10 days; the last 5 all
  cleared inside 1 s.) Operator's shape: an icon in a rail where toasts also get
  recorded. **BUILD IT AS A NOTIFICATION HISTORY, NOT A LOG VIEWER**, and the
  reason is security-by-construction, not taste: `smd.log` is 0600 and clean today
  (0 hits for api_key/token/password/secret/Authorization/session_key) BUT it holds
  ~170 `callsign provider error` lines whose text comes from an EXTERNAL provider
  and is not under our control — serving that file to a browser makes every future
  third-party error string an exfiltration path, which is the exact shape of the
  two P1 credential leaks of 2026-07-25. Feed the rail instead from events the
  daemon already publishes FOR DISPLAY: `tx-alarm`, `drive-alarm`,
  `rig-disconnected`, `bridge-error`, the new drive-monitor state, plus
  client-side toasts. Only operator-facing things can then ever appear in it.
  **Smaller than it sounds:** the hub already caches one slot each for
  `bridge-error`/`rig-disconnected`/`tune-state` for late subscribers, so a
  bounded ring buffer generalises a mechanism that exists. **Daemon-side, not
  client-side** — the motivating cases (looked away, reloaded, was on another tab)
  are precisely what client state loses. **Partly absorbs the sticky-toast note
  above:** with a history rail, transient presentation stops being load-bearing,
  so decide the two together. Open questions for the operator, none to be
  invented: retention (last N events, or a time window?); survive a daemon restart
  (persisted) or in-memory only?; unread badge on the icon, or a plain list?
- ~~[2026-08-01] when answering a cq and auto-work armed, when the contact has completed and nobody calls you, so you start a cq call - the auto-work armed stays active, or the pill stays viewable~~
  — **TRIAGED + FIXED 2026-08-01.** The either/or is BOTH, and the pill was innocent:
  `StartCallCq` reset `caller` / `stalledCalls` / `confirmHold` / `contact` for the fresh
  session and never touched `autoWork`, so the run genuinely survived and the badge was
  honestly reporting it. **It could not fire** — `onSlotIdleArmed` gates on
  `mode == seqIdle`, and a Call-CQ contact resumes CQ rather than ending, so the only
  exit is Abandon, which disarms. So this was never a rogue-transmission risk. What it
  cost was an indicator naming the wrong mechanism (during a CQ run answerers *are*
  worked without a click, but by `pickAnswererLocked`, not the auto-work run), plus a
  call/offset/dial still pinned from the PREVIOUS session that any future path back to
  `seqIdle` would have transmitted on. Operator's call: **clear it** — starting a CQ is a
  new operator-started session that pins its own parameters, and Abandon stops
  everything. Fix is one assignment in `caller_sequencer.go` ahead of the
  `statusLocked`/`publish`, so the frame announcing the CQ already reports the run
  stopped. Pinned by `autowork_test.go` **W12** (state) and **V5** (that frame) — V5
  demonstrated load-bearing by moving the clear after the publish, which leaves W12
  green and V5 red. No SPA change: it rebuilds the whole `qso` object per frame, so the
  `omitempty` on `auto_work_armed` reads as false and the pill goes out.
- ~~[2026-08-01] add to the map the ablity to filter (select) by band: all to whatever configured bands are in the config.~~
  **→ backlog P3 (triage 2026-08-07): the 08-01 analysis stands; open question
  (band-list source (a)/(b)/(c), read = (b) with (a) as ordering hint) is the
  operator's.**
  — **TRIAGED 2026-08-01, not started. Most of the machinery already exists**, which
  makes this smaller than it sounds: every map row already carries a normalised
  `band` (`mapData.svelte.ts:57`), `bandRank` already gives wavelength ordering,
  and **MapView already renders a legend of "the bands actually in the window"**
  (`MapView.svelte:59-67`) — the natural place to hang the control. What is missing
  is only the filter itself; the map has no filtering of any kind today.
  **THE ONE AMBIGUITY, and it needs the operator: "configured bands in the config"
  most likely means `logging_station.operating_bands`** — it exists
  (`types/station.go:44`), is already exposed to the SPA as `operatingBands`
  (`seams.ts:135`), and its doc says *"Empty/unset means all bands"*. But that
  default is the problem: on a station that has never set it, the selector would be
  EMPTY. And a band present in the logged data but absent from `operating_bands`
  would become unreachable — the operator could not select the band they are looking
  at. So the choice is between (a) config `operating_bands`, (b) the bands actually
  present in the window (what the legend already computes, never empty and never
  hides data), or (c) both — config as the ordering/whitelist with a fallback to the
  data. **My read is (b) with (a) as the ordering hint**, but it is the operator's
  call and (a) alone has a failure mode worth stating out loud.
- ~~[2026-08-01] ad adjustable (width) headers to the session panel, plus the ability to order columns: asc, desc, raw~~
  **→ backlog P3 (triage 2026-08-07): the 08-01 analysis stands — any resize UI
  must keep `table-fixed` binding widths (the 07-18 fix); persistence + sort-model
  questions are the operator's.**
  — **TRIAGED 2026-08-01, not started.** `SessionPanel.svelte` has 8 columns at FIXED
  Tailwind widths (`w-20` Time, `w-27` Call, `w-12` Band, `w-14` Mode, `w-10` Sent,
  `w-10` Rcvd, `w-32` Name, `w-32` Country) and no sorting — `{#each session.qsos}`
  renders insertion order, so **"raw" is what it does today** and the third sort
  state is "get me back to logging order", which is the right instinct for a session
  log where order carries meaning.
  **The width half collides with an existing fix, and that is the thing to know
  before starting.** The table is deliberately `table-fixed`, with a comment dated
  dogfood 2026-07-18: auto layout ignores `w-*` as a cap, so long names/countries
  stretched the table instead of ellipsizing. Column widths therefore BIND, and any
  resize UI has to keep that property — a naive drag-to-resize that reverts to auto
  layout reintroduces the 07-18 bug. Persisting widths also needs a decision
  (localStorage vs daemon config); the map's band colours went to daemon config,
  which is the precedent for "operator display preference that should survive a
  different browser". **Open for the operator:** persist or per-session, and whether
  sorting is per-column or one active sort at a time.
- ~~[2026-08-02] Rigs tab: expose the daemon's ACTIVE rig (the one the bridge actually has open, qsoservice activeRigID) so the tab can distinguish it from the configured default.~~
  **→ backlog P2 (triage 2026-08-07): small daemon+SPA feature, needs the active
  rig on the wire first.** Original: Today only default_rig_id reaches the SPA, so after "Set as default" the two diverge until a restart while the UI can't tell. Needs the active rig on the wire (e.g. /v1/config or /v1/rig/...), then show "active" only when the bridge really has that rig open, plus a "default changed — restart to apply" indicator when default != active. Follow-up to the 2026-08-02 relabel that changed the pill from "active" to "default".
- ~~[2026-08-06] ft8 rx audio - when TX'ing the panel changes height. Height should remain consistent with temp gauge or msg.~~ **FIXED same day:** the open card now renders a fixed structure in every state — bar track + two fixed-height lines always present, content varies inside them (V6 pins it, `AudioLevelCard.svelte.test.ts`).
- ~~[2026-08-07] phone/cw port the paste list from the comment field in the logging SPA~~
  **→ backlog P2 (triage 2026-08-07):** port `commentHistory.svelte.ts` (bounded MRU
  + dropdown) from the retired SPA into frontend/app's Phone/CW card — port, never
  patch the source. Behind the FT8 focus.
- [2026-08-07] export session card - when clicking send, a toast appears by it is not on top, but overlayed by the Export session card and thus dimmed.
  — **TRIAGED 2026-08-07, fix queued — root cause found:** `Toasts.svelte` and
  `ExportDialog.svelte` are BOTH `z-50`; the later-mounted dialog wins. NB commit
  `f6c6fe48` says "Fix toast overlay issue" but its diff is only this /log line —
  nothing was fixed. One-line layer fix.
- [2026-08-07] 07:20: clicked VK5GR and got a toast saying already worked this session - but I had not worked
  — **TRIAGED 2026-08-07 — same defect as the abandon item below, one fix.** The
  toast keys on the ENGAGED set (deliberately includes abandoned/incomplete
  contacts, persisted per-tab in sessionStorage), so an earlier engagement that
  never logged still reads "worked". Mechanism stays (allow_duplicate's
  over-marking is the safe direction); the WORDING must split: "worked" only on a
  `session.qsos` hit, engaged-only needs its own message (wording = operator's
  call, queued for the 2026-08-07 design session).
- [2026-08-07] answering a CQ automatically arms auto-work.
  — **TRIAGED 2026-08-07 — as designed, not a bug** (ADR 0059 W9,
  `sequencer.go:687`: an operator-started session arms a run; answering a CQ is
  the entry point the feature was asked for). Gated on `ft8.tx.auto_work_callers`
  — off today stops it. Whether the CHOICE should be per-click instead of policy
  → the design session below.
- [2026-08-07] answering a cq-> abandon->answer same cq->toast 'already worked this session' - this should only be true if the QSO has been successfully logged
  — **TRIAGED 2026-08-07 — folded with the VK5GR item above (one fix, wording
  split logged-vs-engaged).**
- [2026-08-07] there should be a way to answer a cq call without arming auto-work. Maybe (discussion only) ctrl+shift+click-on-cq == auto-work; single click-on-cq == work that station only; ctrl+click == add station to pile-up queue?
  — **TRIAGED 2026-08-07 → design session (auto-work · CQ-answer · operator_pick
  as one grammar).** Current gestures: single-click CQ = answer (+ auto-work when
  policy on) · ctrl/cmd-click calling-you row = pile-up · double-click plain row =
  directed call. The sketch moves the auto-work decision from config policy to
  per-click; intersects the open `operator_pick` thread (config-accepted,
  runtime-rejected).
- [2026-08-07] add a simple search as you typoe field to the session panel for callsigns
  — **TRIAGED 2026-08-07, fix queued:** nothing exists in `SessionPanel.svelte`;
  small SPA-only filter field.
