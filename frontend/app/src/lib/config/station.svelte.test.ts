import { afterEach, describe, expect, it, vi } from 'vitest';
import { stationState, STATION_KEYS, setStationSaved } from './station.svelte';
import type { StationFields } from '../api/config';

afterEach(() => {
    vi.restoreAllMocks();
    setStationSaved(() => {}); // clear any per-test onSaved hook
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

    it('hands onSaved the PUT-response identity and does NOT do a second GET', async () => {
        // GET (load) returns BEFORE; PUT (save) returns AFTER — the daemon's
        // authoritative post-save value. onSaved must receive AFTER, and there
        // must be NO third fetch (the refresh GET that used to fail and either
        // wipe or stale the shared context — review round 2 #1 / round 3 #1).
        let calls = 0;
        vi.stubGlobal(
            'fetch',
            vi.fn((_u: RequestInfo | URL, _i?: RequestInit) => {
                calls++;
                const operator = calls === 1 ? 'BEFORE' : 'AFTER';
                return Promise.resolve(
                    new Response(JSON.stringify(configBody({ operator })), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        await stationState.load();
        expect(stationState.form.operator).toBe('BEFORE');

        const saved: StationFields[] = [];
        setStationSaved((station) => {
            saved.push({ ...station });
        });
        stationState.form.operator = 'edited'; // dirty; the response is authoritative
        await stationState.save();

        expect(saved).toHaveLength(1);
        expect(saved[0].operator).toBe('AFTER');
        expect(calls).toBe(2); // 1 GET (load) + 1 PUT (save) — no refresh GET
    });

    it('holds the save latch through the onSaved refresh (no overlapping saves)', async () => {
        mockJSON(200, configBody());
        await stationState.load();

        // Gate the injected refresh so we can observe the window between the PUT
        // completing and onSaved finishing — the window where the pre-fix code
        // had already cleared `saving` (review 2026-07-20 round 2 #2).
        let release!: () => void;
        const gate = new Promise<void>((r) => (release = r));
        let refreshes = 0;
        setStationSaved(async () => {
            refreshes++;
            await gate;
        });

        stationState.form.station_callsign = '7Q8AC';
        const first = stationState.save();
        await vi.waitFor(() => expect(refreshes).toBe(1)); // onSaved entered + gated
        expect(stationState.saving).toBe(true); // STILL latched during the refresh

        await stationState.save(); // a concurrent save must be a no-op
        expect(refreshes).toBe(1); // it did NOT start a second refresh

        release();
        await first;
        expect(stationState.saving).toBe(false);
    });
});
