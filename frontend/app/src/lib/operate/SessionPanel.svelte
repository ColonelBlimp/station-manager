<script lang="ts">
    // Session table — QSOs logged this sitting, newest first. Pure presentation
    // over the session state (the submit sink adds rows); self-contained tile
    // (ADR 0045/0046): owns its own header (title + the Export… action, which
    // used to live on the InfoPanel wrapper).
    //
    // In-place edit (dogfood 2026-07-18): the callsign is a button that opens
    // the shared EditQsoModal RIGHT HERE — navigating to the Logbook route to
    // edit unmounts the FT8 view and takes a live CQ run off the air
    // (demand-driven capture). Rows without a uuid (pre-upgrade sessions)
    // render as plain text.
    import { session } from './session.svelte';
    import { sessionEdit } from './sessionEdit.svelte';
    import { openExport, focusCallsign } from './state.svelte';
    import { hideTile } from './layout.svelte';
    import EditQsoModal from '../logbook/EditQsoModal.svelte';
    import type { QsoPatch } from '../api/qso-patch';
    const tableHeight = 'h-55';
</script>

<div class="card w-2xl">
    <div class="flex items-center justify-between">
        <!-- The count spans BOTH modes: session.qsos is fed by the Phone/CW submit
             sink and the FT8 ft8-logged SSE alike, so it is the sitting's whole
             tally, not the current mode's. Muted parens match the header's
             "Logbook <name> (n)" treatment. -->
        <h3 class="text-sm font-semibold text-ink">
            Session <span class="font-normal text-muted"
                >({session.qsos.length.toLocaleString()})</span
            >
        </h3>
        <div class="flex items-center gap-x-2">
            <!-- No Map button here: the sidebar's bottom-utilities Map link is
                 always on screen and opens the identical new tab, so a second
                 entry point on this tile was pure duplication (operator,
                 2026-07-25). The map itself is still a standalone time-window
                 view launched in its own tab (ADR 0049 rejection). -->
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

    {#if sessionEdit.openError !== null}
        <p class="mt-2 text-xs text-invalid" data-testid="session-edit-error">
            {sessionEdit.openError}
        </p>
    {/if}

    <div class="mt-3">
        {#if session.qsos.length > 0}
            <div class="{tableHeight} overflow-y-auto">
                <!-- table-fixed: truncate on a td only works when column widths BIND —
                     auto layout grows cells to fit content and ignores w-* as a cap,
                     so long names/countries stretched the table instead of ellipsizing
                     (dogfood 2026-07-18). Fixed layout takes widths from the th row. -->
                <table class="w-full table-fixed">
                    <thead class="bg-surface sticky top-0 z-10">
                        <tr class="text-xs text-muted text-left font-medium">
                            <th class="w-20">Time</th>
                            <th class="w-27 overflow-x-hidden text-nowrap text-ellipsis"
                                >Callsign</th
                            >
                            <th class="w-12">Band</th>
                            <th class="w-14">Mode</th>
                            <th class="w-10">Sent</th>
                            <th class="w-10">Rcvd</th>
                            <th class="w-32">Name</th>
                            <th class="w-32">Country</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each session.qsos as q (q.id)}
                            <tr class="border-b text-sm border-line-soft text-ink last:border-0">
                                <td class="tabular-nums">{q.timeOn}</td>
                                <td class="font-medium">
                                    {#if q.uuid}
                                        <button
                                            type="button"
                                            class="cursor-pointer hover:underline"
                                            title="Edit this QSO (stays on this page — the FT8 run keeps going)"
                                            onclick={() => void sessionEdit.open(q)}
                                        >
                                            {q.callsign}
                                        </button>
                                    {:else}
                                        {q.callsign}
                                    {/if}
                                </td>
                                <td class="w-12">{q.band}</td>
                                <td class="w-14">{q.mode}</td>
                                <td class="tabular-nums">{q.rstSent}</td>
                                <td class="tabular-nums">{q.rstRcvd}</td>
                                <td class="w-32 overflow-hidden text-nowrap text-ellipsis"
                                    >{q.name}</td
                                >
                                <td class="w-32 overflow-hidden text-nowrap text-ellipsis pl-1"
                                    >{q.country}</td
                                >
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

{#if sessionEdit.row !== null}
    <EditQsoModal
        row={sessionEdit.row}
        saving={sessionEdit.saving}
        error={sessionEdit.error}
        onSave={(p: QsoPatch) => void sessionEdit.save(p)}
        onClose={() => sessionEdit.close()}
    />
{/if}
