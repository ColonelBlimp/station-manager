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
        onAudioLevel: vi.fn(),
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

// F-03: valid JSON with the wrong shape — including a valid array of malformed elements — is
// dropped, so a consumer never throws while spreading/#each-ing a non-array and no malformed
// ft8-logged creates a phantom session row. Drops warn once per (event, reason) per subscription.
describe('openFt8Events — wrong-shape frames are dropped (F-03)', () => {
    const GOOD_SLOT = '{"start_utc":"2026-07-09T14:30:15Z","period":"even"}';
    // Each call opens its OWN source and emits through THAT one (the newest), then closes it,
    // so a test that calls emit() more than once asserts against the handler set actually wired
    // to the source it dispatched through — not instances[0], whose listeners belong to the
    // first call's handlers (which would make every later assertion a false positive).
    function emit(type: string, data: string): Ft8EventHandlers {
        const h = makeHandlers();
        const close = openFt8Events(h);
        FakeEventSource.instances[FakeEventSource.instances.length - 1].emit(type, data);
        close();
        return h;
    }

    it('drops ft8-decode whose decodes is a non-array (would throw when spread)', () => {
        expect(
            emit('ft8-decode', `{"slot":${GOOD_SLOT},"decodes":{}}`).onDecode
        ).not.toHaveBeenCalled();
    });

    it('drops ft8-decode with an invalid slot', () => {
        expect(emit('ft8-decode', '{"slot":{},"decodes":null}').onDecode).not.toHaveBeenCalled();
    });

    // The slot period is exactly even|odd on the wire (the WSJT-X parity convention).
    it('drops ft8-decode whose slot period is neither even nor odd', () => {
        const bad = '{"slot":{"start_utc":"x","period":"quarter"},"decodes":null}';
        expect(emit('ft8-decode', bad).onDecode).not.toHaveBeenCalled();
    });

    it('drops ft8-decode with a malformed decode line (missing freq_hz)', () => {
        const bad = `{"slot":${GOOD_SLOT},"decodes":[{"text":"CQ","dt_s":0.1,"snr":-5}]}`;
        expect(emit('ft8-decode', bad).onDecode).not.toHaveBeenCalled();
    });

    it('drops ft8-occupancy whose occupied is a non-array', () => {
        const bad = `{"slot":${GOOD_SLOT},"passband":{"low_hz":200,"high_hz":3000},"signal_width_hz":50,"occupied":{},"suggested":null}`;
        expect(emit('ft8-occupancy', bad).onOccupancy).not.toHaveBeenCalled();
    });

    it('drops ft8-tx whose transmitting is not a boolean (safety)', () => {
        expect(emit('ft8-tx', '{"armed":false,"transmitting":"yes"}').onTx).not.toHaveBeenCalled();
    });

    // armed and transmitting are always on the wire; a frame omitting them must NOT dispatch,
    // or the consumer's `?? false` would silently clear a live arm/transmit to disarmed/idle.
    it('drops ft8-tx that omits armed or transmitting (would falsely clear TX state)', () => {
        expect(emit('ft8-tx', '{}').onTx).not.toHaveBeenCalled();
        expect(emit('ft8-tx', '{"armed":true}').onTx).not.toHaveBeenCalled();
        expect(emit('ft8-tx', '{"transmitting":true}').onTx).not.toHaveBeenCalled();
    });

    it('accepts an idle ft8-tx (armed:false, transmitting:false)', () => {
        expect(emit('ft8-tx', '{"armed":false,"transmitting":false}').onTx).toHaveBeenCalledWith({
            armed: false,
            transmitting: false,
        });
    });

    it('drops ft8-qso whose active is not a boolean', () => {
        expect(emit('ft8-qso', '{"active":"true"}').onQso).not.toHaveBeenCalled();
    });

    // active is always on the wire; a frame omitting it must NOT dispatch, or the consumer's
    // `?? false` would clear a live session to idle.
    it('drops ft8-qso that omits active (would falsely clear the session)', () => {
        expect(emit('ft8-qso', '{"role":"answerer"}').onQso).not.toHaveBeenCalled();
    });

    // their_call and end_reason are consumed via .trim(), which THROWS on a non-string; a frame
    // carrying either as an object must be dropped before it reaches the consumer.
    it('drops ft8-qso whose their_call or end_reason is not a string (would throw on .trim)', () => {
        expect(emit('ft8-qso', '{"active":true,"their_call":{}}').onQso).not.toHaveBeenCalled();
        expect(emit('ft8-qso', '{"active":false,"end_reason":{}}').onQso).not.toHaveBeenCalled();
    });

    it('drops ft8-qso whose answerers is a non-array or has malformed elements (would throw in the pileup)', () => {
        expect(emit('ft8-qso', '{"active":true,"answerers":{}}').onQso).not.toHaveBeenCalled();
        expect(
            emit('ft8-qso', '{"active":true,"answerers":[{"call":"X"}]}').onQso
        ).not.toHaveBeenCalled();
    });

    it('drops ft8-audio-level with a non-numeric level (TX stand-down safety)', () => {
        expect(
            emit('ft8-audio-level', '{"peak_dbfs":"x","rms_dbfs":0}').onAudioLevel
        ).not.toHaveBeenCalled();
    });

    it('accepts a valid ft8-audio-level', () => {
        expect(
            emit('ft8-audio-level', '{"peak_dbfs":-6,"rms_dbfs":-18}').onAudioLevel
        ).toHaveBeenCalledWith({ peak_dbfs: -6, rms_dbfs: -18 });
    });

    // ft8-logged is a success boundary: a malformed frame must not create a phantom session row.
    it('drops ft8-logged without a non-empty uuid and usable callsign', () => {
        expect(emit('ft8-logged', '{"callsign":"K1ABC"}').onLogged).not.toHaveBeenCalled();
        expect(
            emit('ft8-logged', '{"uuid":"","callsign":"K1ABC"}').onLogged
        ).not.toHaveBeenCalled();
        expect(emit('ft8-logged', '{"uuid":"u-1","callsign":""}').onLogged).not.toHaveBeenCalled();
        expect(emit('ft8-logged', '{"uuid":"u-1"}').onLogged).not.toHaveBeenCalled();
    });

    // Whitespace-only is not usable: a blank uuid can't dedup a session row, a blank callsign
    // can't key one.
    it('drops ft8-logged whose uuid or callsign is whitespace-only', () => {
        expect(
            emit('ft8-logged', '{"uuid":"   ","callsign":"K1ABC"}').onLogged
        ).not.toHaveBeenCalled();
        expect(
            emit('ft8-logged', '{"uuid":"u-1","callsign":"  "}').onLogged
        ).not.toHaveBeenCalled();
    });

    it('accepts a valid ft8-logged', () => {
        expect(
            emit('ft8-logged', '{"uuid":"u-1","callsign":"K1ABC"}').onLogged
        ).toHaveBeenCalledWith({
            uuid: 'u-1',
            callsign: 'K1ABC',
        });
    });

    it('warns once per (event, reason) per subscription', () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const h = makeHandlers();
        openFt8Events(h);
        const src = FakeEventSource.instances[0];
        src.emit('ft8-tx', '{"transmitting":"x"}');
        src.emit('ft8-tx', '{"transmitting":1}');
        expect(warn).toHaveBeenCalledTimes(1);
    });
});
