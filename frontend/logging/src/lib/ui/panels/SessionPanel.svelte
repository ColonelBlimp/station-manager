<script lang="ts">
    /*
        SessionPanel — list of QSOs logged during the current operator
        session.

        Mirrors WorkedPanel's table conventions (tabular-nums, header
        row + body rows, scroll under a fixed header) but the columns
        differ: this is the operator's own session record (with
        country and active-path distance) rather than the per-callsign
        prior-contacts view.

        Row click opens the QsoEditOverlay (Stage D) — a modal-style
        independent edit form. The session list is the only entry
        point to historical edits today; a future "logbook" view may
        share the same overlay.

        Empty state: "No QSOs logged this session." Distinguished from
        WorkedPanel's `null` vs `[]` by SessionPanel using a single
        always-present array — sessionQsosState.items always reads as
        an array (possibly empty), never null, so one empty-array
        check covers it.
    */
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { qsoEditState } from '../../states/qsoEdit.svelte';
    import { fetchQso } from '../../api/qso-update';
    import { toasts } from '../../states/toasts.svelte';
    import { formatFrequency } from '../../utils/frequency';
    import QsoEditOverlay from '../components/QsoEditOverlay.svelte';

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

    /*
        Open the edit overlay for the row's UUID. beginOpen() flips the
        modal into a loading state immediately so the operator gets
        instant feedback; populate() arrives after the GET resolves.
        Failure paths (not_found / network / server) toast and reset.
    */
    async function openEdit(uuid: string): Promise<void> {
        if (qsoEditState.open) return; // guard against double-click
        qsoEditState.beginOpen(uuid);
        const outcome = await fetchQso(uuid);
        switch (outcome.kind) {
            case 'ok':
                qsoEditState.populate(outcome.qso);
                break;
            case 'not_found':
                toasts.error('QSO no longer exists');
                qsoEditState.close();
                break;
            case 'network':
                toasts.error('Cannot reach daemon');
                qsoEditState.close();
                break;
            case 'validation':
                toasts.error(outcome.message);
                qsoEditState.close();
                break;
            case 'server':
                toasts.error(`Failed to load: ${outcome.message}`);
                qsoEditState.close();
                break;
        }
    }
</script>

<div class="px-2">
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
                    <!--
                        The whole row is clickable. <tr> can't host a button
                        role cleanly inside a <table> without breaking the
                        table semantics, so the row uses role="button" with
                        keyboard handlers. Hover indicates clickability;
                        focus state borrows the row's existing border.
                    -->
                    <tr
                        class="border-b border-gray-100 last:border-0 hover:bg-indigo-50 focus:bg-indigo-50 cursor-pointer"
                        role="button"
                        tabindex="0"
                        onclick={() => void openEdit(row.uuid)}
                        onkeydown={(e) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                                e.preventDefault();
                                void openEdit(row.uuid);
                            }
                        }}
                    >
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

<!--
    Edit overlay — rendered as a sibling so its fixed-position backdrop
    sits above the rest of the UI rather than inside the panel's own
    overflow / clipping context. The overlay self-gates on
    qsoEditState.open so it's invisible until a row click fires.
-->
<QsoEditOverlay />
