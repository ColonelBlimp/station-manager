/**
 * CAT (Computer Aided Transceiver) state — read-only mirror of the rig.
 *
 * Per ADR 0009 (`docs/decisions/0009-cat-state-decomposition.md`), this
 * is one of four state singletons in the SPA's CAT decomposition:
 *
 *   - **catState** (this) — pure rig mirror. Bridge SSE writes only.
 *     Components MUST NOT read this directly outside the displayedState
 *     derivation; they read displayedState (`displayed.svelte.ts`).
 *   - manualState — operator edits in CAT-off mode.
 *   - configState — operator-declared station properties.
 *   - displayedState — derived view that components actually read.
 *
 * Bridge SSE wire shape per ADR 0010
 * (`docs/decisions/0010-rig-sse-wire-shape.md`): the bridge maintains a
 * current-state cache and emits `rig-state` events (full snapshot on
 * connect, deltas thereafter). The SPA's `lib/states/bridge.svelte.ts`
 * subscribes and merges payloads into catState field-by-field.
 *
 * **Defaults are bootstrap fallbacks** per ADR 0003. The constants below
 * are exported because manualState shares the same defaults — both
 * states bootstrap from these values until either the bridge has
 * something to say (catState) or the operator types something
 * (manualState).
 */

export const DEFAULT_VFO_HZ = 14_250_000;
export const DEFAULT_MODE = 'USB';
export const DEFAULT_SUB_MODE = '';
export const DEFAULT_SELECTED_VFO: 'A' | 'B' = 'A';
export const DEFAULT_RIG_IDENTITY = '';
export const DEFAULT_POWER_WATTS = 100;

class CatState {
    /**
     * Rig identity as reported by CAT (model string, e.g. "IC-7300",
     * "FT-991A"). Empty string when CAT has not yet reported — rig
     * off, bridge unreachable, or CAT disabled. Bridge writes only.
     */
    rigIdentity: string = $state(DEFAULT_RIG_IDENTITY);

    /**
     * VFO A frequency in Hz. Bridge writes only via `rig-state` event
     * merge. Hz (not kHz / MHz) is the natural CAT protocol unit and
     * avoids floating-point drift; JS f64 has 53 bits of integer
     * precision. UI formatting and ADIF MHz conversion happen at
     * presentation/submit boundaries — not here.
     */
    vfoA: number = $state(DEFAULT_VFO_HZ);

    /** VFO B frequency in Hz. Bridge writes only. */
    vfoB: number = $state(DEFAULT_VFO_HZ);

    /**
     * Mode of the active VFO. ADIF MODE field — USB, LSB, CW, FM, AM,
     * RTTY, FT8, etc. Maps to `types.Qso.Mode` at QSO submit time.
     * Bridge writes only.
     */
    mode: string = $state(DEFAULT_MODE);

    /**
     * Sub-mode refinement. ADIF SUBMODE field — e.g. CW-N, FT4, FM-D.
     * Empty string when none reported. Bridge writes only.
     */
    subMode: string = $state(DEFAULT_SUB_MODE);

    /**
     * Which VFO is currently selected. In non-split operation this is
     * both the TX and RX VFO; in split, RX uses this VFO and TX uses
     * the other. Bridge writes only.
     */
    selectedVfo: 'A' | 'B' = $state(DEFAULT_SELECTED_VFO);

    /**
     * Bridge-set split flag from the rig. `null` when the rig has not
     * reported a split status (CAT-off mode), in which case
     * `displayedState.split` derives split from frequency divergence
     * per ADR 0009. `true` / `false` when the rig has reported.
     * Bridge writes only.
     */
    splitOverride: boolean | null = $state(null);

    /**
     * Raw power reported by the rig (watts). 0 when the bridge has
     * not yet reported. `displayedState.effectivePower` applies the
     * operator's amp multiplier per ADR 0009. Bridge writes only.
     */
    power: number = $state(0);
}

export const catState = new CatState();
