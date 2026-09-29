<script lang="ts">
    // Logbooks section, first slice (W-0021 dossier, operator ruling 2026-09-29):
    // the ACTIVE archive's logbooks — add, rename, delete. It never activates an
    // archive, makes a logbook the default, or enables an upload unasked: the
    // Default tag is read-only here, and only the Default logbook receives live
    // contacts until default selection exists.
    import { onMount, untrack } from 'svelte';
    import ManualLink from './ManualLink.svelte';
    import { logbooksState, type LogbookRow } from './logbooks.svelte';
    import { archiveSwitchGate } from './archives.svelte';
    import { isValidCallsign } from '../validators/callsign';

    // visible: Settings keeps every section mounted and hides the inactive ones;
    // the list is read again on every opening of the tab, as Archives is.
    let { visible = true }: { visible?: boolean } = $props();

    onMount(() => {
        if (!visible && !logbooksState.loaded) void logbooksState.load();
    });
    $effect(() => {
        if (visible) untrack(() => void logbooksState.load());
    });

    // Nothing writes while an archive switch is in flight or unresolved: the
    // page may be bound to the archive being left.
    const gate = $derived(archiveSwitchGate());
    const locked = $derived(gate !== null || logbooksState.busy);

    const qsoCountFmt = (n: number | null): string =>
        n === null ? '—' : `${n.toLocaleString()} ${n === 1 ? 'QSO' : 'QSOs'}`;

    // ---- Add ---- (the drafts live in the store, where the leave guard sees them)
    const callsignBad = $derived(
        logbooksState.addCallsign.trim() !== '' &&
            isValidCallsign(logbooksState.addCallsign) !== null
    );
    const canAdd = $derived(
        logbooksState.addName.trim() !== '' &&
            logbooksState.addCallsign.trim() !== '' &&
            !callsignBad &&
            !locked
    );
    // A logbook whose callsign differs from the station's cannot take a live
    // contact: a submit's station callsign must match its logbook (ADR 0055).
    const otherCallsign = $derived(
        logbooksState.addCallsign.trim() !== '' &&
            logbooksState.stationCallsign !== '' &&
            logbooksState.addCallsign.trim().toUpperCase() !==
                logbooksState.stationCallsign.toUpperCase()
    );

    async function onAdd(e: SubmitEvent): Promise<void> {
        e.preventDefault();
        if (!canAdd) return;
        await logbooksState.create({
            name: logbooksState.addName.trim(),
            callsign: logbooksState.addCallsign.trim().toUpperCase(),
            smcloud: logbooksState.addSmcloud && logbooksState.smcloudAvailable,
        });
    }

    // ---- Rename ----
    async function saveRename(row: LogbookRow): Promise<void> {
        const next = logbooksState.renameName.trim();
        if (next === '' || next === row.name) return;
        await logbooksState.rename(row.id, next);
    }

    // ---- Delete ----
    async function onDelete(row: LogbookRow): Promise<void> {
        if (!window.confirm(`Delete the logbook “${row.name}”? It holds no QSOs.`)) return;
        await logbooksState.remove(row.id);
    }
</script>

