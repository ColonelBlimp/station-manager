// Kind wording for the Station Events page (ADR 0010: the daemon ships stable
// codes, the client owns the words). One label per kind and a typed summary
// built ONLY from the fields the daemon's conversion table stores for that
// kind (W-0020 AC7); an unknown kind shows its code as the label, and an
// unknown or malformed detail degrades to a fixed placeholder — raw detail is
// never stringified onto the page.
import type { StationEvent } from '../api/stationEvents';

export const DETAILS_UNAVAILABLE = 'Details unavailable';

export function kindLabel(kind: string): string {
    switch (kind) {
        case 'export.adif_failed':
            return 'ADIF export failed';
        case 'forward.failed':
            return 'Upload failed';
        case 'archive.activated':
            return 'Archive switched';
        case 'archive.activation_failed':
            return 'Archive switch failed';
        case 'tx_alarm.raised':
            return 'TX alarm raised';
        case 'tx_alarm.cleared':
            return 'TX alarm cleared';
        case 'drive_alarm.raised':
            return 'Drive alarm raised';
        case 'drive_alarm.cleared':
            return 'Drive alarm recovered';
        case 'tx.disarmed':
            return 'TX disarmed automatically';
        case 'session.terminated':
            return 'Exchange ended by the daemon';
        default:
            return kind;
    }
}

export function categoryLabel(category: string): string {
    switch (category) {
        case 'notification':
            return 'Notification';
        case 'alarm':
            return 'Alarm';
        default:
            return category;
    }
}

// The TX-alarm codes (ADR 0051) and the daemon's session-end causes, in the
// operator's words. An unlisted identifier is shown as the daemon sent it: it is
// a bounded identifier the daemon minted, never free text.
function alarmCodeLabel(code: string): string {
    switch (code) {
        case 'tx_unconfirmed':
            return 'unkey not confirmed';
        case 'tx_still_keyed':
            return 'rig reported still keyed';
        case 'tx_liveness_lost':
            return 'CAT lost while keyed';
        case 'tx_teardown_unconfirmed':
            return 'unconfirmed at shutdown';
        case 'tx_key_write_failed':
            return 'key write failed';
        case 'drive_no_output':
            return 'no output from the rig';
        default:
            return code;
    }
}

function causeLabel(cause: string): string {
    switch (cause) {
        case 'unattended':
            return 'FT8 view disconnected';
        case 'cat_lost':
            return 'CAT connection lost';
        case 'dial_moved':
            return 'rig left the session frequency';
        case 'dial_unknown':
            return 'rig frequency unreadable';
        case 'tx_not_armed':
            return 'TX was not armed';
        case 'tx_bad_message':
            return 'message cannot be encoded';
        default:
            return cause;
    }
}

// The daemon's stable archive failure codes (internal/archive Fail*), worded
// for the events page; an unknown code shows as its code.
function archiveFailureLabel(code: string): string {
    switch (code) {
        case 'archive_file_missing':
            return 'the archive file is missing';
        case 'archive_file_unreadable':
            return 'the archive file could not be read';
        case 'archive_no_identity':
            return 'the file is not a Station Manager archive';
        case 'archive_identity_mismatch':
            return 'the file belongs to a different archive';
        case 'promotion_persist_failed':
            return 'the switch could not be recorded';
        case 'pending_unclear':
            return 'the restart could not be requested';
        case 'archive_start_failed':
            return 'the daemon could not start on it';
        default:
            return code;
    }
}

function stood(ms: number): string {
    return ms < 1000 ? `stood ${ms} ms` : `stood ${(ms / 1000).toFixed(1)} s`;
}

type Detail = Record<string, unknown>;

const str = (o: Detail, k: string): string | null => {
    const v = o[k];
    return typeof v === 'string' && v.trim() !== '' ? v : null;
};
const num = (o: Detail, k: string): number | null => {
    const v = o[k];
    return typeof v === 'number' && Number.isSafeInteger(v) && v >= 0 ? v : null;
};

// One summariser per kind, each reading ONLY that kind's stored fields and
// answering null when a field is missing or mistyped (→ the placeholder).
const summarisers: Record<string, (o: Detail) => string | null> = {
    'export.adif_failed': (o) => {
        const count = num(o, 'count');
        const outcome = str(o, 'outcome');
        if (count === null || count < 1 || outcome === null) return null;
        return `${count} QSO${count === 1 ? '' : 's'} · ${outcome}`;
    },
    'forward.failed': (o) => {
        const forwarder = str(o, 'forwarder');
        const action = str(o, 'action');
        const attempts = num(o, 'attempts');
        if (forwarder === null || action === null || attempts === null || attempts < 1) return null;
        return `${forwarder} · ${action} · ${attempts} attempt${attempts === 1 ? '' : 's'}`;
    },
    'archive.activated': (o) => str(o, 'label'),
    'archive.activation_failed': (o) => {
        const label = str(o, 'label');
        const code = str(o, 'code');
        if (label === null || code === null) return null;
        return `${label} · ${archiveFailureLabel(code)}`;
    },
    'tx_alarm.raised': codeOnly,
    'drive_alarm.raised': codeOnly,
    'drive_alarm.cleared': codeOnly,
    'tx_alarm.cleared': (o) => {
        const code = str(o, 'code');
        if (code === null) return null;
        if (!('active_ms' in o)) return alarmCodeLabel(code);
        const ms = num(o, 'active_ms');
        return ms === null ? null : `${alarmCodeLabel(code)} · ${stood(ms)}`;
    },
    'tx.disarmed': (o) => {
        const cause = str(o, 'cause');
        return cause === null ? null : causeLabel(cause);
    },
    'session.terminated': (o) => {
        const cause = str(o, 'cause');
        const partner = str(o, 'partner_call');
        const rung = str(o, 'rung');
        if (cause === null || partner === null || rung === null) return null;
        return `${partner} · ${causeLabel(cause)} · at ${rung}`;
    },
};

function codeOnly(o: Detail): string | null {
    const code = str(o, 'code');
    return code === null ? null : alarmCodeLabel(code);
}

export function detailSummary(ev: StationEvent): string {
    const d = ev.detail;
    if (!d || typeof d !== 'object') return DETAILS_UNAVAILABLE;
    const summarise = summarisers[ev.kind];
    if (summarise === undefined) return DETAILS_UNAVAILABLE;
    return summarise(d as Detail) ?? DETAILS_UNAVAILABLE;
}

export function severityDot(sev: string): string {
    switch (sev) {
        case 'error':
            return 'bg-red-500';
        case 'warn':
            return 'bg-amber-500';
        default:
            return 'bg-sky-500';
    }
}

export function fmtTime(iso: string): string {
    const t = new Date(iso);
    return isNaN(t.getTime()) ? iso : t.toLocaleString();
}
