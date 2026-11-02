// W-0012 Phone/CW band memory — slice 1: pure destination selection and the
// CAT-off behaviour (AC1, AC2, AC7; rulings 2, 3, 6 and the next/previous
// ruling of 2026-11-02). No live rig path is reachable from this slice.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    resolveBandDestination,
    eligibleBands,
    stepBand,
    resolveStepDestination,
    selectBandCatOff,
    type MemoryLookup,
    type Position,
} from './bandSelection';
import { rig, setOperatingBands, setFt8Frequencies, setFt4Frequencies } from './rig.svelte';

const NO_MEMORY: MemoryLookup = () => undefined;

function memoryOf(entries: Record<string, Position>): MemoryLookup {
    return (band) => entries[band];
}

const FT8_DIALS = {
    '80m': 3_573_000,
    '40m': 7_074_000,
    '20m': 14_074_000,
    '15m': 21_074_000,
};
const FT4_DIALS = { '40m': 7_047_500, '20m': 14_080_000 };

beforeEach(() => {
    setOperatingBands([]); // the HF..6m default list
    setFt8Frequencies(FT8_DIALS);
    setFt4Frequencies(FT4_DIALS);
});
afterEach(() => {
    setOperatingBands([]);
    setFt8Frequencies({});
    setFt4Frequencies({});
});

describe('AC1: Phone/CW destination', () => {
    // Ruling 2: BAND_DEFAULT_HZ frequencies with an explicit mode table.
    it.each([
        ['160m', 1_900_000, 'LSB'],
        ['80m', 3_700_000, 'LSB'],
        ['40m', 7_100_000, 'LSB'],
        ['30m', 10_125_000, 'CW'],
        ['20m', 14_200_000, 'USB'],
        ['17m', 18_130_000, 'USB'],
        ['15m', 21_300_000, 'USB'],
        ['12m', 24_950_000, 'USB'],
        ['10m', 28_400_000, 'USB'],
        ['6m', 50_150_000, 'USB'],
    ])('%s with no memory goes to the default %i %s', (band, hz, mode) => {
        expect(resolveBandDestination('phone', band, NO_MEMORY)).toEqual({
            kind: 'tune',
            band,
            hz,
            mode,
        });
    });

    it('a remembered frequency and mode win over the default (20m CW returns as CW)', () => {
        const memory = memoryOf({ '20m': { hz: 14_025_000, mode: 'CW' } });
        expect(resolveBandDestination('phone', '20m', memory)).toEqual({
            kind: 'tune',
            band: '20m',
            hz: 14_025_000,
            mode: 'CW',
        });
    });

    it('reads the memory for the selected band only', () => {
        const lookup = vi.fn(NO_MEMORY);
        resolveBandDestination('phone', '40m', lookup);
        expect(lookup).toHaveBeenCalledWith('40m');
        expect(lookup).not.toHaveBeenCalledWith(expect.not.stringMatching(/^40m$/));
    });

    // Ruling 6: 60m has no default frequency in this slice.
    it('60m with no memory has no destination and says why', () => {
        expect(resolveBandDestination('phone', '60m', NO_MEMORY)).toEqual({
            kind: 'none',
            message: 'No Phone/CW default configured for 60m.',
        });
    });

    it('60m with a memory recalls it', () => {
        const memory = memoryOf({ '60m': { hz: 5_363_000, mode: 'USB' } });
        expect(resolveBandDestination('phone', '60m', memory)).toEqual({
            kind: 'tune',
            band: '60m',
            hz: 5_363_000,
            mode: 'USB',
        });
    });

    it('a band with no default and no memory has no destination', () => {
        expect(resolveBandDestination('phone', '4m', NO_MEMORY)).toEqual({
            kind: 'none',
            message: 'No Phone/CW default configured for 4m.',
        });
    });

    // A memory that cannot be a position on that band is not a destination;
    // the default stands in for it (60m: the message).
    it.each([
        ['outside the band', { hz: 7_100_000, mode: 'USB' }],
        ['not a number', { hz: Number.NaN, mode: 'USB' }],
        ['not whole hertz', { hz: 14_200_000.5, mode: 'USB' }],
        ['no mode', { hz: 14_250_000, mode: '' }],
    ])('ignores a 20m memory %s', (_label, position) => {
        const memory = memoryOf({ '20m': position });
        expect(resolveBandDestination('phone', '20m', memory)).toEqual({
            kind: 'tune',
            band: '20m',
            hz: 14_200_000,
            mode: 'USB',
        });
    });
});

