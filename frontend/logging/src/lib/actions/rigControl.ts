/*
    Operator rig-control actions (ADR 0026 inbound command path). These
    coordinate the four-object CAT state (ADR 0009) with the daemon command
    endpoint: when CAT is live they drive the rig (and the UI updates only
    when the rig pushes the new state back — confirm-by-push); when CAT is off
    they edit manualState locally. Shared by both the mouse (VfoBox click) and
    keyboard (Shift+Ctrl) paths so the two behave identically.

    Capability-gated: a live command is only sent when the configured rig
    exposes the op (configState.bridge.ops). When CAT is live but the rig
    can't do it, the action is a no-op — manualState is ignored while live, so
    there's nothing useful to fall back to.
*/

import { displayedState } from '../states/displayed.svelte';
import { manualState } from '../states/manual.svelte';
import { configState } from '../states/config.svelte';
import { bridgeState } from '../states/bridge.svelte';
import { toasts } from '../states/toasts.svelte';
import { sendRigCommand } from '../api/rigCommand';
import { sendRigTune } from '../api/rigTune';

/**
 * Select VFO A or B. CAT off → local manualState selection. CAT live → the
 * FTdx10 has no "select a specific VFO and move onto it" CAT command that
 * actually changes the operating frequency (VS toggles a select flag only —
 * confirmed on-rig: the display indicator moved but the frequency didn't). The
 * working operation is `swap_vfo` (`SV;`), which swaps VFO-A↔B. With only two
 * VFOs, "select the other one" === "swap", so a live select of the non-current
 * VFO swaps; selecting the already-current VFO is a no-op.
 */
export function selectVfo(vfo: 'A' | 'B'): void {
    if (!displayedState.isLive) {
        manualState.selectedVfo = vfo;
        return;
    }
    if (vfo === displayedState.selectedVfo) return;
    swapVfoLive();
}

/**
 * Swap the VFOs (A↔B) — the Shift+Ctrl VFO swap. CAT off → toggle the local
 * manualState selection. CAT live → drive the rig's swap (`SV;`).
 */
export function swapVfo(): void {
    if (!displayedState.isLive) {
        manualState.selectedVfo = displayedState.selectedVfo === 'A' ? 'B' : 'A';
        return;
    }
    swapVfoLive();
}

// swapVfoLive drives the rig's VFO swap (`swap_vfo` → `SV;`), capability-gated.
// The rig pushes the swapped FA/FB (and VS, if it reports one) back over SSE —
// confirm-by-push owns the UI, no optimistic flip.
function swapVfoLive(): void {
    if (!configState.bridge.ops.includes('swap_vfo')) return;
    void driveRig('swap_vfo');
}

/**
 * Step the rig up/down one band (`BU0;` / `BD0;`) — the Shift+Ctrl+] / Shift+Ctrl+[ "run
 * through the bands" sweep. The rig walks its band-stack registers (restoring
 * each band's last freq + mode), and the resulting `FA` push updates the SPA's
 * displayed band. Live-only and capability-gated: band-stepping has no meaning
 * with no rig (manualState has no band concept), so it no-ops when CAT is off
 * or the rig lacks the op.
 */
export function bandUp(): void {
    stepBand('band_up');
}

export function bandDown(): void {
    stepBand('band_down');
}

function stepBand(op: 'band_up' | 'band_down'): void {
    if (!displayedState.isLive) return;
    if (!configState.bridge.ops.includes(op)) return;
    void driveRig(op);
}

/**
 * Jump straight to a band by name (`set_band` → `BS<code>;`) — the Ctrl+Shift+
 * digit shortcuts. The rig restores that band's stack memory; the new band
 * shows via the `FA` push. Live-only and capability-gated, like band-step.
 */
export function selectBand(band: string): void {
    if (!displayedState.isLive) return;
    if (!configState.bridge.ops.includes('set_band')) return;
    void driveRig('set_band', band);
}

// Frequency-step sizes (Hz) for the Shift+Ctrl tuning shortcuts. Page = coarse,
// Arrow = fine — kept here so the magnitudes have one home (the QsoPanel handler
// only knows "coarse up" / "fine down").
const FREQ_STEP_COARSE_HZ = 100;
const FREQ_STEP_FINE_HZ = 10;

// Highest value the rigdef's set_freq field (FA%s; / FB%s; pad 9) holds.
const MAX_FREQ_HZ = 999_999_999;

