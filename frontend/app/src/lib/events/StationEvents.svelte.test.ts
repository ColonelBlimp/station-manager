// Rendered proofs for the Station Events page (W-0020 AC1, AC5, AC6): rows newest
// first across categories with client wording, filters that narrow by asking the
// daemon, a count line and an empty state that name the filter, and an error
// with a retry. The daemon is a stubbed fetch keyed on the query string.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import StationEvents from './StationEvents.svelte';
import { stationEventsState } from './stationEvents.svelte';

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

const ROWS = [
    {
        id: 4,
        category: 'alarm',
        kind: 'tx_alarm.cleared',
        severity: 'info',
        occurred_at: '2026-09-18T10:00:07Z',
        build: 'v-test',
        detail: { code: 'tx_unconfirmed', active_ms: 700 },
    },
    {
        id: 3,
        category: 'alarm',
        kind: 'tx_alarm.raised',
        severity: 'error',
        occurred_at: '2026-09-18T10:00:06Z',
        build: 'v-test',
        detail: { code: 'tx_unconfirmed' },
    },
    {
        id: 2,
        category: 'notification',
        kind: 'forward.failed',
        severity: 'warn',
        occurred_at: '2026-09-18T09:00:00Z',
        build: 'v-test',
        detail: { forwarder: 'qrz', action: 'insert', attempts: 2 },
    },
    {
        id: 1,
        category: 'notification',
        kind: 'export.adif_failed',
        severity: 'error',
        occurred_at: '2026-09-18T08:00:00Z',
        build: 'v-test',
        detail: 'not an object',
    },
];

/** Stub the daemon: answers each GET from `byQuery` (the query string after the
 *  path), records every URL asked. */
