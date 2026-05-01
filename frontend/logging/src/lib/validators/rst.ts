const RST_PATTERN = /^[0-9]{2,3}$/;

export const isValidRst = (value: string): boolean => {
    const trimmed = value.trim();
    if (trimmed === '') {
        return true;
    }
    return RST_PATTERN.test(trimmed);
};
