// A transport-only harness for tests that import the REAL main.ts (the
// composition root): fetch, EventSource (FT8) and WebSocket (the shell and rig
// streams, ADR 0099 W1) are stubbed, the daemon's answers
// are mutable, and every request is recorded with the headers the guards read.
import { vi } from 'vitest';
import { FakeWebSocket, socketFor } from './fakeSocket';
import { setSocketRetryMsForTests } from '../lib/api/socket-reviving';

export { FakeWebSocket };

export class FakeEventSource {
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
        for (const fn of this.listeners.get(type) ?? []) fn({ data } as MessageEvent<string>);
    }
}

export interface Sent {
    method: string;
    url: string;
    archive: string | null;
    defaultLogbook: string | null;
}

export interface Daemon {
    version: unknown;
    config: unknown;
    /** When set, GET /v1/config answers only once this settles. */
    configHold?: Promise<void>;
    /** Answers to other requests, by "METHOD url"; unlisted ones 404. An
     *  answer with `hold` is sent only once that promise settles. */
    routes: Record<string, { status: number; body: unknown; hold?: Promise<void> }>;
    sent: Sent[];
}

function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'content-type': 'application/json' },
    });
}

export function installDaemon(d: Daemon): void {
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('WebSocket', FakeWebSocket);
    setSocketRetryMsForTests(0);
    vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
            const url =
                typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
            const method = (init?.method ?? 'GET').toUpperCase();
            const h = new Headers(init?.headers);
            d.sent.push({
                method,
                url,
                archive: h.get('X-SM-Archive-Id'),
                defaultLogbook: h.get('X-SM-Default-Logbook'),
            });
            if (method === 'GET' && url === '/v1/version') return Promise.resolve(json(d.version));
            if (method === 'GET' && url === '/v1/config') {
                if (d.configHold) return d.configHold.then(() => json(d.config));
                return Promise.resolve(json(d.config));
            }
            const r = d.routes[`${method} ${url.split('?')[0]}`];
            if (r?.hold) return r.hold.then(() => json(r.body, r.status));
            if (r) return Promise.resolve(json(r.body, r.status));
            return Promise.resolve(json({ code: 'not_stubbed' }, 404));
        })
    );
}

export const submits = (d: Daemon): Sent[] =>
    d.sent.filter((s) => s.method === 'POST' && s.url.startsWith('/v1/qso?'));

/** The pair a submit carried: its ?logbook= and its asserted default. */
export const pairOf = (s: Sent): [string | null, string | null] => [
    new URLSearchParams(s.url.split('?')[1]).get('logbook'),
    s.defaultLogbook,
];

/** The shell's current stream socket (the newest on /v1/events/ws). */
export function shellStream(): FakeWebSocket {
    const src = socketFor('/v1/events/ws');
    if (!src) throw new Error('the shell stream was not opened');
    return src;
}
