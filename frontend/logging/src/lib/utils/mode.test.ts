import { describe, expect, it } from 'vitest';
import { resolveModeAndSubmode } from './mode';

describe('resolveModeAndSubmode', () => {
    it('maps USB → MODE=SSB, SUBMODE=USB', () => {
        expect(resolveModeAndSubmode('USB')).toEqual({ mode: 'SSB', subMode: 'USB' });
    });

    it('maps LSB → MODE=SSB, SUBMODE=LSB', () => {
        expect(resolveModeAndSubmode('LSB')).toEqual({ mode: 'SSB', subMode: 'LSB' });
    });

    it('passes FT8 through as a main mode (ADIF 3.x promotes FT8 from MFSK submode to main mode)', () => {
        expect(resolveModeAndSubmode('FT8')).toEqual({ mode: 'FT8', subMode: '' });
    });

    it('passes FT4 through as a main mode (ADIF 3.x same as FT8)', () => {
        expect(resolveModeAndSubmode('FT4')).toEqual({ mode: 'FT4', subMode: '' });
    });

    it('maps PSK31 → MODE=PSK, SUBMODE=PSK31', () => {
        expect(resolveModeAndSubmode('PSK31')).toEqual({ mode: 'PSK', subMode: 'PSK31' });
    });

    it('passes CW through as a main mode with empty submode', () => {
        expect(resolveModeAndSubmode('CW')).toEqual({ mode: 'CW', subMode: '' });
    });

    it('passes FM/AM/RTTY/SSB through as main modes', () => {
        expect(resolveModeAndSubmode('FM')).toEqual({ mode: 'FM', subMode: '' });
        expect(resolveModeAndSubmode('AM')).toEqual({ mode: 'AM', subMode: '' });
        expect(resolveModeAndSubmode('RTTY')).toEqual({ mode: 'RTTY', subMode: '' });
        expect(resolveModeAndSubmode('SSB')).toEqual({ mode: 'SSB', subMode: '' });
    });

    it('preserves an explicit submode refinement on a main mode (CW + CW-N)', () => {
        expect(resolveModeAndSubmode('CW', 'CW-N')).toEqual({ mode: 'CW', subMode: 'CW-N' });
    });

    it('overrides an unrelated subMode when the operator picked a submode-named mode', () => {
        // USB-as-mode means SSB+USB; any subMode CAT pushed alongside is
        // displaced because USB IS the submode in this case.
        expect(resolveModeAndSubmode('USB', 'IGNORED')).toEqual({ mode: 'SSB', subMode: 'USB' });
    });

    it('uppercases and trims input', () => {
        expect(resolveModeAndSubmode('  usb  ')).toEqual({ mode: 'SSB', subMode: 'USB' });
        expect(resolveModeAndSubmode('Ft8')).toEqual({ mode: 'FT8', subMode: '' });
    });

    it('returns empty mode for empty input', () => {
        expect(resolveModeAndSubmode('')).toEqual({ mode: '', subMode: '' });
        expect(resolveModeAndSubmode('   ')).toEqual({ mode: '', subMode: '' });
    });

    it('passes unknown values through (daemon will reject)', () => {
        expect(resolveModeAndSubmode('XYZ')).toEqual({ mode: 'XYZ', subMode: '' });
    });
});
