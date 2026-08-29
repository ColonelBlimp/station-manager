/*
    Forwarder-scoped /v1/config read + write, plus the data-driven type
    descriptors from /v1/forwarder-types (app Settings view, ADR 0044).

    Data-safety contract — VERIFIED against the daemon's overlayConfig +
    mergeForwarders (internal/api/handler_config.go:1241):

      - The forwarders block is replaced WHOLE when present, so the full list
        must ride back on every save. Credentials survive that because
        mergeForwarders merges by NAME onto the stored entry.
      - GET echoes no credential VALUES ever — only `credentials_set`, the list
        of keys currently holding one.
      - An OMITTED credential key keeps its stored value. A key sent BLANK also
        keeps it, unless the field is declared Clearable, in which case blank
        means "reset to the constructor's default".
      - `logging_station` and `station` are NOT sent. The config SPA echoes both
        on a forwarder save (config.svelte.ts:903), which would clobber a
        concurrent edit made in another tab between our GET and PUT — the same
        trap review 2026-07-20 #3 removed from the Station section. Omitting
        them leaves the daemon's current blocks untouched.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';
import { isDurabilityUnconfirmed } from '../config/durability';

/** One credential input, as declared by the forwarder type in Go. */
export interface CredentialField {
    key: string;
    label: string;
    /** "text" | "password" — picks the widget, NEVER the write policy. */
    kind: string;
    help?: string;
    /**
     * Blank means "reset to the default" for this field only. Deliberately not
     * inferred from kind==="text": most text credentials are REQUIRED, and
     * emptying one is a daemon that won't restart, not a reset
     * (internal/forwarding/registry.go:415).
     */
    clearable?: boolean;
}

export interface ForwarderType {
    type: string;
    display_name: string;
    supported_actions: string[];
    credential_fields: CredentialField[];
}

/** A configured destination as GET /v1/config reports it — values masked. */
export interface ForwarderEntry {
    name: string;
    type: string;
    /** Operator's own display name, set only in config.json. '' = use the built-in. */
    label: string;
    enabled: boolean;
    action_filter?: string[];
    /** Which credential keys currently hold a value. Never the values. */
    credentials_set?: string[];
}

/** What a save sends per destination. `credentials` carries only real edits. */
export interface ForwarderPayload {
    name: string;
    type: string;
    enabled: boolean;
    action_filter?: string[];
    credentials?: Record<string, string>;
}

export type ForwardersOutcome =
    | { kind: 'ok'; forwarders: ForwarderEntry[]; durabilityUnconfirmed?: boolean }
    | { kind: 'error'; message: string };

export type TypesOutcome =
    { kind: 'ok'; types: ForwarderType[] } | { kind: 'error'; message: string };

function toEntry(v: unknown): ForwarderEntry | null {
    if (!isPlainObject(v) || typeof v.name !== 'string' || typeof v.type !== 'string') return null;
    return {
        name: v.name,
        type: v.type,
        label: typeof v.label === 'string' ? v.label : '',
        enabled: v.enabled === true,
        action_filter: Array.isArray(v.action_filter)
            ? v.action_filter.filter((a): a is string => typeof a === 'string')
            : undefined,
        credentials_set: Array.isArray(v.credentials_set)
            ? v.credentials_set.filter((k): k is string => typeof k === 'string')
            : [],
    };
}

function parseForwarders(body: unknown): ForwarderEntry[] {
    if (!isPlainObject(body) || !Array.isArray(body.forwarders)) return [];
    return body.forwarders.map(toEntry).filter((f): f is ForwarderEntry => f !== null);
}

export async function fetchForwarders(signal?: AbortSignal): Promise<ForwardersOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    return { kind: 'ok', forwarders: parseForwarders(await readJsonBody(fetched.response)) };
}

export async function fetchForwarderTypes(signal?: AbortSignal): Promise<TypesOutcome> {
    const fetched = await safeFetch('/v1/forwarder-types', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body) || !Array.isArray(body.types)) {
        return { kind: 'error', message: 'malformed /v1/forwarder-types response' };
    }
    // A non-string where a string is declared is REJECTED, not coerced: String()
    // on an object yields "[object Object]", which would render as a credential
    // label and — worse, for `key` — become the JSON key a credential is stored
    // under. Dropping the malformed field is the recoverable choice.
    const str = (v: unknown, fallback = ''): string => (typeof v === 'string' ? v : fallback);
    const types = body.types.filter(isPlainObject).map((t) => ({
        type: str(t.type),
        display_name: str(t.display_name),
        supported_actions: Array.isArray(t.supported_actions)
            ? t.supported_actions.filter((a): a is string => typeof a === 'string')
            : [],
        credential_fields: Array.isArray(t.credential_fields)
            ? t.credential_fields
                  .filter(isPlainObject)
                  .map((f) => ({
                      key: str(f.key),
                      label: str(f.label),
                      kind: str(f.kind, 'text'),
                      help: typeof f.help === 'string' ? f.help : undefined,
                      clearable: f.clearable === true,
                  }))
                  // A field with no key cannot be bound to a credential at all.
                  .filter((f) => f.key !== '')
            : [],
    }));
    return { kind: 'ok', types };
}

export async function saveForwarders(
    forwarders: ForwarderPayload[],
    signal?: AbortSignal
): Promise<ForwardersOutcome> {
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ forwarders }),
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(body) ? (body as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    return {
        kind: 'ok',
        forwarders: parseForwarders(body),
        durabilityUnconfirmed: isDurabilityUnconfirmed(body),
    };
}
