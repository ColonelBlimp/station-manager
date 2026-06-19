import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { _activeSourceForTests, ft8State, startFt8, stopFt8 } from './ft8.svelte';
import { configState } from './config.svelte';
import { sessionQsosState } from './sessionQsos.svelte';

/**
 * Synchronous fake EventSource (same approach as bridge.test.ts): startFt8()
 * makes `new EventSource(url)` resolve to this fake; tests drive open/error and
 * the `ft8-occupancy` event programmatically and assert ft8State.
 */
class FakeEventSource {
    static instances: FakeEventSource[] = [];
    url: string;
    readyState = 0;
    closed = false;
    private listeners = new Map<string, Set<(ev: MessageEvent | Event) => void>>();

    constructor(url: string) {
        this.url = url;
        FakeEventSource.instances.push(this);
    }

    addEventListener(type: string, cb: (ev: MessageEvent | Event) => void): void {
        if (!this.listeners.has(type)) this.listeners.set(type, new Set());
        this.listeners.get(type)!.add(cb);
    }

    close(): void {
        this.closed = true;
        this.readyState = 2;
    }

    emit(type: string, data?: string): void {
        const set = this.listeners.get(type);
        if (!set) return;
        const ev: Event = data !== undefined ? new MessageEvent(type, { data }) : new Event(type);
        for (const cb of set) cb(ev);
    }

    fireOpen(): void {
        this.readyState = 1;
        this.emit('open');
    }

    fireError(): void {
        this.emit('error');
    }
}

function latest(): FakeEventSource {
    const inst = FakeEventSource.instances.at(-1);
    if (!inst) throw new Error('no EventSource constructed');
    return inst;
}

const sampleReport = JSON.stringify({
    slot: { start_utc: '2026-06-07T14:30:15Z', period: 'odd' },
    passband: { low_hz: 200, high_hz: 3000 },
    signal_width_hz: 50,
    occupied: [
        { low_hz: 600, high_hz: 700, source: 'both', level: 0.9 },
        { low_hz: 1400, high_hz: 1600, source: 'energy', level: 0.5 },
    ],
    suggested: [2200, 1800, 760],
});

beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
});

afterEach(() => {
    stopFt8();
    // selectedOffset now persists (localStorage) across stopFt8, so reset it
    // explicitly between tests to keep the singleton from leaking a pick.
    ft8State.selectedOffset = null;
    try {
        localStorage.removeItem('sm.ft8.tx.offset');
    } catch {
        /* ignore */
    }
    vi.unstubAllGlobals();
});

describe('ft8 occupancy transport', () => {
    it('connects on the ft8 events path', () => {
        startFt8();
        expect(latest().url).toBe('/v1/ft8/events');
    });

    it('startFt8 is idempotent — one EventSource', () => {
        startFt8();
        startFt8();
        expect(FakeEventSource.instances).toHaveLength(1);
    });

    it('flips connected on open and back on error', () => {
        startFt8();
        expect(ft8State.connected).toBe(false);
        latest().fireOpen();
        expect(ft8State.connected).toBe(true);
        latest().fireError();
        expect(ft8State.connected).toBe(false);
    });

    it('updates state from an occupancy event', () => {
        startFt8();
        latest().emit('ft8-occupancy', sampleReport);

        expect(ft8State.slot).toEqual({ start_utc: '2026-06-07T14:30:15Z', period: 'odd' });
        expect(ft8State.suggested).toEqual([2200, 1800, 760]);
        expect(ft8State.occupied).toHaveLength(2);
        expect(ft8State.passbandLow).toBe(200);
        expect(ft8State.passbandHigh).toBe(3000);
        expect(ft8State.signalWidth).toBe(50);
    });

    it('treats null occupied/suggested as empty', () => {
        startFt8();
        latest().emit(
            'ft8-occupancy',
            JSON.stringify({
                slot: { start_utc: '2026-06-07T14:30:30Z', period: 'even' },
                passband: { low_hz: 200, high_hz: 3000 },
                signal_width_hz: 50,
                occupied: null,
                suggested: null,
            })
        );
        expect(ft8State.occupied).toEqual([]);
        expect(ft8State.suggested).toEqual([]);
    });

    it('ignores malformed JSON without throwing', () => {
        startFt8();
        latest().emit('ft8-occupancy', '{not json');
        expect(ft8State.slot).toBeNull();
    });

    it('stopFt8 closes the source and resets state', () => {
        startFt8();
        latest().emit('ft8-occupancy', sampleReport);
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:15Z', ['CQ K1ABC FN42']));
        const src = latest();

        stopFt8();
        expect(src.closed).toBe(true);
        expect(_activeSourceForTests()).toBeNull();
        expect(ft8State.slot).toBeNull();
        expect(ft8State.occupied).toEqual([]);
        expect(ft8State.suggested).toEqual([]);
        expect(ft8State.decodes).toEqual([]);
        expect(ft8State.connected).toBe(false);
    });
});

