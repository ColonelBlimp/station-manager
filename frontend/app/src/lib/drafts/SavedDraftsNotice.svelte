<script lang="ts">
    // Saved Phone / CW drafts, immediately below the header on every shell page
    // (ADR 0085, rule 3 slice 1). One entry per saved QSO: what it is, where it
    // came from, its values on demand, and a confirmed Discard. It offers no
    // restore — that arrives with slice 2 — so nothing here promises one.
    import { onMount } from 'svelte';
    import SavedDraftDetails from './SavedDraftDetails.svelte';
    import { savedDraftHeadline, savedDraftSummary, type SavedDraft } from './savedDraft';
    import { discardSavedDraft, savedDrafts, watchSavedDrafts } from './savedDrafts.svelte';

    // Kept current across tabs: re-read on another tab's change and on becoming visible.
    onMount(() => watchSavedDrafts());

    let open = $state<Record<string, boolean>>({});

    function discard(r: SavedDraft): void {
        const ok = window.confirm(
            `Discard the saved QSO ${savedDraftSummary(r)}? This removes only the copy saved in this browser; nothing in any logbook changes.`
        );
        if (ok) void discardSavedDraft(r.id);
    }
</script>

{#if savedDrafts.error}
    <div
        class="border-b border-line bg-surface-muted px-4 py-2 text-sm text-ink sm:px-6 lg:px-8"
        role="status"
        data-testid="saved-drafts-error"
    >
        Saved QSO drafts could not be read from this browser’s storage ({savedDrafts.error}).
    </div>
{/if}
{#if savedDrafts.list.length > 0}
    <section
        class="border-b border-line bg-surface-muted px-4 py-2 sm:px-6 lg:px-8"
        aria-label="Saved QSO drafts"
        data-testid="saved-drafts"
    >
        <ul class="flex flex-col gap-2">
            {#each savedDrafts.list as r (r.id)}
                <li class="text-sm text-ink" data-testid="saved-draft-{r.id}">
                    <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
                        <span class="font-mono font-semibold">{savedDraftSummary(r)}</span>
                        <span>{savedDraftHeadline(r)}</span>
                        <span class="flex gap-2">
                            <button
                                type="button"
                                class="btn text-xs"
                                aria-expanded={open[r.id] === true}
                                onclick={() => (open[r.id] = !open[r.id])}
                                >{open[r.id] ? 'Hide details' : 'Show details'}</button
                            >
                            <button
                                type="button"
                                class="btn text-xs"
                                disabled={savedDrafts.removing === r.id}
                                onclick={() => discard(r)}>Discard</button
                            >
                        </span>
                    </div>
                    {#if open[r.id]}
                        <SavedDraftDetails record={r} />
                    {/if}
                </li>
            {/each}
        </ul>
    </section>
{/if}
