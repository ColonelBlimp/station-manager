import { afterEach, describe, expect, it, vi } from 'vitest';
import { stationState, STATION_KEYS } from './station.svelte';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockJSON(status: number, body: unknown): ReturnType<typeof vi.fn> {
    const spy = vi.fn(
        (_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                }),
            ),
    );
    vi.stubGlobal('fetch', spy);
    return spy;
}

function configBody(overrides: Record<string, string> = {}) {
    return {
        logging_station: {
            station_callsign: '7Q5MLV',
            my_lat: 'S011 26.250',
            my_lon: 'E034 02.500',
            ...overrides,
        },
        station: { default_power: 50 },
    };
}

describe('stationState', () => {
    it('load fills the form and starts not-dirty', async () => {
        mockJSON(200, configBody());
        await stationState.load();
        expect(stationState.loaded).toBe(true);
        expect(stationState.form.station_callsign).toBe('7Q5MLV');
        expect(stationState.dirty).toBe(false);
    });

    it('ensures every rendered key exists (blank) even when the config omits it', async () => {
        mockJSON(200, configBody()); // no my_sig / my_postal_code / my_antenna …
        await stationState.load();
        for (const k of STATION_KEYS) {
            expect(stationState.form[k], `form has "${k}"`).toBeDefined();
        }
        expect(stationState.form.my_sig).toBe('');
        expect(stationState.form.my_postal_code).toBe('');
    });

    it('edits flip dirty; reset reverts to the loaded snapshot', async () => {
        mockJSON(200, configBody());
        await stationState.load();
        stationState.form.station_callsign = 'CHANGED';
        expect(stationState.dirty).toBe(true);
        stationState.reset();
        expect(stationState.form.station_callsign).toBe('7Q5MLV');
        expect(stationState.dirty).toBe(false);
    });

    it('save is a no-op when not dirty (no PUT)', async () => {
        const spy = mockJSON(200, configBody());
        await stationState.load();
        const getCalls = spy.mock.calls.length;
        await stationState.save();
        // No new fetch beyond the load's GET.
        expect(spy.mock.calls.length).toBe(getCalls);
    });

    it('save PUTs when dirty and clears dirty on success', async () => {
        const spy = mockJSON(200, configBody());
        await stationState.load();
        stationState.form.station_callsign = '7Q8AC';
        expect(stationState.dirty).toBe(true);
        await stationState.save();
        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        expect(stationState.dirty).toBe(false);
    });
});
