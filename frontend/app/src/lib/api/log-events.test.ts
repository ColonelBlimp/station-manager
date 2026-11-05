import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { decodeQsoEvent, decodeQsoEventValue, openLogEvents } from './log-events';
import { FakeWebSocket } from '../../test/fakeSocket';
import { SOCKET_RETRY_MS } from './socket-reviving';

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

// The WebSocket transport delivers an already-parsed value; the string decoder
// above is JSON.parse then this same value-level validator (one implementation,
// two entries — ADR 0099 W1 envelope adaptation).
describe('decodeQsoEventValue (the value-level entry the socket transport uses)', () => {
    it('accepts the same payloads as the string decoder', () => {
        expect(decodeQsoEventValue({ qso_uuid: 'u-1', logbook_id: 3 })?.qso_uuid).toBe('u-1');
        expect(decodeQsoEventValue({ qso_id: 7, logbook_id: 3 })?.qso_id).toBe(7);
    });
    it('rejects the same payloads as the string decoder', () => {
        expect(decodeQsoEventValue({ logbook_id: 3 })).toBeNull();
        expect(decodeQsoEventValue({ qso_uuid: '', logbook_id: 3 })).toBeNull();
        expect(decodeQsoEventValue({ qso_uuid: 'u-1', logbook_id: '3' })).toBeNull();
        expect(decodeQsoEventValue(null)).toBeNull();
        expect(decodeQsoEventValue([])).toBeNull();
        expect(decodeQsoEventValue('{"qso_uuid":"u-1","logbook_id":3}')).toBeNull(); // a string is not parsed again
    });
});

// Transport-specific reconnection callback (ADR 0079, dogfood Finding #16): fires
// once per error → reopen transition, never on the boot open, never again on a
// reopen with no new error. The production-boundary pin for the header count that
// consumes it lives in src/main.boot.test.ts. Since ADR 0099 W1 the transport is a
// WebSocket: a reopen arrives on a NEW socket after the fixed retry.
const latest = (): FakeWebSocket => FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
/** Drop the current socket and let the fixed retry open the next one. */
function bounce(): FakeWebSocket {
    latest().emit('error');
    vi.advanceTimersByTime(SOCKET_RETRY_MS);
    return latest();
}

describe('openLogEvents onReconnect', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        FakeWebSocket.instances = [];
        vi.stubGlobal('WebSocket', FakeWebSocket);
    });
    afterEach(() => {
        vi.useRealTimers();
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
        return { h, close, src: FakeWebSocket.instances[0] };
    }

    it('opens the shell stream on the WebSocket twin of /v1/events', () => {
        const { close, src } = open();
        expect(src.path).toBe('/v1/events/ws');
        close();
    });

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
        bounce().emit('open');
        expect(h.onTransportError).toHaveBeenCalledOnce();
        expect(h.onReconnect).toHaveBeenCalledOnce();
        close();
    });

    it('does not fire again on a reopen with no new error', () => {
        const { h, src, close } = open();
        src.emit('open');
        const next = bounce();
        next.emit('open');
        next.emit('open');
        expect(h.onReconnect).toHaveBeenCalledOnce();
        close();
    });

    it('counts a drop followed by a visibility-revived replacement socket as one transition', () => {
        // The drop half lands on the ORIGINAL socket; becoming visible brings the
        // waiting retry forward and the reopen half lands on the REPLACEMENT. The
        // flag must therefore live in the subscription, not the wire closure.
        const { h, close } = open();
        const first = FakeWebSocket.instances[0];
        first.emit('open');
        first.emit('error');
        Object.defineProperty(document, 'visibilityState', {
            configurable: true,
            get: () => 'visible',
        });
        document.dispatchEvent(new Event('visibilitychange'));
        expect(FakeWebSocket.instances).toHaveLength(2);
        const replacement = FakeWebSocket.instances[1];
        replacement.emit('open');
        expect(h.onReconnect).toHaveBeenCalledOnce();
        replacement.emit('open');
        expect(h.onReconnect).toHaveBeenCalledOnce();
        close();
    });

    it('is optional: a subscriber without it survives a transition', () => {
        const h = { onOpen: vi.fn(), onTransportError: vi.fn(), onQsoChanged: vi.fn() };
        const close = openLogEvents(h);
        const src = FakeWebSocket.instances[0];
        expect(() => {
            src.emit('open');
            bounce().emit('open');
        }).not.toThrow();
        close();
    });

    it('drops a rejected qso.* payload with a warning and delivers the next valid one', () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        const { h, src, close } = open();
        src.emit('open');
        src.emit('qso.stored', JSON.stringify({ logbook_id: 3 }));
        expect(h.onQsoChanged).not.toHaveBeenCalled();
        expect(warn).toHaveBeenCalled();
        src.emit('qso.updated', JSON.stringify({ qso_uuid: 'u-9', logbook_id: 3 }));
        expect(h.onQsoChanged).toHaveBeenCalledWith('qso.updated', {
            qso_uuid: 'u-9',
            logbook_id: 3,
        });
        src.emit('forward.succeeded', JSON.stringify({ qso_uuid: 'u-9', logbook_id: 3 }));
        expect(h.onQsoChanged).toHaveBeenCalledOnce(); // forward.* is not a qso.* event here
        warn.mockRestore();
        close();
    });
});

// ONE stream per tab (ADR 0079 dated update 2026-09-14, dogfood 2026-09-11/13):
// every openLogEvents call in a tab rides the same connection. Subscribers are
// ref-counted: the connection opens with the first, is left alone while any
// remain, and closes with the last.
describe('openLogEvents shares one connection per tab', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        FakeWebSocket.instances = [];
        vi.stubGlobal('WebSocket', FakeWebSocket);
    });
    afterEach(() => {
        vi.useRealTimers();
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

    it('a second subscriber opens no second connection and both receive the events', () => {
        const a = handlers();
        const b = handlers();
        const closeA = openLogEvents(a);
        const closeB = openLogEvents(b);
        expect(FakeWebSocket.instances).toHaveLength(1);

        const src = FakeWebSocket.instances[0];
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
        FakeWebSocket.instances[0].emit('open');

        const map = handlers();
        const closeMap = openLogEvents(map);
        expect(FakeWebSocket.instances).toHaveLength(1);
        expect(map.onOpen).toHaveBeenCalledOnce();
        expect(map.onReconnect).not.toHaveBeenCalled();
        closeMap();
        closeShell();
    });

    it('a subscriber joining a stream that is down is not told open until it reopens', () => {
        const shell = handlers();
        const closeShell = openLogEvents(shell);
        const src = FakeWebSocket.instances[0];
        src.emit('open');
        src.emit('error');

        const map = handlers();
        const closeMap = openLogEvents(map);
        expect(map.onOpen).not.toHaveBeenCalled();
        vi.advanceTimersByTime(SOCKET_RETRY_MS);
        latest().emit('open');
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
        const src = FakeWebSocket.instances[0];
        src.emit('open');

        closeMap(); // the Map view unmounts (route change) — the shell keeps its stream
        expect(src.closeCalls).toBe(0);
        src.emit('qso.deleted', JSON.stringify({ qso_uuid: 'u-2', logbook_id: 3 }));
        expect(shell.onQsoChanged).toHaveBeenCalledOnce();
        expect(map.onQsoChanged).not.toHaveBeenCalled();
        closeMap(); // idempotent

        closeShell();
        expect(src.closeCalls).toBe(1);
        // A later subscriber gets a fresh connection, not the closed one.
        const later = handlers();
        const closeLater = openLogEvents(later);
        expect(FakeWebSocket.instances).toHaveLength(2);
        closeLater();
    });
});
