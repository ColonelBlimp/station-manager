/**
 * Comment paste-list — a bounded MRU of the COMMENT text the operator
 * has logged on saved QSOs, offered back as a quick-pick dropdown on
 * the Comment field.
 *
 * Conceptually the persisted cousin of `callsignStack`: a list of
 * saved strings, click one to load it into a form field. The
 * differences that earn it its own module:
 *   - **Populated automatically** on a successful QSO submit (not by
 *     an explicit operator push) — see `QsoPanel.submitQso` case
 *     'stored'.
 *   - **Persisted** (localStorage), because canned phrases ("Lot of
 *     QRN", "Tnx QSO 73") are reusable across sessions, unlike a
 *     pile-up scratchpad which is per-page-load activity.
 *
 * Persistence tier: localStorage (per-device, survives reloads and
 * tab close) — same tier and mirror idiom as `qsoDefaults`. The list
 * is the operator's own phrasebook; it should outlive a tab.
 *
 * Discipline: newest at index 0; dedup moves an existing identical
 * entry back to the top rather than duplicating; empty/whitespace is
 * never stored; the list is capped at MAX_COMMENTS (oldest evicted
 * off the end).
 *
 * @see docs/v2-design/frontend-spa.md "Five-tier persistence"
 */

const COMMENT_HISTORY_KEY = 'sm.commentHistory.items';

/** Bound on the paste list — keeps the dropdown glanceable and the
 *  stored payload small. ~10 per the feature request. */
export const MAX_COMMENTS = 10;

function loadItems(): string[] {
    try {
        const raw = localStorage.getItem(COMMENT_HISTORY_KEY);
        if (raw === null) return [];
        const parsed: unknown = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        // Defensive hydration: a hand-edited / corrupted value must not
        // wedge the dropdown. Keep only non-empty strings and re-apply
        // the cap (a stored list longer than MAX_COMMENTS — e.g. after
        // a future cap reduction — is trimmed on load).
        return parsed
            .filter((x): x is string => typeof x === 'string' && x.trim() !== '')
            .slice(0, MAX_COMMENTS);
    } catch {
        // Missing / unparseable storage — start empty.
        return [];
    }
}

function saveItems(items: string[]): void {
    try {
        localStorage.setItem(COMMENT_HISTORY_KEY, JSON.stringify(items));
    } catch {
        // Storage write failed (quota / private mode) — in-memory state
        // is still correct; we lose cross-reload survival, nothing else.
    }
}

class CommentHistory {
    /**
     * Saved comments, newest at index 0. Plain string[]; the dropdown
     * iterates it directly and there's no per-entry metadata worth
     * modelling. `$state` because the Comment component's dropdown
     * renders from it and it must re-render when a submit prepends a
     * new entry.
     */
    items: string[] = $state(loadItems());

    /**
     * Record a logged comment. Trims; ignores empty; dedups (an
     * existing identical entry is removed first so the freshly-logged
     * copy lands at the top instead of duplicating); caps at
     * MAX_COMMENTS. Reassigns `items` wholesale so the mirror effect
     * and dropdown both see the change.
     */
    add(text: string): void {
        const t = text.trim();
        if (t === '') return;
        const next = this.items.filter((c) => c !== t);
        next.unshift(t);
        if (next.length > MAX_COMMENTS) next.length = MAX_COMMENTS;
        this.items = next;
    }

    /** Empty the paste list. For a future "clear saved comments"
     *  affordance and for test setup. */
    clear(): void {
        this.items = [];
    }
}

export const commentHistory = new CommentHistory();

// Module-level mirror to localStorage — same shape as
// qsoDefaults.svelte.ts. $effect.root because $effect can't run at a
// module's top level without a root context. The first-run latch
// skips the redundant write-back of the just-hydrated value on module
// load (pointless I/O + noise in storage-spy tests).
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

/**
 * Test-time teardown. Production code never calls this (module
 * lifetime = page lifetime). Vitest cases call it between runs so the
 * singleton's mirror effect doesn't leak across tests. Mirrors
 * `qsoDefaults.svelte.ts → _disposeForTests()`.
 */
export function _disposeForTests(): void {
    if (rootDispose) {
        rootDispose();
        rootDispose = null;
    }
}
