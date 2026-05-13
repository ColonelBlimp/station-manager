/*
    Thin daemon-side wrappers for `GET /v1/qso/{uuid}` (fetch the
    record for editing) and `PATCH /v1/qso/{uuid}` (persist the
    edits). Discriminated outcomes follow the same convention as
    `lib/api/qso.ts`, `lib/api/config.ts`, `lib/api/session-email.ts`.

    Wire contract (handler_qso.go):
      - GET  200 OK    → types.Qso JSON
      - GET  404       → { code: "not_found", ... }
      - PATCH 200 OK   → types.Qso JSON (the merged result)
      - PATCH 400      → { code, message, op? } (validation, invalid_json,
                          missing_required_field, invalid_field_value,
                          invalid_time_range, invalid_uuid)
      - PATCH 404      → { code: "not_found", ... }
      - PATCH 409      → { code: "duplicate_key", ... }
      - PATCH 5xx      → { code, message, op? }

    `kind` collapses to:
      - 'ok'         — record fetched / patched successfully.
      - 'not_found'  — UUID didn't resolve (operator opened an overlay
                       for a row that was deleted from another tab).
      - 'duplicate'  — PATCH would collide with another row in the
                       same logbook on the dedupe key.
      - 'validation' — 4xx other than 404/409.
      - 'server'     — 5xx; daemon logged a stack-tagged error.
      - 'aborted'    — caller cancelled via AbortSignal before a response.
      - 'network'    — fetch threw before a Response.
*/

import type { DaemonQsoForEdit } from '../states/qsoEdit.svelte';
import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type FetchQsoOutcome =
    | { kind: 'ok'; qso: DaemonQsoForEdit }
    | { kind: 'not_found'; message: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

export type PatchQsoOutcome =
    | { kind: 'ok'; qso: DaemonQsoForEdit }
    | { kind: 'not_found'; message: string }
    | { kind: 'duplicate'; message: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function fetchQso(uuid: string, signal?: AbortSignal): Promise<FetchQsoOutcome> {
    const fetched = await safeFetch(`/v1/qso/${encodeURIComponent(uuid)}`, { signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const body = await readJsonBody(response);

    if (response.ok) {
        // Guard against a 200 OK with an unparseable / non-object body
        // — without this the overlay would land on { qso: null } and
        // crash on the first field read. Treat as malformed server
        // response.
        if (!isPlainObject(body)) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a non-JSON or empty body for GET /v1/qso/{uuid}',
            };
        }
        return { kind: 'ok', qso: body };
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

export async function patchQso(
    uuid: string,
    body: DaemonQsoForEdit,
    signal?: AbortSignal
): Promise<PatchQsoOutcome> {
    const fetched = await safeFetch(`/v1/qso/${encodeURIComponent(uuid)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const respBody = await readJsonBody(response);

    if (response.ok) {
        // Same malformed-body guard as fetchQso above — a 200 with a
        // null body would bypass every subsequent field read.
        if (!isPlainObject(respBody)) {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a non-JSON or empty body for PATCH /v1/qso/{uuid}',
            };
        }
        return { kind: 'ok', qso: respBody };
    }

    const err = isPlainObject(respBody) ? (respBody as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status === 404) {
        return { kind: 'not_found', message };
    }
    if (response.status === 409) {
        return { kind: 'duplicate', message };
    }
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
