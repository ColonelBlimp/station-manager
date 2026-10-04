<script lang="ts">
    // Saved QSOs, opened from a control in the header beside Logbook — and in
    // the Map toolbar (ADR 0086, replacing ADR 0085 slice 1's strip). The count
    // shows while any record exists (inactive archives and unknown outcomes
    // included) or while storage cannot be read, so a failed read never passes
    // for an empty collection. The overlay panel owns the list, details, Copy
    // and confirmed Discard; closing it keeps every record. An eligible entry
    // offers Restore on Phone / CW; elsewhere "Go to Phone / CW", which only
    // navigates (ADR 0085, Restore commit 4).
    import { onMount } from 'svelte';
    import SavedDraftDetails from './SavedDraftDetails.svelte';
    import { savedDraftHeadline, savedDraftSummary, type SavedDraft } from './savedDraft';
    import {
        discardSavedDraft,
        savedDrafts,
        savedQsosPanel,
        watchSavedDrafts,
    } from './savedDrafts.svelte';
    import { restoreEligibility } from './restore';
    import { readRestoreEnv, restoreFromPanel } from './restoreSession';
    import { setMode } from '../router.svelte';
    import { focusCallsign } from '../operate/state.svelte';

    // Kept current across tabs; the first watch after a reload also announces
    // newly preserved work, once.
    onMount(() => {
        const stop = watchSavedDrafts();
        return () => {
            stop();
            savedQsosPanel.open = false;
        };
    });

    const count = $derived(savedDrafts.list.length);
    const shown = $derived(count > 0 || savedDrafts.error !== '');
    const label = $derived(
        count === 0 && savedDrafts.error !== '' ? 'Unlogged QSOs (?)' : `Unlogged QSOs (${count})`
    );

    let trigger = $state<HTMLButtonElement | null>(null);
    let closeButton = $state<HTMLButtonElement | null>(null);
    let expanded = $state<Record<string, boolean>>({});

    function openPanel(): void {
        savedQsosPanel.open = true;
    }
    // However the panel opened — its control, or the announcement's action —
    // focus starts inside it.
    $effect(() => {
        closeButton?.focus();
    });
    function closePanel(): void {
        savedQsosPanel.open = false;
        trigger?.focus();
    }

    // Escape closes the panel and goes no further: captured on the window and
    // stopped there, so the Phone / CW card's own Escape — which clears the
    // draft — never sees it (ADR 0086).
    //
    // Holding Escape auto-repeats: once the first press has closed the panel,
    // its repeats would reach the card and clear the draft (review 2026-09-30).
    // They are swallowed until a press that is NOT a repeat — i.e. the key was
    // released and pressed again — which behaves as usual. Keyed on `repeat`
    // rather than on keyup, so a keyup lost to a focus change cannot leave
    // Escape suppressed.
    //
    // A modal dialog in front of the panel (Export, Duplicate, a session edit,
    // the archive gate — each marked aria-modal) owns Escape: the panel steps
    // aside so that dialog closes, not the panel hidden behind it (Codex
    // review 894b5359). The panel itself is an overlay, not aria-modal.
    let swallowEscapeRepeats = false;
    function onKeydownCapture(e: KeyboardEvent): void {
        if (e.key !== 'Escape') return;
        if (document.querySelector('[aria-modal="true"]') !== null) return;
        if (swallowEscapeRepeats && e.repeat) {
            e.preventDefault();
            e.stopImmediatePropagation();
            return;
        }
        swallowEscapeRepeats = false;
        if (!savedQsosPanel.open) return;
        e.preventDefault();
        e.stopImmediatePropagation();
        swallowEscapeRepeats = true;
        closePanel();
    }
    onMount(() => {
        window.addEventListener('keydown', onKeydownCapture, { capture: true });
        return () => window.removeEventListener('keydown', onKeydownCapture, { capture: true });
    });

    // What each entry offers. Advisory only: restoreFromPanel decides again,
    // against the environment as it is when pressed, after every wait.
    function offer(r: SavedDraft): { kind: 'restore' | 'go' | 'none'; reason: string } {
        const env = readRestoreEnv();
        if (env === null) return { kind: 'none', reason: '' };
        const e = restoreEligibility(r, env);
        switch (e.kind) {
            case 'eligible':
                return { kind: 'restore', reason: '' };
            case 'go-to-phone-cw':
                return { kind: 'go', reason: '' };
            case 'logged':
                return { kind: 'none', reason: '' };
            case 'unavailable':
                return { kind: 'none', reason: e.reason };
            case 'my-rig-changed':
                return {
                    kind: 'none',
                    reason: `My rig differs: saved ‘${e.saved}’; current ‘${e.current}’.`,
                };
        }
    }

    let refusals = $state<Record<string, string>>({});
    let restoring = $state(false);
    async function restore(r: SavedDraft): Promise<void> {
        restoring = true;
        refusals[r.id] = '';
        try {
            const out = await restoreFromPanel(r);
            if (!out.ok) {
                refusals[r.id] = out.reason;
                return;
            }
            savedQsosPanel.open = false;
            focusCallsign();
        } finally {
            restoring = false;
        }
    }

    function discard(r: SavedDraft): void {
        const ok = window.confirm(
            `Discard the saved QSO ${savedDraftSummary(r)}? This removes only the copy saved in this browser; nothing in any logbook changes.`
        );
        if (ok) void discardSavedDraft(r.id);
    }
