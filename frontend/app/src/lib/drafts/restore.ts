// Whether a saved QSO may be restored here (ADR 0085 RS3–RS7; operator rulings
// 2026-10-01). Pure, so every reason is tested. The Saved QSOs panel reads it
// to offer Restore; restoreSession applies it again when Restore is pressed.

import type { SubmitAttribution } from '../api/submit-attribution';
import type { SavedDraft } from './savedDraft';

export interface RestoreEnv {
    /** The browser offers Web Locks (a secure page): one tab can own a record. */
    locksAvailable: boolean;
    /** This page is Phone / CW, where Restore acts. */
    onPhoneCw: boolean;
    /** The archive this page's stores were proven against; null when unproven. */
    bootArchiveId: string | null;
    /** The active default logbook's UUID. */
    activeLogbookUuid: string;
    /** Today's GET /v1/submit-attribution; null when it could not be read. */
    currentAttribution: SubmitAttribution | null;
}

export type RestoreEligibility =
    | { kind: 'eligible' }
    | { kind: 'logged' }
    | { kind: 'unavailable'; reason: string }
    | { kind: 'my-rig-changed'; saved: string; current: string }
    | { kind: 'go-to-phone-cw' };

export function restoreEligibility(r: SavedDraft, env: RestoreEnv): RestoreEligibility {
    if (r.state === 'logged') return { kind: 'logged' };
    if (!env.locksAvailable) {
        return {
            kind: 'unavailable',
            reason: 'This page cannot reserve the QSO for one tab, so it is read and copy only here.',
        };
    }
    if (r.attribution === null) {
        return { kind: 'unavailable', reason: 'Original attribution unavailable.' };
    }
    // The source, by UUID: a numeric logbook id is reused across archives.
    if (env.bootArchiveId !== r.archiveId || env.activeLogbookUuid !== r.logbookUuid) {
        return {
            kind: 'unavailable',
            reason: `It belongs to ‘${r.archiveLabel}’ (logbook ‘${r.logbookName}’), which is not the archive and logbook in use here.`,
        };
    }
    // MY_RIG is stamped by the server and must match today's stamp exactly.
    // OPERATOR and MY_NAME are not compared with today's: Restore supplies the
    // saved ones explicitly and the server checks all three on submit.
    if (env.currentAttribution === null) {
        return {
            kind: 'unavailable',
            reason: 'The rig attribution in use now could not be read.',
        };
    }
    if (env.currentAttribution.myRig !== r.attribution.myRig) {
        return {
            kind: 'my-rig-changed',
            saved: r.attribution.myRig,
            current: env.currentAttribution.myRig,
        };
    }
    if (!env.onPhoneCw) return { kind: 'go-to-phone-cw' };
    return { kind: 'eligible' };
}
