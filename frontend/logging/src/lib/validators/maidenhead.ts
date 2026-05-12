/**
 * Maidenhead grid square validator.
 *
 * The Maidenhead Locator System encodes a position on Earth at three
 * resolutions:
 *   - 4 chars: AA00       — 2°×1° "field+square" (≈100×200 km)
 *   - 6 chars: AA00aa     — 5'×2.5' "subsquare"  (≈5×10 km, the most
 *                            common ham-radio resolution)
 *   - 8 chars: AA00aa00   — extended subsquare    (≈0.5×1 km)
 *
 * Field (chars 1-2): A-R uppercase  (18 zones each 20° wide, A starts at -180°)
 * Square (chars 3-4): 0-9            (10 zones each 2° / 1° wide)
 * Subsquare (chars 5-6): a-x lowercase by spec, but operators routinely
 *                        type uppercase; we case-fold before matching.
 * Extended (chars 7-8): 0-9
 *
 * Empty input passes — presence is a separate concern (matches the
 * other validators in this directory). Whitespace-only is treated as
 * empty.
 */
// daemon parity: internal/utils/maidenhead.go → maidenheadPattern.
// Both regexes are character-identical; keep them in step manually
// (no shared source). When updating one, search-replace the other.
const GRID_PATTERN = /^[A-R]{2}[0-9]{2}([A-X]{2}([0-9]{2})?)?$/;

export const isValidMaidenhead = (value: string): boolean => {
    const trimmed = value.trim().toUpperCase();
    if (trimmed === '') {
        return true;
    }
    return GRID_PATTERN.test(trimmed);
};
