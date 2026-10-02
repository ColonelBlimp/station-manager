// The recovered contact context belongs to the tab, never a mounted card or
// the live rig. Original provenance stays in record.rig while corrections
// live in rig. No command sender is involved (ADR 0085 RS12–RS15).
import type { SavedDraft } from './savedDraft';
import type { RigReading } from '../operate/rigSnapshot.svelte';
import { frequencyToBand } from '../utils/frequency';
import { resolveModeAndSubmode } from '../utils/mode';

export const recovered = $state<{
    record: SavedDraft | null;
    rig: RigReading | null;
    confirmed: boolean;
}>({ record: null, rig: null, confirmed: false });

/** Confirmation cannot waive a missing or inconsistent contact context. */
export function recoveredRigProblem(): string | null {
    const r = recovered.rig;
    if (r === null) return 'No recovered rig values.';
    if (r.freqHz === null || !Number.isSafeInteger(r.freqHz) || r.freqHz <= 0) {
        return 'Correct the recovered frequency.';
    }
    if (r.band === '' || frequencyToBand(r.freqHz) !== r.band) {
        return 'Correct the recovered band and frequency so they agree.';
    }
    const resolved = resolveModeAndSubmode(r.mode, r.subMode);
    const sub = resolveModeAndSubmode(r.subMode);
    if (
        r.adifMode === '' ||
        resolved.mode !== r.adifMode ||
        resolved.subMode !== r.subMode ||
        (sub.subMode !== '' && sub.mode !== r.adifMode)
    ) {
        return 'Correct the recovered mode and submode so they agree.';
    }
    return null;
}

export function confirmRecoveredRig(): boolean {
    recovered.confirmed = recovered.record !== null && recoveredRigProblem() === null;
    return recovered.confirmed;
}

export function correctRecoveredRig(
    change: Partial<Pick<RigReading, 'freqHz' | 'band' | 'adifMode' | 'subMode'>>
): void {
    const r = recovered.rig;
    if (r === null) return;
    recovered.confirmed = false;
    Object.assign(r, change);
    // The literal was captured for display; corrections use explicit ADIF
    // mode/submode inputs, so bring the literal into agreement with them.
    if (change.adifMode !== undefined || change.subMode !== undefined) {
        r.mode = r.adifMode;
    }
}
