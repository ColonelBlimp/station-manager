// Adapter-layer tests: the wire→display mapping the Worked panel relies on.
// The API client's own behaviour (status codes, malformed bodies, aborts) is
// covered by contact-history.test.ts; these pin the seam's conversions.

import { describe, it, expect, afterEach, vi } from 'vitest';
import { adifDateToDisplay, adifTimeToDisplay, toWorkedQso, apiHistory } from './seams';
import type { ContactHistory } from './contact-history';

afterEach(() => {
    vi.restoreAllMocks();
});

function makeRow(overrides: Partial<ContactHistory> = {}): ContactHistory {
    return {
        id: 1,
        uuid: '00000000-0000-7000-8000-000000000001',
        band: '20m',
        freq: '14.250000',
        mode: 'SSB',
        qso_date: '20260508',
        time_on: '1430',
        name: 'Test',
        country: 'England',
        call: 'M0XYZ',
        rst_sent: '59',
        rst_rcvd: '57',
        notes: '',
        ...overrides,
    };
}

describe('ADIF wire → display conversions', () => {
    it('formats YYYYMMDD as YYYY-MM-DD', () => {
        expect(adifDateToDisplay('20260508')).toBe('2026-05-08');
    });

    it('formats HHMM and HHMMSS as HH:MM', () => {
        expect(adifTimeToDisplay('1430')).toBe('14:30');
        expect(adifTimeToDisplay('143045')).toBe('14:30');
    });

    it('passes unrecognised values through unchanged — odd beats invisible', () => {
        expect(adifDateToDisplay('2026-05-08')).toBe('2026-05-08');
        expect(adifDateToDisplay('')).toBe('');
        expect(adifTimeToDisplay('14:30')).toBe('14:30');
        expect(adifTimeToDisplay('')).toBe('');
    });

    it('maps a wire row to the panel shape', () => {
        expect(toWorkedQso(makeRow())).toEqual({
            date: '2026-05-08',
            timeOn: '14:30',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '57',
            name: 'Test',
        });
    });
});

describe('apiHistory (stubbed fetch)', () => {
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

    it('returns mapped rows on 200', async () => {
        mockFetchJSON(200, { items: [makeRow(), makeRow({ qso_date: '20251102' })] });
        const rows = await apiHistory('M0XYZ');
        expect(rows).toHaveLength(2);
        expect(rows[1].date).toBe('2025-11-02');
    });

    it('is fail-soft: any non-ok outcome is an empty history', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'boom' });
        expect(await apiHistory('M0XYZ')).toEqual([]);

        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('network down')))
        );
        expect(await apiHistory('M0XYZ')).toEqual([]);
    });
});