function stubDaemon(byQuery: Record<string, unknown[] | { status: number }>): { urls: string[] } {
    const urls: string[] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL) => {
            const url = urlOf(input);
            urls.push(url);
            const q = url.replace('/v1/station-events', '');
            const answer = byQuery[q];
            if (answer === undefined) return Promise.reject(new Error(`unexpected fetch: ${url}`));
            if (!Array.isArray(answer)) {
                return Promise.resolve(
                    new Response(
                        JSON.stringify({ code: 'db_error', message: 'database operation failed' }),
                        {
                            status: answer.status,
                            headers: { 'Content-Type': 'application/json' },
                        }
                    )
                );
            }
            return Promise.resolve(
                new Response(JSON.stringify({ items: answer }), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
    return { urls };
}

beforeEach(() => stationEventsState.reset());
afterEach(() => vi.unstubAllGlobals());

describe('StationEvents page', () => {
    it('loads on mount and lists both categories newest first with client wording', async () => {
        stubDaemon({ '?limit=1000': ROWS });
        render(StationEvents);
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4));

        const rows = screen.getAllByRole('listitem').map((li) => li.textContent ?? '');
        expect(rows[0]).toContain('TX alarm cleared');
        expect(rows[0]).toContain('stood 700 ms');
        expect(rows[1]).toContain('TX alarm raised');
        expect(rows[2]).toContain('Upload failed');
        expect(rows[2]).toContain('qrz · insert · 2 attempts');
        // A malformed detail never reaches the page as text.
        expect(rows[3]).toContain('Details unavailable');
        expect(rows[3]).not.toContain('not an object');
        expect(screen.getByTestId('count-line').textContent).toBe('4 events');
        expect(screen.getByRole('heading', { name: 'Station Events' })).toBeTruthy();
    });

    it('a filter chip narrows by asking the daemon, and the count line names the filter', async () => {
        const daemon = stubDaemon({
            '?limit=1000': ROWS,
            '?category=alarm&limit=1000': ROWS.slice(0, 2),
            '?category=alarm&severity=error&limit=1000': [ROWS[1]],
        });
        render(StationEvents);
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4));

        await fireEvent.click(screen.getByRole('button', { name: 'Alarms' }));
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(2));
        expect(screen.getByTestId('count-line').textContent).toBe('2 alarm events');
        // The narrowing is the DAEMON's answer to a new query, never a client slice
        // of the unfiltered page (which would hide older matches beyond the window).
        expect(daemon.urls).toContain('/v1/station-events?category=alarm&limit=1000');

        await fireEvent.click(screen.getByRole('button', { name: 'Error' }));
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(1));
        expect(screen.getByTestId('count-line').textContent).toBe(
            '1 alarm event at severity error'
        );
        expect(daemon.urls).toContain(
            '/v1/station-events?category=alarm&severity=error&limit=1000'
        );
        expect(screen.getByRole('button', { name: 'Alarms' }).getAttribute('aria-pressed')).toBe(
            'true'
        );
    });

    it('does not label rows from the previous filter as the new filter while loading', async () => {
        let finishAlarmLoad: ((response: Response) => void) | undefined;
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                const url = urlOf(input);
                if (url === '/v1/station-events?limit=1000') {
                    return Promise.resolve(
                        new Response(JSON.stringify({ items: ROWS }), { status: 200 })
                    );
                }
                if (url === '/v1/station-events?category=alarm&limit=1000') {
                    return new Promise<Response>((resolve) => {
                        finishAlarmLoad = resolve;
                    });
                }
                return Promise.reject(new Error(`unexpected fetch: ${url}`));
            })
        );
        render(StationEvents);
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4));

        await fireEvent.click(screen.getByRole('button', { name: 'Alarms' }));
        await waitFor(() =>
            expect(screen.getByTestId('count-line').textContent).toBe('Loading alarm events…')
        );
        expect(screen.queryByRole('listitem')).toBeNull();

        if (!finishAlarmLoad) throw new Error('alarm request was not sent');
        finishAlarmLoad(new Response(JSON.stringify({ items: ROWS.slice(0, 2) }), { status: 200 }));
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(2));
        expect(screen.getByTestId('count-line').textContent).toBe('2 alarm events');
    });

    it('ignores an older filter response that arrives after the newer one', async () => {
        let finishAlarmLoad: ((response: Response) => void) | undefined;
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                const url = urlOf(input);
                if (url === '/v1/station-events?limit=1000') {
                    return Promise.resolve(
                        new Response(JSON.stringify({ items: ROWS }), { status: 200 })
                    );
                }
                if (url === '/v1/station-events?category=alarm&limit=1000') {
                    return new Promise<Response>((resolve) => {
                        finishAlarmLoad = resolve;
                    });
                }
                if (url === '/v1/station-events?category=notification&limit=1000') {
                    return Promise.resolve(
                        new Response(JSON.stringify({ items: ROWS.slice(2) }), { status: 200 })
                    );
                }
                return Promise.reject(new Error(`unexpected fetch: ${url}`));
            })
        );
        render(StationEvents);
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4));

        stationEventsState.category = 'alarm';
        const olderLoad = stationEventsState.load();
        stationEventsState.category = 'notification';
        const newerLoad = stationEventsState.load();
        await newerLoad;
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(2));
        expect(screen.getByTestId('count-line').textContent).toBe('2 notification events');

        if (!finishAlarmLoad) throw new Error('alarm request was not sent');
        finishAlarmLoad(new Response(JSON.stringify({ items: ROWS.slice(0, 2) }), { status: 200 }));
        await olderLoad;
        await waitFor(() => {
            const rows = screen.getAllByRole('listitem');
            expect(rows).toHaveLength(2);
            expect(rows[0].textContent).toContain('Upload failed');
            expect(screen.getByTestId('count-line').textContent).toBe('2 notification events');
        });
    });

    it('an empty filtered list names the filter — it reads as a filter, not a fault', async () => {
        stubDaemon({ '?limit=1000': ROWS, '?severity=warn&limit=1000': [] });
        render(StationEvents);
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4));

        await fireEvent.click(screen.getByRole('button', { name: 'Warn' }));
        await waitFor(() =>
            expect(screen.getByTestId('count-line').textContent).toBe('No events at severity warn.')
        );
        expect(screen.queryByRole('listitem')).toBeNull();
        expect(screen.queryByText(/Could not|failed/)).toBeNull();
    });

    it('a daemon failure shows the error with a Retry that refetches', async () => {
        const daemon = stubDaemon({ '?limit=1000': { status: 500 } });
        render(StationEvents);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy());
        expect(screen.getByTestId('count-line').textContent).toBe('Could not load events.');
        expect(screen.queryByRole('listitem')).toBeNull();

        vi.unstubAllGlobals();
        stubDaemon({ '?limit=1000': ROWS });
        await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        await waitFor(() => expect(screen.getAllByRole('listitem')).toHaveLength(4));
        expect(daemon.urls[0]).toBe('/v1/station-events?limit=1000');
    });
});
