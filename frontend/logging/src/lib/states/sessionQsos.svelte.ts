/**
 * Session QSO list — every QSO logged during the current operator
 * session, in submit order.
 *
 * Owner: SPA. The daemon already persists each QSO to its log; this
 * state is the per-session view ("what did I work in this sitting?")
 * that the SessionPanel renders + the email-ADIF flow ships.
 *
 * Persistence: sessionStorage, matching SessionTimer's lifecycle. The
 * session start moment lives at `sm.session.startedAt`; this list
 * lives at `sm.session.qsos`. Both naturally clear when the
 * operator's tab closes — that's what defines "session end" in the
 * UX. F5 / accidental refresh survives.
 *
 * Per ADR 0004 (daemon-vs-SPA split): the daemon owns canonical
 * persistence; this list holds only the fields the SessionPanel needs
 * to render. It deliberately does NOT snapshot the full ADIF record
 * (review 2026-06-19 L2): email-out posts UUIDs and the daemon rebuilds
 * ADIF from the live DB rows, so keeping a per-row ADIF copy was just
 * unnecessary QSO/station PII sitting in sessionStorage.
 *
 * `Distance` is snapshotted from `enrichmentState.paths` at submit
 * time — once the operator Tabs the next callsign, that data is
 * gone from enrichmentState. Without the snapshot the column would
 * show stale numbers (or empty) for any but the most recently logged
 * QSO.
 */

const STORAGE_KEY = 'sm.session.qsos';

export interface SessionQso {
    /** Daemon-issued canonical id (UUIDv7) — used for edit / delete. */
    uuid: string;
    callsign: string;
    name: string;
    /** Raw transmit frequency in Hz; SessionPanel formats via formatFrequency for display. */
    freqHz: number;
    /** Band string, e.g. "20m". */
    band: string;
    rstSent: string;
    rstRcvd: string;
    /** Mode display name (USB / FT8 / etc.) — submode form, not ADIF parent family. */
    mode: string;
    /** UTC HH:MM (e.g. "14:32"). */
    timeOn: string;
    /** UTC YYYY-MM-DD. */
    qsoDate: string;
    /** Country display name from enrichment, or empty if not enriched. */
    country: string;
    /** Active-path distance in km (string for display tolerance — empty when grids unavailable). */
    distanceKm: string;

    /**
     * UTC YYYYMMDD of the send that last included this QSO in a session
     * email, mirrored from the daemon's durable sm_fwrd_by_email_date
     * stamp. Empty / undefined means "not yet emailed to the QSL
     * manager." Set by markEmailed() after a successful send; the
     * daemon row is the source of truth, this is the session-view
     * mirror so the panel can render a Sent indicator without a
     * per-row refetch.
     */
    emailedDate?: string;
}

function loadFromStorage(): SessionQso[] {
    try {
        const raw = sessionStorage.getItem(STORAGE_KEY);
        if (raw === null) return [];
        const parsed = JSON.parse(raw) as unknown;
        if (!Array.isArray(parsed)) return [];
        // Trust the shape — written by this same module. A future
        // schema change will need a version field; today the only
        // failure mode is a corrupted storage entry, which we treat
        // as "start fresh" rather than crashing the panel.
        return parsed as SessionQso[];
    } catch {
        return [];
    }
}

function saveToStorage(items: SessionQso[]): void {
    try {
        sessionStorage.setItem(STORAGE_KEY, JSON.stringify(items));
    } catch {
        // Quota / private-browsing edge cases. The in-memory state is
        // still authoritative for this tab; refresh-survival is what
        // we lose, which is graceful.
    }
}

class SessionQsosState {
    /** Items are kept in submit order — oldest first; SessionPanel renders newest-first via slice().reverse() at render time. */
    items: SessionQso[] = $state(loadFromStorage());

    /** Drives the "Session (N)" badge on the InfoPanel tab. */
    count: number = $derived(this.items.length);

    /**
     * add — append on a successful submit. Persists immediately
     * because a crashed tab between Add and the next mutation would
     * otherwise lose the entry from sessionStorage. Order is the
     * operator's chronological log; UUIDs are unique, but we don't
     * dedupe here — the daemon already enforces the dedupe key on
     * its side (a duplicate POST returns kind=duplicate without
     * calling add()).
     */
    add(qso: SessionQso): void {
        this.items.push(qso);
        saveToStorage(this.items);
    }

    /**
     * update — replace the row keyed by uuid. Used by the Edit overlay
     * once a PATCH completes. No-op if the uuid isn't present (defensive
     * — the row could have been wiped by clear() between the user
     * opening the overlay and submitting the edit).
     */
    update(uuid: string, qso: SessionQso): void {
        const idx = this.items.findIndex((q) => q.uuid === uuid);
        if (idx < 0) return;
        this.items[idx] = qso;
        saveToStorage(this.items);
    }

    /**
     * markEmailed — stamp the given UUIDs with the send date after a
     * successful session email. Mirrors the daemon's durable
     * sm_fwrd_by_email_date write so the SessionPanel can render a Sent
     * indicator immediately. Rows whose UUID isn't in the set are left
     * untouched (a QSO logged after the send stays unsent until the
     * next send includes it). No-op for an empty set — which is also
     * the daemon's "sent but stamp-write failed" signal, leaving those
     * rows visually unsent for a re-send / Logbook-SPA fix.
     */
    markEmailed(uuids: string[], date: string): void {
        if (uuids.length === 0 || date === '') return;
        const set = new Set(uuids);
        let changed = false;
        for (const q of this.items) {
            if (set.has(q.uuid)) {
                q.emailedDate = date;
                changed = true;
            }
        }
        if (changed) saveToStorage(this.items);
    }

    /**
     * clear — wipe the session list. Currently no UI calls this; the
     * sessionStorage lifecycle (cleared on tab close) is the natural
     * reset. Provided for tests + a future "Reset session" affordance
     * if the operator ever asks for one.
     */
    clear(): void {
        this.items = [];
        saveToStorage(this.items);
    }
}

export const sessionQsosState = new SessionQsosState();
