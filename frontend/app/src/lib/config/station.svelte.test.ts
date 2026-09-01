import { afterEach, describe, expect, it, vi } from 'vitest';
import { stationState, STATION_KEYS, setStationSaved } from './station.svelte';
import type { StationFields } from '../api/config';
import { toasts } from '../ui/toasts.svelte';

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
        setup_complete: true,
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

// F-01 — the logging_station baseline is a data-safety precondition. An invalid
// GET must leave the section unloaded, so a whole-block PUT can never be reached
// from it; and a malformed successful PUT response must not clear the form, push
// the shared station context, or move the pristine baseline (frontend-app-review
// F-01; the strict decoder lives in api/config.ts).
describe('stationState — F-01 baseline safety', () => {
    it('an invalid GET baseline leaves the section unloaded and unreachable by save', async () => {
        const spy = mockJSON(200, { setup_complete: true, logging_station: [] }); // syntactically ok, semantically invalid
        await stationState.load();
        expect(stationState.loaded).toBe(false);
        expect(stationState.error).not.toBe('');

        const afterLoad = spy.mock.calls.length;
        await stationState.save(); // must be a no-op: the section is not loaded
        expect(spy.mock.calls).toHaveLength(afterLoad); // no PUT was issued
    });

    it('a malformed 2xx save response cannot alter the form, shared context, or baseline', async () => {
        mockJSON(200, configBody());
        await stationState.load();
        expect(stationState.loaded).toBe(true);

        stationState.form.my_name = 'Marc'; // an edit → dirty and saveable
        expect(stationState.dirty).toBe(true);
        const formBefore = JSON.stringify(stationState.form);

        let contextPushed = false;
        setStationSaved(() => {
            contextPushed = true;
        });

        mockJSON(200, { setup_complete: true, qsl: {} }); // 2xx, but no authoritative logging_station
        await stationState.save();

        // The malformed response did not land: the form keeps the operator's edit
        // (never cleared to blanks), the shared context was never pushed, and the
        // baseline is unchanged — still dirty against the loaded snapshot, still loaded.
        expect(JSON.stringify(stationState.form)).toBe(formBefore);
        expect(contextPushed).toBe(false);
        expect(stationState.dirty).toBe(true);
        expect(stationState.loaded).toBe(true);
    });
});

// PT-6: a save whose PUT response carries `durability:"unconfirmed"` (the daemon
// applied it and it is live on disk, but the parent-directory fsync failed after
// the atomic rename, so crash-durability is not confirmed) must surface ONE
// unambiguous outcome — the caveat toast — and SUPPRESS the ordinary "saved" one.
// This exercises the whole chain: response body → saveStation extraction →
// section toast decision → noteConfigDurability. Reverting either the API-layer
// extraction or the section's suppression flips both assertions.
describe('stationState durability caveat', () => {
    it('surfaces the caveat and suppresses the ordinary saved toast when unconfirmed', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        mockJSON(200, { ...configBody(), durability: 'unconfirmed' });
        await stationState.load();
        stationState.form.station_callsign = '7Q8AC';
        await stationState.save();
        expect(warn, 'the durability caveat is shown').toHaveBeenCalledOnce();
        expect(String(warn.mock.calls[0][0])).toMatch(/survive a crash/i);
        expect(info, 'the ordinary saved toast is suppressed').not.toHaveBeenCalled();
    });

    it('shows the ordinary saved toast and no caveat on a durable save', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        mockJSON(200, configBody()); // no `durability` field — an ordinary durable write
        await stationState.load();
        stationState.form.station_callsign = '7Q8AC';
        await stationState.save();
        expect(info).toHaveBeenCalledWith('Station settings saved.');
        expect(warn, 'no caveat on a durable save').not.toHaveBeenCalled();
    });
});

// F-04c (ADR 0078): a Station save whose PUT TIMED OUT is outcome-unknown, not
// failed. save() re-reads the authoritative config, overlays the operator's OWN
// edits onto the freshly stored block (so a concurrent change to an UNTOUCHED
// field is adopted, never reverted), rebaselines to stored, pushes the STORED
// identity — not the merged draft, which may hold unsaved edits — to the shared
// context, and reports outcome-unknown. A re-read that also fails stays unknown
// with the double-fault guidance; a NON-timeout error still says "Save failed".
function timeoutError(): Error {
    return Object.assign(new Error('timed out'), { name: 'TimeoutError' });
}

