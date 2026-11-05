import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FakeWebSocket } from '../../test/fakeSocket';
import { openRevivingSocket, SOCKET_RETRY_MS } from './socket-reviving';

/*
    THE RECONNECTING WEBSOCKET (ADR 0099 W1, responsibility 9).

    A WebSocket has no automatic reconnect, so openRevivingSocket owns it. The
    rules it must keep:
      S1  one active attempt and one armed retry timer at a time;
      S2  a failure arms ONE fixed retry (1 s, operator ruling 2026-11-05), no
          backoff; an 'error' followed by 'close' is ONE failure (one
          onTransportError, one timer);
      S3  callbacks from a replaced socket are ignored;
      S4  visibility/online revive ONLY a socket waiting to retry, and only in
          a visible tab; a CONNECTING socket is not dead and an OPEN one is
          healthy — neither is ever replaced (the SSE wrapper's "revive only
          when dead", including the initial connect);
      S5  automatic retries do not depend on visibility (as the browser's own
          EventSource retries did in a hidden tab);
      S6  teardown cancels the timer, closes the socket, removes listeners, and
          no later callback acts;
      S7  each frame is parsed once; the envelope is checked before dispatch;
          'connected' and 'keepalive' are transport frames, not events.
*/

function wire() {
    return { onOpen: vi.fn(), onTransportError: vi.fn(), onFrame: vi.fn() };
}
const warn = vi.fn();
const latest = (): FakeWebSocket => FakeWebSocket.instances[FakeWebSocket.instances.length - 1];

function setVisibility(v: 'visible' | 'hidden'): void {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => v });
}

beforeEach(() => {
    vi.useFakeTimers();
    FakeWebSocket.instances = [];
    vi.stubGlobal('WebSocket', FakeWebSocket);
    setVisibility('visible');
    warn.mockReset();
});
afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

