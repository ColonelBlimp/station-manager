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
