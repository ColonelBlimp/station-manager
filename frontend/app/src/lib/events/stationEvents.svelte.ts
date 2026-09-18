/*
    Station Events page state (W-0020) — the read surface of the daemon's durable
    operator-event store: the newest events across the notification and alarm
    categories, narrowed by two filters the operator picks on the page.

    FILTERS ARE SERVER-SIDE: a chip changes the query and refetches, so the
    count line reports what the daemon returned for THAT filter — never a client
    slice of a larger page that could hide older matches beyond the window.

    Transient view state, no persistence: which filter is up is not worth
    surviving a reload. A generation guard keeps an out-of-order response from
    clobbering fresher rows (the rail's lesson, kept).
*/
import {
    fetchStationEvents,
    STATION_EVENTS_LIMIT,
    type StationEvent,
    type StationEventCategory,
    type StationEventSeverity,
} from '../api/stationEvents';

export class StationEventsState {
    items: StationEvent[] = $state([]);
    loading: boolean = $state(false);
    error: string = $state('');
    category: StationEventCategory | '' = $state('');
    severity: StationEventSeverity | '' = $state('');
    #loadGen = 0;

    async load(): Promise<void> {
        const gen = ++this.#loadGen;
        this.loading = true;
        this.error = '';
        this.items = []; // no rows from the previous filter under the new filter's count
        const out = await fetchStationEvents({
            category: this.category || undefined,
            severity: this.severity || undefined,
            limit: STATION_EVENTS_LIMIT,
        });
        if (gen !== this.#loadGen) return; // superseded by a newer load
        if (out.kind === 'ok') {
            this.items = out.items;
        } else {
            this.error = out.message;
        }
        this.loading = false;
    }

    setCategory(c: StationEventCategory | ''): void {
        this.category = c;
        void this.load();
    }

    setSeverity(s: StationEventSeverity | ''): void {
        this.severity = s;
        void this.load();
    }

    /** What the list is narrowed to, in words — for the count line and the
     *  empty state, so an empty filtered list reads as a filter, not a fault. */
    filterDescription(singular = false): string {
        const noun =
            this.category === 'alarm'
                ? 'alarm event'
                : this.category === 'notification'
                  ? 'notification event'
                  : 'event';
        const cat = singular ? noun : `${noun}s`;
        return this.severity ? `${cat} at severity ${this.severity}` : cat;
    }

    countLine(): string {
        if (this.loading) return `Loading ${this.filterDescription()}…`;
        if (this.error) return `Could not load ${this.filterDescription()}.`;
        const n = this.items.length;
        const what = this.filterDescription(n === 1);
        if (n === 0) return `No ${what}.`;
        const shown = n >= STATION_EVENTS_LIMIT ? `newest ${n}` : `${n}`;
        return `${shown} ${what}`;
    }

    reset(): void {
        this.items = [];
        this.loading = false;
        this.error = '';
        this.category = '';
        this.severity = '';
        this.#loadGen++;
    }
}

export const stationEventsState = new StationEventsState();
