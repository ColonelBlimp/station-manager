<script lang="ts">
    // Rig panel — the operating context (band / mode / freq) + CAT link status.
    // CAT-off: the fields are manual entry into the shared rig state. CAT-
    // connected: the rig owns them (rig-state SSE via catLink), so they lock.
    // Pure presentation over rig.svelte; fills whatever host it's given
    // (ADR 0045).
    import {
        rig,
        rigCaps,
        rigGate,
        confirmRig,
        toggleTune,
        selectVfo,
        setMode,
        selectBand,
        operatingBands,
        hasOp,
        type RigWriteResult,
    } from './rig.svelte';
    import { hideTile } from './layout.svelte';
    import { focusCallsign } from './state.svelte';
    import { toasts } from '../ui/toasts.svelte';
    import { formatFrequency, frequencyToBand } from '../utils/frequency';
    import { parseFrequency } from '../validators/frequency';

    // The rig card is shared across modes; the mode-specific behaviour arrives via
    // props. pickBand: Phone/CW passes selectBand (SM is passive — the rig restores
    // that band's freq+mode from its own band-stack); FT8 passes the watering-hole
    // pick (set_freq of ft8_frequencies). requiresCat: FT8 sets it because FT8 can't
    // run without CAT — see catMissing below. Defaults keep the Phone/CW tile —
    // rendered prop-less on the Operate surface — unchanged.
    interface Props {
        pickBand?: (band: string) => Promise<RigWriteResult>;
        requiresCat?: boolean;
    }
    let { pickBand = selectBand, requiresCat = false }: Props = $props();

    // Operator-friendly mode names (sidebands, not families — matches the
    // shipping SPA's baseModes). resolveModeAndSubmode maps them to canonical
    // ADIF (MODE, SUBMODE) at submit time; the daemon rejects e.g. MODE=USB.
    const MODES = ['USB', 'LSB', 'CW', 'FM', 'AM', 'RTTY', 'FT8', 'FT4', 'PSK31'];
    const VFOS = ['A', 'B'] as const;

    const locked = $derived(rig.cat === 'connected');

    // FT8 cannot operate without CAT (capture is gated daemon-side on the rig
    // being connected), so the manual-entry/confirm path is a dead end there.
    // The FT8 host sets requiresCat: with the rig away, every control disables
    // and the card says why — the only unblock is the rig coming online, so
    // offering a Confirm would promise something it can't deliver.
    const catMissing = $derived(requiresCat && !locked);

    // A CAT-pushed mode can fall outside the manual nine (an unmapped rig
    // literal passes through raw); a select with a value not among its options
    // renders blank, so the current value always joins the list.
    const modeOptions = $derived(MODES.includes(rig.mode) ? MODES : [...MODES, rig.mode]);

    // The bands the grid offers = the operator's configured operating_bands (or
    // the HF..6m default when unset). The CAT-pushed band can fall outside that
    // list (rig on a band the operator didn't configure); join the current value
    // so the active band always shows + can be highlighted.
    const bandOptions = $derived.by(() => {
        const list = operatingBands();
        return list.includes(rig.band) ? list : [...list, rig.band];
    });

    // Live mode dropdown (Option A): the rig's OWN literals (rigCaps.rigModes,
    // e.g. LSB/USB/CW-U/DATA-U). Join the current literal if the caps list
    // somehow omits it, so the select never renders blank.
    const liveModeOptions = $derived(
        rig.modeLiteral === '' || rigCaps.rigModes.includes(rig.modeLiteral)
            ? rigCaps.rigModes
            : [...rigCaps.rigModes, rig.modeLiteral]
    );

    // Band follows the frequency (IARU allocations) so the two can't disagree
    // on the logged QSO; the select stays usable for an out-of-band/odd value.
    // parseFrequency accepts the dot-grouped display form AND decimal MHz.
    function syncBand(): void {
        const hz = parseFrequency(rig.freq);
        if (hz === null) return;
        const band = frequencyToBand(hz);
        if (band !== '') rig.band = band;
    }

    // Pick a band directly (button grid — no stepping). The band+freq behaviour
    // (off-CAT default-freq jump vs live set_band) lives in the shared selectBand
    // action, so the grid and the Ctrl+Shift+digit keyboard jump behave the same.
    async function onPickBand(band: string): Promise<void> {
        const r = await pickBand(band);
        if (!r.ok) toasts.error(r.message);
    }

    // #2 (region-AGNOSTIC) — the typed freq doesn't fall in the SELECTED band's
    // ADIF widest envelope (`frequencyToBand`, no region data). Catches the gross
    // band/freq mismatch (e.g. 40m selected, 14.2 MHz typed) + out-of-any-band
    // typos. TX-legality per IARU region + national band plan is the separate
    // band-plan feature. CAT-live: the rig owns the freq, so never flagged.
    const freqOutOfBand = $derived.by(() => {
        if (locked || rig.freq.trim() === '') return false;
        const hz = parseFrequency(rig.freq);
        if (hz === null) return false; // malformed, not an out-of-band case
        return frequencyToBand(hz) !== rig.band;
    });

    // Tune carrier (ADR 0027) — daemon-owned; the SPA sends an intent and the
    // button reflects rig.tuneActive (confirm-by-push). Shown only when the rig
    // supports tune (rigCaps.tune); enabled only when CAT is live (tuning a
    // non-live rig is impossible — the daemon refuses). A failed toggle toasts;
    // the on/off state still comes from the tune-state SSE, not this call.
    async function onTune(): Promise<void> {
        // F-04 confirm-by-push: a timed-out tune is outcome-unknown, not a failure.
        // Silent on a success (accepted/observed/alreadySatisfied/superseded), a
        // WARN on unknown, a definite ERROR only on a real failure.
        const r = await toggleTune();
        if (r.status === 'unknown') toasts.warn(r.message);
        else if (r.status === 'failed') toasts.error(r.message);
    }

    // Clickable VFO boxes (ADR 0026): a rig with select_vfo (FTdx10 VS) truly
    // SELECTS — the boxes keep their contents and operation moves; one with
    // only swap_vfo (SV;) swaps contents ("select the other" == swap). The
    // title says which, because they read very differently at the dial. Boxes
    // stay plain read-outs when the rig is not live or has neither op.
    const canSelect = $derived(locked && hasOp('select_vfo'));
    const canSwap = $derived(locked && (canSelect || hasOp('swap_vfo')));

    async function onSelectVfo(v: 'A' | 'B'): Promise<void> {
        const r = await selectVfo(v);
        if (!r.ok) toasts.error(r.message);
    }

    // Live mode pick (CAT connected) → drive the rig with the chosen literal;
    // confirm-by-push (optimistic in setMode) repaints. Manual mode uses a plain
    // bind:value select (no command).
    async function onModeSelect(e: Event): Promise<void> {
        const r = await setMode((e.currentTarget as HTMLSelectElement).value);
        if (!r.ok) toasts.error(r.message);
    }