// Optimistic-target window for live key-repeat tuning. Each live step computes
// an absolute set_freq from the *previous* target rather than the displayed
// freq, because the confirming FA/FB push lags key-repeat — reading
// displayedState every press would compute several steps off one stale value
// and stutter. After a pause (no step within the window, or a VFO switch) we
// re-sync to displayedState, so a physical-knob turn between bursts is picked up.
const FREQ_REPEAT_WINDOW_MS = 350;
const pendingFreqHz: { A: number | null; B: number | null } = { A: null, B: null };
let lastFreqNudgeAt = 0;
let lastFreqVfo: 'A' | 'B' | null = null;

function clampFreq(hz: number): number {
    if (hz < 0) return 0;
    if (hz > MAX_FREQ_HZ) return MAX_FREQ_HZ;
    return hz;
}

/**
 * Nudge the operating (selected) VFO's frequency by deltaHz — the Shift+Ctrl
 * Page/Arrow tuning shortcuts. CAT off → nudge the selected VFO's manualState
 * freq directly (both VFOs are local). CAT live → drive the rig: set_freq (FA)
 * for VFO-A, set_freq_b (FB) for VFO-B, each capability-gated on its own op; a
 * live nudge no-ops when the rig doesn't expose the relevant command. Uses an
 * optimistic per-VFO target so fast key-repeat tracks cleanly despite
 * confirm-by-push lag.
 */
export function nudgeFreq(deltaHz: number): void {
    const vfo = displayedState.selectedVfo;

    if (!displayedState.isLive) {
        if (vfo === 'A') {
            manualState.vfoA = clampFreq(manualState.vfoA + deltaHz);
        } else {
            manualState.vfoB = clampFreq(manualState.vfoB + deltaHz);
        }
        return;
    }

    const op = vfo === 'A' ? 'set_freq' : 'set_freq_b';
    if (!configState.bridge.ops.includes(op)) return;

    const now = Date.now();
    const prev = pendingFreqHz[vfo];
    const inBurst = prev !== null && lastFreqVfo === vfo && now - lastFreqNudgeAt <= FREQ_REPEAT_WINDOW_MS;
    const base = inBurst ? prev : vfo === 'A' ? displayedState.vfoA : displayedState.vfoB;
    const target = clampFreq(base + deltaHz);

    pendingFreqHz[vfo] = target;
    lastFreqNudgeAt = now;
    lastFreqVfo = vfo;
    void driveRig(op, String(target));
}

/** Test-only: clears the optimistic freq-step state between cases so a prior
 *  test's pending target can't leak into the next within the repeat window. */
export function _resetFreqStepForTests(): void {
    pendingFreqHz.A = null;
    pendingFreqHz.B = null;
    lastFreqNudgeAt = 0;
    lastFreqVfo = null;
}

/** Coarse (±100 Hz) tuning nudge — Shift+Ctrl+PageUp/PageDown. dir is +1/-1. */
export function nudgeFreqCoarse(dir: 1 | -1): void {
    nudgeFreq(dir * FREQ_STEP_COARSE_HZ);
}

/** Fine (±10 Hz) tuning nudge — Shift+Ctrl+ArrowUp/ArrowDown. dir is +1/-1. */
export function nudgeFreqFine(dir: 1 | -1): void {
    nudgeFreq(dir * FREQ_STEP_FINE_HZ);
}

async function driveRig(op: string, value?: string): Promise<void> {
    const outcome = await sendRigCommand(op, value);
    if (outcome.kind !== 'ok') {
        toasts.error(`Rig command failed: ${outcome.message}`);
    }
}

/**
 * Toggle the tune carrier (ADR 0027) — the first TX action. Live + capability
 * gated, like the band controls. Sends the opposite of the daemon's current
 * tune-state (bridgeState.tuneActive); the button reflects the new state only
 * when the `tune-state` SSE push arrives (confirm-by-push, no optimistic flip).
 * The daemon owns the guaranteed stop, so a redundant toggle is harmless.
 */
export function toggleTune(): void {
    if (!displayedState.isLive) return;
    if (!configState.bridge.tune) return;
    void driveTune(!bridgeState.tuneActive);
}

async function driveTune(active: boolean): Promise<void> {
    const outcome = await sendRigTune(active);
    if (outcome.kind !== 'ok') {
        toasts.error(`Tune ${active ? 'start' : 'stop'} failed: ${outcome.message}`);
    }
}
