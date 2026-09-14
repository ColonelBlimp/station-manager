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

// ONE stream per tab (ADR 0079 dated update 2026-09-14, dogfood 2026-09-11/13):
// every openLogEvents call in a tab rides the same EventSource. The Map view used
// to open its own /v1/events beside the shell's always-on one, and with the
// Operate tab's three streams that made six long-lived connections — the
// browser's per-host cap — so the map's own data request never left the browser
// ("Cannot reach the daemon"). Subscribers are ref-counted: the connection opens
// with the first, is left alone while any remain, and closes with the last.
describe('openLogEvents shares one connection per tab', () => {
    beforeEach(() => {
        FakeEventSource.instances = [];
        vi.stubGlobal('EventSource', FakeEventSource);
    });
    afterEach(() => {
        vi.unstubAllGlobals();
    });

    function handlers() {
        return {
            onOpen: vi.fn(),
            onTransportError: vi.fn(),
            onQsoChanged: vi.fn(),
            onReconnect: vi.fn(),
        };
    }

    it('a second subscriber opens no second EventSource and both receive the events', () => {
        const a = handlers();
        const b = handlers();
        const closeA = openLogEvents(a);
        const closeB = openLogEvents(b);
        expect(FakeEventSource.instances).toHaveLength(1);

        const src = FakeEventSource.instances[0];
        src.emit('open');
        src.emit('qso.stored', JSON.stringify({ qso_uuid: 'u-1', logbook_id: 3 }));
        expect(a.onOpen).toHaveBeenCalledOnce();
        expect(b.onOpen).toHaveBeenCalledOnce();
        expect(a.onQsoChanged).toHaveBeenCalledWith('qso.stored', {
            qso_uuid: 'u-1',
            logbook_id: 3,
        });
        expect(b.onQsoChanged).toHaveBeenCalledWith('qso.stored', {
            qso_uuid: 'u-1',
            logbook_id: 3,
        });
        closeA();
        closeB();
    });

    it('a subscriber joining an OPEN stream is told so at once — the map schedules its catch-up on onOpen', () => {
        const shell = handlers();
        const closeShell = openLogEvents(shell);
        FakeEventSource.instances[0].emit('open');

        const map = handlers();
        const closeMap = openLogEvents(map);
        expect(FakeEventSource.instances).toHaveLength(1);
        expect(map.onOpen).toHaveBeenCalledOnce();
        expect(map.onReconnect).not.toHaveBeenCalled();
        closeMap();
        closeShell();
    });

    it('a subscriber joining a stream that is down is not told open until it reopens', () => {
        const shell = handlers();
        const closeShell = openLogEvents(shell);
        const src = FakeEventSource.instances[0];
        src.emit('open');
        src.emit('error');

        const map = handlers();
        const closeMap = openLogEvents(map);
        expect(map.onOpen).not.toHaveBeenCalled();
        src.emit('open');
        expect(map.onOpen).toHaveBeenCalledOnce();
        // The reconnection transition belongs to the subscriber that saw the
        // drop: the shell re-fetches its count; the newcomer saw no error.
        expect(shell.onReconnect).toHaveBeenCalledOnce();
        expect(map.onReconnect).not.toHaveBeenCalled();
        closeMap();
        closeShell();
    });

    it('closing one subscriber leaves the stream up for the rest; the last close tears it down', () => {
        const shell = handlers();
        const map = handlers();
        const closeShell = openLogEvents(shell);
        const closeMap = openLogEvents(map);
        const src = FakeEventSource.instances[0];
        src.emit('open');

        closeMap(); // the Map view unmounts (route change) — the shell keeps its stream
        expect(src.readyState).toBe(1);
        src.emit('qso.deleted', JSON.stringify({ qso_uuid: 'u-2', logbook_id: 3 }));
        expect(shell.onQsoChanged).toHaveBeenCalledOnce();
        expect(map.onQsoChanged).not.toHaveBeenCalled();
        closeMap(); // idempotent

        closeShell();
        expect(src.readyState).toBe(2);
        // A later subscriber gets a fresh connection, not the closed one.
        const later = handlers();
        const closeLater = openLogEvents(later);
        expect(FakeEventSource.instances).toHaveLength(2);
        closeLater();
    });
});
