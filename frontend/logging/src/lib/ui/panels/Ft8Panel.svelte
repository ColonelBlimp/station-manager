<script lang="ts">
    import { onMount, onDestroy } from 'svelte';
    import Button from '../components/Button.svelte';
    import Ft8OccupancyPanel from './Ft8OccupancyPanel.svelte';
    import Ft8MsgPanel from './Ft8MsgPanel.svelte';
    import Ft8SettingsPanel from './Ft8SettingsPanel.svelte';
    import { ft8State, startFt8, stopFt8, type DecodeEntry } from '../../states/ft8.svelte';
    import { ft8EnrichState, type Ft8CallInfo } from '../../states/ft8Enrich.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { parseCqCall, parseCq } from '../../utils/ft8Message';
    import { startFt8Qso } from '../../api/ft8qso';
    import { toasts } from '../../states/toasts.svelte';
    import { frequencyToBand } from '../../utils/frequency';
    import { formatUtcClock } from '../../utils/time';

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

    // Drive flag + worked-before lookups for the CQ stations on screen. Runs once
    // per slot (when the decode list changes) and on a band change (new keys);
    // observe() dedupes, so re-scanning the visible rows costs nothing after the
    // first sighting of each call. Only CQ lines are enriched (one unambiguous
    // callsign); reply/report lines are left plain for now.
    $effect(() => {
        for (const d of ft8State.decodes) {
            const call = parseCqCall(d.text);
            if (call) ft8EnrichState.observe(call, band);
        }
    });

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
    const canAnswer = $derived(
        ft8State.tx.armed && ft8State.selectedOffset !== null && !ft8State.qso.active
    );

    async function answerCq(d: DecodeEntry): Promise<void> {
        const cq = parseCq(d.text);
        if (!cq || !canAnswer || ft8State.selectedOffset === null) return;
        // opFreq is the dial frequency in Hz (selected VFO); the daemon logs the
        // QSO at dial + audio offset, so it needs the dial freq in MHz.
        const out = await startFt8Qso(
            cq.call,
            cq.grid,
            d.startUtc,
            ft8State.selectedOffset,
            opFreq / 1_000_000
        );
        if (out.kind !== 'ok') toasts.error(out.message);
    }

    // Slot label: "14:30:15 · odd · 19 busy", or a waiting state before the first
    // event. start_utc is RFC3339 UTC; render its UTC wall-clock with seconds so
    // the four 15 s slots per minute are distinguishable.
    const slotLabel = $derived.by(() => {
        if (!ft8State.slot) return 'Waiting for slot…';
        const clock = formatUtcClock(new Date(ft8State.slot.start_utc));
        return `${clock} · ${ft8State.slot.period} · ${ft8State.busyCount} busy`;
    });

    // The daemon sends `suggested` best-first by rank. For display we sort a copy
    // ascending by frequency so the chips read left-to-right like a band, and mark
    // the daemon's #1 (suggested[0], pre-sort) with a ★. The rank order stays on
    // the wire for step (e)'s "give me the best slot". Colour-fill is reserved for
    // the future selected-offset state so it doesn't clash with this recommendation
    // marker.
    const topPick = $derived(ft8State.suggested[0] ?? null);
    const sortedOffsets = $derived([...ft8State.suggested].sort((a, b) => a - b));

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

    // ---- Lower-section tabs (same pattern + .tab-item class as InfoPanel) ----
    type Ft8TabId = 'occupancy' | 'ladder' | 'settings';
    const tabs: { id: Ft8TabId; title: string }[] = [
        { id: 'occupancy', title: 'Occupancy' },
        { id: 'ladder', title: 'Operate' },
        { id: 'settings', title: 'Settings' },
    ];
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
    {@const cqCall = parseCqCall(d.text)}
    {@const info = cqCall ? ft8EnrichState.info(cqCall, band) : undefined}
    {@const answerable = cqCall !== null && canAnswer}
    <li class="flex gap-2 whitespace-nowrap">
        <span class="text-gray-400">{formatUtcClock(new Date(d.startUtc))}</span>
        <span class="w-7 text-right text-gray-500">{formatSnr(d.snr)}</span>
        <span class="w-10 text-right text-gray-500">{Math.round(d.freqHz)}</span>
        {#if info?.flag}
            <span class="cursor-default" title={info.country} aria-hidden="true">{info.flag}</span>
        {/if}
        {#if answerable}
            <button
                type="button"
                class="truncate text-left text-gray-700 cursor-pointer hover:underline"
                style:color={rowColor(info)}
                title={`Answer ${cqCall}`}
                onclick={() => answerCq(d)}>{d.text}</button
            >
        {:else}
            <span class="truncate text-gray-700" style:color={rowColor(info)}>{d.text}</span>
        {/if}
    </li>
{/snippet}

<div class="flex justify-center h-80 text-gray-500 space-x-3">
    <div class="flex flex-col text-center">
        <h2 class="text-base font-semibold my-2">Main Freq</h2>
        <div class="flex flex-col place-items-center px-2 space-y-1">
            <div class="flex gap-1">
                <Button id="160m" label="160m" />
                <Button id="80m" label="80m" />
            </div>
            <div class="flex gap-1">
                <Button id="60m" label="60m" />
                <Button id="40m" label="40m" />
            </div>
            <div class="flex gap-1">
                <Button id="30m" label="30m" />
                <Button id="20m" label="20m" />
            </div>
            <div class="flex gap-1">
                <Button id="18m" label="18m" />
                <Button id="15m" label="15m" />
            </div>
            <div class="flex gap-1">
                <Button id="12m" label="12m" />
                <Button id="10m" label="10m" />
            </div>
            <div class="flex gap-1">
                <Button id="6m" label="6m" />
            </div>
        </div>
    </div>
    <div class="flex flex-col text-center ft8-panel-width">
        <h2 class="text-base font-semibold my-2">Band Activity</h2>
        <div
            class="flex ft8-panel-height flex-col rounded border border-gray-300 overflow-y-scroll"
        >
            {#if ft8State.decodes.length > 0}
                <ul class="flex-1 space-y-0.5 px-2 py-1 text-left font-mono text-xs">
                    {#each ft8State.decodes as d (d.id)}
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
        <div
            class="flex ft8-panel-height flex-col rounded border border-gray-300 overflow-y-scroll"
        >
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
        <div class="mt-0.5 text-gray-700 text-xs">{rxCaption}</div>
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
<div class="text-center text-gray-700 text-sm font-semibold pt-2 pb-1">
    Next slot in {secondsToNextSlot}s · {nextSlotParity}
</div>
<!--
    Tabbed lower section (same tablist pattern + .tab-item class as InfoPanel):
      - Occupancy — the TX-offset picker strip (Ft8OccupancyPanel)
      - Operate   — the FT8 transmit surface + message ladder (Ft8MsgPanel)
      - Settings  — FT8 display preferences (Ft8SettingsPanel), daemon-backed
    WAI-ARIA tabs contract mirrors InfoPanel: roving tabindex, arrow/Home/End
    navigation with auto-activation.
-->
<div class="flex flex-col w-full px-6 pt-4">
    <div class="flex flex-row items-center border-b border-gray-400 pb-2">
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
                    <span>{tab.title}</span>
                </button>
            {/each}
        </div>
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
    {/if}
</div>
