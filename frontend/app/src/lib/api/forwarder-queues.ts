/*
    Operator-triggered forwarder queue clearing (W-0005).

    GET  /v1/forwarder-queues            → per-forwarder {clearable, in_flight}
    POST /v1/forwarder/{name}/queue/clear → drop the pending+failed backlog

    `clearable` is the pending+failed backlog a clear removes; `in_flight` is the
    in_progress batch a live worker is processing and never clears. The clear is
    live daemon state, independent of the config draft/save/restart lifecycle.
*/

import {
    daemonErrorMessage,
    isPlainObject,
    readJsonBody,
    safeFetch,
    WRITE_TIMEOUT_MS,
} from './_helpers';

export interface ForwarderQueueCount {
    name: string;
    clearable: number;
    in_flight: number;
}

export type QueuesOutcome =
    { kind: 'ok'; forwarders: ForwarderQueueCount[] } | { kind: 'error'; message: string };

export type ClearOutcome =
    | { kind: 'ok'; discarded: number }
    /** `indeterminate` marks a failure where the clear MAY have committed: the
     *  daemon deletes rows before serializing its response, so any failure AFTER
     *  dispatch — a transport failure of any kind (timeout, or a connection reset
     *  after the request left), or a 200 whose body can't be read — may have
     *  already deleted. The caller must NOT report a plain failure; it must
     *  reconcile with a fresh GET before allowing another clear. A DEFINITE
     *  failure (the daemon RESPONDED with an error status → it did not act) leaves
     *  this false. */
    | { kind: 'error'; message: string; indeterminate?: boolean };

const num = (v: unknown): number => (typeof v === 'number' ? v : 0);

function toCount(v: unknown): ForwarderQueueCount | null {
    if (!isPlainObject(v) || typeof v.name !== 'string') return null;
    return { name: v.name, clearable: num(v.clearable), in_flight: num(v.in_flight) };
}

/** Read every configured forwarder's clearable/in-flight queue counts. */
export async function fetchForwarderQueues(signal?: AbortSignal): Promise<QueuesOutcome> {
    const fetched = await safeFetch('/v1/forwarder-queues', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body) || !Array.isArray(body.forwarders)) {
        return { kind: 'error', message: 'malformed /v1/forwarder-queues response' };
    }
    return {
        kind: 'ok',
        forwarders: body.forwarders
            .map(toCount)
            .filter((f): f is ForwarderQueueCount => f !== null),
    };
}

/** Discard forwarder `name`'s pending+failed backlog; returns the count removed.
 *  The name is URL-encoded so the daemon can round-trip it verbatim (a name with
 *  surrounding whitespace is a legal, distinct forwarder).
 *
 *  Deliberately takes NO AbortSignal: a destructive write must not be cancelled
 *  mid-flight (an abort wouldn't undo a committed delete), and dropping the signal
 *  removes safeFetch's `aborted` outcome — so every transport failure here is a
 *  genuine timeout/reset that may have committed, which is why they are all
 *  treated as indeterminate below. */
export async function clearForwarderQueue(name: string): Promise<ClearOutcome> {
    const fetched = await safeFetch(
        `/v1/forwarder/${encodeURIComponent(name)}/queue/clear`,
        { method: 'POST' },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        // No caller abort is possible (no signal), so a failure here is always a
        // network timeout/reset that reached (or may have reached) the daemon —
        // the delete may have committed. Indeterminate; the caller must reconcile.
        return { kind: 'error', message: fetched.message, indeterminate: true };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        // The daemon RESPONDED with an error status. For this endpoint that means
        // it did not delete (validation rejects before the delete; a failed delete
        // rolls back), so the shown count is still accurate — a DEFINITE failure.
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    if (!isPlainObject(body)) {
        // 200 but unreadable — the daemon writes 200 only after the delete commits,
        // so this succeeded; the caller must reconcile to learn the new count.
        return { kind: 'error', message: 'Unexpected clear response.', indeterminate: true };
    }
    return { kind: 'ok', discarded: num(body.discarded) };
}
