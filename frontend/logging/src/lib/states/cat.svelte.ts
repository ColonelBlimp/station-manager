/**
 * CAT (Computer Aided Transceiver) state.
 *
 * Module-level singleton — any component that reads `catState.enabled`
 * (or future fields) re-renders automatically when the value changes.
 *
 * **Why this is its own state object:** the VFO panel and several future
 * widgets (band indicator, mode display, split indicator, TX/RX state)
 * all derive from the rig's CAT-reported state. Centralising avoids
 * each widget reaching into the bridge transport directly, and gives
 * a single place to enforce the "rig off → use defaults" fallback.
 *
 * **Layering:** this module owns the *state shape*. The bridge SSE
 * subscription that updates these fields lives in (TBD)
 * `lib/bridge.svelte.ts` per the v2-design sketch. Operator-config
 * fields like `enabled` are populated from the daemon's config API,
 * not from the bridge.
 */

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
}

export const catState = new CatState();
