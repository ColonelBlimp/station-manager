<script lang="ts">
    // Destinations for <archive> (ADR 0082 part 9, W-0021 5E): which
    // destinations the ACTIVE archive uploads new QSOs to, per logbook. One
    // card per destination type with a switch over every logbook, then one row
    // per logbook with its own switch, its per-logbook account fields (masked —
    // the daemon only says which are set), its queue, Retry failed and Clear
    // queue. The card's pill is what the daemon HOLDS; the switches are the
    // draft. Saved bindings apply when the daemon restarts.
    import { onMount } from 'svelte';
    import { bindingsState, rowKey, destinationLabel } from './bindings.svelte';
    import { clearForwarderQueue, retryForwarderQueue } from '../api/forwarder-queues';
    import type { BindingState, DestinationBinding } from '../api/archive-bindings';
    import type { CredentialField } from '../api/forwarders';
    import { toasts } from '../ui/toasts.svelte';
    import { navigate, logbookMissingFromUrl } from '../router.svelte';
    import { hasUploadStamp } from '../logbook/uploadStatus';
    import MaskedField from './MaskedField.svelte';
    import ManualLink from './ManualLink.svelte';

    let {
        hasAccountCard = () => false,
        onOpenAccount = () => undefined,
    }: {
        /** Whether the Station accounts section below has a card for the type. */
        hasAccountCard?: (type: string) => boolean;
        /** Open and reveal that card. */
        onOpenAccount?: (type: string) => void;
    } = $props();

    let clearing = $state<Record<string, boolean>>({});
    let retrying = $state<Record<string, boolean>>({});
    // A binding whose count could not be refreshed after a clear or retry: its
    // count and actions are HIDDEN until a later successful read, so a second
    // clear cannot fire, on a stale count, against uploads queued since (U14).
    let staleQueues = $state<Record<string, boolean>>({});

    async function refreshCounts(name: string): Promise<boolean> {
        const ok = await bindingsState.refresh();
        staleQueues = ok ? {} : { ...staleQueues, [name]: true };
        return ok;
    }

    onMount(() => {
        void bindingsState.load();
    });

    const PILL: Record<BindingState, { text: string; cls: string }> = {
        on: {
            text: 'enabled',
            cls: 'border-green-500/40 bg-green-50 text-green-700 dark:bg-green-500/10 dark:text-green-400',
        },
        off: { text: 'disabled', cls: 'border-line bg-surface-muted text-muted' },
        mixed: {
            text: 'mixed',
            cls: 'border-warning/40 bg-surface-muted text-warning',
        },
    };

    // A mixed destination names the logbooks it is off for (ADR 0082 part 2):
    // "mixed" alone sends the operator hunting through the rows. Off = a
    // bound-but-disabled row or no row at all, as the daemon's aggregate counts.
    function offFor(dest: DestinationBinding): string {
        return dest.logbooks
            .filter((r) => !r.enabled)
            .map((r) => r.logbook_name)
            .join(', ');
    }

    function totals(dest: DestinationBinding): {
        waiting: number;
        failed: number;
        in_flight: number;
    } {
        const t = { waiting: 0, failed: 0, in_flight: 0 };
        for (const r of dest.logbooks) {
            t.waiting += r.queue.waiting;
            t.failed += r.queue.failed;
            t.in_flight += r.queue.in_flight;
        }
        return t;
    }

    // "set" is all the daemon ever says about a stored value. A field the
    // daemon defaults from the logbook's callsign (the descriptor marker, ADR
    // 0082 part 3) names that callsign while nothing is stored.
    function placeholderFor(
        setKeys: string[],
        field: CredentialField,
        logbookCallsign: string
    ): string {
        if (setKeys.includes(field.key)) return '•••••••• (set — leave blank to keep)';
        if (field.defaults_to === 'logbook_callsign' && logbookCallsign.trim() !== '') {
            return `${logbookCallsign} — the logbook’s callsign unless you type another`;
        }
        return '';
    }

    async function afterQueueAction(name: string, label: string): Promise<void> {
        if (!(await refreshCounts(name))) {
            toasts.warn(`Couldn't refresh ${label}'s queue count — it'll update on the next load.`);
        }
    }

    // Discard one binding's clearable (pending + failed) backlog. Confirmed,
    // because it cannot be undone; the latch holds through the refresh so a
    // stale count cannot be clicked twice.
    async function onClearQueue(name: string, label: string, clearable: number): Promise<void> {
        if (
            !window.confirm(
                `Discard ${clearable} queued upload${clearable === 1 ? '' : 's'} for ${label}? ` +
                    `In-flight uploads finish; upload history is kept. This can't be undone.`
            )
        ) {
            return;
        }
        clearing = { ...clearing, [name]: true };
        try {
            const out = await clearForwarderQueue(name);
            if (out.kind === 'ok') {
                toasts.info(
                    `Cleared ${out.discarded} queued upload${out.discarded === 1 ? '' : 's'} from ${label}.`
                );
                await afterQueueAction(name, label);
            } else if (out.indeterminate) {
                const confirmed = await refreshCounts(name);
                toasts.warn(
                    confirmed
                        ? `Clearing ${label} didn't confirm cleanly; its queue count has been refreshed to its current state.`
                        : `Clearing ${label} didn't confirm and its queue couldn't be re-read — whether it cleared is unknown.`
                );
            } else {
                toasts.error(out.message);
            }
        } finally {
            clearing = { ...clearing, [name]: false };
        }
    }

    // Re-arm one binding's failed uploads for one more attempt (W-0010 outcome 9).
    async function onRetryFailed(name: string, label: string): Promise<void> {
        retrying = { ...retrying, [name]: true };
        try {
            const out = await retryForwarderQueue(name);
            if (out.kind === 'ok') {
                toasts.info(
                    `Re-queued ${out.rearmed} failed upload${out.rearmed === 1 ? '' : 's'} for ${label}.`
                );
                await afterQueueAction(name, label);
            } else if (out.indeterminate) {
                const confirmed = await refreshCounts(name);
                toasts.warn(
                    confirmed
                        ? `Retrying ${label} didn't confirm cleanly; its queue count has been refreshed to its current state.`
                        : `Retrying ${label} didn't confirm and its queue couldn't be re-read — whether it re-queued is unknown.`
                );
            } else {
                toasts.error(out.message);
            }
        } finally {
            retrying = { ...retrying, [name]: false };
        }
    }
