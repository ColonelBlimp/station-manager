<script lang="ts">
    // Rigs section — the configured rig profiles (app Settings, ADR 0044), as a
    // master-detail: the rig list on the left, a details panel on the right. The
    // list is live (GET /v1/rigs) and selectable; the detail panel shows the
    // selected rig read-only. The editable pickers (model / port / audio device)
    // + write path land next — they need /v1/hardware for the discovered device
    // lists.
    import { onMount } from 'svelte';
    import { rigsState } from './rigs.svelte';
    import type { RigSerial } from '../api/rigs';

    onMount(() => void rigsState.load());

    // Compact one-line summary of a rigdef's serial defaults, e.g.
    // "38400 8N1 · delim ;".
    function serialSummary(s: RigSerial): string {
        const framing = `${s.data_bits ?? '?'}${(s.parity ?? 'none')[0].toUpperCase()}${s.stop_bits ?? '?'}`;
        const parts = [s.baud_rate ? `${s.baud_rate} baud` : '', framing];
        if (s.line_delimiter) parts.push(`delim ${s.line_delimiter}`);
        return parts.filter(Boolean).join(' · ');
    }
</script>

{#snippet row(label: string, value: string, mono = false)}
    <dt class="text-muted">{label}</dt>
    <dd class="truncate text-ink {mono ? 'font-mono text-xs' : ''}" title={value}>{value}</dd>
{/snippet}

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
                            <span class="font-medium text-ink">{rigsState.nameFor(rig)}</span>
                            {#if rig.id === rigsState.defaultRigId}
                                <span
                                    class="rounded bg-surface-muted px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-muted uppercase"
                                    >default</span
                                >
                            {/if}
                        </div>
                    </button>
                </li>
            {/each}
        </ul>

        <!-- Detail: read-only rig details. The editable pickers (model / port /
             audio device) + write path land next; those need /v1/hardware for
             the discovered serial + audio device lists. -->
        <div class="min-w-0 flex-1">
            {#if rigsState.selected}
                {@const rig = rigsState.selected}
                {@const def = rigsState.defFor(rig)}
                <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <h2 class="text-lg font-semibold text-ink">{rigsState.nameFor(rig)}</h2>
                    {#if rig.id === rigsState.defaultRigId}
                        <span
                            class="rounded bg-surface-muted px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-muted uppercase"
                            >active</span
                        >
                    {/if}
                </div>
                {#if def?.manufacturer || def?.model}
                    <p class="mt-0.5 text-sm text-muted">
                        {[def?.manufacturer, def?.model].filter(Boolean).join(' · ')}
                    </p>
                {/if}

                <dl class="mt-5 grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
                    {@render row('Serial port', rig.port || '—', true)}
                    {@render row('Audio RX', rig.audio?.rx || '—')}
                    {@render row('Audio TX', rig.audio?.tx || '—')}
                    {@render row('FT8 mode', rigsState.ft8ModeFor(rig) || '—')}
                    {#if def?.serial}
                        {@render row('Serial defaults', serialSummary(def.serial))}
                    {/if}
                </dl>

                {#if def?.description}
                    <section class="mt-6">
                        <h3 class="mb-1 text-xs font-semibold tracking-wide text-muted uppercase">
                            About
                        </h3>
                        <p class="text-sm leading-relaxed text-muted">{def.description}</p>
                    </section>
                {/if}
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
