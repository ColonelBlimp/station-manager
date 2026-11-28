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
    /** The request's body when it was text (a JSON write); null otherwise. */
    body: string | null;
}

export interface Route {
    status: number;
    /** The answer's body; or a function of the request's URL that gives it,
     *  for an answer that depends on the query (a page of a list). */
    body: unknown;
    hold?: Promise<void>;
}

const bodyOf = (r: Route, url: string): unknown =>
    typeof r.body === 'function' ? (r.body as (url: string) => unknown)(url) : r.body;

export interface Daemon {
    version: unknown;
    config: unknown;
    /** When set, GET /v1/config answers only once this settles. */
    configHold?: Promise<void>;
    /** When set, GET /v1/version (the identity read) answers only once this settles. */
    versionHold?: Promise<void>;
    /** Answers to other requests, by "METHOD url"; unlisted ones 404. An
     *  answer with `hold` is sent only once that promise settles. A list is a
     *  sequence: each request takes the next answer, and the last one stays. */
    routes: Record<string, Route | Route[]>;
    sent: Sent[];
}

/** The state of an ordinary start: a session open, held by this app. */
function ordinarySession(d: Daemon): unknown {
    const version = d.version as { archive?: { id?: string } } | null;
    return {
        archive_id: version?.archive?.id ?? '01a16a00-0000-7000-8000-000000000001',
        generation: 1,
        state: 'open',
        session: {
            id: '01a1f83c-da20-751f-80b7-ec9a3f1f9300',
            logbook_uuid: '01a187bc-0173-7649-8ff3-71db8170c309',
            opened_at: '2026-11-20T09:15:00Z',
            sitting_started_at: '2026-11-20T09:15:00Z',
            has_contacts: false,
            contact_count: 0,
            held_by_you: true,
        },
    };
}

function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'content-type': 'application/json' },
    });
}

// An answer given at once, or only once its hold settles.
function held(hold: Promise<void> | undefined, body: unknown): Promise<Response> {
    return hold ? hold.then(() => json(body)) : Promise.resolve(json(body));
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
                body: typeof init?.body === 'string' ? init.body : null,
            });
            if (method === 'GET' && url === '/v1/version') return held(d.versionHold, d.version);
            if (method === 'GET' && url === '/v1/config') return held(d.configHold, d.config);
            const listed = d.routes[`${method} ${url.split('?')[0]}`];
            const r = Array.isArray(listed)
                ? listed.length > 1
                    ? listed.shift()
                    : listed[0]
                : listed;
            if (r?.hold) return r.hold.then(() => json(bodyOf(r, url), r.status));
            if (r) return Promise.resolve(json(bodyOf(r, url), r.status));
            // The session's state, which every load reads before it shows the
            // workspace (W-0029 part 4): an ordinary start unless a test says
            // otherwise in its routes.
            if (method === 'GET' && url === '/v1/operating-session') {
                return Promise.resolve(json(ordinarySession(d)));
            }
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
