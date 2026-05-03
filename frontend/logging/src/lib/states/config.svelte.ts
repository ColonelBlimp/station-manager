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
    /** Whether CAT is enabled in operator config. */
    enabled: boolean = $state(false);

    /**
     * Linear amp multiplier — applied to rig-reported power to compute
     * effective radiated power for QSO logging. 1.0 means no amp.
     */
    ampMultiplier: number = $state(1.0);
}

class LoggingStationView {
    /** ADIF STATION_CALLSIGN — set during first-run setup. */
    stationCallsign: string = $state('');
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

        this.loggingStation.stationCallsign = resp.logging_station.station_callsign ?? '';

        this.defaultLogbook.id = resp.default_logbook.id;
        this.defaultLogbook.name = resp.default_logbook.name ?? '';
        this.defaultLogbook.callsign = resp.default_logbook.callsign ?? '';
        this.defaultLogbook.description = resp.default_logbook.description ?? '';

        this.defaultRig.id = resp.default_rig.id;
        this.defaultRig.model = resp.default_rig.model ?? '';
        this.defaultRig.port = resp.default_rig.port ?? '';

        this.loaded = true;
    }

    /** Mark the fetch as settled even on failure so consumers can render fallbacks. */
    markLoaded(): void {
        this.loaded = true;
    }
}

export const configState = new ConfigState();
