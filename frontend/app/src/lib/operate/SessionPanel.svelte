<script lang="ts">
    // Session table: the contacts of the daemon's operating session, newest
    // first (W-0029 part 6). Pure presentation over what the page has read of
    // that session (session.svelte.ts): this window adds no row of its own.
    // Self-contained tile (ADR 0045/0046): owns its own header (title + the
    // Export… action, which used to live on the InfoPanel wrapper).
    //
    // What it says when there are no rows depends on WHY (criteria B7, C): no
    // session is open; the archive is being switched; the rows are still being
    // read; they could not be read; or the session has no contact. Only the
    // last is "No QSOs logged this session".
    //
    // In-place edit (dogfood 2026-07-18): the callsign is a button that opens
    // the shared EditQsoModal RIGHT HERE — navigating to the Logbook route to
    // edit unmounts the FT8 view and takes a live CQ run off the air
    // (demand-driven capture). Rows without a uuid (pre-upgrade sessions)
    // render as plain text.
    //
    // The controls (W-0029 part 6, slice 3; criteria E, F): "Continue…" opens
    // the list of earlier sessions, with the ruled sentence beside it. It is
    // offered only where it can act (a session open, or none) and only when
    // there is something to continue or the list could not be read. What a
    // control that was not applied came to is said on a line here, in the
    // words given. And when the page could not find out what a control did
    // (session.control "unknown"; the operator's ruling 1, 2026-11-27) the
    // panel says so, says the rows are as last read, and offers "Try again",
    // which reads the state once and sends no control.
    import { session } from './session.svelte';
    import { recovery, readAfterUnknown } from '../recovery.svelte';
    import { continueList, openContinue, closeContinue } from './continueList.svelte';
    import ContinueDialog from './ContinueDialog.svelte';
    import { sessionEdit } from './sessionEdit.svelte';
    import { openExport, focusCallsign } from './state.svelte';
    import { hideTile } from './layout.svelte';
    import { formatSessionStart } from '../utils/time';
    import { daemonNowMs } from '../api/daemonClock.svelte';
    import { onDestroy } from 'svelte';
    import EditQsoModal from '../logbook/EditQsoModal.svelte';
    import type { QsoPatch } from '../api/qso-patch';
    const tableHeight = 'h-55';

    // Callsign search (dogfood 2026-08-07) — a VIEW over the sitting's rows,
    // never a mutation: the header count stays the total. Substring, not
    // prefix, because this is a "did/when did I work them?" lookup, not the
    // Band Activity funnel (which is deliberately prefix — "calls starting
    // with VK"). Case folded both sides.
    let callsignSearch = $state('');
    const shownQsos = $derived.by(() => {
        const q = callsignSearch.trim().toUpperCase();
        if (q === '') return session.qsos;
        return session.qsos.filter((r) => r.callsign.toUpperCase().includes(q));
    });

    // The session's first start (ruling 1): for a continued session it is days
    // before the sitting the header's clock counts. Whether it is "today's" is
    // judged now, on the daemon's clock, and again each second: a panel left
    // open across midnight UTC comes to say the date.
    let nowMs = $state(daemonNowMs());
    const startTicker = setInterval(() => {
        nowMs = daemonNowMs();
    }, 1000);
    onDestroy(() => clearInterval(startTicker));
    // The dialog lives in this panel: with the panel gone it is closed.
    onDestroy(closeContinue);

    const controlsOffered = $derived(session.state === 'open' || session.state === 'none');
    const continueOffered = $derived(
        controlsOffered &&
            (continueList.available === 'some' || continueList.available === 'unreadable')
    );
    let retrying = $state(false);
    async function tryAgain(): Promise<void> {
        if (retrying) return;
        retrying = true;
        try {
            await readAfterUnknown();
        } finally {
            retrying = false;
        }
    }
    const started = $derived(
        session.sessionId !== null && session.openedAt !== null
            ? formatSessionStart(session.openedAt, new Date(nowMs))
            : ''
    );

    // Why there are no rows to show; '' when there are rows.
    const emptyReason = $derived.by(() => {
        if (session.qsos.length > 0) return '';
        // Which session is open is not known: nothing below may say it is
        // empty, or that none is open.
        if (session.control === 'unknown') return 'The session could not be read.';
        if (session.state === 'archive_switching') return 'The archive is being switched.';
        if (session.sessionId === null) return 'No session is open.';
        if (session.rows === 'failed') {
            return `The session's contacts could not be read. ${session.error}`.trim();
        }
        if (session.rows === 'loading') return 'Reading the session…';
        return 'No QSOs logged this session.';
    });
</script>

