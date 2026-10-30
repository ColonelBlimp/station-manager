import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { allocateContestSerial } from './contest-serial';
import { ARCHIVE_HEADER, setBoundArchive } from './archive-guard';

beforeEach(() => setBoundArchive('archive-1'));
afterEach(() => {
    vi.unstubAllGlobals();
    setBoundArchive(null);
});

function stub(response: Response) {
    const fn = vi.fn((_url: string, _init?: RequestInit) => Promise.resolve(response));
    vi.stubGlobal('fetch', fn);
    return fn;
}

describe('allocateContestSerial', () => {
    it('posts logbook, contest and draft under the archive guard and returns the serial', async () => {
        const fn = stub(Response.json({ serial: 42 }));
        expect(await allocateContestSerial(3, 'UKEIDXSSB', '20261031', 'd-1')).toEqual({
            kind: 'ok',
            serial: 42,
        });
        const [url, init] = fn.mock.calls[0];
        expect(url).toBe('/v1/contest-serial?logbook=3&contest=UKEIDXSSB&start=20261031&draft=d-1');
        expect(init?.method).toBe('POST');
        expect(new Headers(init?.headers).get(ARCHIVE_HEADER)).toBe('archive-1');
    });

    it('reports a daemon refusal with its message', async () => {
        stub(
            Response.json(
                { code: 'logbook_not_found', message: 'logbook does not exist' },
                { status: 404 }
            )
        );
        expect(await allocateContestSerial(9, 'UKEIDXSSB', '20261031', 'd')).toEqual({
            kind: 'error',
            message: 'logbook does not exist',
        });
    });

    it('treats a 200 without a positive integer serial as an error, never a number', async () => {
        stub(Response.json({ serial: '7' }));
        expect((await allocateContestSerial(1, 'X', '20261031', 'd')).kind).toBe('error');
    });

    it('sends nothing while no archive is proven', async () => {
        setBoundArchive(null);
        const fn = stub(Response.json({ serial: 1 }));
        expect((await allocateContestSerial(1, 'X', '20261031', 'd')).kind).toBe('error');
        expect(fn).not.toHaveBeenCalled();
    });
});
