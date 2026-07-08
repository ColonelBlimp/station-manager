<script lang="ts">
    // Worked-before table — previous QSOs with the station being entered. Pure
    // presentation over the worked state (the LoggingCard drives the lookup);
    // self-contained tile (ADR 0045/0046): owns its own header (title + the
    // View… contact overlay action, which used to live on the InfoPanel wrapper).
    import { worked } from './worked.svelte';
    import { qsoClock } from './qso.svelte';
    import { openContact, focusCallsign } from './state.svelte';
    import { hideTile } from './layout.svelte';
    const tableHeight = 'h-55';
</script>

<div class="card w-2xl">
    <div class="flex items-center justify-between">
        <h3 class="text-sm font-semibold text-ink">Worked</h3>
        <div class="flex items-center gap-x-2">
            <!-- Contact-detail overlay — only meaningful once a QSO is underway. -->
            <button
                class="btn text-xs"
                disabled={!qsoClock.started}
                title={qsoClock.started ? undefined : 'Start a QSO to view contact details'}
                onclick={openContact}
            >
                View…
            </button>
            <button
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Hide"
                aria-label="Hide Worked"
                onclick={() => {
                    hideTile('worked');
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
    </div>

    <div class="mt-3">
        {#if worked.status === 'done' && worked.qsos.length > 0}
            <div class="{tableHeight} overflow-y-auto">
                <table class="table-fixed">
                    <thead class="bg-surface sticky top-0 z-10">
                        <tr class="text-xs text-muted text-left font-medium">
                            <th class="w-24 pb-1">Date</th>
                            <th class="w-14">Time</th>
                            <th class="w-12">Band</th>
                            <th class="w-14">Mode</th>
                            <th class="w-10">Sent</th>
                            <th class="w-10">Rcvd</th>
                            <th class="w-40">Name</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each worked.qsos as q (q.uuid)}
                            <tr class="border-b border-line-soft text-ink last:border-0">
                                <td class="py-0.5 tabular-nums">{q.date}</td>
                                <td class="tabular-nums">{q.timeOn}</td>
                                <td class="">{q.band}</td>
                                <td class="">{q.mode}</td>
                                <td class="tabular-nums">{q.rstSent}</td>
                                <td class="tabular-nums">{q.rstRcvd}</td>
                                <td class="">{q.name}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        {:else if worked.status === 'pending'}
            <div class="flex {tableHeight} items-center justify-center text-sm text-muted">
                Checking {worked.call}…
            </div>
        {:else if worked.status === 'done'}
            <div class="flex {tableHeight} items-center justify-center text-sm text-muted">
                No previous QSOs with {worked.call}.
            </div>
        {:else}
            <div class="flex {tableHeight} items-center justify-center text-sm text-muted">
                Previous QSOs appear here as you type a callsign.
            </div>
        {/if}
    </div>
</div>
