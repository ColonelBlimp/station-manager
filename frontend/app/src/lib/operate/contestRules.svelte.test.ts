// W-0023 weekend slice (operator, 2026-10-31): in contest mode the operator
// is warned when the rig strays outside the contest's rules — a band it does
// not use, outside the sponsor's segments, or the wrong mode. UK/EI rule 3
// (the page, read 2026-10-31): 3.5, 7, 14, 21 and 28 MHz; SSB segments
// 3600-3650, 3700-3800 and 14125-14300 kHz; CW 3510-3560 and 14000-14060 kHz;
// rule 9.2: operating outside them, all QSOs disallowed. A warning only.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { contest, contestRuleWarning } from './contest.svelte';
import { rig } from './rig.svelte';

beforeEach(() => {
    contest.on = true;
    contest.id = 'UKEIDXSSB';
    contest.start = '2026-10-31';
    rig.mode = 'USB';
});
afterEach(() => {
    contest.on = false;
});

function at(freq: string, band: string, mode = 'USB'): string | null {
    rig.freq = freq;
    rig.band = band;
    rig.mode = mode;
    return contestRuleWarning();
}

describe('contest rule warning (UK/EI SSB)', () => {
    it('is silent inside the segments and on the open bands', () => {
        for (const [f, b] of [
            ['14.125.000', '20m'],
            ['14.187.500', '20m'],
            ['14.300.000', '20m'],
            ['3.600.000', '80m'],
            ['3.650.000', '80m'],
            ['3.750.000', '80m'],
            ['7.150.000', '40m'],
            ['21.300.000', '15m'],
            ['28.500.000', '10m'],
        ]) {
            expect(at(f, b), `${f} ${b}`).toBeNull();
        }
    });

    it('warns outside the 20 m and 80 m SSB segments, naming rule 9.2', () => {
        const w = at('14.100.000', '20m');
        expect(w).toContain('14.100');
        expect(w).toContain('14.125–14.300');
        expect(w).toContain('rule 9.2');
        expect(at('14.300.500', '20m')).not.toBeNull();
        expect(at('3.675.000', '80m')).toContain('3.600–3.650, 3.700–3.800');
        expect(at('3.599.000', '80m')).not.toBeNull();
    });

    it('warns on a band the contest does not use', () => {
        const w = at('18.150.000', '17m');
        expect(w).toContain('17m');
        expect(w).toContain('80, 40, 20, 15 and 10 m');
        expect(at('10.120.000', '30m')).not.toBeNull();
    });

    // Codex review 92dacbe8: the rig keeps its last band when tuned outside
    // every amateur band (7.150 → 7.400), so the band comes from the frequency.
    it('warns when the frequency leaves the band, whatever band the rig last reported', () => {
        expect(at('7.400.000', '40m')).toContain('7.400');
        expect(at('21.500.000', '15m')).not.toBeNull();
        expect(at('29.800.000', '10m')).not.toBeNull();
        expect(at('7.150.000', '20m')).toBeNull(); // a stale band name, a good frequency
    });

    it('warns on the wrong mode for the event', () => {
        expect(at('14.200.000', '20m', 'AM')).toContain('SSB');
        expect(at('14.200.000', '20m', 'CW')).toContain('SSB');
        expect(at('14.200.000', '20m', 'LSB')).toBeNull();
    });

    it('the CW event has its own segments', () => {
        contest.id = 'UKEIDXCW';
        contest.start = '2027-04-24';
        expect(at('14.030.000', '20m', 'CW')).toBeNull();
        expect(at('14.070.000', '20m', 'CW')).toContain('14.000–14.060');
        expect(at('14.030.000', '20m', 'USB')).toContain('CW');
    });

    it('is silent outside contest mode, and when the frequency is unknown', () => {
        expect(at('', '20m')).toBeNull();
        contest.on = false;
        expect(at('14.100.000', '20m')).toBeNull();
    });
});
