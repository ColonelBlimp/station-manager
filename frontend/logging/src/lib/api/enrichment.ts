/*
    Thin daemon-side wrapper for `GET /v1/enrich/callsign?call=X`.

    Wire contract (api.md §4.4 + ADR 0017 #12):
      - 200 OK → Result {
            callsign:        string,
            country?:        Country (optional, omitempty)
            station?:        ContactedStation (optional, omitempty)
            country_source:  "hamnut" | "cache" | "none"
            station_source:  <provider name> | "cache" | "none"
        }
      - 400 Bad Request → { code, message } (missing or invalid `call` param)

    Per ADR 0017 #12 the daemon's "always-200" contract means provider
    failures collapse to source=none with empty payloads; the SPA does
    NOT need a special-case branch for "all providers down." 400 is the
    only non-2xx — gating client-side on isValidCallsign keeps it rare.

    Result returns a discriminated outcome so callers branch by `kind`
    rather than parsing HTTP / JSON envelopes inline. Matches the shape
    of submitQso in lib/api/qso.ts.
*/

export interface EnrichmentCountry {
    id?: number;
    name?: string;
    prefix?: string;
    ccode?: string;
    continent?: string;
    cq_zone?: string;
    itu_zone?: string;
    dxcc_prefix?: string;
    time_offset?: string;
    short_path_distance?: string;
    long_path_distance?: string;
    short_path_bearing?: string;
    long_path_bearing?: string;
    is_new_entity?: boolean;
    local_time?: string;
    last_refreshed_at?: string;
}

export interface EnrichmentStation {
    csid?: number;
    address?: string;
    age?: string;
    altitude?: string;
    call?: string;
    cont?: string;
    contacted_op?: string;
    country?: string;
    cqz?: string;
    dxcc?: string;
    email?: string;
    eq_call?: string;
    gridsquare?: string;
    iota?: string;
    ituz?: string;
    name?: string;
    qth?: string;
    web?: string;
    lat?: string;
    lon?: string;
    last_refreshed_at?: string;
    [extra: string]: unknown;
}

export interface EnrichmentResult {
    callsign: string;
    country?: EnrichmentCountry;
    station?: EnrichmentStation;
    country_source: 'hamnut' | 'cache' | 'none';
    /** Provider name (e.g. "qrzlookupservice"), "cache", or "none". */
    station_source: string;
}

export type EnrichOutcome =
    | { kind: 'ok'; result: EnrichmentResult }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function enrichCallsign(callsign: string): Promise<EnrichOutcome> {
    let response: Response;
    try {
        response = await fetch(`/v1/enrich/callsign?call=${encodeURIComponent(callsign)}`, {
            method: 'GET',
        });
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        return { kind: 'network', message };
    }

    let body: EnrichmentResult | DaemonError | null;
    try {
        body = (await response.json()) as EnrichmentResult | DaemonError;
    } catch {
        body = null;
    }

    if (response.ok) {
        const result = body as EnrichmentResult | null;
        if (result === null) {
            return {
                kind: 'server',
                code: 'unparseable_response',
                message: 'enrichment response was not valid JSON',
            };
        }
        return { kind: 'ok', result };
    }

    const err = body as DaemonError | null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
