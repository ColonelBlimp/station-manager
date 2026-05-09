<script lang="ts">
    /*
        SessionPanel — list of QSOs logged during the current operator
        session.

        Mirrors WorkedPanel's table conventions (tabular-nums, header
        row + body rows, scroll under a fixed header) but the columns
        differ: this is the operator's own session record (with
        country and active-path distance) rather than the per-callsign
        prior-contacts view.

        Stage B scope: render the table. The recipient input + send
        icon (top-right of the InfoPanel header) and the row-click-to-
        edit overlay land in later stages; this panel is intentionally
        read-only for now.

        Empty state: "No QSOs logged this session." Distinguished from
        WorkedPanel's `null` vs `[]` by SessionPanel using a single
        always-present array — sessionQsosState.items always reads as
        an array (possibly empty), never null, so one empty-array
        check covers it.
    */
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { formatFrequency } from '../../utils/frequency';

    /*
        Reverse for newest-first display without mutating the
        underlying array (which is in submit order — the order
        the email-ADIF flow wants when concatenating records).
        slice() copies; reverse mutates the copy.
    */
    const rows = $derived(sessionQsosState.items.slice().reverse());

    /*
        ADIF date YYYYMMDD → operator-readable YYYY-MM-DD. qsoDraft
        already stores it in dashed form, but the field is named
        ambiguously — use a dedicated helper so future schema changes
        don't have to chase usages.
    */
    const formatDate = (d: string): string => {
        if (d.length === 8) {
            return `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}`;
        }
        return d;
    };

    const formatTime = (t: string): string => {
        if (t.length === 4) {
            return `${t.slice(0, 2)}:${t.slice(2, 4)}`;
        }
        return t;
    };

    /*
        Distance display: numeric km with " km" suffix; empty cell
        when no value (grids missing on either side at submit time).
    */
    const formatDistance = (d: string): string => (d === '' ? '' : `${d} km`);
</script>

<div class="w-full pt-3">
    {#if rows.length === 0}
        <p class="text-sm text-gray-500 italic px-1 py-2">No QSOs logged this session.</p>
    {:else}
        <table class="w-full text-left text-sm tabular-nums">
            <thead class="border-b border-gray-300">
                <tr class="text-gray-700 font-semibold">
                    <th class="py-1 pr-4">Callsign</th>
                    <th class="py-1 pr-4">Name</th>
                    <th class="py-1 pr-4">Freq</th>
                    <th class="py-1 pr-4">Band</th>
                    <th class="py-1 pr-4">Send</th>
                    <th class="py-1 pr-4">Rcvd</th>
                    <th class="py-1 pr-4">Mode</th>
                    <th class="py-1 pr-4">Time On</th>
                    <th class="py-1 pr-4">Country</th>
                    <th class="py-1 pr-4">Distance</th>
                </tr>
            </thead>
            <tbody>
                {#each rows as row (row.uuid)}
                    <tr class="border-b border-gray-100 last:border-0">
                        <td class="py-1 pr-4 font-semibold">{row.callsign}</td>
                        <td class="py-1 pr-4">{row.name}</td>
                        <td class="py-1 pr-4">{formatFrequency(row.freqHz)}</td>
                        <td class="py-1 pr-4">{row.band}</td>
                        <td class="py-1 pr-4">{row.rstSent}</td>
                        <td class="py-1 pr-4">{row.rstRcvd}</td>
                        <td class="py-1 pr-4">{row.mode}</td>
                        <td class="py-1 pr-4">
                            {formatDate(row.qsoDate)}
                            {formatTime(row.timeOn)}
                        </td>
                        <td class="py-1 pr-4">{row.country}</td>
                        <td class="py-1 pr-4">{formatDistance(row.distanceKm)}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {/if}
</div>
