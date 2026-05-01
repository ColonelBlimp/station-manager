const CALLSIGN_MIN = 3;
const CALLSIGN_MAX = 20;
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
