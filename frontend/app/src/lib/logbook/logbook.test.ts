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
        logbookState.markEmailed(['u1'], null);
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

// vi.fn fetch stubs receive RequestInfo | URL; String(new Request(...)) would
// stringify to '[object Request]', so narrow explicitly (tests pass strings,
// but the signature must be honest for lint's no-base-to-string).
function urlText(input: RequestInfo | URL): string {
    return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
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
                const url = urlText(input);
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
                    const body = JSON.parse(init.body as string) as Record<string, unknown>;
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

// Request-generation guard: the selector stays enabled during a load, so a
// slow response for logbook A must not clobber a faster, NEWER selection of
// logbook B (rows/count/cursors would show A's data under B's selector).
describe('logbook switch — stale responses are discarded', () => {
    afterEach(() => {
        vi.unstubAllGlobals();
        logbookState.rows = [];
        logbookState.selectedId = null;
    });

    it('a late page/count for the previous logbook does not overwrite the new one', async () => {
        const pageBody = (calls: string[]) => ({
            items: calls.map((c, i) => ({ id: i + 1, uuid: `u-${c}`, call: c })),
            next_cursor: null,
        });
        let releaseA: (v: Response) => void = () => undefined;
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                const url = urlText(input);
                if (url.startsWith('/v1/logbook/1/qso')) {
                    // Logbook 1 is SLOW — held until after logbook 2 answers.
                    return new Promise<Response>((res) => (releaseA = res));
                }
                if (url.startsWith('/v1/logbook/1/count')) {
                    return Promise.resolve(
                        new Response(JSON.stringify({ count: 111 }), { status: 200 })
                    );
                }
                if (url.startsWith('/v1/logbook/2/qso')) {
                    return Promise.resolve(
                        new Response(JSON.stringify(pageBody(['K2AAA'])), { status: 200 })
                    );
                }
                if (url.startsWith('/v1/logbook/2/count')) {
                    return Promise.resolve(
                        new Response(JSON.stringify({ count: 1 }), { status: 200 })
                    );
                }
                return Promise.resolve(new Response('{}', { status: 404 }));
            })
        );

        const first = logbookState.selectLogbook(1); // hangs on its page fetch
        await logbookState.selectLogbook(2); // completes fully
        expect(logbookState.rows.map((r) => r.call)).toEqual(['K2AAA']);
        // Logbook 1's page finally lands — it must be dropped, not applied.
        releaseA(new Response(JSON.stringify(pageBody(['G1OLD', 'G1OLD2'])), { status: 200 }));
        await first;
        expect(logbookState.selectedId).toBe(2);
        expect(logbookState.rows.map((r) => r.call)).toEqual(['K2AAA']);
        expect(logbookState.count).toBe(1);
        expect(logbookState.loading).toBe(false);
    });
});

