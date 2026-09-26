<script lang="ts">
    // Forwarding (ADR 0082, W-0021 5E) — two sections on one tab:
    //
    //   - "Destinations for <archive>" (DestinationsSection): which
    //     destinations the ACTIVE archive uploads to, per logbook, with each
    //     logbook's own account fields and queue. Owned by the archive.
    //   - "Station accounts" (below): what every archive shares — the SM Cloud
    //     service URL and token. Owned by config.json. No on/off here: whether a
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
    import StoredSecretField from './StoredSecretField.svelte';
    import ManualLink from './ManualLink.svelte';
    import { toasts } from '../ui/toasts.svelte';

    let {
        onRestart = () => undefined,
        restarting = false,
    }: {
        /** Settings' Restart daemon, handed to the destinations' banner. */
        onRestart?: () => void;
        restarting?: boolean;
    } = $props();

    onMount(() => {
        void forwardingState.load();
    });

    // A station card says what it holds — "SM Cloud service and token" — so it
    // does not read as a second "SM Cloud" beside the destination (ruling
    // 2026-09-26, station drill C.4). The suffix fits the one station-scoped
    // type today (a service URL and a token); revisit with a second one. An
    // entry this build cannot describe keeps its plain name.
    function accountTitleOf(f: { type: string; label?: string }): string {
        const td = forwardingState.typeFor(f.type);
        const name = f.label || td?.display_name || f.type;
        return td ? `${name} service and token` : name;
    }

    function accountTitleFor(type: string): string {
        const f = forwardingState.drafts.find((d) => d.type === type);
        return f ? accountTitleOf(f) : type;
    }

    // A station entry belongs in this section when it has station-wide
    // fields, or no descriptor at all (explained, and round-tripped on save).
    // ClubLog is not one (operator ruling 2026-09-26): its API key identifies
    // the software and is built in; its application password is the user's,
    // per logbook. A build without the key is stated on its destination card.
    function inStationAccounts(type: string): boolean {
        if (!forwardingState.typeFor(type)) return true;
        return forwardingState.stationFields(type).length > 0;
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

    async function saveAccounts(): Promise<void> {
        if (!(await forwardingState.save())) return;
        if (!(await bindingsState.refreshEligibility())) {
            toasts.warn(
                'Station account saved, but the destinations could not be refreshed; reload the tab before turning one on.'
            );
        }
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
    <!-- Outside the station-account load branches below: the destinations
         load and hold their own drafts, so a reload of the config.json half
         must neither remount them (reloading over the operator's edits) nor
         hide them when only that half failed to load. -->
    <DestinationsSection
        {hasAccountCard}
        onOpenAccount={openAccount}
        accountTitle={accountTitleFor}
        {onRestart}
        {restarting}
    />

    <section id="station-accounts" aria-labelledby="station-accounts-heading" class="space-y-4">
        <div class="flex items-center gap-2">
            <h2 id="station-accounts-heading" class="text-base font-semibold text-ink">
                Station accounts
            </h2>
            <ManualLink anchor="station-accounts" label="How station accounts work" />
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
                                {accountTitleOf(f)}{#if edited}<span
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
                        {#if !td}
                            <p class="mt-3 text-sm text-warning">
                                This forwarder type isn't supported by this daemon build — its
                                credentials can't be edited here. Its settings are preserved on
                                save.
                            </p>
                        {:else if forwardingState.stationFields(f.type).length > 0}
                            <div class="mt-4 space-y-3">
                                {#each forwardingState.stationFields(f.type) as field (field.key)}
                                    <!-- A stored value is a status line (ruling
                                         2026-09-26). Reset appears ONLY for a field the
                                         daemon declares Clearable, and only when a value
                                         is stored: it is not a delete — those fields have
                                         a constructor default, and emptying any OTHER
                                         credential is a daemon that won't restart. -->
                                    <StoredSecretField
                                        label={field.label}
                                        kind={field.kind}
                                        stored={f.credentialsSet.includes(field.key)}
                                        value={f.credentials[field.key] ?? ''}
                                        cleared={f.cleared.includes(field.key)}
                                        removable={field.clearable === true}
                                        removeLabel="Reset to default"
                                        removedNote="Resets to the default when you save."
                                        help={field.help ?? ''}
                                        oninput={(v: string) => {
                                            if (v === '') delete f.credentials[field.key];
                                            else f.credentials[field.key] = v;
                                        }}
                                        onremove={() => forwardingState.clear(f.name, field.key)}
                                        onundo={() => forwardingState.uncleared(f.name, field.key)}
                                    />
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
                    onclick={() => saveAccounts()}
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
