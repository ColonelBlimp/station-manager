// daemon parity: internal/qsoservice/validation.go → IsValidCallsign
// (3-32 chars, must contain a digit, ASCII letters/digits plus '/').
// Length and digit-presence match exactly. The SPA pattern is
// intentionally stricter on the character set: the daemon accepts '-'
// (occasionally seen in special-event calls), the SPA rejects it
// because operator workflow today is /-suffix portable/mobile only,
// and a typo'd dash should surface as a red border rather than silently
// log. Revisit if a special-event call is rejected by red-border in
// practice.
const CALLSIGN_MIN = 3;
const CALLSIGN_MAX = 32;
const CALLSIGN_PATTERN = new RegExp(
    `^(?!\\/)(?!.*\\/$)(?!.*\\/\\/)(?=.*[A-Z])(?=.*\\d)[A-Z0-9/]{${CALLSIGN_MIN},${CALLSIGN_MAX}}$`
);

export const isValidCallsign = (value: string): boolean => {
    const trimmed = value.trim().toUpperCase();
    if (trimmed === '') {
        return true;
    }
    const len = trimmed.length;
    if (len < CALLSIGN_MIN || len > CALLSIGN_MAX) {
        return false;
    }
    return CALLSIGN_PATTERN.test(trimmed);
};
