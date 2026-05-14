/*
    Zone-style range validators for the My Station panel: CQ Zone
    (1-40), ITU Zone (1-90), DXCC entity (0-522, where 0 = None /
    maritime per ARRL).

    Mirrors the daemon-side `isValidZone(s, min, max)` in
    `internal/api/validation.go` exactly: empty input passes
    (operator hasn't filled it in / cleared it), non-empty must
    parse as a non-negative base-10 integer in the closed range.

    Daemon parity is load-bearing — the SPA validator gives the
    operator a red border before they hit Update; the daemon's
    matching check is the authoritative backstop on PUT
    /v1/config. The two must agree on what "valid" means or
    operators see "looks fine here, rejected on save" or
    vice-versa.

    The 522 cap on DXCC is the current ARRL maximum at time of
    writing — bump when ARRL adds a new entity (rare; once every
    few years; the same comment lives on the daemon-side handler).
*/

const DIGITS_ONLY = /^[0-9]+$/;

/**
 * Build a range validator returning null when valid (including empty)
 * and `i18nKey` when the input is non-numeric or out of [min, max].
 * See `passthrough.ts` for the shared `string | null` contract.
 */
const inRange =
    (min: number, max: number, i18nKey: string) =>
    (value: string): string | null => {
        const trimmed = value.trim();
        if (trimmed === '') {
            return null;
        }
        if (!DIGITS_ONLY.test(trimmed)) {
            return i18nKey;
        }
        const n = parseInt(trimmed, 10);
        return n >= min && n <= max ? null : i18nKey;
    };

export const isValidCqZone = inRange(1, 40, 'validators.cq_zone');
export const isValidItuZone = inRange(1, 90, 'validators.itu_zone');
export const isValidDxcc = inRange(0, 522, 'validators.dxcc');
