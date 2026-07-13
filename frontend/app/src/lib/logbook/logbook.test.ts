import { afterEach, describe, expect, it } from 'vitest';
import { logbookState } from './logbook.svelte';
import type { LogbookQso } from '../api/logbooks';

function qso(id: number, uuid?: string): LogbookQso {
    return { id, uuid, call: `C${id}` };
}

afterEach(() => {
    logbookState.clearSelection();
    logbookState.rows = [];
});

// The email-out payload keys off UUIDs, but selection is by numeric id and spans
// pages — so the UUID has to be captured at toggle time, not read back from the
// (page-only) `rows`. These tests pin that capture + the markEmailed mirror.
describe('logbook selection → email UUIDs', () => {
    it('toggleRow captures the UUID; selectedUuids lists the selected rows', () => {
        logbookState.toggleRow(qso(1, 'u1'));
        logbookState.toggleRow(qso(2, 'u2'));
        expect(logbookState.selectedCount).toBe(2);
        expect(logbookState.selectedUuids.sort()).toEqual(['u1', 'u2']);
    });

    it('toggling a row off drops both its id and its UUID', () => {
        const a = qso(1, 'u1');
        logbookState.toggleRow(a);
        logbookState.toggleRow(qso(2, 'u2'));
        logbookState.toggleRow(a); // off
        expect(logbookState.selectedCount).toBe(1);
        expect(logbookState.selectedUuids).toEqual(['u2']);
    });

    it('UUIDs survive paging away — selection is not derived from the visible page', () => {
        logbookState.rows = [qso(1, 'u1')];
        logbookState.toggleRow(logbookState.rows[0]);
        logbookState.rows = [qso(9, 'u9')]; // operator paged forward; row 1 no longer visible
        expect(logbookState.selectedUuids).toEqual(['u1']);
    });

    it('a selected row without a UUID is counted but excluded from the email payload', () => {
        logbookState.toggleRow(qso(1)); // no uuid (pre-UUID legacy import)
        logbookState.toggleRow(qso(2, 'u2'));
        expect(logbookState.selectedCount).toBe(2);
        expect(logbookState.selectedUuids).toEqual(['u2']);
    });

    it('toggleAllVisible selects then clears the visible rows and their UUIDs', () => {
        logbookState.rows = [qso(1, 'u1'), qso(2, 'u2'), qso(3, 'u3')];
        logbookState.toggleAllVisible();
        expect(logbookState.selectedUuids.sort()).toEqual(['u1', 'u2', 'u3']);
        logbookState.toggleAllVisible();
        expect(logbookState.selectedCount).toBe(0);
        expect(logbookState.selectedUuids).toEqual([]);
    });

    it('clearSelection drops ids and UUIDs together', () => {
        logbookState.toggleRow(qso(1, 'u1'));
        logbookState.clearSelection();
        expect(logbookState.selectedCount).toBe(0);
        expect(logbookState.selectedUuids).toEqual([]);
    });

    it('markEmailed flips only the matching visible rows to forwarded-by-email', () => {
        logbookState.rows = [qso(1, 'u1'), qso(2, 'u2')];
        logbookState.markEmailed(['u1']);
        expect(logbookState.rows[0].sm_fwrd_by_email_status).toBe('Y');
        expect(logbookState.rows[1].sm_fwrd_by_email_status).toBeUndefined();
    });
});

// Bulk Re-enrich: the backfill pattern as a toolbar action. The load-bearing
// policy is skip-if-unchanged — every PATCH re-arms that QSO's QRZ update
// upload, so an already-correct row must never fire a no-op re-upload. Grid
// fills only when the stored one is empty (on-air grid stays authoritative).
import { vi } from 'vitest';

function enrichBody(call: string, station: Record<string, string>) {
    return {
        callsign: call,
        station: { call, ...station },
        country_source: 'hamnut',
        station_source: 'qrzlookupservice',
    };
}

describe('bulk re-enrich (skip-if-unchanged)', () => {
    afterEach(() => {
        vi.unstubAllGlobals();
        logbookState.clearSelection();
        logbookState.rows = [];
        logbookState.notice = null;
    });

    it('patches only rows whose enrichment differs; unchanged and off-page are counted, not written', async () => {
        const patched: { url: string; body: Record<string, unknown> }[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
                const url = String(input);
                if (url.startsWith('/v1/enrich/callsign')) {
                    const call = new URL(url, 'http://x').searchParams.get('call') ?? '';
                    expect(url).toContain('refresh=true');
                    // Fresh data: name + dxcc for both calls; UR7MA's stored row
                    // already matches, EA1AAA's is missing its name.
                    const data =
                        call === 'UR7MA'
                            ? {
                                  name: 'Vladimir',
                                  country: 'Ukraine',
                                  dxcc: '288',
                                  gridsquare: 'KN59',
                              }
                            : { name: 'Ana', country: 'Spain', dxcc: '281', gridsquare: 'IN52' };
                    return Promise.resolve(
                        new Response(JSON.stringify(enrichBody(call, data)), { status: 200 })
                    );
                }
                if (url.startsWith('/v1/qso/') && init?.method === 'PATCH') {
                    const body = JSON.parse(String(init.body)) as Record<string, unknown>;
                    patched.push({ url, body });
                    return Promise.resolve(
                        new Response(
                            JSON.stringify({ id: 2, uuid: 'u-2', call: 'EA1AAA', ...body }),
                            {
                                status: 200,
                            }
                        )
                    );
                }
                return Promise.resolve(new Response('nf', { status: 404 }));
            })
        );

        logbookState.rows = [
            // Already correct — must be SKIPPED (no PATCH, no QRZ re-upload).
            {
                id: 1,
                uuid: 'u-1',
                call: 'UR7MA',
                name: 'Vladimir',
                country: 'Ukraine',
                dxcc: '288',
                gridsquare: 'KN59RB', // stored grid non-empty → never touched
            },
            // Missing name + dxcc — must be PATCHED with just the deltas.
            { id: 2, uuid: 'u-2', call: 'EA1AAA', country: 'Spain', gridsquare: '' },
        ];
        logbookState.toggleRow(logbookState.rows[0]);
        logbookState.toggleRow(logbookState.rows[1]);
        // A third selected id with no row on this page → "skipped" count.
        logbookState.selected.add(999);

        await logbookState.reEnrichSelected();

        expect(patched).toHaveLength(1);
        expect(patched[0].url).toBe('/v1/qso/u-2');
        expect(patched[0].body).toEqual({ name: 'Ana', dxcc: '281', gridsquare: 'IN52' });
        expect(logbookState.notice).toBe(
            'Re-enriched 1 · 1 unchanged · 1 selected on other pages skipped.'
        );
        // The patched row was replaced in place with the daemon's canonical merge.
        expect(logbookState.rows[1].name).toBe('Ana');
    });
});
