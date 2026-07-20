import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchStation, saveStation } from './config';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockJSON(status: number, body: unknown) {
    const spy = vi.fn((_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
        Promise.resolve(
            new Response(JSON.stringify(body), {
                status,
                headers: { 'Content-Type': 'application/json' },
            })
        )
    );
    vi.stubGlobal('fetch', spy);
    return spy;
}

// A realistic GET /v1/config: logging_station carries fields the Station form
// does NOT render (my_lat/my_lon, derived by the daemon), and there is an
// operational `station` block the section never edits.
function configBody() {
    return {
        logging_station: {
            station_callsign: '7Q5MLV',
            operator: '7Q5MLV',
            my_gridsquare: 'KH78an',
            my_lat: 'S011 26.250',
            my_lon: 'E034 02.500',
        },
        station: { amp_enabled: false, default_power: 50, operating_bands: ['20m'] },
    };
}

describe('fetchStation', () => {
    it('reads logging_station including the fields the form never renders', async () => {
        mockJSON(200, configBody());
        const res = await fetchStation();
        expect(res.kind).toBe('ok');
        if (res.kind !== 'ok') return;
        expect(res.config.station.station_callsign).toBe('7Q5MLV');
        // The derived fields the form never shows must still be carried.
        expect(res.config.station.my_lat).toBe('S011 26.250');
        expect(res.config.station.my_lon).toBe('E034 02.500');
    });

    it('errors (never throws) on a non-2xx GET', async () => {
        mockJSON(500, { message: 'boom' });
        const res = await fetchStation();
        expect(res.kind).toBe('error');
    });
});

describe('saveStation — data safety', () => {
    it('PUTs the FULL logging_station (unrendered fields preserved) and does NOT send the operational station block', async () => {
        const spy = mockJSON(200, configBody());
        const loaded = await fetchStation();
        expect(loaded.kind).toBe('ok');
        if (loaded.kind !== 'ok') return;

        // Edit one rendered field; the derived my_lat/my_lon are untouched but
        // MUST ride back in the PUT — the daemon replaces the whole block, so an
        // omitted field would be zeroed (the config-wipe this guards against).
        const cfg = loaded.config;
        cfg.station.station_callsign = '7Q8AC';
        await saveStation(cfg);

        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        const sent = JSON.parse(put![1]!.body as string) as {
            logging_station: Record<string, string>;
            station?: unknown;
        };
        expect(sent.logging_station.station_callsign).toBe('7Q8AC');
        expect(sent.logging_station.my_lat).toBe('S011 26.250');
        expect(sent.logging_station.my_lon).toBe('E034 02.500');
        // The operational block is intentionally absent — echoing a stale copy
        // would clobber a concurrent amp/power/band change (review #3).
        expect('station' in sent).toBe(false);
    });

    it('errors (never throws) on a non-2xx PUT', async () => {
        mockJSON(400, { message: 'invalid' });
        const res = await saveStation({ station: { station_callsign: 'X' } });
        expect(res.kind).toBe('error');
    });
});
