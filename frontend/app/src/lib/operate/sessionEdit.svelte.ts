/**
 * In-place edit for Session-panel rows (dogfood 2026-07-18): editing used to
 * mean navigating to the Logbook ROUTE, which unmounts the FT8 view and —
 * via demand-driven capture — takes a live CQ run off the air. Hosting the
 * shared EditQsoModal inside the Operate view removes that trap at the root:
 * no route change, so the FT8 subscription (and the mic) stay held.
 *
 * The session list carries a trimmed row, so open() hydrates the full QSO by
 * uuid first (GET /v1/qso/{uuid}); save() PATCHes it.
 *
 * The saved contact is not written into the list here (W-0029 part 6, A6, B5).
 * The list is what the page has read of the daemon's session: a save asks for
 * the rows to be read again, and the row then shows the daemon's merged
 * contact. So an answer that returns after the session has changed has nowhere
 * to be written: the new session's rows are the new session's.
 */

import { fetchQso, patchQso, type QsoPatch } from '../api/qso-patch';
import type { LogbookQso } from '../api/logbooks';
import { refreshSessionRows, type SessionQso } from './session.svelte';
import { sessionActionBlock } from './archiveStores.svelte';

class SessionEditState {
    /** The hydrated row the modal edits; null = modal closed. */
    row: LogbookQso | null = $state(null);
    saving: boolean = $state(false);
    /** Save failure, shown inside the modal. */
    error: string | null = $state(null);
    /** Hydration failure, shown in the panel (the modal never opened). */
    openError: string | null = $state(null);
    /** True while the row fetch is in flight (row click → modal open). */
    opening: boolean = $state(false);

    /** Bumped on every open/close. A save captures it before the PATCH and
     *  leaves the modal alone if it was closed (or reopened on another row)
     *  meanwhile: row A's answer must not close or mark the edit of row B. */
    #gen = 0;

    async open(sq: SessionQso): Promise<void> {
        if (this.opening || this.saving || sq.uuid === undefined || sq.uuid === '') return;
        // An unproven archive sends nothing: the row may be another archive's.
        const blocked = sessionActionBlock();
        if (blocked !== null) {
            this.openError = blocked;
            return;
        }
        this.opening = true;
        this.openError = null;
        const out = await fetchQso(sq.uuid);
        this.opening = false;
        if (out.kind !== 'ok') {
            this.openError = `Cannot load ${sq.callsign} for editing: ${out.message}`;
            return;
        }
        this.#gen++;
        this.error = null;
        this.row = out.qso;
    }

    close(): void {
        this.#gen++;
        this.row = null;
        this.error = null;
        this.saving = false;
    }

    async save(patch: QsoPatch): Promise<void> {
        if (this.saving) return; // Ctrl+Enter repeat while a PATCH is in flight
        const blocked = sessionActionBlock();
        if (blocked !== null) {
            this.error = blocked; // the modal and its edit stay open for when it clears
            return;
        }
        const uuid = this.row?.uuid;
        if (uuid === undefined || uuid === '') {
            this.error = 'This QSO has no id and cannot be edited.';
            return;
        }
        const gen = this.#gen;
        this.saving = true;
        this.error = null;
        const out = await patchQso(uuid, patch);
        // The contact is saved whether or not the modal is still on it: the rows
        // are read again either way, and show what the daemon holds.
        if (out.kind === 'ok') refreshSessionRows();
        if (gen !== this.#gen) return; // the modal moved on
        this.saving = false;
        if (out.kind !== 'ok') {
            this.error = out.message;
            return;
        }
        this.close();
    }
}

export const sessionEdit = new SessionEditState();
