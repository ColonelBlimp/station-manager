export const BandNames = [
    '160m',
    '80m',
    '60m',
    '40m',
    '30m',
    '20m',
    '17m',
    '15m',
    '12m',
    '10m',
    '6m',
] as const;

export type BandName = (typeof BandNames)[number];

// Minimum CAT frequency string length (Yaesu-style: 9 digits kHz, zero-padded)
export const FREQ_MIN_CAT_LENGTH = 9;

interface BandRange {
    band: BandName;
    minMHz: number; // inclusive
    maxMHz: number; // inclusive
}

// ADIF-like amateur band ranges in MHz (simplified)
const BAND_RANGES: BandRange[] = [
    { band: '160m', minMHz: 1.8, maxMHz: 2.0 },
    { band: '80m', minMHz: 3.5, maxMHz: 4.0 },
    { band: '60m', minMHz: 5.0, maxMHz: 5.5 },
    { band: '40m', minMHz: 7.0, maxMHz: 7.3 },
    { band: '30m', minMHz: 10.1, maxMHz: 10.15 },
    { band: '20m', minMHz: 14.0, maxMHz: 14.35 },
    { band: '17m', minMHz: 18.068, maxMHz: 18.168 },
    { band: '15m', minMHz: 21.0, maxMHz: 21.45 },
    { band: '12m', minMHz: 24.89, maxMHz: 24.99 },
    { band: '10m', minMHz: 28.0, maxMHz: 29.7 },
    { band: '6m', minMHz: 50.0, maxMHz: 54.0 },
];

/**
 * Parse a CAT frequency string (kHz, 9-digit, zero-padded) into MHz.
 *
 * CAT format we expect here is a fixed-width numeric string where the value
 * represents kHz. For example:
 *   "007101000" -> 7,101,000 kHz -> 7.101 MHz
 */
export function parseCatKHzToMHz(freqKHz: string | null | undefined): number | null {
    if (!freqKHz) return null;
    const trimmed = freqKHz.trim();
    if (trimmed.length < FREQ_MIN_CAT_LENGTH) return null;
    if (!/^\d+$/.test(trimmed)) return null;

    const value = Number(trimmed);
    if (!Number.isFinite(value) || value <= 0) return null;

    // Value is kHz; divide by 1_000 to get MHz.
    // because value is actually kHz * 1000
    return value / 1_000_000;
}

export function parseDatabaseFreqToDottedKhz(freq: string | undefined): string {
    // Format validated plain kHz string into dotted thousands of groups for UI.
    if (!freq) return '';
    const trimmed = freq.trim();
    if (!/^[0-9]+$/.test(trimmed)) return '';
    if (trimmed.length < 7 || trimmed.length > 8) return '';
    const value = Number(trimmed);
    if (!Number.isFinite(value) || value <= 0) return '';

    const chars = trimmed.split('');
    const parts: string[] = [];
    while (chars.length > 3) {
        parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    }
    if (chars.length) {
        parts.unshift(chars.join(''));
    }

    return parts.join('.');
}

/**
 * Parse a user-entered frequency string in MHz into a number.
 */
export function parseUserMHz(freqMHzStr: string | null | undefined): number | null {
    if (!freqMHzStr) return null;
    const normalized = freqMHzStr.trim().replace(',', '.');
    if (!normalized) return null;

    const value = Number(normalized);
    if (!Number.isFinite(value) || value <= 0) return null;

    // Optionally clamp to a reasonable upper bound to catch obvious typos.
    if (value > 1000) return null;

    return value;
}

/**
 * Parse a "dotted" MHz string like `7.200.000` or `14.320.000` into a numeric MHz value.
 * Dots are treated as group separators, not decimal separators; the value is assumed
 * to have 6 fractional digits in MHz (i.e. units of 1 Hz).
 */
function parseDottedMHz(freqStr: string | null | undefined): number | null {
    if (freqStr == null) return null;
    const trimmed = freqStr.trim();
    if (!trimmed) return null;

    // Require digits separated by single dots, no leading/trailing dot.
    if (!/^[0-9]+(?:\.[0-9]+)*$/.test(trimmed)) return null;

    const compact = trimmed.replace(/\./g, '');
    if (!compact || !/^\d+$/.test(compact)) return null;

    const raw = Number(compact);
    if (!Number.isFinite(raw)) return null;

    const valueMHz = raw / 1_000_000; // convert from Hz-like integer to MHz

    if (valueMHz <= 0) return null;
    if (valueMHz > 1000) return null;

    return valueMHz;
}

/**
 * Parse a "dotted" kHz string like `7.101.000` (meaning 7,101.000 kHz)
 * into a numeric MHz value.
 *
 * Implementation detail:
 * - We treat the dotted string as a Hz-like integer with 3 extra decimal
 *   places (kHz) compared to MHz. For example:
 *   "7.101.000" -> 7101000 -> 7.101 MHz.
 */
export function parseDottedKHzToMHz(freqStr: string | null | undefined): number | null {
    if (freqStr == null) return null;
    const trimmed = freqStr.trim();
    if (!trimmed) return null;

    if (!/^[0-9]+(?:\.[0-9]+)*$/.test(trimmed)) return null;

    const compact = trimmed.replace(/\./g, '');
    if (!/^[0-9]+$/.test(compact)) return null;

    const raw = Number(compact);
    if (!Number.isFinite(raw) || raw <= 0) return null;

    // raw is effectively kHz * 1000 -> convert to MHz.
    return raw / 1_000_000;
}

