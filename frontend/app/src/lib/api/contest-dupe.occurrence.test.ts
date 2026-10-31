import { describe, it, expect, afterEach, vi } from 'vitest';
import { fetchContestDupe } from './contest-dupe';

afterEach(() => vi.unstubAllGlobals());

// W-0023 weekend slice: a contest occurrence scopes the dupe question.
describe('fetchContestDupe with a contest occurrence', () => {
    it('sends contest and start with the query', async () => {
        const fn = vi.fn((_url: string) => Promise.resolve(Response.json({ duplicate: true })));
        vi.stubGlobal('fetch', fn);
        const out = await fetchContestDupe({
            logbook: 1,
            call: 'G4ABC',
            band: '20m',
            contest: 'UKEIDXSSB',
            start: '20261031',
        });
        expect(out).toEqual({ kind: 'ok', duplicate: true });
        expect(fn.mock.calls[0][0]).toBe(
            '/v1/contest-dupe?logbook=1&call=G4ABC&band=20m&contest=UKEIDXSSB&start=20261031'
        );
    });
});
