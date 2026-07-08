<script lang="ts">
    // Rig panel — the operating context (band / mode / freq) + CAT link status.
    // CAT-off: the fields are manual entry into the shared rig state. CAT-
    // connected: the rig owns them (rig-state SSE via catLink), so they lock.
    // Pure presentation over rig.svelte; fills whatever host it's given
    // (ADR 0045).
    import { rig, rigGate, confirmRig } from './rig.svelte';
    import { hideTile } from './layout.svelte';
    import { focusCallsign } from './state.svelte';
    import { formatFrequency, frequencyToBand } from '../utils/frequency';
    import { parseFrequency } from '../validators/frequency';

    const BANDS = ['160m', '80m', '60m', '40m', '30m', '20m', '17m', '15m', '12m', '10m', '6m'];
    // Operator-friendly mode names (sidebands, not families — matches the
    // shipping SPA's baseModes). resolveModeAndSubmode maps them to canonical
    // ADIF (MODE, SUBMODE) at submit time; the daemon rejects e.g. MODE=USB.
    const MODES = ['USB', 'LSB', 'CW', 'FM', 'AM', 'RTTY', 'FT8', 'FT4', 'PSK31'];
    const VFOS = ['A', 'B'] as const;

    const locked = $derived(rig.cat === 'connected');

    // A CAT-pushed mode can fall outside the manual nine (an unmapped rig
    // literal passes through raw); a select with a value not among its options
    // renders blank, so the current value always joins the list.
    const modeOptions = $derived(MODES.includes(rig.mode) ? MODES : [...MODES, rig.mode]);

    // Band follows the frequency (IARU allocations) so the two can't disagree
    // on the logged QSO; the select stays usable for an out-of-band/odd value.
    // parseFrequency accepts the dot-grouped display form AND decimal MHz.
    function syncBand(): void {
        const hz = parseFrequency(rig.freq);
        if (hz === null) return;
        const band = frequencyToBand(hz);
        if (band !== '') rig.band = band;
    }
</script>

<div class="card w-2xl">
    <!-- Self-contained tile header (ADR 0045/0046): title · rig name · CAT gate
         pill — the glance/status that used to live on the InfoPanel wrapper. -->
    <div class="mb-3 flex items-center gap-x-3">
        <h3 class="text-sm font-semibold text-ink">Rig</h3>
        {#if rig.identity !== ''}
            <span class="text-sm text-muted">{rig.identity}</span>
        {/if}
        <span
            class="flex items-center gap-x-1.5 rounded-full bg-surface-muted px-2.5 py-1 text-xs font-medium text-ink"
        >
            <span
                class="size-2 rounded-full"
                class:bg-green-500={rigGate() === 'live'}
                class:bg-gray-400={rigGate() === 'manual'}
                class:bg-amber-500={rigGate() === 'unconfirmed'}
                class:bg-red-500={rigGate() === 'lost'}
            ></span>
            {rigGate() === 'live'
                ? 'CAT connected'
                : rigGate() === 'manual'
                  ? 'Manual — confirmed'
                  : rigGate() === 'unconfirmed'
                    ? 'Manual — confirm to log'
                    : 'CAT link lost'}
        </span>
        <button
            class="ml-auto cursor-pointer rounded-md text-muted hover:text-ink"
            title="Hide"
            aria-label="Hide Rig"
            onclick={() => {
                hideTile('rig');
                focusCallsign();
            }}
        >
            <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="1.5"
                aria-hidden="true"
                class="size-5"
            >
                <path d="M6 18 18 6M6 6l12 12" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
        </button>
    </div>

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
            {#each modeOptions as m (m)}
                <option value={m}>{m}</option>
            {/each}
        </select>
    </div>
    {#if locked}
        <!-- CAT-locked: both VFOs, SM dot-grouped display (14.199.950 —
             matches the rig + the shipping VFO box), the selected one dotted.
             Read-outs only: click-to-select/swap needs the inbound command
             path (ADR 0026) and lands with rig control. -->
        {#each VFOS as v (v)}
            {@const hz = v === 'A' ? rig.vfoA : rig.vfoB}
            <div>
                <label
                    for={`rp-vfo-${v}`}
                    class="flex items-center gap-x-1.5 text-sm font-medium text-ink"
                >
                    VFO-{v}
                    {#if rig.selectedVfo === v}
                        <span class="size-2 rounded-full bg-green-500" title="Selected"></span>
                    {/if}
                </label>
                <input
                    id={`rp-vfo-${v}`}
                    class="input w-32 tabular-nums"
                    disabled
                    value={hz === null ? '—' : formatFrequency(hz)}
                />
            </div>
        {/each}
    {:else}
        <div>
            <label for="rp-freq" class="block text-sm font-medium text-ink">Frequency (MHz)</label>
            <input
                id="rp-freq"
                class="input w-32 tabular-nums"
                autocomplete="off"
                spellcheck="false"
                placeholder="14.255.000"
                bind:value={rig.freq}
                oninput={syncBand}
            />
        </div>
    {/if}

    <!-- Link status is in this tile's own header (title · rig name · CAT pill);
         only the gate affordances render here. -->
    <div class="ml-auto flex flex-col items-end gap-y-1">
        {#if rigGate() === 'unconfirmed' || rigGate() === 'lost'}
            <!-- Set/Confirm (ADR 0044): the operator asserts the values are
                 right — once per band per session. On 'lost' it also takes
                 manual ownership (keeps the last rig values); a returning
                 rig auto-lifts either way. -->
            <button class="btn btn-primary text-xs" onclick={confirmRig}>
                {rigGate() === 'lost' ? 'Go manual — confirm' : 'Confirm'}
            </button>
            <p class="max-w-48 text-right text-xs text-muted">
                Logging is blocked until the band is confirmed.
            </p>
        {/if}
        {#if rig.linkError !== ''}
            <p class="max-w-56 text-right text-xs text-invalid">Bridge: {rig.linkError}</p>
        {/if}
    </div>
    </div>
</div>
