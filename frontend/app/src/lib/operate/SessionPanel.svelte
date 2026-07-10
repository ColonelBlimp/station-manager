<script lang="ts">
    // Session table — QSOs logged this sitting, newest first. Pure presentation
    // over the session state (the submit sink adds rows); self-contained tile
    // (ADR 0045/0046): owns its own header (title + the Export… action, which
    // used to live on the InfoPanel wrapper).
    import { session } from './session.svelte';
    import { openExport, focusCallsign } from './state.svelte';
    import { hideTile } from './layout.svelte';
    const tableHeight = 'h-55';
</script>

<div class="card w-2xl">
    <div class="flex items-center justify-between">
        <h3 class="text-sm font-semibold text-ink">Session</h3>
        <div class="flex items-center gap-x-2">
            <!-- Export / email the session — disabled with an empty log. -->
            <button class="btn text-xs" disabled={session.qsos.length === 0} onclick={openExport}>
                Export…
            </button>
            <button
                class="cursor-pointer rounded-md text-muted hover:text-ink"
                title="Hide"
                aria-label="Hide Session"
                onclick={() => {
                    hideTile('session');
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
        {#if session.qsos.length > 0}
            <div class="{tableHeight} overflow-y-auto">
                <table class="">
                    <thead class="bg-surface sticky top-0 z-10">
                        <tr class="text-xs text-muted text-left font-medium">
                            <th class="w-20">Time</th>
                            <th class="w-28 overflow-x-hidden text-nowrap text-ellipsis"
                                >Callsign</th
                            >
                            <th class="w-12">Band</th>
                            <th class="w-14">Mode</th>
                            <th class="w-10">Sent</th>
                            <th class="w-10">Rcvd</th>
                            <th class="w-32 overflow-x-hidden text-nowrap text-ellipsis">Name</th>
                            <th class="w-32 overflow-x-hidden text-nowrap text-ellipsis">Country</th
                            >
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
                                <td class="truncate">{q.country}</td>
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
    </div>
</div>