describe('openRevivingSocket', () => {
    it('connects to the same host over ws:, on the given path', () => {
        const close = openRevivingSocket('/v1/events/ws', wire(), warn);
        expect(FakeWebSocket.instances).toHaveLength(1);
        const u = new URL(latest().url);
        expect(u.protocol).toBe('ws:');
        expect(u.host).toBe(location.host);
        expect(u.pathname).toBe('/v1/events/ws');
        close();
    });

    it('S2: a failure retries once after exactly the fixed interval', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        latest().emit('open');
        latest().emit('error'); // error, then close
        expect(w.onTransportError).toHaveBeenCalledOnce();
        vi.advanceTimersByTime(SOCKET_RETRY_MS - 1);
        expect(FakeWebSocket.instances).toHaveLength(1);
        vi.advanceTimersByTime(1);
        expect(FakeWebSocket.instances).toHaveLength(2);
        expect(SOCKET_RETRY_MS).toBe(1000);
        close();
    });

    it('S2: the interval is fixed — repeated failures never back off', () => {
        const close = openRevivingSocket('/p', wire(), warn);
        for (let i = 1; i <= 5; i++) {
            latest().emit('error');
            vi.advanceTimersByTime(SOCKET_RETRY_MS);
            expect(FakeWebSocket.instances).toHaveLength(i + 1);
        }
        close();
    });

    it('S1/S2: error then close is one failure — one timer, one new socket', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        const s = latest();
        s.onerror?.(new Event('error'));
        s.onclose?.({ code: 1006 } as CloseEvent);
        expect(w.onTransportError).toHaveBeenCalledOnce();
        vi.advanceTimersByTime(SOCKET_RETRY_MS * 3);
        expect(FakeWebSocket.instances).toHaveLength(2);
        close();
    });

    it('a close with no error (server 1001) is a failure too', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        latest().emit('open');
        latest().emitClose(1001);
        expect(w.onTransportError).toHaveBeenCalledOnce();
        vi.advanceTimersByTime(SOCKET_RETRY_MS);
        expect(FakeWebSocket.instances).toHaveLength(2);
        close();
    });

    it('S3: callbacks from a replaced socket are ignored', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        const old = latest();
        old.emit('error');
        vi.advanceTimersByTime(SOCKET_RETRY_MS);
        const current = latest();
        expect(current).not.toBe(old);
        old.emit('open');
        old.emit('qso.stored', '{"x":1}');
        old.emit('error');
        expect(w.onOpen).not.toHaveBeenCalled();
        expect(w.onFrame).not.toHaveBeenCalled();
        expect(w.onTransportError).toHaveBeenCalledOnce(); // only the first, real failure
        vi.advanceTimersByTime(SOCKET_RETRY_MS * 2);
        expect(FakeWebSocket.instances).toHaveLength(2); // the stale error armed nothing
        current.emit('open');
        expect(w.onOpen).toHaveBeenCalledOnce();
        close();
    });

    it('S4: visibility revives a socket waiting to retry, at once, and cancels its timer', () => {
        const close = openRevivingSocket('/p', wire(), warn);
        latest().emit('error');
        document.dispatchEvent(new Event('visibilitychange'));
        expect(FakeWebSocket.instances).toHaveLength(2);
        vi.advanceTimersByTime(SOCKET_RETRY_MS * 3);
        expect(FakeWebSocket.instances).toHaveLength(2); // the cancelled timer fired nothing
        close();
    });

    it('S4: online revives a waiting socket in a visible tab', () => {
        const close = openRevivingSocket('/p', wire(), warn);
        latest().emit('error');
        window.dispatchEvent(new Event('online'));
        expect(FakeWebSocket.instances).toHaveLength(2);
        close();
    });

    it('S4: a CONNECTING socket — including the initial connect — is never replaced', () => {
        const close = openRevivingSocket('/p', wire(), warn);
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('online'));
        expect(FakeWebSocket.instances).toHaveLength(1);
        expect(latest().closeCalls).toBe(0);
        // A retry attempt in progress is equally left alone.
        latest().emit('error');
        vi.advanceTimersByTime(SOCKET_RETRY_MS);
        const attempt = latest();
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('online'));
        expect(latest()).toBe(attempt);
        expect(attempt.closeCalls).toBe(0);
        close();
    });

    it('S4: an OPEN socket is never touched', () => {
        const close = openRevivingSocket('/p', wire(), warn);
        latest().emit('open');
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('online'));
        expect(FakeWebSocket.instances).toHaveLength(1);
        expect(latest().closeCalls).toBe(0);
        close();
    });

    it('S4: in a hidden tab, visibility/online bring nothing forward', () => {
        const close = openRevivingSocket('/p', wire(), warn);
        latest().emit('error');
        setVisibility('hidden');
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('online'));
        expect(FakeWebSocket.instances).toHaveLength(1);
        close();
    });

    it('S5: automatic retries continue in a hidden tab', () => {
        setVisibility('hidden');
        const close = openRevivingSocket('/p', wire(), warn);
        latest().emit('error');
        vi.advanceTimersByTime(SOCKET_RETRY_MS);
        expect(FakeWebSocket.instances).toHaveLength(2);
        close();
    });

    it('S6: teardown cancels the timer, closes the socket and removes listeners', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        latest().emit('error');
        close();
        vi.advanceTimersByTime(SOCKET_RETRY_MS * 3);
        document.dispatchEvent(new Event('visibilitychange'));
        window.dispatchEvent(new Event('online'));
        expect(FakeWebSocket.instances).toHaveLength(1);
    });

    it('S6: teardown of an open socket closes it, and its late callbacks act on nothing', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        const s = latest();
        s.emit('open');
        close();
        expect(s.closeCalls).toBe(1);
        s.emit('qso.stored', '{"x":1}');
        s.emit('error');
        expect(w.onFrame).not.toHaveBeenCalled();
        expect(w.onTransportError).not.toHaveBeenCalled();
        vi.advanceTimersByTime(SOCKET_RETRY_MS * 3);
        expect(FakeWebSocket.instances).toHaveLength(1);
    });

    it('S7: an envelope dispatches its event name and parsed data; transport frames are not events', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        const s = latest();
        s.emit('open');
        s.emit('connected');
        s.emit('keepalive');
        expect(warn).not.toHaveBeenCalled(); // transport frames are neither events nor malformed
        s.emit('qso.stored', '{"qso_uuid":"u","logbook_id":3}');
        expect(w.onFrame).toHaveBeenCalledOnce();
        expect(w.onFrame).toHaveBeenCalledWith('qso.stored', { qso_uuid: 'u', logbook_id: 3 });
        close();
    });

    it('S7: malformed frames are dropped with a warning, and later valid frames still dispatch', () => {
        const w = wire();
        const close = openRevivingSocket('/p', w, warn);
        const s = latest();
        s.raw('{not json');
        s.raw('[]');
        s.raw('{"data":{}}'); // no event name
        s.raw('{"event":5,"data":{}}');
        s.raw('{"event":"qso.stored"}'); // an event with no data member
        expect(w.onFrame).not.toHaveBeenCalled();
        expect(warn).toHaveBeenCalled();
        s.emit('qso.stored', '{"a":1}');
        expect(w.onFrame).toHaveBeenCalledWith('qso.stored', { a: 1 });
        close();
    });
});