<div class="space-y-8">
    <section>
        <div class="mb-3 flex items-center gap-2">
            <h2 class="text-base font-semibold text-ink">Logbooks</h2>
            <ManualLink anchor="logbooks" label="How logbooks work" />
        </div>
        {#if logbooksState.archiveLabel}
            <p class="mb-3 text-sm text-muted">
                In the active archive <span
                    class="font-semibold text-ink"
                    data-testid="logbooks-archive">{logbooksState.archiveLabel}</span
                >
            </p>
        {/if}
        {#if gate}
            <p
                class="mb-3 rounded-md border border-line bg-surface-muted px-3 py-2 text-sm text-ink"
                role="status"
                data-testid="logbooks-gate"
            >
                {gate}
            </p>
        {/if}
        {#if !logbooksState.loaded && logbooksState.loading}
            <p class="text-sm text-muted">Loading…</p>
        {:else if !logbooksState.loaded && logbooksState.error}
            <div class="card">
                <p class="text-sm text-ink">Couldn’t load the logbooks: {logbooksState.error}</p>
                <button class="btn mt-3" onclick={() => logbooksState.load()}>Retry</button>
            </div>
        {:else}
            <table class="w-full text-left text-sm">
                <thead class="text-xs text-muted">
                    <tr>
                        <th class="py-1 pr-3 font-semibold">Name</th>
                        <th class="py-1 pr-3 font-semibold">Callsign</th>
                        <th class="py-1 pr-3 font-semibold">QSOs</th>
                        <th class="py-1 pr-3 font-semibold"><span class="sr-only">Actions</span></th
                        >
                    </tr>
                </thead>
                <tbody>
                    {#each logbooksState.rows as row (row.id)}
                        {@const block = logbooksState.deleteBlock(row)}
                        <tr class="border-t border-line" data-testid="logbook-row-{row.id}">
                            <td class="py-2 pr-3">
                                {#if logbooksState.renameId === row.id}
                                    <input
                                        class="input w-56"
                                        aria-label="New name for {row.name}"
                                        bind:value={logbooksState.renameName}
                                        maxlength="80"
                                    />
                                {:else}
                                    <span class="text-ink">{row.name}</span
                                    >{#if row.id === logbooksState.defaultId}<span
                                            class="ml-1 rounded border border-line px-1 text-xs text-muted"
                                            title="Live contacts go to this logbook"
                                            data-testid="logbook-default-{row.id}">Default</span
                                        >{/if}
                                {/if}
                            </td>
                            <td class="py-2 pr-3 font-mono">{row.callsign}</td>
                            <td class="py-2 pr-3">{qsoCountFmt(row.count)}</td>
                            <td class="py-2 text-right whitespace-nowrap">
                                {#if logbooksState.renameId === row.id}
                                    <button
                                        class="btn btn-primary"
                                        disabled={locked ||
                                            logbooksState.renameName.trim() === '' ||
                                            logbooksState.renameName.trim() === row.name}
                                        onclick={() => saveRename(row)}>Save name</button
                                    >
                                    <button class="btn" onclick={() => logbooksState.cancelRename()}
                                        >Cancel</button
                                    >
                                {:else}
                                    <button
                                        class="btn"
                                        disabled={locked}
                                        aria-label="Rename {row.name}"
                                        onclick={() => logbooksState.startRename(row)}
                                        >Rename</button
                                    >
                                    <button
                                        class="btn"
                                        disabled={locked || block !== null}
                                        title={block ?? undefined}
                                        aria-label="Delete {row.name}"
                                        onclick={() => onDelete(row)}>Delete</button
                                    >
                                {/if}
                            </td>
                        </tr>
                    {:else}
                        <tr><td colspan="4" class="py-2 text-muted">No logbooks.</td></tr>
                    {/each}
                </tbody>
            </table>
        {/if}
    </section>

    <section>
        <h2 class="mb-3 text-base font-semibold text-ink">Add logbook</h2>
        <form class="flex flex-wrap items-end gap-3" onsubmit={onAdd}>
            <label class="flex w-56 flex-col gap-1 text-sm text-ink">
                Name
                <input class="input" bind:value={logbooksState.addName} maxlength="80" />
            </label>
            <label class="flex w-40 flex-col gap-1 text-sm text-ink">
                Callsign
                <input
                    class="input font-mono uppercase"
                    class:input-error={callsignBad}
                    aria-invalid={callsignBad}
                    value={logbooksState.addCallsign}
                    oninput={(e) => logbooksState.setAddCallsign(e.currentTarget.value)}
                    autocapitalize="characters"
                />
                {#if callsignBad}
                    <span class="text-xs text-invalid"
                        >3–32 letters, digits or /, with at least one letter and one digit</span
                    >
                {/if}
            </label>
            {#if logbooksState.smcloudAvailable}
                <label class="flex items-center gap-2 pb-2 text-sm text-ink">
                    <input type="checkbox" bind:checked={logbooksState.addSmcloud} />
                    Upload to SM Cloud
                </label>
            {/if}
            <button type="submit" class="btn btn-primary" disabled={!canAdd}>Add logbook</button>
        </form>
        <p class="mt-2 text-xs text-muted" data-testid="logbook-live-note">
            Live contacts keep going to the Default logbook.{#if otherCallsign}
                A logbook with a different callsign can’t receive live contacts yet.{/if}
        </p>
    </section>
</div>
