// Contest mode for the Phone / CW card (W-0023 weekend slice). On: the card
// shows the exchange (Sent #, Rcvd #, District), committing a callsign
// allocates the sent serial, and the QSO is logged with CONTEST_ID = id and
// APP_SM_CONTEST_START = start — the occurrence: an annual event repeats its
// CONTEST_ID, so each edition (or a dry run under another date) is its own
// serial sequence and its own Cabrillo export.
// Remembered per browser — a convenience, so a reload mid-contest stays in
// contest mode; storage failure just starts it off.

import { UKEI_DISTRICTS } from './ukeiDistricts';

const KEY = 'sm.contest';

/** One supported contest occurrence: what the header's dropdown offers. */
export interface ContestOccurrence {
    id: string; // ADIF CONTEST_ID
    start: string; // the event's start date, YYYY-MM-DD (UTC)
    label: string;
    // What a blank Rcvd # is logged as, when the contest's rules give one.
    rcvdSerialDefault?: string;
    // The districts a received district must be one of ('--' = not copied).
    districts?: Readonly<Record<string, string>>;
}

// The supported contests (operator, 2026-10-31: chosen from a dropdown in the
// shell header). Dates from the sponsor's rules page as recorded in W-0023;
// the Cabrillo converter (scripts/ukeicc-cabrillo.py) supports these IDs.
export const CONTESTS: readonly ContestOccurrence[] = [
    // UK/EI rule 6: a received serial not copied is logged as zero and still
    // scores (operator, 2026-10-31: blank Rcvd # is logged as 0). Districts:
    // Appendix 2 (operator, 2026-10-31: no bogus district is logged).
    {
        id: 'UKEIDXSSB',
        start: '2026-10-31',
        label: 'UK/EI DX SSB — 31 Oct 2026',
        rcvdSerialDefault: '0',
        districts: UKEI_DISTRICTS,
    },
    {
        id: 'UKEIDXCW',
        start: '2027-04-24',
        label: 'UK/EI DX CW — 24 Apr 2027',
        rcvdSerialDefault: '0',
        districts: UKEI_DISTRICTS,
    },
    {
        id: 'UKEIDXSSB',
        start: '2027-10-23',
        label: 'UK/EI DX SSB — 23 Oct 2027',
        rcvdSerialDefault: '0',
        districts: UKEI_DISTRICTS,
    },
];

/** What a blank Rcvd # is logged as for this contest ID ('' when none). */
export function rcvdDefaultFor(id: string): string {
    return CONTESTS.find((c) => c.id === id)?.rcvdSerialDefault ?? '';
}

/** The district list for this contest ID, or null when it has none. */
export function districtsFor(id: string): Readonly<Record<string, string>> | null {
    return CONTESTS.find((c) => c.id === id)?.districts ?? null;
}

/** The name of a district of the contest chosen in the header ('' when not one). */
export function districtName(code: string): string {
    const list = districtsFor(contestId());
    const key = code.trim().toUpperCase();
    return list !== null && Object.hasOwn(list, key) ? list[key] : '';
}

/** The Rcvd # default of the contest chosen in the header. */
export function contestRcvdDefault(): string {
    return rcvdDefaultFor(contestId());
}

interface ContestSettings {
    on: boolean;
    id: string;
    start: string; // the event's start date, YYYY-MM-DD (UTC)
}

function load(): ContestSettings {
    const fallback = { on: false, id: CONTESTS[0].id, start: CONTESTS[0].start };
    let v: unknown;
    try {
        v = JSON.parse(localStorage.getItem(KEY) ?? 'null');
    } catch {
        return fallback; // No storage, or junk in it: start with contest mode off.
    }
    if (v === null || typeof v !== 'object') return fallback;
    const { on, id, start } = v as { on?: unknown; id?: unknown; start?: unknown };
    if (typeof on !== 'boolean') return fallback;
    // Only a supported occurrence is kept: the dropdown can show nothing else.
    const known = CONTESTS.some((c) => c.id === id && c.start === start);
    return known ? { on, id: id as string, start: start as string } : { ...fallback, on };
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
