/*
    Enrichment-scoped /v1/config read + write for the app Settings view (ADR
    0044) — the port of the standalone config SPA's Enrichment tab (ADR 0017).

    Named lookup.ts, not enrichment.ts: api/enrichment.ts is already the QSO-time
    `GET /v1/enrich/callsign` wrapper. This is the CONFIG surface for the same
    subsystem.

    Data-safety contract — VERIFIED against the daemon's overlayConfig +
    mergeLookup (internal/api/handler_config.go):

      - THE CHAIN IS REPLACED WHOLE. mergeLookup rebuilds it purely from the
        payload; there is no merge-by-name that keeps absent entries. So every
        provider must ride every save, including ones this build renders no UI
        for — omitting one DELETES it. Passwords survive because each provider's
        secret is merged by name onto the stored entry.
      - GET never echoes a provider password — only `password_set`. Blank
        therefore means "not retyped", and `password` is OMITTED rather than
        sent as "".
      - `password_clear` is the only way to remove a stored provider password
        (added 2026-08-03, same contract as smtp's — see resolveMaskedPassword).
      - TTLs are pointers on the wire: OMIT to mean "use the default",
        send an explicit 0 to mean "trust this cache indefinitely". These are
        different instructions and a blank box must not send 0.
      - url / view_url / timeout_sec are taken AS SENT for every provider.
        Normalize can re-stamp registered-provider defaults, but an unrecognised
        provider must be round-tripped intact or it is silently emptied.
      - NOTHING but `lookup` is sent.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';

/** Canonical provider names the daemon stamps (internal/types/lookup.go). */
export const HAMNUT_PROVIDER = 'hamnutlookupservice';
export const QRZ_PROVIDER = 'qrzlookupservice';

/** One provider as GET /v1/config reports it — password masked to a flag. */
export interface LookupProvider {
    name: string;
    /** Exclusive authority/order within the callsign chain; zero for hamnut. */
    priority: number;
    /** Operator's config.json display name. '' = fall back to the built-in. */
    label: string;
    enabled: boolean;
    url: string;
    username: string;
    password_set: boolean;
    timeout_sec: number;
    view_url: string;
}

export interface LookupEntry {
    hamnut: LookupProvider;
    chain: LookupProvider[];
    continue_if_blank: string[];
    country_ttl_days: number;
    station_ttl_days: number;
    refresh_max_in_flight: number;
}

/** What a save sends per provider. `password` rides only when freshly typed. */
export interface LookupProviderPayload {
    name: string;
    priority?: number;
    enabled: boolean;
    url?: string;
    username?: string;
    password?: string;
    password_clear?: boolean;
    timeout_sec?: number;
    view_url?: string;
}

export interface LookupPayload {
    hamnut: LookupProviderPayload;
    chain: LookupProviderPayload[];
    continue_if_blank: string[];
    country_ttl_days?: number;
    station_ttl_days?: number;
    refresh_max_in_flight: number;
}

/**
 * One provider's descriptor from GET /v1/lookup-types (ADR 0062) — what the
 * daemon knows about a provider, replacing the hardcoded map this section used
 * to carry. A provider compiled into the daemon appears here; adding one needs
 * no change in this SPA.
 */
export interface LookupType {
    name: string;
    display_name: string;
    help?: string;
    /** "country" (the single prefix provider) | "callsign" (the chain). */
    kind: string;
    /** False for a provider anonymous BY DESIGN — it gets no credential inputs. */
    needs_credentials: boolean;
}

/** A callsign field that can decide whether the next provider is consulted. */
export interface CompletionField {
    name: string;
    display_name: string;
}

export type LookupTypesOutcome =
    | { kind: 'ok'; types: LookupType[]; completionFields: CompletionField[] }
    | {
          kind: 'error';
          message: string;
      };

function toType(v: unknown): LookupType | null {
    if (!isPlainObject(v) || typeof v.name !== 'string' || typeof v.display_name !== 'string') {
        return null;
    }
    return {
        name: v.name,
        display_name: v.display_name,
        help: typeof v.help === 'string' ? v.help : undefined,
        kind: typeof v.kind === 'string' ? v.kind : '',
        needs_credentials: v.needs_credentials === true,
    };
}

