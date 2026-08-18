/*
    Comment paste-list — a bounded MRU of the COMMENT text the operator has logged
    on saved QSOs, offered back as a quick-pick dropdown on the Comment field
    (CommentField.svelte). Ported from the retired logging SPA (its
    states/commentHistory.svelte.ts) by operator direction 2026-08-18, restoring the
    recent-comments picker dropped in the app-shell consolidation (W-0003).

    Conceptually the persisted cousin of callsignStack: a list of saved strings,
    click one to load it into a form field. The differences that earn its own
    module: it is populated AUTOMATICALLY on a successful QSO submit (not an explicit
    operator push), and it is PERSISTED (localStorage) — canned phrases ("Lot of QRN",
    "Tnx QSO 73") are reusable across sessions, unlike a per-page-load pile-up
    scratchpad. Per-device tier, survives reloads and tab close.

    Discipline: newest at index 0; dedup moves an existing identical entry back to the
    top rather than duplicating; empty/whitespace is never stored; capped at
    MAX_COMMENTS (oldest evicted off the end).
*/

const COMMENT_HISTORY_KEY = 'sm.commentHistory.items';

/** Bound on the paste list — keeps the dropdown glanceable and the stored payload
 *  small. ~10 per the original feature request. */
export const MAX_COMMENTS = 10;

function loadItems(): string[] {
    try {
        const raw = localStorage.getItem(COMMENT_HISTORY_KEY);
        if (raw === null) return [];
        const parsed: unknown = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        // Defensive hydration: a hand-edited / corrupted value must not wedge the
        // dropdown. Keep only non-empty strings and re-apply the cap.
        return parsed
            .filter((x): x is string => typeof x === 'string' && x.trim() !== '')
            .slice(0, MAX_COMMENTS);
    } catch {
        return []; // missing / unparseable storage — start empty
    }
}

function saveItems(items: string[]): void {
    try {
        localStorage.setItem(COMMENT_HISTORY_KEY, JSON.stringify(items));
    } catch {
        // Storage write failed (quota / private mode) — in-memory state is still
        // correct; we lose cross-reload survival, nothing else.
    }
}

class CommentHistory {
    /** Saved comments, newest at index 0. $state so the dropdown re-renders when a
     *  submit prepends a new entry. */
    items: string[] = $state(loadItems());

    /**
     * Record a logged comment. Trims; ignores empty; dedups (an existing identical
     * entry is removed first so the freshly-logged copy lands at the top instead of
     * duplicating); caps at MAX_COMMENTS. Reassigns `items` wholesale so the mirror
     * effect and the dropdown both see the change.
     */
    add(text: string): void {
        const t = text.trim();
        if (t === '') return;
        const next = this.items.filter((c) => c !== t);
        next.unshift(t);
        if (next.length > MAX_COMMENTS) next.length = MAX_COMMENTS;
        this.items = next;
    }

    /** Empty the paste list (test setup / a future "clear saved comments"). */
    clear(): void {
        this.items = [];
    }
}

export const commentHistory = new CommentHistory();

// Module-level mirror to localStorage. $effect.root because $effect can't run at a
// module's top level without a root context. The first-run latch skips the redundant
// write-back of the just-hydrated value on module load.
let rootDispose: (() => void) | null = $effect.root(() => {
    let firstRun = true;
    $effect(() => {
        const snapshot = commentHistory.items;
        if (firstRun) {
            firstRun = false;
            return;
        }
        saveItems(snapshot);
    });
});

/** Test-time teardown. Production never calls this (module lifetime = page
 *  lifetime); vitest cases call it so the singleton's mirror effect doesn't leak. */
export function _disposeForTests(): void {
    if (rootDispose) {
        rootDispose();
        rootDispose = null;
    }
}
