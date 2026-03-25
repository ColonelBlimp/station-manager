// Strict RFC3339-like format with explicit timezone offset (e.g. 2025-12-02T10:31:04+02:00).
const REMOTE_TIME_REGEX = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})([+-])(\d{2}):(\d{2})$/;

export function getTimeUTC(): string {
    const data: Date = new Date();
    return (
        data.getUTCHours().toString().padStart(2, '0') +
        ':' +
        data.getUTCMinutes().toString().padStart(2, '0')
    );
}
export function getDateUTC(): string {
    const data: Date = new Date();
    return (
        data.getUTCFullYear().toString() +
        '-' +
        (data.getUTCMonth() + 1).toString().padStart(2, '0') +
        '-' +
        data.getUTCDate().toString().padStart(2, '0')
    );
}
export function extractRemoteTime(timestamp?: string): string {
    if (!timestamp) {
        return '';
    }

    const trimmed = timestamp.trim();
    const match = REMOTE_TIME_REGEX.exec(trimmed);
    if (!match) {
        return '';
    }

    const [, , , , hour, minute, second, , offsetHour, offsetMinute] = match;
    const hourNum = Number(hour);
    const minuteNum = Number(minute);
    const secondNum = Number(second);
    const offsetHourNum = Number(offsetHour);
    const offsetMinuteNum = Number(offsetMinute);
    if (
        Number.isNaN(hourNum) ||
        Number.isNaN(minuteNum) ||
        Number.isNaN(secondNum) ||
        Number.isNaN(offsetHourNum) ||
        Number.isNaN(offsetMinuteNum) ||
        hourNum > 23 ||
        minuteNum > 59 ||
        secondNum > 59 ||
        offsetHourNum > 14 ||
        offsetMinuteNum > 59
    ) {
        return '';
    }

    return `${hour}:${minute}`;
}

/**
 * Formats a provided time string in the format HHMM into a readable "HH:MM" format.
 *
 * @param {string | undefined} timeStr - A string representing time in "HHMM" format. If undefined, an empty string is returned.
 * @return {string} The formatted time string in "HH:MM" format. Returns an empty string if the input is undefined.
 * @throws Will throw an error if the provided string does not have a length of exactly 4 characters.
 */
export function formatTime(timeStr: string | undefined): string {
    if (timeStr === undefined) {
        return '';
    }
    if (timeStr.length !== 4) {
        throw new Error('Invalid time string length');
    }
    const hours: string = timeStr.slice(0, 2);
    const minutes: string = timeStr.slice(2, 4);
    return `${hours}:${minutes}`;
}

export function formatTimeSecondsToHHColonMMColonSS(seconds: number): string {
    const h = Math.floor(seconds / 3600)
        .toString()
        .padStart(2, '0');
    const m = Math.floor((seconds % 3600) / 60)
        .toString()
        .padStart(2, '0');
    const s = Math.floor(seconds % 60)
        .toString()
        .padStart(2, '0');
    return `${h}:${m}:${s}`;
}

/**
 * Formats a date string in the format 'YYYYMMDD' into 'YYYY-MM-DD'.
 *
 * @param {string | undefined} dateStr - The input date string in 'YYYYMMDD' format. If undefined, an empty string will be returned.
 * @return {string} The formatted date string in 'YYYY-MM-DD' format. Throws an error if the input string length is invalid.
 */
export function formatDate(dateStr: string | undefined): string {
    if (dateStr === undefined || dateStr.length !== 8) {
        return '';
    }
    const year: string = dateStr.slice(0, 4);
    const month: string = dateStr.slice(4, 6);
    const day: string = dateStr.slice(6, 8);
    return `${year}-${month}-${day}`;
}
