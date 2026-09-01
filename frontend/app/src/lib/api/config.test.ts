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
        setup_complete: true,
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
        // A station-only edit: send ONLY logging_station.
        await saveStation({ station: cfg.station });

        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        const sent = JSON.parse(put![1]!.body as string) as {
            logging_station: Record<string, string>;
            qsl?: unknown;
            station?: unknown;
        };
        expect(sent.logging_station.station_callsign).toBe('7Q8AC');
        expect(sent.logging_station.my_lat).toBe('S011 26.250');
        expect(sent.logging_station.my_lon).toBe('E034 02.500');
        // qsl is NOT sent for a station-only edit — resending a stale qsl would
        // clobber a concurrent change to it (clean-room review 17bb2ffa P1).
        expect('qsl' in sent).toBe(false);
        // The operational block is likewise absent — echoing a stale copy would
        // clobber a concurrent amp/power/band change (review #3).
        expect('station' in sent).toBe(false);
    });

    it('sends ONLY the qsl block for a QSL-only edit (logging_station untouched)', async () => {
        const spy = mockJSON(200, configBody());
        await saveStation({ qsl: { qsl_via: 'LoTW', qslmsg: '', qsl_sent_via: 'E' } });

        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        const sent = JSON.parse(put![1]!.body as string) as Record<string, unknown>;
        expect(sent.qsl).toEqual({ qsl_via: 'LoTW', qslmsg: '', qsl_sent_via: 'E' });
        expect('logging_station' in sent).toBe(false);
        expect('station' in sent).toBe(false);
    });

    it('errors (never throws) on a non-2xx PUT', async () => {
        mockJSON(400, { message: 'invalid' });
        const res = await saveStation({ station: { station_callsign: 'X' } });
        expect(res.kind).toBe('error');
    });
});

// F-01 — logging_station is a REQUIRED, all-string plain-object block. A
// syntactically-successful but semantically-invalid read (missing/null/array/
// non-string members, or a blank callsign once setup is complete) must surface as
// an ERROR, not a silently-empty "loaded" block that a whole-block PUT could send
// back as blanks (frontend-app-review.md F-01).
describe('fetchStation — F-01 strict logging_station', () => {
    it.each([
        ['missing', { setup_complete: true }],
        ['null', { setup_complete: true, logging_station: null }],
        ['array', { setup_complete: true, logging_station: [] }],
        ['string', { setup_complete: true, logging_station: 'nope' }],
    ])('rejects a %s logging_station as a load error', async (_label, body) => {
        mockJSON(200, body);
        expect((await fetchStation()).kind).toBe('error');
    });

    it('rejects a non-string logging_station member', async () => {
        mockJSON(200, {
            setup_complete: true,
            logging_station: { station_callsign: '7Q5MLV', my_dxcc: 492 },
        });
        expect((await fetchStation()).kind).toBe('error');
    });

    it.each([
        ['missing', { logging_station: {} }],
        ['null', { setup_complete: null, logging_station: {} }],
        ['string', { setup_complete: 'false', logging_station: {} }],
    ])('rejects a %s setup_complete discriminator', async (_label, body) => {
        mockJSON(200, body);
        expect((await fetchStation()).kind).toBe('error');
    });

    it('requires a non-empty station_callsign once setup is complete', async () => {
        mockJSON(200, { setup_complete: true, logging_station: { station_callsign: '   ' } });
        expect((await fetchStation()).kind).toBe('error');
        mockJSON(200, { setup_complete: true, logging_station: {} });
        expect((await fetchStation()).kind).toBe('error');
        mockJSON(200, { setup_complete: true, logging_station: { station_callsign: '7Q5MLV' } });
        expect((await fetchStation()).kind).toBe('ok');
    });

    it('preserves the pre-setup case: setup_complete false with an empty logging_station', async () => {
        mockJSON(200, { setup_complete: false, logging_station: {} });
        const res = await fetchStation();
        expect(res.kind).toBe('ok');
        if (res.kind !== 'ok') return;
        expect(res.config.station).toEqual({});
    });
});

describe('saveStation — F-01 strict response', () => {
    it('rejects a malformed 2xx save response (missing logging_station)', async () => {
        mockJSON(200, { setup_complete: true, qsl: {} }); // 2xx, but no authoritative logging_station
        const res = await saveStation({ station: { station_callsign: '7Q5MLV' } });
        expect(res.kind).toBe('error');
    });
});

// F-04c (ADR 0078): a config PUT whose response was lost to a FIRED timeout is
// AMBIGUOUS — the daemon may already have replaced the block — so saveStation
// must carry that out as `timedOut` for the section to reconcile by re-reading,
// rather than flattening it into a plain "failed". Only a fired timeout is
// marked: an HTTP rejection IS a definite rejection (the daemon answered), while
// a generic non-timeout transport failure is left unmarked too — it is not
// proven to have committed OR failed, so it keeps its existing wording with no
// new claim either way.
describe('saveStation — timed-out write is ambiguous (F-04c)', () => {
    it('marks a fired timeout as timedOut (outcome-unknown)', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const res = await saveStation({ station: { station_callsign: '7Q5MLV' } });
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBe(true);
    });

    it('does NOT mark a non-timeout transport failure as timedOut', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
        );
        const res = await saveStation({ station: { station_callsign: '7Q5MLV' } });
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBeFalsy();
    });

    it('does NOT mark an HTTP rejection as timedOut (the daemon answered — definite)', async () => {
        mockJSON(400, { message: 'invalid' });
        const res = await saveStation({ station: { station_callsign: 'X' } });
        expect(res.kind).toBe('error');
        if (res.kind !== 'error') return;
        expect(res.timedOut).toBeFalsy();
    });
});
