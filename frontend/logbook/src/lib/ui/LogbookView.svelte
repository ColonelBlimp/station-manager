<script lang="ts">
    // Logbook browse view — pick a logbook, page through its QSOs. Read-only for
    // now; the heavier logbook-management surface (multi-select, export, upload,
    // email, per-row edit) is a follow-up. Paging is cursor-based Next/Prev/First
    // (the daemon has no page-number/offset endpoint — see logbook.svelte.ts).
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

<main class="min-h-screen bg-gray-50 p-6 font-sans text-gray-900">
    <div class="mx-auto max-w-6xl">
        <div class="mb-4 flex flex-wrap items-center gap-4">
            <h1 class="text-2xl font-semibold">Logbook</h1>

            <!-- Logbook selector -->
            <label class="flex items-center gap-2 text-sm">
                <span class="text-gray-600">Logbook</span>
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
                    value={logbookState.pageSize}
                    onchange={(e) => logbookState.setPageSize(Number(e.currentTarget.value))}
                >
                    {#each logbookState.pageSizeOptions as n (n)}
                        <option value={n}>{n}</option>
                    {/each}
                </select>
            </label>
        </div>

        {#if logbookState.error !== null}
            <p class="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
                {logbookState.error}
            </p>
        {:else if logbookState.logbooks.length === 0 && !logbookState.loading}
            <p class="text-sm text-gray-500">No logbooks yet.</p>
        {:else}
            <div class="overflow-x-auto rounded-md border border-gray-200 bg-white">
                <table class="w-full border-collapse text-left text-sm">
                    <thead class="border-b border-gray-200 bg-gray-50 text-xs text-gray-500 uppercase">
                        <tr>
                            <th class="px-3 py-2 font-medium">Date</th>
                            <th class="px-3 py-2 font-medium">Time</th>
                            <th class="px-3 py-2 font-medium">Callsign</th>
                            <th class="px-3 py-2 font-medium">Band</th>
                            <th class="px-3 py-2 font-medium">Freq</th>
                            <th class="px-3 py-2 font-medium">Mode</th>
                            <th class="px-3 py-2 font-medium">Country</th>
                            <th class="px-3 py-2 font-medium">Name</th>
                            <th class="px-3 py-2 font-medium">Comment</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each logbookState.rows as row (row.id)}
                            <tr class="border-b border-gray-100 last:border-b-0 hover:bg-gray-50">
                                <td class="px-3 py-1.5 font-mono whitespace-nowrap text-gray-600"
                                    >{formatQsoDate(row.qso_date)}</td
                                >
                                <td class="px-3 py-1.5 font-mono whitespace-nowrap text-gray-600"
                                    >{formatTime(row.time_on)}</td
                                >
                                <td
                                    class="px-3 py-1.5 font-mono font-semibold whitespace-nowrap {handled(
                                        row
                                    )
                                        ? 'text-green-800'
                                        : 'text-red-700'}"
                                    title={handled(row)
                                        ? undefined
                                        : 'Not yet forwarded or uploaded'}>{row.call ?? ''}</td
                                >
                                <td class="px-3 py-1.5 whitespace-nowrap text-gray-700">{row.band ?? ''}</td>
                                <td class="px-3 py-1.5 font-mono whitespace-nowrap text-gray-700"
                                    >{formatFreq(row.freq)}</td
                                >
                                <td class="px-3 py-1.5 whitespace-nowrap text-gray-700"
                                    >{formatMode(row.mode, row.submode)}</td
                                >
                                <td
                                    class="max-w-[10rem] truncate px-3 py-1.5 text-gray-700"
                                    title={row.country ?? ''}>{row.country ?? ''}</td
                                >
                                <td
                                    class="max-w-[10rem] truncate px-3 py-1.5 text-gray-700"
                                    title={row.name ?? ''}>{row.name ?? ''}</td
                                >
                                <td
                                    class="max-w-[14rem] truncate px-3 py-1.5 text-gray-500"
                                    title={row.comment ?? ''}>{row.comment ?? ''}</td
                                >
                            </tr>
                        {/each}
                        {#if logbookState.rows.length === 0 && !logbookState.loading}
                            <tr>
                                <td colspan="9" class="px-3 py-6 text-center text-sm text-gray-400"
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