describe('ft8 TX-offset selection', () => {
    it('selectOffset sets the selected offset; re-picking replaces it', () => {
        startFt8();
        expect(ft8State.selectedOffset).toBeNull();
        ft8State.selectOffset(760);
        expect(ft8State.selectedOffset).toBe(760);
        ft8State.selectOffset(1800);
        expect(ft8State.selectedOffset).toBe(1800);
    });

    it('the selected offset survives stopFt8 (persisted, view-leave/return)', () => {
        startFt8();
        ft8State.selectOffset(760);
        stopFt8();
        expect(ft8State.selectedOffset).toBe(760);
    });

    it('persists the selected offset to localStorage', () => {
        ft8State.selectOffset(1800);
        expect(localStorage.getItem('sm.ft8.tx.offset')).toBe('1800');
    });
});

describe('ft8 channelOccupied (selected-channel collision check)', () => {
    it('is null with no offset picked', () => {
        ft8State.selectedOffset = null;
        ft8State.occupied = [{ low_hz: 1000, high_hz: 1050 }];
        expect(ft8State.channelOccupied).toBeNull();
    });

    it('is null when an offset is picked but no occupancy report has arrived', () => {
        // beforeEach's stopFt8() leaves slot === null = "no report yet".
        ft8State.selectedOffset = 1500;
        ft8State.occupied = [];
        expect(ft8State.slot).toBeNull();
        expect(ft8State.channelOccupied).toBeNull();
    });

    // review 2026-06-19 L1: a report that arrived with NO occupied bands is a
    // valid clear slot → false, not "unknown". slot != null distinguishes it from
    // the no-report case above.
    it('is false (clear) when a report arrived with an empty occupied list', () => {
        ft8State.slot = { start_utc: '2026-06-07T14:30:15Z', period: 'odd' };
        ft8State.selectedOffset = 1500;
        ft8State.occupied = [];
        expect(ft8State.channelOccupied).toBe(false);
    });

    it('is false when the channel span clears every occupied band', () => {
        ft8State.slot = { start_utc: '2026-06-07T14:30:15Z', period: 'odd' };
        ft8State.signalWidth = 50;
        ft8State.selectedOffset = 1500; // [1500,1550)
        ft8State.occupied = [
            { low_hz: 1000, high_hz: 1050 },
            { low_hz: 1560, high_hz: 1610 },
        ];
        expect(ft8State.channelOccupied).toBe(false);
    });

    it('is true when the channel span overlaps an occupied band', () => {
        ft8State.slot = { start_utc: '2026-06-07T14:30:15Z', period: 'odd' };
        ft8State.signalWidth = 50;
        ft8State.selectedOffset = 1500; // [1500,1550)
        ft8State.occupied = [{ low_hz: 1540, high_hz: 1590 }];
        expect(ft8State.channelOccupied).toBe(true);
    });

    it('treats touching edges as clear (half-open overlap)', () => {
        ft8State.slot = { start_utc: '2026-06-07T14:30:15Z', period: 'odd' };
        ft8State.signalWidth = 50;
        ft8State.selectedOffset = 1500; // [1500,1550)
        // A band starting exactly at the channel's high edge does not overlap.
        ft8State.occupied = [{ low_hz: 1550, high_hz: 1600 }];
        expect(ft8State.channelOccupied).toBe(false);
    });
});

/** Build a DecodeReport JSON string with the given texts (all at distinct freqs). */
function decodeReport(startUtc: string, texts: string[]): string {
    return JSON.stringify({
        slot: { start_utc: startUtc, period: 'odd' },
        decodes: texts.map((text, i) => ({
            text,
            freq_hz: 1000 + i * 100,
            dt_s: 0.2,
            snr: -10 + i,
        })),
    });
}