// A fetch stub: the PUT rejects (timeout by default), each GET returns the next
// queued config body (load first, reconcile re-read second).
function stubPutTimeoutGets(getBodies: unknown[], putReject: () => Error = timeoutError) {
    let get = 0;
    const spy = vi.fn((_url: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'PUT') return Promise.reject(putReject());
        const body = getBodies[Math.min(get, getBodies.length - 1)];
        get++;
        return Promise.resolve(
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
    });
    vi.stubGlobal('fetch', spy);
    return spy;
}

describe('stationState — timed-out reconciliation (F-04c)', () => {
    it('re-reads, keeps the operator edit, adopts a concurrent untouched change, warns unknown, never "saved"', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        // GET1 (load): operator op1 / my_name name1. GET2 (re-read): a second
        // writer changed the UNTOUCHED my_name to name2; operator still op1.
        stubPutTimeoutGets([
            configBody({ operator: 'op1', my_name: 'name1' }),
            configBody({ operator: 'op1', my_name: 'name2' }),
        ]);

        await stationState.load();
        stationState.form.operator = 'op2'; // operator-owned edit; my_name untouched

        const saved: StationFields[] = [];
        setStationSaved((s) => {
            saved.push({ ...s });
        });

        await stationState.save();

        // Merge: operator (owned) kept; my_name (untouched) adopts stored.
        expect(stationState.form.operator).toBe('op2');
        expect(stationState.form.my_name).toBe('name2');
        // Outcome-unknown warn with the success re-read tail; no failure, no "saved".
        expect(warn).toHaveBeenCalledOnce();
        expect(String(warn.mock.calls[0][0])).toMatch(/the outcome is unknown/);
        expect(String(warn.mock.calls[0][0])).toMatch(/review and save again/);
        expect(info).not.toHaveBeenCalledWith('Station settings saved.');
        expect(error).not.toHaveBeenCalled();
        // Context is fed the STORED identity (op1/name2), NOT the merged draft (op2).
        expect(saved).toHaveLength(1);
        expect(saved[0].operator).toBe('op1');
        expect(saved[0].my_name).toBe('name2');
        // Rebaselined to stored ⇒ still dirty, because op2 is unsaved.
        expect(stationState.dirty).toBe(true);
    });

    it('when the reconciling re-read ALSO fails, stays outcome-unknown, keeps edits, does not rebaseline', async () => {
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        let get = 0;
        vi.stubGlobal(
            'fetch',
            vi.fn((_url: RequestInfo | URL, init?: RequestInit) => {
                if (init?.method === 'PUT') return Promise.reject(timeoutError());
                get++;
                if (get === 1) {
                    return Promise.resolve(
                        new Response(JSON.stringify(configBody({ operator: 'op1' })), {
                            status: 200,
                            headers: { 'Content-Type': 'application/json' },
                        })
                    );
                }
                return Promise.resolve(new Response('{}', { status: 503 })); // re-read fails
            })
        );

        await stationState.load();
        stationState.form.operator = 'op2';
        await stationState.save();

        expect(error).toHaveBeenCalledOnce();
        expect(String(error.mock.calls[0][0])).toMatch(/the outcome is unknown/);
        expect(String(error.mock.calls[0][0])).toMatch(
            /check its status before deciding whether to retry/
        );
        expect(stationState.form.operator).toBe('op2'); // edits kept
        expect(stationState.dirty).toBe(true);
        expect(stationState.loaded).toBe(true);
        expect(info).not.toHaveBeenCalledWith('Station settings saved.');
    });

    it('a NON-timeout save error still reports "Save failed" and does not re-read', async () => {
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        const spy = stubPutTimeoutGets(
            [configBody({ operator: 'op1' })],
            () => new TypeError('Failed to fetch') // generic network, NOT a timeout
        );

        await stationState.load();
        const afterLoad = spy.mock.calls.length;
        stationState.form.operator = 'op2';
        await stationState.save();

        expect(error).toHaveBeenCalledOnce();
        expect(String(error.mock.calls[0][0])).toMatch(/Save failed/);
        // Only the failed PUT beyond the load GET — NO reconcile re-read.
        expect(spy.mock.calls.length).toBe(afterLoad + 1);
    });
});
