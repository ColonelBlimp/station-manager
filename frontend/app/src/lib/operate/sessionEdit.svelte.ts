/**
 * In-place edit for Session-panel rows (dogfood 2026-07-18): editing used to
 * mean navigating to the Logbook ROUTE, which unmounts the FT8 view and —
 * via demand-driven capture — takes a live CQ run off the air. Hosting the
 * shared EditQsoModal inside the Operate view removes that trap at the root:
 * no route change, so the FT8 subscription (and the mic) stay held.
 *
 * The session list carries a trimmed row, so open() hydrates the full QSO by
 * uuid first (GET /v1/qso/{uuid}); save() PATCHes and overlays the daemon's
 * canonical merged result back onto the session row.
 */

import { fetchQso, patchQso, type QsoPatch } from '../api/qso-patch';
import type { LogbookQso } from '../api/logbooks';
import { updateSessionQso, type SessionQso } from './session.svelte';

/** ADIF HHMM[SS] → the session list's HH:MM[:SS] display shape. */
function displayTime(t: string | undefined, fallback: string): string {
    if (t === undefined || !/^\d{4}(\d{2})?$/.test(t)) return fallback;
    const parts = [t.slice(0, 2), t.slice(2, 4)];
    if (t.length === 6) parts.push(t.slice(4, 6));
    return parts.join(':');
}

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

    /** The session-list id of the row being edited (the write-back target). */
    #sessionId = 0;

    async open(sq: SessionQso): Promise<void> {
        if (this.opening || sq.uuid === undefined || sq.uuid === '') return;
        this.opening = true;
        this.openError = null;
        const out = await fetchQso(sq.uuid);
        this.opening = false;
        if (out.kind !== 'ok') {
            this.openError = `Cannot load ${sq.callsign} for editing: ${out.message}`;
            return;
        }
        this.#sessionId = sq.id;
        this.error = null;
        this.row = out.qso;
    }

    close(): void {
        this.row = null;
        this.error = null;
        this.saving = false;
    }

    async save(patch: QsoPatch): Promise<void> {
        const uuid = this.row?.uuid;
        if (uuid === undefined || uuid === '') {
            this.error = 'This QSO has no id and cannot be edited.';
            return;
        }
        this.saving = true;
        this.error = null;
        const out = await patchQso(uuid, patch);
        this.saving = false;
        if (out.kind !== 'ok') {
            this.error = out.message;
            return;
        }
        // Write the daemon's canonical merged row back onto the session list.
        // Session rows show the operator-friendly mode literal (USB, FT8) —
        // that's ADIF submode when present, mode otherwise. Band may have been
        // re-derived from freq server-side, so overlay it too.
        const q = out.qso;
        const overlay: Partial<Omit<SessionQso, 'id'>> = {
            callsign: q.call ?? '',
            band: q.band ?? '',
            mode: q.submode !== undefined && q.submode !== '' ? q.submode : (q.mode ?? ''),
            rstSent: q.rst_sent ?? '',
            rstRcvd: q.rst_rcvd ?? '',
            name: q.name ?? '',
            country: q.country ?? '',
            comment: q.comment ?? '',
        };
        const t = displayTime(q.time_on, '');
        if (t !== '') overlay.timeOn = t;
        updateSessionQso(this.#sessionId, overlay);
        this.close();
    }
}

export const sessionEdit = new SessionEditState();
