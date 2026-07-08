<script lang="ts">
    // Session table — QSOs logged this sitting, newest first. Pure presentation
    // over the session state (the submit sink adds rows); fills whatever host
    // it's given (ADR 0045) — today the InfoPanel below the logging card.
    import { session } from './session.svelte';
    const tableHeight = 'h-55';
</script>

{#if session.qsos.length > 0}
    <div class="{tableHeight} overflow-y-auto">
        <table class="">
            <thead class="bg-surface sticky top-0 z-10">
                <tr class="text-xs text-muted text-left font-medium">
                    <th class="w-20">Time</th>
                    <th class="w-28 overflow-x-hidden text-nowrap text-ellipsis">Callsign</th>
                    <th class="w-12">Band</th>
                    <th class="w-14">Mode</th>
                    <th class="w-10">Sent</th>
                    <th class="w-10">Rcvd</th>
                    <th class="w-32 overflow-x-hidden text-nowrap text-ellipsis">Name</th>
                    <th class="w-32 overflow-x-hidden text-nowrap text-ellipsis">Country</th>
                </tr>
            </thead>
            <tbody>
                {#each session.qsos as q (q.id)}
                    <tr class="border-b border-line-soft text-ink last:border-0">
                        <td class="tabular-nums">{q.timeOn}</td>
                        <td class="font-medium">{q.callsign}</td>
                        <td class="">{q.band}</td>
                        <td class="">{q.mode}</td>
                        <td class="tabular-nums">{q.rstSent}</td>
                        <td class="tabular-nums">{q.rstRcvd}</td>
                        <td class="truncate">{q.name}</td>
                        <td class="truncate" >{q.country}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </div>
{:else}
    <div class="flex {tableHeight} items-center justify-center text-sm text-muted">
        No QSOs logged this session.
    </div>
{/if}
