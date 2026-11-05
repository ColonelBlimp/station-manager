// A capture-everything WebSocket fake for the shell and rig stream transports
// (ADR 0099 W1; jsdom has no WebSocket server). It speaks the daemon's envelope:
// emit(name, data) delivers {"event": name, "data": <parsed data>} as one text
// frame, mirroring how the SSE fakes delivered `event: name / data: <json>`, so
// behavioural assertions written against the SSE fakes carry over unchanged.
//   emit('open')  — the handshake completed (WebSocket 'open')
//   emit('error') — the connection failed: 'error' then 'close', as browsers do
//   raw(text)     — an arbitrary text frame (malformed-envelope cases)
export class FakeWebSocket {
    static instances: FakeWebSocket[] = [];
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSING = 2;
    static readonly CLOSED = 3;
    readyState = 0;
    closeCalls = 0;
    onopen: ((ev: Event) => void) | null = null;
    onmessage: ((ev: MessageEvent) => void) | null = null;
    onerror: ((ev: Event) => void) | null = null;
    onclose: ((ev: CloseEvent) => void) | null = null;

    constructor(public url: string) {
        FakeWebSocket.instances.push(this);
    }

    /** The URL's path, e.g. '/v1/events/ws'. */
    get path(): string {
        return new URL(this.url).pathname;
    }

    close(): void {
        this.closeCalls++;
        this.readyState = 3;
    }

    emit(type: string, data?: string): void {
        if (type === 'open') {
            this.readyState = 1;
            this.onopen?.(new Event('open'));
            return;
        }
        if (type === 'error') {
            this.readyState = 3;
            this.onerror?.(new Event('error'));
            this.onclose?.({ code: 1006 } as CloseEvent);
            return;
        }
        const frame: Record<string, unknown> = { event: type };
        if (data !== undefined) frame.data = JSON.parse(data) as unknown;
        this.raw(JSON.stringify(frame));
    }

    /** Only 'close' (no preceding 'error'): e.g. a server close frame. */
    emitClose(code = 1001): void {
        this.readyState = 3;
        this.onclose?.({ code } as CloseEvent);
    }

    raw(text: string): void {
        this.onmessage?.({ data: text } as MessageEvent);
    }
}

/** The newest socket opened on `path` (a reconnect opens a new one). */
export function socketFor(path: string): FakeWebSocket | undefined {
    const all = FakeWebSocket.instances.filter((s) => s.path === path);
    return all[all.length - 1];
}

/** Wait for the reconnect that follows a failure of `old` on the same path. */
export async function nextSocket(
    old: FakeWebSocket,
    waitFor: (fn: () => void) => Promise<void>
): Promise<FakeWebSocket> {
    let next: FakeWebSocket | undefined;
    await waitFor(() => {
        next = socketFor(old.path);
        if (next === old || next === undefined) throw new Error('no reconnect yet');
    });
    return next!;
}
