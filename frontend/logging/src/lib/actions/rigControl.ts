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
 * Select VFO A or B. CAT off → local manualState swap. CAT live + the rig
 * exposes set_vfo → drive the rig (confirm-by-push flips the UI when the VS
 * push arrives). CAT live + no set_vfo → no-op.
 */
export function selectVfo(vfo: 'A' | 'B'): void {
    if (!displayedState.isLive) {
        manualState.selectedVfo = vfo;
        return;
    }
    if (!configState.bridge.ops.includes('set_vfo')) {
        return;
    }
    void driveRig('set_vfo', `VFO-${vfo}`);
}

/**
 * Toggle the selected VFO (A↔B) — the Shift+Ctrl VFO swap. Resolves the "other" VFO
 * from the currently displayed selection and defers to selectVfo so the
 * CAT-on/off branching lives in exactly one place.
 */
export function swapVfo(): void {
    selectVfo(displayedState.selectedVfo === 'A' ? 'B' : 'A');
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
