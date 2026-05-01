/**
 * CAT (Computer Aided Transceiver) state.
 *
 * Module-level singleton — any component that reads `catState.enabled`
 * (or any other field) re-renders automatically when the value changes.
 *
 * **Why this is its own state object:** the VFO panel and several future
 * widgets (band indicator, mode display, split indicator, TX/RX state)
 * all derive from the rig's CAT-reported state. Centralising avoids
 * each widget reaching into the bridge transport directly, and gives
 * a single place to enforce the "rig off → use defaults" fallback.
 *
 * **Layering:** this module owns the *state shape*. The bridge SSE
 * subscription that updates these fields lives in (TBD)
 * `lib/states/bridge.svelte.ts` per the v2-design sketch. Operator-config
 * fields like `enabled` are populated from the daemon's config API,
 * not from the bridge.
 *
 * **Defaults are bootstrap fallbacks.** The constants below are the
 * second (and only) in-code tier per ADR
 * `docs/decisions/0003-spa-config-daemon-only.md`: daemon `/v1/config`
 * is the authoritative source, and these hardcoded constants are what
 * the UI shows during boot before the first daemon response arrives,
 * or on first-install when the daemon's config file doesn't yet exist.
 * Live CAT values from the bridge override both at runtime. There is
 * no localStorage cache layer — the SPA is hosted by the daemon, so
 * the daemon is always reachable when the SPA is running.
 * Implementation of the daemon-config wiring is deferred; until then,
 * only these hardcoded fallbacks are in use.
 */

const DEFAULT_VFO_HZ = 14_250_000;
const DEFAULT_MODE = 'USB';
const DEFAULT_SUB_MODE = '';
const DEFAULT_SELECTED_VFO: 'A' | 'B' = 'A';
const DEFAULT_SPLIT = false;
const DEFAULT_RIG_IDENTITY = '';

class CatState {
    /**
     * Whether the operator has enabled CAT in their configuration.
     *
     * Distinct from "the rig is currently responding" — CAT can be
     * enabled while the rig is switched off, the cable is unplugged,
     * or the bridge service isn't reachable. In all of those cases
     * the VFO panel falls back to default values; this flag only
     * tells you whether the operator *intends* CAT to be in use.
     */
    enabled: boolean = $state(false);

    /**
     * Rig identity as reported by CAT (model string, e.g. "IC-7300",
     * "FT-991A"). Empty string when CAT has not yet reported — rig
     * off, bridge unreachable, or CAT disabled. Not part of operator
     * config; purely live from the rig.
     */
    rigIdentity: string = $state(DEFAULT_RIG_IDENTITY);

    /**
     * VFO A frequency in Hz. Hz (not kHz / MHz) is the natural CAT
     * protocol unit and avoids floating-point drift; JS f64 has 53
     * bits of integer precision, comfortably more than any radio
     * frequency. UI formatting (e.g. "14.250.000") is a render
     * concern, not a state concern. ADIF stores frequency in MHz —
     * conversion happens at the QSO submit boundary.
     */
    vfoA: number = $state(DEFAULT_VFO_HZ);

    /**
     * VFO B frequency in Hz. Defaults to the same value as VFO A;
     * many rigs power up with both VFOs synced, and split is off
     * by default so VFO B is functionally inert until the operator
     * acts on it.
     */
    vfoB: number = $state(DEFAULT_VFO_HZ);

    /**
     * Mode of the active VFO. ADIF MODE field — USB, LSB, CW, FM,
     * AM, RTTY, FT8, etc. Maps directly to `types.Qso.Mode` at QSO
     * submit time.
     */
    mode: string = $state(DEFAULT_MODE);

    /**
     * Sub-mode refinement. ADIF SUBMODE field — e.g. CW-N (narrow),
     * FT4 (subset of digital), FM-D (digital FM). Empty string when
     * the mode has no submode or none is reported. Maps to
     * `types.Qso.SubMode` at submit time.
     */
    subMode: string = $state(DEFAULT_SUB_MODE);

    /**
     * Which VFO is currently selected. In non-split operation this
     * is both the TX and RX VFO. In split operation, RX uses this
     * VFO and TX uses the other. Defaults to 'A' which is the
     * conventional power-on selection across most rigs.
     */
    selectedVfo: 'A' | 'B' = $state(DEFAULT_SELECTED_VFO);

    /**
     * Split operation flag. When true, RX uses `selectedVfo` and TX
     * uses the other VFO. Some rigs report split via a dedicated
     * status flag; others infer it from VFO frequency divergence.
     * The bridge normalises both into this single boolean.
     */
    split: boolean = $state(DEFAULT_SPLIT);
}

export const catState = new CatState();
