<script lang="ts">
    // Enrichment section — callsign/country lookup providers + cache freshness
    // (ADR 0017). Ported from the standalone config SPA's Enrichment tab.
    //
    // Provider-SPECIFIC by choice (QRZ + Hamnut), not a generic chain editor:
    // those are the only two implemented, and unlike Forwarding there is no
    // descriptor endpoint to drive a data-driven form from. The save still
    // round-trips every provider — see enrichment.svelte.ts.
    import { onMount } from 'svelte';
    import { enrichmentState } from './enrichment.svelte';
    import MaskedField from './MaskedField.svelte';

    onMount(() => void enrichmentState.load());

    // Digits only, at the keystroke: blank must survive as blank (it is what
    // asks the daemon for its default), and a coerce would turn "abc" into a
    // number the operator never chose.
    function digitsOnly(e: Event, set: (v: string) => void): void {
        const el = e.currentTarget as HTMLInputElement;
        const cleaned = el.value.replace(/\D/g, '');
        el.value = cleaned;
        set(cleaned);
    }

    // Which TTL boxes hold an explicit 0 — named, so the notice says WHICH
    // cache is affected rather than making the operator work it out.
    const zeroTtls = $derived(
        [
            { label: 'Country TTL', v: enrichmentState.draft.countryTtlDays },
            { label: 'Station TTL', v: enrichmentState.draft.stationTtlDays },
        ]
            .filter((t) => t.v === '0')
            .map((t) => t.label)
    );
</script>

