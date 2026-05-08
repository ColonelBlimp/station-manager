/**
 * Contact-history state — latest `/v1/contact-history` response for
 * the currently-Tabbed callsign.
 *
 * Lives at the SPA level. Persistence tier: in-memory only — the
 * panel's purpose is "have I worked THIS callsign before"; surviving
 * a refresh would mean showing stale data for whoever was last in
 * the form.
 *
 * Reactivity: `items` is `$state` because the WorkedPanel template
 * iterates it and the InfoPanel tab strip binds `count` to its
 * length. `count` is a `$derived` so the tab badge updates without
 * a manual recompute.
 */

import type { ContactHistory } from '../api/contact-history';

class ContactHistoryState {
    /**
     * Most recent contact-history result, or `null` when no Tab has
     * fetched yet (or after `clear()` post-submit). Templates branch
     * on `items === null` to render the empty / pre-Tab state.
     */
    items: ContactHistory[] | null = $state(null);

    /**
     * Count of prior QSOs with the current callsign. 0 when the
     * fetch returned an empty list ("never worked them"); 0 also
     * when no fetch has run yet — both cases want the InfoPanel
     * badge to read `(0)`. Distinct from `items === null` (which is
     * the rendering "show empty state vs hide content" gate).
     */
    count: number = $derived(this.items?.length ?? 0);

    /**
     * setResult — called by `QsoPanel.handleEnrich` after the
     * fetch returns. Even an empty list lands here (so `items`
     * flips from null → [] and the badge updates from "no fetch
     * yet" to "0 prior contacts").
     */
    setResult(rows: ContactHistory[]): void {
        this.items = rows;
    }

    /**
     * clear — called after a successful QSO submit and from the
     * Clear button, mirroring `enrichmentState.clear()` so each
     * QSO starts with a fresh InfoPanel.
     */
    clear(): void {
        this.items = null;
    }
}

export const contactHistoryState = new ContactHistoryState();
