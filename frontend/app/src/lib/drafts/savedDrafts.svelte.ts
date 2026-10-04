// The saved-draft list the shell shows (ADR 0085, rule 3 slice 1). Read from
// browser storage, and read again whenever another tab announces a committed
// change or this tab becomes visible (operator ruling 2026-09-30). Discard
// removes only the saved browser record, never a logged QSO; the entry leaves
// the list only once the deletion has succeeded.

import { toasts } from '../ui/toasts.svelte';
import { announceDraftsChanged, onDraftsChanged } from './draftChannel';
import { draftStore, listSavedDrafts } from './draftStore';
import { savedDraftHeadline, type SavedDraft } from './savedDraft';
import { draftLocksAvailable, reserveSavedDraft, type DraftReservation } from './draftLock';
import { ownsRecoveredRecord } from './restoreSession';
import { discardRecovered, setRecoveredWrittenListener } from './recoveredSave.svelte';

export const savedDrafts: { list: SavedDraft[]; error: string; removing: string } = $state({
    list: [],
    error: '',
    removing: '',
});

/** The overlay panel's open state (ADR 0086). While it is open the Phone / CW
 *  card's shortcuts stand down, as they do for the Export dialog. */
export const savedQsosPanel: { open: boolean } = $state({ open: false });

// A recovered QSO's saved edits refresh this tab's own list (the change notice
// reaches only the other tabs).
setRecoveredWrittenListener(() => refreshSavedDraftsNow());

// Every read is numbered and only the newest may apply its result: an older
// read — begun before a discard, answering after it — must not resurrect the
// discarded entry, and overlapping re-reads must not land out of order.
let generation = 0;

/** What became of one read: it set the list, a newer read superseded it, or
 *  it failed (the list kept, the error shown). */
export type ListReadOutcome = 'applied' | 'superseded' | 'failed';

// Every completed read is announced, whatever started it (a refresh, a
// visibility change, another tab's notice), so a barrier can observe newer reads
// without waiting on its own (review 2026-10-03).
type ReadDone = (generationOfRead: number, outcome: ListReadOutcome) => void;
let readListeners: ReadDone[] = [];

/** Read the list. A failed read keeps what is shown and says so. */
export function loadSavedDrafts(): Promise<ListReadOutcome> {
    const mine = ++generation;
    const read = (async (): Promise<ListReadOutcome> => {
        try {
            const list = await listSavedDrafts();
            if (mine !== generation) return 'superseded';
            savedDrafts.list = list;
            savedDrafts.error = '';
            return 'applied';
        } catch (e) {
            if (mine !== generation) return 'superseded';
            savedDrafts.error = e instanceof Error ? e.message : String(e);
            return 'failed';
        }
    })();
    void read.then((outcome) => {
        for (const listener of readListeners.slice()) listener(mine, outcome);
    });
    return read;
}

/** Read the list and resolve true once it holds what storage held when this
 *  was called: as soon as ANY read begun at or after this call sets the list —
 *  its own, or a newer one for another reason — without waiting on its own
 *  read; false when the newest such read fails (review 2026-10-03). */
export function refreshSavedDraftsNow(): Promise<boolean> {
    const start = generation + 1; // the read started below, and every later one
    return new Promise<boolean>((resolve) => {
        const listener: ReadDone = (gen, outcome) => {
            if (gen < start || outcome === 'superseded') return; // older, or overtaken
            readListeners = readListeners.filter((l) => l !== listener);
            // Any read begun at or after this call that sets the list saw at least
            // what storage held now; a failure is reported only by the newest.
            resolve(outcome === 'applied');
        };
        readListeners.push(listener);
        void loadSavedDrafts();
    });
}

/** Remove one saved record. True when it is gone; on failure it stays shown. */
export async function discardSavedDraft(id: string): Promise<boolean> {
    // The record this tab has restored (operator ruling 3): its own path settles
    // the edits and the reservation; the lock request below would refuse it.
    if (ownsRecoveredRecord(id)) {
        savedDrafts.removing = id;
        try {
            if (!(await discardRecovered(id))) return false;
        } finally {
            savedDrafts.removing = '';
        }
        savedDrafts.list = savedDrafts.list.filter((r) => r.id !== id);
        void loadSavedDrafts();
        return true;
    }
    savedDrafts.removing = id;
    let claim: DraftReservation | null = null;
    try {
        // Without Web Locks Restore cannot run here; retain the existing
        // read/copy/discard behavior. An available but denied lock fails closed.
        if (draftLocksAvailable()) claim = await reserveSavedDraft(id);
        await draftStore().remove(id);
    } catch (e) {
        const detail = e instanceof Error ? e.message : String(e);
        toasts.error(`The saved QSO could not be discarded (${detail}); it is still kept.`);
        return false;
    } finally {
        await claim?.release();
        savedDrafts.removing = '';
    }
    savedDrafts.list = savedDrafts.list.filter((r) => r.id !== id);
    announceDraftsChanged(); // committed: the other tabs re-read
    void loadSavedDrafts(); // supersedes any read begun before the delete
    return true;
}

// The one-time announcement (ADR 0086): a committed save leaves it in this
// tab's sessionStorage, which survives the reload that follows; the first view
// to watch the list after that reload takes it and shows an ordinary toast.
// Taking it removes it, so later reloads and cross-tab refreshes never repeat it.
const ANNOUNCE_KEY = 'station-manager.saved-qso-announcement';

export interface PreservedAnnouncement {
    message: string;
    level: 'info' | 'warn';
}

/** Leave the announcement for this tab's reload. Called only after a save
 *  COMMITTED; the wording keeps an unknown logging outcome. */
export function rememberPreservedForAnnouncement(r: SavedDraft): void {
    const a: PreservedAnnouncement = {
        message: `${savedDraftHeadline(r)} It is under Unlogged QSOs.`,
        level: r.outcome === 'unknown' ? 'warn' : 'info',
    };
    try {
        sessionStorage.setItem(ANNOUNCE_KEY, JSON.stringify(a));
    } catch {
        // No session storage: the Unlogged QSOs control still shows the record.
    }
}

/** Take the pending announcement, if any; it is gone afterwards. */
export function consumePreservedAnnouncement(): PreservedAnnouncement | null {
    try {
        const raw = sessionStorage.getItem(ANNOUNCE_KEY);
        if (raw === null) return null;
        sessionStorage.removeItem(ANNOUNCE_KEY);
        const a: unknown = JSON.parse(raw);
        if (
            typeof a === 'object' &&
            a !== null &&
            typeof (a as PreservedAnnouncement).message === 'string' &&
            ((a as PreservedAnnouncement).level === 'info' ||
                (a as PreservedAnnouncement).level === 'warn')
        ) {
            return a as PreservedAnnouncement;
        }
        return null;
    } catch {
        return null;
    }
}

/** Keep the list current while a view shows it: read now, then again on
 *  another tab's announcement and whenever this tab becomes visible. The
 *  first watch after a reload also shows any pending announcement, once. */
export function watchSavedDrafts(): () => void {
    const pending = consumePreservedAnnouncement();
    // No automatic timeout (the toast stack's bounded eviction still applies)
    // and a direct way to the list (choice A, 2026-10-04): a self-dismissing
    // "saved" note was read as "logged".
    if (pending !== null) {
        toasts[pending.level](pending.message, 0, {
            label: 'Open Unlogged QSOs',
            run: () => (savedQsosPanel.open = true),
        });
    }
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
    readListeners = [];
    savedQsosPanel.open = false;
    savedDrafts.list = [];
    savedDrafts.error = '';
    savedDrafts.removing = '';
}
