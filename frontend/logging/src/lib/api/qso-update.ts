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
      - 'network'    — fetch threw before a Response.
*/

import type { DaemonQsoForEdit } from '../states/qsoEdit.svelte';

export type FetchQsoOutcome =
    | { kind: 'ok'; qso: DaemonQsoForEdit }
    | { kind: 'not_found'; message: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'network'; message: string };

export type PatchQsoOutcome =
    | { kind: 'ok'; qso: DaemonQsoForEdit }
    | { kind: 'not_found'; message: string }
    | { kind: 'duplicate'; message: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function fetchQso(uuid: string): Promise<FetchQsoOutcome> {
    let response: Response;
    try {
        response = await fetch(`/v1/qso/${encodeURIComponent(uuid)}`);
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }

    let body: unknown;
    try {
        body = await response.json();
    } catch {
        body = null;
    }

    if (response.ok) {
        // Guard against a 200 OK with an unparseable / non-object body
        // — without this the overlay would land on { qso: null } and
        // crash on the first field read. Treat as malformed server
        // response.
        if (body === null || typeof body !== 'object') {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a non-JSON or empty body for GET /v1/qso/{uuid}',
            };
        }
        return { kind: 'ok', qso: body };
    }

    const err = body as DaemonError | null;
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
    body: DaemonQsoForEdit
): Promise<PatchQsoOutcome> {
    let response: Response;
    try {
        response = await fetch(`/v1/qso/${encodeURIComponent(uuid)}`, {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }

    let respBody: unknown;
    try {
        respBody = await response.json();
    } catch {
        respBody = null;
    }

    if (response.ok) {
        // Same malformed-body guard as fetchQso above — a 200 with a
        // null body would bypass every subsequent field read.
        if (respBody === null || typeof respBody !== 'object') {
            return {
                kind: 'server',
                code: 'malformed_response',
                message: 'daemon returned a non-JSON or empty body for PATCH /v1/qso/{uuid}',
            };
        }
        return { kind: 'ok', qso: respBody };
    }

    const err = respBody as DaemonError | null;
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