describe('ft8 decode feed', () => {
    it('accumulates decodes newest-slot-first', () => {
        startFt8();
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:00Z', ['CQ A', 'CQ B']));
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:15Z', ['CQ C']));

        // Newest slot (14:30:15) on top, then the prior slot's block.
        expect(ft8State.decodes.map((d) => d.text)).toEqual(['CQ C', 'CQ A', 'CQ B']);
        expect(ft8State.decodes[0].startUtc).toBe('2026-06-07T14:30:15Z');
    });

    it('sorts decodes within a slot by ascending frequency', () => {
        startFt8();
        latest().emit(
            'ft8-decode',
            JSON.stringify({
                slot: { start_utc: '2026-06-07T14:30:15Z', period: 'odd' },
                decodes: [
                    { text: 'HIGH', freq_hz: 2400, dt_s: 0.1, snr: 3 },
                    { text: 'LOW', freq_hz: 600, dt_s: 0.1, snr: -18 },
                ],
            })
        );
        expect(ft8State.decodes.map((d) => d.text)).toEqual(['LOW', 'HIGH']);
    });

    it('carries SNR through to the decode entry', () => {
        startFt8();
        latest().emit(
            'ft8-decode',
            JSON.stringify({
                slot: { start_utc: '2026-06-07T14:30:15Z', period: 'odd' },
                decodes: [{ text: 'CQ A', freq_hz: 1000, dt_s: 0.2, snr: -7 }],
            })
        );
        expect(ft8State.decodes[0].snr).toBe(-7);
    });

    it('assigns unique ids and skips empty/null slots', () => {
        startFt8();
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:00Z', ['CQ A']));
        latest().emit(
            'ft8-decode',
            JSON.stringify({ slot: { start_utc: 'x', period: 'odd' }, decodes: [] })
        );
        latest().emit(
            'ft8-decode',
            JSON.stringify({ slot: { start_utc: 'y', period: 'odd' }, decodes: null })
        );
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:15Z', ['CQ B']));

        expect(ft8State.decodes).toHaveLength(2);
        const ids = ft8State.decodes.map((d) => d.id);
        expect(new Set(ids).size).toBe(2);
    });

    it('caps the rolling history', () => {
        startFt8();
        for (let s = 0; s < 60; s++) {
            latest().emit('ft8-decode', decodeReport(`2026-06-07T14:${s}:00Z`, ['A', 'B', 'C']));
        }
        expect(ft8State.decodes.length).toBeLessThanOrEqual(100);
    });

    it('ignores malformed decode JSON', () => {
        startFt8();
        latest().emit('ft8-decode', '{bad');
        expect(ft8State.decodes).toEqual([]);
    });

    it('clearDecodes empties the feed (band-change clear)', () => {
        startFt8();
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:00Z', ['CQ A', 'CQ B']));
        expect(ft8State.decodes.length).toBeGreaterThan(0);
        ft8State.clearDecodes();
        expect(ft8State.decodes).toEqual([]);
    });
});

describe('ft8 tx status (ft8-tx SSE)', () => {
    it('hydrates ft8State.tx from an ft8-tx event', () => {
        startFt8();
        latest().emit(
            'ft8-tx',
            JSON.stringify({
                armed: true,
                transmitting: true,
                message: 'CQ G0XYZ IO91',
                offset_hz: 1500,
                error: '',
            })
        );
        expect(ft8State.tx.armed).toBe(true);
        expect(ft8State.tx.transmitting).toBe(true);
        expect(ft8State.tx.message).toBe('CQ G0XYZ IO91');
        expect(ft8State.tx.offsetHz).toBe(1500);
    });

    it('defaults omitted fields and carries an error code', () => {
        startFt8();
        latest().emit('ft8-tx', JSON.stringify({ armed: true, error: 'ft8_tx_failed' }));
        expect(ft8State.tx.armed).toBe(true);
        expect(ft8State.tx.transmitting).toBe(false);
        expect(ft8State.tx.message).toBe('');
        expect(ft8State.tx.error).toBe('ft8_tx_failed');
    });

    it('stopFt8 resets tx status (daemon replays the truth on reconnect)', () => {
        startFt8();
        latest().emit('ft8-tx', JSON.stringify({ armed: true }));
        stopFt8();
        expect(ft8State.tx.armed).toBe(false);
        expect(ft8State.tx.transmitting).toBe(false);
    });

    it('ignores malformed tx JSON', () => {
        startFt8();
        latest().emit('ft8-tx', '{bad');
        expect(ft8State.tx.armed).toBe(false);
    });
});

