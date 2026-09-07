import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { decodeQsoEvent, openLogEvents } from './log-events';

// AW-1 alpha.2: the qso.* event decode is additive — it prefers the canonical qso_uuid but
// still accepts a legacy qso_id-only event, and rejects one carrying neither identifier.
// logbook_id must stay a number (the contacts map keys on it). alpha.3 removes the
// qso_id-only fallback.
describe('decodeQsoEvent (AW-1 alpha.2 additive decode)', () => {
    it('accepts a uuid-only payload (qso_uuid preferred, no qso_id)', () => {
        const p = decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1', logbook_id: 3 }));
        expect(p).not.toBeNull();
        expect(p?.qso_uuid).toBe('u-1');
    });

    it('tolerates a legacy qso_id-only payload during alpha.2', () => {
        const p = decodeQsoEvent(JSON.stringify({ qso_id: 7, logbook_id: 3 }));
        expect(p).not.toBeNull();
        expect(p?.qso_id).toBe(7);
    });

    it('accepts a payload carrying both ids', () => {
        const p = decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1', qso_id: 7, logbook_id: 3 }));
        expect(p).not.toBeNull();
    });

    it('rejects a payload with neither identifier', () => {
        expect(decodeQsoEvent(JSON.stringify({ logbook_id: 3 }))).toBeNull();
    });

    it('rejects an empty qso_uuid as an identifier', () => {
        expect(decodeQsoEvent(JSON.stringify({ qso_uuid: '', logbook_id: 3 }))).toBeNull();
    });

    it('rejects a non-numeric or absent logbook_id (the map keys on it)', () => {
        expect(decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1', logbook_id: '3' }))).toBeNull();
        expect(decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1' }))).toBeNull();
    });

    it('returns null on invalid JSON', () => {
        expect(decodeQsoEvent('{not json')).toBeNull();
    });

    it('returns null for valid JSON that is not a payload object', () => {
        expect(decodeQsoEvent('null')).toBeNull();
    });
});

// Transport-specific reconnection callback (ADR 0079, dogfood Finding #16): fires
// once per error → reopen transition, never on the boot open, never again on a
// reopen with no new error. The production-boundary pin for the header count that
// consumes it lives in src/main.boot.test.ts.
class FakeEventSource {
    static instances: FakeEventSource[] = [];
    listeners = new Map<string, ((ev: MessageEvent<string>) => void)[]>();
    readyState = 0;
    constructor(public url: string) {
        FakeEventSource.instances.push(this);
    }
    addEventListener(type: string, fn: (ev: MessageEvent<string>) => void): void {
        const list = this.listeners.get(type) ?? [];
        list.push(fn);
        this.listeners.set(type, list);
    }
    close(): void {
        this.readyState = 2;
    }
    emit(type: string, data?: string): void {
        if (type === 'open') this.readyState = 1;
        if (type === 'error') this.readyState = 0; // CONNECTING: the browser is retrying
        for (const fn of this.listeners.get(type) ?? []) fn({ data } as MessageEvent<string>);
    }
}

describe('openLogEvents onReconnect', () => {
    beforeEach(() => {
        FakeEventSource.instances = [];
        vi.stubGlobal('EventSource', FakeEventSource);
    });
    afterEach(() => {
        vi.unstubAllGlobals();
    });

    function open() {
        const h = {
            onOpen: vi.fn(),
            onTransportError: vi.fn(),
            onQsoChanged: vi.fn(),
            onReconnect: vi.fn(),
        };
        const close = openLogEvents(h);
        return { h, close, src: FakeEventSource.instances[0] };
    }

    it('does not fire on the boot open', () => {
        const { h, src, close } = open();
        src.emit('open');
        expect(h.onOpen).toHaveBeenCalledOnce();
        expect(h.onReconnect).not.toHaveBeenCalled();
        close();
    });

    it('fires exactly once after an error → reopen transition', () => {
        const { h, src, close } = open();
        src.emit('open');
        src.emit('error');
        src.emit('open');
        expect(h.onTransportError).toHaveBeenCalledOnce();
        expect(h.onReconnect).toHaveBeenCalledOnce();
        close();
    });

    it('does not fire again on a reopen with no new error', () => {
        const { h, src, close } = open();
        src.emit('open');
        src.emit('error');
        src.emit('open');
        src.emit('open');
        expect(h.onReconnect).toHaveBeenCalledOnce();
        close();
    });

    it('counts a drop followed by an openReviving replacement stream as one transition', () => {
        // The drop half lands on the ORIGINAL source; openReviving then replaces it
        // (a stream left CONNECTING after an error is revived when the tab is
        // visible, sse-reviving V4) and the reopen half lands on the REPLACEMENT.
        // The flag must therefore live in the subscription, not the wire closure —
        // a per-source flag would start false on the new source and miss this.
        const { h, close } = open();
        const first = FakeEventSource.instances[0];
        first.emit('open');
        first.emit('error'); // dropped, retrying, never recovers
        Object.defineProperty(document, 'visibilityState', {
            configurable: true,
            get: () => 'visible',
        });
        document.dispatchEvent(new Event('visibilitychange'));
        expect(FakeEventSource.instances).toHaveLength(2);
        const replacement = FakeEventSource.instances[1];
        replacement.emit('open');
        expect(h.onReconnect).toHaveBeenCalledOnce();
        replacement.emit('open');
        expect(h.onReconnect).toHaveBeenCalledOnce();
        close();
    });

    it('is optional: a subscriber without it survives a transition', () => {
        const h = { onOpen: vi.fn(), onTransportError: vi.fn(), onQsoChanged: vi.fn() };
        const close = openLogEvents(h);
        const src = FakeEventSource.instances[0];
        expect(() => {
            src.emit('open');
            src.emit('error');
            src.emit('open');
        }).not.toThrow();
        close();
    });
});
