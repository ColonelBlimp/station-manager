/**
 * Operator configuration state — view of `/v1/config`.
 *
 * Per ADR 0003 (`docs/decisions/0003-spa-config-daemon-only.md`):
 * the daemon's filesystem is the only persistent source of operator
 * config; this module is a SPA-side reactive cache. Per ADR 0009
 * (`docs/decisions/0009-cat-state-decomposition.md`), it is one of
 * the four state singletons in the decomposition — alongside
 * catState (rig mirror), manualState (operator edits), and
 * displayedState (derived).
 *
 * `applyResponse(...)` is called from `app.svelte`'s onMount after
 * `fetchConfig()` resolves; the SPA renders defaults in the brief
 * window before that lands and after a network failure (the toast
 * tells the operator to check the daemon — the UI keeps working with
 * the stub state).
 *
 * **Sub-section: `station`** — operator-declared station properties
 * that participate in displayedState's derivations (CAT-side concern,
 * distinct from the daemon-fetched logging_station identity below):
 *
 *   - `enabled` — whether CAT is in use. Distinct from
 *     `bridgeState.connected` (transport) and `bridgeState.rigResponding`
 *     (rig health). Operator's intent.
 *   - `ampMultiplier` — linear amp factor for `effectivePower`
 *     computation in displayedState. 1.0 = no amp. 2.0 = 200W amp on
 *     a 100W rig. Per ADR 0009 this is the canonical example of a
 *     "station property" that combines with a CAT-reported field
 *     (`catState.power`) to produce the QSO-logged value.
 *
 * **Daemon-fetched sections** (populated by applyResponse):
 *
 *   - `setupComplete` — false on first run; SPA renders setup
 *     dialog. Flips true when the operator submits their callsign.
 *   - `loaded` — false until the GET /v1/config response (or its
 *     failure) is processed. Lets components show a brief loading
 *     state instead of flashing the setup dialog before hydration.
 *   - `loggingStation` — operator's ADIF MY_* identity. Setup
 *     dialog writes station_callsign here.
 *   - `defaultLogbook` — projection of the DB row matched by
 *     default_logbook_id. Display fields populate post-setup.
 *   - `defaultRig` — projection of the configured rig matched by
 *     default_rig_id. Empty until CAT lands.
 */

import type { ConfigResponse } from '../api/config';

class StationConfig {
    /** Whether CAT is enabled in operator config. SPA-only, in-memory. */
    enabled: boolean = $state(false);

    /**
     * Whether the linear-amp multiplier is applied to rig power on
     * QSO submit. Daemon-persisted via /v1/config station.amp_enabled.
     * `displayedState.effectivePower` checks this flag before
     * multiplying (false → raw rig power passes through).
     */
    ampEnabled: boolean = $state(false);

    /**
     * Linear amp multiplier — applied to rig-reported power to compute
     * effective radiated power for QSO logging. 1.0 means no amp.
     * Daemon-persisted via /v1/config station.amp_multiplier; daemon
     * rejects negative values and values above 1000.
     */
    ampMultiplier: number = $state(1.0);

    /**
     * TX power in watts used by displayedState.rawPower when CAT is
     * unavailable. CAT-reported power overrides this when the bridge
     * is live. 0 means "not set" — ADIF TX_PWR is omitted from the
     * QSO record (matches the existing omit-when-zero rule). Set in
     * the My Station Equipment sub-tab; daemon-persisted via
     * /v1/config station.default_power; daemon rejects values
     * outside [0, 2000].
     */
    defaultPower: number = $state(0);
}

class LoggingStationView {
    /**
     * Static-ish ADIF MY_* identity fields. Hydrated from `/v1/config`
     * and rarely change once set, so they are plain properties — no
     * `$state` overhead. Nothing reactively derives from these; setup
     * dialog and My Station card write them directly via
     * `applyResponse(...)` after a PUT round-trip. `bind:value` works
     * fine on plain fields — it's a binding mechanism, not a reactive
     * read.
     *
     * Bucketing matches the operator-confirmed split (session 34):
     *   - Identity (station/operator/owner) — fallback chain applied in
     *     applyResponse.
     *   - Operator-typed (My Station panel via ValidatedInput).
     *   - Daemon-derived (myLat/myLon from myGridsquare) — read-only,
     *     overwritten on every applyResponse, never edited in the SPA.
     *
     * ADIF fallback chain (per spec):
     *   - If STATION_CALLSIGN is absent, OPERATOR is treated as both the
     *     logging station's callsign and the logging operator's callsign.
     *   - If OWNER_CALLSIGN is absent, STATION_CALLSIGN is treated as both
     *     the station's callsign and the owner's callsign.
     */
    /** ADIF STATION_CALLSIGN — set during first-run setup. */
    stationCallsign: string = '';

    /** ADIF OPERATOR — the logging operator's callsign. Reactive: drives downstream consumers. */
    operator: string = $state('');

    /** ADIF OWNER_CALLSIGN — licensee/club owner if different from station_callsign. */
    ownerCallsign: string = '';