describe('ft8 qso status (ft8-qso SSE)', () => {
    it('hydrates ft8State.qso from an ft8-qso event', () => {
        startFt8();
        latest().emit(
            'ft8-qso',
            JSON.stringify({
                active: true,
                role: 'worker',
                their_call: 'K1ABC',
                their_grid: 'FN42',
                state: 'reporting',
                next_message: 'K1ABC G0XYZ R-12',
                repeats: 1,
                max_repeats: 6,
            })
        );
        expect(ft8State.qso.active).toBe(true);
        expect(ft8State.qso.role).toBe('worker');
        expect(ft8State.qso.theirCall).toBe('K1ABC');
        expect(ft8State.qso.theirGrid).toBe('FN42');
        expect(ft8State.qso.state).toBe('reporting');
        expect(ft8State.qso.nextMessage).toBe('K1ABC G0XYZ R-12');
        expect(ft8State.qso.repeats).toBe(1);
        expect(ft8State.qso.maxRepeats).toBe(6);
    });

    it('defaults maxRepeats to 0 when the cap is absent (uncapped/one-shot rung)', () => {
        startFt8();
        latest().emit(
            'ft8-qso',
            JSON.stringify({ active: true, their_call: 'K1ABC', state: 'confirming' })
        );
        expect(ft8State.qso.maxRepeats).toBe(0);
    });

    it('treats an idle event as inactive with defaults', () => {
        startFt8();
        latest().emit('ft8-qso', JSON.stringify({ active: false }));
        expect(ft8State.qso.active).toBe(false);
        expect(ft8State.qso.theirCall).toBe('');
    });

    it('stopFt8 resets qso status', () => {
        startFt8();
        latest().emit('ft8-qso', JSON.stringify({ active: true, their_call: 'K1ABC' }));
        stopFt8();
        expect(ft8State.qso.active).toBe(false);
    });

    it('ignores malformed qso JSON', () => {
        startFt8();
        latest().emit('ft8-qso', '{bad');
        expect(ft8State.qso.active).toBe(false);
    });
});

describe('ft8 logged → session list (ft8-logged SSE)', () => {
    beforeEach(() => sessionQsosState.clear());
    afterEach(() => sessionQsosState.clear());

    const loggedReport = JSON.stringify({
        uuid: 'uuid-1',
        callsign: 'K1ABC',
        freq_hz: 14_075_500,
        band: '20m',
        rst_sent: '-12',
        rst_rcvd: '-10',
        mode: 'FT8',
        time_on: '09:05',
        qso_date: '2026-06-10',
        gridsquare: 'FN42',
        country: 'United States',
    });

    it('adds a completed FT8 QSO to the shared session list', () => {
        startFt8();
        latest().emit('ft8-logged', loggedReport);
        expect(sessionQsosState.count).toBe(1);
        const row = sessionQsosState.items[0];
        expect(row.uuid).toBe('uuid-1');
        expect(row.callsign).toBe('K1ABC');
        expect(row.band).toBe('20m');
        expect(row.mode).toBe('FT8');
        expect(row.freqHz).toBe(14_075_500);
        expect(row.timeOn).toBe('09:05');
        expect(row.rstSent).toBe('-12');
        expect(row.rstRcvd).toBe('-10');
        expect(row.country).toBe('United States');
    });

    it('dedups a repeated uuid (one-shot event, defensive against double-delivery)', () => {
        startFt8();
        latest().emit('ft8-logged', loggedReport);
        latest().emit('ft8-logged', loggedReport);
        expect(sessionQsosState.count).toBe(1);
    });

    it('ignores an event with no uuid (cannot edit/email without it)', () => {
        startFt8();
        latest().emit('ft8-logged', JSON.stringify({ callsign: 'K1ABC' }));
        expect(sessionQsosState.count).toBe(0);
    });

    it('ignores malformed logged JSON', () => {
        startFt8();
        latest().emit('ft8-logged', '{bad');
        expect(sessionQsosState.count).toBe(0);
    });
});

// The row cap + feed mode are daemon-owned settings now (configState.ft8Display);
// the decode handler reads them each slot. These tests drive configState directly
// and restore the defaults after each so they don't leak into the other suites.
describe('ft8 decode history cap (configState.ft8Display.historyMax)', () => {
    afterEach(() => {
        configState.ft8Display.historyMax = 100;
    });

    const manyTexts = (n: number): string[] => Array.from({ length: n }, (_, i) => `C${i}`);

    it('caps newly added decodes to the configured limit', () => {
        startFt8();
        configState.ft8Display.historyMax = 10;
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:00Z', manyTexts(15)));
        expect(ft8State.decodes).toHaveLength(10);
    });
});

describe('ft8 decode feed mode (configState.ft8Display.feedMode)', () => {
    afterEach(() => {
        configState.ft8Display.feedMode = 'accumulate';
    });

    it('defaults to accumulate', () => {
        expect(configState.ft8Display.feedMode).toBe('accumulate');
    });

    it('in single mode, each slot replaces the previous one', () => {
        startFt8();
        configState.ft8Display.feedMode = 'single';
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:00Z', ['CQ A', 'CQ B']));
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:15Z', ['CQ C']));

        expect(ft8State.decodes.map((d) => d.text)).toEqual(['CQ C']);
        expect(ft8State.decodes[0].startUtc).toBe('2026-06-07T14:30:15Z');
    });

    it('in accumulate mode, slots roll up', () => {
        startFt8();
        configState.ft8Display.feedMode = 'accumulate';
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:00Z', ['CQ A', 'CQ B']));
        latest().emit('ft8-decode', decodeReport('2026-06-07T14:30:15Z', ['CQ C']));
        expect(ft8State.decodes).toHaveLength(3);
    });
});
