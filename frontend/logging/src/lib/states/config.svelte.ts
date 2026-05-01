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
 * **Stub for now.** The `/v1/config` fetch and PUT plumbing lands
 * in a later session. Defaults below are hardcoded fallbacks per
 * ADR 0003 — what the SPA shows on first paint before any daemon
 * response, or first-install when no config file exists.
 *
 * **Sub-section: `station`** — operator-declared station properties
 * that participate in displayedState's derivations. Currently:
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
 *   Future sub-section growth: antennaGain, transverterOffset,
 *   stationCallsign, gridSquare, and similar operator-declared
 *   transformations.
 */

class StationConfig {
    /** Whether CAT is enabled in operator config. */
    enabled: boolean = $state(false);

    /**
     * Linear amp multiplier — applied to rig-reported power to compute
     * effective radiated power for QSO logging. 1.0 means no amp.
     */
    ampMultiplier: number = $state(1.0);
}

class ConfigState {
    station: StationConfig = new StationConfig();
}

export const configState = new ConfigState();
