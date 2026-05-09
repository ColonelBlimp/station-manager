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

const inRange =
    (min: number, max: number) =>
    (value: string): boolean => {
        const trimmed = value.trim();
        if (trimmed === '') {
            return true;
        }
        if (!DIGITS_ONLY.test(trimmed)) {
            return false;
        }
        const n = parseInt(trimmed, 10);
        return n >= min && n <= max;
    };

export const isValidCqZone = inRange(1, 40);
export const isValidItuZone = inRange(1, 90);
export const isValidDxcc = inRange(0, 522);
