<script lang="ts">
    // Every value of a saved (or unsaveable) Phone / CW draft as selectable
    // text, with Copy (ADR 0085). The clipboard needs a secure context, so the
    // text itself is always there to select by hand when Copy is refused.
    import { savedDraftLines, savedDraftText, type SaveState, type SavedDraft } from './savedDraft';
    import { toasts } from '../ui/toasts.svelte';

    let { record, state = 'saved' }: { record: SavedDraft; state?: SaveState } = $props();
    const rows = $derived(savedDraftLines(record, state));

    async function copy(): Promise<void> {
        try {
            await navigator.clipboard.writeText(savedDraftText(record, state));
            toasts.info('Copied the QSO details.');
        } catch {
            toasts.warn('The browser did not allow copying — select the text and copy it.');
        }
    }
</script>

<dl
    class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-sm select-text"
    data-testid="saved-draft-details"
>
    {#each rows as [label, value] (label)}
        <dt class="text-muted">{label}</dt>
        <dd class="break-words text-ink">{value}</dd>
    {/each}
</dl>
<button type="button" class="btn mt-2" onclick={() => void copy()}>Copy</button>
