/**
 * Time / date utility helpers.
 *
 * Ham QSOs are logged in UTC universally — formatters here always use
 * the UTC components of the Date, never local-timezone components.
 *
 * Output formats match the HTML5 input element value formats so the
 * results can be passed straight to `<input type="date" value=...>` and
 * `<input type="time" value=...>` without further translation:
 *
 *   formatUtcDate(d)  → "YYYY-MM-DD"
 *   formatUtcTime(d)  → "HH:MM"
 *
 * ADIF translation (`YYYY-MM-DD` → `YYYYMMDD`, `HH:MM` → `HHMM`) is the
 * submit-pipeline's responsibility and lives elsewhere — when we add
 * QSO submit, the ADIF translators will sit in this module too.
 */

const pad = (n: number): string => n.toString().padStart(2, '0');

export function formatUtcDate(d: Date): string {
    return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}

export function formatUtcTime(d: Date): string {
    return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

/**
 * Format a Date's UTC wall-clock as `HH:MM:SS`. Used for FT8 slot labels,
 * where the seconds component distinguishes the four 15-second slots within a
 * minute (:00 / :15 / :30 / :45).
 */
export function formatUtcClock(d: Date): string {
    return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
}

/**
 * Format a duration in milliseconds as `HH:MM:SS`. Hours can grow
 * past 99 — Field Day operating sessions, contest weekends, etc.
 * Negative or fractional inputs are floored to whole seconds; very
 * negative inputs clamp to 0 (a session that "started in the future"
 * is operator error, not something to render as `-00:01:00`).
 */
export function formatDurationHms(ms: number): string {
    const totalSeconds = Math.max(0, Math.floor(ms / 1000));
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
}

/**
 * The UTC calendar day after a `YYYY-MM-DD` string, in the same format. A blank
 * or unparseable input returns '' so a malformed date never fabricates a bogus
 * next-day value.
 */
export function nextUtcDate(date: string): string {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
    if (!m) return '';
    // Day + 1 with no mutation — Date.UTC normalises the overflow (e.g. Dec 32 →
    // Jan 1), which also keeps the no-mutable-Date lint rule happy.
    return formatUtcDate(new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + 1)));
}

/**
 * QSO_DATE_OFF (`YYYY-MM-DD`) — the date at TIME_OFF, given QSO_DATE and the
 * on/off times at MINUTE precision (`HH:MM`). Returns the day after qsoDate when
 * TIME_OFF is earlier than TIME_ON (the contact crossed UTC midnight), otherwise
 * qsoDate itself. The minute-precision rollover test mirrors the daemon's
 * coherence check, so a genuine midnight QSO carries a next-day QSO_DATE_OFF and
 * logs instead of bouncing with invalid_time_range. Shared by the live-log
 * submit path and the edit overlay so the two can't drift apart.
 */
export function deriveQsoDateOff(qsoDate: string, timeOnHHMM: string, timeOffHHMM: string): string {
    return timeOffHHMM < timeOnHHMM ? nextUtcDate(qsoDate) : qsoDate;
}
