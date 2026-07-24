// Render-path smoke for the Logbook page port: the browse surface mounts in the
// app shell, loads a logbook + page through the state module, and renders the
// table with the tri-state callsign tint hooks. The selection/email mechanics
// are covered by logbook.test.ts; this pins the state ↔ view wiring.

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Logbook from './Logbook.svelte';
import { logbookState } from './logbook.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

// vi.fn fetch stubs receive RequestInfo | URL; String(new Request(...)) would
// stringify to '[object Request]', so narrow explicitly (tests pass strings,
// but the signature must be honest for lint's no-base-to-string).
function urlText(input: RequestInfo | URL): string {
    return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function jsonResponse(body: unknown): Response {
    return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
    });
}

// Stub the daemon: logbook list → count → one QSO page (+ /v1/config for the
// mailer/forwarders best-effort reads).
beforeEach(() => {
    vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL) => {
            const url = urlText(input);
            if (url.startsWith('/v1/config')) {
                return Promise.resolve(
                    jsonResponse({
                        mailer: { enabled: false },
                        forwarders: [{ name: 'qrz', type: 'qrz', enabled: true }],
                    })
                );
            }
            if (url === '/v1/logbook') {
                return Promise.resolve(
                    jsonResponse([{ id: 1, name: 'Malawi 2026', callsign: '7Q5MLV' }])
                );
            }
            if (url.includes('/count')) {
                return Promise.resolve(jsonResponse({ count: 2 }));
            }
            if (url.includes('/qso')) {
                return Promise.resolve(
                    jsonResponse({
                        items: [
                            {
                                id: 1,
                                uuid: 'u-1',
                                qso_date: '20260711',
                                time_on: '144346',
                                call: 'EA3IE',
                                band: '15m',
                                freq: '21.074',
                                mode: 'FT8',
                                country: 'Spain',
                                qrzcom_qso_upload_status: 'Y',
                            },
                            {
                                id: 2,
                                uuid: 'u-2',
                                qso_date: '20260711',
                                time_on: '144600',
                                call: 'UR7MA',
                                band: '15m',
                                freq: '21.074',
                                mode: 'FT8',
                                country: 'Ukraine',
                            },
                        ],
                        next_cursor: null,
                    })
                );
            }
            return Promise.resolve(new Response('not found', { status: 404 }));
        })
    );
});

afterEach(() => {
    vi.unstubAllGlobals();
    logbookState.clearSelection();
    logbookState.rows = [];
    logbookState.logbooks = [];
    logbookState.selectedId = null;
    logbookState.error = null;
    // Reset filter state — it's a shared singleton, and a leaked notEmailedOnly
    // makes the next test's toggle flip the wrong way (isolation bug).
    logbookState.notEmailedOnly = false;
});

