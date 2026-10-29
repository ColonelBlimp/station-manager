// A transport-only harness for tests that import the REAL main.ts (the
// composition root): fetch and EventSource are stubbed, the daemon's answers
// are mutable, and every request is recorded with the headers the guards read.
import { vi } from 'vitest';

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
    /** Answers to other requests, by "METHOD url"; unlisted ones 404. */
    routes: Record<string, { status: number; body: unknown }>;
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
            if (method === 'GET' && url === '/v1/config') return Promise.resolve(json(d.config));
            const r = d.routes[`${method} ${url.split('?')[0]}`];
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

export function shellStream(): FakeEventSource {
    const src = FakeEventSource.instances.find((s) => s.url === '/v1/events');
    if (!src) throw new Error('the shell stream was not opened');
    return src;
}
