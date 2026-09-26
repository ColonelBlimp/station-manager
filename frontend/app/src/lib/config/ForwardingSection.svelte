<script lang="ts">
    // Forwarding (ADR 0082, W-0021 5E) — two sections on one tab:
    //
    //   - "Destinations for this archive" (DestinationsSection): which
    //     destinations the ACTIVE archive uploads to, per logbook, with each
    //     logbook's own account fields and queue. Owned by the archive.
    //   - "Station accounts" (below): what every archive shares — the SM Cloud
    //     service URL and token, and whether this build carries ClubLog's
    //     application key. Owned by config.json. No on/off here: whether a
    //     destination is on is a binding of the archive, never of the account.
    //
    // Station-account inputs are data-driven from GET /v1/forwarder-types
    // (station-scoped fields only), so adding a forwarder type in Go needs no
    // change here. The three blank states this UI must keep distinct (see
    // forwarding.svelte.ts buildPayload): never touched, typed-then-erased, and
    // explicitly reset. Only the last sends "" — and only the last gets a
    // control, shown solely for fields the daemon declares Clearable.
    import { onMount } from 'svelte';
    import { forwardingState } from './forwarding.svelte';
    import { bindingsState } from './bindings.svelte';
    import DestinationsSection from './DestinationsSection.svelte';
    import MaskedField from './MaskedField.svelte';

    onMount(() => {
        void forwardingState.load();
    });

    // The application-key presence the daemon reports for a type ('' = the
    // type needs none, or the bindings view is not loaded).
    function buildKeyOf(type: string): '' | 'present' | 'absent' {
        return (
            bindingsState.view?.destinations.find((d) => d.type === type)?.account.build_key ?? ''
        );
    }

    // A station entry belongs in this section when it has something station-
    // wide to show: station-scoped fields, an application key built into the
    // daemon, or no descriptor at all (explained, and round-tripped on save).
    function inStationAccounts(type: string): boolean {
        if (!forwardingState.typeFor(type)) return true;
        return forwardingState.stationFields(type).length > 0 || buildKeyOf(type) !== '';
    }

    const accounts = $derived(forwardingState.drafts.filter((f) => inStationAccounts(f.type)));

    // A destination's "incomplete station account" is fixed in its card here.
    function hasAccountCard(type: string): boolean {
        return accounts.some(
            (f) => f.type === type && forwardingState.stationFields(type).length > 0
        );
    }

    // "Open its station account": open that card and bring it into view. The
    // element's own `open` is set, not a forced-open state, so a card the
    // operator collapsed afterwards opens again on the next click.
    function openAccount(type: string): void {
        const card = document.getElementById(`account-${type}`);
        if (!(card instanceof HTMLDetailsElement)) return;
        card.open = true;
        card.scrollIntoView?.({ block: 'start' });
    }

    function isSet(setKeys: string[], key: string): boolean {
        return setKeys.includes(key);
    }

    // "set" is the only thing the daemon ever tells us about a stored value, so
    // it is the only thing the placeholder may claim.
    function placeholderFor(setKeys: string[], key: string): string {
        return isSet(setKeys, key) ? '•••••••• (set — leave blank to keep)' : '';
    }
</script>

<!-- Shape matches StationSection deliberately, so the tabs read as one page:
     the same mx-auto max-w-3xl shell (the strip's own container is max-w-5xl,
     so an unwrapped section sits wider and left of its neighbours), the same
     loading / error-card / body branch order (here inside Station accounts),
     the same space-y-8 rhythm, and the same border-t save footer. The per-account CARDS are the one
     deliberate departure — these are repeated entities, not named sections, so
     Station's <h2> headings would be inventing titles for them. -->
