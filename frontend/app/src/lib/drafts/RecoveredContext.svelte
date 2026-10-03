<script lang="ts">
    import { untrack } from 'svelte';
    import {
        recovered,
        correctRecoveredRig,
        confirmRecoveredRig,
        recoveredRigProblem,
    } from './recovered.svelte';
    import { parseFrequency } from '../validators/frequency';
    import { formatUtc } from './savedDraft';
    import { recoveredSave } from './recoveredSave.svelte';

    // Text remains editable even when it is incomplete or malformed. The
    // recovered numeric reading becomes missing until the input parses.
    let frequency = $state('');
    $effect(() => {
        // Initialise on a new record or remount using the current correction.
        // Typing must not retrigger this effect and erase malformed input.
        const record = recovered.record;
        const hz = record === null ? null : untrack(() => recovered.rig?.freqHz);
        frequency = hz == null ? '' : (hz / 1e6).toFixed(6);
    });
    const problem = $derived(recoveredRigProblem());
</script>

{#if recovered.record !== null && recovered.rig !== null}
    <div class="mb-3 space-y-2 rounded-md border border-line p-3 text-sm">
        <p class="font-semibold">Recovered QSO from ‘{recovered.record.archiveLabel}’</p>
        <p class="text-xs text-muted">
            {recovered.record.rig.basis === 'before-drop'
                ? 'Last reading before the connection dropped'
                : 'Reading when saved'} · {formatUtc(recovered.record.rig.capturedAt)}
        </p>
        {#if recovered.record.rigCorrection !== null}
            <p class="text-xs text-muted" data-testid="recovered-original">
                Corrected from the original reading: {recovered.record.rig.freqHz === null
                    ? 'unknown'
                    : `${(recovered.record.rig.freqHz / 1e6).toFixed(6)} MHz`} · {recovered.record
                    .rig.band || 'unknown'} · {recovered.record.rig.adifMode || 'unknown'}{recovered
                    .record.rig.subMode
                    ? ` / ${recovered.record.rig.subMode}`
                    : ''}
            </p>
        {/if}
        <div class="grid grid-cols-2 gap-2">
            <label class="block"
                >Recovered frequency (MHz)
                <input
                    class="input w-full"
                    value={frequency}
                    oninput={(e) => {
                        frequency = e.currentTarget.value;
                        correctRecoveredRig({ freqHz: parseFrequency(frequency) });
                    }}
                />
            </label>
            <label class="block"
                >Recovered band
                <input
                    class="input w-full"
                    value={recovered.rig.band}
                    oninput={(e) =>
                        correctRecoveredRig({ band: e.currentTarget.value.trim().toLowerCase() })}
                />
            </label>
            <label class="block"
                >Recovered mode
                <input
                    class="input w-full"
                    value={recovered.rig.adifMode}
                    oninput={(e) =>
                        correctRecoveredRig({
                            adifMode: e.currentTarget.value.trim().toUpperCase(),
                        })}
                />
            </label>
            <label class="block"
                >Recovered submode
                <input
                    class="input w-full"
                    value={recovered.rig.subMode}
                    oninput={(e) =>
                        correctRecoveredRig({
                            subMode: e.currentTarget.value.trim().toUpperCase(),
                        })}
                />
            </label>
        </div>
        {#if recoveredSave.error}
            <p role="alert" data-testid="recovered-save-error">{recoveredSave.error}</p>
        {/if}
        {#if recoveredSave.listError}
            <p role="status" data-testid="recovered-list-error">{recoveredSave.listError}</p>
        {/if}
        {#if recoveredSave.clearing}
            <p role="status">Saving the latest edit before clearing…</p>
        {/if}
        {#if problem !== null}
            <p role="status">{problem}</p>
        {:else if recovered.confirmed}
            <p role="status">Recovered rig values confirmed.</p>
        {:else}
            <p role="status">Confirm or correct these saved rig values before logging.</p>
        {/if}
        <button
            type="button"
            class="btn text-xs"
            disabled={problem !== null || recovered.confirmed}
            onclick={() => confirmRecoveredRig()}>Confirm recovered rig values</button
        >
    </div>
{/if}
