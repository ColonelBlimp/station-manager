/*
    Thin daemon-side wrapper for `POST /v1/qso`. Returns a discriminated
    union so the caller can branch on outcome without parsing HTTP / JSON
    error envelopes inline.

    Wire contract (api.md §4.2 + §4.6, ADR 0016):
      - 201 Created  → { status: "stored",    uuid: <UUIDv7>, id: <int64> }
      - 200 OK       → { status: "duplicate", uuid: <UUIDv7>, id: <int64> }   (dedupe-checked, non-force)
      - 4xx          → { code, message, op? }                                  (validation / client error)
      - 5xx          → { code, message, op? }                                  (server / db error)

    The UUIDv7 is the canonical external identifier per ADR 0016. The
    daemon also returns the local int PK for transitional logging
    only; everything client-facing should key off `uuid`.

    `kind` collapses to:
      - 'stored'     — fresh QSO persisted, draft can be cleared
      - 'duplicate'  — already-logged QSO matched on dedupe key; daemon
                       did not touch the row. Caller decides whether to
                       offer ?force=1 retry.
      - 'validation' — 4xx with a daemon-emitted code (e.g.
                       'invalid_adif', 'callsign_mismatch'). Caller
                       surfaces the message; draft is preserved.
      - 'server'     — 5xx; daemon logged a stack-tagged error. Caller
                       shows a generic retry message; draft is preserved.
      - 'aborted'    — caller cancelled via AbortSignal before a response.
                       Draft preserved; UI should treat this as "no-op."
      - 'network'    — fetch threw before a Response (daemon unreachable,
                       DNS, CORS preflight failure). Draft preserved.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type SubmitOutcome =
    | { kind: 'stored'; uuid: string }
    | { kind: 'duplicate'; uuid: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export interface SubmitOptions {
    /**
     * When true, append `?force=1` so the daemon bypasses dedupe and
     * stores the QSO even if a matching row already exists. The
     * doc-comment above promises this knob; this is its wire seam.
     * Default false — operator confirms the duplicate intentionally.
     */
    force?: boolean;
    signal?: AbortSignal;
}

export async function submitQso(
    adif: string,
    logbookID: number,
    options: SubmitOptions = {}
): Promise<SubmitOutcome> {
    const params = new URLSearchParams({ logbook: String(logbookID) });
    if (options.force) params.set('force', '1');
    const fetched = await safeFetch(`/v1/qso?${params.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-adif' },
        body: adif,
        signal: options.signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const body = await readJsonBody(response);

    if (response.ok) {
        // The uuid is load-bearing — every downstream consumer
        // (sessionQsos, the edit overlay, the email-out flow) keys off
        // it. A 200 with a missing or empty uuid (proxy interference,
        // daemon regression) would propagate phantom empty IDs and
        // corrupt the session list silently. Downgrade to malformed.
        if (
            !isPlainObject(body) ||
            typeof body.uuid !== 'string' ||
            body.uuid === ''
        ) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a successful submit without a uuid',
            };
        }
        if (body.status === 'duplicate') {
            return { kind: 'duplicate', uuid: body.uuid };
        }
        return { kind: 'stored', uuid: body.uuid };
    }

    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
