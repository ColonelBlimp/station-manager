/**
 * Derived view over catState / manualState / configState / bridgeState.
 *
 * Per ADR 0009 (`docs/decisions/0009-cat-state-decomposition.md`),
 * this is the **only** state object components should read for CAT-
 * adjacent values. Reading the underlying singletons directly bypasses
 * the precedence rule and creates drift over time.
 *
 * The structural ownership of the four-object model is:
 *
 *   - `catState`     — bridge writes only (rig mirror).
 *   - `manualState`  — SPA writes only (operator edits).
 *   - `configState`  — SPA config-loader writes (operator-declared).
 *   - `displayedState` — none — `$derived` only.
 *
 * Components read `displayedState.X`. Writes go to whichever underlying
 * singleton owns the field — never to displayedState directly.
 *
 * **The three-flag rule** (ADR 0006, refined by ADR 0009 + ADR 0010):
 * a "live" rig source is in charge when ALL THREE of these hold —
 * configState.station.enabled (operator wants CAT) AND
 * bridgeState.connected (SSE channel open) AND
 * bridgeState.rigResponding (rig is actively reporting). When any is
 * false, the SPA falls back to manualState.
 *
 * `editable` is the inverse — operator can edit when no live source.
 *
 * **`split` is structurally derived.** No writeable `split` field
 * exists anywhere — only `splitOverride` (in catState, bridge-written)
 * and `manualState.vfoA`/`vfoB` (SPA-written). To "set split" in
 * CAT-off mode means changing a VFO frequency. Same-frequency split
 * is not expressible in CAT-off mode (limitation accepted in 0006/0009).
 *
 * **`effectivePower`** is the canonical ADR-0009 example: combines a
 * rig-reported field with a station-declared multiplier. Same pattern
 * recurs for transverter offset, antenna gain, and similar
 * transformations as they're added.
 */

import { catState } from './cat.svelte';
import { manualState } from './manual.svelte';
import { configState } from './config.svelte';
import { bridgeState } from './bridge.svelte';

class DisplayedState {
    /**
     * Whether a live rig source is driving values. True when CAT is
     * enabled in config AND the SSE transport is open AND the rig is
     * actively reporting. Reads switch to catState when true.
     */
    isLive: boolean = $derived(
        configState.station.enabled
        && bridgeState.connected
        && bridgeState.rigResponding,
    );

    /**
     * Whether SPA edit affordances should accept input. Inverse of
     * `isLive` — operator can edit whenever no live rig source is in
     * charge (CAT disabled, bridge unreachable, or rig not responding).
     * Components gate input enable/disable, button click handlers,
     * keyboard shortcuts that mutate CAT-state, etc., on this flag.
     */
    editable: boolean = $derived(!this.isLive);

    vfoA: number = $derived(this.isLive ? catState.vfoA : manualState.vfoA);
    vfoB: number = $derived(this.isLive ? catState.vfoB : manualState.vfoB);
    mode: string = $derived(this.isLive ? catState.mode : manualState.mode);
    subMode: string = $derived(this.isLive ? catState.subMode : manualState.subMode);
    selectedVfo: 'A' | 'B' = $derived(
        this.isLive ? catState.selectedVfo : manualState.selectedVfo,
    );
    rigIdentity: string = $derived(this.isLive ? catState.rigIdentity : '');

    /**
     * Split state. When live, uses the rig's reported splitOverride
     * (defaulting to false if the rig hasn't reported one yet). When
     * not live, derived from frequency divergence in manualState.
     */
    split: boolean = $derived(
        this.isLive
            ? (catState.splitOverride ?? false)
            : (manualState.vfoA !== manualState.vfoB),
    );

    /**
     * Raw power. Rig-reported when live; operator-typed (manualState)
     * when not. Watts.
     */
    rawPower: number = $derived(this.isLive ? catState.power : manualState.power);

    /**
     * Effective radiated power = raw × amp multiplier. This is what
     * gets logged in QSO submissions.
     */
    effectivePower: number = $derived(
        this.rawPower * configState.station.ampMultiplier,
    );
}

export const displayedState = new DisplayedState();
