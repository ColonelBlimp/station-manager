// Transport tests — event routing + parse guards. The state transitions the
// handlers drive are covered in ft8.svelte.test.ts; here the handlers are spies
// and the EventSource is a capture-everything fake (jsdom has none).

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { openFt8Events, type Ft8EventHandlers } from './ft8-sse';

class FakeEventSource {
    static instances: FakeEventSource[] = [];
    listeners = new Map<string, ((ev: MessageEvent<string>) => void)[]>();
    closed = false;

    constructor(public url: string) {
        FakeEventSource.instances.push(this);
    }

    addEventListener(type: string, fn: (ev: MessageEvent<string>) => void): void {
        const list = this.listeners.get(type) ?? [];
        list.push(fn);
        this.listeners.set(type, list);
    }

    close(): void {
        this.closed = true;
    }

    emit(type: string, data?: string): void {
        for (const fn of this.listeners.get(type) ?? []) {
            fn({ data } as MessageEvent<string>);
        }
    }
}

function makeHandlers(): Ft8EventHandlers {
    return {
        onOpen: vi.fn(),
        onError: vi.fn(),
        onOccupancy: vi.fn(),
        onDecode: vi.fn(),
        onTx: vi.fn(),
        onQso: vi.fn(),
        onLogged: vi.fn(),
    };
}

beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
});

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

describe('openFt8Events', () => {
    it('opens the daemon-relative stream and routes each parsed event', () => {
        const h = makeHandlers();
        openFt8Events(h);
        const src = FakeEventSource.instances[0];
        expect(src.url).toBe('/v1/ft8/events');

        src.emit('open');
        expect(h.onOpen).toHaveBeenCalledOnce();

        src.emit(
            'ft8-decode',
            '{"slot":{"start_utc":"2026-07-09T14:30:15Z","period":"even"},"decodes":null}'
        );
        expect(h.onDecode).toHaveBeenCalledWith({
            slot: { start_utc: '2026-07-09T14:30:15Z', period: 'even' },
            decodes: null,
        });

        src.emit(
            'ft8-occupancy',
            '{"slot":{"start_utc":"x","period":"odd"},"passband":{"low_hz":200,"high_hz":3000},"signal_width_hz":50,"occupied":[],"suggested":[1500]}'
        );
        expect(h.onOccupancy).toHaveBeenCalledOnce();

        src.emit('ft8-tx', '{"armed":true,"transmitting":false}');
        expect(h.onTx).toHaveBeenCalledWith({ armed: true, transmitting: false });

        src.emit('ft8-qso', '{"active":true,"role":"answerer","their_call":"PJ4/NA2AA"}');
        expect(h.onQso).toHaveBeenCalledOnce();

        src.emit('ft8-logged', '{"uuid":"u-1","callsign":"PJ4/NA2AA"}');
        expect(h.onLogged).toHaveBeenCalledWith({ uuid: 'u-1', callsign: 'PJ4/NA2AA' });

        src.emit('error');
        expect(h.onError).toHaveBeenCalledOnce();
    });

    it('drops malformed JSON without calling the handler', () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const h = makeHandlers();
        openFt8Events(h);
        FakeEventSource.instances[0].emit('ft8-decode', '{not json');
        expect(h.onDecode).not.toHaveBeenCalled();
        expect(warn).toHaveBeenCalled();
    });

    it('returns a close function that tears the source down', () => {
        const close = openFt8Events(makeHandlers());
        close();
        expect(FakeEventSource.instances[0].closed).toBe(true);
    });
});
