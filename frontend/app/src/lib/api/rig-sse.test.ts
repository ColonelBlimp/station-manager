// Transport tests — event routing + parse guards. The state transitions the
// handlers drive are covered in rig.svelte.test.ts; here the handlers are
// spies and the EventSource is a capture-everything fake (jsdom has none).

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { openRigEvents, type RigEventHandlers } from './rig-sse';

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

function makeHandlers(): RigEventHandlers {
    return {
        onOpen: vi.fn(),
        onTransportError: vi.fn(),
        onRigState: vi.fn(),
        onRigDisconnected: vi.fn(),
        onBridgeError: vi.fn(),
        onTuneState: vi.fn(),
        onTxAlarm: vi.fn(),
        onDriveAlarm: vi.fn(),
        onRigMeters: vi.fn(),
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

describe('openRigEvents', () => {
    it('opens the daemon-relative stream and routes parsed payloads', () => {
        const h = makeHandlers();
        openRigEvents(h);

        const src = FakeEventSource.instances[0];
        expect(src.url).toBe('/v1/rig/events');

        src.emit('open');
        expect(h.onOpen).toHaveBeenCalledOnce();

        src.emit('rig-state', '{"vfoA":14255000,"mode":"USB"}');
        expect(h.onRigState).toHaveBeenCalledWith({ vfoA: 14255000, mode: 'USB' });

        src.emit('rig-disconnected', '{"code":"rig_no_data"}');
        expect(h.onRigDisconnected).toHaveBeenCalledWith({ code: 'rig_no_data' });

        src.emit('bridge-error', '{"code":"port_permission","details":{"port":"/dev/ttyUSB0"}}');
        expect(h.onBridgeError).toHaveBeenCalledWith({
            code: 'port_permission',
            details: { port: '/dev/ttyUSB0' },
        });

        src.emit('tune-state', '{"active":true}');
        expect(h.onTuneState).toHaveBeenCalledWith({ active: true });

        src.emit('tx-alarm', '{"active":true,"code":"tx_unconfirmed"}');
        expect(h.onTxAlarm).toHaveBeenCalledWith({ active: true, code: 'tx_unconfirmed' });

        // The drive alarm is a SEPARATE event, so it must be listened for
        // separately: EventSource delivers only the named types registered, and
        // an unregistered event is dropped silently in the browser with nothing
        // to show it happened.
        src.emit('drive-alarm', '{"active":true,"code":"drive_no_output"}');
        expect(h.onDriveAlarm).toHaveBeenCalledWith({ active: true, code: 'drive_no_output' });
        expect(h.onTxAlarm).toHaveBeenCalledTimes(1);

        src.emit('rig-meters', '{"meter":"ALC","value":26}');
        expect(h.onRigMeters).toHaveBeenCalledWith({ meter: 'ALC', value: 26 });

        src.emit('error');
        expect(h.onTransportError).toHaveBeenCalledOnce();
    });

    it('drops malformed JSON without calling the handler', () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const h = makeHandlers();
        openRigEvents(h);

        FakeEventSource.instances[0].emit('rig-state', '{not json');
        expect(h.onRigState).not.toHaveBeenCalled();
        expect(warn).toHaveBeenCalled();
    });

    it('returns a close function that tears the source down', () => {
        const close = openRigEvents(makeHandlers());
        close();
        expect(FakeEventSource.instances[0].closed).toBe(true);
    });
});

// F-03: valid JSON with the wrong shape must be dropped, leaving the last known good state —
// never dispatched to a handler (which would silently corrupt a safety state) and never
// thrown. Each drop warns once per (event, reason) per subscription.
describe('openRigEvents — wrong-shape frames are dropped (F-03)', () => {
    // Each call opens its OWN source and emits through THAT one (the newest), then closes it,
    // so a test that calls emit() more than once asserts against the handler set actually wired
    // to the source it dispatched through — not instances[0], whose listeners belong to the
    // first call's handlers (which would make every later assertion a false positive).
    function emit(type: string, data: string): RigEventHandlers {
        const h = makeHandlers();
        const close = openRigEvents(h);
        FakeEventSource.instances[FakeEventSource.instances.length - 1].emit(type, data);
        close();
        return h;
    }

    it('drops a tune-state whose active is not a boolean (tune state unchanged)', () => {
        expect(emit('tune-state', '{"active":"true"}').onTuneState).not.toHaveBeenCalled();
    });

    it('drops a tx-alarm whose active is not a boolean (the safety alarm is never falsely set/cleared)', () => {
        expect(emit('tx-alarm', '{"active":0}').onTxAlarm).not.toHaveBeenCalled();
    });

    it('drops a drive-alarm with a non-boolean active', () => {
        expect(emit('drive-alarm', '{"active":"x"}').onDriveAlarm).not.toHaveBeenCalled();
    });

    it('drops a rig-state whose vfoA is a string (never a wrong logged frequency)', () => {
        expect(emit('rig-state', '{"vfoA":"14255000"}').onRigState).not.toHaveBeenCalled();
    });

    it('drops a rig-state whose mode is numeric, or vfoB/selectedVfo are wrong-typed', () => {
        expect(emit('rig-state', '{"mode":5}').onRigState).not.toHaveBeenCalled();
        expect(emit('rig-state', '{"vfoB":"x"}').onRigState).not.toHaveBeenCalled();
        expect(emit('rig-state', '{"selectedVfo":1}').onRigState).not.toHaveBeenCalled();
    });

    // selectedVfo is exactly "A" | "B" on the wire; any other value would corrupt the
    // consumer's 'A' | 'B'-typed field, so a valid-JSON string like "C" is still dropped.
    it('drops a rig-state whose selectedVfo is a string other than A or B', () => {
        expect(emit('rig-state', '{"selectedVfo":"C"}').onRigState).not.toHaveBeenCalled();
        expect(emit('rig-state', '{"selectedVfo":"B"}').onRigState).toHaveBeenCalledWith({
            selectedVfo: 'B',
        });
    });

    // An empty or unknown-only frame carries no state this build models — dropped, not merged
    // as a silent no-op (the embedded SPA ships with its daemon; a real frame has a known key).
    it('drops an empty or unknown-only rig-state frame', () => {
        expect(emit('rig-state', '{}').onRigState).not.toHaveBeenCalled();
        expect(emit('rig-state', '{"someFutureField":5}').onRigState).not.toHaveBeenCalled();
    });

    it('drops a rig-meters whose value is a string', () => {
        expect(
            emit('rig-meters', '{"meter":"ALC","value":"26"}').onRigMeters
        ).not.toHaveBeenCalled();
    });

    // A meter poll is a (meter, value) pair the daemon always sends whole; either half missing
    // is meaningless, so the frame is dropped rather than dispatched with a defaulted half.
    it('drops a rig-meters that omits meter or value', () => {
        expect(emit('rig-meters', '{"meter":"ALC"}').onRigMeters).not.toHaveBeenCalled();
        expect(emit('rig-meters', '{"value":26}').onRigMeters).not.toHaveBeenCalled();
    });

    it('drops a bridge-error / rig-disconnected with a non-string code', () => {
        expect(emit('bridge-error', '{"code":5}').onBridgeError).not.toHaveBeenCalled();
        expect(emit('rig-disconnected', '{}').onRigDisconnected).not.toHaveBeenCalled();
    });

    it('drops a non-object frame (array or primitive)', () => {
        const h = makeHandlers();
        openRigEvents(h);
        const src = FakeEventSource.instances[0];
        src.emit('rig-state', '[]');
        src.emit('tune-state', '5');
        expect(h.onRigState).not.toHaveBeenCalled();
        expect(h.onTuneState).not.toHaveBeenCalled();
    });

    it('warns once per (event, reason) per subscription, and still dispatches a later valid frame', () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const h = makeHandlers();
        openRigEvents(h);
        const src = FakeEventSource.instances[0];
        src.emit('tune-state', '{"active":"true"}'); // wrong shape → 1 warn
        src.emit('tune-state', '{"active":"false"}'); // same (event, reason) → no second warn
        expect(warn).toHaveBeenCalledTimes(1);
        src.emit('tune-state', '{"active":true}'); // valid → dispatched
        expect(h.onTuneState).toHaveBeenCalledWith({ active: true });
    });

    it('resets the warn throttle on a fresh subscription (close/reopen)', () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const close = openRigEvents(makeHandlers());
        FakeEventSource.instances[0].emit('tune-state', '{"active":"true"}');
        close();
        openRigEvents(makeHandlers()); // new subscription → throttle reset
        FakeEventSource.instances[1].emit('tune-state', '{"active":"true"}');
        expect(warn).toHaveBeenCalledTimes(2);
    });
});
