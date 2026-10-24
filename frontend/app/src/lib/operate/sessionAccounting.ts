// Session export/email accounting (W-0021 "alpha.4 hold: fix design", revision 3,
// slice 5; Finding #6: B2-10's "Exported 4 QSOs" over a one-record file).
//
// An action snapshots its rows at the click, so a row logged, edited or emailed
// while the request is in flight changes neither what was sent nor what is said.
// The denominator is every snapshot row: the distinct UUIDs sent plus the rows
// with no QSO ID (never sent). A repeated UUID is sent once and disclosed as a
// collapsed duplicate, never as missing and never as delivered.
//
// What the daemon reports is accepted only when it accounts exactly for what was
// sent; otherwise the tab claims no success and marks nothing. "Could not be found
// in this archive" is the wording throughout: a missing row does not prove it was
// deleted.

export interface Selection {
    /** Distinct UUIDs in snapshot order: what the request sends. */
    uuids: string[];
    /** Snapshot rows without a QSO ID. */
    noId: number;
    /** Rows whose UUID repeats one already in the selection. */
    collapsed: number;
}

export interface Report {
    level: 'info' | 'warn';
    text: string;
}

export interface ExportCounts {
    requested: number;
    exported: number;
    omitted: number;
}

export interface EmailLists {
    sent: string[];
    omitted: string[];
    emailed: string[];
}

const DUPLICATES = 'Duplicate selections were counted once.';
const EXPORT_UNCONFIRMED =
    'The file was saved, but the daemon did not confirm how many QSOs it holds — check it before relying on it.';

export function snapshotSelection(rows: readonly { uuid?: string }[]): Selection {
    const uuids: string[] = [];
    let noId = 0;
    let collapsed = 0;
    for (const r of rows) {
        if (!r.uuid) noId++;
        else if (uuids.includes(r.uuid)) collapsed++;
        else uuids.push(r.uuid);
    }
    return { uuids, noId, collapsed };
}

const qsos = (n: number) => `${n} QSO${n === 1 ? '' : 's'}`;
const notFound = (n: number) => `${n} could not be found in this archive`;
const noIdPart = (n: number) => `${n} ${n === 1 ? 'has' : 'have'} no QSO ID`;

// Facts the tab knows without the daemon, appended as sentences.
function localNotes(sel: Selection, withNoId: boolean): string {
    const notes: string[] = [];
    if (withNoId && sel.noId > 0) notes.push(`${noIdPart(sel.noId)}.`);
    if (sel.collapsed > 0) notes.push(DUPLICATES);
    return notes.map((n) => ` ${n}`).join('');
}

const isCount = (n: number) => Number.isSafeInteger(n) && n >= 0;

function exportConfirmed(sel: Selection, c: ExportCounts | null): c is ExportCounts {
    return (
        c !== null &&
        isCount(c.requested) &&
        isCount(c.exported) &&
        isCount(c.omitted) &&
        c.requested === sel.uuids.length &&
        c.exported + c.omitted === c.requested &&
        c.exported >= 1
    );
}

export function exportReport(sel: Selection, counts: ExportCounts | null): Report {
    if (!exportConfirmed(sel, counts)) {
        return { level: 'warn', text: EXPORT_UNCONFIRMED + localNotes(sel, true) };
    }
    if (counts.omitted === 0 && sel.noId === 0) {
        const head = `Exported ${qsos(counts.exported)} to ADIF`;
        return { level: 'info', text: sel.collapsed > 0 ? `${head}. ${DUPLICATES}` : head };
    }
    const parts = [`Exported ${counts.exported} of ${sel.uuids.length + sel.noId} selected QSOs.`];
    if (counts.omitted > 0) parts.push(`${notFound(counts.omitted)}.`);
    return { level: 'warn', text: parts.join(' ') + localNotes(sel, true) };
}

const unique = (xs: string[]) => new Set(xs).size === xs.length;

// The exact partition revision 3 requires: length arithmetic alone would accept
// sent=[A,A], omitted=[B] for a request of A, B and C.
function partitionHolds(sel: Selection, l: EmailLists): boolean {
    if (!unique(l.sent) || !unique(l.omitted) || !unique(l.emailed)) return false;
    if (l.sent.length === 0) return false;
    const sent = new Set(l.sent);
    if (l.omitted.some((u) => sent.has(u))) return false;
    if (l.sent.length + l.omitted.length !== sel.uuids.length) return false;
    if (![...l.sent, ...l.omitted].every((u) => sel.uuids.includes(u))) return false;
    return l.emailed.every((u) => sent.has(u));
}

/** The toast for a sent email, and the UUIDs that may be marked emailed. */
export function emailReport(
    sel: Selection,
    to: string,
    lists: EmailLists | null
): Report & { mark: string[] } {
    if (lists === null || !partitionHolds(sel, lists)) {
        return {
            level: 'warn',
            text:
                `Email sent to ${to}, but the daemon did not confirm which QSOs it contained. ` +
                `No Session rows were marked as emailed.${localNotes(sel, true)}`,
            mark: [],
        };
    }
    const sent = lists.sent.length;
    const omitted = lists.omitted.length;
    const stamped = lists.emailed.length;
    const short = omitted > 0 || sel.noId > 0;
    const parts = [
        short
            ? `Sent ${sent} of ${sel.uuids.length + sel.noId} QSOs to ${to}`
            : `Sent ${qsos(sent)} to ${to}`,
    ];
    if (omitted > 0) parts.push(notFound(omitted));
    if (sel.noId > 0) parts.push(noIdPart(sel.noId));
    let text = parts.join('; ');
    if (stamped === 0) text += ', but none were recorded as emailed';
    else if (stamped < sent) {
        text += `, but only ${stamped} ${stamped === 1 ? 'was' : 'were'} recorded as emailed; the rest may be offered again`;
    }
    const complete = !short && stamped === sent;
    if (!complete) text += '.';
    if (sel.collapsed > 0) text += `${complete ? '.' : ''} ${DUPLICATES}`;
    return { level: complete ? 'info' : 'warn', text, mark: lists.emailed };
}
