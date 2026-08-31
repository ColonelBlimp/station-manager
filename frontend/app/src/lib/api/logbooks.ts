/*
    Logbook page daemon API (ported from the shipping logbook SPA, ADR 0044) — the read surface for browsing logged QSOs:
      - GET /v1/logbook                  → list logbooks (the selector)
      - GET /v1/logbook/{id}/count       → total QSO count (the "of N")
      - GET /v1/logbook/{id}/qso         → cursor-paginated QSO page
                                           (?limit, ?after; → {items, next_cursor})

    Cursor pagination is forward-only: each page carries a `next_cursor` (null when
    no more rows); the SPA keeps the per-page cursors to walk Next/Prev (no
    page-number jumps — the daemon has no offset endpoint, by design).
*/

import { daemonErrorMessage, isPlainObject, readJsonBody, safeFetch } from './_helpers';

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
    /** Canonical QSO identifier (AW-1) — the key for selection, edit, and row rendering. */
    uuid: string;
    /** DEPRECATED daemon-local numeric id — removed in v2.0.0-alpha.3. Do not key on it. */
    id?: number;
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
    // Enrichment-derived fields (flat on the wire like everything else) — read
    // by the bulk Re-enrich's skip-if-unchanged comparison, so an already-
    // correct row never fires a no-op PATCH (each PATCH re-arms a QRZ update
    // upload).
    dxcc?: string;
    cqz?: string;
    ituz?: string;
    cont?: string;
    // Enrichment-precomputed coordinates (decimal-degree strings when the
    // lookup provided them) — the contacts map plots these and falls back to
    // gridsquare when absent or unparseable (e.g. import-era ADIF Location
    // format), per its fail-soft "N of M plotted" rule.
    lat?: string;
    lon?: string;
    my_lat?: string;
    my_lon?: string;
    // Upload/forward status (e.g. "Y") — drive the callsign tint.
    sm_fwrd_by_email_status?: string;
    qrzcom_qso_upload_status?: string;
    clublog_qso_upload_status?: string;
}

export type LogbooksOutcome =
    { kind: 'ok'; logbooks: Logbook[] } | { kind: 'error'; message: string };

export type CountOutcome = { kind: 'ok'; count: number } | { kind: 'error'; message: string };

export type QsoPageOutcome =
    | { kind: 'ok'; items: LogbookQso[]; nextCursor: string | null }
    | { kind: 'error'; message: string };

const transportMessage = (kind: string): string =>
    kind === 'network' ? 'Cannot reach the daemon.' : 'Request failed.';

// Per-record decoders (F-03c, ADR 0077). The daemon's list endpoints feed long-lived keyed
// renders (the logbook selector, the QSO table), so a malformed or duplicate-keyed record would
// throw mid-render rather than degrade one row. decodeRecords drops the bad ones (keeping the
// good), deduplicates on the render key, and warns ONCE per response with the drop count.
function isLogbook(v: unknown): v is Logbook {
    return (
        isPlainObject(v) &&
        typeof v.id === 'number' &&
        typeof v.name === 'string' &&
        typeof v.callsign === 'string'
    );
}
// A page row keys selection, edit, and rendering on uuid, so a row without a usable one is unkeyed
// and dropped; every other field is display-only and left to the row to render defensively.
function isLogbookQso(v: unknown): v is LogbookQso {
    return isPlainObject(v) && typeof v.uuid === 'string' && v.uuid !== '';
}

function decodeRecords<T>(
    raw: unknown[],
    validate: (v: unknown) => v is T,
    keyOf: (v: T) => string | number,
    label: string
): T[] {
    const out: T[] = [];
    const seen = new Set<string | number>();
    let dropped = 0;
    for (const el of raw) {
        if (!validate(el)) {
            dropped++;
            continue;
        }
        const key = keyOf(el);
        if (seen.has(key)) {
            dropped++;
            continue;
        }
        seen.add(key);
        out.push(el);
    }
    if (dropped > 0) {
        console.warn(
            `[${label}] dropped ${dropped} of ${raw.length} malformed or duplicate record(s)`
        );
    }
    return out;
}

// next_cursor is the pagination boundary. The daemon always serializes it (a *string with no
// omitempty), so exactly a string (the opaque cursor) or null (no more rows) is valid. Anything
// else — a number, an object, or a MISSING key (undefined) — is a malformed boundary: stop paging
// (the safe side: never page past a cursor we cannot parse) and warn, rather than treat the gap as
// a clean end.
function decodeCursor(v: unknown, label: string): string | null {
    if (typeof v === 'string') return v;
    if (v === null) return null;
    console.warn(`[${label}] next_cursor was not string|null; treating as end of pagination`);
    return null;
}

/** List all logbooks for the selector. */
export async function fetchLogbooks(signal?: AbortSignal): Promise<LogbooksOutcome> {
    const fetched = await safeFetch('/v1/logbook', { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok)
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    if (!Array.isArray(body)) return { kind: 'error', message: 'Unexpected logbooks response.' };
    const logbooks = decodeRecords(body, isLogbook, (l) => l.id, 'logbooks');
    return { kind: 'ok', logbooks };
}

/** Total QSO count for a logbook (the "of N"). `missingFrom` (a forwarder name)
 *  restricts the count to QSOs not yet uploaded to that destination; `notEmailed`
 *  restricts it to QSOs not yet forwarded by email — both match the filtered page
 *  so the "of N" stays honest. */
export async function fetchLogbookCount(
    id: number,
    missingFrom?: string,
    notEmailed?: boolean,
    signal?: AbortSignal
): Promise<CountOutcome> {
    const q = new URLSearchParams();
    if (missingFrom) q.set('missing_from', missingFrom);
    if (notEmailed) q.set('not_emailed', 'true');
    const qs = q.toString();
    const fetched = await safeFetch(`/v1/logbook/${id}/count${qs ? `?${qs}` : ''}`, { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok)
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    if (!isPlainObject(body) || typeof body.count !== 'number') {
        return { kind: 'error', message: 'Unexpected count response.' };
    }
    return { kind: 'ok', count: body.count };
}

/** One cursor-paginated QSO page. `after` is the opaque cursor from a prior page's
 *  next_cursor; omit for the first page. `missingFrom` (a forwarder name) restricts
 *  the page to QSOs not yet uploaded to that destination (ADR 0039 backfill);
 *  `notEmailed` restricts it to QSOs not yet forwarded by email. */
export async function fetchQsoPage(
    id: number,
    limit: number,
    after?: string,
    missingFrom?: string,
    notEmailed?: boolean,
    signal?: AbortSignal
): Promise<QsoPageOutcome> {
    const q = new URLSearchParams({ limit: String(limit) });
    if (after) q.set('after', after);
    if (missingFrom) q.set('missing_from', missingFrom);
    if (notEmailed) q.set('not_emailed', 'true');
    const fetched = await safeFetch(`/v1/logbook/${id}/qso?${q}`, { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok)
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    if (!isPlainObject(body) || !Array.isArray(body.items)) {
        return { kind: 'error', message: 'Unexpected QSO-page response.' };
    }
    const items = decodeRecords(body.items, isLogbookQso, (q) => q.uuid, 'logbook-page');
    const nextCursor = decodeCursor(body.next_cursor, 'logbook-page');
    return { kind: 'ok', items, nextCursor };
}
