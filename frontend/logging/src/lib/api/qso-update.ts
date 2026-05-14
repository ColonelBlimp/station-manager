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

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

/**
 * Subset of `types.Qso` the SPA populates from `GET /v1/qso/{uuid}`
 * and patches via `PATCH /v1/qso/{uuid}`. Mirrors what the edit
 * overlay exposes — callsign, name, QTH, comment, RST pair, mode,
 * frequency, date, times, Details-panel fields (rig, rx_pwr, notes),
 * and the operator's APP_SM_REQUEST_QSL flag. Country is
 * read-back-required (daemon rejects empty country); other
 * enrichment-derived fields aren't surfaced because the overlay's
 * purpose is "fix the obvious typo," not "rewrite arbitrary tags".
 *
 * Wire shape is FLAT: `types.Qso` embeds `QsoDetails`,
 * `ContactedStation`, `LoggingStation`, and `Qsl` as anonymous structs,
 * and Go's encoding/json promotes their fields to the top level on
 * marshal. So `call`, `band`, `freq`, etc. all appear as siblings of
 * `uuid`, NOT nested under `contacted_station` / `qso_details`. The
 * PATCH body shape is the same (qsoservice.Update calls
 * `json.Unmarshal(body, &merged)` against the flat struct). An earlier
 * nested-shape draft in `qsoEdit.svelte.ts` silently failed populate()
 * because the interface didn't match the wire — every field read as
 * undefined, the form rendered blank.
 */
export interface DaemonQsoForEdit {
    uuid?: string;
    /**
     * APP_SM_REQUEST_QSL flag — operator's reminder to request a QSL
     * card. Top-level on types.Qso because it's a station-manager
     * application-extension field, not an embedded sub-struct's field.
     */
    app_sm_request_qsl?: boolean;

    // ContactedStation — promoted from the embedded struct.
    call?: string;
    name?: string;
    qth?: string;
    country?: string;

    // QsoDetails — promoted from the embedded struct.
    rst_sent?: string;
    rst_rcvd?: string;
    mode?: string;
    freq?: string;
    freq_rx?: string;
    band?: string;
    qso_date?: string;
    qso_date_off?: string;
    time_on?: string;
    time_off?: string;
    comment?: string;
    rig?: string;
    rx_pwr?: string;
    notes?: string;
}

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
