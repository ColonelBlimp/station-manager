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
        cqMessage: '',
        repeats: 0,
        maxRepeats: 0,
        skipArmed: false,
        nextArmed: false,
        autoWorkArmed: false,
        ourReport: '',
        theirReport: '',
        theirPeriod: '',
        fd: false,
        type4: false,
        ourClass: '',
        ourSection: '',
        theirClass: '',
        theirSection: '',
        answerMode: '',
        answerers: [],
        queue: [],
        drainPaused: false,
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

    it('type-4 answerer: bare opening → their roger → our 73 (no grid/report rungs)', () => {
        const l = buildLadder(
            qso({ active: true, role: 'answerer', type4: true, theirCall: 'PJ4/NA2AA' }),
            true,
            'ME',
            'AA00'
        );
        expect(l.rungs.map((r) => r.text)).toEqual([
            'PJ4/NA2AA ME',
            'ME PJ4/NA2AA RR73',
            'PJ4/NA2AA ME 73',
        ]);
        expect(l.rungs.some((r) => /AA00|<GRID>|R[+-]/.test(r.text))).toBe(false);
        expect(l.step).toBe(0); // calling
    });

    it('type-4 answerer confirming → the 73 rung is current (step 2)', () => {
        const l = buildLadder(
            qso({
                active: true,
                role: 'answerer',
                type4: true,
                state: 'confirming',
                theirCall: 'PJ4/NA2AA',
            }),
            true,
            'ME',
            'AA00'
        );
        expect(l.step).toBe(2);
        expect(l.rungs[2].text).toBe('PJ4/NA2AA ME 73');
    });

    it('type-4 worker: their bare call → our RR73 (single TX rung, no CQ row)', () => {
        const l = buildLadder(
            qso({ active: true, role: 'worker', type4: true, theirCall: 'K1ABC/D' }),
            true,
            'ME',
            'AA00'
        );
        expect(l.rungs.map((r) => r.text)).toEqual([
            'ME K1ABC/D',
            'K1ABC/D ME RR73',
            'ME K1ABC/D 73',
        ]);
        expect(l.rungs.some((r) => r.text.startsWith('CQ'))).toBe(false);
        expect(l.step).toBe(1); // rogering (our RR73)
    });
});

// W-0011: the CQ token between CQ and the call. The only editable content in the
// whole ladder; every other rung's fields are fixed by the protocol.
describe('CQ modifier', () => {
    it('rides between CQ and the call on the idle preview, trimmed and upper-cased', () => {
        const l = buildLadder(qso(), false, '7Q5MLV', 'KH78', ' af ');
        expect(l.rungs[0].text).toBe('CQ AF 7Q5MLV KH78');
        expect(l.rungs[0].cq).toEqual({ call: '7Q5MLV', grid: 'KH78' });
    });
    it('an empty token is the standard CQ, and so is the legacy four-argument call', () => {
        expect(buildLadder(qso(), false, '7Q5MLV', 'KH78', '').rungs[0].text).toBe(
            'CQ 7Q5MLV KH78'
        );
        expect(buildLadder(qso(), false, '7Q5MLV', 'KH78').rungs[0].text).toBe('CQ 7Q5MLV KH78');
    });
    it("a live Call-CQ run shows the daemon's own CQ text, not the typed token", () => {
        const live = qso({
            active: true,
            role: 'caller',
            state: 'calling',
            nextMessage: 'CQ AF 7Q5MLV KH78',
        });
        expect(buildLadder(live, false, '7Q5MLV', 'KH78', 'DX').rungs[0].text).toBe(
            'CQ AF 7Q5MLV KH78'
        );
    });
    it("a tab that did not start the run shows the run's CQ from the frame while an answerer is worked", () => {
        // Second tab, or this tab after a reload: no local token, and next_message is
        // the report — the frame's cq_message is the only truthful source.
        const live = qso({
            active: true,
            role: 'caller',
            state: 'reporting',
            theirCall: 'DL9UW',
            nextMessage: 'DL9UW 7Q5MLV -08',
            cqMessage: 'CQ AF 7Q5MLV KH78',
        });
        expect(buildLadder(live, false, '7Q5MLV', 'KH78', '').rungs[0].text).toBe(
            'CQ AF 7Q5MLV KH78'
        );
        // And the frame wins over a DIFFERENT local token, both phases.
        expect(buildLadder(live, false, '7Q5MLV', 'KH78', 'DX').rungs[0].text).toBe(
            'CQ AF 7Q5MLV KH78'
        );
    });

    it('while the run works an answerer the CQ rung keeps the composed CQ', () => {
        const live = qso({
            active: true,
            role: 'caller',
            state: 'reporting',
            theirCall: 'DL9UW',
            nextMessage: 'DL9UW 7Q5MLV -08',
        });
        expect(buildLadder(live, false, '7Q5MLV', 'KH78', 'AF').rungs[0].text).toBe(
            'CQ AF 7Q5MLV KH78'
        );
    });
    it('only the caller ladder carries the editable parts', () => {
        const answering = qso({ active: true, role: 'answerer', theirCall: 'DL9UW' });
        const l = buildLadder(answering, false, '7Q5MLV', 'KH78', 'AF');
        expect(l.rungs.every((r) => r.cq === undefined)).toBe(true);
        expect(l.rungs.map((r) => r.text).join(' | ')).not.toContain('AF');
    });
});