<div class="mx-auto max-w-3xl space-y-8">
    <p class="text-sm text-muted">
        Forwarding uploads each <em>new</em> QSO to the destinations its archive is bound to. QSOs logged
        while a destination was off aren't sent automatically — upload those from the logbook's backfill.
    </p>

    <!-- Outside the station-account load branches below: the destinations
         load and hold their own drafts, so a reload of the config.json half
         must neither remount them (reloading over the operator's edits) nor
         hide them when only that half failed to load. -->
    <DestinationsSection {hasAccountCard} onOpenAccount={openAccount} />

    <section id="station-accounts" aria-labelledby="station-accounts-heading" class="space-y-4">
        <div>
            <h2 id="station-accounts-heading" class="text-base font-semibold text-ink">
                Station accounts
            </h2>
            <p class="mt-0.5 text-sm text-muted">
                Shared by every archive: the SM Cloud service and its token, and ClubLog's
                application key built into this daemon. Values are stored on the daemon and never
                sent back to the browser, so leaving a field blank keeps the saved value.
            </p>
        </div>

        {#if !forwardingState.loaded && forwardingState.loading}
            <p class="text-sm text-muted">Loading…</p>
        {:else if !forwardingState.loaded && forwardingState.error}
            <div class="card">
                <p class="text-sm text-ink">
                    Couldn’t load the station accounts: {forwardingState.error}
                </p>
                <button class="btn mt-3" onclick={() => forwardingState.load()}>Retry</button>
            </div>
        {:else}
            {#if accounts.length === 0}
                <p class="text-sm text-muted">No station-wide settings for any destination.</p>
            {/if}

            {#each accounts as f (f.type + ':' + f.name)}
                {@const td = forwardingState.typeFor(f.type)}
                <!-- One disclosure per station account, the LoggingCard
                     "Contact details" pattern (operate/LoggingCard.svelte).
                     The summary carries what collapsing must not hide: which
                     service, whether this build can edit it, and — because a
                     collapsed card can hide an edit the footer only reports in
                     aggregate — whether it has unsaved changes. -->
                {@const edited = forwardingState.hasEdits(f.name)}
                {@const buildKey = buildKeyOf(f.type)}
                <details
                    id={`account-${f.type}`}
                    class="rounded-md border border-line"
                    open={edited || undefined}
                    data-testid="account-card"
                >
                    <!-- A card with unsaved edits CANNOT be collapsed: hiding a
                         pending change behind a closed disclosure is how an
                         operator saves something they have forgotten they
                         typed. preventDefault on the summary click stops the
                         toggle outright rather than letting it close and
                         snapping it back, which reads as a broken control.
                         Save or Cancel is the way out — both clear hasEdits,
                         and Cancel is always available, so a card cannot get
                         stuck open. -->
                    <!-- The <summary> itself keeps its DEFAULT display, which is
                         what renders the browser's native disclosure triangle —
                         same as Rigs → Mode mappings. Putting `flex` on the
                         summary suppresses that marker (it belongs to
                         display:list-item), so the row layout lives on an
                         inline-flex wrapper inside instead. -->
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
                             triangle rather than below it: a block-level child
                             would start its own line box and push the content
                             under the marker. -->
                        <span class="inline-flex items-center gap-2 align-middle">
                            <!-- The operator's config.json label wins over the name
                             baked into the binary, which is a build+deploy to
                             change and already dates (smcloud's "SM Cloud
                             backup"). Falls back to the built-in, then to the
                             raw type, so a destination is never nameless. -->
                            <span class="font-semibold text-ink">
                                {f.label || td?.display_name || f.type}{#if edited}<span
                                        class="text-warning"
                                        title="Unsaved changes">*</span
                                    >{/if}
                            </span>
                            <!-- No mono `name` here. It is the durable key
                             (qso_upload.forwarder_name) but it is not operator
                             information: ADR 0039 seeds one entry per type, so
                             it always equals the type and just repeats the
                             service name in a second font. -->
                            {#if !td}
                                <span class="text-xs text-warning">unsupported</span>
                            {/if}
                        </span>
                    </summary>

                    <div class="border-t border-line px-3 py-3">
                        {#if buildKey !== ''}
                            <p class="text-sm text-ink" data-testid="build-key">
                                Application key:
                                {#if buildKey === 'present'}
                                    built into this daemon.
                                {:else}
                                    <span class="text-warning"
                                        >not in this build — uploads wait in the queue until a build
                                        with the key is installed.</span
                                    >
                                {/if}
                            </p>
                        {/if}

                        {#if !td}
                            <p class="mt-3 text-sm text-warning">
                                This forwarder type isn't supported by this daemon build — its
                                credentials can't be edited here. Its settings are preserved on
                                save.
                            </p>
                        {:else if forwardingState.stationFields(f.type).length > 0}
                            <div class="mt-4 space-y-3">
                                {#each forwardingState.stationFields(f.type) as field (field.key)}
                                    {@const cleared = f.cleared.includes(field.key)}
                                    <label class="flex flex-col gap-1">
                                        <span class="text-sm font-medium text-ink"
                                            >{field.label}</span
                                        >

                                        {#if field.kind === 'password'}
                                            <MaskedField
                                                value={f.credentials[field.key] ?? ''}
                                                oninput={(v: string) =>
                                                    (f.credentials[field.key] = v)}
                                                placeholder={placeholderFor(
                                                    f.credentialsSet,
                                                    field.key
                                                )}
                                            />
                                        {:else}
                                            <input
                                                type="text"
                                                class="input w-full"
                                                disabled={cleared}
                                                value={f.credentials[field.key] ?? ''}
                                                oninput={(e) =>
                                                    (f.credentials[field.key] =
                                                        e.currentTarget.value)}
                                                placeholder={placeholderFor(
                                                    f.credentialsSet,
                                                    field.key
                                                )}
                                                autocomplete="off"
                                                spellcheck="false"
                                            />
                                        {/if}

                                        <!-- Reset appears ONLY for a field the daemon declares
                                     Clearable. It is not a delete: those fields have a
                                     constructor default, and emptying any OTHER credential
                                     is a daemon that won't restart. -->
                                        {#if field.clearable}
                                            {#if cleared}
                                                <span
                                                    class="flex items-center gap-2 text-xs text-warning"
                                                >
                                                    Will reset to the default on save.
                                                    <button
                                                        type="button"
                                                        class="underline"
                                                        onclick={() =>
                                                            forwardingState.uncleared(
                                                                f.name,
                                                                field.key
                                                            )}
                                                    >
                                                        Undo
                                                    </button>
                                                </span>
                                            {:else}
                                                <button
                                                    type="button"
                                                    class="self-start text-xs text-muted underline hover:text-ink"
                                                    onclick={() =>
                                                        forwardingState.clear(f.name, field.key)}
                                                >
                                                    Reset to default
                                                </button>
                                            {/if}
                                        {/if}

                                        {#if field.help}
                                            <span class="text-xs text-muted">{field.help}</span>
                                        {/if}
                                    </label>
                                {/each}
                            </div>
                        {/if}
                    </div>
                </details>
            {/each}

            {#if forwardingState.dirty}
                <div
                    class="rounded-md border border-warning bg-surface-muted px-3 py-2 text-sm text-warning"
                >
                    ⚠ Station account changes apply when the daemon restarts — the workers bind
                    their accounts at startup.
                </div>
            {/if}

            <div class="flex items-center gap-3 border-t border-line pt-4">
                <button
                    class="btn btn-primary"
                    disabled={!forwardingState.dirty || forwardingState.saving}
                    onclick={() => forwardingState.save()}
                >
                    {forwardingState.saving ? 'Saving…' : 'Save'}
                </button>
                <button
                    class="btn"
                    disabled={!forwardingState.dirty || forwardingState.saving}
                    onclick={() => forwardingState.reset()}
                >
                    Cancel
                </button>
                {#if forwardingState.dirty}
                    <span class="text-xs text-muted">Unsaved changes</span>
                {/if}
            </div>
        {/if}
    </section>
</div>
