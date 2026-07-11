// Adapter-layer tests: the wire→display mapping the Worked panel relies on.
// The API client's own behaviour (status codes, malformed bodies, aborts) is
// covered by contact-history.test.ts; these pin the seam's conversions.

import { describe, it, expect, afterEach, vi } from 'vitest';
import {
    adifDateToDisplay,
    adifTimeToDisplay,
    toWorkedQso,
    apiHistory,
    fetchStationContext,
    fetchLogbookCount,
} from './seams';
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
            uuid: '00000000-0000-7000-8000-000000000001',
            date: '2026-05-08',
            timeOn: '14:30',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '57',
            name: 'Test',
            notes: '',
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
        mockFetchJSON(200, {
            items: [
                makeRow(),
                makeRow({ uuid: '00000000-0000-7000-8000-000000000002', qso_date: '20251102' }),
            ],
        });
        const rows = await apiHistory('M0XYZ');
        expect(rows).toHaveLength(2);
        expect(rows[1].date).toBe('2025-11-02');
    });

    it('drops rows without a valid uuid and dedupes duplicates (keyed-each safety)', async () => {
        mockFetchJSON(200, {
            items: [
                makeRow(), // uuid …001
                makeRow({ uuid: '' }), // missing uuid → dropped
                null, // malformed row → dropped
                makeRow({ qso_date: '20251102' }), // duplicate uuid …001 → dropped
                makeRow({ uuid: '00000000-0000-7000-8000-000000000002' }), // kept
            ],
        });
        const rows = await apiHistory('M0XYZ');
        expect(rows.map((r) => r.uuid)).toEqual([
            '00000000-0000-7000-8000-000000000001',
            '00000000-0000-7000-8000-000000000002',
        ]);
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

describe('fetchStationContext bridge block (stubbed fetch)', () => {
    function mockConfig(body: unknown): void {
        const response = new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(response))
        );
    }

    it('parses catEnabled + well-formed mode_mappings, dropping malformed entries', async () => {
        mockConfig({
            logging_station: { my_gridsquare: 'KH66', station_callsign: '7Q5MLV' },
            default_logbook: { id: 3 },
            bridge: {
                enabled: true,
                mode_mappings: {
                    USB: { mode: 'SSB', submode: 'USB' },
                    'CW-U': { mode: 'CW' },
                    BROKEN: { submode: 'no-mode-field' },
                    ALSO_BROKEN: 'not-an-object',
                },
            },
        });
        const ctx = await fetchStationContext();
        expect(ctx.catEnabled).toBe(true);
        expect(ctx.modeMappings).toEqual({
            USB: { mode: 'SSB', submode: 'USB' },
            'CW-U': { mode: 'CW', submode: undefined },
        });
    });

    it('defaults to CAT-off with no mappings when the bridge block is absent', async () => {
        mockConfig({
            logging_station: { my_gridsquare: 'KH66', station_callsign: '7Q5MLV' },
            default_logbook: { id: 3 },
        });
        const ctx = await fetchStationContext();
        expect(ctx.catEnabled).toBe(false);
        expect(ctx.modeMappings).toEqual({});
    });

    it('reads station.operating_bands (empty when the station block is absent)', async () => {
        mockConfig({
            logging_station: { station_callsign: '7Q5MLV' },
            station: { operating_bands: ['80m', '40m', '20m', '15m', '10m'] },
        });
        const ctx = await fetchStationContext();
        expect(ctx.operatingBands).toEqual(['80m', '40m', '20m', '15m', '10m']);

        mockConfig({ logging_station: { station_callsign: '7Q5MLV' } });
        expect((await fetchStationContext()).operatingBands).toEqual([]);
    });

    it('reads ft8_display feed_mode / history_max / cq_to_top', async () => {
        mockConfig({
            logging_station: { station_callsign: '7Q5MLV' },
            ft8_display: { feed_mode: 'single', history_max: 250, cq_to_top: true },
        });
        const ctx = await fetchStationContext();
        expect(ctx.ft8FeedMode).toBe('single');
        expect(ctx.ft8HistoryMax).toBe(250);
        expect(ctx.ft8CqToTop).toBe(true);
    });

    it('defaults ft8_display to accumulate / 100 / no-cq-float when the block is absent', async () => {
        mockConfig({ logging_station: { station_callsign: '7Q5MLV' } });
        const ctx = await fetchStationContext();
        expect(ctx.ft8FeedMode).toBe('accumulate');
        expect(ctx.ft8HistoryMax).toBe(100);
        expect(ctx.ft8CqToTop).toBe(false);
    });

    it('falls back to accumulate for an unknown feed_mode literal', async () => {
        mockConfig({
            logging_station: { station_callsign: '7Q5MLV' },
            ft8_display: { feed_mode: 'bogus' },
        });
        expect((await fetchStationContext()).ft8FeedMode).toBe('accumulate');
    });

    it('reads default_logbook.name and bridge.rig_name for the header identity', async () => {
        mockConfig({
            logging_station: { station_callsign: '7Q5MLV' },
            default_logbook: { id: 3, name: 'Malawi 2026' },
            bridge: { enabled: true, rig_name: 'FTdx10' },
        });
        const ctx = await fetchStationContext();
        expect(ctx.logbookName).toBe('Malawi 2026');
        expect(ctx.rigName).toBe('FTdx10');
    });

    it('defaults logbook/rig names to empty when the blocks are absent', async () => {
        mockConfig({ logging_station: { station_callsign: '7Q5MLV' } });
        const ctx = await fetchStationContext();
        expect(ctx.logbookName).toBe('');
        expect(ctx.rigName).toBe('');
    });
});

describe('fetchLogbookCount (stubbed fetch)', () => {
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

    it('returns the count on 200', async () => {
        mockFetchJSON(200, { logbook_id: 3, count: 1234 });
        expect(await fetchLogbookCount(3)).toBe(1234);
    });

    it('is fail-soft: any non-ok outcome is null (keep the last good count)', async () => {
        mockFetchJSON(500, { code: 'db_error', message: 'boom' });
        expect(await fetchLogbookCount(3)).toBeNull();
    });

    it('skips the fetch for an unset logbook (id < 1)', async () => {
        expect(await fetchLogbookCount(0)).toBeNull();
    });
});
