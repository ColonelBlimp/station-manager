import { afterEach, describe, expect, it, vi } from 'vitest';
import { submitQso } from './qso';

afterEach(() => vi.restoreAllMocks());

function mockFetch(status: number, body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

const UUID = '00000000-0000-7000-8000-000000000001';

// F-03: the submit decoder validates the exact success contract. An unknown 2xx status must
// downgrade to malformed_response (draft preserved), never be assumed 'stored'. body.status
// is authoritative: a mismatched HTTP code must not change the classification.
describe('submitQso response decoding (F-03)', () => {
    it('201 stored → kind stored (no HTTP-mismatch warning)', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(201, { status: 'stored', uuid: UUID, id: 7 });
        expect(await submitQso('<eor>', 1)).toEqual({ kind: 'stored', uuid: UUID });
        expect(warn).not.toHaveBeenCalled();
    });

    it('200 duplicate → kind duplicate (no HTTP-mismatch warning)', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(200, { status: 'duplicate', uuid: UUID, id: 7 });
        expect(await submitQso('<eor>', 1)).toEqual({ kind: 'duplicate', uuid: UUID });
        expect(warn).not.toHaveBeenCalled();
    });

    it('an unknown 2xx status is malformed_response, not stored (preserves the draft)', async () => {
        mockFetch(200, { status: 'unexpected', uuid: UUID });
        const out = await submitQso('<eor>', 1);
        expect(out.kind).toBe('server');
        expect((out as { code?: string }).code).toBe('malformed_response');
    });

    it('a 2xx with no status field is malformed_response', async () => {
        mockFetch(200, { uuid: UUID });
        const out = await submitQso('<eor>', 1);
        expect(out.kind).toBe('server');
        expect((out as { code?: string }).code).toBe('malformed_response');
    });

    it('a successful body without a uuid is malformed_response', async () => {
        mockFetch(201, { status: 'stored', uuid: '' });
        const out = await submitQso('<eor>', 1);
        expect(out.kind).toBe('server');
        expect((out as { code?: string }).code).toBe('malformed_response');
    });

    // body.status is authoritative — a wrong HTTP code must NOT change the classification
    // (a false-malformed on a genuinely stored QSO would drive a duplicate-dialog double-write).
    it('classifies by body.status even when the HTTP code mismatches (200 + stored), and warns', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(200, { status: 'stored', uuid: UUID });
        expect(await submitQso('<eor>', 1)).toEqual({ kind: 'stored', uuid: UUID });
        expect(warn).toHaveBeenCalledOnce();
        expect(String(warn.mock.calls[0][0])).toContain('HTTP 200');
    });

    it('classifies by body.status even when the HTTP code mismatches (201 + duplicate), and warns', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetch(201, { status: 'duplicate', uuid: UUID });
        expect(await submitQso('<eor>', 1)).toEqual({ kind: 'duplicate', uuid: UUID });
        expect(warn).toHaveBeenCalledOnce();
        expect(String(warn.mock.calls[0][0])).toContain('HTTP 201');
    });

    it('4xx → validation with the daemon code', async () => {
        mockFetch(400, { code: 'invalid_adif', message: 'bad' });
        const out = await submitQso('<eor>', 1);
        expect(out.kind).toBe('validation');
        expect((out as { code?: string }).code).toBe('invalid_adif');
    });

    it('5xx → server with the daemon code', async () => {
        mockFetch(500, { code: 'db_error', message: 'boom' });
        const out = await submitQso('<eor>', 1);
        expect(out.kind).toBe('server');
        expect((out as { code?: string }).code).toBe('db_error');
    });
});