describe('AC2: FT destination', () => {
    it.each([
        ['ft8', '40m', 7_074_000, 'FT8'],
        ['ft8', '15m', 21_074_000, 'FT8'],
        ['ft4', '40m', 7_047_500, 'FT4'],
        ['ft4', '20m', 14_080_000, 'FT4'],
    ] as const)('%s %s goes to its own dial', (view, band, hz, mode) => {
        expect(resolveBandDestination(view, band, NO_MEMORY)).toEqual({
            kind: 'tune',
            band,
            hz,
            mode,
        });
    });

    it.each([
        ['ft8', '60m', 'No FT8 frequency configured for 60m.'],
        ['ft4', '15m', 'No FT4 frequency configured for 15m.'],
    ] as const)('%s %s with no dial has no destination', (view, band, message) => {
        expect(resolveBandDestination(view, band, NO_MEMORY)).toEqual({ kind: 'none', message });
    });

    // Review 2026-11-02 (P2): config accepts any positive dial, so a dial that
    // is not on its band is refused rather than displayed as that band.
    it('a dial that is not on its band has no destination', () => {
        setFt8Frequencies({ ...FT8_DIALS, '20m': 7_074_000 });
        expect(resolveBandDestination('ft8', '20m', NO_MEMORY)).toEqual({
            kind: 'none',
            message: 'The FT8 frequency configured for 20m (7.074.000) is not on 20m.',
        });
    });

    it.each([
        ['ft8', 7_074_000],
        ['ft4', 7_047_500],
    ] as const)('%s never reads Phone/CW memory', (view, dial) => {
        const lookup = vi.fn(memoryOf({ '40m': { hz: 7_150_000, mode: 'LSB' } }));
        const dest = resolveBandDestination(view, '40m', lookup);
        expect(lookup).not.toHaveBeenCalled();
        expect(dest).toMatchObject({ kind: 'tune', hz: dial });
    });
});

describe('eligible bands (next/previous ruling)', () => {
    it('Phone/CW: the operating bands in configured order', () => {
        setOperatingBands(['20m', '40m', '15m']);
        expect(eligibleBands('phone', NO_MEMORY)).toEqual(['20m', '40m', '15m']);
    });

    // Ruling (a), 2026-11-02: a band with no usable destination is not eligible.
    it('Phone/CW: a band with neither valid memory nor a default is not eligible', () => {
        setOperatingBands(['80m', '60m', '40m', '4m']);
        expect(eligibleBands('phone', NO_MEMORY)).toEqual(['80m', '40m']);
    });

    it('Phone/CW: invalid 60m memory does not make 60m eligible', () => {
        setOperatingBands(['80m', '60m', '40m']);
        const memory = memoryOf({ '60m': { hz: 7_100_000, mode: 'USB' } });
        expect(eligibleBands('phone', memory)).toEqual(['80m', '40m']);
    });

    it('Phone/CW: a saved 60m position makes 60m eligible', () => {
        setOperatingBands(['80m', '60m', '40m']);
        const memory = memoryOf({ '60m': { hz: 5_363_000, mode: 'USB' } });
        expect(eligibleBands('phone', memory)).toEqual(['80m', '60m', '40m']);
    });

    it('FT8/FT4: operating bands with that profile dial, in configured order', () => {
        setOperatingBands(['20m', '60m', '40m', '15m', '80m']);
        expect(eligibleBands('ft8', NO_MEMORY)).toEqual(['20m', '40m', '15m', '80m']);
        expect(eligibleBands('ft4', NO_MEMORY)).toEqual(['20m', '40m']);
    });

    it('FT: a band whose dial is not on it is not eligible', () => {
        setOperatingBands(['80m', '40m', '20m']);
        setFt8Frequencies({ ...FT8_DIALS, '20m': 7_074_000 });
        expect(eligibleBands('ft8', NO_MEMORY)).toEqual(['80m', '40m']);
    });
});

