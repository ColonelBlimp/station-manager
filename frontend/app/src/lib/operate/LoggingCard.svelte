<script lang="ts">
    // Phone/CW logging card — the fast-path entry fields (callsign, RST, time,
    // name) plus a comment line. Rarely touched lookups (QTH, grid) live in the
    // Details card; the grid is enrichment-filled into the draft, not typed here.
    //
    // The right column hosts EnrichmentCard (flag / DXCC + NEW / bearing SP-LP /
    // distance — mirrors the FT8 Band-Activity enrichment). This card is only its
    // host, not its owner: EnrichmentCard fills whatever box it's given, so a
    // future drag/pin layout can relocate it without touching either component.
    //
    // Reads/writes the shared QSO draft (qso.svelte); it does NOT submit directly
    // (logDraft() calls the injected sink) and makes no assumption about where it's
    // positioned (ADR 0045). Rig fields (freq/mode/band) belong to the Rig panel.
    import { onMount } from 'svelte';
    import { draft, canLog, logDraft, resetDraft, stampOn } from './qso.svelte';
    import { observeWorked } from './worked.svelte';
    import { rigReady } from './rig.svelte';
    import EnrichmentCard from './EnrichmentCard.svelte';

    function upperCall(): void {
        draft.callsign = draft.callsign.toUpperCase();
    }

    // The worked-before lookup is driven here, not in WorkedPanel: it must run
    // while the panel is CLOSED so a hit can auto-open it. This card owns the
    // callsign field, so it hosts the observation. (Enrichment observes from
    // EnrichmentCard, which is always mounted.)
    $effect(() => observeWorked(draft.callsign));

    // QSO start = when the card appears. Time off is stamped at log.
    onMount(stampOn);
</script>

<div class="card w-(--card-w)">
    <div class="flex gap-x-6">
        <!-- Left: fast-path entry fields -->
        <div class="flex flex-col">
            <div class="flex items-end gap-x-2">
                <div>
                    <label for="lc-call" class="block text-sm font-medium text-ink">Callsign</label>
                    <input
                        id="lc-call"
                        class="input w-32 uppercase"
                        autocomplete="off"
                        spellcheck="false"
                        placeholder="Callsign"
                        bind:value={draft.callsign}
                        oninput={upperCall}
                    />
                </div>
                <div>
                    <label for="lc-rst-s" class="block text-sm font-medium text-ink">RST Sent</label
                    >
                    <input id="lc-rst-s" class="input w-15" bind:value={draft.rstSent} />
                </div>
                <div>
                    <label for="lc-rst-r" class="block text-sm font-medium text-ink">RST Rcvd</label
                    >
                    <input id="lc-rst-r" class="input w-15" bind:value={draft.rstRcvd} />
                </div>
            </div>

            <div class="mt-2 flex items-end gap-x-2">
                <div>
                    <label for="lc-date-on" class="block text-sm font-medium text-ink"
                        >Date On</label
                    >
                    <input
                        id="lc-date-on"
                        class="input w-32"
                        placeholder="YYYY-MM-DD"
                        bind:value={draft.dateOn}
                    />
                </div>
                <div>
                    <label for="lc-time-on" class="block text-sm font-medium text-ink"
                        >Time On</label
                    >
                    <input
                        id="lc-time-on"
                        class="input w-24"
                        placeholder="HH:MM:SS"
                        bind:value={draft.timeOn}
                    />
                </div>
            </div>

            <div class="mt-2 flex items-end gap-2">
                <div>
                    <label for="lc-date-off" class="block text-sm font-medium text-ink"
                        >Date Off</label
                    >
                    <input
                        id="lc-date-off"
                        class="input w-32"
                        placeholder="YYYY-MM-DD"
                        bind:value={draft.dateOff}
                    />
                </div>
                <div>
                    <label for="lc-time-off" class="block text-sm font-medium text-ink"
                        >Time Off</label
                    >
                    <input
                        id="lc-time-off"
                        class="input w-24"
                        placeholder="HH:MM:SS"
                        bind:value={draft.timeOff}
                    />
                </div>
            </div>

            <div class="mt-2">
                <label for="lc-name" class="block text-sm font-medium text-ink">Name</label>
                <input id="lc-name" class="input w-60" autocomplete="off" bind:value={draft.name} />
            </div>

            <div class="mt-2">
                <label for="lc-comment" class="block text-sm font-medium text-ink">Comment</label>
                <input
                    id="lc-comment"
                    class="input w-60"
                    autocomplete="off"
                    bind:value={draft.comment}
                />
            </div>
        </div>

        <!-- Right: always-on enrichment (relocatable — this square is just its first host) -->
        <div class="flex flex-col">
            <div class="flex w-56 h-43 shrink-0 mt-0">
                <EnrichmentCard />
            </div>
            <div class="flex mt-auto justify-end gap-x-2">
                <button class="btn" onclick={resetDraft}>Clear</button>
                <!-- CAT gate: a lost rig link blocks logging (the band/mode on
                     record could be stale); CAT-off manual entry logs fine. -->
                <button
                    class="btn btn-primary"
                    onclick={logDraft}
                    disabled={!canLog() || !rigReady()}
                    title={!rigReady() ? 'CAT link lost — rig context may be stale' : undefined}
                    >Log QSO</button
                >
            </div>
        </div>
    </div>
</div>
