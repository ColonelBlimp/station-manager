const RST_PATTERN = /^[0-9]{2,3}$/;

/**
 * Returns null when valid (including empty) and the i18n key
 * `'validators.rst'` when not 2-or-3 digits. See `passthrough.ts` for
 * the shared `string | null` contract.
 */
export const isValidRst = (value: string): string | null => {
    const trimmed = value.trim();
    if (trimmed === '') {
        return null;
    }
    return RST_PATTERN.test(trimmed) ? null : 'validators.rst';
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
