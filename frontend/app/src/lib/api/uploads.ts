/*
    Manual upload backfill (ADR 0039): POST /v1/forwarder/{name}/uploads queues
    the selected stored QSOs for upload to one enabled forwarder. The daemon skips
    QSOs already uploaded to that destination (unless force) and reports a summary;
    the existing per-destination worker then drains the queue in the background.
*/

import {
    daemonErrorMessage,
    isPlainObject,
    readJsonBody,
    safeFetch,
    WRITE_TIMEOUT_MS,
} from './_helpers';

// F-04a: shared operator-facing lead for a write whose outcome the SPA could not confirm because the
// request timed out after (possibly) reaching the daemon. Never label this "failed".
const OUTCOME_UNKNOWN_LEAD =
    'The request timed out before Station Manager confirmed the result, so the outcome is unknown.';

export interface EnqueueResult {
    enqueued: number;
    skipped_uploaded: number;
    skipped_deleted?: string[];
    not_found?: string[];
    /** UUIDs refused because the destination accepts retries of previously
     *  queued live uploads only (ClubLog — realtime.php forbids catch-up
     *  batches); these need an ADIF export uploaded on the destination's site. */
    skipped_no_history?: string[];
}

export type EnqueueOutcome =
    | { kind: 'ok'; result: EnqueueResult }
    // `timedOut` marks the AMBIGUOUS enqueue (F-04a): the batch may already be queued, and the SPA
    // cannot prove it, so the caller must show "outcome unknown" rather than report a failure.
    | { kind: 'error'; message: string; timedOut?: boolean };

const num = (v: unknown): number => (typeof v === 'number' ? v : 0);

/** Queue `uuids` for upload to forwarder `name`. `force` re-sends QSOs already
 *  uploaded to that destination (default: skip them). */
export async function enqueueUploads(
    name: string,
    uuids: string[],
    force = false,
    signal?: AbortSignal
): Promise<EnqueueOutcome> {
    const fetched = await safeFetch(
        `/v1/forwarder/${encodeURIComponent(name)}/uploads`,
        {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ uuids, force }),
            signal,
        },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        // A fired write timeout is ambiguous: this is a multi-uuid batch the worker drains in the
        // background, and the SPA has no per-entry proof API — a missing queue entry could mean
        // "never enqueued" OR "already drained" — so the outcome cannot be reconciled. Report it as
        // unknown and point at the upload status; do NOT infer failure (F-04a). Every other
        // transport failure keeps its existing generic message unchanged.
        if (fetched.kind === 'network' && fetched.timedOut === true) {
            return {
                kind: 'error',
                message: `${OUTCOME_UNKNOWN_LEAD} Check its upload status before trying again.`,
                timedOut: true,
            };
        }
        return { kind: 'error', message: fetched.message };
    }

    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        return {
            kind: 'error',
            message: daemonErrorMessage(fetched.response.status, body),
        };
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
            skipped_no_history: Array.isArray(body.skipped_no_history)
                ? (body.skipped_no_history as string[])
                : undefined,
        },
    };
}
