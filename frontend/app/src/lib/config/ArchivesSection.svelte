<script lang="ts">
    // Archives section (ADR 0071, W-0021 slice 4): the station's QSO archives —
    // separate database files, one active at a time. The list shows the daemon's
    // own state for each (active / pending / inactive): a candidate is pending
    // until the restart proves it. Activation restarts the daemon (the attended
    // restart is the switch); creation provisions an inactive managed archive
    // named by its semantics — never a path.
    import { onMount } from 'svelte';
    import ManualLink from './ManualLink.svelte';
    import {
        activateArchive,
        archivesState,
        createArchive,
        loadArchives,
        mintRequestKey,
        pendingArchive,
    } from './archives.svelte';
    import type { QsoArchive } from '../api/qso-archives';
    import { isValidCallsign } from '../validators/callsign';

    onMount(() => {
        if (!archivesState.loaded) void loadArchives();
    });

    // The create form. requestKey is minted when the form is first touched and
    // kept across a refused or ambiguous submit, so a retry reuses a creation
    // that did land instead of making a second archive.
    let label = $state('');
    let logbookName = $state('');
    let logbookCallsign = $state('');
    let requestKey = $state('');
    // A callsign is uppercase as typed (drill 1, 2026-09-24): the same rule the
    // logging card applies, so the field never shows a value the daemon would
    // not store.
    function upperCallsign(): void {
        logbookCallsign = logbookCallsign.toUpperCase();
    }
    // The callsign is validated as typed with the SPA's shared rule (3–32
    // chars, a letter and a digit — the daemon's rule): a malformed value marks
    // the field and holds the submit (drill 1, 2026-09-24: the form accepted a
    // one-character callsign and left the refusal to the daemon).
    const callsignBad = $derived(
        logbookCallsign.trim() !== '' && isValidCallsign(logbookCallsign) !== null
    );
    const canSubmit = $derived(
        label.trim() !== '' &&
            logbookName.trim() !== '' &&
            logbookCallsign.trim() !== '' &&
            !callsignBad
    );

    async function onCreate(e: SubmitEvent): Promise<void> {
        e.preventDefault();
        if (!canSubmit || archivesState.creating) return;
        if (requestKey === '') requestKey = mintRequestKey();
        const ok = await createArchive({
            requestKey,
            label: label.trim(),
            logbookName: logbookName.trim(),
            logbookCallsign: logbookCallsign.trim().toUpperCase(),
        });
        if (ok) {
            label = '';
            logbookName = '';
            logbookCallsign = '';
            requestKey = '';
        }
    }

    const pending = $derived(pendingArchive());
    // Activate is offered only where it can be acted on: not on the active
    // archive, not while a candidate is pending (the daemon refuses a second
    // activation until the restart), not while a request is in flight.
    const canActivate = $derived(
        (a: QsoArchive) =>
            a.state === 'inactive' &&
            !pending &&
            !archivesState.activating &&
            !archivesState.stale &&
            !archivesState.switchUnresolved
    );

    const bytesFmt = (n: number | null): string => {
        if (n === null) return '—';
        if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} KB`;
        return `${(n / (1024 * 1024)).toFixed(1)} MB`;
    };
    const whenFmt = (iso: string | null): string => {
        if (iso === null) return '—';
        const d = new Date(iso);
        return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
    };
    const stateLabel = (a: QsoArchive): string =>
        a.state === 'active' ? 'Active' : a.state === 'pending' ? 'Pending restart' : 'Inactive';
</script>

<div class="space-y-8">
    <section>
        <div class="mb-3 flex items-center gap-2">
            <h2 class="text-base font-semibold text-ink">Archives</h2>
            <ManualLink anchor="qso-archives" label="How archives work" />
        </div>
        {#if !archivesState.loaded && archivesState.loading}
            <p class="text-sm text-muted">Loading…</p>
        {:else if !archivesState.loaded && archivesState.error}
            <div class="card">
                <p class="text-sm text-ink">Couldn’t load the archives: {archivesState.error}</p>
                <button class="btn mt-3" onclick={() => loadArchives()}>Retry</button>
            </div>
        {:else}
            {#if archivesState.error}
                <!-- A read failed after an earlier one succeeded: the list below is the
                     last one read, not necessarily the daemon's, and it says so. -->
                <div class="card mb-3" role="alert" data-testid="stale">
                    <p class="text-sm text-ink">
                        The archive list could not be refreshed ({archivesState.error}); what is
                        shown may be out of date.
                    </p>
                    <button class="btn mt-3" onclick={() => loadArchives()}>Retry</button>
                </div>
            {/if}
            {#if pending}
                <p
                    class="mb-3 rounded-md border border-line bg-surface-muted px-3 py-2 text-sm text-ink"
                    role="status"
                >
                    “{pending.label}” is pending: the daemon restarts to open it. Transmit stays
                    sealed until then.
                </p>
            {/if}
            <table class="w-full text-left text-sm">
                <thead class="text-xs text-muted">
                    <tr>
                        <th class="py-1 pr-3 font-semibold">Label</th>
                        <th class="py-1 pr-3 font-semibold">State</th>
                        <th class="py-1 pr-3 font-semibold">Ownership</th>
                        <th class="py-1 pr-3 font-semibold">Size</th>
                        <th class="py-1 pr-3 font-semibold">Last written</th>
                        <th class="py-1 pr-3 font-semibold"><span class="sr-only">Actions</span></th
                        >
                    </tr>
                </thead>
                <tbody>
                    {#each archivesState.list as a (a.id)}
                        <tr class="border-t border-line align-top">
                            <td class="py-2 pr-3 font-medium text-ink">
                                {a.label}
                                <!-- The reason sits in a tooltip out of the row's flow, so it
                                     cannot push the columns. The glyph is a BUTTON so the
                                     keyboard reaches it (Codex P2 on 23508984); hover and
                                     focus (a tap focuses it) both reveal the reason, and
                                     aria-describedby reads it to a screen reader. -->
                                {#if a.lastActivationError}
                                    <span class="relative ml-1 inline-block">
                                        <button
                                            type="button"
                                            class="peer cursor-help text-warning"
                                            aria-label="Last activation failed"
                                            aria-describedby="activation-failed-{a.id}">⚠</button
                                        >
                                        <span
                                            id="activation-failed-{a.id}"
                                            role="tooltip"
                                            class="absolute top-full left-0 z-10 mt-1 hidden w-72 rounded-md border border-line bg-surface px-2 py-1 text-xs font-normal text-ink shadow peer-hover:block peer-focus:block"
                                            data-testid="activation-error"
                                            >Last activation failed: {a.lastActivationError}</span
                                        >
                                    </span>
                                {/if}
                            </td>
                            <td class="py-2 pr-3 text-ink" data-testid="state-{a.id}"
                                >{stateLabel(a)}</td
                            >
                            <td class="py-2 pr-3 text-muted">{a.ownership}</td>
                            <td class="py-2 pr-3 text-muted">{bytesFmt(a.sizeBytes)}</td>
                            <td class="py-2 pr-3 text-muted">{whenFmt(a.modifiedAt)}</td>
                            <td class="py-2 text-right">
                                {#if a.state !== 'active'}
                                    <button
                                        type="button"
                                        class="btn"
                                        disabled={!canActivate(a)}
                                        onclick={() => activateArchive(a.id)}
                                        aria-label="Activate {a.label}">Activate</button
                                    >
                                {/if}
                            </td>
                        </tr>
                    {:else}
                        <tr><td colspan="6" class="py-2 text-muted">No archives yet.</td></tr>
                    {/each}
                </tbody>
            </table>
        {/if}
    </section>

    <section>
        <!-- What the form used to explain — including that a new archive uploads
             nowhere (ADR 0082 part 9) — is in the manual's "Creating an archive". -->
        <div class="mb-3 flex items-center gap-2">
            <h2 class="text-base font-semibold text-ink">New archive</h2>
            <ManualLink anchor="creating-an-archive" label="How creating an archive works" />
        </div>
        <form class="flex flex-wrap items-end gap-3" onsubmit={onCreate}>
            <label class="flex w-56 flex-col gap-1 text-sm text-ink">
                Label
                <input class="input" bind:value={label} maxlength="80" />
            </label>
            <label class="flex w-56 flex-col gap-1 text-sm text-ink">
                First logbook
                <input class="input" bind:value={logbookName} maxlength="80" />
            </label>
            <label class="flex w-40 flex-col gap-1 text-sm text-ink">
                Logbook callsign
                <input
                    class="input font-mono uppercase"
                    class:input-error={callsignBad}
                    aria-invalid={callsignBad}
                    bind:value={logbookCallsign}
                    oninput={upperCallsign}
                    autocapitalize="characters"
                />
                {#if callsignBad}
                    <span class="text-xs text-invalid" data-testid="callsign-error"
                        >3–32 letters, digits or /, with at least one letter and one digit</span
                    >
                {/if}
            </label>
            <button
                type="submit"
                class="btn btn-primary"
                disabled={!canSubmit || archivesState.creating}
                >{archivesState.creating ? 'Creating…' : 'Create archive'}</button
            >
        </form>
    </section>
</div>
