<script lang="ts">
    // Rigs section — the configured rig profiles (app Settings, ADR 0044), as a
    // master-detail: the rig list on the left, a details panel on the right.
    // First increment: the list is live (GET /v1/rigs) and selectable; the
    // details panel is a blank placeholder — the per-rig editor (model /
    // port / audio / serial overrides / mode mappings) lands next.
    import { onMount } from 'svelte';
    import { rigsState } from './rigs.svelte';

    onMount(() => void rigsState.load());
</script>

{#if !rigsState.loaded && rigsState.loading}
    <p class="text-sm text-muted">Loading…</p>
{:else if !rigsState.loaded && rigsState.error}
    <div class="card">
        <p class="text-sm text-ink">Couldn’t load rigs: {rigsState.error}</p>
        <button class="btn mt-3" onclick={() => rigsState.load()}>Retry</button>
    </div>
{:else if rigsState.rigs.length === 0}
    <div class="grid min-h-[40vh] place-items-center rounded-xl border border-dashed border-line">
        <p class="text-sm text-muted">No rigs configured.</p>
    </div>
{:else}
    <div class="flex gap-6">
        <!-- Master: rig list -->
        <ul class="w-64 shrink-0 space-y-1">
            {#each rigsState.rigs as rig (rig.id)}
                <li>
                    <button
                        class="w-full rounded-md border px-3 py-2 text-left transition-colors {rigsState.selectedId ===
                        rig.id
                            ? 'border-focus bg-surface-muted'
                            : 'border-line hover:bg-surface-muted'}"
                        onclick={() => rigsState.select(rig.id)}
                    >
                        <div class="flex items-center gap-2">
                            <span class="font-medium text-ink">{rig.model}</span>
                            {#if rig.id === rigsState.defaultRigId}
                                <span
                                    class="rounded bg-surface-muted px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-muted uppercase"
                                    >default</span
                                >
                            {/if}
                        </div>
                        <div class="truncate text-xs text-muted" title={rig.port}>{rig.port}</div>
                    </button>
                </li>
            {/each}
        </ul>

        <!-- Detail: blank panel (per-rig editor lands next increment) -->
        <div class="flex-1">
            {#if rigsState.selected}
                <h2 class="text-base font-semibold text-ink">{rigsState.selected.model}</h2>
                <p class="truncate text-sm text-muted" title={rigsState.selected.port}>
                    {rigsState.selected.port}
                </p>
                <div
                    class="mt-4 grid min-h-[40vh] place-items-center rounded-xl border border-dashed border-line"
                >
                    <p class="text-sm text-muted">Rig details — coming soon.</p>
                </div>
            {:else}
                <div
                    class="grid min-h-[40vh] place-items-center rounded-xl border border-dashed border-line"
                >
                    <p class="text-sm text-muted">Select a rig to view its details.</p>
                </div>
            {/if}
        </div>
    </div>
{/if}
