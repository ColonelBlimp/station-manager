<script lang="ts">
    // Rig panel — the operating context (band / mode / freq) + CAT link status.
    // CAT-off: the fields are manual entry into the shared rig state. CAT-
    // connected: the rig owns them (SSE-driven later), so they lock. Pure
    // presentation over rig.svelte; fills whatever host it's given (ADR 0045).
    import { rig } from './rig.svelte';
    import { frequencyToBand } from '../utils/frequency';

    const BANDS = ['160m', '80m', '60m', '40m', '30m', '20m', '17m', '15m', '12m', '10m', '6m'];
    const MODES = ['SSB', 'CW', 'AM', 'FM'];

    const locked = $derived(rig.cat === 'connected');

    // Band follows the frequency (IARU allocations) so the two can't disagree
    // on the logged QSO; the select stays usable for an out-of-band/odd value.
    function syncBand(): void {
        const mhz = Number.parseFloat(rig.freq);
        if (!Number.isFinite(mhz)) return;
        const band = frequencyToBand(Math.round(mhz * 1_000_000));
        if (band !== '') rig.band = band;
    }
</script>

<div class="flex items-end gap-x-4">
    <div>
        <label for="rp-band" class="block text-sm font-medium text-ink">Band</label>
        <select id="rp-band" class="input w-24" disabled={locked} bind:value={rig.band}>
            {#each BANDS as b (b)}
                <option value={b}>{b}</option>
            {/each}
        </select>
    </div>
    <div>
        <label for="rp-mode" class="block text-sm font-medium text-ink">Mode</label>
        <select id="rp-mode" class="input w-24" disabled={locked} bind:value={rig.mode}>
            {#each MODES as m (m)}
                <option value={m}>{m}</option>
            {/each}
        </select>
    </div>
    <div>
        <label for="rp-freq" class="block text-sm font-medium text-ink">Frequency (MHz)</label>
        <input
            id="rp-freq"
            class="input w-32"
            autocomplete="off"
            spellcheck="false"
            placeholder="14.255"
            disabled={locked}
            bind:value={rig.freq}
            oninput={syncBand}
        />
    </div>

    <div class="ml-auto flex flex-col items-end gap-y-1">
        <span
            class="flex items-center gap-x-1.5 rounded-full bg-surface-muted px-2.5 py-1 text-xs font-medium text-ink"
        >
            <span
                class="size-2 rounded-full"
                class:bg-gray-400={rig.cat === 'off'}
                class:bg-green-500={rig.cat === 'connected'}
                class:bg-red-500={rig.cat === 'lost'}
            ></span>
            {rig.cat === 'off'
                ? 'CAT off — manual entry'
                : rig.cat === 'connected'
                  ? 'CAT connected'
                  : 'CAT link lost'}
        </span>
        <!-- DEV sim — stands in for the bridge's rig-state/rig-disconnected SSE
             until the /v1 wiring; remove with the stub layer. -->
        <label class="flex items-center gap-x-1 text-xs text-muted">
            sim
            <select class="input h-6 w-28 py-0 text-xs" bind:value={rig.cat}>
                <option value="off">off</option>
                <option value="connected">connected</option>
                <option value="lost">lost</option>
            </select>
        </label>
    </div>
</div>