</script>

<div class="card w-2xl">
    <!-- Self-contained tile header (ADR 0045/0046): title · rig name · CAT gate
         pill — the glance/status that used to live on the InfoPanel wrapper. -->
    <div class="mb-3 flex items-center gap-x-3">
        <h3 class="text-sm font-semibold text-ink">Rig Control</h3>
        {#if rig.identity !== ''}
            <span class="text-sm text-muted">{rig.identity}</span>
        {/if}
        <span
            class="flex items-center gap-x-1.5 rounded-full bg-surface-muted px-2.5 py-1 text-xs font-medium text-ink"
        >
            <span
                class="size-2 rounded-full"
                class:bg-green-500={rigGate() === 'live'}
                class:bg-gray-400={rigGate() === 'manual' && !catMissing}
                class:bg-amber-500={rigGate() === 'unconfirmed' && !catMissing}
                class:bg-red-500={rigGate() === 'lost' || catMissing}
            ></span>
            {rigGate() === 'live'
                ? 'CAT connected'
                : rigGate() === 'lost'
                  ? 'CAT link lost'
                  : catMissing
                    ? 'CAT required'
                    : rigGate() === 'manual'
                      ? 'Manual — confirmed'
                      : 'Manual — confirm to log'}
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

    <!-- Band — a button-per-band grid (direct jump, no stepping). The active
         band highlight follows rig.band: off-CAT it moves at once; on a live
         set_band it updates when the rig confirms (confirm-by-push), so no
         snap-back. Live → set_band (rig restores that band's freq + mode from
         its own stack); manual → set band + default freq. -->
    <div class="mb-4">
        <span class="mb-1 block text-sm font-medium text-ink">Band</span>
        <div class="flex flex-wrap gap-1">
            {#each bandOptions as b (b)}
                <button
                    type="button"
                    class="min-w-11 cursor-pointer rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 {rig.band ===
                    b
                        ? 'border-focus bg-focus text-white'
                        : 'border-line text-ink hover:bg-surface-muted'}"
                    aria-pressed={rig.band === b}
                    disabled={catMissing}
                    onclick={() => onPickBand(b)}
                >
                    {b}
                </button>
            {/each}
        </div>
    </div>

    <div class="flex items-end gap-x-4">
        <div>
            {#if locked}
                <!-- Live: Option A — the rig's OWN mode literals; a pick sends
                     set_mode with the literal (one-way value; setMode is
                     optimistic so there's no snap-back on the push). -->
                <span class="block text-sm font-medium text-ink">Mode</span>
                <select
                    class="input w-24"
                    disabled={!hasOp('set_mode')}
                    value={rig.modeLiteral}
                    onchange={onModeSelect}
                >
                    {#each liveModeOptions as m (m)}
                        <option value={m}>{m}</option>
                    {/each}
                </select>
            {:else}
                <label for="rp-mode" class="block text-sm font-medium text-ink">Mode</label>
                <select id="rp-mode" class="input w-24" disabled={catMissing} bind:value={rig.mode}>
                    {#each modeOptions as m (m)}
                        <option value={m}>{m}</option>
                    {/each}
                </select>
            {/if}
        </div>
        {#if locked}
            <!-- CAT-locked: both VFOs, SM dot-grouped display (14.199.950 —
             matches the rig + the shipping VFO box), the selected one dotted.
             When the rig exposes swap_vfo, each box is a button: clicking the
             non-selected one swaps A↔B (ADR 0026); otherwise it's a read-out. -->
            {#each VFOS as v (v)}
                {@const hz = v === 'A' ? rig.vfoA : rig.vfoB}
                {@const shown = hz === null ? '—' : formatFrequency(hz)}
                <div>
                    <span class="flex items-center gap-x-1.5 text-sm font-medium text-ink">
                        VFO-{v}
                        {#if rig.selectedVfo === v}
                            <span class="size-2 rounded-full bg-green-500" title="Selected"></span>
                        {/if}
                    </span>
                    {#if canSwap}
                        <button
                            type="button"
                            class="input w-32 cursor-pointer text-left tabular-nums hover:outline-focus"
                            aria-pressed={rig.selectedVfo === v}
                            onclick={() => onSelectVfo(v)}
                            title={rig.selectedVfo === v
                                ? 'Selected VFO'
                                : canSelect
                                  ? 'Click to select this VFO'
                                  : 'Click to swap onto this VFO'}
                        >
                            {shown}
                        </button>
                    {:else}
                        <input class="input w-32 tabular-nums" disabled value={shown} />
                    {/if}
                </div>
            {/each}
        {:else}
            <div>
                <label for="rp-freq" class="block text-sm font-medium text-ink"
                    >Frequency (MHz)</label
                >
                <input
                    id="rp-freq"
                    class="input w-32 tabular-nums"
                    class:input-error={freqOutOfBand}
                    autocomplete="off"
                    spellcheck="false"
                    placeholder="14.255.000"
                    disabled={catMissing}
                    bind:value={rig.freq}
                    oninput={syncBand}
                />
            </div>
        {/if}

        {#if rigCaps.tune}
            <!-- Tune carrier: bottom-aligned on the input baseline. Active goes
             solid red + pulses so a keyed carrier is unmistakable. Click-only
             (no shortcut) — starting a transmission is a deliberate action. -->
            <div class="flex flex-col justify-end">
                <button
                    type="button"
                    class="btn text-sm {rig.tuneActive
                        ? 'border-red-600 bg-red-600 text-white hover:bg-red-700 animate-pulse'
                        : ''}"
                    disabled={rig.cat !== 'connected'}
                    aria-pressed={rig.tuneActive}
                    onclick={onTune}
                    title={rig.tuneActive
                        ? 'Transmitting tune carrier — click to stop'
                        : rig.cat === 'connected'
                          ? 'Key a reduced-power tune carrier to tune the amp'
                          : 'Tune needs a live CAT connection'}
                >
                    {rig.tuneActive ? 'Tuning…' : 'Tune'}
                </button>
            </div>
        {/if}
    </div>

    <!-- Validation messages sit in their own row BELOW the inputs, so the error
         text can't push the freq field out of the items-end row alignment. -->
    {#if freqOutOfBand}
        <p class="mt-1 text-xs text-invalid">Outside the {rig.band} band</p>
    {/if}

    <!-- Gate affordances at the card's bottom-right: message ABOVE the button.
         Link status is in the tile header (title · rig name · CAT pill). -->
    {#if catMissing || rigGate() === 'unconfirmed' || rigGate() === 'lost' || rig.linkError !== ''}
        <div class="mt-4 flex flex-col items-end gap-y-1">
            {#if rig.linkError !== ''}
                <p class="max-w-56 text-right text-xs text-invalid">Bridge: {rig.linkError}</p>
            {/if}
            {#if catMissing}
                <p class="max-w-56 text-right text-xs text-muted">
                    FT8 needs a live CAT connection — controls are disabled until the rig connects.
                </p>
            {:else if rigGate() === 'unconfirmed' || rigGate() === 'lost'}
                <!-- Confirm (ADR 0044): the operator asserts the QSO settings
                     (band / mode / freq) are right — once per band per session.
                     Changing band does NOT move the freq, so this confirms the
                     whole setting, not just the band. On 'lost' it also takes
                     manual ownership (keeps the last rig values); a returning rig
                     auto-lifts either way. -->
                <p class="max-w-56 text-right text-xs text-muted">
                    Logging is blocked until the QSO settings are confirmed.
                </p>
                <button class="btn btn-primary text-xs" onclick={confirmRig}>
                    {rigGate() === 'lost' ? 'Go manual — confirm' : 'Confirm'}
                </button>
            {/if}
        </div>
    {/if}
</div>
