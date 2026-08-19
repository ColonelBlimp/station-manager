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

describe('stationState QSL defaults (config qsl block, ported from the config SPA)', () => {
    it('loads the QSL defaults from the qsl block', async () => {
        mockJSON(200, {
            ...configBody(),
            qsl: { qsl_via: 'via M0XXX', qslmsg: 'Tnx QSO 73', qsl_sent_via: 'B' },
        });
        await stationState.load();
        expect(stationState.qslForm).toEqual({
            qsl_via: 'via M0XXX',
            qslmsg: 'Tnx QSO 73',
            qsl_sent_via: 'B',
        });
        expect(stationState.dirty).toBe(false);
    });

    it('a config with no qsl block loads blank defaults and is not dirty', async () => {
        mockJSON(200, configBody()); // no qsl block
        await stationState.load();
        expect(stationState.qslForm).toEqual({ qsl_via: '', qslmsg: '', qsl_sent_via: '' });
        expect(stationState.dirty).toBe(false);
    });

    it('editing a QSL default flips dirty; reset reverts it', async () => {
        mockJSON(200, configBody());
        await stationState.load();
        stationState.qslForm.qsl_sent_via = 'D';
        expect(stationState.dirty).toBe(true);
        stationState.reset();
        expect(stationState.qslForm.qsl_sent_via).toBe('');
        expect(stationState.dirty).toBe(false);
    });

    it('a QSL-only edit sends only the whole qsl block — not logging_station', async () => {
        const spy = mockJSON(200, configBody());
        await stationState.load();
        stationState.qslForm.qsl_via = 'LoTW';
        stationState.qslForm.qsl_sent_via = 'E';
        await stationState.save();

        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        const sent = JSON.parse((put![1] as RequestInit).body as string) as Record<string, unknown>;
        expect(sent.qsl).toEqual({ qsl_via: 'LoTW', qslmsg: '', qsl_sent_via: 'E' });
        // logging_station is NOT resent — the identity was untouched, so a stale
        // copy must not clobber a concurrent change (clean-room review 17bb2ffa P1).
        expect('logging_station' in sent).toBe(false);
        expect(stationState.dirty).toBe(false);
    });

    it('a station-only edit sends only logging_station — not the qsl block', async () => {
        const spy = mockJSON(200, configBody());
        await stationState.load();
        stationState.form.operator = 'CHANGED';
        await stationState.save();

        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        const sent = JSON.parse((put![1] as RequestInit).body as string) as Record<string, unknown>;
        expect((sent.logging_station as Record<string, string>).operator).toBe('CHANGED');
        expect('qsl' in sent).toBe(false);
    });
});

// A failed RELOAD must mark the section unloaded, not just record an error.
// Settings is mounted behind a router branch (App.svelte:100), so navigating
// away unmounts it while this module — a singleton — survives; returning
// re-fires onMount → load(). Leaving `loaded` true there renders the previous
// session's identity as though it were current, and logging_station is
// round-tripped WHOLE, so one edit rewrites every other field at its stale
// value. Full reasoning in email.svelte.test.ts (clean-room review
// dcb0316e69b9); this is the same defect in a sibling section.
describe('stationState stale-reload guard', () => {
    it('a failed reload after a successful one clears loaded', async () => {
        mockJSON(200, configBody());
        await stationState.load();
        expect(stationState.loaded).toBe(true);

        mockJSON(503, {});
        await stationState.load();

        expect(stationState.loaded).toBe(false);
        expect(stationState.error).not.toBe('');
    });

    it('a pending reload immediately marks the section unloaded', async () => {
        const body = configBody();
        mockJSON(200, body);
        await stationState.load();

        let release!: (response: Response) => void;
        vi.stubGlobal(
            'fetch',
            vi.fn(
                () =>
                    new Promise<Response>((resolve) => {
                        release = resolve;
                    })
            )
        );
        const reload = stationState.load();

        expect(stationState.loading).toBe(true);
        expect(stationState.loaded).toBe(false);

        release(
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
        await reload;
        expect(stationState.loaded).toBe(true);
    });

    it('save is refused while the section is not loaded', async () => {
        const spy = mockJSON(200, configBody());
        await stationState.load();
        const calls = spy.mock.calls.length;
        stationState.form.operator = 'STALE';
        stationState.loaded = false;

        await stationState.save();

        expect(spy.mock.calls).toHaveLength(calls);
    });
});
