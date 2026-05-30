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
        <!--
            overflow-x-auto so a wide (10-column) session table scrolls
            horizontally INSIDE the panel instead of widening the app
            shell. The shell is `w-fit` (app.svelte <main>), so an
            unconstrained table grew the whole card once any QSO landed,
            which dragged the InfoPanel tab strip wider and pushed the
            Session-tab email controls out of view. Containing the table
            here keeps the card — and the tab strip — at a stable width.
        -->
        <div class="overflow-x-auto">
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
                        <th class="py-1 pr-4 sr-only">Actions</th>
                    </tr>
                </thead>
                <tbody>
                    {#each rows as row (row.uuid)}
                        <!--
                        Edit affordance lives in a trailing cell rather
                        than as a role-on-row. role="button" on <tr>
                        overrode the implicit row semantics, so screen
                        readers stopped announcing cell-by-cell and the
                        gridcell relationship was lost. Inline button
                        per row preserves proper table semantics; the
                        explicit Edit control is also a clearer
                        keyboard target than a row-wide hit area.
                    -->
                        <tr class="border-b border-line-soft last:border-0">
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
                            <td class="py-1 pr-2">
                                <button
                                    type="button"
                                    class="font-bold text-indigo-700 hover:text-indigo-900 cursor-pointer"
                                    aria-label={`Edit QSO with ${row.callsign}`}
                                    onclick={() => void openEdit(row.uuid)}
                                >
                                    Edit
                                </button>
                            </td>
                        </tr>
                    {/each}
                </tbody>
            </table>
        </div>
    {/if}
</div>

<!--
    Edit overlay — rendered as a sibling so its fixed-position backdrop
    sits above the rest of the UI rather than inside the panel's own
    overflow / clipping context. The overlay self-gates on
    qsoEditState.open so it's invisible until a row click fires.
-->
<QsoEditOverlay />