describe('Logbook page', () => {
    it('mounts, loads the first logbook, and renders its QSO page', async () => {
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        expect(screen.getByText('Malawi 2026 (7Q5MLV)')).toBeInTheDocument();
        expect(screen.getByText('EA3IE')).toBeInTheDocument();
        expect(screen.getByText('UR7MA')).toBeInTheDocument();
        expect(screen.getByText(/showing 1–2 of 2/)).toBeInTheDocument();
        // Tri-state tint against enabled=[qrz]: EA3IE uploaded → green;
        // UR7MA not → red. (Theme-aware classes from uploadColorClass.)
        expect(screen.getByText('EA3IE').className).toContain('text-green-700');
        expect(screen.getByText('UR7MA').className).toContain('text-red-700');
    });

    it('Edit opens the modal seeded from the row; ESC closes it', async () => {
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        screen.getByLabelText('Edit QSO with EA3IE').click();
        flushSync();

        const dialog = screen.getByRole('dialog', { name: 'Edit QSO' });
        expect(dialog).toBeInTheDocument();
        // Seeded from the row (ADIF → display forms).
        expect(screen.getByDisplayValue('EA3IE')).toBeInTheDocument();
        expect(screen.getByDisplayValue('2026-07-11')).toBeInTheDocument();
        expect(screen.getByDisplayValue('21.074')).toBeInTheDocument();

        await import('@testing-library/svelte').then(({ fireEvent }) =>
            fireEvent.keyDown(window, { key: 'Escape' })
        );
        flushSync();
        expect(screen.queryByRole('dialog', { name: 'Edit QSO' })).toBeNull();
    });

    it('Re-enrich fills the form from a fresh lookup for review (no write)', async () => {
        const fetchMock = vi.mocked(globalThis.fetch);
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        // Open the row that's missing its name (the flaky-link repair case).
        screen.getByLabelText('Edit QSO with UR7MA').click();
        flushSync();

        // Add the enrich route to the stub, then click Re-enrich.
        fetchMock.mockImplementation((input: RequestInfo | URL) => {
            const url = urlText(input);
            if (url.startsWith('/v1/enrich/callsign')) {
                expect(url).toContain('refresh=true');
                return Promise.resolve(
                    jsonResponse({
                        callsign: 'UR7MA',
                        country: { name: 'Ukraine', cq_zone: '16', itu_zone: '29' },
                        station: {
                            call: 'UR7MA',
                            name: 'Vladimir B. Gorlov',
                            country: 'Ukraine',
                            dxcc: '288',
                            gridsquare: 'KN59RB',
                        },
                        country_source: 'hamnut',
                        station_source: 'qrzlookupservice',
                    })
                );
            }
            return Promise.resolve(new Response('not found', { status: 404 }));
        });

        screen.getByRole('button', { name: 'Re-enrich' }).click();
        await flush();
        flushSync();

        expect(screen.getByDisplayValue('Vladimir B. Gorlov')).toBeInTheDocument();
        expect(screen.getByDisplayValue('KN59RB')).toBeInTheDocument(); // grid was empty → filled
        expect(screen.getByText(/review, then Save/)).toBeInTheDocument();
        // Fetch-and-review only: no PATCH fired.
        const patchCalls = fetchMock.mock.calls.filter((c) => c[1]?.method === 'PATCH');
        expect(patchCalls).toHaveLength(0);
    });

    it('select-all + upload toolbar appear once rows are selected', async () => {
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        const selectAll = screen.getByLabelText('Select all rows on this page');
        selectAll.click();
        flushSync();

        expect(screen.getByText('2 selected')).toBeInTheDocument();
        // No destination picked → Clear only, no Upload button.
        expect(screen.queryByText(/^Upload 2/)).toBeNull();
        expect(screen.getByRole('button', { name: 'Clear' })).toBeInTheDocument();
    });

    it('toggling "Not emailed only" reloads count + page with the server-side filter', async () => {
        const fetchMock = vi.mocked(globalThis.fetch);
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        fetchMock.mockClear();
        // Flip the filter on — it must REFETCH (the page-local version just hid
        // loaded rows), and both the page and the count carry not_emailed=true so
        // the filter (and the "of N") span the whole logbook, not one page.
        screen.getByLabelText('Not emailed only').click();
        await flush();
        await flush();
        flushSync();

        const urls = fetchMock.mock.calls.map((c) => urlText(c[0]));
        expect(urls.some((u) => u.includes('/qso') && u.includes('not_emailed=true'))).toBe(true);
        expect(urls.some((u) => u.includes('/count') && u.includes('not_emailed=true'))).toBe(true);
    });

    it('emailing a row while "Not emailed only" is on refreshes the filtered count + page', async () => {
        const fetchMock = vi.mocked(globalThis.fetch);
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        // Turn the filter on (reloads), then clear so we see only the post-email fetches.
        screen.getByLabelText('Not emailed only').click();
        await flush();
        await flush();
        flushSync();
        fetchMock.mockClear();

        // A successful email stamps a loaded row; with the server-side filter on,
        // the count + cursor trail must refresh, else the pager goes stale (P2).
        logbookState.markEmailed(['u-1']);
        await flush();
        await flush();
        flushSync();

        const urls = fetchMock.mock.calls.map((c) => urlText(c[0]));
        expect(urls.some((u) => u.includes('/count') && u.includes('not_emailed=true'))).toBe(true);
        expect(urls.some((u) => u.includes('/qso') && u.includes('not_emailed=true'))).toBe(true);
    });

    it('emailing a row with the filter OFF triggers no reload', async () => {
        const fetchMock = vi.mocked(globalThis.fetch);
        render(Logbook);
        await flush();
        await flush();
        flushSync();
        fetchMock.mockClear();

        logbookState.markEmailed(['u-1']); // notEmailedOnly defaults false
        await flush();
        flushSync();

        expect(fetchMock).not.toHaveBeenCalled();
    });

    it('shows a filter-specific empty message when nothing matches "Not emailed only"', async () => {
        const fetchMock = vi.mocked(globalThis.fetch);
        render(Logbook);
        await flush();
        await flush();
        flushSync();

        // Re-stub so the filtered page returns empty (every QSO already emailed)
        // while the logbook itself is non-empty — the P3 misfire condition.
        fetchMock.mockImplementation((input: RequestInfo | URL) => {
            const url = urlText(input);
            if (url.includes('/count')) return Promise.resolve(jsonResponse({ count: 5 }));
            if (url.includes('/qso'))
                return Promise.resolve(jsonResponse({ items: [], next_cursor: null }));
            return Promise.resolve(jsonResponse({ mailer: { enabled: false }, forwarders: [] }));
        });

        screen.getByLabelText('Not emailed only').click();
        await flush();
        await flush();
        flushSync();

        expect(screen.getByText('No QSOs still need emailing.')).toBeInTheDocument();
        expect(screen.queryByText('No QSOs in this logbook.')).toBeNull();
    });
});
