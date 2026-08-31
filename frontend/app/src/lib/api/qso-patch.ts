/*
    QSO edit API — PATCH /v1/qso/{uuid}. Ported from the shipping logbook SPA
    (ADR 0044 consolidation).

    The daemon overlays the JSON patch onto the stored QSO: keys present in the
    body are applied, everything else is left unchanged, and immutable fields
    (identity, logbook, station callsign, forwarding state, enrichment) are
    restored server-side regardless of what we send. So the SPA only sends the
    editable fields it shows; the daemon re-validates the merged result and
    re-derives band from freq. A validation failure comes back as a 400/409 with
    the {code, message} envelope; the message is operator-facing enough to show.
*/

import {
    DEFAULT_TIMEOUT_MS,
    isPlainObject,
    readJsonBody,
    safeFetch,
    WRITE_TIMEOUT_MS,
} from './_helpers';
import type { LogbookQso } from './logbooks';

// F-03c (ADR 0077): a single-record GET/PATCH response must be a plain object (an array is NOT —
// typeof [] === 'object' would otherwise pass and land an array in the store as a QSO) whose uuid
// EQUALS the one we requested. A mismatched or missing uuid means a wrong or corrupted record;
// return it as an error so the caller never writes it to the store keyed as the edited QSO.
function decodeQso(body: unknown, uuid: string, label: string): PatchOutcome {
    if (!isPlainObject(body)) return { kind: 'error', message: label };
    if (typeof body.uuid !== 'string' || body.uuid !== uuid) {
        return { kind: 'error', message: 'The daemon returned a QSO with an unexpected id.' };
    }
    return { kind: 'ok', qso: body as unknown as LogbookQso };
}

/** The editable subset of a QSO, by ADIF JSON tag. All optional — only the
 *  fields the edit form touches are sent. */
export interface QsoPatch {
    qso_date?: string;
    qso_date_off?: string;
    time_on?: string;
    time_off?: string;
    call?: string;
    freq?: string;
    band?: string;
    mode?: string;
    submode?: string;
    rst_sent?: string;
    rst_rcvd?: string;
    country?: string;
    name?: string;
    gridsquare?: string;
    comment?: string;
    // Enrichment-derived fields, carried by the Re-enrich repair path (the
    // 2026-07-13 DXCC backfill proved these PATCH-writable — the daemon's
    // immutable stash-restore covers identity/forwarding, not these).
    dxcc?: string;
    cqz?: string;
    ituz?: string;
    cont?: string;
}

export type PatchOutcome = { kind: 'ok'; qso: LogbookQso } | { kind: 'error'; message: string };

/** Fetch one QSO by its canonical uuid — GET /v1/qso/{uuid}. The response is
 *  the full QSO JSON, a superset of the logbook list-row shape, so surfaces
 *  that only carry a trimmed row (the Operate session list) can hydrate the
 *  edit modal without paging the logbook. */
export async function fetchQso(uuid: string): Promise<PatchOutcome> {
    const fetched = await safeFetch(`/v1/qso/${uuid}`, {}, { timeoutMs: DEFAULT_TIMEOUT_MS });
    if (!fetched.ok) {
        return {
            kind: 'error',
            message: fetched.kind === 'network' ? 'Cannot reach the daemon.' : 'Request failed.',
        };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const message =
            body &&
            typeof body === 'object' &&
            typeof (body as { message?: unknown }).message === 'string'
                ? (body as { message: string }).message
                : `Daemon error (${fetched.response.status}).`;
        return { kind: 'error', message };
    }
    return decodeQso(body, uuid, 'Unexpected QSO response.');
}

/** Apply an edit to one QSO. `uuid` is the QSO's canonical id (carried on every
 *  row). Returns the updated row on success, or a daemon-supplied message. */
export async function patchQso(uuid: string, patch: QsoPatch): Promise<PatchOutcome> {
    const fetched = await safeFetch(
        `/v1/qso/${uuid}`,
        {
            method: 'PATCH',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(patch),
        },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        return {
            kind: 'error',
            message: fetched.kind === 'network' ? 'Cannot reach the daemon.' : 'Request failed.',
        };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        // Error envelope is {code, message}; fall back to a generic line.
        const message =
            body &&
            typeof body === 'object' &&
            typeof (body as { message?: unknown }).message === 'string'
                ? (body as { message: string }).message
                : `Daemon error (${fetched.response.status}).`;
        return { kind: 'error', message };
    }
    return decodeQso(body, uuid, 'Unexpected update response.');
}
