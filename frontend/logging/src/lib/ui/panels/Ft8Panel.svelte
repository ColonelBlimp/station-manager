<script lang="ts">
    import { onMount, onDestroy } from 'svelte';
    import Ft8OccupancyPanel from './Ft8OccupancyPanel.svelte';
    import Ft8MsgPanel from './Ft8MsgPanel.svelte';
    import Ft8SettingsPanel from './Ft8SettingsPanel.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import SessionEmailControls from './SessionEmailControls.svelte';
    import Ft8EnrichmentBox from './Ft8EnrichmentBox.svelte';
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { ft8State, startFt8, stopFt8, type DecodeEntry } from '../../states/ft8.svelte';
    import { ft8PileupStack } from '../../states/ft8PileupStack.svelte';
    import { ft8EnrichState, type Ft8CallInfo } from '../../states/ft8Enrich.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { catState } from '../../states/cat.svelte';
    import { parseCqCall, parseCq, parseDirectedToMe } from '../../utils/ft8Message';
    import { startFt8Qso, startFt8WorkCaller } from '../../api/ft8qso';
    import { toasts } from '../../states/toasts.svelte';
    import { frequencyToBand, formatFrequency } from '../../utils/frequency';
    import { formatUtcClock } from '../../utils/time';
    import { pathInfo } from '../../utils/bearing';
    import { setFreq, setMode } from '../../actions/rigControl';

    // Tune the rig to an FT8 band's dial freq AND assert the rig's FT8 mode, so
    // picking a band also puts the rig in data mode (e.g. USB-D on the IC-7300).
    // Mode is asserted only when CAT is live: setMode then drives the rig with
    // the rig literal (configState.bridge.ft8Mode); CAT-off it would write that
    // literal into manualState, which expects operator-friendly modes — so off,
    // we just tune (unchanged). Both calls are individually capability-gated.
    function tuneToFt8Band(freq: number): void {
        setFreq(freq);
        if (displayedState.isLive && configState.bridge.ft8Mode) {
            setMode(configState.bridge.ft8Mode);
        }
    }

    // The occupancy stream is scoped to this view: open while FT8 mode is showing,
    // close on leave (LoggingCard mounts/unmounts this panel with the Operating
    // Mode switch). See ft8.svelte.ts. The enrichment cache is dropped on leave
    // too so a re-open re-resolves against current operating history.
    onMount(startFt8);

    // Slot countdown — FT8 slots align to the UTC :00/:15/:30/:45 boundaries. This
    // panel stays mounted the whole time FT8 mode is shown, so the footer countdown
    // is live regardless of which lower tab is active. Light 500 ms tick.
    const SLOT_SECONDS = 15; // FT8 slot length (protocol constant)
    let nowSec = $state(Math.floor(Date.now() / 1000));
    let slotTimer: ReturnType<typeof setInterval> | undefined;
    onMount(() => {
        slotTimer = setInterval(() => (nowSec = Math.floor(Date.now() / 1000)), 500);
    });
    onDestroy(() => {
        stopFt8();
        ft8EnrichState.clear();
        clearInterval(slotTimer);
    });

    // Countdown to the next slot + that slot's parity. seconds-to-next = 15 −
    // (epoch mod 15) (epoch 0 is a slot boundary). Next-slot parity matches the
    // daemon convention (occupancy.go SlotRefFromTime): (unix / 15) % 2 == 0 → even
    // (:00/:30), else odd (:15/:45).
    const secondsToNextSlot = $derived(SLOT_SECONDS - (nowSec % SLOT_SECONDS));
    const nextSlotParity = $derived(
        (Math.floor(nowSec / SLOT_SECONDS) + 1) % 2 === 0 ? 'even' : 'odd'
    );

    // Current operating band, derived from the selected VFO. The worked-before
    // lookup is band+mode-specific, so the band is part of each enrichment key.
    const opFreq = $derived(
        displayedState.selectedVfo === 'B' ? displayedState.vfoB : displayedState.vfoA
    );
    const band = $derived(frequencyToBand(opFreq));
    // Whether opFreq is a real, rig-reported dial frequency rather than the placeholder
    // default `catState` initialises to. FT8 keys through a live rig (arming requires
    // isLive), but the rig can be "responding" while its frequency poll hasn't landed
    // yet — leaving opFreq at the 14.250 placeholder. We must NOT log that, and we show
    // the operator a "waiting" state instead of a plausible-but-wrong frequency.
    const freqKnown = $derived(displayedState.isLive && catState.freqKnown);

    // Clear the Band Activity feed when the operating band changes: the
    // accumulated rows are decodes from the previous band's watering hole and
    // would be misleading mixed with the new band's traffic. Track the last
    // seen band so intra-band dial nudges (that don't cross a band boundary)
    // don't wipe the list. lastBand starts '' so the first run just records
    // the band — the feed is empty on mount, so that initial clear is a no-op.
    // Empty band ('' — no/invalid dial freq) is ignored so a transient unknown
    // doesn't clear it.
    let lastBand = $state('');
    $effect(() => {
        if (band !== '' && band !== lastBand) {
            ft8State.clearDecodes();
            lastBand = band;
        }
    });

    // Operator's Maidenhead grid — the local end of the beam-heading calc for each CQ.
    // pathInfo() tolerates 4/6/8-char; empty/invalid yields no bearing (blank column).
    const myGrid = $derived(configState.loggingStation.myGridsquare);
    // Operator's callsign — used to spot decodes calling US (the pile-up). Falls back to
    // the operator field, mirroring the daemon's identity resolution. parseDirectedToMe
    // trims/upper-cases; a blank value never matches.
    const myCall = $derived(
        configState.loggingStation.stationCallsign || configState.loggingStation.operator || ''
    );

    // A Main-Freq button highlights when the live dial is within this much of its
    // configured FT8 freq. The dial sits exactly on the watering hole during FT8, so
    // the small window only absorbs rig rounding / a fine nudge — it means "you're on
    // this band's FT8 freq", not merely "somewhere in this band".
    const BAND_MATCH_TOL_HZ = 500;

    // Drive flag + worked-before lookups for the CQ stations on screen. Runs once
    // per slot (when the decode list changes) and on a band change (new keys);
    // observe() dedupes, so re-scanning the visible rows costs nothing after the
    // first sighting of each call. Only CQ lines are enriched (one unambiguous
    // callsign); reply/report lines are left plain for now.
    $effect(() => {
        for (const d of ft8State.decodes) {
            // CQ lines, plus stations calling US (the pile-up) — both carry one
            // unambiguous callsign, so both get the flag + worked-before tint.
            const call = parseCqCall(d.text) ?? parseDirectedToMe(d.text, myCall)?.call ?? null;
            if (call) ft8EnrichState.observe(call, band);
        }
    });

    // Worked-station enrichment for the Rx Frequency pane's Enrichment box (both
    // roles — answering a CQ or working someone who answered our CQ). The far-end
    // call may not have been a CQ row (caller role), so enrich it here too; the
    // box shows op name, country (flag + name) and the short-path distance.
    $effect(() => {
        if (ft8State.qso.active && ft8State.qso.theirCall) {
            ft8EnrichState.observe(ft8State.qso.theirCall, band);
        }
    });
    const workedInfo = $derived(
        ft8State.qso.active && ft8State.qso.theirCall
            ? ft8EnrichState.info(ft8State.qso.theirCall, band)
            : undefined
    );
    // Short-path geometry to the worked station, from the operator's grid to the
    // enriched (QRZ) locator — computed once and split into distance + beam heading
    // for the enrichment pane. null when either grid is missing/invalid.
    const workedPath = $derived(
        myGrid && workedInfo?.grid ? pathInfo(myGrid, workedInfo.grid) : null
    );
    const workedDistanceKm = $derived(workedPath?.shortPathDistanceKm ?? null);
    const workedBearingDeg = $derived(workedPath?.shortPathBearing ?? null);

    // SNR formatted WSJT-X-style: an explicit-sign integer dB ("+04", "-13").
    function formatSnr(snr: number): string {
        return (snr >= 0 ? '+' : '-') + String(Math.abs(snr)).padStart(2, '0');
    }

    // Tint colour for a CQ row: the attention colour when the station is new on
    // this band, the muted colour when worked before, and no override (default
    // text colour) while the worked answer is still pending or for non-CQ rows.
    function rowColor(info: Ft8CallInfo | undefined): string | undefined {
        if (!info || info.worked === undefined) return undefined;
        return info.worked
            ? configState.ft8Display.highlightWorked
            : configState.ft8Display.highlightUnworked;
    }

    // Answering a CQ (ADR 0031 step e3): a CQ row is clickable to start an
    // exchange when TX is armed, an offset is picked, and no QSO is already
    // running. The daemon then auto-advances the ladder.
    // freqKnown is part of the gate: without a real rig frequency we'd log the QSO on
    // the placeholder band (the 14.250 bug). Better to leave the row un-clickable — the
    // freq label below the Main-Freq buttons shows "waiting for rig" so the reason is
    // visible — than to record a wrong-band contact.
    const canAnswer = $derived(
        ft8State.tx.armed && ft8State.selectedOffset !== null && !ft8State.qso.active && freqKnown
    );

    async function answerCq(d: DecodeEntry): Promise<void> {
        const cq = parseCq(d.text);
        if (!cq || !canAnswer || ft8State.selectedOffset === null) return;
        // opFreq is the dial frequency in Hz (selected VFO); the daemon logs the
        // QSO at the dial frequency (FT8 convention — not dial+offset), so it
        // needs the dial freq in MHz.
        const out = await startFt8Qso(
            cq.call,
            cq.grid,
            d.startUtc,
            ft8State.selectedOffset,
            opFreq / 1_000_000
        );
        if (out.kind !== 'ok') toasts.error(out.message);
    }

    // Working a station calling US (ADR 0033 "work a caller"): a directed-at-me row is
    // clickable under the same gate as answering a CQ (armed + offset + idle). d.snr is
    // our SNR of their calling signal — the report we send back (RST_SENT).
    async function workCaller(d: DecodeEntry): Promise<void> {
        const toMe = parseDirectedToMe(d.text, myCall);
        if (!toMe || !canAnswer || ft8State.selectedOffset === null) return;
        const out = await startFt8WorkCaller(
            toMe.call,
            toMe.grid,
            d.snr,
            d.startUtc,
            ft8State.selectedOffset,
            opFreq / 1_000_000
        );
        if (out.kind !== 'ok') toasts.error(out.message);
    }

    // Pile-up stacking: Ctrl/Cmd+click a calling-you decode to ENQUEUE it (pure
    // capture — call/grid/SNR/slot — available in ANY state, including mid-QSO or
    // disarmed, because nothing transmits at capture time). Plain click still works
    // the station now (when armed+idle). This is the whole point: grab callers the
    // instant you see them in your RX slot, even when you can't act on them yet.
    function enqueueCaller(d: DecodeEntry): void {
        const toMe = parseDirectedToMe(d.text, myCall);
        if (!toMe) return;
        ft8PileupStack.push({ call: toMe.call, grid: toMe.grid, snr: d.snr, slotUtc: d.startUtc });
        // A fresh capture re-enables draining (an Abandon earlier paused it).
        ft8PileupStack.resume();
    }
    function onCallerClick(e: MouseEvent, d: DecodeEntry): void {
        if (e.ctrlKey || e.metaKey) {
            enqueueCaller(d);
            return;
        }
        if (canAnswer) void workCaller(d);
        // Plain click while busy/disarmed: no-op — Ctrl+click to stack instead.
    }

    // Drain the pile-up stack (operator-curated FIFO): whenever the rig is armed +
    // idle + freq known + an offset picked and auto-drain is enabled, work the head
    // via the work-a-caller path, advancing as each contact completes. `draining`
    // latches from the StartWorkCaller call until qso.active confirms, so the effect
    // can't double-fire in the window before the daemon pushes the active state. The
    // queue lives wholly in the SPA; the daemon is untouched (reuses StartWorkCaller).
    let draining = false;
    $effect(() => {
        // A contact is active (ours just started, or in flight) → never drain, and
        // clear the latch: our start has landed.
        if (ft8State.qso.active) {
            draining = false;
            return;
        }
        if (draining) return;
        if (!ft8PileupStack.enabled || ft8PileupStack.items.length === 0) return;
        if (!ft8State.tx.armed || !freqKnown || ft8State.selectedOffset === null) return;
        const head = ft8PileupStack.peek();
        if (!head) return;
        draining = true;
        ft8PileupStack.dequeue();
        const offset = ft8State.selectedOffset;
        void startFt8WorkCaller(
            head.call,
            head.grid,
            head.snr,
            head.slotUtc,
            offset,
            opFreq / 1_000_000
        ).then((out) => {
            // On success the latch clears when qso.active flips true (above). On a
            // refused start, surface it and release the latch so the next idle tick
            // can try the next entry.
            if (out.kind !== 'ok') {
                toasts.error(out.message);
                draining = false;
            }
        });
    });

    // Slot label: "14:30:15 · odd", or a waiting state before the first event.
    // start_utc is RFC3339 UTC; render its UTC wall-clock with seconds so the four
    // 15 s slots per minute are distinguishable. (Congestion isn't shown here — the
    // occupancy strip carries it visually; a raw count wasn't actionable.)
    const slotLabel = $derived.by(() => {
        if (!ft8State.slot) return 'Waiting for slot…';
        const clock = formatUtcClock(new Date(ft8State.slot.start_utc));
        return `${clock} · ${ft8State.slot.period}`;
    });

    // The daemon sends `suggested` best-first by rank. For display we sort a copy
    // ascending by frequency so the chips read left-to-right like a band, and mark
    // the daemon's #1 (suggested[0], pre-sort) with a ★. The rank order stays on
    // the wire for step (e)'s "give me the best slot". Colour-fill is reserved for
    // the future selected-offset state so it doesn't clash with this recommendation
    // marker.
    const topPick = $derived(ft8State.suggested[0] ?? null);
    const sortedOffsets = $derived([...ft8State.suggested].sort((a, b) => a - b));

    // Band Activity ordering. Normally the feed is the decode list as-is (newest slot
    // first, freq-ascending within slot). With "Float CQ calls to top" on, CQ rows are
    // stably partitioned above the rest (each group keeps its existing order) so the
    // answerable stations sit together at the top. Slot separators are suppressed in
    // that mode (the list is no longer slot-ordered) — see showSlot in the markup.
    const cqToTop = $derived(configState.ft8Display.cqToTop);
    const orderedDecodes = $derived.by(() => {
        if (!cqToTop) return ft8State.decodes;
        const cq: DecodeEntry[] = [];
        const rest: DecodeEntry[] = [];
        for (const d of ft8State.decodes) {
            (parseCqCall(d.text) !== null ? cq : rest).push(d);
        }
        return [...cq, ...rest];
    });

    // ---- Rx Frequency pane (WSJT-X-style) -------------------------------------
    // Filter the decode feed to the conversation the operator is watching. While
    // a QSO is active, the messages involving the worked station — matched by
    // callsign, so a few-Hz drift in their audio offset never drops them (more
    // precise than WSJT-X's pure-frequency window, which is all it has to go on).
    // When idle, the decodes sitting on the selected TX offset (±tolerance), i.e.
    // "what is on the channel I'm parked on". Tolerance ≈ the nominal signal width.
    const rxTol = $derived(ft8State.signalWidth > 0 ? ft8State.signalWidth : 50);
    const rxDecodes = $derived.by(() => {
        const all = ft8State.decodes;
        const q = ft8State.qso;
        if (q.active && q.theirCall) {
            const call = q.theirCall.toUpperCase();
            return all.filter((d) => d.text.toUpperCase().split(/\s+/).includes(call));
        }
        if (ft8State.selectedOffset !== null) {
            const off = ft8State.selectedOffset;
            return all.filter((d) => Math.abs(d.freqHz - off) <= rxTol);
        }
        return [];
    });
    // Caption under the Rx Frequency header: what the pane is currently keyed to.
    const rxCaption = $derived.by(() => {
        const q = ft8State.qso;
        if (q.active && q.theirCall) return `Following ${q.theirCall}`;
        if (ft8State.selectedOffset !== null)
            return `Offset ${ft8State.selectedOffset} Hz ±${rxTol}`;
        return 'No offset selected';
    });
    // Is the selected TX channel currently under another signal? Centralised on
    // ft8State.channelOccupied (overlap of [offset, offset+signalWidth) with any
    // occupied band); null when no offset is picked or no occupancy yet. Shared by
    // the footer Offset readout and the always-visible "Working X" banner below.
    const channelOccupied = $derived(ft8State.channelOccupied);
    // Highlight class for the footer Offset readout: red pill = busy, green pill =
    // clear (the Occupancy strip's red-600 / green-500). Only while idle on a picked
    // offset; the "Following X" / "No offset" captions stay neutral (inherit gray).
    // null (no occupancy yet) reads as clear, matching the prior empty-band default.
    const rxCaptionClass = $derived.by(() => {
        if (ft8State.qso.active || ft8State.selectedOffset === null) return '';
        return channelOccupied === true
            ? 'text-center rounded px-1.5 bg-red-600 text-white py-0.5 w-38'
            : 'text-center rounded px-1.5 bg-green-500 text-white py-0.5 w-38';
    });

    // "Working [callsign]" channel banner (always visible, every lower tab). Shown
    // only while a contact is in flight (qso.active). Colour tracks the selected TX
    // channel's occupancy so a station landing on the channel between pick and TX is
    // visible BEFORE the next transmission keys, not discovered after it starts.
    const workingCall = $derived(ft8State.qso.active ? ft8State.qso.theirCall : '');
    // green = channel clear · red = channel busy · gray = unknown (no offset picked
    // or no occupancy report yet — can't claim either).
    const workingBannerClass = $derived(
        channelOccupied === true
            ? 'bg-red-600 text-white'
            : channelOccupied === false
              ? 'bg-green-600 text-white'
              : 'bg-gray-200 text-gray-600'
    );
    const workingChannelLabel = $derived(
        channelOccupied === true
            ? 'channel BUSY'
            : channelOccupied === false
              ? 'channel clear'
              : 'channel unknown'
    );
    // Attempts-remaining countdown before the sequencer auto-abandons the current
    // rung (ADR 0031 off-ramp). The daemon sends max_repeats ONLY on the capped rungs
    // (>0), so a positive cap is the whole show-condition — no cap-vs-one-shot logic
    // here. remaining = cap − attempts-so-far, floored at 0 (at 0 the next slot
    // abandons). '' when not on a capped rung (calling CQ / one-shot 73/RR73).
    const callsLeft = $derived(
        ft8State.qso.maxRepeats > 0
            ? Math.max(0, ft8State.qso.maxRepeats - ft8State.qso.repeats)
            : null
    );
    const callsLeftLabel = $derived(
        callsLeft === null ? '' : ` · ${callsLeft} ${callsLeft === 1 ? 'call' : 'calls'} left`
    );

    // ---- Lower-section tabs (same pattern + .tab-item class as InfoPanel) ----
    type Ft8TabId = 'occupancy' | 'ladder' | 'session' | 'settings';
    // $derived so the Session count tracks sessionQsosState as FT8 QSOs land.
    const tabs: { id: Ft8TabId; title: string; count?: number }[] = $derived([
        { id: 'occupancy', title: 'Occupancy' },
        // Operate carries the pile-up depth so the operator sees the stack building
        // from any tab (the drawer itself lives in the Operate tab).
        { id: 'ladder', title: 'Operate', count: ft8PileupStack.count || undefined },
        { id: 'settings', title: 'Settings' },
        { id: 'session', title: 'Session', count: sessionQsosState.count },
    ]);
    let activeTab: Ft8TabId = $state('occupancy');

    const tabItemClass = (isActive: boolean): string =>
        isActive
            ? 'text-indigo-700 cursor-default'
            : 'text-gray-500 hover:text-gray-700 cursor-pointer';

    // WAI-ARIA tabs keyboard contract (mirrors InfoPanel): arrows cycle (wrap),
    // Home/End jump to ends, auto-activation so the panel follows focus.
    function moveTab(delta: number): void {
        const idx = tabs.findIndex((t) => t.id === activeTab);
        const next = (idx + delta + tabs.length) % tabs.length;
        activeTab = tabs[next].id;
        document.getElementById(`ft8tab-${tabs[next].id}`)?.focus();
    }

    function handleTabKeydown(e: KeyboardEvent): void {
        switch (e.key) {
            case 'ArrowRight':
                e.preventDefault();
                moveTab(1);
                break;
            case 'ArrowLeft':
                e.preventDefault();
                moveTab(-1);
                break;
            case 'Home':
                e.preventDefault();
                activeTab = tabs[0].id;
                document.getElementById(`ft8tab-${tabs[0].id}`)?.focus();
                break;
            case 'End':
                e.preventDefault();
                activeTab = tabs[tabs.length - 1].id;
                document.getElementById(`ft8tab-${tabs[tabs.length - 1].id}`)?.focus();
                break;
        }
    }
