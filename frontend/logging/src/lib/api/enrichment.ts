/*
    Thin daemon-side wrapper for `GET /v1/enrich/callsign?call=X`.

    Wire contract (api.md §4.4 + ADR 0017 #12):
      - 200 OK → Result {
            callsign:        string,
            country?:        Country (optional, omitempty)
            station?:        ContactedStation (optional, omitempty)
            country_source:  "hamnut" | "country_table" | "none"
            station_source:  <provider name> | "contacted_station" | "none"
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

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

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

// Daemon parity: internal/types/contacted_station.go → ContactedStation.
// Every JSON-tagged field on the Go struct is mirrored here so a typo
// like `station.gridsqure` is a compile-time error rather than a silent
// `unknown`. The prior `[extra: string]: unknown` index signature was
// removed because it defeated typo detection: any property access
// resolved to `unknown` instead of "this field doesn't exist."
// Forward-compat for future daemon fields is already covered by
// structural typing — extra JSON keys are silently ignored on reads.
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
    iota_island_id?: string;
    ituz?: string;
    name?: string;
    qth?: string;
    sig?: string;
    sig_info?: string;
    web?: string;
    wwff_ref?: string;
    lat?: string;
    lon?: string;
    last_refreshed_at?: string;
}

export interface EnrichmentResult {
    callsign: string;
    country?: EnrichmentCountry;
    station?: EnrichmentStation;
    // Daemon emits "country_table" for a cache hit (NOT "cache"), "hamnut" for
    // a cold-miss upstream call, or "none". See review 2026-06-04 L2.
    country_source: 'hamnut' | 'country_table' | 'none';
    /** Provider service name (e.g. "qrzlookupservice"), "contacted_station" (cache hit), or "none". */
    station_source: string;
}

export type EnrichOutcome =
    | { kind: 'ok'; result: EnrichmentResult }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

export async function enrichCallsign(
    callsign: string,
    signal?: AbortSignal
): Promise<EnrichOutcome> {
    const fetched = await safeFetch(`/v1/enrich/callsign?call=${encodeURIComponent(callsign)}`, {
        method: 'GET',
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const response = fetched.response;
    const body = await readJsonBody(response);

    if (response.ok) {
        if (!isPlainObject(body)) {
            return {
                kind: 'server',
                code: 'unparseable_response',
                message: 'enrichment response was not valid JSON',
            };
        }
        return { kind: 'ok', result: body as unknown as EnrichmentResult };
    }

    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    if (response.status >= 500) {
        return { kind: 'server', code, message };
    }
    return { kind: 'validation', code, message };
}
