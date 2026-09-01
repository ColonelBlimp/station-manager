import { afterEach, describe, expect, it, vi } from 'vitest';
import { saveEmail, type SmtpPayload } from './email';

afterEach(() => {
    vi.restoreAllMocks();
});

const PAYLOAD: SmtpPayload = {
    enabled: true,
    host: 'smtp.example.org',
    port: 0,
    username: 'tx@example.org',
    from: 'tx@example.org',
    default_recipient: 'qsl@example.org',
    starttls: true,
    timeout_sec: 0,
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
// AMBIGUOUS — the daemon may already have replaced the smtp block — so saveEmail
// must carry that out as `timedOut` for the section to reconcile by re-reading,
// rather than flattening it into a plain "failed". Only a fired timeout is
// marked: an HTTP rejection IS a definite rejection (the daemon answered), while
// a generic non-timeout transport failure is left unmarked too — it is not
// proven to have committed OR failed, so it keeps its existing wording with no
// new claim either way.
describe('saveEmail — timed-out write is ambiguous (F-04c)', () => {
    it('marks a fired timeout as timedOut (outcome-unknown)', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const res = await saveEmail(PAYLOAD);
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBe(true);
    });

    it('does NOT mark a non-timeout transport failure as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const res = await saveEmail(PAYLOAD);
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBeFalsy();
    });

    it('does NOT mark an HTTP rejection as timedOut (the daemon answered — definite)', async () => {
        mockJSON(400, { message: 'invalid' });
        const res = await saveEmail(PAYLOAD);
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBeFalsy();
    });
});
