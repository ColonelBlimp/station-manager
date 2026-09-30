<script lang="ts">
    // Saved QSOs, opened from a control in the header beside Logbook — and in
    // the Map toolbar (ADR 0086, replacing ADR 0085 slice 1's strip). The count
    // shows while any record exists (inactive archives and unknown outcomes
    // included) or while storage cannot be read, so a failed read never passes
    // for an empty collection. The overlay panel owns the list, details, Copy
    // and confirmed Discard; closing it keeps every record. It offers no
    // restore yet, so nothing here promises one.
    import { onMount, tick } from 'svelte';
    import SavedDraftDetails from './SavedDraftDetails.svelte';
    import { savedDraftHeadline, savedDraftSummary, type SavedDraft } from './savedDraft';
    import {
        discardSavedDraft,
        savedDrafts,
        savedQsosPanel,
        watchSavedDrafts,
    } from './savedDrafts.svelte';

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
        count === 0 && savedDrafts.error !== '' ? 'Saved QSOs (?)' : `Saved QSOs (${count})`
    );

    let trigger = $state<HTMLButtonElement | null>(null);
    let closeButton = $state<HTMLButtonElement | null>(null);
    let expanded = $state<Record<string, boolean>>({});

    async function openPanel(): Promise<void> {
        savedQsosPanel.open = true;
        await tick();
        closeButton?.focus();
    }
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
    let swallowEscapeRepeats = false;
    function onKeydownCapture(e: KeyboardEvent): void {
        if (e.key !== 'Escape') return;
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
        onclick={() => (savedQsosPanel.open ? closePanel() : void openPanel())}>{label}</button
    >
{/if}

{#if savedQsosPanel.open}
    <!-- Overlay: nothing underneath moves. Below the header (z-40), above the
         drawers (z-20); fits a narrow screen. -->
    <div
        class="card fixed top-18 right-4 z-30 max-h-[calc(100vh-6rem)] w-[min(40rem,calc(100vw-2rem))] overflow-y-auto shadow-xl"
        role="dialog"
        aria-label="Saved QSOs"
    >
        <div class="mb-2 flex items-center justify-between">
            <h2 class="text-sm font-semibold text-ink">Saved QSOs</h2>
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
                <li class="text-sm text-ink" data-testid="saved-draft-{r.id}">
                    <div class="font-mono font-semibold">{savedDraftSummary(r)}</div>
                    <div>{savedDraftHeadline(r)}</div>
                    <div class="mt-1 flex gap-2">
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
