/*
    QSO archives (ADR 0071, W-0021 slice 4) — the station-global catalogue:
      - GET  /v1/qso-archives                  → {archives: [{id, label, ownership, state, …}]}
      - POST /v1/qso-archives                  → provision a managed archive (201; 200 when
                                                  request_key found the one an earlier request made)
      - POST /v1/qso-archives/{id}/activate    → 202 {id, durability}: the NEXT start serves it;
                                                  the daemon restarts, TX admission sealed meanwhile

    Honest state (AC 4): the daemon's `state` is the only truth the SPA shows —
    a 202 makes an archive `pending`, never active; the restart proves it.
    Every refusal keeps the daemon's code + message: they are the operator's
    diagnostic (the busy reason, the file's path), not a generic failure.
*/

import {
    daemonErrorMessage,
    isPlainObject,
    readJsonBody,
    safeFetch,
    WRITE_TIMEOUT_MS,
} from './_helpers';

export type ArchiveOwnership = 'managed' | 'legacy' | 'external';
export type ArchiveState = 'active' | 'pending' | 'inactive';

export interface QsoArchive {
    id: string;
    label: string;
    ownership: ArchiveOwnership;
    state: ArchiveState;
    /** The most recent failed activation's diagnostic; '' when none. */
    lastActivationError: string;
    /** The file's size at listing time; null when the daemon could not stat it. */
    sizeBytes: number | null;
    /** The file's last write (RFC 3339, UTC); null when unknown. Not a "last opened". */
    modifiedAt: string | null;
}

export interface CreateArchiveInput {
    /** Idempotency key: a retry with the same key returns the archive the first made. */
    requestKey: string;
    label: string;
    logbookName: string;
    logbookCallsign: string;
}

export type ArchivesOutcome =
    { kind: 'ok'; archives: QsoArchive[] } | { kind: 'error'; message: string };

/** `refused`: the daemon answered with a code — its message is shown as is.
 *  `network`: a TRANSPORT failure — the write is UNCONFIRMED whatever the
 *  cause (a timeout, a reset after the request left, an unreachable daemon):
 *  it may have reached the daemon, so the caller must reconcile, never call it
 *  failed; `timedOut` says which. `error`: the daemon ANSWERED without a code
 *  or with a body the SPA could not read — a definite non-acceptance. */
export type CreateArchiveOutcome =
    | { kind: 'ok'; archive: QsoArchive; reused: boolean }
    | { kind: 'refused'; code: string; message: string }
    | { kind: 'network'; message: string; timedOut?: boolean }
    | { kind: 'error'; message: string };

export type ActivateArchiveOutcome =
    | { kind: 'accepted'; id: string; durability: 'durable' | 'uncertain' }
    | { kind: 'refused'; code: string; message: string }
    | { kind: 'network'; message: string; timedOut?: boolean }
    | { kind: 'error'; message: string };

/** The daemon's identity as /v1/version reports it: the per-process instance
 *  and the archive it serves (ADR 0071). null when it cannot be read. */
export interface DaemonIdentity {
    instance: string;
    archiveId: string;
}

export async function fetchDaemonIdentity(signal?: AbortSignal): Promise<DaemonIdentity | null> {
    const fetched = await safeFetch('/v1/version', { signal });
    if (!fetched.ok || !fetched.response.ok) return null;
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body) || typeof body.instance !== 'string') return null;
    const archive = isPlainObject(body.archive) ? body.archive : null;
    return {
        instance: body.instance,
        archiveId: archive && typeof archive.id === 'string' ? archive.id : '',
    };
}

const OWNERSHIPS: ArchiveOwnership[] = ['managed', 'legacy', 'external'];
const STATES: ArchiveState[] = ['active', 'pending', 'inactive'];

export function toArchive(v: unknown): QsoArchive | null {
    if (!isPlainObject(v) || typeof v.id !== 'string' || typeof v.label !== 'string') return null;
    const ownership = OWNERSHIPS.find((o) => o === v.ownership);
    const state = STATES.find((s) => s === v.state);
    if (!ownership || !state) return null;
    return {
        id: v.id,
        label: v.label,
        ownership,
        state,
        lastActivationError:
            typeof v.last_activation_error === 'string' ? v.last_activation_error : '',
        sizeBytes: typeof v.size_bytes === 'number' ? v.size_bytes : null,
        modifiedAt: typeof v.modified_at === 'string' ? v.modified_at : null,
    };
}

function codedRefusal(status: number, body: unknown): { code: string; message: string } | null {
    if (!isPlainObject(body) || typeof body.code !== 'string') return null;
    return {
        code: body.code,
        message: typeof body.message === 'string' ? body.message : daemonErrorMessage(status, body),
    };
}

export async function fetchQsoArchives(signal?: AbortSignal): Promise<ArchivesOutcome> {
    const fetched = await safeFetch('/v1/qso-archives', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const res = fetched.response;
    const body = await readJsonBody(res);
    if (!res.ok) return { kind: 'error', message: daemonErrorMessage(res.status, body) };
    if (!isPlainObject(body) || !Array.isArray(body.archives)) {
        return { kind: 'error', message: 'malformed /v1/qso-archives response' };
    }
    const archives: QsoArchive[] = [];
    for (const raw of body.archives) {
        const a = toArchive(raw);
        if (a) archives.push(a);
    }
    return { kind: 'ok', archives };
}

export async function createQsoArchive(
    input: CreateArchiveInput,
    signal?: AbortSignal
): Promise<CreateArchiveOutcome> {
    const fetched = await safeFetch(
        '/v1/qso-archives',
        {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                request_key: input.requestKey,
                label: input.label,
                logbook_name: input.logbookName,
                logbook_callsign: input.logbookCallsign,
            }),
            signal,
        },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        const timedOut = fetched.kind === 'network' ? fetched.timedOut : undefined;
        return { kind: 'network', message: fetched.message, timedOut };
    }
    const res = fetched.response;
    const body = await readJsonBody(res);
    if (res.status === 201 || res.status === 200) {
        const archive = isPlainObject(body) ? toArchive(body.archive) : null;
        if (!archive) return { kind: 'error', message: 'malformed create response' };
        return { kind: 'ok', archive, reused: isPlainObject(body) && body.reused === true };
    }
    const refusal = codedRefusal(res.status, body);
    if (refusal) return { kind: 'refused', ...refusal };
    return { kind: 'error', message: daemonErrorMessage(res.status, body) };
}

export async function activateQsoArchive(
    id: string,
    signal?: AbortSignal
): Promise<ActivateArchiveOutcome> {
    const fetched = await safeFetch(
        `/v1/qso-archives/${encodeURIComponent(id)}/activate`,
        { method: 'POST', signal },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        const timedOut = fetched.kind === 'network' ? fetched.timedOut : undefined;
        return { kind: 'network', message: fetched.message, timedOut };
    }
    const res = fetched.response;
    const body = await readJsonBody(res);
    if (res.status === 202) {
        const durability =
            isPlainObject(body) && body.durability === 'uncertain' ? 'uncertain' : 'durable';
        return { kind: 'accepted', id, durability };
    }
    const refusal = codedRefusal(res.status, body);
    if (refusal) return { kind: 'refused', ...refusal };
    return { kind: 'error', message: daemonErrorMessage(res.status, body) };
}