function toCompletionField(v: unknown): CompletionField | null {
    if (!isPlainObject(v) || typeof v.name !== 'string' || typeof v.display_name !== 'string') {
        return null;
    }
    return { name: v.name, display_name: v.display_name };
}

export async function fetchLookupTypes(signal?: AbortSignal): Promise<LookupTypesOutcome> {
    const fetched = await safeFetch('/v1/lookup-types', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    const raw = isPlainObject(body) && Array.isArray(body.types) ? body.types : [];
    const types: LookupType[] = [];
    for (const t of raw) {
        const parsed = toType(t);
        if (parsed) types.push(parsed);
    }
    const rawCompletion =
        isPlainObject(body) && Array.isArray(body.completion_fields) ? body.completion_fields : [];
    const completionFields: CompletionField[] = [];
    for (const field of rawCompletion) {
        const parsed = toCompletionField(field);
        if (parsed) completionFields.push(parsed);
    }
    return { kind: 'ok', types, completionFields };
}

export type LookupOutcome =
    { kind: 'ok'; lookup: LookupEntry } | { kind: 'error'; message: string };

function toProvider(v: unknown, fallbackPriority = 0): LookupProvider {
    const o = isPlainObject(v) ? v : {};
    return {
        name: typeof o.name === 'string' ? o.name : '',
        priority:
            typeof o.priority === 'number' && Number.isInteger(o.priority)
                ? o.priority
                : fallbackPriority,
        label: typeof o.label === 'string' ? o.label : '',
        enabled: o.enabled === true,
        url: typeof o.url === 'string' ? o.url : '',
        username: typeof o.username === 'string' ? o.username : '',
        password_set: o.password_set === true,
        timeout_sec: typeof o.timeout_sec === 'number' ? o.timeout_sec : 0,
        view_url: typeof o.view_url === 'string' ? o.view_url : '',
    };
}

function toEntry(v: unknown): LookupEntry {
    const o = isPlainObject(v) ? v : {};
    const chain = Array.isArray(o.chain)
        ? o.chain
              .map((provider, index) => toProvider(provider, index + 1))
              .sort((a, b) => a.priority - b.priority)
        : [];
    return {
        hamnut: toProvider(o.hamnut),
        chain,
        // A missing field is the pre-ADR-0068 wire shape. Preserve an explicit
        // [] because it is the operator's legacy first-substantive escape hatch.
        continue_if_blank: Array.isArray(o.continue_if_blank)
            ? o.continue_if_blank.filter((field): field is string => typeof field === 'string')
            : ['name', 'gridsquare'],
        country_ttl_days: typeof o.country_ttl_days === 'number' ? o.country_ttl_days : 0,
        station_ttl_days: typeof o.station_ttl_days === 'number' ? o.station_ttl_days : 0,
        refresh_max_in_flight:
            typeof o.refresh_max_in_flight === 'number' ? o.refresh_max_in_flight : 0,
    };
}

export async function fetchLookup(signal?: AbortSignal): Promise<LookupOutcome> {
    const fetched = await safeFetch('/v1/config', { signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    if (!fetched.response.ok) return { kind: 'error', message: `HTTP ${fetched.response.status}` };
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) return { kind: 'error', message: 'malformed /v1/config response' };
    return { kind: 'ok', lookup: toEntry(body.lookup) };
}

export async function saveLookup(
    payload: LookupPayload,
    signal?: AbortSignal
): Promise<LookupOutcome> {
    const fetched = await safeFetch('/v1/config', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        // Only `lookup` — the config SPA echoes logging_station and station on
        // an enrichment save (config.svelte.ts:976), which would clobber a
        // concurrent identity or power change made between our GET and our PUT.
        body: JSON.stringify({ lookup: payload }),
        signal,
    });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        const err = isPlainObject(body) ? (body as { message?: string }) : null;
        return { kind: 'error', message: err?.message ?? `HTTP ${fetched.response.status}` };
    }
    if (!isPlainObject(body)) return { kind: 'error', message: 'malformed save response' };
    return { kind: 'ok', lookup: toEntry(body.lookup) };
}
