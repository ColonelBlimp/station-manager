/**
 * Contacts-map data story (backlog "QSO contacts map", Phase 2): an
 * operator-picked time window over logged QSOs, live-refreshed from the
 * daemon's event stream. The map is a READ-ONLY view — no session entity,
 * stored or derived (ADR 0049 rejection): "this sitting" is just "QSOs in
 * the last N minutes".
 *
 * Data path: page GET /v1/logbook/{id}/qso newest-first and stop at the
 * window edge; subscribe /v1/events FIRST (stream, then fetch — the
 * documented reconnect contract) and re-run the windowed fetch on any
 * qso.* event for our logbook (the payload is minimal by design; a
 * head-refetch is cheap and idempotent). Coordinates prefer the
 * enrichment's decimal lat/lon and fall back to the gridsquare's cell
 * centre; rows with neither stay unplotted and are surfaced as
 * "N of M plotted" — fail-soft, never blocking the view.
 */

import { fetchQsoPage, type LogbookQso, type QsoPageOutcome } from '../api/logbooks';
import { openLogEvents, type QsoEventPayload } from '../api/log-events';
import { fetchStationContext } from '../api/seams';
import { gridToDecimal, haversineKm, calculateBearing } from '../utils/bearing';
import { normalizeBand } from './bandColors';
import type { LatLon } from './engine';

export interface DurationOption {
    label: string;
    minutes: number;
}

/** The picker's fixed choices; "custom" rides the same minutes field. */
export const DURATIONS: DurationOption[] = [
    { label: '15 min', minutes: 15 },
    { label: '30 min', minutes: 30 },
    { label: '1 h', minutes: 60 },
    { label: '2 h', minutes: 120 },
    { label: '3 h', minutes: 180 },
    { label: '6 h', minutes: 360 },
    { label: '12 h', minutes: 720 },
    { label: '24 h', minutes: 1440 },
    { label: '48 h', minutes: 2880 },
];

/** One plottable contact — everything WorldMap and the tooltip need. */
export interface MapQso {
    key: string;
    call: string;
    /** Normalised band token ("20m"), '' when the row carries none. */
    band: string;
    point: LatLon;
    label: string;
}

// Page size / page cap for one windowed collection. The cap bounds a
// pathological window (e.g. 48 h over a contest log) at 5 000 rows — when
// hit, the view says so rather than silently truncating.
const PAGE_LIMIT = 200;
const MAX_PAGES = 25;

/** ADIF date (YYYYMMDD) + time (HHMM[SS], UTC) → epoch ms, null if unparseable. */
export function qsoEpochMs(qsoDate?: string, timeOn?: string): number | null {
    if (qsoDate === undefined || !/^\d{8}$/.test(qsoDate)) return null;
    const t = timeOn !== undefined && /^\d{4}(\d{2})?$/.test(timeOn) ? timeOn : '0000';
    return Date.UTC(
        Number(qsoDate.slice(0, 4)),
        Number(qsoDate.slice(4, 6)) - 1,
        Number(qsoDate.slice(6, 8)),
        Number(t.slice(0, 2)),
        Number(t.slice(2, 4)),
        t.length === 6 ? Number(t.slice(4, 6)) : 0
    );
}

/** Far-end coordinates: enrichment decimal lat/lon first, else the
 *  gridsquare's cell centre, else null (unplottable). parseFloat rejects
 *  import-era ADIF Location strings ("N051 30.000") → grid fallback. */
export function rowPoint(q: LogbookQso): LatLon | null {
    const lat = Number.parseFloat(q.lat ?? '');
    const lon = Number.parseFloat(q.lon ?? '');
    if (Number.isFinite(lat) && Number.isFinite(lon)) return { lat, lon };
    return gridToDecimal(q.gridsquare ?? '');
}

/** A page fetcher, injectable so collectWindow tests need no fetch mock. */
export type Pager = (after?: string) => Promise<QsoPageOutcome>;

export interface WindowResult {
    rows: LogbookQso[];
    /** True when MAX_PAGES fired before the window edge — coverage is partial. */
    capped: boolean;
}

/**
 * Collect all QSOs with timestamp ≥ sinceMs by paging newest-first until a
 * row falls past the edge. Rows with no parseable timestamp are skipped
 * (they cannot be windowed; the logbook view is where broken rows surface).
 */
export async function collectWindow(pager: Pager, sinceMs: number): Promise<WindowResult> {
    const rows: LogbookQso[] = [];
    let after: string | undefined;
    for (let page = 0; page < MAX_PAGES; page++) {
        const out = await pager(after);
        if (out.kind !== 'ok') throw new Error(out.message);
        for (const q of out.items) {
            const ts = qsoEpochMs(q.qso_date, q.time_on);
            if (ts === null) continue;
            if (ts < sinceMs) return { rows, capped: false };
            rows.push(q);
        }
        if (out.nextCursor === null) return { rows, capped: false };
        after = out.nextCursor;
    }
    return { rows, capped: true };
}