</script>

<section aria-labelledby="destinations-heading" class="space-y-4">
    <!-- Named by the daemon's archive label; "this archive" until it is
         known. No explanation here: the tab links the manual (station review
         2026-09-26), and each point of action carries its own note. -->
    <div class="flex items-center gap-2">
        <h2 id="destinations-heading" class="text-base font-semibold text-ink">
            Destinations for {bindingsState.view?.archive_label || 'this archive'}
        </h2>
        <ManualLink anchor="forwarding" label="How forwarding works" />
    </div>

    {#if !bindingsState.loaded && bindingsState.loading}
        <p class="text-sm text-muted">Loading…</p>
    {:else if !bindingsState.loaded && bindingsState.error}
        <div class="card">
            <p class="text-sm text-ink">Couldn’t load the destinations: {bindingsState.error}</p>
            <button class="btn mt-3" onclick={() => bindingsState.load()}>Retry</button>
        </div>
    {:else if bindingsState.view}
        {#each bindingsState.view.destinations as dest (dest.type)}
            {@const label = destinationLabel(dest)}
            {@const edited = bindingsState.destinationEdited(dest)}
            {@const pill = PILL[dest.state]}
            {@const t = totals(dest)}
            <!-- A total that includes a stale count is stale too (U14). -->
            {@const stale = dest.logbooks.some((r) => r.bound && staleQueues[r.forwarder_name])}
            {@const draft = bindingsState.draftState(dest)}
            {@const fields = bindingsState.logbookFields(dest.type)}
            <details
                class="rounded-md border border-line"
                open={edited || undefined}
                data-testid="destination-card"
            >
                <summary
                    class="cursor-pointer px-3 py-2 select-none"
                    title={edited ? 'Save or discard before collapsing' : undefined}
                    onclick={(e) => {
                        // A card holding unsaved edits cannot be collapsed: a
                        // hidden pending change is how edits get saved by accident.
                        if (
                            edited &&
                            e.currentTarget.parentElement instanceof HTMLDetailsElement &&
                            e.currentTarget.parentElement.open
                        ) {
                            e.preventDefault();
                        }
                    }}
                >
                    <span class="inline-flex items-center gap-2 align-middle">
                        <span class="font-semibold text-ink"
                            >{label}{#if edited}<span class="text-warning" title="Unsaved changes"
                                    >*</span
                                >{/if}</span
                        >
                        <!-- What the daemon HOLDS for this archive (the switches
                             below are the draft). -->
                        <span
                            class="rounded border px-1.5 py-0.5 text-[10px] font-semibold tracking-wide uppercase {pill.cls}"
                            data-testid="destination-state">{pill.text}</span
                        >
                        {#if dest.state === 'mixed'}
                            <span class="text-xs text-muted" data-testid="destination-off-for"
                                >off for {offFor(dest)}</span
                            >
                        {/if}
                        {#if !stale && t.waiting + t.failed + t.in_flight > 0}
                            <span
                                class="text-[11px] text-muted"
                                title="Uploads waiting to send · failed and not retried · currently being sent"
                            >
                                {t.waiting} waiting · {t.failed} failed · {t.in_flight} in flight
                            </span>
                        {/if}
                    </span>
                </summary>

                <div class="space-y-3 border-t border-line px-3 py-3">
                    {#if dest.reason}
                        <p class="text-sm text-muted" role="note" data-testid="destination-reason">
                            Can't be turned on here: {dest.reason}.
                            <!-- The link only when the station account IS the gap:
                                 an SM Cloud refusal on a complete account (the
                                 identity rule) is not fixed there. -->
                            {#if !dest.account.configured && hasAccountCard(dest.type)}
                                <button
                                    type="button"
                                    class="underline hover:text-ink"
                                    onclick={() => onOpenAccount(dest.type)}
                                    >Open its station account</button
                                >
                            {/if}
                        </p>
                    {/if}
                    {#if dest.account.build_key === 'absent'}
                        <p class="text-sm text-warning" data-testid="build-key-absent">
                            This build of Station Manager carries no {label} application key: its uploads
                            wait in the queue until a build with the key is installed.
                        </p>
                    {/if}

                    {#if dest.logbooks.length > 1}
                        <label class="flex w-fit items-center gap-2 text-sm text-ink">
                            <input
                                type="checkbox"
                                class="cursor-pointer"
                                checked={draft === 'on'}
                                indeterminate={draft === 'mixed'}
                                disabled={bindingsState.saving ||
                                    (dest.reason !== '' && draft === 'off')}
                                aria-label={`${label}: every logbook in this archive`}
                                onchange={(e) =>
                                    bindingsState.setAll(dest.type, e.currentTarget.checked)}
                            />
                            Every logbook in this archive
                        </label>
                    {/if}

                    {#each dest.logbooks as row (row.logbook_id)}
                        {@const k = rowKey(dest.type, row.logbook_id)}
                        {@const d = bindingsState.drafts[k]}
                        {#if d}
                            <div
                                class="rounded-md border border-line p-3"
                                data-testid="binding-row"
                            >
                                <label class="flex w-fit items-center gap-2 text-sm text-ink">
                                    <input
                                        type="checkbox"
                                        class="cursor-pointer"
                                        checked={d.enabled}
                                        disabled={bindingsState.saving ||
                                            (dest.reason !== '' && !d.enabled)}
                                        aria-label={`${label} for ${row.logbook_name}`}
                                        onchange={(e) =>
                                            bindingsState.setRow(
                                                dest.type,
                                                row.logbook_id,
                                                e.currentTarget.checked
                                            )}
                                    />
                                    <span class="font-medium">{row.logbook_name}</span>
                                    <span class="font-mono text-xs text-muted"
                                        >{row.logbook_callsign}</span
                                    >
                                    {#if bindingsState.rowEdited(dest.type, row)}<span
                                            class="text-warning"
                                            title="Unsaved changes">*</span
                                        >{/if}
                                </label>

                                {#if fields.length > 0}
                                    <div class="mt-3 space-y-3">
                                        {#each fields as field (field.key)}
                                            {@const marked =
                                                bindingsState.missing[k]?.includes(field.key) ??
                                                false}
                                            {@const removing = d.cleared.includes(field.key)}
                                            <label class="flex flex-col gap-1">
                                                <span class="text-sm font-medium text-ink"
                                                    >{field.label}</span
                                                >
                                                {#if field.kind === 'password'}
                                                    <MaskedField
                                                        value={d.credentials[field.key] ?? ''}
                                                        invalid={marked}
                                                        disabled={bindingsState.saving || removing}
                                                        oninput={(v: string) =>
                                                            bindingsState.setField(
                                                                dest.type,
                                                                row.logbook_id,
                                                                field.key,
                                                                v
                                                            )}
                                                        placeholder={placeholderFor(
                                                            row.credentials_set,
                                                            field,
                                                            row.logbook_callsign
                                                        )}
                                                    />
                                                {:else}
                                                    <input
                                                        type="text"
                                                        class="input w-full"
                                                        class:input-error={marked}
                                                        aria-invalid={marked || undefined}
                                                        disabled={bindingsState.saving || removing}
                                                        value={d.credentials[field.key] ?? ''}
                                                        oninput={(e) =>
                                                            bindingsState.setField(
                                                                dest.type,
                                                                row.logbook_id,
                                                                field.key,
                                                                e.currentTarget.value
                                                            )}
                                                        placeholder={placeholderFor(
                                                            row.credentials_set,
                                                            field,
                                                            row.logbook_callsign
                                                        )}
                                                        autocomplete="off"
                                                        spellcheck="false"
                                                    />
                                                {/if}
                                                {#if marked}
                                                    <span class="text-xs text-invalid"
                                                        >Required to turn this on.</span
                                                    >
                                                {/if}
                                                <!-- Removing a stored value is offered only on
                                                     a row that is off: the daemon refuses a
                                                     removal from a row that stays on. -->
                                                {#if row.credentials_set.includes(field.key) && !d.enabled}
                                                    {#if removing}
                                                        <span
                                                            class="flex items-center gap-2 text-xs text-warning"
                                                        >
                                                            The stored value will be removed on
                                                            save.
                                                            <button
                                                                type="button"
                                                                class="underline"
                                                                disabled={bindingsState.saving}
                                                                onclick={() =>
                                                                    bindingsState.uncleared(
                                                                        dest.type,
                                                                        row.logbook_id,
                                                                        field.key
                                                                    )}>Undo</button
                                                            >
                                                        </span>
                                                    {:else}
                                                        <button
                                                            type="button"
                                                            class="self-start text-xs text-muted underline hover:text-ink"
                                                            disabled={bindingsState.saving}
                                                            onclick={() =>
                                                                bindingsState.clear(
                                                                    dest.type,
                                                                    row.logbook_id,
                                                                    field.key
                                                                )}>Remove the stored value</button
                                                        >
                                                    {/if}
                                                {/if}
                                                {#if field.help}
                                                    <span class="text-xs text-muted"
                                                        >{field.help}</span
                                                    >
                                                {/if}
                                            </label>
                                        {/each}
                                    </div>
                                {/if}

                                {#if row.bound && staleQueues[row.forwarder_name]}
                                    <p class="mt-3 text-xs text-muted" data-testid="queue-stale">
                                        This queue's count couldn't be refreshed; it will show again
                                        on the next load.
                                    </p>
                                {:else if row.bound}
                                    {@const q = row.queue}
                                    {@const name = row.forwarder_name}
                                    {@const rowLabel = `${label} for ${row.logbook_name}`}
                                    <div class="mt-3 space-y-2">
                                        <p
                                            class="text-[11px] text-muted"
                                            title="Uploads waiting to send · failed and not retried · currently being sent"
                                        >
                                            {q.waiting} waiting · {q.failed} failed · {q.in_flight} in
                                            flight
                                        </p>
                                        <div class="flex flex-wrap gap-2">
                                            <button
                                                class="btn"
                                                disabled={q.failed === 0 || retrying[name]}
                                                aria-label={`Retry failed uploads for ${rowLabel}`}
                                                onclick={() => onRetryFailed(name, rowLabel)}
                                            >
                                                {retrying[name]
                                                    ? 'Retrying…'
                                                    : `Retry failed (${q.failed})`}
                                            </button>
                                            <button
                                                class="btn"
                                                disabled={q.waiting + q.failed === 0 ||
                                                    clearing[name]}
                                                aria-label={`Clear the queue for ${rowLabel}`}
                                                onclick={() =>
                                                    onClearQueue(
                                                        name,
                                                        rowLabel,
                                                        q.waiting + q.failed
                                                    )}
                                            >
                                                {clearing[name]
                                                    ? 'Clearing…'
                                                    : `Clear queue (${q.waiting + q.failed})`}
                                            </button>
                                        </div>
                                        {#if q.failed > 0 && hasUploadStamp(dest.type)}
                                            <p class="text-xs text-muted">
                                                <a
                                                    class="underline hover:text-ink"
                                                    href={logbookMissingFromUrl(
                                                        name,
                                                        row.logbook_id
                                                    )}
                                                    onclick={(e) => {
                                                        e.preventDefault();
                                                        navigate('logbook', {
                                                            missingFrom: name,
                                                            logbookId: row.logbook_id,
                                                        });
                                                    }}
                                                    >Show the QSOs of {row.logbook_name} not on {label}</a
                                                >
                                                — every QSO not on {label}, not only the failed
                                                uploads.
                                            </p>
                                        {/if}
                                    </div>
                                {/if}
                            </div>
                        {/if}
                    {/each}
                </div>
            </details>
        {/each}

        {#if bindingsState.view.restart_required}
            <div
                class="rounded-md border border-warning bg-surface-muted px-3 py-2 text-sm text-warning"
                role="status"
                data-testid="bindings-restart"
            >
                ⚠ The saved destinations differ from the ones the daemon is running with; they apply
                when it restarts (Restart daemon, above).
            </div>
        {/if}
        {#if bindingsState.refusal}
            <div
                class="rounded-md border border-warning bg-surface-muted px-3 py-2 text-sm text-warning"
                role="alert"
                data-testid="bindings-refusal"
            >
                {bindingsState.refusal}
            </div>
        {/if}

        <div class="flex items-center gap-3 border-t border-line pt-4">
            <button
                class="btn btn-primary"
                disabled={!bindingsState.dirty || bindingsState.saving}
                onclick={() => bindingsState.save()}
            >
                {bindingsState.saving ? 'Saving…' : 'Save destinations'}
            </button>
            <button
                class="btn"
                disabled={!bindingsState.dirty || bindingsState.saving}
                onclick={() => bindingsState.reset()}
            >
                Discard destination changes
            </button>
        </div>
    {/if}
</section>
