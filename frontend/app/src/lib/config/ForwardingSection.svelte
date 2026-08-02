<script lang="ts">
    // Forwarding section — the operator's upload destinations (QRZ, ClubLog, SM
    // Cloud …). Ported from the standalone config SPA's Forwarding tab (ADR
    // 0044).
    //
    // Per ADR 0039 the list is NON-SPARSE: the daemon seeds an entry for every
    // supported destination and re-adds any missing one at load, so this is a
    // FIXED list with no add/remove. A destination is turned off by disabling
    // it — deleting an entry would just be re-seeded at the next restart.
    //
    // Credential inputs are data-driven from GET /v1/forwarder-types, so adding
    // a forwarder type in Go needs no change here.
    //
    // The three blank states this UI must keep distinct (see
    // forwarding.svelte.ts buildPayload): never touched, typed-then-erased, and
    // explicitly reset. Only the last sends "" — and only the last gets a
    // control, shown solely for fields the daemon declares Clearable.
    import { onMount } from 'svelte';
    import { forwardingState } from './forwarding.svelte';
    import MaskedField from './MaskedField.svelte';

    onMount(() => void forwardingState.load());

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
     loading / error-card / body branch order, the same space-y-8 rhythm, and
     the same border-t save footer. The per-destination CARDS are the one
     deliberate departure — these are repeated entities, not named sections, so
     Station's <h2> headings would be inventing titles for them. -->
<div class="mx-auto max-w-3xl">
    {#if !forwardingState.loaded && forwardingState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !forwardingState.loaded && forwardingState.error}
        <div class="card">
            <p class="text-sm text-ink">Couldn’t load forwarding: {forwardingState.error}</p>
            <button class="btn mt-3" onclick={() => forwardingState.load()}>Retry</button>
        </div>
    {:else}
        <div class="space-y-8">
            <p class="text-sm text-muted">
                Every supported destination is listed below. Enable the ones you use and enter their
                credentials — forwarding then uploads each <em>new</em> QSO to the enabled destinations.
                Credentials are stored on the daemon and never sent back to the browser, so leaving a
                field blank keeps the saved value. QSOs logged while a destination was off aren't sent
                automatically — upload those from the logbook's backfill.
            </p>

            {#if forwardingState.drafts.length === 0}
                <p class="text-sm text-muted">
                    No forwarder destinations available from the daemon.
                </p>
            {/if}

            {#each forwardingState.drafts as f (f.type + ':' + f.name)}
                {@const td = forwardingState.typeFor(f.type)}
                <!-- One disclosure per destination, the LoggingCard "Contact
                     details" pattern (operate/LoggingCard.svelte:290). The list
                     is fixed and grows with every new online service, so
                     everything expanded is a page that only gets longer.

                     The summary has to carry enough that collapsing loses
                     nothing an operator scans for: which service, whether it is
                     on, whether this build can edit it, and — because a
                     collapsed card can hide an edit the footer only reports in
                     aggregate — whether it has unsaved changes. The Enabled
                     TOGGLE lives inside: an interactive control in <summary>
                     fights the disclosure for the click. -->
                {@const edited = forwardingState.hasEdits(f.name)}
                <details class="rounded-md border border-line" open={edited || undefined}>
                    <!-- A card with unsaved edits CANNOT be collapsed: hiding a
                         pending change behind a closed disclosure is how an
                         operator saves something they have forgotten they
                         typed. preventDefault on the summary click stops the
                         toggle outright rather than letting it close and
                         snapping it back, which reads as a broken control.
                         Save or Cancel is the way out — both clear hasEdits,
                         and Cancel is always available, so a card cannot get
                         stuck open. -->
                    <summary
                        class="flex cursor-pointer items-center gap-2 px-3 py-2 select-none"
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
                        <!-- Same pill as the active-rig badge (RigsSection.svelte:119):
                             identical geometry and type, green when on. Disabled keeps
                             the shape and drops to neutral, because unlike a rig — where
                             exactly one is active and the rest offer a button — every
                             destination shows its state, so the row has to read at a
                             glance either way. Text is lower-case; the uppercase is CSS,
                             matching the rig pill. -->
                        <span
                            class="rounded border px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase {f.enabled
                                ? 'border-green-500/40 bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400'
                                : 'border-line bg-surface-muted text-muted'}"
                        >
                            {f.enabled ? 'enabled' : 'disabled'}
                        </span>
                        {#if !td}
                            <span class="text-xs text-warning">unsupported</span>
                        {/if}
                    </summary>

                    <div class="border-t border-line px-3 py-3">
                        <label class="flex w-fit items-center gap-1.5 text-sm text-ink">
                            <input
                                type="checkbox"
                                bind:checked={f.enabled}
                                class="cursor-pointer"
                            />
                            Enabled
                        </label>

                        {#if !td}
                            <p class="mt-3 text-sm text-warning">
                                This forwarder type isn't supported by this daemon build — its
                                credentials can't be edited here. Its settings are preserved on
                                save.
                            </p>
                        {:else}
                            <div class="mt-4 space-y-3">
                                {#each td.credential_fields as field (field.key)}
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
                    ⚠ Forwarding changes apply when the daemon restarts — the worker binds its
                    destinations at startup.
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
        </div>
    {/if}
</div>
