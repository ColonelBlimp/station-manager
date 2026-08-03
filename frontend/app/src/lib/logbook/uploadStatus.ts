/*
    Per-QSO upload-status helpers for the logbook view (ADR 0039).

    "Uploaded to X?" keys on the QSO's durable ADIF upload-status stamp
    (<prefix>_qso_upload_status === "Y") — the SAME signal the daemon's
    missing_from filter and the enqueue skip-check use, so the colour, the
    filtered list, and what actually gets sent all agree.

    A forwarder is identified to the operator by its config NAME but stamps under
    a per-TYPE ADIF prefix, so we map type → stamp field. New forwarder types add
    one line here (matching the daemon's per-type stamp); a type with no entry is
    treated as "no stamp signal" and simply doesn't contribute to the colour.
*/

import type { LogbookQso } from '../api/logbooks';

/** One enabled forwarder, from /v1/config's masked forwarders block. */
export interface ForwarderInfo {
    name: string;
    /** Operator's config.json display name; '' means fall back to the name. */
    label: string;
    type: string;
    enabled: boolean;
}

/**
 * What to CALL a destination in front of the operator: their own label, else
 * its config name.
 *
 * Never its `name` when a label exists, and never blank — a destination whose
 * row reads empty looks like a rendering fault. Strictly a DISPLAY helper:
 * `name` remains the durable key everywhere it is sent (qso_upload's UNIQUE
 * constraint, the `missing_from` filter, POST /v1/forwarder/{name}/uploads), so
 * anything addressing the daemon must keep using it.
 *
 * One step shorter than Settings → Forwarding's chain (label → display_name →
 * type): the logbook does not fetch /v1/forwarder-types, so an unlabelled
 * destination shows "smcloud" here where Settings shows "SM Cloud backup".
 * Deliberate — a second endpoint fetch and its failure mode is a lot to carry
 * for a name the operator can set themselves.
 */
export function forwarderLabel(f: ForwarderInfo): string {
    return f.label.trim() || f.name;
}

/** type → the QSO JSON field carrying its ADIF upload-status stamp. */
const STAMP_FIELD: Record<string, string> = {
    qrz: 'qrzcom_qso_upload_status',
    clublog: 'clublog_qso_upload_status',
};

/**
 * Tri-state upload completeness of one QSO against the enabled forwarders (E):
 *   - 'complete' — uploaded to all of E
 *   - 'partial'  — uploaded to some but not all
 *   - 'none'     — uploaded to none
 *   - 'neutral'  — E is empty (or none of E has a known stamp field): the
 *                  light means nothing, so the callsign keeps its default colour
 */
export type UploadState = 'complete' | 'partial' | 'none' | 'neutral';

function stampField(type: string): string | undefined {
    return STAMP_FIELD[type];
}

/**
 * Whether this forwarder type records a per-QSO "uploaded" stamp — i.e. whether
 * "which QSOs are missing from it?" is a question the logbook row can answer.
 *
 * Not every destination can. SM Cloud is the case that surfaced this — a ROW
 * MIRROR holding a full copy rather than a derived record it extracts once, so it
 * deliberately registers no ADIF prefix — but "records no stamp" does not imply
 * mirroring (the dev stub registers no prefix and mirrors nothing). The daemon
 * rejects `missing_from` for any such type, so offering it as a filter is a
 * guaranteed 400 — exactly what picking "Not on smcloud" used to do (dogfood
 * 2026-07-27). Such a type remains a valid UPLOAD target; only the gap view is
 * undefined.
 */
export function hasUploadStamp(type: string): boolean {
    return stampField(type) !== undefined;
}

function isStamped(row: LogbookQso, type: string): boolean {
    const field = stampField(type);
    if (!field) return false;
    return (row as unknown as Record<string, string | undefined>)[field] === 'Y';
}

export function uploadState(row: LogbookQso, enabled: ForwarderInfo[]): UploadState {
    let known = 0;
    let on = 0;
    for (const f of enabled) {
        if (!stampField(f.type)) continue; // type stamps no status → can't judge
        known++;
        if (isStamped(row, f.type)) on++;
    }
    if (known === 0) return 'neutral';
    if (on === known) return 'complete';
    if (on === 0) return 'none';
    return 'partial';
}

/** Human tooltip naming which destinations the QSO is on / still missing. */
export function uploadTooltip(row: LogbookQso, enabled: ForwarderInfo[]): string {
    const onList: string[] = [];
    const missingList: string[] = [];
    for (const f of enabled) {
        if (!stampField(f.type)) continue;
        (isStamped(row, f.type) ? onList : missingList).push(forwarderLabel(f));
    }
    if (onList.length === 0 && missingList.length === 0) return '';
    const parts: string[] = [];
    if (onList.length > 0) parts.push(`On: ${onList.join(', ')}`);
    if (missingList.length > 0) parts.push(`Missing: ${missingList.join(', ')}`);
    return parts.join(' · ');
}

/** Tailwind text-colour class for the callsign tint — theme-aware (the app
 *  shell renders in light AND dark; the shipping SPA's light-only 800-weight
 *  tints vanish on a dark surface). */
export function uploadColorClass(state: UploadState): string {
    switch (state) {
        case 'complete':
            return 'text-green-700 dark:text-green-400';
        case 'partial':
            return 'text-amber-600 dark:text-amber-400';
        case 'none':
            return 'text-red-700 dark:text-red-400';
        case 'neutral':
            return ''; // default text colour — light is meaningless with no E
    }
}