// markEmailed, with the "not emailed only" filter active, must resync paging by
// RESETTING to page 0 — refetching the current pageIndex keeps a stale start
// cursor that can strand the operator on an emptied last page (P2 review
// follow-up on the filtered-refresh fix).
describe('markEmailed — filtered paging reset', () => {
    afterEach(() => {
        vi.unstubAllGlobals();
        logbookState.clearSelection();
        logbookState.rows = [];
        logbookState.selectedId = null;
        logbookState.notEmailedOnly = false;
        logbookState.pageIndex = 0;
    });

    it('resets to page 0 and reloads the filtered snapshot when notEmailedOnly is on', async () => {
        const urls: string[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                urls.push(urlText(input));
                return Promise.resolve(
                    new Response(JSON.stringify({ items: [], next_cursor: null, count: 0 }), {
                        status: 200,
                    })
                );
            })
        );
        logbookState.selectedId = 1;
        logbookState.notEmailedOnly = true;
        logbookState.pageIndex = 2;
        logbookState.rows = [qso(1, 'u1')];

        logbookState.markEmailed(['u1'], 1); // origin === current logbook
        await new Promise((r) => setTimeout(r, 0));
        // Reloaded page 0 (pre-fix this refetched pageIndex 2 and could strand the
        // operator on an emptied page); #loadPage resets pageIndex on success.
        expect(logbookState.pageIndex).toBe(0);
        expect(urls.some((u) => u.includes('/qso') && u.includes('not_emailed=true'))).toBe(true);
    });

    it('does not reload when the send originated from a different logbook', async () => {
        const fetchFn = vi.fn(() => Promise.resolve(new Response('{}', { status: 200 })));
        vi.stubGlobal('fetch', fetchFn);
        logbookState.selectedId = 2; // operator has switched to logbook 2
        logbookState.notEmailedOnly = true;
        logbookState.pageIndex = 3;
        logbookState.rows = [qso(1, 'u1')];

        logbookState.markEmailed(['u1'], 1); // a completion for logbook 1
        await new Promise((r) => setTimeout(r, 0));

        expect(fetchFn).not.toHaveBeenCalled(); // logbook 2 is left untouched
        expect(logbookState.pageIndex).toBe(3);
    });

    it('preserves the current page + pageIndex when the refresh fails', async () => {
        // The reload errors (500). The stale-cursor risk is real, but blanking the
        // page with no way back is worse — keep the displayed snapshot.
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('boom', { status: 500 })))
        );
        logbookState.selectedId = 1;
        logbookState.notEmailedOnly = true;
        logbookState.pageIndex = 2;
        logbookState.rows = [qso(1, 'u1')];

        logbookState.markEmailed(['u1'], 1);
        await new Promise((r) => setTimeout(r, 0));

        expect(logbookState.rows).toHaveLength(1); // not blanked by a pre-clear
        expect(logbookState.pageIndex).toBe(2); // still on the current page
    });

    it('leaves paging untouched and does not reload when the filter is off', async () => {
        const fetchFn = vi.fn(() => Promise.resolve(new Response('{}', { status: 200 })));
        vi.stubGlobal('fetch', fetchFn);
        logbookState.selectedId = 1;
        logbookState.notEmailedOnly = false;
        logbookState.pageIndex = 2;
        logbookState.rows = [qso(1, 'u1')];

        logbookState.markEmailed(['u1'], 1);
        await new Promise((r) => setTimeout(r, 0));

        expect(logbookState.pageIndex).toBe(2);
        expect(fetchFn).not.toHaveBeenCalled();
    });
});