<div class="card w-2xl">
    <div class="flex items-center justify-between">
        <!-- The count spans BOTH modes: it is the number of the session's
             contacts the daemon holds, whichever mode logged them. Muted parens
             match the header's "Logbook <name> (n)" treatment. -->
        <h3 class="text-sm font-semibold text-ink">
            Session <span class="font-normal text-muted"
                >({session.qsos.length.toLocaleString()})</span
            >
            <!-- While a control's outcome is not known the count is as last
                 read, whether or not there are rows under it: "Session (0)"
                 would be a claim about a session the page does not know
                 (codex-291). -->
            {#if session.control === 'unknown'}
                <span class="ml-1 text-xs font-normal text-warning" data-testid="session-last-read"
                    >last read</span
                >
            {/if}
            {#if started !== ''}
                <span class="ml-2 text-xs font-normal text-muted" data-testid="session-started"
                    >Started {started}</span
                >
            {/if}
        </h3>
        <div class="flex items-center gap-x-2">
            <!-- No Map button here: the sidebar's bottom-utilities Map link is
                 always on screen and opens the identical new tab, so a second
                 entry point on this tile was pure duplication (operator,
                 2026-07-25). The map itself is still a standalone time-window
                 view launched in its own tab (ADR 0049 rejection). -->
            <input
                type="search"
                bind:value={callsignSearch}
                placeholder="Find call"
                aria-label="Search session callsigns"
                class="w-24 rounded border border-line bg-surface px-2 py-1 text-xs text-ink uppercase focus:ring-2 focus:ring-focus-ring focus:outline-none"
            />
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

    {#if continueOffered}
        <div class="mt-2 flex items-center gap-x-3" data-testid="session-controls">
            <button
                class="btn text-xs"
                title="Continue an earlier session"
                disabled={session.control !== ''}
                onclick={openContinue}
            >
                Continue…
            </button>
            <span class="text-xs text-muted">Contacts stay in the session they were logged in.</span
            >
        </div>
    {/if}
    {#if session.control === 'unknown'}
        <p class="mt-2 text-xs text-invalid" role="alert" data-testid="session-unknown">
            <!-- What the control came to stays said; why the session could not
                 be read is said beside it, never in its place (codex-291). -->
            {recovery.controlError} The session could not be read{recovery.unreadError === ''
                ? '.'
                : `: ${recovery.unreadError}`}
            {#if session.qsos.length > 0}The contacts below are as last read.{/if}
            <button
                type="button"
                class="ml-1 cursor-pointer underline disabled:cursor-not-allowed disabled:opacity-50"
                disabled={retrying}
                onclick={() => void tryAgain()}>Try again</button
            >
        </p>
    {:else if recovery.controlError !== ''}
        <p class="mt-2 text-xs text-invalid" role="alert" data-testid="session-control-error">
            {recovery.controlError}
        </p>
    {/if}

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
                     (dogfood 2026-07-18). Fixed layout takes widths from the th row.
                     Name and Country state NO width: they share what the other six
                     leave, so the table is exactly as wide as the list. With a width
                     on all eight the columns added up to 628px, 10px more than the
                     618px beside the vertical scrollbar, and the list scrolled
                     sideways (dogfood 2026-11-13; measured in Electron 44). -->
                <table class="w-full table-fixed">
                    <thead class="bg-surface sticky top-0 z-10">
                        <tr class="text-xs text-muted text-left font-medium">
                            <th class="w-20">Time</th>
                            <!-- 136px: the widest 13-character callsigns measured drew 133px
                                 at this weight (KH0/WA6MWM/MM); at the earlier 108px they ran
                                 over Band (operator, 2026-11-16). -->
                            <th class="w-34">Callsign</th>
                            <th class="w-12">Band</th>
                            <th class="w-14">Mode</th>
                            <th class="w-10">Sent</th>
                            <th class="w-10">Rcvd</th>
                            <th>Name</th>
                            <th>Country</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each shownQsos as q (q.id)}
                            <tr class="border-b text-sm border-line-soft text-ink last:border-0">
                                <td class="tabular-nums">{q.timeOn}</td>
                                <!-- The backstop for a callsign wider than its column: cut
                                     short with an ellipsis, never drawn over Band, with the
                                     whole callsign in the tooltip. The button is an inline
                                     box of its own, so it is held to the cell's width and
                                     clipped itself; align-bottom keeps the row's height, which
                                     a clipped inline box would otherwise change. -->
                                <td
                                    class="overflow-hidden font-medium text-nowrap text-ellipsis"
                                    title={q.callsign}
                                >
                                    {#if q.uuid}
                                        <button
                                            type="button"
                                            class="max-w-full cursor-pointer overflow-hidden align-bottom text-nowrap text-ellipsis hover:underline"
                                            title="{q.callsign} — edit this QSO (stays on this page; the FT8 run keeps going)"
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
                                <td class="overflow-hidden text-nowrap text-ellipsis">{q.name}</td>
                                <td class="overflow-hidden text-nowrap text-ellipsis pl-1"
                                    >{q.country}</td
                                >
                            </tr>
                        {/each}
                    </tbody>
                </table>
                <!-- The no-match state must not look like an empty session — a
                     filter that hides everything and says nothing reads as "no
                     QSOs logged", the nearest confusable state. -->
                {#if shownQsos.length === 0}
                    <div class="py-4 text-center text-sm text-muted">
                        No callsigns match “{callsignSearch.trim()}”.
                    </div>
                {/if}
            </div>
        {:else}
            <div
                class="flex {tableHeight} items-center justify-center text-center text-sm text-muted"
                data-testid="session-empty"
            >
                {emptyReason}
            </div>
        {/if}
    </div>
</div>

<ContinueDialog />

{#if sessionEdit.row !== null}
    <EditQsoModal
        row={sessionEdit.row}
        saving={sessionEdit.saving}
        error={sessionEdit.error}
        onSave={(p: QsoPatch) => void sessionEdit.save(p)}
        onClose={() => sessionEdit.close()}
    />
{/if}