</script>

{#if shown}
    <button
        type="button"
        class="btn shrink-0 text-xs"
        aria-haspopup="dialog"
        aria-expanded={savedQsosPanel.open}
        bind:this={trigger}
        onclick={() => (savedQsosPanel.open ? closePanel() : openPanel())}>{label}</button
    >
{/if}

{#if savedQsosPanel.open}
    <!-- Overlay: nothing underneath moves. Below the header (z-40), above the
         drawers (z-20); fits a narrow screen. -->
    <div
        class="card fixed top-18 right-4 z-30 max-h-[calc(100vh-6rem)] w-[min(40rem,calc(100vw-2rem))] overflow-y-auto shadow-xl"
        role="dialog"
        aria-label="Unlogged QSOs"
    >
        <div class="mb-2 flex items-center justify-between">
            <h2 class="text-sm font-semibold text-ink">Unlogged QSOs</h2>
            <button
                type="button"
                class="cursor-pointer rounded-md px-1 text-muted hover:text-ink"
                aria-label="Close"
                bind:this={closeButton}
                onclick={closePanel}>×</button
            >
        </div>
        {#if savedDrafts.error}
            <p class="mb-2 text-sm text-ink" role="status" data-testid="saved-drafts-error">
                Saved QSO drafts could not be read from this browser’s storage ({savedDrafts.error}).
            </p>
        {/if}
        <ul class="flex flex-col gap-3">
            {#each savedDrafts.list as r (r.id)}
                {@const o = offer(r)}
                <li class="text-sm text-ink" data-testid="saved-draft-{r.id}">
                    <div class="font-mono font-semibold">{savedDraftSummary(r)}</div>
                    <div>{savedDraftHeadline(r)}</div>
                    {#if o.reason}<div class="text-muted">{o.reason}</div>{/if}
                    {#if refusals[r.id]}<div role="alert">{refusals[r.id]}</div>{/if}
                    <div class="mt-1 flex gap-2">
                        {#if o.kind === 'restore'}
                            <button
                                type="button"
                                class="btn btn-primary text-xs"
                                disabled={restoring}
                                onclick={() => void restore(r)}>Restore</button
                            >
                        {:else if o.kind === 'go'}
                            <button
                                type="button"
                                class="btn text-xs"
                                onclick={() => setMode('phone')}>Go to Phone / CW</button
                            >
                        {/if}
                        <button
                            type="button"
                            class="btn text-xs"
                            aria-expanded={expanded[r.id] === true}
                            onclick={() => (expanded[r.id] = !expanded[r.id])}
                            >{expanded[r.id] ? 'Hide details' : 'Show details'}</button
                        >
                        <button
                            type="button"
                            class="btn text-xs"
                            disabled={savedDrafts.removing === r.id}
                            onclick={() => discard(r)}>Discard</button
                        >
                    </div>
                    {#if expanded[r.id]}
                        <SavedDraftDetails record={r} />
                    {/if}
                </li>
            {/each}
        </ul>
    </div>
{/if}
