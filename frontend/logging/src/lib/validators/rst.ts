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
