// Contest mode for the Phone / CW card (W-0023 weekend slice). On: the card
// shows the exchange (Sent #, Rcvd #, District), committing a callsign
// allocates the sent serial, and the QSO is logged with CONTEST_ID = id and
// APP_SM_CONTEST_START = start — the occurrence: an annual event repeats its
// CONTEST_ID, so each edition (or a dry run under another date) is its own
// serial sequence and its own Cabrillo export.
// Remembered per browser — a convenience, so a reload mid-contest stays in
// contest mode; storage failure just starts it off.

import { UKEI_DISTRICTS } from './ukeiDistricts';
import { rig } from './rig.svelte';
import { parseFrequency } from '../validators/frequency';
import { frequencyToBand } from '../utils/frequency';
import { resolveModeAndSubmode } from '../utils/mode';

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
    // The event's mode (ADIF MODE family) and bands: each band the contest
    // uses, with its required segments in kHz, or null where the whole band
    // may be used. The UI warns outside them.
    mode?: 'SSB' | 'CW';
    bands?: Readonly<Record<string, ReadonlyArray<readonly [number, number]> | null>>;
    // When it runs and the off-time rules the header timers count by.
    timing?: ContestTiming;
}

export interface ContestTiming {
    startUtc: string; // HH:MM on the start date
    hours: number;
    offTimeMinutes: number; // a gap this long is an off period
    maxOperatingMinutes: number; // the limited-time category's operating time
}

// UK/EI rule 2: every contest runs from 1200z Saturday to 1200z Sunday; rule
// 4.4(b): in the 12-hour category each off period is at least 60 minutes.
const UKEI_TIMING: ContestTiming = {
    startUtc: '12:00',
    hours: 24,
    offTimeMinutes: 60,
    maxOperatingMinutes: 12 * 60,
};

// UK/EI rule 3 (the page, read 2026-10-31): 3.5, 7, 14, 21 and 28 MHz;
// "contest-preferred segments must be observed" — on 80 m and 20 m, SSB
// 3600-3650, 3700-3800 and 14125-14300 kHz, CW 3510-3560 and 14000-14060 kHz.
// Rule 9.2: operating outside the published band segments, all QSOs disallowed.
const UKEI_SSB_BANDS = {
    '80m': [
        [3600, 3650],
        [3700, 3800],
    ],
    '40m': null,
    '20m': [[14125, 14300]],
    '15m': null,
    '10m': null,
} as const;
const UKEI_CW_BANDS = {
    '80m': [[3510, 3560]],
    '40m': null,
    '20m': [[14000, 14060]],
    '15m': null,
    '10m': null,
} as const;

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
        timing: UKEI_TIMING,
        mode: 'SSB',
        bands: UKEI_SSB_BANDS,
    },
    {
        id: 'UKEIDXCW',
        start: '2027-04-24',
        label: 'UK/EI DX CW — 24 Apr 2027',
        rcvdSerialDefault: '0',
        districts: UKEI_DISTRICTS,
        timing: UKEI_TIMING,
        mode: 'CW',
        bands: UKEI_CW_BANDS,
    },
    {
        id: 'UKEIDXSSB',
        start: '2027-10-23',
        label: 'UK/EI DX SSB — 23 Oct 2027',
        rcvdSerialDefault: '0',
        districts: UKEI_DISTRICTS,
        timing: UKEI_TIMING,
        mode: 'SSB',
        bands: UKEI_SSB_BANDS,
    },
];

/** What a blank Rcvd # is logged as for this contest ID ('' when none). */
export function rcvdDefaultFor(id: string): string {
    return CONTESTS.find((c) => c.id === id)?.rcvdSerialDefault ?? '';
}

/** A contest the catalogue supports — the only ones whose exchange the app checks. */
export function isSupportedContest(id: string): boolean {
    return CONTESTS.some((c) => c.id === id);
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

/** The contest chosen in the header, from the catalogue. */
function chosenContest(): ContestOccurrence | undefined {
    return CONTESTS.find((c) => c.id === contestId());
}

const mhz = (khz: number): string => (khz / 1000).toFixed(3);

/** A breach of the contest's rules: its kind (stable while the operator
 *  retunes within it — the segment on a band, a band, a mode) and its text. */
export interface ContestRuleProblem {
    key: string;
    message: string;
}

/** In contest mode, how the rig is outside the contest's rules — a band it
 *  does not use, outside the sponsor's segments, or the wrong mode — or null.
 *  A warning only: it never blocks logging. Silent while the frequency is
 *  unknown. */
export function contestRuleProblem(): ContestRuleProblem | null {
    const c = chosenContest();
    if (!contest.on || c?.bands === undefined) return null;
    const hz = parseFrequency(rig.freq);
    if (hz === null) return null;
    const name = c.label.split(' — ')[0];
    // The band comes from the frequency, not rig.band: the rig keeps its last
    // band when tuned outside every amateur band (Codex review 92dacbe8).
    const band = frequencyToBand(hz);
    if (!Object.hasOwn(c.bands, band)) {
        const list = Object.keys(c.bands).map((b) => b.replace('m', ''));
        const bands = `${list.slice(0, -1).join(', ')} and ${list[list.length - 1]} m`;
        return {
            key: `band:${band || 'none'}`,
            message:
                band !== ''
                    ? `${band} is not a ${name} band (${bands}).`
                    : `${mhz(hz / 1000)} is outside the amateur bands — not a ${name} band (${bands}).`,
        };
    }
    const segments = c.bands[band];
    const khz = hz / 1000;
    if (segments !== null && !segments.some(([lo, hi]) => khz >= lo && khz <= hi)) {
        const ranges = segments.map(([lo, hi]) => `${mhz(lo)}–${mhz(hi)}`).join(', ');
        return {
            key: `segment:${band}`,
            message: `${mhz(khz)} is outside the ${name} segment ${ranges} — rule 9.2: all QSOs disallowed.`,
        };
    }
    if (c.mode !== undefined && resolveModeAndSubmode(rig.mode).mode !== c.mode) {
        return {
            key: `mode:${rig.mode}`,
            message: `${rig.mode} is not this contest's mode (${c.mode}).`,
        };
    }
    return null;
}

/** The text of contestRuleProblem(), or null. */
export function contestRuleWarning(): string | null {
    return contestRuleProblem()?.message ?? null;
}

/** The chosen contest's window and off-time rules, or null when it has none. */
export function contestWindow(): {
    startMs: number;
    endMs: number;
    offMin: number;
    maxOpMin: number;
} | null {
    const c = chosenContest();
    const start = contestStart();
    if (c?.timing === undefined || start === '') return null;
    const [h, m] = c.timing.startUtc.split(':').map(Number);
    const startMs = Date.UTC(+start.slice(0, 4), +start.slice(4, 6) - 1, +start.slice(6, 8), h, m);
    return {
        startMs,
        endMs: startMs + c.timing.hours * 3_600_000,
        offMin: c.timing.offTimeMinutes,
        maxOpMin: c.timing.maxOperatingMinutes,
    };
}
