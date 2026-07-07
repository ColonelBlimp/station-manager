<script lang="ts">
    // Details — the deliberate, rarely-touched per-QSO fields kept off the
    // fast-path logging card (QTH; gridsquare is enrichment-filled and only
    // corrected here). Pure presentation over the shared draft: edits write
    // straight into it and log with the QSO. Fills whatever host it's given
    // (ADR 0045) — today the InfoPanel below the logging card.
    import { draft } from './qso.svelte';
    import { isValidMaidenhead } from '../validators/maidenhead';

    function upperGrid(): void {
        draft.gridsquare = draft.gridsquare.toUpperCase();
    }

    const gridInvalid = $derived(isValidMaidenhead(draft.gridsquare) !== null);
</script>

<div class="flex items-start gap-x-4">
    <div>
        <label for="dp-qth" class="block text-sm font-medium text-ink">QTH</label>
        <input id="dp-qth" class="input w-60" autocomplete="off" bind:value={draft.qth} />
    </div>
    <div>
        <label for="dp-grid" class="block text-sm font-medium text-ink">Gridsquare</label>
        <input
            id="dp-grid"
            class="input w-32 uppercase"
            autocomplete="off"
            spellcheck="false"
            placeholder="e.g. KH66"
            bind:value={draft.gridsquare}
            oninput={upperGrid}
        />
        {#if gridInvalid}
            <p class="mt-1 text-xs text-invalid">Not a valid grid square</p>
        {:else}
            <p class="mt-1 text-xs text-muted">Filled by enrichment — correct it here</p>
        {/if}
    </div>
</div>
