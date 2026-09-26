import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchArchiveBindings, saveArchiveBindings } from './archive-bindings';

// ADR 0082 part 9 (W-0021 5E): the client for GET/PUT
// /v1/qso-archives/{uuid}/bindings. The decoder keeps what it can read and
// drops what it cannot; a refusal keeps the daemon's code; a timed-out PUT is
// ambiguous (ADR 0078), never a plain failure.

afterEach(() => vi.unstubAllGlobals());

function stub(status: number, body: unknown, seen?: { url: string; init?: RequestInit }[]) {
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            seen?.push({ url: input, init });
            return Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
}

const VIEW = {
    archive_id: 'a1',
    archive_label: 'Home',
    restart_required: true,
    destinations: [
        {
            type: 'qrz',
            display_name: 'QRZ Logbook',
            account: { configured: true },
            state: 'mixed',
            logbooks: [
                {
                    logbook_id: 1,
                    logbook_uuid: 'u1',
                    logbook_name: 'Main',
                    logbook_callsign: 'M0ABC',
                    bound: true,
                    enabled: true,
                    forwarder_name: 'qrz',
                    credentials_set: ['api_key'],
                    queue: { waiting: 1, failed: 2, in_flight: 0 },
                },
                { logbook_id: 2, logbook_name: 'Second', bound: false, enabled: false },
                'not-a-row',
                // A row the SPA could not key: no id, or an id that is not a number.
                { logbook_name: 'no id — dropped', bound: true, enabled: true },
                { logbook_id: '3', logbook_name: 'string id — dropped', enabled: true },
            ],
        },
        {
            type: 'clublog',
            display_name: 'ClubLog',
            account: { configured: true, build_key: 'absent' },
            state: 'off',
            reason: '',
            logbooks: [],
        },
        { display_name: 'no type — dropped' },
    ],
};

describe('fetchArchiveBindings', () => {
    it('decodes the view, defaults absent parts, and drops malformed entries', async () => {
        const seen: { url: string }[] = [];
        stub(200, VIEW, seen);
        const out = await fetchArchiveBindings('a 1');
        expect(seen[0].url).toBe('/v1/qso-archives/a%201/bindings');
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        const v = out.bindings;
        expect(v.restart_required).toBe(true);
        expect(v.destinations.map((d) => d.type)).toEqual(['qrz', 'clublog']);
        const qrz = v.destinations[0];
        expect(qrz.state).toBe('mixed');
        expect(qrz.logbooks).toHaveLength(2);
        expect(qrz.logbooks[0].queue).toEqual({ waiting: 1, failed: 2, in_flight: 0 });
        expect(qrz.logbooks[1]).toMatchObject({
            logbook_id: 2,
            bound: false,
            enabled: false,
            forwarder_name: '',
            credentials_set: [],
            queue: { waiting: 0, failed: 0, in_flight: 0 },
        });
        expect(v.destinations[1].account.build_key).toBe('absent');
        expect(qrz.account.build_key).toBe('');
    });

    it('an unknown aggregate state reads as off rather than claiming more', async () => {
        stub(200, { ...VIEW, destinations: [{ ...VIEW.destinations[0], state: 'bogus' }] });
        const out = await fetchArchiveBindings('a1');
        expect(out.kind === 'ok' && out.bindings.destinations[0].state).toBe('off');
    });

    it('a refusal carries the daemon code and message', async () => {
        stub(409, {
            code: 'archive_not_active',
            message: 'bindings are edited on the active archive only',
        });
        const out = await fetchArchiveBindings('a1');
        expect(out).toMatchObject({ kind: 'error', code: 'archive_not_active' });
    });
});

describe('saveArchiveBindings', () => {
    it('PUTs the request and decodes the fresh view', async () => {
        const seen: { url: string; init?: RequestInit }[] = [];
        stub(200, VIEW, seen);
        const req = {
            destinations: [
                {
                    type: 'qrz',
                    logbooks: [{ logbook_id: 1, enabled: true, credentials: { api_key: 'k' } }],
                },
            ],
        };
        const out = await saveArchiveBindings('a1', req);
        expect(seen[0].init?.method).toBe('PUT');
        expect(JSON.parse(seen[0].init?.body as string)).toEqual(req);
        expect(out.kind).toBe('ok');
    });

    it('a refusal keeps its code; a definite failure is not marked timed out', async () => {
        stub(400, {
            code: 'binding_field_required',
            message: 'API key is required to turn this destination on',
        });
        const out = await saveArchiveBindings('a1', { destinations: [] });
        expect(out).toMatchObject({ kind: 'error', code: 'binding_field_required' });
        expect(out.kind === 'error' && out.timedOut).toBeFalsy();
    });

    it('a timed-out PUT is ambiguous', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(Object.assign(new Error('t'), { name: 'TimeoutError' })))
        );
        const out = await saveArchiveBindings('a1', { destinations: [] });
        expect(out).toMatchObject({ kind: 'error', timedOut: true });
    });
});
