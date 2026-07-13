<script lang="ts">
    // Logbook page — the ADR 0044 port of the shipping logbook SPA's browse
    // surface: pick a logbook, page through its QSOs (cursor-based Next/Prev/
    // First — the daemon has no offset endpoint), multi-select rows, and run
    // the bulk upload-backfill (ADR 0039). Restyled onto the app shell's theme
    // tokens (light + dark); behaviour and state are the battle-tested port
    // (logbook.svelte.ts). Email-out and per-row Edit land in follow-up
    // increments.
    import { onMount } from 'svelte';
    import { logbookState } from './logbook.svelte';
    import { formatQsoDate, formatTime, formatFreq, formatMode } from './format';
    import { uploadState, uploadTooltip, uploadColorClass } from './uploadStatus';
    import EditQsoModal from './EditQsoModal.svelte';
    import LogbookEmailControls from './LogbookEmailControls.svelte';
    import type { LogbookQso } from '../api/logbooks';

    onMount(() => void logbookState.init());

    // Tri-state callsign colour against the ENABLED forwarders E (ADR 0039):
    // green = on all of E, amber = on some, red = on none, default = no enabled
    // forwarders (the cue means nothing). The tooltip names which it's on/missing.
    function callClass(row: LogbookQso): string {
        return uploadColorClass(uploadState(row, logbookState.enabledForwarders));
    }
    function callTitle(row: LogbookQso): string | undefined {
        const t = uploadTooltip(row, logbookState.enabledForwarders);
        return t === '' ? undefined : t;
    }
</script>

