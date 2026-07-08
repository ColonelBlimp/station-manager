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
