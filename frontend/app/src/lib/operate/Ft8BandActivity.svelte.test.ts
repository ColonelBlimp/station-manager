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
    reloadFt8EngagedFromStorage,
    type Ft8AnswerArgs,
    type Ft8WorkArgs,
    type Ft8TxResult,
    type Ft8TxActions,
} from './ft8.svelte';
import { setFt8Enricher, resetFt8EnrichForTests } from './ft8Enrich.svelte';
import { ft8PileupStack, _resetPileupForTests } from './ft8Pileup.svelte';
import { rig } from './rig.svelte';
import { session } from './session.svelte';
import { _resetForTests as resetToasts, toastsState } from '../ui/toasts.svelte';
import { noteDaemonDate, _resetDaemonClockForTests } from '../api/daemonClock.svelte';
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
        skip: okResult,
        next: okResult,
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
    _resetDaemonClockForTests();
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

// A slot start on the 15 s lattice, `slotsAgo` slots before now, with a chosen
// parity. Fixtures below used a hardcoded 2026-07-09 date until decode staleness
// landed (2026-07-31): a fixed date ages past the three-minute limit, the row stops
// being workable, and the test fails for a reason that has nothing to do with what
// it asserts. Anything testing a CLICK therefore needs a slot that is fresh
// relative to now. Parity is kept explicit because the pile-up rules depend on it.
function freshSlot(parity: 'even' | 'odd', slotsAgo = 0): string {
    let k = Math.floor(Date.now() / 15000) - slotsAgo;
    if ((k % 2 === 0) !== (parity === 'even')) k -= 1;
    return new Date(k * 15000).toISOString();
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

    // See the directed-call case below: a station worked earlier this session is still
    // answerable. Working it again is the operator's call — and sometimes the only way
    // to give the other station a contact they never completed.
    it('DOES re-answer a station already logged this session on this band', async () => {
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
        expect(answered).toBe(1);
    });
});

