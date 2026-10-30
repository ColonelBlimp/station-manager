// Contest mode for the Phone / CW card (W-0023 weekend slice). On: the card
// shows the exchange (Sent #, Rcvd #, District), committing a callsign
// allocates the sent serial, and the QSO is logged with CONTEST_ID = id and
// APP_SM_CONTEST_START = start — the occurrence: an annual event repeats its
// CONTEST_ID, so each edition (or a dry run under another date) is its own
// serial sequence and its own Cabrillo export.
// Remembered per browser — a convenience, so a reload mid-contest stays in
// contest mode; storage failure just starts it off.

const KEY = 'sm.contest';

interface ContestSettings {
    on: boolean;
    id: string;
    start: string; // the event's start date, YYYY-MM-DD (UTC)
}

function load(): ContestSettings {
    const fallback = { on: false, id: 'UKEIDXSSB', start: '' };
    let v: unknown;
    try {
        v = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    } catch {
        return fallback; // No storage, or junk in it: start with contest mode off.
    }
    if (v === null || typeof v !== 'object') return fallback;
    const { on, id, start } = v as { on?: unknown; id?: unknown; start?: unknown };
    if (typeof on !== 'boolean' || typeof id !== 'string') return fallback;
    return { on, id, start: typeof start === 'string' ? start : '' };
}

export const contest = $state(load());

// The daemon's rule for a contest ID (handler_contest_serial.go): it also heads
// the Cabrillo file, so no spaces.
const CONTEST_ID_RE = /^[A-Z0-9_-]{1,40}$/;

/** The CONTEST_ID as logged, upper case — '' unless it is a valid ID. */
export function contestId(): string {
    const id = contest.id.trim().toUpperCase();
    return CONTEST_ID_RE.test(id) ? id : '';
}

/** The occurrence as logged, ADIF Date YYYYMMDD — '' unless the start is a real date. */
export function contestStart(): string {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(contest.start.trim());
    if (m === null) return '';
    const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
    // Days in the month: day 0 of the next month is this month's last day.
    const days = (Date.UTC(y, mo, 0) - Date.UTC(y, mo - 1, 1)) / 86_400_000 + 1;
    return mo >= 1 && mo <= 12 && d >= 1 && d <= days ? m[1] + m[2] + m[3] : '';
}

$effect.root(() => {
    $effect(() => {
        const v = JSON.stringify({ on: contest.on, id: contest.id, start: contest.start });
        try {
            localStorage.setItem(KEY, v);
        } catch {
            // Remembering is a convenience only.
        }
    });
});
