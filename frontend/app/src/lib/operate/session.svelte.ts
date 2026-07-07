// Session log state — QSOs logged since this window opened. In-memory by
// design: the daemon owns the durable log; this list is the operator's
// "what have I worked this sitting" glance. When the /v1 wiring lands the
// submit sink feeds it from the POST response (and FT8 rows arrive via the
// ft8-logged SSE), same as the logging SPA — the shape here stays.

export interface SessionQso {
    id: number;
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

export const session: { qsos: SessionQso[] } = $state({ qsos: [] });

let nextId = 1;

// Newest first — the row just logged is the one the operator glances at.
export function addSessionQso(q: Omit<SessionQso, 'id'>): void {
    session.qsos.unshift({ ...q, id: nextId++ });
}