describe('stepBand (next/previous ruling)', () => {
    it.each([
        ['next mid-list', ['20m', '40m', '15m'], '40m', 1, '15m'],
        ['next wraps at the end', ['20m', '40m', '15m'], '15m', 1, '20m'],
        ['previous mid-list', ['20m', '40m', '15m'], '40m', -1, '20m'],
        ['previous wraps at the start', ['20m', '40m', '15m'], '20m', -1, '15m'],
        ['a single band steps to itself', ['20m'], '20m', 1, '20m'],
    ] as const)('on-list: %s follows configured order', (_l, list, from, dir, want) => {
        expect(stepBand([...list], from, dir)).toBe(want);
    });

    it.each([
        ['next from excluded 60m', ['80m', '40m', '20m'], '60m', 1, '40m'],
        ['previous from excluded 60m', ['80m', '40m', '20m'], '60m', -1, '80m'],
        // Frequency order, not configured order, decides the entry point.
        ['next from 60m, configured out of order', ['20m', '80m', '40m'], '60m', 1, '40m'],
        ['previous from 60m, configured out of order', ['20m', '40m', '80m'], '60m', -1, '80m'],
        ['next wraps to the lowest', ['40m', '20m'], '6m', 1, '40m'],
        ['previous wraps to the highest', ['40m', '20m'], '160m', -1, '20m'],
    ] as const)('off-list: %s', (_l, list, from, dir, want) => {
        expect(stepBand([...list], from, dir)).toBe(want);
    });

    it.each([
        ['', 1, '20m'],
        ['', -1, '15m'],
        ['unknown', 1, '20m'],
        ['unknown', -1, '15m'],
    ] as const)('unknown current %j, dir %i → configured end', (from, dir, want) => {
        expect(stepBand(['20m', '40m', '15m'], from, dir)).toBe(want);
    });

    it('no eligible bands → nothing', () => {
        expect(stepBand([], '20m', 1)).toBeNull();
        expect(stepBand([], '20m', -1)).toBeNull();
    });
});

describe('resolveStepDestination', () => {
    it('Phone/CW next resolves the stepped band through memory then default', () => {
        setOperatingBands(['40m', '20m', '15m']);
        const memory = memoryOf({ '20m': { hz: 14_250_000, mode: 'USB' } });
        expect(resolveStepDestination('phone', '40m', 1, memory)).toEqual({
            kind: 'tune',
            band: '20m',
            hz: 14_250_000,
            mode: 'USB',
        });
        expect(resolveStepDestination('phone', '20m', 1, memory)).toEqual({
            kind: 'tune',
            band: '15m',
            hz: 21_300_000,
            mode: 'USB',
        });
    });

    it('FT8 previous from an excluded band enters at the nearest lower dial band', () => {
        setOperatingBands(['80m', '60m', '40m', '20m']);
        expect(resolveStepDestination('ft8', '60m', -1, NO_MEMORY)).toEqual({
            kind: 'tune',
            band: '80m',
            hz: 3_573_000,
            mode: 'FT8',
        });
    });

    describe('Phone/CW skips bands with no destination (ruling (a))', () => {
        beforeEach(() => setOperatingBands(['80m', '60m', '40m']));

        it('next from 80m skips 60m', () => {
            expect(resolveStepDestination('phone', '80m', 1, NO_MEMORY)).toMatchObject({
                band: '40m',
            });
        });

        it('previous from 40m skips 60m', () => {
            expect(resolveStepDestination('phone', '40m', -1, NO_MEMORY)).toMatchObject({
                band: '80m',
            });
        });

        it('wrapping skips it too', () => {
            setOperatingBands(['60m', '80m', '40m']);
            expect(resolveStepDestination('phone', '40m', 1, NO_MEMORY)).toMatchObject({
                band: '80m',
            });
            setOperatingBands(['80m', '40m', '60m']);
            expect(resolveStepDestination('phone', '80m', -1, NO_MEMORY)).toMatchObject({
                band: '40m',
            });
        });

        it('invalid 60m memory is still skipped', () => {
            const memory = memoryOf({ '60m': { hz: 7_100_000, mode: 'USB' } });
            expect(resolveStepDestination('phone', '80m', 1, memory)).toMatchObject({
                band: '40m',
            });
        });

        it('a saved 60m position is visited', () => {
            const memory = memoryOf({ '60m': { hz: 5_363_000, mode: 'USB' } });
            expect(resolveStepDestination('phone', '80m', 1, memory)).toEqual({
                kind: 'tune',
                band: '60m',
                hz: 5_363_000,
                mode: 'USB',
            });
        });

        it('every configured band lacking a destination explains that', () => {
            setOperatingBands(['60m', '4m']);
            expect(resolveStepDestination('phone', '60m', 1, NO_MEMORY)).toEqual({
                kind: 'none',
                message: 'No operating band has a Phone/CW position or default.',
            });
        });
    });

    it('FT next skips a band whose dial is not on it', () => {
        setOperatingBands(['40m', '20m', '15m']);
        setFt8Frequencies({ ...FT8_DIALS, '20m': 7_074_000 });
        expect(resolveStepDestination('ft8', '40m', 1, NO_MEMORY)).toMatchObject({
            band: '15m',
        });
    });

    it('no eligible bands → an explanation, no destination', () => {
        setOperatingBands(['60m', '30m']); // neither carries an FT4 dial
        expect(resolveStepDestination('ft4', '40m', 1, NO_MEMORY)).toEqual({
            kind: 'none',
            message: 'No operating band has a usable FT4 frequency configured.',
        });
    });
});

