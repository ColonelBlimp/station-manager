import { afterEach, describe, expect, it, vi } from 'vitest';
import { saveLookup, type LookupPayload } from './lookup';

afterEach(() => {
    vi.restoreAllMocks();
});

const PAYLOAD: LookupPayload = {
    hamnut: { name: 'hamnutlookupservice', enabled: true },
    chain: [{ name: 'qrzlookupservice', priority: 1, enabled: true }],
    continue_if_blank: ['name'],
    refresh_max_in_flight: 4,
};

function mockJSON(status: number, body: unknown) {
    vi.stubGlobal(
        'fetch',
        vi.fn((_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

// F-04c (ADR 0078): a config PUT whose response was lost to a FIRED timeout is
// AMBIGUOUS — the daemon may already have replaced the lookup block — so
// saveLookup must carry that out as `timedOut` for the section to reconcile by
// re-reading, rather than flattening it into a plain "failed". Only a fired
// timeout is marked: an HTTP rejection IS a definite rejection (the daemon
// answered), while a generic non-timeout transport failure is left unmarked too
// — it is not proven to have committed OR failed, so it keeps its existing
// wording with no new claim either way.
describe('saveLookup — timed-out write is ambiguous (F-04c)', () => {
    it('marks a fired timeout as timedOut (outcome-unknown)', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const res = await saveLookup(PAYLOAD);
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBe(true);
    });

    it('does NOT mark a non-timeout transport failure as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const res = await saveLookup(PAYLOAD);
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBeFalsy();
    });

    it('does NOT mark an HTTP rejection as timedOut (the daemon answered — definite)', async () => {
        mockJSON(400, { message: 'invalid' });
        const res = await saveLookup(PAYLOAD);
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBeFalsy();
    });
});
