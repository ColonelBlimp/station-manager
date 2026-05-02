/**
 * Logging station profile — identity, location, and equipment of the
 * station this SPA is logging from.
 *
 * Held as a Svelte writable store rather than a `$state` rune
 * (settled session 28). Reasoning:
 *
 *   - These fields are populated once at app start (by `/v1/config`
 *     fetch when the daemon endpoint lands; by hand-edit of the
 *     defaults below until then) and rarely mutate during the page
 *     life. A future settings UI will mutate via `station.set(...)`.
 *   - No `$derived` reads from these values, so per-field reactive
 *     subscriptions earn their keep nowhere. Wrapping each field in
 *     `$state` would be Proxy overhead with no consumer.
 *   - The store gives the right shape: a single subscription point
 *     for templates that DO display these (planned "My Station" card,
 *     header chrome), and `get(station)` for one-shot reads at QSO
 *     submit time.
 *
 * The reactivity boundary: `$state` for fields that drive `$derived`
 * computations (`configState.station.enabled`, `.ampMultiplier`); the
 * store for static profile data (here). See `frontend-spa.md`
 * "State / persistence layers" for the broader split.
 *
 * File location: `lib/stores/` (plural) rather than `lib/states/`.
 * The `lib/states/` directory holds runes-based state modules
 * (`*.svelte.ts`); `lib/stores/` holds Svelte stores. Distinct
 * paradigms, distinct directories.
 *
 * **Why "station" and not "operator":** in ham radio, three callsigns
 * can co-exist for a single QSO record:
 *   - STATION_CALLSIGN — the call attached to the station / log entry
 *     (e.g. 7Q1DX)
 *   - the owner of the station (e.g. 7Q5MLV — license holder of the
 *     physical station, typically same person but not always)
 *   - OPERATOR — the actual person at the controls (e.g. 7Q7EB —
 *     guest operator, contest team member, etc.)
 * v1 only models STATION_CALLSIGN. Operator/owner extensions can land
 * later if a multi-op or club-station scenario surfaces.
 *
 * Defaults: empty strings everywhere. The omit-if-empty rule in
 * `formatAdifRecord` (see `lib/utils/adif.ts`) handles unset fields
 * cleanly. Edit the literals below to populate your own station
 * details — until the daemon `/v1/config` endpoint lands, this file
 * IS the configuration source.
 *
 * ADIF mapping (per pass-through in QsoPanel.submitQso):
 *   stationCallsign     → STATION_CALLSIGN
 *   location.gridSquare → MY_GRIDSQUARE
 *   location.name       → MY_NAME
 *   equipment.rig       → MY_RIG
 *   equipment.antenna   → MY_ANTENNA
 */

import {type Writable, writable} from 'svelte/store';

export interface Station {
    /** ADIF STATION_CALLSIGN — the call attached to this log/station. */
    stationCallsign: string;
    /** Where the station is. */
    location: {
        /** Maidenhead grid square, e.g. "IO91vl" — ADIF MY_GRIDSQUARE. */
        gridSquare: string;
        /** Operator's name (what you say to the contact) — ADIF MY_NAME. */
        name: string;
    };
    /** What the station is using. */
    equipment: {
        /** Human-friendly rig name, e.g. "IC-7300" — ADIF MY_RIG. */
        rig: string;
        /** Antenna description — ADIF MY_ANTENNA. */
        antenna: string;
    };
}

export const station: Writable<Station> = writable<Station>({
    stationCallsign: '7Q5DX',
    location: {
        gridSquare: 'KH78an',
        name: 'My name',
    },
    equipment: {
        rig: 'FTdx10',
        antenna: 'Hex Beam',
    },
});