describe('AC7: CAT-off selection changes only the displayed context', () => {
    beforeEach(() => {
        rig.cat = 'off';
        rig.band = '20m';
        rig.freq = '14.255.000';
        rig.mode = 'USB';
    });

    function displayed() {
        return { band: rig.band, freq: rig.freq, mode: rig.mode };
    }

    it('Phone/CW default sets band, frequency and mode', () => {
        expect(selectBandCatOff('phone', '40m', NO_MEMORY)).toEqual({ status: 'accepted' });
        expect(displayed()).toEqual({ band: '40m', freq: '7.100.000', mode: 'LSB' });
    });

    it('Phone/CW memory wins', () => {
        const memory = memoryOf({ '30m': { hz: 10_136_000, mode: 'USB' } });
        selectBandCatOff('phone', '30m', memory);
        expect(displayed()).toEqual({ band: '30m', freq: '10.136.000', mode: 'USB' });
    });

    it('60m with no memory leaves everything unchanged and says why', () => {
        expect(selectBandCatOff('phone', '60m', NO_MEMORY)).toEqual({
            status: 'failed',
            kind: 'refused',
            message: 'No Phone/CW default configured for 60m.',
        });
        expect(displayed()).toEqual({ band: '20m', freq: '14.255.000', mode: 'USB' });
    });

    it('FT8 sets the band and dial, and leaves the mode as it is today', () => {
        const lookup = vi.fn(NO_MEMORY);
        expect(selectBandCatOff('ft8', '40m', lookup)).toEqual({ status: 'accepted' });
        expect(displayed()).toEqual({ band: '40m', freq: '7.074.000', mode: 'USB' });
        expect(lookup).not.toHaveBeenCalled();
    });

    it('FT4 sets the band and dial, and leaves the mode as it is today', () => {
        rig.mode = 'LSB';
        expect(selectBandCatOff('ft4', '20m', NO_MEMORY)).toEqual({ status: 'accepted' });
        expect(displayed()).toEqual({ band: '20m', freq: '14.080.000', mode: 'LSB' });
    });

    // Review 2026-11-02 (P2): never display "20m / 7.074 MHz".
    it('FT dial not on its band: refused, display unchanged', () => {
        rig.band = '40m';
        rig.freq = '7.150.000';
        setFt8Frequencies({ ...FT8_DIALS, '20m': 7_074_000 });
        expect(selectBandCatOff('ft8', '20m', NO_MEMORY)).toEqual({
            status: 'failed',
            kind: 'refused',
            message: 'The FT8 frequency configured for 20m (7.074.000) is not on 20m.',
        });
        expect(displayed()).toEqual({ band: '40m', freq: '7.150.000', mode: 'USB' });
    });

    it('FT4 with no dial leaves everything unchanged and says why', () => {
        expect(selectBandCatOff('ft4', '15m', NO_MEMORY)).toEqual({
            status: 'failed',
            kind: 'refused',
            message: 'No FT4 frequency configured for 15m.',
        });
        expect(displayed()).toEqual({ band: '20m', freq: '14.255.000', mode: 'USB' });
    });

    it('refuses with CAT connected: the live path is not this function', () => {
        rig.cat = 'connected';
        const result = selectBandCatOff('phone', '40m', NO_MEMORY);
        expect(result).toMatchObject({ status: 'failed', kind: 'refused' });
        expect(displayed()).toEqual({ band: '20m', freq: '14.255.000', mode: 'USB' });
        rig.cat = 'off';
    });
});