describe('Ft8BandActivity pile-up enqueue (Ctrl+click)', () => {
    it('Ctrl+clicking a calling-you row queues it; a CQ row is not queued', async () => {
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode(freshSlot('even'), [
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
            decode(freshSlot('even'), [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        ft8Link.onDecode(
            decode(freshSlot('odd'), [{ text: '7Q5MLV DL1XYZ JO31', freq_hz: 900, snr: 1 }])
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
            decode(freshSlot('even'), [
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

// Directed call (WSJT-X double-click): the SENDER of a plain decode row is
// callable without waiting for their CQ — a DX running a pile-up can go many
// minutes between CQs (the T22TT case, 2026-07-13). Double-click only; a
// single click on a plain row must never start a transmission.
describe('Ft8BandActivity directed call (double-click a plain row)', () => {
    it('double-click calls the sender via answerCq with the decode slot + fd:false', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({
            answerCq: (a) => (got.push(a), okResult()),
        });
        // ONE reading of the clock, used for both the decode and the expectation.
        // freshSlot() reads Date.now(), so calling it twice is a RACE: even-parity
        // slots are 30 s apart, and a slot boundary landing between the two calls
        // moves the expected value by a whole pair. That turned CI red on 503f31c7.
        const slot = freshSlot('even');
        render(Ft8BandActivity);
        ft8Link.onDecode(decode(slot, [{ text: 'K1ABC T22TT RI91', freq_hz: 1200, snr: -7 }]));
        flushSync();

        const rowBtn = screen.getByTitle(
            'Double-click to call T22TT (directed call — no CQ needed)'
        );
        await fireEvent.dblClick(rowBtn);
        await flush();

        expect(got).toHaveLength(1);
        expect(got[0]).toMatchObject({
            theirCall: 'T22TT',
            theirGrid: 'RI91',
            slotUtc: slot,
            offsetHz: 1500,
            fd: false,
            theirSnr: -7,
        });
    });

    it('a single click on a plain row never transmits', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('2026-07-09T14:30:00Z', [{ text: 'K1ABC T22TT -05', freq_hz: 1200, snr: -7 }])
        );
        flushSync();

        await fireEvent.click(
            screen.getByTitle('Double-click to call T22TT (directed call — no CQ needed)')
        );
        await flush();
        expect(got).toHaveLength(0);
    });

    it('a hashed sender is not callable — the row stays plain text', () => {
        setFt8OperatorCall('7Q5MLV');
        armReady();
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode('2026-07-09T14:30:00Z', [{ text: 'K1ABC <T22TT> R-07', freq_hz: 1200, snr: -7 }])
        );
        flushSync();

        expect(screen.getByText('K1ABC <T22TT> R-07')).toBeInTheDocument();
        expect(screen.queryByTitle(/Double-click to call/)).toBeNull();
    });

    // A repeat contact is ADVISORY, never a refusal: the operator is the licensee and
    // may work a station as often as they choose, and SM knows only its own log — not
    // whether the other station has the QSO. Blocking here fired exactly when working
    // again was correct (XE1GM, dogfood 2026-07-26: never copied our RR73, asked
    // eleven times, and this guard prevented the repair).
    it('worked-this-session does NOT block the directed call — it informs and proceeds', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });
        session.qsos.push({
            id: 1,
            callsign: 'T22TT',
            timeOn: '14:00:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '-05',
            rstRcvd: '-07',
            name: '',
            country: '',
            comment: '',
        });
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode(freshSlot('even'), [{ text: 'K1ABC T22TT RI91', freq_hz: 1200, snr: -7 }])
        );
        flushSync();

        await fireEvent.dblClick(
            screen.getByTitle('Double-click to call T22TT (directed call — no CQ needed)')
        );
        await flush();
        expect(got).toHaveLength(1);
        expect(got[0]).toMatchObject({ theirCall: 'T22TT' });
    });
});

describe('Ft8BandActivity deliberate-repeat intent', () => {
    // A contact is over on the air BEFORE the daemon finishes logging it: the
    // sequencer publishes the terminal idle first, then enriches and submits in a
    // goroutine. So `session.qsos` is still empty when the operator re-clicks, and
    // deriving the repeat intent from it alone sent allow_duplicate:false — which,
    // for two contacts inside one minute, silently discarded the second on-air
    // exchange. The engaged-call set closes that window (codex 0f08d2b2 P1).
    it('flags a repeat from a contact the daemon has not finished logging yet', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });

        // The sequencer engaged W1ABC, then went idle — no ft8-logged yet, so
        // session.qsos is still empty.
        // 14.074 MHz = 20 m, the band the click below happens on.
        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            dial_freq_mhz: 14.074,
        });
        ft8Link.onQso({ active: false });
        expect(session.qsos).toHaveLength(0);

        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();
        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();

        expect(got).toHaveLength(1);
        expect(got[0].allowDuplicate).toBe(true);
    });

    // Both consumers define a same-session duplicate as CALL + BAND. Keying the
    // engaged set on callsign alone classified the FIRST contact on a NEW band as a
    // repeat — and allow_duplicate becomes Submit's `force`, which uses a RANDOM
    // dedupe key, so that contact would lose duplicate protection outright
    // (codex a5667b00 P2).
    it('does not flag a station engaged on a DIFFERENT band', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });

        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            dial_freq_mhz: 14.074, // 20 m
        });
        ft8Link.onQso({ active: false });

        rig.band = '40m'; // band change — this is a NEW contact, not a repeat
        rig.freq = '7.074.000';
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();
        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();

        expect(got).toHaveLength(1);
        expect(got[0].allowDuplicate).toBe(false);
    });

    // A reload inside the async-logging window would otherwise drop the engagement
    // before session.qsos ever learns it — the exact data-loss case (codex a5667b00
    // P1). sessionStorage survives the reload and dies with the tab.
    it('survives a reload: engagement is restored from session storage', async () => {
        setFt8OperatorCall('7Q5MLV');
        rig.band = '20m';
        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            dial_freq_mhz: 14.074, // 20 m
        });
        ft8Link.onQso({ active: false });

        // Simulate a page reload: module state is rebuilt from storage, session.qsos
        // is empty (the daemon never got to log it).
        reloadFt8EngagedFromStorage();
        session.qsos.length = 0;

        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });
        setFt8OperatorCall('7Q5MLV');
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();
        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();

        expect(got).toHaveLength(1);
        expect(got[0].allowDuplicate).toBe(true);
    });

    // The engagement must be filed against the band the QSO is PINNED to, not
    // whatever the rig happens to show. The two are independent streams, so a band
    // change mid-contact (or a skew between them) would otherwise file a 20 m
    // contact under 40 m — and persistence would carry that error across a reload
    // (codex 18008c10 P1).
    it('attributes the engagement to the pinned dial, not live rig state', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });

        // Contact pinned to 20 m; the rig is dragged to 40 m while it runs.
        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            dial_freq_mhz: 14.074,
        });
        rig.band = '40m';
        rig.freq = '7.074.000';
        ft8Link.onQso({
            active: true,
            role: 'answerer',
            their_call: 'W1ABC',
            dial_freq_mhz: 14.074,
        });
        ft8Link.onQso({ active: false });

        // On 40 m this station has NOT been worked — the mid-contact rig state must
        // not have leaked an engagement onto this band.
        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();
        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();

        expect(got).toHaveLength(1);
        expect(got[0].allowDuplicate).toBe(false);
    });

    it('does not flag a station this session has never engaged', async () => {
        setFt8OperatorCall('7Q5MLV');
        const got: Ft8AnswerArgs[] = [];
        armReady({ answerCq: (a) => (got.push(a), okResult()) });

        render(Ft8BandActivity);
        ft8Link.onDecode(decode('t1', [{ text: 'CQ W1ABC FN42', freq_hz: 1200, snr: -12 }]));
        flushSync();
        await fireEvent.click(screen.getByText('CQ W1ABC FN42'));
        await flush();

        expect(got).toHaveLength(1);
        expect(got[0].allowDuplicate).toBe(false);
    });
});

