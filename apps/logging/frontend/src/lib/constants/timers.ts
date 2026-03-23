/**
 * Application-wide timing and limit constants.
 * Centralizes magic numbers for maintainability.
 */

/** Interval in milliseconds for updating QSO time_off field (1 minute) */
export const QSO_TIMER_INTERVAL_MS = 60_000;

/** Default maximum number of items to keep in the clipboard history */
export const DEFAULT_CLIPBOARD_MAX_LENGTH = 10;

/** Interval in milliseconds for contest timers (1 second) */
export const CONTEST_TIMER_INTERVAL_MS = 1_000;
