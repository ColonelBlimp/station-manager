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
        Normalize re-stamps them only for the two names it knows (hamnut, QRZ),
        so anything else must be round-tripped or it is silently emptied.
      - NOTHING but `lookup` is sent.
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';

/** Canonical provider names the daemon stamps (internal/types/lookup.go). */
export const HAMNUT_PROVIDER = 'hamnutlookupservice';
export const QRZ_PROVIDER = 'qrzlookupservice';

/** One provider as GET /v1/config reports it — password masked to a flag. */
export interface LookupProvider {
    name: string;
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
    country_ttl_days: number;
    station_ttl_days: number;
    refresh_max_in_flight: number;
}

/** What a save sends per provider. `password` rides only when freshly typed. */
export interface LookupProviderPayload {
    name: string;
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
    country_ttl_days?: number;
    station_ttl_days?: number;
    refresh_max_in_flight: number;
}

export type LookupOutcome =
    { kind: 'ok'; lookup: LookupEntry } | { kind: 'error'; message: string };

function toProvider(v: unknown): LookupProvider {
    const o = isPlainObject(v) ? v : {};
    return {
        name: typeof o.name === 'string' ? o.name : '',
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
    return {
        hamnut: toProvider(o.hamnut),
        chain: Array.isArray(o.chain) ? o.chain.map(toProvider) : [],
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
