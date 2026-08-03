<script lang="ts">
    // Enrichment section — callsign/country lookup sources + cache freshness
    // (ADR 0017). Ported from the standalone config SPA's Enrichment tab.
    //
    // One disclosure per SOURCE, the Forwarding shape (operator, 2026-08-03):
    // the list grows with every provider the daemon gains, so everything
    // expanded is a page that only gets longer, and the summary has to carry
    // enough that collapsing loses nothing an operator scans for — which
    // service, whether it is on, whether this build can edit it, and whether it
    // has unsaved changes.
    //
    // Unlike Forwarding there is no descriptor endpoint, so the friendly name
    // and blurb come from a small map in enrichment.svelte.ts. A provider not in
    // it still renders, with the uniform wire fields — LookupProviderInfo has a
    // fixed shape, so only the presentation is unknown, not the form.
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
                logging — when a source is unreachable the QSO is logged with whatever is known.
                Passwords are stored on the daemon and never sent back to the browser, so leaving
                one blank keeps the saved value.
            </p>

            {#each enrichmentState.draft.providers as p (p.name)}
                {@const meta = enrichmentState.metaFor(p.name)}
                {@const edited = enrichmentState.hasEdits(p.name)}
                <!-- The EFFECTIVE enabled state, shared by the summary pill and
                     the toggle below so the two can never disagree: a collapsed
                     card is all an operator sees for most sources. -->
                {@const on = enrichmentState.effectiveEnabled(p)}
                <details class="rounded-md border border-line" open={edited || undefined}>
                    <!-- A source with unsaved edits CANNOT be collapsed: hiding
                         a pending change behind a closed disclosure is how an
                         operator saves something they have forgotten they
                         typed. preventDefault stops the toggle outright rather
                         than letting it close and snapping back, which reads as
                         a broken control. Save or Cancel is the way out. -->
                    <summary
                        class="cursor-pointer px-3 py-2 select-none"
                        title={edited ? 'Save or cancel before collapsing' : undefined}
                        onclick={(e) => {
                            if (
                                edited &&
                                e.currentTarget.parentElement instanceof HTMLDetailsElement &&
                                e.currentTarget.parentElement.open
                            ) {
                                e.preventDefault();
                            }
                        }}
                    >
                        <!-- inline-flex so the row sits BESIDE the native
                             triangle rather than below it. -->
                        <span class="inline-flex items-center gap-2 align-middle">
                            <span class="font-semibold text-ink">
                                {enrichmentState.labelFor(p.name)}{#if edited}<span
                                        class="text-warning"
                                        title="Unsaved changes">*</span
                                    >{/if}
                            </span>
                            <span class="text-xs text-muted">
                                {p.country ? 'country' : 'callsign'}
                            </span>
                            <!-- Same pill as Forwarding's, so the two sections
                                 read as one page. -->
                            <span
                                class="rounded border px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase {on
                                    ? 'border-green-500/40 bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400'
                                    : 'border-line bg-surface-muted text-muted'}"
                            >
                                {on ? 'enabled' : 'disabled'}
                            </span>
                            {#if !meta}
                                <span class="text-xs text-warning">unrecognised</span>
                            {/if}
                        </span>
                    </summary>

                    <div class="space-y-3 border-t border-line px-3 py-3">
                        {#if meta}
                            <p class="text-sm text-muted">{meta.blurb}</p>
                        {:else}
                            <p class="text-sm text-warning">
                                This lookup source is not recognised by this build, so there is no
                                description for it. Its settings are preserved on save, and the
                                fields below still apply.
                            </p>
                        {/if}

                        <!-- Reads the EFFECTIVE state, not the raw toggle: a
                             pending credential removal forces a credentialed
                             source off. Deliberately NOT a bind — the displayed
                             value is derived, and writing back through it would
                             re-introduce the mutation whose missing reversal
                             was the P2 (review a6a3b1fcb40d). Locked while the
                             removal is pending, because a toggle that cannot
                             take effect is indistinguishable from a broken one. -->
                        <label
                            class="flex w-fit items-center gap-1.5 text-sm text-ink"
                            class:opacity-60={enrichmentState.removalPending(p)}
                        >
                            <input
                                type="checkbox"
                                class="cursor-pointer"
                                checked={enrichmentState.effectiveEnabled(p)}
                                disabled={enrichmentState.removalPending(p)}
                                onchange={(e) => (p.enabled = e.currentTarget.checked)}
                            />
                            Enabled
                        </label>

                        <!-- Hamnut is anonymous BY DESIGN, so it gets no
                             credential boxes at all rather than empty ones the
                             operator might think they are meant to fill. An
                             unrecognised source does get them: the wire shape is
                             uniform, so the fields are known even when the
                             service is not. -->
                        {#if !meta || meta.credentialed}
                            <label class="flex w-72 flex-col gap-1">
                                <span class="text-sm font-medium text-ink">Username</span>
                                <input
                                    class="input"
                                    autocomplete="off"
                                    spellcheck="false"
                                    bind:value={p.username}
                                />
                            </label>
                            <label class="flex w-72 flex-col gap-1">
                                <span class="text-sm font-medium text-ink">Password</span>
                                <MaskedField
                                    value={p.password}
                                    oninput={(v: string) => enrichmentState.setPassword(p.name, v)}
                                    placeholder={p.passwordSet
                                        ? '•••••••• (set — leave blank to keep)'
                                        : ''}
                                />
                            </label>

                            <!-- Removal is a third state the box cannot express:
                                 it looks identical whether blank means "keep" or
                                 "erase". Offered only when something is stored —
                                 a control that appears to work and does nothing
                                 teaches the operator that it worked. -->
                            {#if p.passwordSet}
                                {#if p.passwordCleared}
                                    <div
                                        class="flex w-72 flex-col gap-2 rounded-md border border-warning bg-surface-muted px-3 py-2"
                                    >
                                        <span class="text-xs text-warning">
                                            The stored password will be removed when you save, and
                                            this source has been switched off — it can't run without
                                            a login. Enter a new password to use it again.
                                        </span>
                                        <button
                                            class="btn self-start"
                                            type="button"
                                            onclick={() => enrichmentState.keepPassword(p.name)}
                                        >
                                            Keep stored password
                                        </button>
                                    </div>
                                {:else}
                                    <button
                                        class="btn self-start"
                                        type="button"
                                        onclick={() => enrichmentState.clearPassword(p.name)}
                                    >
                                        Remove stored password
                                    </button>
                                {/if}
                            {/if}
                        {/if}
                    </div>
                </details>
            {/each}

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
