<script lang="ts">
    // Worked-before table — previous QSOs with the station being entered.
    // Pure presentation over the worked state (the LoggingCard drives the
    // lookup); fills whatever host it's given (ADR 0045) — today the InfoPanel
    // below the logging card.
    import { worked } from './worked.svelte';
</script>

{#if worked.status === 'done' && worked.qsos.length > 0}
    <table class="w-full text-left text-sm">
        <thead>
            <tr class="border-b border-line text-xs text-muted">
                <th class="py-1.5 pr-4 font-medium">Date</th>
                <th class="py-1.5 pr-4 font-medium">Time</th>
                <th class="py-1.5 pr-4 font-medium">Band</th>
                <th class="py-1.5 pr-4 font-medium">Mode</th>
                <th class="py-1.5 pr-4 font-medium">Sent</th>
                <th class="py-1.5 pr-4 font-medium">Rcvd</th>
                <th class="py-1.5 font-medium">Name</th>
            </tr>
        </thead>
        <tbody>
            {#each worked.qsos as q (q.date + q.timeOn + q.band + q.mode)}
                <tr class="border-b border-line-soft text-ink last:border-0">
                    <td class="py-1.5 pr-4 tabular-nums">{q.date}</td>
                    <td class="py-1.5 pr-4 tabular-nums">{q.timeOn}</td>
                    <td class="py-1.5 pr-4">{q.band}</td>
                    <td class="py-1.5 pr-4">{q.mode}</td>
                    <td class="py-1.5 pr-4 tabular-nums">{q.rstSent}</td>
                    <td class="py-1.5 pr-4 tabular-nums">{q.rstRcvd}</td>
                    <td class="py-1.5">{q.name}</td>
                </tr>
            {/each}
        </tbody>
    </table>
{:else if worked.status === 'pending'}
    <div class="flex h-24 items-center justify-center text-sm text-muted">
        Checking {worked.call}…
    </div>
{:else if worked.status === 'done'}
    <div class="flex h-24 items-center justify-center text-sm text-muted">
        No previous QSOs with {worked.call}.
    </div>
{:else}
    <div class="flex h-24 items-center justify-center text-sm text-muted">
        Previous QSOs appear here as you type a callsign.
    </div>
{/if}
