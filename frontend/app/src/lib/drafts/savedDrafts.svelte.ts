// The saved-draft list the shell shows (ADR 0085, rule 3 slice 1). Read from
// browser storage, and read again whenever another tab announces a committed
// change or this tab becomes visible (operator ruling 2026-09-30). Discard
// removes only the saved browser record, never a logged QSO; the entry leaves
// the list only once the deletion has succeeded.

import { toasts } from '../ui/toasts.svelte';
import { announceDraftsChanged, onDraftsChanged } from './draftChannel';
import { draftStore, listSavedDrafts } from './draftStore';
import type { SavedDraft } from './savedDraft';

export const savedDrafts: { list: SavedDraft[]; error: string; removing: string } = $state({
    list: [],
    error: '',
    removing: '',
});

// Every read is numbered and only the newest may apply its result: an older
// read — begun before a discard, answering after it — must not resurrect the
// discarded entry, and overlapping re-reads must not land out of order.
let generation = 0;

/** Read the list. A failed read keeps what is shown and says so. */
export async function loadSavedDrafts(): Promise<void> {
    const mine = ++generation;
    try {
        const list = await listSavedDrafts();
        if (mine !== generation) return;
        savedDrafts.list = list;
        savedDrafts.error = '';
    } catch (e) {
        if (mine !== generation) return;
        savedDrafts.error = e instanceof Error ? e.message : String(e);
    }
}

/** Remove one saved record. True when it is gone; on failure it stays shown. */
export async function discardSavedDraft(id: string): Promise<boolean> {
    savedDrafts.removing = id;
    try {
        await draftStore().remove(id);
    } catch (e) {
        const detail = e instanceof Error ? e.message : String(e);
        toasts.error(`The saved QSO could not be discarded (${detail}); it is still kept.`);
        return false;
    } finally {
        savedDrafts.removing = '';
    }
    savedDrafts.list = savedDrafts.list.filter((r) => r.id !== id);
    announceDraftsChanged(); // committed: the other tabs re-read
    void loadSavedDrafts(); // supersedes any read begun before the delete
    return true;
}

/** Keep the list current while a view shows it: read now, then again on
 *  another tab's announcement and whenever this tab becomes visible. */
export function watchSavedDrafts(): () => void {
    void loadSavedDrafts();
    const unsubscribe = onDraftsChanged(() => void loadSavedDrafts());
    const onVisible = (): void => {
        if (document.visibilityState === 'visible') void loadSavedDrafts();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
        unsubscribe();
        document.removeEventListener('visibilitychange', onVisible);
    };
}

export function _resetSavedDraftsForTests(): void {
    generation = 0;
    savedDrafts.list = [];
    savedDrafts.error = '';
    savedDrafts.removing = '';
}