/**
 * Core band resolver that works on numeric MHz values.
 */
export function frequencyToBandMHz(freqMHz: number | null | undefined): BandName | '' {
    if (freqMHz == null || !Number.isFinite(freqMHz)) return '';

    for (const range of BAND_RANGES) {
        if (freqMHz >= range.minMHz && freqMHz <= range.maxMHz) {
            return range.band;
        }
    }

    return '';
}

/** Convenience: map CAT kHz string (9-digit) to a band name. */
export function frequencyToBandFromCat(freqKHz: string | null | undefined): BandName | '' {
    const mhz = parseCatKHzToMHz(freqKHz);
    return frequencyToBandMHz(mhz);
}

/** Convenience: map user MHz string to a band name. */
export function frequencyToBandFromMHz(freqMHzStr: string | null | undefined): BandName | '' {
    const mhz = parseUserMHz(freqMHzStr);
    return frequencyToBandMHz(mhz);
}

/** Convenience: map dotted MHz string (e.g. `7.200.000`) to a band name. */
export function frequencyToBandFromDottedMHz(freqStr: string | null | undefined): BandName | '' {
    const mhz = parseDottedMHz(freqStr);
    return frequencyToBandMHz(mhz);
}

/**
 * Format a CAT frequency string (kHz, typically 9-digit, zero-padded) into a
 * dotted MHz string suitable for display, e.g.:
 *   "014320000" -> "14.320.000"
 *
 * Rules/assumptions:
 * - Input must be a string of digits, at least FREQ_MIN_LENGTH long.
 * - We interpret the value as kHz and convert to MHz with three decimal places,
 *   then format as groups of 3 digits separated by dots (thousands-style).
 * - Returns an empty string on invalid input.
 */
export function formatCatKHzToDottedMHz(freqKHz: string | null | undefined): string {
    const mhz = parseCatKHzToMHz(freqKHz);
    if (mhz == null) return '';

    // Represent MHz with six fractional digits so we have full kHz + Hz precision
    // before grouping (e.g. 7.101234 MHz -> "7.101234").
    const mhzStr = mhz.toFixed(6);
    const [intPart, fracPartRaw] = mhzStr.split('.');
    const fracPart = (fracPartRaw ?? '').padEnd(6, '0').slice(0, 6); // exactly 6 digits

    // Build a continuous string like "7" + "101234" = "7101234" and then
    // split into MHz.kHz.Hz groups: 1-3 digits for MHz, 3 for kHz, 3 for Hz.
    const base = intPart + fracPart; // e.g. "7" + "101000" -> "7101000"
    const chars = base.split('');
    const parts: string[] = [];

    // Last 3 digits: Hz-level group
    parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    // Next 3 digits: kHz-level group
    parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    // Remaining: MHz-level group
    if (chars.length) {
        parts.unshift(chars.join(''));
    }

    return parts.join('.');
}

/**
 * Format a dotted kHz string (e.g. `7.101.000`) as a normalised dotted MHz
 * string. This is essentially a thin wrapper over `parseDottedKHzToMHz`
 * combined with the same grouping logic used for CAT kHz values.
 */
export function formatDottedKHzToDottedMHz(freqStr: string | null | undefined): string {
    const mhz = parseDottedKHzToMHz(freqStr);
    if (mhz == null) return '';

    const mhzStr = mhz.toFixed(6);
    const [intPart, fracPartRaw] = mhzStr.split('.');
    const fracPart = (fracPartRaw ?? '').padEnd(6, '0').slice(0, 6);

    const base = intPart + fracPart;
    const chars = base.split('');
    const parts: string[] = [];

    parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    if (chars.length) {
        parts.unshift(chars.join(''));
    }

    return parts.join('.');
}

/**
 * Convert a dotted kHz-style string (e.g. "14.320.000") into a short MHz
 * string (e.g. "14.320"). Returns an empty string for invalid input.
 */
export function dottedKHzToShortMHz(freqStr: string | null | undefined): string {
    const mhz = parseDottedKHzToMHz(freqStr);
    if (mhz == null) return '';
    // 3 decimal places is typical for amateur HF MHz representation.
    return mhz.toFixed(3).replace(/\.0+$/, '.0').replace(/\.0$/, '');
}

/**
 * Convert a raw frequency string from the database (plain kHz, no padding or dots)
 * into a dotted MHz string suitable for UI display, e.g. "14320" -> "14.320".
 * Returns an empty string when the value is missing or invalid.
 */
export function rawKHzStringToDottedMHz(freqStr: string | null | undefined): string {
    if (freqStr == null) return '';
    const trimmed = freqStr.trim();
    if (!trimmed) return '';
    if (!/^\d+$/.test(trimmed)) return '';

    const value = Number(trimmed);
    if (!Number.isFinite(value) || value <= 0) return '';

    const mhz = value / 1_000;
    if (mhz > 1000) return '';
    if (mhz < 1e-3) return '';

    const mhzStr = mhz.toFixed(6);
    const [intPart, fracPartRaw] = mhzStr.split('.');
    const fracPart = (fracPartRaw ?? '').padEnd(6, '0').slice(0, 6);

    const base = intPart + fracPart;
    const chars = base.split('');
    const parts: string[] = [];

    parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    parts.unshift(chars.splice(chars.length - 3, 3).join(''));
    if (chars.length) {
        parts.unshift(chars.join(''));
    }

    return parts.join('.').replace(/\.0+$/, '.0').replace(/\.0$/, '');
}
