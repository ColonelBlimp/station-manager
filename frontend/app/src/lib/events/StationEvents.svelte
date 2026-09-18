<script lang="ts">
    // Station Events page (W-0020) — the full-page replacement for the header's
    // notification slide-over: every durable operator event the daemon keeps,
    // newest first, with category and severity filters and a count line so an
    // empty filtered list reads as a filter and not a fault. Rows carry client
    // wording per kind (lib/events/wording.ts, ADR 0010); an unknown detail
    // shape reads "Details unavailable", never its raw content.
    import { onMount } from 'svelte';
    import { stationEventsState as st } from './stationEvents.svelte';
    import { kindLabel, categoryLabel, detailSummary, severityDot, fmtTime } from './wording';
    import type { StationEventCategory, StationEventSeverity } from '../api/stationEvents';

    onMount(() => void st.load());

    const categories: { value: StationEventCategory | ''; label: string }[] = [
        { value: '', label: 'All' },
        { value: 'notification', label: 'Notifications' },
        { value: 'alarm', label: 'Alarms' },
    ];
    const severities: { value: StationEventSeverity | ''; label: string }[] = [
        { value: '', label: 'Any severity' },
        { value: 'info', label: 'Info' },
        { value: 'warn', label: 'Warn' },
        { value: 'error', label: 'Error' },
    ];
</script>

<div class="mx-auto max-w-7xl">
    <div class="mb-4 flex flex-wrap items-center gap-4">
        <h1 class="text-2xl font-semibold text-ink">Station Events</h1>
        <div
            class="inline-flex overflow-hidden rounded-md border border-line text-xs"
            role="group"
            aria-label="Category"
        >
            {#each categories as c, i (c.value)}
                <button
                    type="button"
                    class="cursor-pointer px-2 py-1 {i > 0
                        ? 'border-l border-line'
                        : ''} {st.category === c.value
                        ? 'bg-focus text-white'
                        : 'text-muted hover:bg-surface-muted'}"
                    aria-pressed={st.category === c.value}
                    onclick={() => st.setCategory(c.value)}>{c.label}</button
                >
            {/each}
        </div>
        <div
            class="inline-flex overflow-hidden rounded-md border border-line text-xs"
            role="group"
            aria-label="Severity"
        >
            {#each severities as s, i (s.value)}
                <button
                    type="button"
                    class="cursor-pointer px-2 py-1 {i > 0
                        ? 'border-l border-line'
                        : ''} {st.severity === s.value
                        ? 'bg-focus text-white'
                        : 'text-muted hover:bg-surface-muted'}"
                    aria-pressed={st.severity === s.value}
                    onclick={() => st.setSeverity(s.value)}>{s.label}</button
                >
            {/each}
        </div>
        <button class="btn ml-auto" onclick={() => void st.load()} disabled={st.loading}>
            {st.loading ? 'Loading…' : 'Refresh'}
        </button>
    </div>

    <p class="mb-2 text-sm text-muted" data-testid="count-line">{st.countLine()}</p>

    {#if st.error}
        <div class="card">
            <p class="text-sm text-red-600 dark:text-red-400">{st.error}</p>
            <button class="btn mt-3" onclick={() => void st.load()}>Retry</button>
        </div>
    {:else if st.items.length > 0}
        <ul class="divide-y divide-line rounded-xl border border-line bg-surface">
            {#each st.items as ev (ev.id)}
                <li class="flex gap-x-3 px-4 py-3">
                    <span
                        class="mt-1.5 size-2 shrink-0 rounded-full {severityDot(ev.severity)}"
                        aria-hidden="true"
                    ></span>
                    <span class="sr-only">Severity: {ev.severity}</span>
                    <div class="min-w-0 flex-1">
                        <p class="text-sm font-medium text-ink">
                            {kindLabel(ev.kind)}
                            <span
                                class="ml-2 rounded bg-surface-muted px-1.5 py-0.5 text-xs font-normal text-muted"
                                >{categoryLabel(ev.category)}</span
                            >
                        </p>
                        <p class="truncate text-xs text-muted">{detailSummary(ev)}</p>
                        <p class="mt-0.5 text-xs text-muted">
                            <span>{fmtTime(ev.occurred_at)}</span>
                            <span class="text-muted/70"> · {ev.build}</span>
                        </p>
                    </div>
                </li>
            {/each}
        </ul>
    {/if}
</div>
