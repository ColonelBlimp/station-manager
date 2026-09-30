// The rig reading a saved draft carries (ADR 0085). An archive switch restarts
// the daemon, and the rig stream can reconnect and report the replacement
// daemon's reading before this tab learns of the switch — so the last reading
// before the FIRST loss of the rig connection is held, and neither later
// reports nor repeated loss callbacks replace it. A verified same-archive
// recovery retires it, so an unrelated later switch cannot reuse it — but
// only the recovery it verified: every loss advances an epoch, and a check
// retires only when no loss happened since it began (review 2026-09-30: a
// late identity answer retired a newer loss's reading).

import { rig } from './rig.svelte';
import { parseFrequency } from '../validators/frequency';
import { resolveModeAndSubmode } from '../utils/mode';
import { isoAt } from '../utils/time';

export interface RigReading {
    /** null when the frequency is unknown — never a default. */
    freqHz: number | null;
    band: string;
    /** The operator-facing literal (USB, CW-U); adifMode/subMode resolve it. */
    mode: string;
    adifMode: string;
    subMode: string;
    /** before-drop: the last reading before the rig connection was lost;
     *  when-saved: the reading at the moment the draft was saved. */
    basis: 'before-drop' | 'when-saved';
    capturedAt: string;
}

// $state.raw: the Phone / CW report default-fill reads whether a reading is
// held (a reconnect's mode report must not rewrite reports the held reading
// belongs with), so holding and retiring must be observable.
let held = $state.raw<RigReading | null>(null);
let epoch = 0;

function read(basis: RigReading['basis'], at: number): RigReading {
    const { mode, subMode } = resolveModeAndSubmode(rig.mode);
    return {
        freqHz: parseFrequency(rig.freq),
        band: rig.band,
        mode: rig.mode,
        adifMode: mode,
        subMode,
        basis,
        capturedAt: isoAt(at),
    };
}

/** The rig connection was lost: keep the reading from before it, once. */
export function noteRigDrop(at: number = Date.now()): void {
    epoch++;
    if (held === null) held = read('before-drop', at);
}

/** The loss epoch a recovery check starts from: read it BEFORE awaiting. */
export function rigDropEpoch(): number {
    return epoch;
}

/** A verified recovery on the same archive retires the held reading — only
 *  when no loss happened since the check began (`since` = rigDropEpoch()). */
export function retireRigSnapshot(since: number): void {
    if (since === epoch) held = null;
}

/** A reading from before a connection loss is held (not yet retired). */
export function rigSnapshotHeld(): boolean {
    return held !== null;
}

/** The reading to save with a draft: the held one, else the current one. */
export function rigReadingForSave(at: number = Date.now()): RigReading {
    return held ?? read('when-saved', at);
}

export function _resetRigSnapshotForTests(): void {
    held = null;
    epoch = 0;
}
