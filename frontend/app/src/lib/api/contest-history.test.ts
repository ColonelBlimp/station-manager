import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import { fetchContestCompletions } from './contest-history';
import { setBoundArchive } from './archive-guard';

beforeEach(() => setBoundArchive('archive-1'));
afterEach(() => {
    vi.unstubAllGlobals();
    setBoundArchive(null);
});

const item = (call: string, date: string, on: string, off: string, extra: object = {}) => ({
    uuid: call + on,
    call,
    qso_date: date,
    time_on: on,
    qso_date_off: date,
    time_off: off,
    contest_id: 'UKEIDXSSB',
    app_sm_contest_start: '20261031',
    ...extra,
});

describe('fetchContestCompletions', () => {
    it('collects the occurrence completion times newest-first, stopping before the start date', async () => {
        const pages = [
            {
                items: [
                    item('G4A', '20261101', '0930', '093130'),
                    item('K1X', '20261101', '0920', '0921', { contest_id: undefined }),
                    item('G4B', '20261031', '2359', '000110', { qso_date_off: '20261101' }),
                ],
                next_cursor: 'c1',
            },
            {
                items: [
                    item('G4C', '20261031', '1201', '120230'),
                    item('G4D', '20261031', '1150', '1151', { app_sm_contest_start: '20261030' }),
                    item('OLD', '20261030', '1700', '1701'),
                ],
                next_cursor: 'c2',
            },
            { items: [item('NEVER', '20261029', '1200', '1201')], next_cursor: null },
        ];
        const fn = vi.fn((url: string) => {
            const i = url.includes('after=c2') ? 2 : url.includes('after=c1') ? 1 : 0;
            return Promise.resolve(Response.json(pages[i]));
        });
        vi.stubGlobal('fetch', fn);
        const out = await fetchContestCompletions(1, 'UKEIDXSSB', '20261031');
        expect(out).toEqual([
            Date.UTC(2026, 10, 1, 9, 31, 30),
            Date.UTC(2026, 10, 1, 0, 1, 10),
            Date.UTC(2026, 9, 31, 12, 2, 30),
        ]);
        expect(fn).toHaveBeenCalledTimes(2); // page 2 reached a date before the start: stop
    });

    it('a failed read is null, never a partial list taken as complete', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('', { status: 500 })))
        );
        expect(await fetchContestCompletions(1, 'UKEIDXSSB', '20261031')).toBeNull();
    });
});
