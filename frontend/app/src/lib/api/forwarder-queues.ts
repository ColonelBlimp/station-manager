/*
    Operator-triggered forwarder queue clearing (W-0005).

    GET  /v1/forwarder-queues            → per-forwarder {clearable, in_flight}
    POST /v1/forwarder/{name}/queue/clear → drop the pending+failed backlog

    `clearable` is the pending+failed backlog a clear removes; `in_flight` is the
    in_progress batch a live worker is processing and never clears. The clear is
    live daemon state, independent of the config draft/save/restart lifecycle.
*/

import { daemonErrorMessage, isPlainObject, readJsonBody, safeFetch } from './_helpers';

export interface ForwarderQueueCount {
    name: string;
    clearable: number;
    in_flight: number;
}

export type QueuesOutcome =
    { kind: 'ok'; forwarders: ForwarderQueueCount[] } | { kind: 'error'; message: string };

export type ClearOutcome = { kind: 'ok'; discarded: number } | { kind: 'error'; message: string };

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
 *  surrounding whitespace is a legal, distinct forwarder). */
export async function clearForwarderQueue(
    name: string,
    signal?: AbortSignal
): Promise<ClearOutcome> {
    const fetched = await safeFetch(`/v1/forwarder/${encodeURIComponent(name)}/queue/clear`, {
        method: 'POST',
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    if (!isPlainObject(body)) {
        return { kind: 'error', message: 'Unexpected clear response.' };
    }
    return { kind: 'ok', discarded: num(body.discarded) };
}
