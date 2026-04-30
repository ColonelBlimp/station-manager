const RST_PATTERN = /^[0-9]{2,3}$/;

export const isValidRst = (value: string): boolean => RST_PATTERN.test(value);