/** Tooltip line: call · grid · distance · short-path bearing (what the
 *  operator glances at before swinging the antenna). Distance/bearing come
 *  from the resolved points so lat/lon-only rows still get them. */
export function qsoLabel(q: LogbookQso, point: LatLon, origin: LatLon | null): string {
    const parts = [q.call ?? '?'];
    if (q.gridsquare) parts.push(q.gridsquare);
    if (origin !== null) {
        const km = Math.ceil(haversineKm(origin.lat, origin.lon, point.lat, point.lon));
        const brg = calculateBearing(origin.lat, origin.lon, point.lat, point.lon);
        parts.push(`${km.toLocaleString()} km`, `${brg.toFixed(1)}°`);
    }
    if (q.band) parts.push(q.band);
    if (q.mode) parts.push(q.mode);
    return parts.join(' · ');
}

export type MapStatus = 'loading' | 'ok' | 'error';

interface MapDataState {
    status: MapStatus;
    message: string;
    durationMin: number;
    /** Plottable contacts, newest first. */
    qsos: MapQso[];
    /** Total rows in the window (plotted + grid-less). */
    total: number;
    capped: boolean;
    origin: LatLon | null;
    /** Event stream up — arcs appear live. */
    live: boolean;
    /** Operator band-colour overrides (config `map.band_colors`), layered
     *  over the default palette by the view's bandColor calls. */
    bandColors: Record<string, string>;
}

export const mapData = $state<MapDataState>({
    status: 'loading',
    message: '',
    durationMin: 360,
    qsos: [],
    total: 0,
    capped: false,
    origin: null,
    live: false,
    bandColors: {},
});

let logbookId = 0;
let closeEvents: (() => void) | null = null;
let refreshTimer: ReturnType<typeof setTimeout> | null = null;
let generation = 0;

async function refresh(): Promise<void> {
    const gen = ++generation;
    const sinceMs = Date.now() - mapData.durationMin * 60_000;
    try {
        const pager: Pager = (after) => fetchQsoPage(logbookId, PAGE_LIMIT, after);
        const { rows, capped } = await collectWindow(pager, sinceMs);
        if (gen !== generation) return; // a newer refresh superseded this one
        mapData.qsos = rows.flatMap((q) => {
            const point = rowPoint(q);
            if (point === null) return [];
            return [
                {
                    key: q.uuid ?? String(q.id),
                    call: q.call ?? '?',
                    band: normalizeBand(q.band),
                    point,
                    label: qsoLabel(q, point, mapData.origin),
                },
            ];
        });
        mapData.total = rows.length;
        mapData.capped = capped;
        mapData.status = 'ok';
        mapData.message = '';
    } catch (e) {
        if (gen !== generation) return;
        mapData.status = 'error';
        mapData.message = e instanceof Error ? e.message : 'Load failed.';
    }
}

/** Collapse event bursts (a contest run logs fast) into one refetch. */
function scheduleRefresh(): void {
    if (refreshTimer !== null) return;
    refreshTimer = setTimeout(() => {
        refreshTimer = null;
        void refresh();
    }, 300);
}

/** The picker's entry point — also the manual-refresh path (same window). */
export function setDuration(minutes: number): void {
    mapData.durationMin = minutes;
    mapData.status = 'loading';
    void refresh();
}

/**
 * Bring the view up: resolve station context (logbook + origin grid), open
 * the event stream, then run the first windowed fetch. Returns the teardown.
 */
export function startMapData(): () => void {
    mapData.status = 'loading';
    void (async () => {
        const ctx = await fetchStationContext();
        logbookId = ctx.logbookId;
        mapData.origin = gridToDecimal(ctx.myGrid);
        mapData.bandColors = ctx.mapBandColors;
        if (logbookId === 0) {
            mapData.status = 'error';
            mapData.message = 'Station config unavailable — cannot resolve the logbook.';
            return;
        }
        // Stream first, then fetch — events for rows the fetch already
        // returns are idempotent (the refetch is the idempotency).
        closeEvents = openLogEvents({
            onOpen: () => {
                mapData.live = true;
            },
            onTransportError: () => {
                mapData.live = false;
            },
            onQsoChanged: (_event, p: QsoEventPayload) => {
                if (p.logbook_id === logbookId) scheduleRefresh();
            },
        });
        await refresh();
    })();
    return () => {
        generation++; // invalidate any in-flight refresh
        if (refreshTimer !== null) {
            clearTimeout(refreshTimer);
            refreshTimer = null;
        }
        if (closeEvents !== null) {
            closeEvents();
            closeEvents = null;
        }
        mapData.live = false;
    };
}
