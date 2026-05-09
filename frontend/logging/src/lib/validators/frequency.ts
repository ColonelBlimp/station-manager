/**
 * Frequency input validation and parsing.
 *
 * Two accepted input formats — both unambiguously map to a Hz value:
 *
 * 1. **Display format** (matches what `Vfos` renders): MHz.kHz.Hz with
 *    periods as separators. Examples:
 *      `"14.250.000"` → 14_250_000 Hz
 *      `"7.000.500"`  → 7_000_500 Hz
 *    Each segment is 1–3 digits except the leading MHz which is 1–5.
 *
 * 2. **Decimal MHz**: a single number, optionally with a decimal part.
 *    The fractional part is interpreted as the post-MHz position so
 *    `"14.250"` = 14.250 MHz = 14_250_000 Hz.
 *      `"14"`         → 14_000_000 Hz
 *      `"14.25"`      → 14_250_000 Hz
 *      `"14.250"`     → 14_250_000 Hz
 *      `"14.123456"`  → 14_123_456 Hz (microHz precision in MHz space)
 *
 * Anything else returns null/false. In particular, leading-period
 * (`".250"`), trailing-period (`"14."`), and 4+ period forms reject.
 *
 * Range is checked: 100 kHz to 30 GHz. Generous enough to cover all
 * amateur allocations without forcing a band-aware check at the
 * validator layer (band classification is handled separately in
 * `Vfos.svelte`'s `frequencyToBand`, and out-of-band frequencies are
 * still well-formed input — the user might intentionally tune outside
 * a band edge for receiving).
 *
 * Empty and whitespace-only strings return `true`/non-null per the
 * project's validator-presence convention (settled 2026-05-01) — the
 * validator has no objection to absence; required-ness is enforced
 * at the form layer.
 */

const FREQ_MIN_HZ = 100_000; // 100 kHz
const FREQ_MAX_HZ = 30_000_000_000; // 30 GHz

const DISPLAY_PATTERN = /^(\d{1,5})\.(\d{1,3})\.(\d{1,3})$/;
const DECIMAL_PATTERN = /^(\d{1,5})(?:\.(\d{1,6}))?$/;

function inRange(hz: number): boolean {
    return hz >= FREQ_MIN_HZ && hz <= FREQ_MAX_HZ;
}

export function parseFrequency(value: string): number | null {
    const trimmed = value.trim();
    if (trimmed === '') return null;

    const display = DISPLAY_PATTERN.exec(trimmed);
    if (display) {
        const mhz = parseInt(display[1], 10);
        const khz = parseInt(display[2], 10);
        const hz = parseInt(display[3], 10);
        if (khz > 999 || hz > 999) return null;
        const total = mhz * 1_000_000 + khz * 1_000 + hz;
        return inRange(total) ? total : null;
    }

    const decimal = DECIMAL_PATTERN.exec(trimmed);
    if (decimal) {
        const mhz = parseInt(decimal[1], 10);
        const fracStr = decimal[2] ?? '';
        const fracPadded = fracStr.padEnd(6, '0').slice(0, 6);
        const frac = parseInt(fracPadded, 10) || 0;
        const total = mhz * 1_000_000 + frac;
        return inRange(total) ? total : null;
    }

    return null;
}

export function isValidFrequency(value: string): boolean {
    if (value.trim() === '') return true;
    return parseFrequency(value) !== null;
}
