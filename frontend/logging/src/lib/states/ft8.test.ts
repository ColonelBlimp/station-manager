import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { _activeSourceForTests, ft8State, startFt8, stopFt8 } from './ft8.svelte';

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
        expect(ft8State.busyCount).toBe(2);
        expect(ft8State.suggested).toEqual([2200, 1800, 760]);
        expect(ft8State.occupied).toHaveLength(2);
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
        expect(ft8State.busyCount).toBe(0);
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
        expect(ft8State.busyCount).toBe(0);
        expect(ft8State.suggested).toEqual([]);
        expect(ft8State.decodes).toEqual([]);
        expect(ft8State.connected).toBe(false);
    });
});

/** Build a DecodeReport JSON string with the given texts (all at distinct freqs). */
function decodeReport(startUtc: string, texts: string[]): string {
    return JSON.stringify({
        slot: { start_utc: startUtc, period: 'odd' },
        decodes: texts.map((text, i) => ({ text, freq_hz: 1000 + i * 100, dt_s: 0.2 })),
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
                    { text: 'HIGH', freq_hz: 2400, dt_s: 0.1 },
                    { text: 'LOW', freq_hz: 600, dt_s: 0.1 },
                ],
            })
        );
        expect(ft8State.decodes.map((d) => d.text)).toEqual(['LOW', 'HIGH']);
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
});
