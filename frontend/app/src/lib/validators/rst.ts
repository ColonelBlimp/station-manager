// The RST scale, not just the shape (tightened over the shipping SPA's
// any-2-3-digits pattern, which let 77 and 000 through — operator catch
// 2026-07-08): Readability 1–5, Strength 1–9, Tone 1–9. Zero is invalid in
// every position. The Tone digit only exists on CW (operator ruling, same
// day), so there are two validators: RST (CW — tone optional, ops give
// both 59 and 599) and RS (everything else voice/digital — exactly two
// digits; 599 on USB is as wrong as 77). draftProblems picks by rig mode.
const RST_PATTERN = /^[1-5][1-9][1-9]?$/;
const RS_PATTERN = /^[1-5][1-9]$/;

/**
 * CW report: R 1–5, S 1–9, optional Tone 1–9. Returns null when valid
 * (including empty) and the i18n key `'validators.rst'` otherwise. See
 * `passthrough.ts` for the shared `string | null` contract.
 */
export const isValidRst = (value: string): string | null => {
    const trimmed = value.trim();
    if (trimmed === '') {
        return null;
    }
    return RST_PATTERN.test(trimmed) ? null : 'validators.rst';
};

/**
 * Voice/non-CW-digital report: R 1–5, S 1–9 — no tone digit. Returns null
 * when valid (including empty) and the i18n key `'validators.rs'` otherwise.
 */
export const isValidRs = (value: string): string | null => {
    const trimmed = value.trim();
    if (trimmed === '') {
        return null;
    }
    return RS_PATTERN.test(trimmed) ? null : 'validators.rs';
};

// Signed dB SNR — optional sign + 1–2 digits, e.g. "-12", "+04", "0".
// WSJT-X reports fall well within ±50 dB, so two digits is ample.
const SIGNAL_REPORT_PATTERN = /^[+-]?[0-9]{1,2}$/;

/**
 * Report validator for the WSJT-X-family weak-signal digital modes
 * (FT8/FT4/JT*), which report a signed dB SNR rather than RST digits.
 * Returns null when valid (including empty) and the i18n key
 * `'validators.signalReport'` otherwise. Empty passes — presence is a
 * form-layer concern, as with `isValidRst`.
 */
export const isValidSignalReport = (value: string): string | null => {
    const trimmed = value.trim();
    if (trimmed === '') {
        return null;
    }
    return SIGNAL_REPORT_PATTERN.test(trimmed) ? null : 'validators.signalReport';
};
