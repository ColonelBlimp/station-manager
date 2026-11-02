// W-0012 unrecognised-callsign slice (operator rulings 2026-11-02): the enrich
// seam must keep the daemon's explicit "not recognised" verdict even when the
// response carries neither a country nor a station object — collapsing it to
// null (the "nothing known" result) would lose the one signal the Phone form
// warns on.

import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiEnrich } from './seams';

afterEach(() => {
    vi.unstubAllGlobals();
});

function mockEnrich(body: unknown): void {
    vi.stubGlobal(
        'fetch',
        vi.fn((): Promise<Response> =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

describe('apiEnrich: the not-recognised verdict', () => {
    it('keeps country_source not_found with no country or station object', async () => {
        mockEnrich({ callsign: '0H5RW', country_source: 'not_found', station_source: 'none' });
        const got = await apiEnrich('0H5RW');
        expect(got).not.toBeNull();
        expect(got?.countryNotFound).toBe(true);
        expect(got?.country).toBe('');
    });

    it('still returns null when both layers are none (nothing known, no verdict)', async () => {
        mockEnrich({ callsign: 'G4ABC', country_source: 'none', station_source: 'none' });
        expect(await apiEnrich('G4ABC')).toBeNull();
    });

    it('a country result is never not-found', async () => {
        mockEnrich({
            callsign: 'OH5RW',
            country: { name: 'Finland', ccode: 'FI' },
            country_source: 'country_table',
            station_source: 'none',
        });
        const got = await apiEnrich('OH5RW');
        expect(got?.countryNotFound).toBe(false);
        expect(got?.country).toBe('Finland');
    });
});