<div class="mx-auto max-w-3xl">
    {#if !enrichmentState.loaded && enrichmentState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !enrichmentState.loaded && enrichmentState.error}
        <div class="card">
            <p class="text-sm text-ink">
                Couldn’t load enrichment settings: {enrichmentState.error}
            </p>
            <button class="btn mt-3" onclick={() => enrichmentState.load()}>Retry</button>
        </div>
    {:else}
        <div class="space-y-8">
            <p class="text-sm text-muted">
                Where Station Manager fills in a contacted station’s details. Lookups never block
                logging — when a provider is unreachable the QSO is logged with whatever is known.
            </p>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Callsign lookup — QRZ.com</h2>
                <p class="text-sm text-muted">
                    Fills name, grid and address from QRZ. Needs a QRZ subscription with XML/API
                    access.
                </p>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={enrichmentState.draft.qrzEnabled}
                    />
                    Enabled
                </label>
                <label class="flex w-72 flex-col gap-1">
                    <span class="text-sm font-medium text-ink">Username</span>
                    <input
                        class="input"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={enrichmentState.draft.qrzUsername}
                    />
                </label>
                <label class="flex w-72 flex-col gap-1">
                    <span class="text-sm font-medium text-ink">Password</span>
                    <MaskedField
                        value={enrichmentState.draft.qrzPassword}
                        oninput={(v: string) => enrichmentState.setQrzPassword(v)}
                        placeholder={enrichmentState.draft.qrzPasswordSet
                            ? '•••••••• (set — leave blank to keep)'
                            : ''}
                    />
                </label>

                <!-- Removal is a third state the box cannot express: it looks
                     identical whether blank means "keep" or "erase". Offered
                     only when something is stored — a control that appears to
                     work and does nothing teaches the operator it worked. -->
                {#if enrichmentState.draft.qrzPasswordSet}
                    {#if enrichmentState.draft.qrzPasswordCleared}
                        <div
                            class="flex w-72 flex-col gap-2 rounded-md border border-warning bg-surface-muted px-3 py-2"
                        >
                            <span class="text-xs text-warning">
                                The stored QRZ password will be removed when you save. Lookups will
                                fail until a new one is entered.
                            </span>
                            <button
                                class="btn self-start"
                                type="button"
                                onclick={() => enrichmentState.keepQrzPassword()}
                            >
                                Keep stored password
                            </button>
                        </div>
                    {:else}
                        <button
                            class="btn self-start"
                            type="button"
                            onclick={() => enrichmentState.clearQrzPassword()}
                        >
                            Remove stored password
                        </button>
                    {/if}
                {/if}
            </section>

            <section class="space-y-3">
                <h2 class="text-base font-semibold text-ink">Country lookup — Hamnut</h2>
                <p class="text-sm text-muted">
                    Resolves DXCC / CQ / ITU zones from the callsign prefix. Free and anonymous — no
                    credentials needed.
                </p>
                <label class="flex items-center gap-2 text-sm text-ink">
                    <input
                        type="checkbox"
                        class="cursor-pointer"
                        bind:checked={enrichmentState.draft.hamnutEnabled}
                    />
                    Enabled
                </label>
            </section>

            <section>
                <h2 class="mb-3 text-base font-semibold text-ink">Cache freshness</h2>
                <div class="flex flex-wrap gap-x-4 gap-y-3">
                    <label class="flex w-40 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Country TTL (days)</span>
                        <input
                            class="input"
                            inputmode="numeric"
                            placeholder="365"
                            value={enrichmentState.draft.countryTtlDays}
                            oninput={(e) =>
                                digitsOnly(e, (v) => (enrichmentState.draft.countryTtlDays = v))}
                        />
                    </label>
                    <label class="flex w-40 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Station TTL (days)</span>
                        <input
                            class="input"
                            inputmode="numeric"
                            placeholder="90"
                            value={enrichmentState.draft.stationTtlDays}
                            oninput={(e) =>
                                digitsOnly(e, (v) => (enrichmentState.draft.stationTtlDays = v))}
                        />
                    </label>
                    <label class="flex w-40 flex-col gap-1">
                        <span class="text-sm font-medium text-ink">Max refresh in flight</span>
                        <input
                            class="input"
                            inputmode="numeric"
                            placeholder="4"
                            value={enrichmentState.draft.refreshMaxInFlight}
                            oninput={(e) =>
                                digitsOnly(
                                    e,
                                    (v) => (enrichmentState.draft.refreshMaxInFlight = v)
                                )}
                        />
                    </label>
                </div>
                <p class="mt-2 text-xs text-muted">
                    A TTL decides when a cached country or station record is re-fetched on next use.
                    Leave a box blank to use the default shown.
                </p>

                <!-- 0 and blank are OPPOSITE instructions in the same box —
                     blank takes the daemon default, 0 disables staleness
                     entirely — so a 0 has to say what it means. Shown only when
                     a 0 is actually entered, otherwise it reads as decoration
                     rather than a statement about this value. -->
                {#if zeroTtls.length > 0}
                    <p class="mt-2 text-xs text-warning">
                        {zeroTtls.join(' and ')}
                        {zeroTtls.length > 1 ? 'are' : 'is'} set to 0: that cache never goes stale, so
                        records are kept until something else replaces them.
                    </p>
                {/if}
            </section>

            {#if enrichmentState.dirty}
                <div
                    class="rounded-md border border-warning bg-surface-muted px-3 py-2 text-sm text-warning"
                >
                    ⚠ Enrichment changes apply when the daemon restarts — the lookup providers bind
                    at startup.
                </div>
            {/if}

            <div class="flex items-center gap-3 border-t border-line pt-4">
                <button
                    class="btn btn-primary"
                    disabled={!enrichmentState.dirty || enrichmentState.saving}
                    onclick={() => enrichmentState.save()}
                >
                    {enrichmentState.saving ? 'Saving…' : 'Save'}
                </button>
                <button
                    class="btn"
                    disabled={!enrichmentState.dirty || enrichmentState.saving}
                    onclick={() => enrichmentState.reset()}
                >
                    Cancel
                </button>
                {#if enrichmentState.dirty}
                    <span class="text-xs text-muted">Unsaved changes</span>
                {/if}
            </div>
        </div>
    {/if}
</div>
