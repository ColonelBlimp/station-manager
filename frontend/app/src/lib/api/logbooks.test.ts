// F-03c (ADR 0077): per-record decoders on the logbook read surface. A malformed element is
// dropped (valid ones kept), duplicate keys are dropped so keyed rendering cannot throw, and the
// cursor is accepted only as exactly string|null. One aggregated warning per response reports how
// many records were dropped. safeFetch/readJsonBody are the real thing over a mocked fetch.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchLogbooks, fetchQsoPage } from './logbooks';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockFetchJSON(status: number, body: unknown): void {
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

const LB = (over: Record<string, unknown> = {}) => ({
    id: 1,
    name: 'Home',
    callsign: 'M0ABC',
    ...over,
});
const ROW = (over: Record<string, unknown> = {}) => ({ uuid: 'u-1', call: 'K1ABC', ...over });

describe('fetchLogbooks — per-record decode (F-03c)', () => {
    it('drops a logbook with a non-numeric id, non-string name, or non-string callsign', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, [
            LB({ id: 1 }),
            LB({ id: '2' }), // id not a number
            LB({ id: 3, name: 5 }), // name not a string
            LB({ id: 4, callsign: null }), // callsign not a string
            LB({ id: 5 }),
        ]);
        const out = await fetchLogbooks();
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.logbooks.map((l) => l.id)).toEqual([1, 5]);
    });

    it('drops duplicate logbook ids (keeps the first) so a keyed selector cannot throw', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, [LB({ id: 7, name: 'first' }), LB({ id: 7, name: 'second' })]);
        const out = await fetchLogbooks();
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.logbooks).toEqual([{ id: 7, name: 'first', callsign: 'M0ABC' }]);
    });

    it('warns exactly once per response, summarising the drop count', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, [LB({ id: 1 }), LB({ id: '2' }), LB({ id: 1 })]); // 1 malformed + 1 dup
        await fetchLogbooks();
        expect(warn).toHaveBeenCalledTimes(1);
    });

    it('does not warn when every record is valid', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, [LB({ id: 1 }), LB({ id: 2 })]);
        await fetchLogbooks();
        expect(warn).not.toHaveBeenCalled();
    });
});

describe('fetchQsoPage — per-record decode (F-03c)', () => {
    it('drops a page row without a non-empty string uuid, keeping the valid rows', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, {
            items: [
                ROW({ uuid: 'a' }),
                ROW({ uuid: '' }), // empty string
                ROW({ uuid: 5 }), // non-string
                { call: 'K1ABC' }, // uuid key absent entirely
                ROW({ uuid: 'b' }),
            ],
            next_cursor: null,
        });
        const out = await fetchQsoPage(1, 50);
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.items.map((r) => r.uuid)).toEqual(['a', 'b']);
    });

    it('drops duplicate row uuids so #each keying cannot throw', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, {
            items: [ROW({ uuid: 'dup', call: 'FIRST' }), ROW({ uuid: 'dup', call: 'SECOND' })],
            next_cursor: null,
        });
        const out = await fetchQsoPage(1, 50);
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.items).toHaveLength(1);
        expect(out.items[0].call).toBe('FIRST');
    });

    it('accepts next_cursor as exactly a string or null', async () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, { items: [ROW()], next_cursor: 'CURSOR' });
        let out = await fetchQsoPage(1, 50);
        expect(out.kind === 'ok' && out.nextCursor).toBe('CURSOR');

        mockFetchJSON(200, { items: [ROW()], next_cursor: null });
        out = await fetchQsoPage(1, 50);
        expect(out.kind === 'ok' && out.nextCursor).toBe(null);
    });

    it('treats a non-string, non-null next_cursor as end-of-pagination and warns', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, { items: [ROW()], next_cursor: 42 });
        const out = await fetchQsoPage(1, 50);
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.nextCursor).toBe(null);
        expect(warn).toHaveBeenCalled();
    });

    // The daemon always serializes next_cursor (a *string, no omitempty), so a MISSING key is
    // malformed — not a clean end — and must warn and stop rather than pass silently.
    it('treats a missing next_cursor key as malformed and warns', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, { items: [ROW()] }); // next_cursor key absent entirely
        const out = await fetchQsoPage(1, 50);
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.nextCursor).toBe(null);
        expect(warn).toHaveBeenCalled();
    });

    it('warns exactly once per response for dropped rows', async () => {
        const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
        mockFetchJSON(200, {
            items: [ROW({ uuid: 'a' }), ROW({ uuid: 5 }), ROW({ uuid: 'a' })], // 1 malformed + 1 dup
            next_cursor: null,
        });
        await fetchQsoPage(1, 50);
        expect(warn).toHaveBeenCalledTimes(1);
    });
});
