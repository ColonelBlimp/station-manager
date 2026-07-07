<script lang="ts">
    // Session table — QSOs logged this sitting, newest first. Pure presentation
    // over the session state (the submit sink adds rows); fills whatever host
    // it's given (ADR 0045) — today the InfoPanel below the logging card.
    import { session } from './session.svelte';
</script>

{#if session.qsos.length > 0}
    <table class="w-full text-left text-sm">
        <thead>
            <tr class="border-b border-line text-xs text-muted">
                <th class="py-1.5 pr-4 font-medium">Time</th>
                <th class="py-1.5 pr-4 font-medium">Callsign</th>
                <th class="py-1.5 pr-4 font-medium">Band</th>
                <th class="py-1.5 pr-4 font-medium">Mode</th>
                <th class="py-1.5 pr-4 font-medium">Sent</th>
                <th class="py-1.5 pr-4 font-medium">Rcvd</th>
                <th class="py-1.5 pr-4 font-medium">Name</th>
                <th class="py-1.5 font-medium">Country</th>
            </tr>
        </thead>
        <tbody>
            {#each session.qsos as q (q.id)}
                <tr class="border-b border-line-soft text-ink last:border-0">
                    <td class="py-1.5 pr-4 tabular-nums">{q.timeOn}</td>
                    <td class="py-1.5 pr-4 font-medium">{q.callsign}</td>
                    <td class="py-1.5 pr-4">{q.band}</td>
                    <td class="py-1.5 pr-4">{q.mode}</td>
                    <td class="py-1.5 pr-4 tabular-nums">{q.rstSent}</td>
                    <td class="py-1.5 pr-4 tabular-nums">{q.rstRcvd}</td>
                    <td class="max-w-32 truncate py-1.5 pr-4">{q.name}</td>
                    <td class="max-w-40 truncate py-1.5">{q.country}</td>
                </tr>
            {/each}
        </tbody>
    </table>
{:else}
    <div class="flex h-24 items-center justify-center text-sm text-muted">
        No QSOs logged this session.
    </div>
{/if}
