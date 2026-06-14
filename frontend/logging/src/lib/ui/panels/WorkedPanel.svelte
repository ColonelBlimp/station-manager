<script lang="ts">
    /*
        Worked panel — the first InfoPanel tab. Answers "have I worked
        this callsign before, and where?". Reads from
        `contactHistoryState` (set by `QsoPanel.handleEnrich` after
        the daemon's `/v1/contact-history?call=X` returns; cleared on
        submit-stored and on Clear).

        Empty states are deliberately distinct:
          - `items === null`  — no Tab has fetched yet (or post-clear).
                                Show nothing; the parent card stays
                                empty so it doesn't read as "I've
                                checked, you have no prior contacts."
          - `items === []`    — fetch returned, no prior contacts. A
                                short "no prior contacts" hint reads
                                as a positive signal ("new to you")
                                rather than a missing panel.

        Layout: simple table. Newest QSO first (the daemon orders
        results that way). Date, Time, Band, Mode, RST sent / rcvd —
        the columns operators check most often when deciding "is this
        a new QSO or a duplicate". `font-mono tabular-nums` keeps
        digit columns aligned at small sizes.
    */
    import { contactHistoryState } from '../../states/contactHistory.svelte';
    import { t } from '../../i18n';

    /*
        Format ADIF YYYYMMDD as YYYY-MM-DD for display. Cheap; no
        Date constructor needed (avoids browser-TZ surprises on a
        date-only value).
    */
    function formatQsoDate(yyyymmdd: string): string {
        if (yyyymmdd.length !== 8) return yyyymmdd;
        return `${yyyymmdd.slice(0, 4)}-${yyyymmdd.slice(4, 6)}-${yyyymmdd.slice(6, 8)}`;
    }

    /*
        Format ADIF HHMM as HH:MM. Same rationale as formatQsoDate —
        no Date round-trip.
    */
    function formatQsoTime(hhmm: string): string {
        if (hhmm.length !== 4) return hhmm;
        return `${hhmm.slice(0, 2)}:${hhmm.slice(2, 4)}`;
    }
</script>

<div class="px-2">
    {#if contactHistoryState.items === null}
        <!-- No fetch yet — render nothing; the tab area stays clean.
             Pre-Tab the operator hasn't asked the question. -->
    {:else if contactHistoryState.items.length === 0}
        <p class="text-sm text-ink">{t('worked.empty')}</p>
    {:else}
        <div class="overflow-x-auto overflow-y-scroll h-67">
            <table class="w-full text-sm tabular-nums">
                <thead>
                    <tr class="border-b border-line text-left">
                        <th class="w-24 px-2 py-1 font-semibold">Date</th>
                        <th class="w-16 px-2 py-1 font-semibold">Time</th>
                        <th class="w-16 px-2 py-1 font-semibold">Band</th>
                        <th class="w-20 px-2 py-1 font-semibold">Mode</th>
                        <th class="w-10 px-2 py-1 font-semibold">Sent</th>
                        <th class="w-10 px-2 py-1 font-semibold">Rcvd</th>
                        <th class="px-2 py-1 font-semibold">Notes</th>
                    </tr>
                </thead>
                <tbody>
                    {#each contactHistoryState.items as qso (qso.uuid)}
                        <tr class="border-b border-line-soft">
                            <td class="px-2 py-1">{formatQsoDate(qso.qso_date)}</td>
                            <td class="px-2 py-1">{formatQsoTime(qso.time_on)}</td>
                            <td class="px-2 py-1">{qso.band}</td>
                            <td class="px-2 py-1">{qso.mode}</td>
                            <td class="px-2 py-1">{qso.rst_sent}</td>
                            <td class="px-2 py-1">{qso.rst_rcvd}</td>
                            <td class="px-2 py-1">{qso.notes}</td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}
</div>
