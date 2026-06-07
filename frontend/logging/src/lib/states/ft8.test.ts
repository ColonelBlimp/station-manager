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
        const src = latest();

        stopFt8();
        expect(src.closed).toBe(true);
        expect(_activeSourceForTests()).toBeNull();
        expect(ft8State.slot).toBeNull();
        expect(ft8State.busyCount).toBe(0);
        expect(ft8State.suggested).toEqual([]);
        expect(ft8State.connected).toBe(false);
    });
});
