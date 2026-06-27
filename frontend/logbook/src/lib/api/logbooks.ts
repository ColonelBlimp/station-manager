/*
    Logbook SPA daemon API — the read surface for browsing logged QSOs:
      - GET /v1/logbook                  → list logbooks (the selector)
      - GET /v1/logbook/{id}/count       → total QSO count (the "of N")
      - GET /v1/logbook/{id}/qso         → cursor-paginated QSO page
                                           (?limit, ?after; → {items, next_cursor})

    Cursor pagination is forward-only: each page carries a `next_cursor` (null when
    no more rows); the SPA keeps the per-page cursors to walk Next/Prev (no
    page-number jumps — the daemon has no offset endpoint, by design).
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

/** One logbook (mirrors types.Logbook; only the fields the SPA shows). */
export interface Logbook {
    id: number;
    name: string;
    callsign: string;
    description?: string;
}

/**
 * A QSO row as served by the daemon. types.Qso embeds QsoDetails / ContactedStation
 * / LoggingStation, which Go promotes into ONE flat JSON object — so these are all
 * top-level keys. Only the fields the table renders are typed here.
 */
export interface LogbookQso {
    id: number;
    uuid?: string;
    qso_date?: string; // ADIF YYYYMMDD
    qso_date_off?: string; // ADIF YYYYMMDD (overnight QSOs)
    time_on?: string; // ADIF HHMM[SS]
    time_off?: string; // ADIF HHMM[SS]
    call?: string;
    band?: string;
    freq?: string; // ADIF-native MHz decimal (e.g. "14.074")
    mode?: string;
    submode?: string;
    rst_sent?: string;
    rst_rcvd?: string;
    country?: string;
    name?: string;
    gridsquare?: string;
    comment?: string;
    // Upload/forward status (e.g. "Y") — drive the callsign tint.
    sm_fwrd_by_email_status?: string;
    qrzcom_qso_upload_status?: string;
    clublog_qso_upload_status?: string;
}

export type LogbooksOutcome =
    | { kind: 'ok'; logbooks: Logbook[] }
    | { kind: 'error'; message: string };

export type CountOutcome = { kind: 'ok'; count: number } | { kind: 'error'; message: string };

export type QsoPageOutcome =
    | { kind: 'ok'; items: LogbookQso[]; nextCursor: string | null }
    | { kind: 'error'; message: string };

const transportMessage = (kind: string): string =>
    kind === 'network' ? 'Cannot reach the daemon.' : 'Request failed.';

/** List all logbooks for the selector. */
export async function fetchLogbooks(signal?: AbortSignal): Promise<LogbooksOutcome> {
    const fetched = await safeFetch('/v1/logbook', { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    if (!fetched.response.ok)
        return { kind: 'error', message: `Daemon error (${fetched.response.status}).` };
    const body = await readJsonBody(fetched.response);
    if (!Array.isArray(body)) return { kind: 'error', message: 'Unexpected logbooks response.' };
    return { kind: 'ok', logbooks: body as Logbook[] };
}

/** Total QSO count for a logbook (the "of N"). */
export async function fetchLogbookCount(id: number, signal?: AbortSignal): Promise<CountOutcome> {
    const fetched = await safeFetch(`/v1/logbook/${id}/count`, { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    if (!fetched.response.ok)
        return { kind: 'error', message: `Daemon error (${fetched.response.status}).` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body) || typeof body.count !== 'number') {
        return { kind: 'error', message: 'Unexpected count response.' };
    }
    return { kind: 'ok', count: body.count };
}

/** One cursor-paginated QSO page. `after` is the opaque cursor from a prior page's
 *  next_cursor; omit for the first page. */
export async function fetchQsoPage(
    id: number,
    limit: number,
    after?: string,
    signal?: AbortSignal
): Promise<QsoPageOutcome> {
    const q = new URLSearchParams({ limit: String(limit) });
    if (after) q.set('after', after);
    const fetched = await safeFetch(`/v1/logbook/${id}/qso?${q}`, { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    if (!fetched.response.ok)
        return { kind: 'error', message: `Daemon error (${fetched.response.status}).` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body) || !Array.isArray(body.items)) {
        return { kind: 'error', message: 'Unexpected QSO-page response.' };
    }
    const nextCursor = typeof body.next_cursor === 'string' ? body.next_cursor : null;
    return { kind: 'ok', items: body.items as LogbookQso[], nextCursor };
}
