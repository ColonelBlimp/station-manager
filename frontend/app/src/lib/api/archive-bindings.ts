/*
    Destination bindings of the ACTIVE archive (ADR 0082, W-0021 5D/5E):
    GET/PUT /v1/qso-archives/{uuid}/bindings. One card per registered
    destination type, an aggregate switch state over the archive's logbooks,
    and one row per logbook. Credentials never ride this wire: rows and accounts
    list the keys that hold a value.

    The decoder keeps what it can read and drops what it cannot (ADR 0077): a
    row or destination without its identifying field is dropped, an absent
    count reads 0, and an unknown aggregate state reads "off" — never a claim
    the daemon did not make.
*/
import {
    safeFetch,
    readJsonBody,
    isPlainObject,
    daemonErrorMessage,
    WRITE_TIMEOUT_MS,
} from './_helpers';

export type BindingState = 'on' | 'off' | 'mixed';

export interface BindingQueue {
    waiting: number;
    failed: number;
    in_flight: number;
}

export interface LogbookBinding {
    logbook_id: number;
    logbook_uuid: string;
    logbook_name: string;
    logbook_callsign: string;
    /** false when the logbook has no row yet (absent counts as off). */
    bound: boolean;
    enabled: boolean;
    /** The opaque handle the queue endpoints take; '' when unbound. */
    forwarder_name: string;
    /** Logbook-scoped keys that hold a value — never the values. */
    credentials_set: string[];
    queue: BindingQueue;
}

export interface StationAccount {
    configured: boolean;
    label: string;
    fields_set: string[];
    /** 'present' | 'absent' for a type whose application key is built into the
     *  daemon (ClubLog); '' for a type that needs none. */
    build_key: '' | 'present' | 'absent';
}

export interface DestinationBinding {
    type: string;
    display_name: string;
    account: StationAccount;
    state: BindingState;
    /** Why this destination cannot be turned on here; '' when it can. */
    reason: string;
    logbooks: LogbookBinding[];
}

export interface ArchiveBindings {
    archive_id: string;
    archive_label: string;
    /** Saved rows differ from the ones the running daemon started with. */
    restart_required: boolean;
    destinations: DestinationBinding[];
}

export interface LogbookBindingEdit {
    logbook_id: number;
    enabled: boolean;
    credentials?: Record<string, string>;
    credentials_clear?: string[];
}

export interface BindingsRequest {
    destinations: { type: string; logbooks: LogbookBindingEdit[] }[];
}

export type BindingsOutcome =
    | { kind: 'ok'; bindings: ArchiveBindings }
    /** `code` is the daemon's refusal code when it answered; `timedOut` marks a
     *  write whose response never came, so it may have committed (ADR 0078). */
    | { kind: 'error'; message: string; code?: string; timedOut?: boolean };

const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const strs = (v: unknown): string[] =>
    Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [];

function toRow(v: unknown): LogbookBinding | null {
    if (!isPlainObject(v) || typeof v.logbook_id !== 'number') return null;
    const q = isPlainObject(v.queue) ? v.queue : {};
    return {
        logbook_id: v.logbook_id,
        logbook_uuid: str(v.logbook_uuid),
        logbook_name: str(v.logbook_name),
        logbook_callsign: str(v.logbook_callsign),
        bound: v.bound === true,
        enabled: v.enabled === true,
        forwarder_name: str(v.forwarder_name),
        credentials_set: strs(v.credentials_set),
        queue: { waiting: num(q.waiting), failed: num(q.failed), in_flight: num(q.in_flight) },
    };
}

function toDestination(v: unknown): DestinationBinding | null {
    if (!isPlainObject(v) || typeof v.type !== 'string' || v.type === '') return null;
    const a = isPlainObject(v.account) ? v.account : {};
    const state: BindingState = v.state === 'on' || v.state === 'mixed' ? v.state : 'off';
    const buildKey = a.build_key === 'present' || a.build_key === 'absent' ? a.build_key : '';
    return {
        type: v.type,
        display_name: str(v.display_name) || v.type,
        account: {
            configured: a.configured === true,
            label: str(a.label),
            fields_set: strs(a.fields_set),
            build_key: buildKey,
        },
        state,
        reason: str(v.reason),
        logbooks: Array.isArray(v.logbooks)
            ? v.logbooks.map(toRow).filter((r): r is LogbookBinding => r !== null)
            : [],
    };
}

function toView(body: unknown): ArchiveBindings | null {
    if (!isPlainObject(body) || !Array.isArray(body.destinations)) return null;
    return {
        archive_id: str(body.archive_id),
        archive_label: str(body.archive_label),
        restart_required: body.restart_required === true,
        destinations: body.destinations
            .map(toDestination)
            .filter((d): d is DestinationBinding => d !== null),
    };
}

async function answer(fetched: Awaited<ReturnType<typeof safeFetch>>): Promise<BindingsOutcome> {
    if (!fetched.ok) {
        return {
            kind: 'error',
            message: fetched.message,
            timedOut: fetched.kind === 'network' && fetched.timedOut === true,
        };
    }
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const code = isPlainObject(body) && typeof body.code === 'string' ? body.code : undefined;
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body), code };
    }
    const view = toView(body);
    if (view === null) return { kind: 'error', message: 'malformed bindings response' };
    return { kind: 'ok', bindings: view };
}

const path = (archiveId: string): string =>
    `/v1/qso-archives/${encodeURIComponent(archiveId)}/bindings`;

/** Read the active archive's bindings. */
export async function fetchArchiveBindings(
    archiveId: string,
    signal?: AbortSignal
): Promise<BindingsOutcome> {
    return answer(await safeFetch(path(archiveId), { signal }));
}

/** Save the changed rows; the daemon validates the whole request and writes it
 *  in one transaction, answering with the fresh view. No AbortSignal: a write
 *  must not be cancelled mid-flight. */
export async function saveArchiveBindings(
    archiveId: string,
    req: BindingsRequest
): Promise<BindingsOutcome> {
    return answer(
        await safeFetch(
            path(archiveId),
            {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(req),
            },
            { timeoutMs: WRITE_TIMEOUT_MS }
        )
    );
}
