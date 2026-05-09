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
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

interface DaemonOk {
    items: ContactHistory[];
}

export async function fetchContactHistory(callsign: string): Promise<ContactHistoryOutcome> {
    let response: Response;
    try {
        response = await fetch(`/v1/contact-history?call=${encodeURIComponent(callsign)}`, {
            method: 'GET',
        });
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }

    let body: DaemonOk | DaemonError | null;
    try {
        body = (await response.json()) as DaemonOk | DaemonError;
    } catch {
        body = null;
    }

    if (response.ok) {
        const ok = body as DaemonOk | null;
        return { kind: 'ok', items: ok?.items ?? [] };
    }

    const err = body as DaemonError | null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
