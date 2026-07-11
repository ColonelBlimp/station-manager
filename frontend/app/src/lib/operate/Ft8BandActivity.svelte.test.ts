// Render-path test: decodes pushed through the state module's ft8-decode handler
// must appear in the mounted pane, grouped by slot, with CQ + calling-you tints.
// Guards the ft8State ↔ Band Activity wiring (the feed logic is covered in
// ft8.svelte.test.ts).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Ft8BandActivity from './Ft8BandActivity.svelte';
import {
    ft8State,
    ft8Link,
    setFt8OperatorCall,
    setFt8MyGrid,
    setFt8TxActions,
    setFt8DisplayPrefs,
    resetFt8ForTests,
    type Ft8AnswerArgs,
    type Ft8WorkArgs,
    type Ft8TxResult,
    type Ft8TxActions,
} from './ft8.svelte';
import { setFt8Enricher, resetFt8EnrichForTests } from './ft8Enrich.svelte';
import { ft8PileupStack, _resetPileupForTests } from './ft8Pileup.svelte';
import { rig } from './rig.svelte';
import { session } from './session.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';
import type { DecodeReport } from '../api/ft8-sse';

const flush = () => new Promise((r) => setTimeout(r, 0));
const okResult = (): Promise<Ft8TxResult> => Promise.resolve({ ok: true, message: '' });

// A TX-action recorder with all seams stubbed ok; individual tests capture the one
// they exercise. Also puts the rig + TX state into a "ready to transmit" shape.
function armReady(over: Partial<Ft8TxActions> = {}): void {
    setFt8TxActions({
        arm: okResult,
        callCq: okResult,
        answerCq: okResult,
        workCaller: okResult,
        abandon: okResult,
        ...over,
    });
    rig.cat = 'connected';
    rig.freq = '14.074.000';
    ft8State.selectedOffset = 1500;
    ft8State.tx.armed = true;
}

beforeEach(() => {
    resetFt8ForTests();
    resetFt8EnrichForTests();
    _resetPileupForTests();
    resetToasts();
    session.qsos.length = 0;
    rig.band = '20m';
    rig.cat = 'off';
    rig.freq = '14.255.000';
});

function decode(
    startUtc: string,
    lines: { text: string; freq_hz: number; snr: number }[]
): DecodeReport {
    return {
        slot: { start_utc: startUtc, period: 'even' },
        decodes: lines.map((l) => ({ ...l, dt_s: 0 })),
    };
}

