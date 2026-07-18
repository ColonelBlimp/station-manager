// Session log state — QSOs logged since this sitting began. The daemon owns the
// durable log; this list is the operator's "what have I worked this sitting"
// glance, fed by the submit-response (Phone/CW) and the ft8-logged SSE (FT8).
//
// Persisted to sessionStorage so it survives a page reload — a dogfood redeploy
// (or any F5) reloads /app/, and without this the whole session's contacts would
// vanish from the review/export panel. sessionStorage (not localStorage) matches
// the "this sitting" semantics: a reload keeps it, closing the tab ends it.

export interface SessionQso {
    id: number;
    /** Canonical daemon UUIDv7 (ADR 0016) — the email-out marks + a future edit
     *  action key off it. */
    uuid?: string;
    callsign: string;
    timeOn: string; // HH:MM:SS (UTC)
    band: string; // rig-provided at log time (rig.svelte)
    mode: string; // ditto — and what tells FT8 rows from Phone/CW in the shared session list
    rstSent: string;
    rstRcvd: string;
    name: string;
    country: string; // enrichment-provided, '' when unknown
    comment: string;
}

const STORE_KEY = 'sm.session.qsos';

// Hydrate from sessionStorage at boot; fail-soft to an empty session on any
// problem (private mode, malformed value). nextId continues past the stored max so
// keyed-each ids stay unique after a reload.
function hydrate(): { qsos: SessionQso[]; nextId: number } {
    try {
        const raw = sessionStorage.getItem(STORE_KEY);
        if (raw) {
            const parsed: unknown = JSON.parse(raw);
            if (
                parsed &&
                typeof parsed === 'object' &&
                Array.isArray((parsed as { qsos?: unknown }).qsos)
            ) {
                const qsos = (parsed as { qsos: SessionQso[] }).qsos;
                const maxId = qsos.reduce((m, q) => Math.max(m, q.id ?? 0), 0);
                return { qsos, nextId: maxId + 1 };
            }
        }
    } catch {
        // storage unavailable / malformed — start empty, session-only
    }
    return { qsos: [], nextId: 1 };
}

const booted = hydrate();

export const session: { qsos: SessionQso[] } = $state({ qsos: booted.qsos });

let nextId = booted.nextId;

function persist(): void {
    try {
        sessionStorage.setItem(STORE_KEY, JSON.stringify({ qsos: session.qsos }));
    } catch {
        // storage unavailable — the session stays in-memory only, which is fine
    }
}

// Newest first — the row just logged is the one the operator glances at.
export function addSessionQso(q: Omit<SessionQso, 'id'>): void {
    session.qsos.unshift({ ...q, id: nextId++ });
    persist();
}

/** Overlay edited fields onto a session row (the in-place edit's write-back —
 *  the daemon's canonical merged QSO wins over what was captured at log time). */
export function updateSessionQso(id: number, patch: Partial<Omit<SessionQso, 'id'>>): void {
    const i = session.qsos.findIndex((q) => q.id === id);
    if (i === -1) return;
    session.qsos[i] = { ...session.qsos[i], ...patch };
    persist();
}

/** Test seam — clear the session (state + persisted copy) between cases. */
export function _resetSessionForTests(): void {
    session.qsos.length = 0;
    nextId = 1;
    try {
        sessionStorage.removeItem(STORE_KEY);
    } catch {
        /* ignore */
    }
}