</script>

<!--
    FT8 operating-mode panel. Per the cards-vs-panels convention this is a
    content panel inside LoggingCard, shown when the header's Operating Mode
    switch is set to "FT8" (Phone/CW renders QsoPanel + CountryPanel + InfoPanel
    instead).

    Top row: Band Activity (every decode this slot) · Rx Frequency (the decode
    feed filtered to the conversation being watched — the worked station while a
    QSO is active, else the selected offset ±tolerance, WSJT-X-style) · Clear
    Offsets (the daemon's clear base offsets, frequency-sorted, ★ = top pick;
    click to select for TX). Offset occupancy + picking lives on the Occupancy
    tab's strip.
-->

{#snippet decodeRow(d: DecodeEntry)}
    {@const cq = parseCq(d.text)}
    {@const toMe = cq ? null : parseDirectedToMe(d.text, myCall)}
    {@const lineCall = cq?.call ?? toMe?.call ?? null}
    {@const lineGrid = cq?.grid ?? toMe?.grid ?? ''}
    {@const info = lineCall ? ft8EnrichState.info(lineCall, band) : undefined}
    {@const answerable = cq !== null && canAnswer}
    {@const queued = toMe !== null && ft8PileupStack.items.some((x) => x.call === lineCall)}
    {@const bearing = lineGrid && myGrid ? pathInfo(myGrid, lineGrid)?.shortPathBearing : undefined}
    <!-- A station calling US (toMe) gets a distinct amber tint so it stands out from
         band chatter. The row is ALWAYS clickable: Ctrl/Cmd+click adds it to the pile-
         up stack (any state — pure capture), plain click works it now (when armed+idle).
         A ✓ marks one already on the stack. -->
    <li class="flex gap-2 whitespace-nowrap {toMe ? 'rounded bg-amber-50' : ''}">
        <span class="w-7 text-right text-gray-500">{formatSnr(d.snr)}</span>
        <span class="w-10 text-right text-gray-500">{Math.round(d.freqHz)}</span>
        <span class="w-10 text-right text-indigo-600" title="Beam heading (short path)"
            >{bearing !== undefined
                ? `${Math.round(bearing).toString().padStart(3, '0')}°`
                : ''}</span
        >
        {#if info?.flag}
            <span class="cursor-default" title={info.country} aria-hidden="true">{info.flag}</span>
        {/if}
        {#if answerable}
            <button
                type="button"
                class="truncate text-left text-gray-700 cursor-pointer hover:underline"
                style:color={rowColor(info)}
                title={`Answer ${lineCall}`}
                onclick={() => answerCq(d)}>{d.text}</button
            >
        {:else if toMe}
            <button
                type="button"
                class="truncate text-left font-medium text-amber-700 cursor-pointer hover:underline"
                title={canAnswer
                    ? `Work ${lineCall} now · Ctrl+click to add to the pile-up stack`
                    : `${lineCall} is calling you · Ctrl+click to add to the pile-up stack`}
                onclick={(e) => onCallerClick(e, d)}>{queued ? '✓ ' : ''}{d.text}</button
            >
        {:else}
            <span class="truncate text-gray-700" style:color={rowColor(info)}>{d.text}</span>
        {/if}
    </li>
{/snippet}

<!-- Slot divider for accumulate (multi-slot) Band Activity: the per-row timestamp is
     dropped, so a divider carries each slot's UTC time + the operating band (e.g.
     "14:30:15 · 20m"); the rows below keep SNR / freq / flag / message. Single-slot
     mode shows no divider — the footer already carries the one slot's time. -->
{#snippet slotSeparator(utc: string)}
    <li
        class="mt-0.5 border-t border-gray-200 pt-0.5 text-gray-400 first:mt-0 first:border-t-0 first:pt-0"
    >
        {formatUtcClock(new Date(utc))}{#if band}&nbsp;·&nbsp;{band}{/if}
    </li>
{/snippet}

<!-- Column header for the decode lists. Lives inside the scroll container as a sticky
     row (top-0) so it stays pinned while the rows scroll under it; column widths/gap
     mirror decodeRow so the labels line up over their data. -->
{#snippet headerRow()}
    <div
        class="sticky top-0 z-10 flex gap-2 whitespace-nowrap border-b border-gray-200 bg-white pb-0.5 pl-1 pr-2 pt-1 font-mono text-xs text-gray-400"
    >
        <span class="w-7 text-right">dB</span>
        <span class="w-10 text-right">Hz</span>
        <span class="w-10 text-right">Beam</span>
        <span class="flex-1 text-left">Message</span>
    </div>
{/snippet}

<!-- Main-Freq band button: on click tunes the operating VFO to this band's configured
     FT8 dial freq AND asserts the rig's FT8 mode when CAT-live (setFreq → set_freq +
     setMode → ft8Mode; CAT off → manualState freq only), and highlights (btn-primary)
     when the live dial is already on that freq. Disabled if the band has no
     configured frequency. -->
{#snippet bandButton(b: string)}
    {@const freq = configState.ft8Frequencies[b]}
    {@const active =
        freqKnown && freq !== undefined && Math.abs(opFreq - freq) <= BAND_MATCH_TOL_HZ}
    <button
        type="button"
        class="btn w-14 {active ? 'btn-primary' : 'btn-secondary'}"
        disabled={freq === undefined}
        title={freq !== undefined ? `Tune ${b} — ${(freq / 1_000_000).toFixed(3)} MHz` : b}
        onclick={() => freq !== undefined && tuneToFt8Band(freq)}>{b}</button
    >
{/snippet}

<div class="flex justify-center h-85 text-gray-500 space-x-3 mt-1">
    <div class="flex flex-col text-center">
        <h2 class="text-base font-semibold my-2">Main Freq</h2>
        <div class="flex flex-col place-items-center px-2 space-y-1">
            <div class="flex gap-1">{@render bandButton('160m')}{@render bandButton('80m')}</div>
            <div class="flex gap-1">{@render bandButton('60m')}{@render bandButton('40m')}</div>
            <div class="flex gap-1">{@render bandButton('30m')}{@render bandButton('20m')}</div>
            <div class="flex gap-1">{@render bandButton('17m')}{@render bandButton('15m')}</div>
            <div class="flex gap-1">{@render bandButton('12m')}{@render bandButton('10m')}</div>
            <div class="flex gap-1">{@render bandButton('6m')}</div>
        </div>
        <!-- The live dial frequency (the value FT8 logs). Shown so the operator can
             verify it before working anyone — its absence was why wrong-band QSOs went
             unnoticed. "waiting for rig" (amber) when the rig hasn't reported a dial yet
             (catState.freqKnown false); FT8 TX is blocked in that state, so it doubles as
             the why-can't-I-click explanation. -->
        <div class="mt-4 text-base" title="Live dial frequency — what FT8 logs">
            {#if freqKnown}
                <span class="font-semibold text-indigo-700">{formatFrequency(opFreq)} Hz</span>
            {:else}
                <span class="text-amber-600">waiting for rig…</span>
            {/if}
        </div>
    </div>
    <div class="flex flex-col text-center ft8-panel-width">
        <h2 class="text-base font-semibold my-2">Band Activity</h2>
        <div class="flex h-65 flex-col rounded border border-gray-300 overflow-y-scroll">
            {#if orderedDecodes.length > 0}
                <!-- Slot separators only in accumulate mode AND when not floating CQ to
                     top (the CQ-first order isn't slot-grouped, so separators would be
                     meaningless). -->
                {@const showSlot = configState.ft8Display.feedMode === 'accumulate' && !cqToTop}
                {@render headerRow()}
                <ul class="flex-1 space-y-0.5 py-1 pl-1 pr-2 text-left font-mono text-xs">
                    {#each orderedDecodes as d, i (d.id)}
                        {#if showSlot && (i === 0 || orderedDecodes[i - 1].startUtc !== d.startUtc)}
                            {@render slotSeparator(d.startUtc)}
                        {/if}
                        {@render decodeRow(d)}
                    {/each}
                </ul>
            {:else}
                <p class="mt-1 text-xs">Waiting for decodes…</p>
            {/if}
        </div>
        <div class="mt-0.5 text-gray-700 text-xs">{slotLabel}</div>
    </div>
    <div class="flex flex-col text-center ft8-panel-width">
        <h2 class="text-base font-semibold my-2">Rx Frequency</h2>
        <div class="flex h-19 flex-col rounded border border-gray-300 overflow-y-scroll">
            {#if rxDecodes.length > 0}
                <ul class="flex-1 space-y-0.5 px-2 py-1 text-left font-mono text-xs">
                    {#each rxDecodes as d (d.id)}
                        {@render decodeRow(d)}
                    {/each}
                </ul>
            {:else if ft8State.qso.active}
                <p class="mt-1 text-xs">Waiting for {ft8State.qso.theirCall}…</p>
            {:else if ft8State.selectedOffset !== null}
                <p class="mt-1 text-xs">Nothing on this offset.</p>
            {:else}
                <p class="mt-1 text-xs">Pick an offset on the Occupancy tab.</p>
            {/if}
        </div>
        <Ft8EnrichmentBox
            call={ft8State.qso.theirCall}
            info={workedInfo}
            distanceKm={workedDistanceKm}
            bearingDeg={workedBearingDeg}
        />
    </div>
    <div class="flex flex-col text-center w-20">
        <h2 class="pt-1.5 text-xs font-semibold my-2">Clear Offsets</h2>
        {#if sortedOffsets.length > 0}
            <div class="flex flex-col place-items-center px-2 space-y-1 overflow-y-scroll">
                {#each sortedOffsets as offset (offset)}
                    <button
                        type="button"
                        class="w-16 rounded border px-2 py-0.5 font-mono text-sm text-left {offset ===
                        ft8State.selectedOffset
                            ? 'border-green-700 bg-green-50 text-green-900'
                            : 'border-gray-300 bg-gray-100 text-gray-700'}"
                        title={offset === topPick
                            ? 'Daemon’s top-ranked clear offset — click to select for TX'
                            : 'Click to select for TX'}
                        onclick={() => ft8State.selectOffset(offset)}
                    >
                        {offset}{#if offset === topPick}&nbsp;*{/if}
                    </button>
                {/each}
            </div>
        {:else if ft8State.slot}
            <p class="mt-1 text-xs">Band is full.</p>
        {:else}
            <p class="mt-1 text-xs">Waiting…</p>
        {/if}
    </div>
</div>
<!-- Slot countdown — moved here from the Operate tab so it's visible regardless of
     the active lower tab; sits at the bottom of the main activity row. -->
<div class="flex flex-row gap-x-2 text-gray-700 text-sm font-semibold justify-center w-full h-6.5">
    <div class="w-40 text-right">
        Next slot in {secondsToNextSlot}s · {nextSlotParity}
    </div>
    <div class="">&nbsp;|&nbsp;</div>
    <!-- RH info cell. While a contact is in flight it shows the worked station +
         selected-channel occupancy + the auto-abandon "N calls left" countdown
         (green=clear · red=busy · gray=unknown), replacing the idle offset readout —
         a station landing on the picked channel surfaces before the next TX keys.
         It lives in this always-visible countdown row, so it shows on every lower tab. -->
    {#if workingCall}
        <div
            class="rounded px-2 py-0.5 {workingBannerClass}"
            title="Worked station — selected TX channel occupancy + calls left before auto-abandon"
        >
            Working {workingCall} — {workingChannelLabel}{callsLeftLabel}
        </div>
    {:else}
        <div class={rxCaptionClass}>{rxCaption}</div>
    {/if}
</div>
<!-- Heroicon "cog-6-tooth" (outline) — the Settings tab icon, matching the gear
     used on InfoPanel's My Station tab so the two operating modes read alike. -->
{#snippet settingsIcon()}
    <svg
        class="size-5"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M10.343 3.94c.09-.542.56-.94 1.11-.94h1.093c.55 0 1.02.398 1.11.94l.149.894c.07.424.384.764.78.93.398.164.855.142 1.205-.108l.737-.527a1.125 1.125 0 0 1 1.45.12l.773.774c.39.389.44 1.002.12 1.45l-.527.737c-.25.35-.272.806-.107 1.204.165.397.505.71.93.78l.893.15c.543.09.94.559.94 1.109v1.094c0 .55-.397 1.02-.94 1.11l-.894.149c-.424.07-.764.383-.929.78-.165.398-.143.854.107 1.204l.527.738c.32.447.269 1.06-.12 1.45l-.774.773a1.125 1.125 0 0 1-1.449.12l-.738-.527c-.35-.25-.806-.272-1.203-.107-.398.165-.71.505-.781.929l-.149.894c-.09.542-.56.94-1.11.94h-1.094c-.55 0-1.019-.398-1.11-.94l-.148-.894c-.071-.424-.384-.764-.781-.93-.398-.164-.854-.142-1.204.108l-.738.527c-.447.32-1.06.269-1.45-.12l-.773-.774a1.125 1.125 0 0 1-.12-1.45l.527-.737c.25-.35.272-.806.108-1.204-.165-.397-.506-.71-.93-.78l-.894-.15c-.542-.09-.94-.56-.94-1.109v-1.094c0-.55.398-1.02.94-1.11l.894-.149c.424-.07.765-.383.93-.78.165-.398.143-.854-.108-1.204l-.526-.738a1.125 1.125 0 0 1 .12-1.45l.773-.773a1.125 1.125 0 0 1 1.45-.12l.737.527c.35.25.807.272 1.204.107.397-.165.71-.505.78-.929l.15-.894Z"
        />
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"
        />
    </svg>
{/snippet}

<!-- Heroicon "list-bullet" (outline) — the Session tab icon, matching InfoPanel's
     Session tab so the shared session log reads alike across both modes. -->
{#snippet sessionIcon()}
    <svg
        class="size-5"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M8.25 6.75h12M8.25 12h12m-12 5.25h12M3.75 6.75h.007v.008H3.75V6.75Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0ZM3.75 12h.007v.008H3.75V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Zm-.375 5.25h.007v.008H3.75v-.008Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
        />
    </svg>
{/snippet}

<!-- Heroicon "chart-bar" (outline) — the Occupancy tab icon; the bars read as the
     per-slot spectrum occupancy the picker strip shows. -->
{#snippet occupancyIcon()}
    <svg
        class="size-5"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z"
        />
    </svg>
{/snippet}

<!-- Heroicon "signal" (outline) — the Operate tab icon; broadcast arcs read as
     "on the air", the transmit surface where you arm + call/answer. -->
{#snippet operateIcon()}
    <svg
        class="size-5"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M9.348 14.652a3.75 3.75 0 0 1 0-5.304m5.304 0a3.75 3.75 0 0 1 0 5.304m-7.425 2.121a6.75 6.75 0 0 1 0-9.546m9.546 0a6.75 6.75 0 0 1 0 9.546M5.106 18.894c-3.808-3.807-3.808-9.98 0-13.788m13.788 0c3.808 3.807 3.808 9.98 0 13.788M12 12h.008v.008H12V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
        />
    </svg>
{/snippet}

<!--
    Tabbed lower section (same tablist pattern + .tab-item class as InfoPanel):
      - Occupancy — the TX-offset picker strip (Ft8OccupancyPanel)
      - Operate   — the FT8 transmit surface + message ladder (Ft8MsgPanel)
      - Session   — the shared session QSO log (SessionPanel) + email-out, same as
                    InfoPanel's Session tab; FT8 QSOs land here via the ft8-logged SSE
      - Settings  — FT8 display preferences (Ft8SettingsPanel), daemon-backed
    WAI-ARIA tabs contract mirrors InfoPanel: roving tabindex, arrow/Home/End
    navigation with auto-activation. justify-between floats the Session-tab email
    controls to the right of the strip (only while Session is active + mailer on).
-->
<div class="flex flex-col w-full px-6 pt-4">
    <div class="flex flex-row items-center justify-between border-b border-gray-400 pb-2">
        <div role="tablist" class="flex flex-row items-center space-x-12">
            {#each tabs as tab (tab.id)}
                <button
                    id={`ft8tab-${tab.id}`}
                    type="button"
                    role="tab"
                    class="tab-item {tabItemClass(activeTab === tab.id)}"
                    aria-selected={activeTab === tab.id}
                    aria-controls={`ft8panel-${tab.id}`}
                    tabindex={activeTab === tab.id ? 0 : -1}
                    onclick={() => (activeTab = tab.id)}
                    onkeydown={handleTabKeydown}
                >
                    {#if tab.id === 'occupancy'}{@render occupancyIcon()}{/if}
                    {#if tab.id === 'ladder'}{@render operateIcon()}{/if}
                    {#if tab.id === 'settings'}{@render settingsIcon()}{/if}
                    {#if tab.id === 'session'}{@render sessionIcon()}{/if}
                    <span>{tab.title}{tab.count !== undefined ? ` (${tab.count})` : ''}</span>
                </button>
            {/each}
        </div>
        {#if activeTab === 'session' && configState.mailer.enabled}
            <aside
                class="relative flex shrink-0 flex-row items-center gap-x-2"
                aria-label="Email session ADIF"
            >
                <SessionEmailControls />
            </aside>
        {/if}
    </div>

    {#if activeTab === 'occupancy'}
        <div id="ft8panel-occupancy" role="tabpanel" aria-labelledby="ft8tab-occupancy">
            <Ft8OccupancyPanel />
        </div>
    {:else if activeTab === 'ladder'}
        <div id="ft8panel-ladder" role="tabpanel" aria-labelledby="ft8tab-ladder">
            <Ft8MsgPanel />
        </div>
    {:else if activeTab === 'settings'}
        <div id="ft8panel-settings" role="tabpanel" aria-labelledby="ft8tab-settings">
            <Ft8SettingsPanel />
        </div>
    {:else if activeTab === 'session'}
        <div id="ft8panel-session" role="tabpanel" aria-labelledby="ft8tab-session">
            <SessionPanel />
        </div>
    {/if}
</div>