<div class="mx-auto max-w-7xl">
    <div class="mb-4 flex flex-wrap items-center gap-4">
        <h1 class="text-2xl font-semibold text-ink">Logbook</h1>

        <!-- Logbook selector -->
        <select
            class="input mt-0 w-auto"
            aria-label="Logbook"
            value={logbookState.selectedId ?? ''}
            disabled={logbookState.logbooks.length === 0}
            onchange={(e) => logbookState.selectLogbook(Number(e.currentTarget.value))}
        >
            {#each logbookState.logbooks as lb (lb.id)}
                <option value={lb.id}>{lb.name} ({lb.callsign})</option>
            {/each}
        </select>

        <!-- Page size -->
        <label class="flex items-center gap-2 text-sm text-muted">
            Rows
            <select
                class="input mt-0 w-auto"
                bind:value={logbookState.pageSize}
                onchange={(e) => logbookState.setPageSize(Number(e.currentTarget.value))}
            >
                {#each logbookState.pageSizeOptions as n (n)}
                    <option value={n}>{n}</option>
                {/each}
            </select>
        </label>

        <!-- Backfill destination (ADR 0039): pick a service to see the QSOs not
             yet uploaded to it and to target the Upload action. "All" = plain
             browse. "Show uploaded" reveals the whole logbook while keeping the
             destination as the upload target. -->
        {#if logbookState.enabledForwarders.length > 0}
            <label class="flex items-center gap-2 text-sm text-muted">
                Uploads
                <select
                    class="input mt-0 w-auto"
                    value={logbookState.selectedDestination}
                    onchange={(e) => logbookState.selectDestination(e.currentTarget.value)}
                >
                    <option value="">All</option>
                    {#each logbookState.enabledForwarders as f (f.name)}
                        <option value={f.name}>Not on {f.name}</option>
                    {/each}
                </select>
            </label>
            {#if logbookState.hasDestination}
                <label class="flex items-center gap-2 text-sm text-muted">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        checked={logbookState.showUploaded}
                        onchange={() => logbookState.toggleShowUploaded()}
                    />
                    Show uploaded
                </label>
            {/if}
        {/if}

        <!-- "Still needs emailing" filter — client-side, hides already-emailed
             rows on the current page (see logbook.svelte.ts notEmailedOnly). -->
        <label class="flex items-center gap-2 text-sm text-muted">
            <input
                type="checkbox"
                class="cursor-pointer"
                checked={logbookState.notEmailedOnly}
                onchange={() => logbookState.toggleNotEmailedOnly()}
            />
            Not emailed only
        </label>

        <!-- Selection toolbar: count + email-out + upload-to-destination + Clear.
             Email posts the selected rows' UUIDs to /v1/session/email
             (LogbookEmailControls). -->
        {#if logbookState.selectedCount > 0}
            <div class="ml-auto flex flex-wrap items-center gap-2 text-sm">
                <span class="font-medium text-focus"
                    >{logbookState.selectedCount.toLocaleString()} selected</span
                >
                {#if logbookState.hasDestination}
                    <button
                        type="button"
                        class="btn text-xs"
                        disabled={logbookState.uploading}
                        onclick={() => logbookState.uploadSelected()}
                        >{logbookState.uploading
                            ? 'Uploading…'
                            : `Upload ${logbookState.selectedCount} to ${logbookState.selectedDestination}`}</button
                    >
                {/if}
                <LogbookEmailControls />
                <button
                    type="button"
                    class="btn text-xs"
                    onclick={() => logbookState.clearSelection()}>Clear</button
                >
            </div>
        {/if}
    </div>

    {#if logbookState.notice !== null}
        <p
            class="mb-3 rounded-md border border-green-300 bg-green-50 px-3 py-2 text-sm text-green-800 dark:border-green-800 dark:bg-green-500/10 dark:text-green-300"
        >
            {logbookState.notice}
        </p>
    {/if}

    {#if logbookState.error !== null}
        <p
            class="rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-800 dark:bg-red-500/10 dark:text-red-300"
        >
            {logbookState.error}
        </p>
    {:else if logbookState.logbooks.length === 0 && !logbookState.loading}
        <p class="text-sm text-muted">No logbooks yet.</p>
    {:else}
        <div class="overflow-x-auto rounded-xl border border-line bg-surface">
            <table class="w-full table-fixed border-collapse text-left text-sm">
                <thead
                    class="h-8 border-b border-line bg-surface-muted text-xs font-medium text-muted uppercase"
                >
                    <tr>
                        <th class="w-10 pl-3">
                            <input
                                type="checkbox"
                                class="cursor-pointer align-middle"
                                aria-label="Select all rows on this page"
                                checked={logbookState.allVisibleSelected}
                                indeterminate={logbookState.someVisibleSelected}
                                disabled={logbookState.visibleRows.length === 0}
                                onchange={() => logbookState.toggleAllVisible()}
                            />
                        </th>
                        <th class="w-26">Date</th>
                        <th class="w-16">Time</th>
                        <th class="w-32">Callsign</th>
                        <th class="w-13">Band</th>
                        <th class="w-18">Freq</th>
                        <th class="w-32">Mode</th>
                        <th class="w-32">Country</th>
                        <th class="w-32">Name</th>
                        <th class="pl-4">Comment</th>
                        <th class="w-16 text-center">Email</th>
                        <th class="w-14"><span class="sr-only">Edit</span></th>
                    </tr>
                </thead>
                <tbody>
                    {#each logbookState.visibleRows as row (row.id)}
                        <tr
                            class="h-6 border-b border-line font-mono whitespace-nowrap text-ink last:border-b-0 hover:bg-surface-muted {logbookState.selected.has(
                                row.id
                            )
                                ? 'bg-focus/10'
                                : ''}"
                        >
                            <td class="pt-1 pl-3">
                                <input
                                    type="checkbox"
                                    class="cursor-pointer items-center"
                                    aria-label="Select QSO with {row.call ?? 'unknown'}"
                                    checked={logbookState.selected.has(row.id)}
                                    onchange={() => logbookState.toggleRow(row)}
                                />
                            </td>
                            <td>{formatQsoDate(row.qso_date)}</td>
                            <td>{formatTime(row.time_on)}</td>
                            <td class="font-semibold {callClass(row)}" title={callTitle(row)}
                                >{row.call ?? ''}</td
                            >
                            <td>{row.band ?? ''}</td>
                            <td>{formatFreq(row.freq)}</td>
                            <td>{formatMode(row.mode, row.submode)}</td>
                            <td class="overflow-hidden text-ellipsis" title={row.country ?? ''}
                                >{row.country ?? ''}</td
                            >
                            <td class="overflow-hidden text-ellipsis" title={row.name ?? ''}
                                >{row.name ?? ''}</td
                            >
                            <td class="overflow-hidden pl-4 text-ellipsis" title={row.comment ?? ''}
                                >{row.comment ?? ''}</td
                            >
                            <td class="text-center">
                                {#if row.sm_fwrd_by_email_status === 'Y'}
                                    <span
                                        class="text-green-700 dark:text-green-400"
                                        title="Sent via email">✓</span
                                    >
                                {:else}
                                    <span class="text-muted/50" title="Not sent via email">–</span>
                                {/if}
                            </td>
                            <td class="pr-3 text-right">
                                <button
                                    type="button"
                                    class="cursor-pointer rounded px-1.5 py-0.5 font-sans text-xs text-focus hover:bg-surface-muted disabled:cursor-not-allowed disabled:text-muted/50"
                                    aria-label="Edit QSO with {row.call ?? 'unknown'}"
                                    disabled={!row.uuid}
                                    title={row.uuid
                                        ? 'Edit this QSO'
                                        : 'This QSO has no id and cannot be edited'}
                                    onclick={() => logbookState.openEdit(row)}>Edit</button
                                >
                            </td>
                        </tr>
                    {/each}
                    {#if logbookState.visibleRows.length === 0 && !logbookState.loading}
                        <tr>
                            <td colspan="12" class="px-3 py-6 text-center text-sm text-muted">
                                {#if logbookState.rows.length === 0}
                                    No QSOs in this logbook.
                                {:else}
                                    All QSOs on this page have been emailed.
                                {/if}
                            </td>
                        </tr>
                    {/if}
                </tbody>
            </table>
        </div>

        <!-- Pager -->
        <div class="mt-3 flex items-center gap-2 text-sm">
            <button
                type="button"
                class="btn text-xs"
                disabled={!logbookState.hasPrev || logbookState.loading}
                onclick={() => logbookState.firstPage()}>« First</button
            >
            <button
                type="button"
                class="btn text-xs"
                disabled={!logbookState.hasPrev || logbookState.loading}
                onclick={() => logbookState.prevPage()}>‹ Prev</button
            >
            <span class="px-1 text-muted">
                {#if logbookState.count > 0}
                    showing {logbookState.rangeStart.toLocaleString()}–{logbookState.rangeEnd.toLocaleString()}
                    of {logbookState.count.toLocaleString()}
                {:else}
                    —
                {/if}
            </span>
            <button
                type="button"
                class="btn text-xs"
                disabled={!logbookState.hasNext || logbookState.loading}
                onclick={() => logbookState.nextPage()}>Next ›</button
            >
            {#if logbookState.loading}
                <span class="text-muted">Loading…</span>
            {/if}
        </div>
    {/if}
</div>

{#if logbookState.editing !== null}
    <EditQsoModal row={logbookState.editing} />
{/if}
