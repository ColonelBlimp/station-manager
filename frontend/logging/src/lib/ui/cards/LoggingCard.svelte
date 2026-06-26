<script lang="ts">
    import SessionTimer from '../components/SessionTimer.svelte';
    import PhonePanel from '../panels/PhonePanel.svelte';
    import Ft8Panel from '../panels/Ft8Panel.svelte';
    import { configState } from '../../states/config.svelte';
    import {
        snapshotOperatingState,
        restoreOperatingState,
        type OperatingSnapshot,
    } from '../../actions/rigControl';

    const countFormatter = new Intl.NumberFormat();
    const formattedCount = $derived(countFormatter.format(configState.defaultLogbook.qsoCount));

    // Operating-mode switch (header <select>): "phone" renders the QSO / Country
    // / Info logging layout; "ft8" renders the FT8 panel. Persisted to
    // localStorage so the chosen panel survives a page reload (per-device UI
    // preference — same tier as qsoDefaults).
    const OPERATING_MODE_KEY = 'sm.operatingMode';

    function loadOperatingMode(): 'phone' | 'ft8' {
        try {
            const raw = localStorage.getItem(OPERATING_MODE_KEY);
            if (raw === 'phone' || raw === 'ft8') return raw;
        } catch {
            // localStorage unavailable — fall through to the default.
        }
        return 'phone';
    }

    let operatingMode = $state<'phone' | 'ft8'>(loadOperatingMode());

    // Operating-state memory across a mode switch — BOTH ways. Tuning in either mode
    // changes the rig's freq/mode (or manualState when CAT-off); without this a switch
    // leaves you parked on the other mode's frequencies. On every switch we snapshot
    // the OUTGOING mode's operating state (VFO freqs/mode/selection) and restore the
    // INCOMING mode's last snapshot — so phone→ft8 returns to your last FT8 band/freq
    // and ft8→phone to your last Phone/CW. Restore re-tunes a live rig (gated by the
    // config knob restoreRigOnModeSwitch) or rewrites manualState (CAT-off, always).
    // First switch into a mode has no snapshot yet → nothing to restore (as before).
    // In-memory only — snapshots are NOT persisted, so a reload mid-mode won't trigger
    // a surprise re-tune. prevOperatingMode/snapshots are plain (non-$state) so updating
    // them inside the effect doesn't re-trigger it; seed prevOperatingMode from the
    // loader (not the $state var) so the first $effect run is a no-op transition.
    let prevOperatingMode: 'phone' | 'ft8' = loadOperatingMode();
    const snapshots: Record<'phone' | 'ft8', OperatingSnapshot | null> = { phone: null, ft8: null };

    $effect(() => {
        const mode = operatingMode;
        if (mode !== prevOperatingMode) {
            snapshots[prevOperatingMode] = snapshotOperatingState();
            const incoming = snapshots[mode];
            if (incoming) restoreOperatingState(incoming);
            prevOperatingMode = mode;
        }
        try {
            localStorage.setItem(OPERATING_MODE_KEY, mode);
        } catch {
            // localStorage unavailable — persistence is best-effort.
        }
    });
</script>

<!--
    h-13.5 / w-70 are one-off shell dimensions — they exist only
    here and don't anchor a relationship that warrants a design token.
    If a second header strip appears with the same dimensions, lift
    these to tokens at that point.
-->
<header class="flex items-center h-13.5 px-4 border-b border-b-line-soft">
    <div class="flex flex-row items-center w-72 cursor-default">
        <h1 class="text-lg font-bold tracking-tight">Operating Mode:</h1>
        <div class="grid grid-cols-1 w-30 ml-2 mt-0.5">
            <select
                bind:value={operatingMode}
                class="text-sm col-start-1 row-start-1 w-full appearance-none rounded-md bg-surface py-1 pr-8 pl-3 outline-1 -outline-offset-1 outline-line focus:outline-2 focus:-outline-offset-2 focus:outline-focus disabled:bg-surface-disabled disabled:cursor-not-allowed"
            >
                <option value="phone">Phone/CW</option>
                <option value="ft8">FT8</option>
            </select>
            <svg
                class="pointer-events-none col-start-1 row-start-1 mr-2 size-5 self-center justify-self-end text-gray-500 sm:size-4"
                viewBox="0 0 16 16"
                fill="currentColor"
                aria-hidden="true"
                data-slot="icon"
            >
                <path
                    fill-rule="evenodd"
                    d="M4.22 6.22a.75.75 0 0 1 1.06 0L8 8.94l2.72-2.72a.75.75 0 1 1 1.06 1.06l-3.25 3.25a.75.75 0 0 1-1.06 0L4.22 7.28a.75.75 0 0 1 0-1.06Z"
                    clip-rule="evenodd"
                />
            </svg>
        </div>
    </div>
    <div class="w-36 text-xs"></div>
    <div class="w-32 text-xs"></div>
    <div class="flex flex-col text-xs font-semibold w-52 text-nowrap cursor-default">
        <div class="flex flex-row items-center">
            <div class="flex-none w-15">Logbook:</div>
            <div class="text-ellipsis overflow-hidden text-green-800">
                {configState.defaultLogbook.name} ({formattedCount})
            </div>
        </div>
        <div class="flex flex-row items-center">
            <div class="flex-none w-15">Rig:</div>
            <div
                class="text-ellipsis overflow-hidden {configState.station.rigName
                    ? 'text-green-800'
                    : 'text-gray-400 italic'}"
            >
                {configState.station.rigName || 'not set'}
            </div>
        </div>
    </div>
    <div class="flex text-sm font-semibold w-46 cursor-default">
        <div class="w-24">Session Time:</div>
        <div class="text-right w-17"><SessionTimer /></div>
    </div>
</header>
{#if operatingMode === 'ft8'}
    <Ft8Panel />
{:else}
    <PhonePanel />
{/if}