describe('Ft8BandActivity renderer', () => {
    it('shows the empty state before any decode', () => {
        render(Ft8BandActivity);
        expect(screen.getByText(/Decodes appear here/)).toBeInTheDocument();
    });

    it('renders live decodes grouped by slot, with CQ + calling-you tints', () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);

        ft8Link.onDecode(
            decode('2026-07-09T14:30:15Z', [
                { text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 },
                { text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }, // calling us
            ])
        );
        flushSync();

        // Both decodes rendered.
        expect(screen.getByText('CQ W1ABC FN42')).toBeInTheDocument();
        expect(screen.getByText('7Q5MLV PA3KUS JO21')).toBeInTheDocument();
        // Slot divider carries the UTC clock.
        expect(screen.getByText(/14:30:15/)).toBeInTheDocument();

        // CQ row tinted amber; the station calling us tinted blue.
        const cqRow = screen.getByText('CQ W1ABC FN42').closest('tr');
        expect(cqRow?.className).toContain('amber');
        const callRow = screen.getByText('7Q5MLV PA3KUS JO21').closest('tr');
        expect(callRow?.className).toContain('blue');
    });

    it('floats CQ rows to the top and drops slot dividers when cq_to_top is set', () => {
        setFt8OperatorCall('7Q5MLV');
        setFt8DisplayPrefs({ cqToTop: true });
        render(Ft8BandActivity);

        // Older slot carries a CQ; the newer slot a plain third-party exchange
        // (neither a CQ nor directed at us). Newest-first, the plain row would
        // normally sit above the CQ — cq_to_top must invert that.
        ft8Link.onDecode(
            decode('2026-07-09T14:30:00Z', [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 }])
        );
        ft8Link.onDecode(
            decode('2026-07-09T14:30:30Z', [{ text: 'PA3KUS DL1ABC -07', freq_hz: 1400, snr: -3 }])
        );
        flushSync();

        // No slot-divider rows in cq-to-top mode (the feed is no longer slot-ordered).
        expect(screen.queryByText('14:30:00')).toBeNull();
        expect(screen.queryByText('14:30:30')).toBeNull();

        // The CQ (older slot) is floated ABOVE the newer plain exchange.
        const cq = screen.getByText('CQ W1ABC FN42');
        const plain = screen.getByText('PA3KUS DL1ABC -07');
        expect(cq.compareDocumentPosition(plain) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('shows a per-CQ short-path bearing from the operator grid + decode grid', () => {
        setFt8MyGrid('IO91'); // London-ish
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -8 }]));
        flushSync();
        // FN42 (New England) from IO91 is roughly WNW — just assert a degree value renders.
        const row = screen.getByText('CQ W1ABC FN42').closest('tr');
        expect(row?.textContent).toMatch(/\d+°/);
    });

    it('renders the country flag once the enricher resolves', async () => {
        setFt8Enricher(() =>
            Promise.resolve({
                country: 'United States',
                ccode: 'US',
                dxcc: '291',
                isNewEntity: false,
                grid: 'FN42',
                name: '',
                qth: '',
                email: '',
                cqZone: '',
                ituZone: '',
            })
        );
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -8 }]));
        flushSync();
        await flush();
        flushSync();
        expect(screen.getByText('CQ W1ABC FN42').closest('tr')?.textContent).toContain('🇺🇸');
    });

    it('enriches a station calling you — flag rendered + country/DXCC in the tooltip', async () => {
        setFt8OperatorCall('7Q5MLV');
        setFt8Enricher(() =>
            Promise.resolve({
                country: 'Netherlands',
                ccode: 'NL',
                dxcc: '263',
                isNewEntity: false,
                grid: 'JO21',
                name: '',
                qth: '',
                email: '',
                cqZone: '',
                ituZone: '',
            })
        );
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }]));
        flushSync();
        await flush();
        flushSync();

        // A calling station now gets a flag (previously CQ-only).
        const btn = screen.getByText('7Q5MLV PA3KUS JO21');
        expect(btn.closest('tr')?.textContent).toContain('🇳🇱');
        // …and its work tooltip carries the country + DXCC entity.
        const title = btn.getAttribute('title') ?? '';
        expect(title).toContain('Work this station calling you');
        expect(title).toContain('Netherlands');
        expect(title).toContain('263');
    });

    it('typed filter hides non-matching rows; a station calling you always shows', () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('t1', [
                { text: 'CQ VK3ABC QF22', freq_hz: 1500, snr: -5 },
                { text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -8 },
                { text: '7Q5MLV DL1XYZ JO31', freq_hz: 800, snr: 2 }, // calling us
            ])
        );
        ft8State.bandFilter = 'VK';
        flushSync();

        expect(screen.getByText('CQ VK3ABC QF22')).toBeInTheDocument(); // token starts with VK
        expect(screen.queryByText('CQ W1ABC FN42')).toBeNull(); // filtered out
        expect(screen.getByText('7Q5MLV DL1XYZ JO31')).toBeInTheDocument(); // caller bypasses the filter
    });

    it('hide-hashed (config) drops <...> decodes but keeps identifiable ones', () => {
        setFt8OperatorCall('7Q5MLV');
        setFt8DisplayPrefs({ hideHashedCalls: true });
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('t1', [
                { text: 'CQ <...> FN42', freq_hz: 1500, snr: -5 },
                { text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -8 },
            ])
        );
        flushSync();

        expect(screen.queryByText('CQ <...> FN42')).toBeNull();
        expect(screen.getByText('CQ W1ABC FN42')).toBeInTheDocument();
    });
});