/*
    DECODE STALENESS — found in dogfooding, 2026-07-31.

    UA4FKT's last decode was 01:27:45 UTC. At 01:33:16 the operator clicked their
    Band Activity row and SM transmitted a full six-rung ladder at a station that
    had left the air five and a half minutes earlier. The row was still there
    because retention is by COUNT (historyMax 100) and never by AGE — exactly ONE
    decode arrived on the whole band in that window, so the cap evicted nothing.

    ACCEPTANCE CRITERION (operator-approved 2026-07-31, threshold theirs):

      When a station stops transmitting, its row stops looking workable within one
      freshness window — and I can tell "nobody is calling" apart from "the decoder
      has stopped".

    GREY, NOT REMOVE (operator's call). Deleting rows would make a quiet band look
    identical to a dead decoder, which is the confusable state the criterion names.

    THE ROW MUST AGE WITH NO NEW DECODES ARRIVING, and that is the whole subtlety —
    on the band that produced this bug, ONE decode arrived in five and a half
    minutes. A staleness test that pushes a fresh decode to trigger the re-render
    proves nothing about the case it exists for, so S3 advances the clock instead.

    UNPARSEABLE SLOT TIMES ARE NOT STALE (S4). Unknown age is not old age — the
    same discipline the daemon keeps by returning a parse error rather than
    ErrStaleDecode.
*/
describe('Ft8BandActivity staleness', () => {
    const MINUTE = 60_000;
    const ago = (ms: number): string => new Date(Date.now() - ms).toISOString();

    // S1 — THE DISCRIMINATOR. A recent decode is worked exactly as before. Without
    // it, an implementation that refused every click would pass S2 and S3.
    it('works a fresh row normally', async () => {
        setFt8OperatorCall('7Q5MLV');
        const calls: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                calls.push(a);
                return okResult();
            },
        });
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode(ago(30_000), [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        flushSync();

        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'));
        await flush();

        expect(calls).toHaveLength(1);
    });

    // S2 — THE CRITERION. A decode older than the limit keys nothing, and says why.
    it('does not transmit when a stale row is clicked, and explains', async () => {
        setFt8OperatorCall('7Q5MLV');
        let worked = 0;
        armReady({
            workCaller: () => {
                worked++;
                return okResult();
            },
        });
        render(Ft8BandActivity);
        // The old row, then a NEWER slot that establishes where the daemon's clock
        // is now. A lone decode cannot be stale — it is the most recent thing the
        // daemon has said — so staleness needs later activity to be measured against.
        ft8Link.onDecode(
            decode(ago(4 * MINUTE), [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        ft8Link.onDecode(decode(ago(0), [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 }]));
        flushSync();

        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'));
        await flush();

        expect(worked).toBe(0);
        // Told, not silently ignored — the criterion's "tell it apart from the click
        // not registering" clause.
        expect(toastsState.items.map((t) => t.message).join(' ')).toMatch(/too old|stale/i);
        // And visibly not workable, so the click is discouraged before it happens.
        const row = screen.getByText('7Q5MLV PA3KUS JO21').closest('tr');
        expect(row?.className).toMatch(/opacity/);
    });

    // S3 — IT AGES ON ITS OWN. No new decode arrives; only time passes. This is the
    // case the bug actually occurred in, and the one a decode-driven re-render
    // cannot cover.
    it('greys a row that ages past the limit with no new decodes arriving', async () => {
        vi.useFakeTimers();
        try {
            setFt8OperatorCall('7Q5MLV');
            armReady();
            render(Ft8BandActivity);
            ft8Link.onDecode(
                decode(new Date().toISOString(), [
                    { text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 },
                ])
            );
            flushSync();

            const row = () => screen.getByText('7Q5MLV PA3KUS JO21').closest('tr');
            expect(row()?.className).not.toMatch(/opacity/);

            // Nothing pushed — the band has simply gone quiet.
            await vi.advanceTimersByTimeAsync(4 * MINUTE);
            flushSync();

            expect(row()?.className).toMatch(/opacity/);
        } finally {
            vi.useRealTimers();
        }
    });

    // S5 — CLOCK SKEW MUST NOT BLOCK A LIVE BAND (codex 9d7a3f46 P1). Staleness is
    // measured against the DAEMON's clock, so a browser running minutes fast must
    // not grey every row on arrival and refuse every click while the daemon — which
    // would have accepted them — never sees the request. Operating the SPA from
    // another machine on the LAN is enough to meet this.
    //
    // Calibrated from the HTTP `Date` header, NOT from a decode frame: the FT8 hub
    // replays the last decode to every new subscriber, so a tab opened after
    // capture stopped would otherwise adopt an ancient slot as "now" and make its
    // own stale rows look fresh (codex 0d85428e P2).
    it('works rows normally when the browser clock runs ahead of the daemon', async () => {
        setFt8OperatorCall('7Q5MLV');
        const calls: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                calls.push(a);
                return okResult();
            },
        });
        // The daemon is 20 minutes behind this browser.
        noteDaemonDate(new Date(Date.now() - 20 * MINUTE).toUTCString());
        render(Ft8BandActivity);

        // A decode that is CURRENT by the daemon's clock, and ancient by ours.
        ft8Link.onDecode(
            decode(ago(20 * MINUTE), [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        flushSync();

        const row = screen.getByText('7Q5MLV PA3KUS JO21');
        expect(row.closest('tr')?.className).not.toMatch(/opacity/);

        await fireEvent.click(row);
        await flush();
        expect(calls).toHaveLength(1);
    });

    // S6 — A REPLAYED DECODE MUST NOT MOVE THE CLOCK. The hub replays the last
    // decode frame to a new subscriber, so a tab opened long after capture stopped
    // receives an ancient slot as its FIRST frame. Calibrating from that would
    // declare it "now" and make every stale row workable again — the guard
    // defeating itself, with the daemon then rejecting the click.
    it('a decode frame does not recalibrate the clock', async () => {
        setFt8OperatorCall('7Q5MLV');
        let worked = 0;
        armReady({
            workCaller: () => {
                worked++;
                return okResult();
            },
        });
        render(Ft8BandActivity);

        // Exactly the replay shape: one ancient frame, arriving now, as the first
        // thing this tab sees. Uncalibrated, so browser-now stands in for daemon-now.
        ft8Link.onDecode(
            decode(ago(20 * MINUTE), [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        flushSync();

        const row = screen.getByText('7Q5MLV PA3KUS JO21');
        expect(row.closest('tr')?.className).toMatch(/opacity/);
        await fireEvent.click(row);
        await flush();
        expect(worked).toBe(0);
    });

    // S7 — FAIL OPEN WHEN THE CLOCK IS NOT TRUSTWORTHY (codex 503f31c7 P2). A
    // suspended laptop freezes the monotonic clock while wall time runs on, and
    // from inside the page that is indistinguishable from a wall-clock step. The
    // client therefore stops claiming to know: nothing is greyed, every click goes
    // through, and the daemon — which holds the only clock that matters — decides.
    //
    // Failing open rather than closed is the load-bearing choice. Closed would
    // re-create the previous round's deadlock: greyed rows block clicks, and the
    // requests that would recalibrate the clock are exactly those clicks.
    it('does not grey or block anything while the clock is untrustworthy', async () => {
        setFt8OperatorCall('7Q5MLV');
        const calls: Ft8WorkArgs[] = [];
        armReady({
            workCaller: (a) => {
                calls.push(a);
                return okResult();
            },
        });
        // Faked BEFORE render, or the component's re-check interval is a real one
        // that advanceTimersByTime cannot drive. Only the timers — Date and
        // performance stay real until the suspend is modelled below.
        vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
        noteDaemonDate(new Date().toUTCString());
        render(Ft8BandActivity);
        ft8Link.onDecode(
            decode(ago(10 * MINUTE), [{ text: '7Q5MLV PA3KUS JO21', freq_hz: 800, snr: 2 }])
        );
        ft8Link.onDecode(decode(ago(0), [{ text: 'CQ W1ABC FN42', freq_hz: 1500, snr: -12 }]));
        flushSync();
        // Stale while the clock is believed — the discriminator for what follows.
        expect(screen.getByText('7Q5MLV PA3KUS JO21').closest('tr')?.className).toMatch(/opacity/);

        // The machine sleeps: wall time advances, monotonic time does not. Only the
        // INTERVAL is faked — Date/performance stay under the spies below, which are
        // what model the suspend.
        const perfFrozen = performance.now();
        const wallAfterSleep = Date.now() + 10 * MINUTE;
        vi.spyOn(performance, 'now').mockReturnValue(perfFrozen);
        vi.spyOn(Date, 'now').mockReturnValue(wallAfterSleep);
        // The component re-checks on its own tick, which is how a resumed tab
        // notices without any decode arriving.
        vi.advanceTimersByTime(6000);
        flushSync();
        expect(screen.getByText('7Q5MLV PA3KUS JO21').closest('tr')?.className).not.toMatch(
            /opacity/
        );

        await fireEvent.click(screen.getByText('7Q5MLV PA3KUS JO21'));
        await flush();
        expect(calls).toHaveLength(1);
        vi.useRealTimers();
        vi.restoreAllMocks();
    });

    // S4 — an unparseable slot time is NOT stale. Unknown age is not old age, and
    // refusing on a fact we do not have would be worse than allowing the click.
    it('treats a decode with an unparseable slot time as workable', async () => {
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
    });
});
