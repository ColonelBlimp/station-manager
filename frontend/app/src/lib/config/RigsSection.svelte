<script lang="ts">
    // Rigs section — the configured rig profiles (app Settings, ADR 0044), as a
    // master-detail: the rig list on the left, a details panel on the right. The
    // identity is read-only; the CONNECTION (serial port + audio RX/TX) is
    // editable via pickers populated from /v1/hardware, saved via a whole-
    // catalogue PUT (see rigs.svelte.ts data-safety note). Model / ft8_mode /
    // set-default / add / delete land in follow-up increments.
    import { onMount } from 'svelte';
    import { rigsState } from './rigs.svelte';
    import type { RigSerial } from '../api/rigs';
    import type { AudioDevice } from '../api/hardware';

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

{#snippet audioPicker(
    label: string,
    which: 'rx' | 'tx',
    devices: AudioDevice[],
    current: string | undefined
)}
    <label class="flex flex-col gap-1">
        <span class="text-sm font-medium text-ink">{label}</span>
        <select
            class="input"
            value={current ?? ''}
            disabled={rigsState.saving}
            onchange={(e) => rigsState.setDraftAudio(which, e.currentTarget.value)}
        >
            <option value="">— none —</option>
            <!-- keep the stored device even if it isn't currently present -->
            {#if current && !devices.some((d) => d.name === current)}
                <option value={current}>{current} (not detected)</option>
            {/if}
            {#each devices as d (d.name)}
                <option value={d.name}>{d.name}</option>
            {/each}
        </select>
    </label>
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
                        class="w-full rounded-md border px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60 {rigsState.selectedId ===
                        rig.id
                            ? 'border-focus bg-surface-muted'
                            : 'border-line hover:bg-surface-muted'}"
                        disabled={rigsState.saving}
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

        <!-- Detail: identity read-only; the CONNECTION (port + audio) is editable
             via pickers from /v1/hardware. Model / ft8_mode / set-default /
             add / delete land in follow-up increments. -->
        <div class="min-w-0 flex-1">
            {#if rigsState.selected && rigsState.draft}
                {@const rig = rigsState.selected}
                {@const draft = rigsState.draft}
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

                <!-- Connection (editable) -->
                <section class="mt-5 max-w-md space-y-3">
                    <h3 class="text-xs font-semibold tracking-wide text-muted uppercase">
                        Connection
                    </h3>

                    <label class="flex flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Serial port</span>
                        <select
                            class="input font-mono text-xs"
                            value={draft.port}
                            disabled={rigsState.saving}
                            onchange={(e) => rigsState.setDraftPort(e.currentTarget.value)}
                        >
                            <!-- keep the stored value even if it isn't currently detected -->
                            {#if draft.port && !rigsState.serialPorts.some((p) => p.id === draft.port)}
                                <option value={draft.port}>{draft.port} (not detected)</option>
                            {/if}
                            {#each rigsState.serialPorts as p (p.id)}
                                <option value={p.id}>{p.label}</option>
                            {/each}
                        </select>
                    </label>

                    {#if rigsState.audioAvailable}
                        {@render audioPicker('Audio RX', 'rx', rigsState.capture, draft.audio?.rx)}
                        {@render audioPicker('Audio TX', 'tx', rigsState.playback, draft.audio?.tx)}
                    {:else}
                        <!-- static/CGO-free daemon: no enumeration, show stored names -->
                        <dl class="grid grid-cols-[6rem_1fr] gap-x-4 gap-y-1 text-sm">
                            {@render row('Audio RX', draft.audio?.rx || '—')}
                            {@render row('Audio TX', draft.audio?.tx || '—')}
                        </dl>
                        <p class="text-xs text-muted">
                            Audio devices can't be enumerated by this daemon build (read-only).
                        </p>
                    {/if}

                    {#if rigsState.dirty}
                        <p class="text-xs text-muted">
                            Changes take effect after a daemon restart.
                        </p>
                    {/if}
                    <div class="flex items-center gap-3 pt-1">
                        <button
                            class="btn btn-primary"
                            disabled={!rigsState.dirty || rigsState.saving}
                            onclick={() => rigsState.save()}
                        >
                            {rigsState.saving ? 'Saving…' : 'Save'}
                        </button>
                        <button
                            class="btn"
                            disabled={!rigsState.dirty || rigsState.saving}
                            onclick={() => rigsState.resetDraft()}
                        >
                            Cancel
                        </button>
                    </div>
                </section>

                <!-- Operating (read-only) -->
                <dl class="mt-6 grid max-w-md grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
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