describe('Ft8BandActivity click-to-work (first RF path)', () => {
    it('answering a CQ: clicking an armed+ready CQ row calls answerCq with the parsed exchange', async () => {
        setFt8OperatorCall('7Q5MLV');
        const calls: Ft8AnswerArgs[] = [];
        armReady({
            answerCq: (a) => {
                calls.push(a);
                return okResult();
            },
        });
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();

        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();

        expect(calls).toHaveLength(1);
        expect(calls[0].theirCall).toBe('W1ABC');
        expect(calls[0].theirGrid).toBe('FN42');
        expect(calls[0].offsetHz).toBe(1500);
        expect(calls[0].opFreqMHz).toBeCloseTo(14.074, 3); // dial freq, not dial+offset
        expect(calls[0].fd).toBe(false);
    });

    it('working a caller: clicking a directed-at-me row calls workCaller with our SNR as the report', async () => {
        setFt8OperatorCall('7Q5MLV');
        const calls: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                calls.push(a);
                return okResult();
            },
        });
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }]));
        flushSync();

        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'));
        await flush();

        expect(calls).toHaveLength(1);
        expect(calls[0].theirCall).toBe('PA3KUS');
        expect(calls[0].theirGrid).toBe('JO21');
        expect(calls[0].theirSnr).toBe(2);
    });

    it('does NOT transmit when TX is disarmed', async () => {
        setFt8OperatorCall('7Q5MLV');
        let answered = 0;
        armReady({
            answerCq: () => {
                answered++;
                return okResult();
            },
        });
        ft8State.tx.armed = false; // disarmed — the whole point of the gate
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();

        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();
        expect(answered).toBe(0);
    });

    it('does NOT re-work a station already logged this session on this band', async () => {
        setFt8OperatorCall('7Q5MLV');
        let answered = 0;
        armReady({
            answerCq: () => {
                answered++;
                return okResult();
            },
        });
        session.qsos.push({
            id: 1,
            callsign: 'W1ABC',
            timeOn: '14:00:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();

        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();
        expect(answered).toBe(0); // dupe guard blocked it
    });
});

describe('Ft8BandActivity pile-up enqueue (Ctrl+click)', () => {
    it('Ctrl+clicking a calling-you row queues it; a CQ row is not queued', async () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('2026-07-09T14:30:00Z', [
                { text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }, // calling us
                { text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 }, // a CQ, not a caller
            ])
        );
        flushSync();

        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'), { ctrlKey: true });
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['PA3KUS']);

        // Ctrl+click on a CQ row does NOT queue (only callers do).
        await fireEvent.click(screen.getByText('CQ W1ABC FN42'), { ctrlKey: true });
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['PA3KUS']);
    });

    it('rejects a wrong-parity add (single-parity run)', async () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('2026-07-09T14:30:00Z', [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        ft8Link.onDecode(
            decode('2026-07-09T14:30:15Z', [{ text: '7Q5MLV DL1XYZ JO31', freq_hz: 900, snr: 1 }])
        );
        flushSync();

        // First (even :00) locks the run to even.
        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'), { ctrlKey: true });
        expect(ft8PileupStack.lockedParity).toBe('even');
        // An odd (:15) caller is rejected — the queue stays single-parity.
        await fireEvent.click(screen.getByText('7Q5MLV DL1XYZ JO31'), { ctrlKey: true });
        expect(ft8PileupStack.items.map((e) => e.call)).toEqual(['PA3KUS']);
    });
});

describe('Ft8BandActivity row markers', () => {
    it('marks a queued caller (Q) and the currently-worked station (working dot)', async () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('2026-07-09T14:30:00Z', [
                { text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }, // caller → queued
                { text: '7Q5MLV DL1XYZ JO31', freq_hz: 900, snr: 1 }, // caller → will be worked
            ])
        );
        flushSync();

        // Queue PA3KUS → Q badge, and nothing is being worked yet.
        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'), { ctrlKey: true });
        flushSync();
        expect(screen.getByTitle('In the pile-up queue')).toBeInTheDocument();
        expect(screen.queryByTitle('Working now')).toBeNull();

        // A QSO with DL1XYZ goes active → its row gets the working dot; PA3KUS keeps Q.
        ft8State.qso.active = true;
        ft8State.qso.theirCall = 'DL1XYZ';
        flushSync();
        expect(screen.getByTitle('Working now')).toBeInTheDocument();
        expect(screen.getByTitle('In the pile-up queue')).toBeInTheDocument();
    });
});
