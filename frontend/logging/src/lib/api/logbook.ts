/*
    Thin daemon-side wrappers for `/v1/logbook/{id}/*` endpoints that the
    logging SPA consumes. Today: just the QSO-count probe used by the
    LoggingCard header. Other logbook CRUD lives in the (future) logbook
    SPA per `feedback_logging_vs_logbook_scope`.

    Wire contract (handler_logbook_count.go):
      - 200 OK  → { logbook_id: <int64>, count: <int64> }
      - 400 Bad Request → { code, message } (malformed id)
      - 404 Not Found   → { code, message } (logbook does not exist)
      - 500 Server      → { code, message } (db error)
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type LogbookCountOutcome =
    | { kind: 'ok'; count: number }
    | { kind: 'not_found'; message: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function fetchLogbookCount(
    logbookID: number,
    signal?: AbortSignal
): Promise<LogbookCountOutcome> {
    const fetched = await safeFetch(`/v1/logbook/${logbookID}/count`, {
        method: 'GET',
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const body = await readJsonBody(response);

    if (response.ok) {
        if (!isPlainObject(body) || typeof body.count !== 'number') {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned logbook count without a numeric count field',
            };
        }
        return { kind: 'ok', count: body.count };
    }

    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status === 404) {
        return { kind: 'not_found', message };
    }
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
