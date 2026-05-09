import { afterEach, describe, expect, it, vi } from 'vitest';
import { sendSessionEmail } from './session-email';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockFetchJSON(status: number, body: unknown): void {
    const response = new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
    vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(response))
    );
}

const REQ = { to: 'qsl@example.org', adif: '<call:5>K1ABC<eor>' };

describe('sendSessionEmail', () => {
    it('posts JSON to /v1/session/email', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(
                    new Response(JSON.stringify({ status: 'sent' }), { status: 200 })
                )
        );
        vi.stubGlobal('fetch', fetchSpy);

        await sendSessionEmail(REQ);

        expect(fetchSpy).toHaveBeenCalledTimes(1);
        const [url, init] = fetchSpy.mock.calls[0];
        expect(url).toBe('/v1/session/email');
        expect(init?.method).toBe('POST');
        expect((init?.headers as Record<string, string>)['Content-Type']).toBe('application/json');
        expect(init?.body).toBe(JSON.stringify(REQ));
    });

    it('returns kind=sent on 200', async () => {
        mockFetchJSON(200, { status: 'sent' });
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({ kind: 'sent' });
    });

    it('returns kind=mailer_disabled on 503', async () => {
        mockFetchJSON(503, {
            code: 'mailer_disabled',
            message: 'SMTP is not configured (smtp.host is empty)',
        });
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({
            kind: 'mailer_disabled',
            message: 'SMTP is not configured (smtp.host is empty)',
        });
    });

    it('returns kind=smtp_failure on 502', async () => {
        mockFetchJSON(502, {
            code: 'smtp_failure',
            message: 'SMTP send failed; check daemon logs for the cause',
        });
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({
            kind: 'smtp_failure',
            message: 'SMTP send failed; check daemon logs for the cause',
        });
    });

    it('returns kind=invalid for 400 with daemon code+message', async () => {
        mockFetchJSON(400, {
            code: 'invalid_field_value',
            message: 'to must be an email address',
        });
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({
            kind: 'invalid',
            code: 'invalid_field_value',
            message: 'to must be an email address',
        });
    });

    it('returns kind=invalid for 400 missing_required_field', async () => {
        mockFetchJSON(400, {
            code: 'missing_required_field',
            message: 'adif is required',
        });
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({
            kind: 'invalid',
            code: 'missing_required_field',
            message: 'adif is required',
        });
    });

    it('returns kind=server for 500', async () => {
        mockFetchJSON(500, { code: 'internal_error', message: 'unexpected' });
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({
            kind: 'server',
            code: 'internal_error',
            message: 'unexpected',
        });
    });

    it('returns kind=network when fetch throws', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({ kind: 'network', message: 'Failed to fetch' });
    });

    it('falls back to a synthesised error envelope when the body is unparseable', async () => {
        const response = new Response('not json', {
            status: 502,
            headers: { 'Content-Type': 'text/plain' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );
        const out = await sendSessionEmail(REQ);
        expect(out).toEqual({
            kind: 'smtp_failure',
            message: 'HTTP 502',
        });
    });

    it('passes optional subject + filename through to the body', async () => {
        const fetchSpy = vi.fn(
            (_input: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
                Promise.resolve(
                    new Response(JSON.stringify({ status: 'sent' }), { status: 200 })
                )
        );
        vi.stubGlobal('fetch', fetchSpy);

        await sendSessionEmail({
            to: 'a@b',
            adif: '<call:3>ABC<eor>',
            subject: 'Custom Subject',
            filename: 'custom.adi',
        });

        const [, init] = fetchSpy.mock.calls[0];
        const body = JSON.parse(init?.body as string) as Record<string, string>;
        expect(body.subject).toBe('Custom Subject');
        expect(body.filename).toBe('custom.adi');
    });
});
