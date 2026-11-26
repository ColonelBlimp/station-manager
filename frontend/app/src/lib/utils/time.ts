const pad = (n: number): string => n.toString().padStart(2, '0');

/**
 * Format a duration (milliseconds) as HH:MM:SS. Hours are zero-padded to two
 * digits but allowed to grow past 99 — Field Day operating sessions, contest
 * weekends, etc. Negative or fractional inputs are floored to whole seconds;
 * very negative inputs clamp to 0 (a session that "started in the future" is
 * operator error, not something to render as `-00:01:00`).
 *
 * Ported verbatim from the logging SPA (`frontend/logging` utils/time) for the
 * SessionTimer.
 */
export function formatDurationHms(ms: number): string {
    const totalSeconds = Math.max(0, Math.floor(ms / 1000));
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
}

/**
 * When a session started, as the Session panel says it (W-0029 part 6, operator
 * ruling 2026-11-25): "09:15 UTC", with the date in front when the start is not
 * today's in UTC ("2026-11-20 09:15 UTC"). '' for a start that cannot be read.
 */
export function formatSessionStart(iso: string, now: Date = new Date()): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '';
    const day = d.toISOString().slice(0, 10);
    const time = `${d.toISOString().slice(11, 16)} UTC`;
    return day === now.toISOString().slice(0, 10) ? time : `${day} ${time}`;
}
