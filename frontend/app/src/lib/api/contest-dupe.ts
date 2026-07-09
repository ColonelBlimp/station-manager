/*
    Thin daemon-side wrapper for `GET /v1/contest-dupe?logbook=&call=&band=[&mode=]`.

    Wire contract (handler_contest_dupe.go):
      - 200 OK → { duplicate: boolean } — "have I worked this call on this
        band (and mode, if given) in this logbook already?"
      - 400 Bad Request → { code, message } (missing/invalid logbook, call, or band)
      - 404 Not Found    → { code, message } (logbook does not exist)
      - 500 Server       → { code, message } (db error)

    First consumer is the FT8 Band Activity feed's worked-before highlight: it
    asks per CQ callsign on the current band+mode. The lookup is best-effort —
    any non-ok outcome leaves the row undecorated (highlight absent), never an
    error surfaced to the operator, matching the "enrichment degrades, never
    blocks" posture.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type ContestDupeOutcome =
    | { kind: 'ok'; duplicate: boolean }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export interface ContestDupeQuery {
    logbook: number;
    call: string;
    band: string;
    mode?: string;
}

export async function fetchContestDupe(
    q: ContestDupeQuery,
    signal?: AbortSignal
): Promise<ContestDupeOutcome> {
    const params = new URLSearchParams({
        logbook: String(q.logbook),
        call: q.call,
        band: q.band,
    });
    if (q.mode) params.set('mode', q.mode);

    const fetched = await safeFetch(`/v1/contest-dupe?${params.toString()}`, {
        method: 'GET',
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const body = await readJsonBody(response);

    if (response.ok) {
        // A well-formed success is `{duplicate: bool}`. A 200 whose body lacks a
        // boolean `duplicate` is a daemon regression, not a "not a dupe" answer:
        // surface it as malformed rather than silently treating it as false.
        if (!isPlainObject(body) || typeof body.duplicate !== 'boolean') {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a 200 without a boolean duplicate for /v1/contest-dupe',
            };
        }
        return { kind: 'ok', duplicate: body.duplicate };
    }

    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
