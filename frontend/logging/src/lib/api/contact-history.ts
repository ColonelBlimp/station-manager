/*
    Thin daemon-side wrapper for `GET /v1/contact-history?call=X`.

    Wire contract (api.md + handler_contact_history.go):
      - 200 OK → { items: ContactHistory[] } — newest-first, capped at
        Server.MaxContactHistoryResults (default 100).
      - 400 Bad Request → { code, message } (missing or invalid call).
      - 500 Server → { code, message } (db error).

    Empty results are 200 with `items: []`, not 404 — "never worked
    them" is a normal answer, not an error.

    The optional `?logbook=<id>` filter is intentionally omitted from
    the SPA wrapper for now: operators almost always want "have I ever
    worked them" rather than "in this contest only". A logbook filter
    can land alongside the contest panel when that ships.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface ContactHistory {
    id: number;
    uuid: string;
    band: string;
    freq: string;
    mode: string;
    qso_date: string; // ADIF YYYYMMDD
    time_on: string; // ADIF HHMM
    name: string;
    country: string;
    call: string;
    rst_sent: string;
    rst_rcvd: string;
    notes: string;
}

export type ContactHistoryOutcome =
    | { kind: 'ok'; items: ContactHistory[] }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function fetchContactHistory(
    callsign: string,
    signal?: AbortSignal
): Promise<ContactHistoryOutcome> {
    const fetched = await safeFetch(`/v1/contact-history?call=${encodeURIComponent(callsign)}`, {
        method: 'GET',
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const body = await readJsonBody(response);

    if (response.ok) {
        // 200 with an unparseable body is a daemon regression — every
        // success path on this endpoint emits at least `{items: []}`.
        // Empty array on a missing `items` field preserves the same
        // "no contacts" outcome the panel renders for a fresh callsign.
        const items =
            isPlainObject(body) && Array.isArray(body.items)
                ? (body.items as ContactHistory[])
                : [];
        return { kind: 'ok', items };
    }

    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
