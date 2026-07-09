// Pure ladder builder — role-aware rungs + the current-row (step) logic.

import { describe, it, expect } from 'vitest';
import { buildLadder } from './ft8Ladder';
import type { Ft8QsoStatus } from './ft8.svelte';

function qso(over: Partial<Ft8QsoStatus> = {}): Ft8QsoStatus {
    return {
        active: false,
        role: '',
        theirCall: '',
        theirGrid: '',
        state: '',
        nextMessage: '',
        repeats: 0,
        maxRepeats: 0,
        ourReport: '',
        theirReport: '',
        theirPeriod: '',
        fd: false,
        ourClass: '',
        ourSection: '',
        theirClass: '',
        theirSection: '',
        ...over,
    };
}

describe('buildLadder', () => {
    it('idle → caller preview with placeholders, CQ row current', () => {
        const l = buildLadder(qso(), false, '7Q5MLV', 'KH66mv');
        expect(l.rungs[0].text).toBe('CQ 7Q5MLV KH66'); // grid trimmed to 4
        expect(l.rungs[1].text).toBe('7Q5MLV <DX> <GRID>');
        expect(l.step).toBe(0);
    });

    it('answerer opening — our grid rung is current (step 0)', () => {
        const l = buildLadder(
            qso({ active: true, role: 'answerer', theirCall: 'PJ4/NA2AA', state: 'calling' }),
            false,
            '7Q5MLV',
            'KH66'
        );
        expect(l.rungs[0]).toEqual({ dir: 'tx', text: 'PJ4/NA2AA 7Q5MLV KH66' });
        expect(l.step).toBe(0);
    });

    it('rowFor: once a rung is sent (repeats>0, not transmitting) the reply row is current', () => {
        const l = buildLadder(
            qso({ active: true, role: 'answerer', theirCall: 'X', state: 'calling', repeats: 1 }),
            false,
            'ME',
            'AA00'
        );
        expect(l.step).toBe(1); // bumped to the RX reply
    });

    it('answerer reporting → the R-report rung (step 2) with real reports', () => {
        const l = buildLadder(
            qso({
                active: true,
                role: 'answerer',
                theirCall: 'X',
                state: 'reporting',
                ourReport: '-08',
            }),
            true,
            'ME',
            'AA00'
        );
        expect(l.step).toBe(2);
        expect(l.rungs[2].text).toContain('R-08');
    });

    it('worker ladder drops the CQ row (opening is their call to us)', () => {
        const l = buildLadder(
            qso({ active: true, role: 'worker', theirCall: 'DL9UW', theirGrid: 'JO21' }),
            true,
            'ME',
            'AA00'
        );
        expect(l.rungs[0].dir).toBe('rx');
        expect(l.rungs.some((r) => r.text.startsWith('CQ'))).toBe(false);
    });

    it('Field Day answerer uses class/section rungs, not grid/report', () => {
        const l = buildLadder(
            qso({
                active: true,
                role: 'answerer',
                fd: true,
                theirCall: 'K7T',
                ourClass: '1D',
                ourSection: 'WWA',
            }),
            true,
            'ME',
            'AA00'
        );
        expect(l.rungs[0].text).toBe('K7T ME 1D WWA');
    });
});
