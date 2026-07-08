<script lang="ts">
    // Worked-before table — previous QSOs with the station being entered.
    // Pure presentation over the worked state (the LoggingCard drives the
    // lookup); fills whatever host it's given (ADR 0045) — today the InfoPanel
    // below the logging card.
    import { worked } from './worked.svelte';
    const tableHeight = 'h-55';
</script>

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
