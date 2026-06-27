<script lang="ts">
    // Logbook browse view — pick a logbook, page through its QSOs, multi-select
    // rows. The selection mechanism is here (per-row + select-all-on-page checkboxes,
    // selection persists across pages); the bulk ACTIONS it feeds (export, upload,
    // email, per-row edit) are still a follow-up. Paging is cursor-based
    // Next/Prev/First (the daemon has no page-number/offset endpoint — see
    // logbook.svelte.ts).
    import { onMount } from 'svelte';
    import { logbookState } from '../states/logbook.svelte';
    import { formatQsoDate, formatTime, formatFreq, formatMode } from '../utils/format';
    import type { LogbookQso } from '../api/logbooks';

    onMount(() => void logbookState.init());

    // Callsign tint: red when the QSO hasn't been forwarded/uploaded ANYWHERE yet
    // (a quick "still needs sending" cue, mirroring the v1 logbook), green once it
    // has. Status fields are "Y" when done.
    function handled(row: LogbookQso): boolean {
        return (
            row.sm_fwrd_by_email_status === 'Y' ||
            row.qrzcom_qso_upload_status === 'Y' ||
            row.clublog_qso_upload_status === 'Y'
        );
    }
</script>

<main class="bg-gray-50 p-6 font-sans text-gray-900">
    <div class="mx-auto w-280">
        <div class="mb-4 flex flex-wrap items-center gap-4">
            <h1 class="text-2xl font-semibold">Logbook</h1>

            <!-- Logbook selector -->
            <label class="flex items-center gap-2 text-sm">
                <select
                    class="rounded-md border border-gray-300 px-2 py-1 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none disabled:opacity-50"
                    value={logbookState.selectedId ?? ''}
                    disabled={logbookState.logbooks.length === 0}
                    onchange={(e) => logbookState.selectLogbook(Number(e.currentTarget.value))}
                >
                    {#each logbookState.logbooks as lb (lb.id)}
                        <option value={lb.id}>{lb.name} ({lb.callsign})</option>
                    {/each}
                </select>
            </label>

            <!-- Page size -->
            <label class="flex items-center gap-2 text-sm">
                <span class="text-gray-600">Rows</span>
                <select
                    class="rounded-md border border-gray-300 px-2 py-1 text-sm focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none"
                    bind:value={logbookState.pageSize}
                    onchange={(e) => logbookState.setPageSize(Number(e.currentTarget.value))}
                >
                    {#each logbookState.pageSizeOptions as n (n)}
                        <option value={n}>{n}</option>
                    {/each}
                </select>
            </label>

            <!-- Selection indicator. The bulk actions (forward / export / email /
                 upload-selected) are a follow-up; for now selection just reports a
                 count and offers a Clear. -->
            {#if logbookState.selectedCount > 0}
                <div class="ml-auto flex items-center gap-2 text-sm">
                    <span class="font-medium text-indigo-700"
                        >{logbookState.selectedCount.toLocaleString()} selected</span
                    >
                    <button
                        type="button"
                        class="cursor-pointer rounded-md border border-gray-300 px-2 py-1 hover:bg-gray-50"
                        onclick={() => logbookState.clearSelection()}>Clear</button
                    >
                </div>
            {/if}
        </div>

        {#if logbookState.error !== null}
            <p class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
                {logbookState.error}
            </p>
        {:else if logbookState.logbooks.length === 0 && !logbookState.loading}
            <p class="text-sm text-gray-500">No logbooks yet.</p>
        {:else}
            <div class="overflow-x-auto rounded-md border border-gray-300 bg-white">
                <table class="table-fixed w-full border-collapse text-left text-sm">
                    <thead class="h-8 font-medium border-b border-gray-300 bg-gray-50 text-xs text-gray-500 uppercase">
                        <tr>
                            <th class="w-10 pl-3">
                                <input
                                    type="checkbox"
                                    class="cursor-pointer align-middle"
                                    aria-label="Select all rows on this page"
                                    checked={logbookState.allVisibleSelected}
                                    indeterminate={logbookState.someVisibleSelected}
                                    disabled={logbookState.rows.length === 0}
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
                        </tr>
                    </thead>
                    <tbody>
                        {#each logbookState.rows as row (row.id)}
                            <tr
                                class="h-6 font-mono border-b border-gray-300 last:border-b-0 text-gray-700 whitespace-nowrap hover:bg-gray-100 {logbookState.selected.has(
                                    row.id
                                )
                                    ? 'bg-indigo-100'
                                    : ''}"
                            >
                                <td class="pl-3 pt-1">
                                    <input
                                        type="checkbox"
                                        class="cursor-pointer items-center"
                                        aria-label="Select QSO with {row.call ?? 'unknown'}"
                                        checked={logbookState.selected.has(row.id)}
                                        onchange={() => logbookState.toggleRow(row.id)}
                                    />
                                </td>
                                <td class=""
                                    >{formatQsoDate(row.qso_date)}</td
                                >
                                <td class=""
                                    >{formatTime(row.time_on)}</td
                                >
                                <td
                                    class="font-semibold {handled(
                                        row
                                    )
                                        ? 'text-green-800'
                                        : 'text-red-700'}"
                                    title={handled(row)
                                        ? undefined
                                        : 'Not yet forwarded or uploaded'}>{row.call ?? ''}</td
                                >
                                <td class=""
                                    >{row.band ?? ''}</td
                                >
                                <td class=""
                                    >{formatFreq(row.freq)}</td
                                >
                                <td class=""
                                    >{formatMode(row.mode, row.submode)}</td
                                >
                                <td
                                    class="overflow-hidden text-ellipsis"
                                    title={row.country ?? ''}>{row.country ?? ''}</td
                                >
                                <td
                                    class="overflow-hidden text-ellipsis"
                                    title={row.name ?? ''}>{row.name ?? ''}</td
                                >
                                <td
                                    class="pl-4 overflow-hidden text-ellipsis"
                                    title={row.comment ?? ''}>{row.comment ?? ''}</td
                                >
                            </tr>
                        {/each}
                        {#if logbookState.rows.length === 0 && !logbookState.loading}
                            <tr>
                                <td colspan="10" class="px-3 py-6 text-center text-sm text-gray-400"
                                    >No QSOs in this logbook.</td
                                >
                            </tr>
                        {/if}
                    </tbody>
                </table>
            </div>

            <!-- Pager -->
            <div class="mt-3 flex items-center gap-2 text-sm">
                <button
                    type="button"
                    class="cursor-pointer rounded-md border border-gray-300 px-2 py-1 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-40"
                    disabled={!logbookState.hasPrev || logbookState.loading}
                    onclick={() => logbookState.firstPage()}>« First</button
                >
                <button
                    type="button"
                    class="cursor-pointer rounded-md border border-gray-300 px-2 py-1 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-40"
                    disabled={!logbookState.hasPrev || logbookState.loading}
                    onclick={() => logbookState.prevPage()}>‹ Prev</button
                >
                <span class="px-1 text-gray-600">
                    {#if logbookState.count > 0}
                        showing {logbookState.rangeStart.toLocaleString()}–{logbookState.rangeEnd.toLocaleString()}
                        of {logbookState.count.toLocaleString()}
                    {:else}
                        —
                    {/if}
                </span>
                <button
                    type="button"
                    class="cursor-pointer rounded-md border border-gray-300 px-2 py-1 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-40"
                    disabled={!logbookState.hasNext || logbookState.loading}
                    onclick={() => logbookState.nextPage()}>Next ›</button
                >
                {#if logbookState.loading}
                    <span class="text-gray-400">Loading…</span>
                {/if}
            </div>
        {/if}
    </div>
</main>
