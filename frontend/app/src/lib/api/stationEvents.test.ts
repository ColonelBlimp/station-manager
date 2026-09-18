import { describe, it, expect, afterEach, vi } from 'vitest';
import { fetchStationEvents, stationEventsQuery } from './stationEvents';

afterEach(() => {
    vi.unstubAllGlobals();
});

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

describe('stationEventsQuery', () => {
    it('sends only the filters that are set, with the limit', () => {
        expect(stationEventsQuery({})).toBe('?limit=1000');
        expect(stationEventsQuery({ category: 'alarm' })).toBe('?category=alarm&limit=1000');
        expect(stationEventsQuery({ category: 'notification', severity: 'error', limit: 50 })).toBe(
            '?category=notification&severity=error&limit=50'
        );
    });
});

describe('fetchStationEvents', () => {
    it('reads /v1/station-events with the filter and returns the items', async () => {
        let url = '';
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                url = urlOf(input);
                return Promise.resolve(
                    new Response(
                        JSON.stringify({
                            items: [
                                {
                                    id: 9,
                                    category: 'alarm',
                                    kind: 'tx_alarm.raised',
                                    severity: 'error',
                                    occurred_at: '2026-09-18T10:00:00Z',
                                    build: 'v-test',
                                    detail: { code: 'tx_unconfirmed' },
                                },
                            ],
                        }),
                        { status: 200, headers: { 'Content-Type': 'application/json' } }
                    )
                );
            })
        );
        const out = await fetchStationEvents({ category: 'alarm', severity: 'error' });
        expect(url).toBe('/v1/station-events?category=alarm&severity=error&limit=1000');
        expect(out.kind).toBe('ok');
        if (out.kind === 'ok') expect(out.items[0].kind).toBe('tx_alarm.raised');
    });

    it('turns a daemon rejection and a malformed body into errors, never a throw', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response(
                        JSON.stringify({
                            code: 'invalid_field_value',
                            message: 'unknown category',
                        }),
                        {
                            status: 400,
                            headers: { 'Content-Type': 'application/json' },
                        }
                    )
                )
            )
        );
        const rejected = await fetchStationEvents({});
        expect(rejected.kind).toBe('error');

        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response(JSON.stringify({ nope: true }), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                )
            )
        );
        const malformed = await fetchStationEvents({});
        expect(malformed).toEqual({
            kind: 'error',
            message: 'Unexpected station events response.',
        });
    });
});
