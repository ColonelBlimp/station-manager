// Adapters from the daemon API clients to the operate-surface seams — the
// real counterparts of lib/dev/*Stub.ts, injected in main.ts (ADR 0045: the
// state modules never import this layer).
//
// Mapping posture is fail-soft throughout: any non-ok outcome collapses to
// null / '' and the UI shows its empty state — enrichment never blocks
// logging, and a daemon hiccup is never an operator-facing error here.

import { enrichCallsign } from './enrichment';
import { isPlainObject, readJsonBody, safeFetch } from './_helpers';
import type { Enrichment } from '../operate/enrich.svelte';

/** Real enricher for setEnricher — GET /v1/enrich/callsign. The signal lets a
 *  superseded lookup cancel its in-flight daemon/upstream request. */
export async function apiEnrich(call: string, signal?: AbortSignal): Promise<Enrichment | null> {
    const out = await enrichCallsign(call, signal);
    if (out.kind !== 'ok') return null;

    const { country, station } = out.result;
    if (country === undefined && station === undefined) return null; // both sources "none"

    return {
        country: country?.name ?? '',
        ccode: country?.ccode ?? '',
        // The numeric entity (station.dxcc) is usually absent on cache hits;
        // the entity prefix is the reliable identity on the wire.
        dxcc: station?.dxcc ?? country?.dxcc_prefix ?? '',
        isNewEntity: country?.is_new_entity ?? null,
        grid: station?.gridsquare ?? '',
        name: station?.name ?? '',
        qth: station?.qth ?? '',
    };
}

/**
 * The station facts a submit needs, from GET /v1/config: my grid (also the
 * near end of the enrichment bearing), the station/operator callsigns for the
 * ADIF record, and the default logbook the POST targets. Zero-values on any
 * failure — the submit sink refuses with a clear message rather than posting
 * a QSO against the wrong logbook.
 */
export interface StationContext {
    myGrid: string;
    stationCallsign: string;
    operator: string;
    logbookId: number;
}

export async function fetchStationContext(): Promise<StationContext> {
    const none: StationContext = { myGrid: '', stationCallsign: '', operator: '', logbookId: 0 };
    const fetched = await safeFetch('/v1/config', { method: 'GET' });
    if (!fetched.ok || !fetched.response.ok) return none;
    const body = await readJsonBody(fetched.response);
    if (!isPlainObject(body)) return none;

    const ls = isPlainObject(body.logging_station) ? body.logging_station : {};
    const lb = isPlainObject(body.default_logbook) ? body.default_logbook : {};
    const str = (v: unknown): string => (typeof v === 'string' ? v : '');
    return {
        myGrid: str(ls.my_gridsquare),
        stationCallsign: str(ls.station_callsign),
        operator: str(ls.operator),
        logbookId: typeof lb.id === 'number' ? lb.id : 0,
    };
}