/*
    L5/L6 — THE SELECTED DESTINATION IS NAMED BY LABEL EVERYWHERE IT IS SHOWN,
    AND BY NAME EVERYWHERE IT IS SENT.

    The first pass at forwarder labels (commit 28851875) did the dropdown and
    the tooltip and stopped there, so picking "QRZ (club account)" produced
    "Upload 1 to qrz" and then "Queued 1 to qrz" — the same destination under two
    identities inside one workflow (clean-room review 288518755c52).

    The failure was an enumeration one: `selectedDestination` was treated as
    purely a key because that is its job in enqueueUploads, and the three places
    it is ALSO rendered went unlooked-for. The complete set is the upload button,
    the empty-filter message, and this notice.

    L6b is the guard that stops the fix over-applying. Every one of these strings
    is cosmetic; the argument to enqueueUploads is not, and swapping the label in
    there would address a destination the daemon has never heard of.
*/
describe('selected destination labelling', () => {
    afterEach(() => {
        logbookState.forwarders = [];
        logbookState.selectedDestination = '';
        logbookState.notice = null;
    });

    // L5 — the label getter resolves the selected destination through the
    // operator's label, and falls back to the raw name for an unlabelled one.
    it('L5: resolves the selected destination to its label', () => {
        logbookState.forwarders = [
            { name: 'qrz', label: 'QRZ (club account)', type: 'qrz', enabled: true },
            { name: 'clublog', label: '', type: 'clublog', enabled: true },
        ];

        logbookState.selectedDestination = 'qrz';
        expect(logbookState.selectedDestinationLabel).toBe('QRZ (club account)');

        logbookState.selectedDestination = 'clublog';
        expect(logbookState.selectedDestinationLabel).toBe('clublog');
    });

    // L5b — a destination that is no longer in the list (disabled between the
    // pick and the render) still names itself rather than going blank.
    it('L5b: falls back to the raw name for an unknown destination', () => {
        logbookState.forwarders = [];
        logbookState.selectedDestination = 'vanished';
        expect(logbookState.selectedDestinationLabel).toBe('vanished');
    });

    // L6c — THE LABEL IS CAPTURED BEFORE THE AWAIT, like `dest` already was.
    //
    // The destination <select> is NOT disabled during an upload (only the button
    // is), so the operator can change it while the request is in flight. Reading
    // the label after the await then reports "Queued 1 to <the NEW destination>"
    // for records that went to the old one — a notice that names somewhere the
    // QSOs were never sent (review 72a61e962f52).
    //
    // The pre-existing code already captured `dest` before awaiting for exactly
    // this reason; resolving the label afterwards broke that symmetry. The
    // fixture changes the selection WHILE the request is gated open, which is
    // the only way the two orderings differ.
    it('L6c: the notice names the destination the upload was SENT to', async () => {
        let release!: () => void;
        const gate = new Promise<void>((r) => (release = r));
        vi.stubGlobal(
            'fetch',
            vi.fn(async () => {
                await gate;
                return new Response(JSON.stringify({ enqueued: 1, skipped_uploaded: 0 }), {
                    status: 200,
                });
            })
        );
        logbookState.forwarders = [
            { name: 'qrz', label: 'QRZ (club account)', type: 'qrz', enabled: true },
            { name: 'clublog', label: 'ClubLog (contest)', type: 'clublog', enabled: true },
        ];
        logbookState.selectedDestination = 'qrz';
        logbookState.rows = [qso(1, 'u-1')];
        logbookState.toggleRow(logbookState.rows[0]);

        const inFlight = logbookState.uploadSelected();
        // Operator changes their mind mid-request. The selector is live.
        logbookState.selectedDestination = 'clublog';
        release();
        await inFlight;

        expect(logbookState.notice).toContain('QRZ (club account)');
        expect(logbookState.notice).not.toContain('ClubLog (contest)');
        vi.restoreAllMocks();
    });

    // L6 — the success notice uses the label...
    it('L6: the queued notice names the destination by label', async () => {
        const calls: string[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                calls.push(urlText(input));
                return Promise.resolve(
                    new Response(JSON.stringify({ enqueued: 1, skipped_uploaded: 0 }), {
                        status: 200,
                    })
                );
            })
        );
        logbookState.forwarders = [
            { name: 'qrz', label: 'QRZ (club account)', type: 'qrz', enabled: true },
        ];
        logbookState.selectedDestination = 'qrz';
        logbookState.rows = [qso(1, 'u-1')];
        logbookState.toggleRow(logbookState.rows[0]);

        await logbookState.uploadSelected();

        expect(logbookState.notice).toContain('QRZ (club account)');
        expect(logbookState.notice).not.toContain('to qrz');

        // L6b — ...while the REQUEST still addresses the durable name.
        expect(calls.some((u) => u.includes('/v1/forwarder/qrz/uploads'))).toBe(true);
        vi.restoreAllMocks();
    });

    // L7 — ClubLog / no-bulk-backfill retry-only workflow, ported from the retiring
    // logbook SPA (W-0003 AC2). ClubLog forbids catch-up batches on its realtime
    // endpoint, so the daemon enqueues only rows with prior queue history and reports
    // the rest as skipped_no_history; the UI flips the Upload button to a retry
    // flavour and the notice tells the operator to ADIF-export the skipped rows.
    it('L7: destinationRetryOnly is true only for a no-bulk-backfill type (ClubLog)', () => {
        logbookState.forwarders = [
            { name: 'qrz', label: 'QRZ (club account)', type: 'qrz', enabled: true },
            { name: 'clublog', label: 'ClubLog', type: 'clublog', enabled: true },
        ];
        logbookState.selectedDestination = 'qrz';
        expect(logbookState.destinationRetryOnly).toBe(false); // qrz accepts bulk backfill
        logbookState.selectedDestination = 'clublog';
        expect(logbookState.destinationRetryOnly).toBe(true); // ClubLog: retry-only
        logbookState.selectedDestination = '';
        expect(logbookState.destinationRetryOnly).toBe(false); // no destination selected
    });

    it('L7b: a ClubLog upload reports skipped_no_history rows as needing an ADIF export', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.resolve(
                    new Response(
                        JSON.stringify({
                            enqueued: 0,
                            skipped_uploaded: 0,
                            skipped_no_history: ['u-1', 'u-2'],
                        }),
                        { status: 200 }
                    )
                )
            )
        );
        logbookState.forwarders = [
            { name: 'clublog', label: 'ClubLog', type: 'clublog', enabled: true },
        ];
        logbookState.selectedDestination = 'clublog';
        logbookState.rows = [qso(1, 'u-1')];
        logbookState.toggleRow(logbookState.rows[0]);

        await logbookState.uploadSelected();

        expect(logbookState.notice).toContain('2 skipped'); // the count surfaces…
        expect(logbookState.notice).toMatch(/never uploaded live/i); // …with the reason…
        expect(logbookState.notice).toMatch(/ADIF export/i); // …and the remedy.
        vi.restoreAllMocks();
    });
});
