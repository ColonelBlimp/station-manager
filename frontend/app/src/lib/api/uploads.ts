/*
    Manual upload backfill (ADR 0039): POST /v1/forwarder/{name}/uploads queues
    the selected stored QSOs for upload to one enabled forwarder. The daemon skips
    QSOs already uploaded to that destination (unless force) and reports a summary;
    the existing per-destination worker then drains the queue in the background.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface EnqueueResult {
    enqueued: number;
    skipped_uploaded: number;
    skipped_deleted?: string[];
    not_found?: string[];
}

export type EnqueueOutcome =
    { kind: 'ok'; result: EnqueueResult } | { kind: 'error'; message: string };

const num = (v: unknown): number => (typeof v === 'number' ? v : 0);

/** Queue `uuids` for upload to forwarder `name`. `force` re-sends QSOs already
 *  uploaded to that destination (default: skip them). */
export async function enqueueUploads(
    name: string,
    uuids: string[],
    force = false,
    signal?: AbortSignal
): Promise<EnqueueOutcome> {
    const fetched = await safeFetch(`/v1/forwarder/${encodeURIComponent(name)}/uploads`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ uuids, force }),
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };

    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        // Surface the daemon's error message/code when present.
        const msg =
            isPlainObject(body) && typeof body.message === 'string'
                ? body.message
                : isPlainObject(body) && typeof body.code === 'string'
                  ? body.code
                  : `Daemon error (${fetched.response.status}).`;
        return { kind: 'error', message: msg };
    }
    if (!isPlainObject(body)) {
        return { kind: 'error', message: 'Unexpected upload response.' };
    }
    return {
        kind: 'ok',
        result: {
            enqueued: num(body.enqueued),
            skipped_uploaded: num(body.skipped_uploaded),
            skipped_deleted: Array.isArray(body.skipped_deleted)
                ? (body.skipped_deleted as string[])
                : undefined,
            not_found: Array.isArray(body.not_found) ? (body.not_found as string[]) : undefined,
        },
    };
}
