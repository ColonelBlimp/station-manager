export const CALLSIGN_MIN = 3;
export const CALLSIGN_MAX = 20;
export const CALLSIGN_PATTERN = new RegExp(
    `^(?!\\/)(?!.*\\/$)(?!.*\\/\\/)(?=.*[A-Z])(?=.*\\d)[A-Z0-9/]{${CALLSIGN_MIN},${CALLSIGN_MAX}}$`
);

export function isValidCallsignLength(value: string): boolean {
    const trimmed = value.trim().toUpperCase();
    const len = trimmed.length;
    return len === 0 || (len >= CALLSIGN_MIN && len <= CALLSIGN_MAX);
}

export function isValidCallsignForLog(value: string): boolean {
    const trimmed = value.trim().toUpperCase();
    const len = trimmed.length;
    if (len < CALLSIGN_MIN || len > CALLSIGN_MAX) return false;
    return CALLSIGN_PATTERN.test(trimmed);
}
