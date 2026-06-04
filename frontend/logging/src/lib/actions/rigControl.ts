/*
    Operator rig-control actions (ADR 0026 inbound command path). These
    coordinate the four-object CAT state (ADR 0009) with the daemon command
    endpoint: when CAT is live they drive the rig (and the UI updates only
    when the rig pushes the new state back — confirm-by-push); when CAT is off
    they edit manualState locally. Shared by both the mouse (VfoBox click) and
    keyboard (Ctrl+\ swap) paths so the two behave identically.

    Capability-gated: a live command is only sent when the configured rig
    exposes the op (configState.bridge.ops). When CAT is live but the rig
    can't do it, the action is a no-op — manualState is ignored while live, so
    there's nothing useful to fall back to.
*/

import { displayedState } from '../states/displayed.svelte';
import { manualState } from '../states/manual.svelte';
import { configState } from '../states/config.svelte';
import { toasts } from '../states/toasts.svelte';
import { sendRigCommand } from '../api/rigCommand';

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
 * Toggle the selected VFO (A↔B) — the Ctrl+\ swap. Resolves the "other" VFO
 * from the currently displayed selection and defers to selectVfo so the
 * CAT-on/off branching lives in exactly one place.
 */
export function swapVfo(): void {
    selectVfo(displayedState.selectedVfo === 'A' ? 'B' : 'A');
}

async function driveRig(op: string, value: string): Promise<void> {
    const outcome = await sendRigCommand(op, value);
    if (outcome.kind !== 'ok') {
        toasts.error(`Rig command failed: ${outcome.message}`);
    }
}
