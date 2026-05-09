<script lang="ts">
    /*
        Details panel — one of the four InfoPanel tabs. Mixes
        operator-typed per-QSO inputs (Power / Rig / Notes / Request
        QSL) with read-only displays of enrichment results (Email /
        Web Site / CQ Zone / ITU Zone) and a "Lookup on QRZ.com" link
        built from the current callsign.

        Field-source split (settled session 46):
          - Operator-typed (writes to qsoDraft): Power → ADIF RX_PWR,
            Rig → ADIF RIG, Notes → ADIF NOTES, Request QSL → custom
            APP_SM_REQUEST_QSL='Y' (operator's reminder to request a
            card; not a standard ADIF QSL_* tag).
          - Read-only display (reads from enrichmentState): Email,
            Web Site (with external-link launcher), CQ Zone, ITU Zone.
            "Look like fields, but not editable" — per-operator
            request, since an editable email/web duplicates what My
            Station / config already covers and risks divergence.
          - Link: "Lookup on QRZ.com" → https://www.qrz.com/db/<call>.
            Opens in a new tab; gated on a non-empty callsign.

        Layout: two-column flex. Left holds the editable trio + the
        request-QSL checkbox; right holds the four read-only displays
        + the QRZ.com link. Reuses .input-base and .textarea-base from
        app.css so styling matches the QSO panel's inputs.
    */
    import { qsoDraft } from '../../states/qsoDraft.svelte';
    import { enrichmentState } from '../../states/enrichment.svelte';

    /*
        Build the QRZ.com profile URL. www.qrz.com/db/<callsign> is
        the public profile page; logged-in users see more, but the
        bare URL works for everyone. encodeURIComponent guards against
        any character that slipped through the validator; in practice
        callsigns are ASCII alphanumerics + / -.
    */
    const qrzProfileUrl = $derived.by(() => {
        const c = qsoDraft.callsign.trim();
        if (c === '') return null;
        return `https://www.qrz.com/db/${encodeURIComponent(c.toUpperCase())}`;
    });

    function openExternal(url: string | null | undefined): void {
        if (url === null || url === undefined || url === '') return;
        window.open(url, '_blank', 'noopener,noreferrer');
    }
</script>

<!--
    External-link icon — Heroicon outline arrow-top-right-on-square,
    inlined to match the InfoPanel tab-icon pattern.
-->
{#snippet externalIcon()}
    <svg
        class="size-4"
        fill="none"
        viewBox="0 0 24 24"
        stroke-width="1.5"
        stroke="currentColor"
        aria-hidden="true"
    >
        <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M13.5 6H5.25A2.25 2.25 0 0 0 3 8.25v10.5A2.25 2.25 0 0 0 5.25 21h10.5A2.25 2.25 0 0 0 18 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25"
        />
    </svg>
{/snippet}

<div class="flex flex-row gap-x-6 px-4 py-4">
    <!-- Left column: operator-typed -->
    <div class="flex flex-col gap-y-3 w-1/2">
        <div class="flex flex-row gap-x-3">
            <div class="w-24">
                <label for="rx-pwr" class="input-label">Power</label>
                <input
                    id="rx-pwr"
                    type="text"
                    inputmode="numeric"
                    placeholder="RX Power"
                    bind:value={qsoDraft.rxPwr}
                    oninput={(e) => {
                        // Strip non-digits inline — matches the RST input pattern.
                        const t = e.currentTarget;
                        const cleaned = t.value.replace(/[^0-9]/g, '');
                        if (cleaned !== t.value) {
                            t.value = cleaned;
                            qsoDraft.rxPwr = cleaned;
                        }
                    }}
                    class="input-base mt-1"
                    autocomplete="off"
                />
            </div>
            <div class="flex-1">
                <label for="rig" class="input-label">Rig</label>
                <textarea
                    id="rig"
                    placeholder="Working conditions"
                    bind:value={qsoDraft.rig}
                    rows="3"
                    class="textarea-base mt-1 w-full"
                ></textarea>
            </div>
        </div>

        <div class="-mt-4">
            <label for="notes" class="input-label">Notes</label>
            <textarea
                id="notes"
                placeholder="My personal notes"
                bind:value={qsoDraft.notes}
                rows="3"
                class="textarea-base mt-1 w-full"
            ></textarea>
        </div>

        <div class="flex flex-row items-center gap-x-2 mt-1">
            <span class="input-label">Request QSL:</span>
            <input
                id="request-qsl"
                type="checkbox"
                bind:checked={qsoDraft.requestQsl}
                class="size-4"
            />
        </div>
    </div>

    <!-- Right column: read-only displays + QRZ link -->
    <div class="flex flex-col gap-y-3 w-1/2">
        <div>
            <span class="input-label">Email</span>
            <input
                type="text"
                placeholder="Email"
                value={enrichmentState.result?.station?.email ?? ''}
                readonly
                tabindex={-1}
                class="input-base mt-1 bg-surface-disabled cursor-default"
            />
        </div>

        <div>
            <span class="input-label">Web Site</span>
            <div class="flex flex-row items-center gap-x-2 mt-1">
                <input
                    type="text"
                    placeholder="Web site"
                    value={enrichmentState.result?.station?.web ?? ''}
                    readonly
                    tabindex={-1}
                    class="input-base bg-surface-disabled cursor-default"
                />
                <button
                    type="button"
                    aria-label="Open contact's website in a new tab"
                    title="Open in new tab"
                    onclick={() => openExternal(enrichmentState.result?.station?.web)}
                    disabled={!enrichmentState.result?.station?.web}
                    class="text-ink hover:text-focus disabled:text-gray-400 disabled:cursor-not-allowed"
                >
                    {@render externalIcon()}
                </button>
            </div>
        </div>

        <div class="flex flex-row items-center gap-x-2 text-sm">
            <button
                type="button"
                onclick={() => openExternal(qrzProfileUrl)}
                disabled={qrzProfileUrl === null}
                class="inline-flex items-center gap-x-1 text-ink hover:text-focus disabled:text-gray-400 cursor-pointer disabled:cursor-not-allowed"
            >
                <span>Lookup on QRZ.com</span>
                {@render externalIcon()}
            </button>
        </div>

        <div class="text-sm">
            <span class="font-semibold">CQ Zone:</span>
            <span class="ml-1 tabular-nums">{enrichmentState.result?.country?.cq_zone ?? ''}</span>
        </div>

        <div class="text-sm">
            <span class="font-semibold">ITU Zone:</span>
            <span class="ml-1 tabular-nums">{enrichmentState.result?.country?.itu_zone ?? ''}</span>
        </div>
    </div>
</div>
