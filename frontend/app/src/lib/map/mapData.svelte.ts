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
import { storageGet, storageSet } from '../utils/storage';
import {
    gridToCell,
    gridToDecimal,
    haversineKm,
    calculateBearing,
    type GridCell,
} from '../utils/bearing';
import { normalizeBand } from './bandColors';
import type { LatLon } from './engine';

export interface DurationOption {
    label: string;
    minutes: number;
}

/** The picker choice survives tab close/reopen, like the greyline toggle. */
const WINDOW_KEY = 'sm-map-window';

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

/** Persisted window minutes → a valid picker choice; the 6 h default when
 *  absent, unparsable, or no longer one of the offered durations. */
export function storedDurationMin(raw: string | null): number {
    const v = Number(raw);
    return DURATIONS.some((d) => d.minutes === v) ? v : 360;
}

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

/** Coordinates that disagree with their own grid, warned about once each —
 *  rowPoint runs per row on every windowed refresh, so an unguarded warning
 *  would repeat for the life of the tab. */
// A plain Set, not SvelteSet: nothing renders from this and nothing should. It
// exists only to keep a console warning from repeating, so making it reactive
// would invite re-renders driven by a logging detail.
// eslint-disable-next-line svelte/prefer-svelte-reactivity
const gridConflictSeen = new Set<string>();

/** Whether a coordinate pair falls inside the cell its locator declares.
 *  Inclusive at the edges: the operator chose no margin, so the boundary
 *  belongs to the cell rather than to a band of invented tolerance. */
function agreesWithCell(lat: number, lon: number, cell: GridCell): boolean {
    return (
        Math.abs(lat - cell.lat) <= cell.latSpan / 2 &&
        Math.abs(lon - cell.lon) <= cell.lonSpan / 2
    );
}

/** Far-end coordinates: enrichment decimal lat/lon when it agrees with the
 *  station's own gridsquare, else the grid's cell centre, else null
 *  (unplottable). parseFloat rejects import-era ADIF Location strings
 *  ("N051 30.000") → grid fallback.
 *
 *  Coordinates are preferred because they are more precise than a cell, but
 *  only while they are consistent with it. QRZ returns lat/lon and grid as
 *  independent fields and storage merges them independently, so a station can
 *  carry a correct grid beside coordinates for somewhere else entirely: two rows
 *  of the newest 500 in the dogfood log drew arcs to the South Pole that way.
 *  Trusting the grid in that case loses precision the coordinates never had.
 *
 *  With NO usable grid the coordinates are used as given, however odd they look.
 *  There is nothing to contradict them, and a plausibility rule would relocate
 *  stations on a guess — a worse fault than the one this avoids, because nothing
 *  would show it had happened. */
export function rowPoint(q: LogbookQso): LatLon | null {
    const lat = Number.parseFloat(q.lat ?? '');
    const lon = Number.parseFloat(q.lon ?? '');
    const cell = gridToCell(q.gridsquare ?? '');
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) {
        return cell === null ? null : { lat: cell.lat, lon: cell.lon };
    }
    if (cell === null || agreesWithCell(lat, lon, cell)) return { lat, lon };
    const key = `${q.call ?? ''}|${q.gridsquare ?? ''}|${q.lat ?? ''}|${q.lon ?? ''}`;
    if (!gridConflictSeen.has(key)) {
        gridConflictSeen.add(key);
        console.warn(
            `[map-data] ${q.call ?? '(no call)'} coordinates ${lat},${lon} fall outside grid ${q.gridsquare}; plotting the grid`
        );
    }
    return { lat: cell.lat, lon: cell.lon };
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
/** Data-refresh generation: every refresh() bumps it so a superseded fetch
 *  discards its result. NOT a lifecycle signal — an operator changing the
 *  window mid-startup bumps this, so startup must not read it as teardown. */
let generation = 0;
/** Lifecycle generation: bumped ONLY by teardown. The startup continuation
 *  compares it across its await to avoid installing the listener + stream
 *  after the view is already gone. */
let lifecycle = 0;

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
    storageSet(WINDOW_KEY, String(minutes));
    mapData.status = 'loading';
    void refresh();
}

/**
 * Bring the view up: resolve station context (logbook + origin grid), open
 * the event stream, then run the first windowed fetch. Returns the teardown.
 */
export function startMapData(): () => void {
    mapData.status = 'loading';
    mapData.durationMin = storedDurationMin(storageGet(WINDOW_KEY));
    // Teardown may run while the context fetch below is still pending; the
    // lifecycle bump it does lets the continuation detect that and bail
    // BEFORE installing the listener + stream — otherwise they'd leak with no
    // owner left to close them.
    const startLife = lifecycle;
    void (async () => {
        const ctx = await fetchStationContext();
        if (startLife !== lifecycle) return; // torn down while awaiting — install nothing
        logbookId = ctx.logbookId;
        mapData.origin = gridToDecimal(ctx.myGrid);
        mapData.bandColors = ctx.mapBandColors;
        if (logbookId === 0) {
            mapData.status = 'error';
            mapData.message = 'Station config unavailable — cannot resolve the logbook.';
            return;
        }
        // Hidden tabs get throttled timers (the debounce below) and possibly
        // a silently-dead stream, so a backgrounded map goes stale and NOTHING
        // forces a catch-up when the operator returns (dogfood 2026-07-18).
        // One immediate refetch on becoming visible heals every root cause at
        // the only moment staleness matters. Idempotent + generation-guarded,
        // so a burst of tab switches costs one in-flight fetch at most.
        document.addEventListener('visibilitychange', onVisibilityCatchUp);
        // Stream first, then fetch — events for rows the fetch already
        // returns are idempotent (the refetch is the idempotency).
        closeEvents = openLogEvents({
            onOpen: () => {
                mapData.live = true;
                // EVERY open schedules a catch-up refetch: the stream has no
                // backlog, so a QSO logged/edited while it was down (a
                // reconnect gap, OR a failed first attempt while the baseline
                // fetch ran) produced no event and the map would sit stale
                // until the next unrelated change. Idempotent + coalesced —
                // the extra startup fetch is the cost of never missing a gap.
                scheduleRefresh();
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
        lifecycle++; // stop a pending startup from installing anything
        generation++; // invalidate any in-flight refresh
        document.removeEventListener('visibilitychange', onVisibilityCatchUp);
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

function onVisibilityCatchUp(): void {
    if (!document.hidden) void refresh();
}
