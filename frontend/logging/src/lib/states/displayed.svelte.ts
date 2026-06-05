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
import { resolveModeAndSubmode } from '../utils/mode';

class DisplayedState {
    /**
     * Whether a live rig source is driving values. True when CAT is
     * enabled in config AND the SSE transport is open AND the rig is
     * actively reporting. Reads switch to catState when true.
     */
    isLive: boolean = $derived(
        configState.station.enabled && bridgeState.connected && bridgeState.rigResponding
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

    /**
     * ADIF MODE — the validated main-mode string the QSO submit will
     * carry. Resolution depends on which source is in charge:
     *
     *   - CAT live: `catState.mode` is the rig literal (e.g.
     *     "DATA-U"); we look it up in `configState.bridge.modeMappings`
     *     (merged rigdef defaults + operator overrides). Missing
     *     entries pass the rig string through unchanged — daemon-
     *     side validation will 400 the QSO submit, surfacing the gap
     *     to the operator (fix: add a row in My Station → Mode
     *     Mappings).
     *   - CAT off: operator's dropdown pick lives in `manualState.mode`
     *     as an operator-friendly string (e.g. "USB", "FT8"). We run
     *     it through `resolveModeAndSubmode` to produce the canonical
     *     ADIF MODE (e.g. "USB" → "SSB"; "FT8" → "FT8").
     *
     * Either way, the value at this property is already-resolved ADIF
     * — the QSO submit can use it directly without another resolve.
     */
    mode: string = $derived.by(() => {
        if (this.isLive) {
            const m = configState.bridge.modeMappings[catState.mode];
            return m?.mode ?? catState.mode;
        }
        const resolved = resolveModeAndSubmode(manualState.mode, manualState.subMode);
        return resolved.mode;
    });

    /**
     * ADIF SUBMODE — paired with `mode` above. Comes from the same
     * mapping entry when CAT is live, and from the same
     * `resolveModeAndSubmode` invocation as `mode` when CAT is off.
     * Empty when the chosen mode has no submode refinement (CW, FM,
     * FT8 are themselves ADIF main modes; USB / LSB / PSK31 etc.
     * resolve as submodes of SSB / PSK).
     */
    subMode: string = $derived.by(() => {
        if (this.isLive) {
            const m = configState.bridge.modeMappings[catState.mode];
            return m?.submode ?? '';
        }
        const resolved = resolveModeAndSubmode(manualState.mode, manualState.subMode);
        return resolved.subMode;
    });

    /**
     * The rig's current mode LITERAL (e.g. "USB", "DATA-U", "CW-U") when CAT
     * is live — the value the live Mode control surface binds to and that
     * `set_mode` round-trips (Option A: the live dropdown shows the rig's own
     * mode list). Distinct from `mode`/`subMode` above, which resolve that
     * literal to ADIF for the log. Empty when CAT is off — the off control
     * uses the operator-friendly `manualState` mode instead.
     */
    rigMode: string = $derived(this.isLive ? catState.mode : '');

    selectedVfo: 'A' | 'B' = $derived(this.isLive ? catState.selectedVfo : manualState.selectedVfo);
    rigIdentity: string = $derived(this.isLive ? catState.rigIdentity : '');

    /**
     * Rigdef name resolved daemon-side from `bridge.cat.driver` (e.g.
     * "Yaesu FTdx10"). Distinct from `rigIdentity` (which is the
     * IDENTITY value the rig hardware reports through CAT, e.g.
     * "FTdx10"): rigName is the operator's chosen driver's human-
     * readable name and is always available once `/v1/config` has
     * hydrated. Empty when CAT is not live.
     */
    rigName: string = $derived(this.isLive ? configState.station.rigName : '');

    /**
     * Split state. When live, uses the rig's reported splitOverride
     * (defaulting to false if the rig hasn't reported one yet). When
     * not live, derived from frequency divergence in manualState.
     */
    split: boolean = $derived(
        this.isLive ? (catState.splitOverride ?? false) : manualState.vfoA !== manualState.vfoB
    );

    /**
     * Raw power. Rig-reported when CAT is live; configured default
     * (configState.station.defaultPower, set in My Station →
     * Equipment) when CAT is off / unavailable. Watts. The CAT-off
     * branch was previously manualState.power but no UI ever drove
     * that field — moved to configState as a "set once" preference
     * (session 36, 2026-05-05).
     */
    rawPower: number = $derived(this.isLive ? catState.power : configState.station.defaultPower);

    /**
     * Effective radiated power = raw × amp multiplier when the amp
     * is enabled, raw power otherwise. This is what gets logged in
     * QSO submissions as ADIF TX_PWR.
     */
    effectivePower: number = $derived(
        configState.station.ampEnabled
            ? this.rawPower * configState.station.ampMultiplier
            : this.rawPower
    );
}

export const displayedState = new DisplayedState();
