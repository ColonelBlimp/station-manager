// Station Events read — GET /v1/station-events (W-0020, ADR 0076). The one read
// surface of the daemon's durable operator-event store, across the notification
// and alarm categories, newest first. `detail` is the typed metadata the daemon
// stored per kind; it is `unknown` here so the UI narrows it defensively and
// degrades an unknown or future shape to a fixed placeholder rather than
// stringifying raw content (ADR 0010: stable codes, client wording).
import { daemonErrorMessage, isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type StationEventCategory = 'notification' | 'alarm';
export type StationEventSeverity = 'info' | 'warn' | 'error';

// The whole retained ring: 500 rows per category × two categories. The page
// has no pagination, so a smaller cap would hide retained rows without a path
// to reach them (W-0020).
export const STATION_EVENTS_LIMIT = 1000;

export interface StationEvent {
    id: number;
    category: string;
    kind: string;
    severity: string;
    occurred_at: string;
    build: string;
    detail: unknown;
}

export interface StationEventFilter {
    category?: StationEventCategory;
    severity?: StationEventSeverity;
    limit?: number;
}

export type StationEventsOutcome =
    { kind: 'ok'; items: StationEvent[] } | { kind: 'error'; message: string };

const transportMessage = (kind: string): string =>
    kind === 'aborted' ? 'Request was cancelled.' : 'Could not reach the daemon.';

/** The query string for a filter — exported so the page's tests can pin what a
 *  chip sends without stubbing fetch twice. Omitted filters are omitted. */
export function stationEventsQuery(f: StationEventFilter): string {
    const q = new URLSearchParams();
    if (f.category) q.set('category', f.category);
    if (f.severity) q.set('severity', f.severity);
    q.set('limit', String(f.limit ?? STATION_EVENTS_LIMIT));
    return `?${q.toString()}`;
}

export async function fetchStationEvents(
    f: StationEventFilter = {},
    signal?: AbortSignal
): Promise<StationEventsOutcome> {
    const fetched = await safeFetch(`/v1/station-events${stationEventsQuery(f)}`, { signal });
    if (!fetched.ok) return { kind: 'error', message: transportMessage(fetched.kind) };
    const body = await readJsonBody(fetched.response);
    if (!fetched.response.ok) {
        return { kind: 'error', message: daemonErrorMessage(fetched.response.status, body) };
    }
    if (!isPlainObject(body) || !Array.isArray(body.items)) {
        return { kind: 'error', message: 'Unexpected station events response.' };
    }
    return { kind: 'ok', items: body.items as StationEvent[] };
}