    // Operator-typed via the My Station panel (session 34 scope).
    myAltitude: string = '';
    myAntenna: string = '';
    myCity: string = '';
    myCountry: string = '';
    myCqZone: string = '';
    myDxcc: string = '';
    myGridsquare: string = '';
    myItuZone: string = '';
    myMorseKeyInfo: string = '';
    myMorseKeyType: string = '';
    myName: string = '';
    myPostalCode: string = '';
    myRig: string = '';
    myStreet: string = '';

    // Daemon-derived from myGridsquare. The SPA never writes these
    // directly — a PUT with a new myGridsquare causes the daemon to
    // recompute and the response hydrates them here.
    myLat: string = '';
    myLon: string = '';
}

class DefaultLogbookView {
    id: number = $state(0);
    name: string = $state('');
    callsign: string = $state('');
    description: string = $state('');
}

class DefaultRigView {
    id: number = $state(0);
    model: string = $state('');
    port: string = $state('');
}

class ConfigState {
    /** CAT-side station properties — see ADR 0009. */
    station: StationConfig = new StationConfig();

    /**
     * Daemon's setup gate. Renders the setup dialog when false; the
     * dialog flips this to true on successful PUT /v1/config.
     */
    setupComplete: boolean = $state(false);

    /**
     * False until the first /v1/config fetch settles (success or
     * failure). Components wait on this to avoid flashing a setup
     * dialog before the real state arrives.
     */
    loaded: boolean = $state(false);

    loggingStation: LoggingStationView = new LoggingStationView();
    defaultLogbook: DefaultLogbookView = new DefaultLogbookView();
    defaultRig: DefaultRigView = new DefaultRigView();

    /**
     * Hydrate from a daemon GET/PUT /v1/config response. Each block
     * is copied field-by-field rather than wholesale-assigned so the
     * Svelte 5 $state reactivity boundaries on the inner classes are
     * preserved (assigning a new instance would replace the proxy
     * with a plain object and break $derived consumers).
     */
    applyResponse(resp: ConfigResponse): void {
        this.setupComplete = resp.setup_complete;

        // ADIF fallback chain: OPERATOR fills missing STATION_CALLSIGN;
        // STATION_CALLSIGN (after that resolution) fills missing
        // OWNER_CALLSIGN. Order matters — owner depends on the
        // post-fallback station value.
        const operator = resp.logging_station.operator ?? '';
        const stationCallsign = resp.logging_station.station_callsign || operator;
        const ownerCallsign = resp.logging_station.owner_callsign || stationCallsign;

        this.loggingStation.operator = operator;
        this.loggingStation.stationCallsign = stationCallsign;
        this.loggingStation.ownerCallsign = ownerCallsign;

        const ls = resp.logging_station;
        this.loggingStation.myAltitude = ls.my_altitude ?? '';
        this.loggingStation.myAntenna = ls.my_antenna ?? '';
        this.loggingStation.myCity = ls.my_city ?? '';
        this.loggingStation.myCountry = ls.my_country ?? '';
        this.loggingStation.myCqZone = ls.my_cq_zone ?? '';
        this.loggingStation.myDxcc = ls.my_dxcc ?? '';
        this.loggingStation.myGridsquare = ls.my_gridsquare ?? '';
        this.loggingStation.myItuZone = ls.my_itu_zone ?? '';
        this.loggingStation.myMorseKeyInfo = ls.my_morse_key_info ?? '';
        this.loggingStation.myMorseKeyType = ls.my_morse_key_type ?? '';
        this.loggingStation.myName = ls.my_name ?? '';
        this.loggingStation.myPostalCode = ls.my_postal_code ?? '';
        this.loggingStation.myRig = ls.my_rig ?? '';
        this.loggingStation.myStreet = ls.my_street ?? '';

        this.loggingStation.myLat = ls.my_lat ?? '';
        this.loggingStation.myLon = ls.my_lon ?? '';

        this.defaultLogbook.id = resp.default_logbook.id;
        this.defaultLogbook.name = resp.default_logbook.name ?? '';
        this.defaultLogbook.callsign = resp.default_logbook.callsign ?? '';
        this.defaultLogbook.description = resp.default_logbook.description ?? '';

        this.defaultRig.id = resp.default_rig.id;
        this.defaultRig.model = resp.default_rig.model ?? '';
        this.defaultRig.port = resp.default_rig.port ?? '';

        // station block — daemon-persisted amp config. CAT-enabled
        // (this.station.enabled) stays SPA-only and is not touched
        // here; only the amp pair round-trips.
        if (resp.station) {
            this.station.ampEnabled = resp.station.amp_enabled ?? false;
            this.station.ampMultiplier = resp.station.amp_multiplier ?? 1.0;
            this.station.defaultPower = resp.station.default_power ?? 0;
        }

        this.loaded = true;
    }

    /** Mark the fetch as settled even on failure so consumers can render fallbacks. */
    markLoaded(): void {
        this.loaded = true;
    }
}

export const configState = new ConfigState();
