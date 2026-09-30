// The saved-draft list the shell's notice shows (ADR 0085, rule 3 slice 1).
// Read from browser storage when the notice mounts. Discard removes only the
// saved browser record, never a logged QSO; the entry leaves the notice only
// once the deletion has succeeded.

import { toasts } from '../ui/toasts.svelte';
import { draftStore, listSavedDrafts } from './draftStore';
import type { SavedDraft } from './savedDraft';

export const savedDrafts: { list: SavedDraft[]; error: string; removing: string } = $state({
    list: [],
    error: '',
    removing: '',
});

export async function loadSavedDrafts(): Promise<void> {
    try {
        savedDrafts.list = await listSavedDrafts();
        savedDrafts.error = '';
    } catch (e) {
        savedDrafts.error = e instanceof Error ? e.message : String(e);
    }
}

/** Remove one saved record. True when it is gone; on failure it stays shown. */
export async function discardSavedDraft(id: string): Promise<boolean> {
    savedDrafts.removing = id;
    try {
        await draftStore().remove(id);
        savedDrafts.list = savedDrafts.list.filter((r) => r.id !== id);
        return true;
    } catch (e) {
        const detail = e instanceof Error ? e.message : String(e);
        toasts.error(`The saved QSO could not be discarded (${detail}); it is still kept.`);
        return false;
    } finally {
        savedDrafts.removing = '';
    }
}

export function _resetSavedDraftsForTests(): void {
    savedDrafts.list = [];
    savedDrafts.error = '';
    savedDrafts.removing = '';
}
