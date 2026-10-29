import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import DestinationsSection from './DestinationsSection.svelte';
import { bindingsState, rowKey, _resetBindingsForTests } from './bindings.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    W-0021 5F.4 commit 4 (ADR 0092; rulings J3, J5, 2026-10-12): SM Cloud rows
    born on identity, and the server support line.

      UI1 each refusal the daemon names shows on its row, with the switch
          disabled while the row is off.
      UI2 the SM Cloud card shows the daemon's support line only while the
          support is unverified.
      UI3 a hidden field is never shown and never sent:
          a. the view's hidden_fields drop the Cloud logbook name field from
             that row and keep it on a row that uses it;
          b. a typed value or a removal mark on a hidden field never reaches
             the PUT, and a row whose only edit was hidden is not sent;
          c. an eligibility re-read that hides a field drops the typed value
             and the removal mark on it;
          d. the re-read after a timed-out save does the same.
*/

const TYPES = {
    types: [
        {
            type: 'smcloud',
            display_name: 'SM Cloud backup',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'Service URL', kind: 'text', scope: 'station' },
                {
                    key: 'logbook',
                    label: 'Cloud logbook name',
                    kind: 'text',
                    clearable: true,
                    scope: 'logbook',
                },
            ],
        },
    ],
};

const UNVERIFIED =
    'SM Cloud server support is not verified: the server could not be reached at startup. Uploads wait and retry until a supporting server is available.';

const REASONS = [
    'previous SM Cloud upload history exists; automatic conversion needs manual recovery',
    'this logbook already holds QSOs and the archive cannot show that none reached SM Cloud; automatic conversion needs manual recovery',
    'this binding still uploads by its Cloud logbook name until the daemon restarts; restart, then turn it on',
    'SM Cloud server support is still being checked; try again shortly',
    'the SM Cloud server does not support per-logbook backups; upgrade it',
    'SM Cloud server support could not be checked; the daemon log has the cause',
    'the SM Cloud server changed since the daemon started; restart to check it',
    'this binding belongs to an SM Cloud adoption; re-enabling it is supported only in the Home archive',
    'this binding carries conflicting identity and adoption markers; manual recovery is required',
];

type RowOver = Record<string, unknown>;

/** Logbook 1 uses its Cloud logbook name (Home's default); logbook 2 is
 *  shaped by `second`. */
function view(second: RowOver = {}, support: unknown = { state: 'supported', message: '' }) {
    return {
        archive_id: 'A1',
        archive_label: 'Home',
        restart_required: false,
        destinations: [
            {
                type: 'smcloud',
                display_name: 'SM Cloud backup',
                account: { configured: true },
                state: 'mixed',
                reason: '',
                new_logbook_reason: '',
                support,
                logbooks: [
                    {
                        logbook_id: 1,
                        logbook_uuid: 'u1',
                        logbook_name: 'Default',
                        logbook_callsign: 'M0ABC',
                        bound: true,
                        enabled: true,
                        forwarder_name: 'smcloud',
                        credentials_set: ['logbook'],
                        queue: { waiting: 0, failed: 0, in_flight: 0 },
                    },
                    {
                        logbook_id: 2,
                        logbook_uuid: 'u2',
                        logbook_name: 'Portable',
                        logbook_callsign: 'M0ABC',
                        bound: true,
                        enabled: false,
                        forwarder_name: 'smcloud.u2',
                        credentials_set: ['logbook'],
                        queue: { waiting: 0, failed: 0, in_flight: 0 },
                        ...second,
                    },
                ],
            },
        ],
    };
}

function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
}

let served: unknown;
let putReply: unknown;
let puts: unknown[];
let gets = 0;
let holdNext = false;
let held: ((r: Response) => void) | null = null;

beforeEach(() => {
    served = view();
    putReply = view();
    puts = [];
    gets = 0;
    holdNext = false;
    held = null;
    vi.stubGlobal(
        'fetch',
        vi.fn((url: string, init?: RequestInit) => {
            if (url === '/v1/version') {
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            }
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings') {
                if (init?.method === 'PUT') {
                    puts.push(JSON.parse(init.body as string));
                    return putReply === 'timeout'
                        ? Promise.reject(Object.assign(new Error('t'), { name: 'TimeoutError' }))
                        : Promise.resolve(json(putReply));
                }
                gets++;
                if (holdNext) {
                    holdNext = false;
                    return new Promise<Response>((resolve) => (held = resolve));
                }
                return Promise.resolve(json(served));
            }
            return Promise.resolve(new Response('{}', { status: 404 }));
        })
    );
});

afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    _resetBindingsForTests();
    resetToasts();
});

const HIDDEN = { hidden_fields: ['logbook'] };

/** Every logbook key any PUT carried, typed or cleared. */
function sentKeys(): string[] {
    const keys: string[] = [];
    for (const body of puts as { destinations: { logbooks: Record<string, unknown>[] }[] }[]) {
        for (const d of body.destinations) {
            for (const l of d.logbooks) {
                keys.push(...Object.keys((l.credentials as Record<string, string>) ?? {}));
                keys.push(...((l.credentials_clear as string[]) ?? []));
            }
        }
    }
    return keys;
}

describe('bindingsState — SM Cloud rows born on identity (5F.4)', () => {
    it('UI3a: the view carries hidden_fields and the support state', async () => {
        served = view(HIDDEN, { state: 'unverified', message: UNVERIFIED });
        await bindingsState.load();
        const dest = bindingsState.view!.destinations[0];
        expect(dest.logbooks[1].hidden_fields).toEqual(['logbook']);
        expect(dest.logbooks[0].hidden_fields).toEqual([]);
        expect(dest.support).toEqual({ state: 'unverified', message: UNVERIFIED });
    });

    it('UI3b: a hidden field’s typed value and removal mark are never sent', async () => {
        served = view(HIDDEN);
        await bindingsState.load();
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        expect(bindingsState.buildRequest().destinations).toEqual([]);
        bindingsState.clear('smcloud', 2, 'logbook');
        expect(bindingsState.buildRequest().destinations).toEqual([]);
        bindingsState.uncleared('smcloud', 2, 'logbook');
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        bindingsState.setRow('smcloud', 2, true);
        const req = bindingsState.buildRequest();
        expect(req.destinations[0].logbooks).toEqual([{ logbook_id: 2, enabled: true }]);
        await bindingsState.save();
        expect(sentKeys()).not.toContain('logbook');
    });

    it('UI3c: a re-read that hides the field drops a stale typed value', async () => {
        served = view();
        await bindingsState.load();
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        served = view(HIDDEN);
        expect(await bindingsState.refreshEligibility()).toBe(true);
        expect(bindingsState.view!.destinations[0].logbooks[1].hidden_fields).toEqual(['logbook']);
        expect(bindingsState.drafts[rowKey('smcloud', 2)].credentials.logbook ?? '').toBe('');
        bindingsState.setRow('smcloud', 2, true);
        await bindingsState.save();
        expect(sentKeys()).not.toContain('logbook');
    });

    it('UI3c: a re-read that hides the field drops a stale removal mark', async () => {
        served = view();
        await bindingsState.load();
        bindingsState.clear('smcloud', 2, 'logbook');
        expect(bindingsState.drafts[rowKey('smcloud', 2)].cleared).toEqual(['logbook']);
        served = view(HIDDEN);
        expect(await bindingsState.refreshEligibility()).toBe(true);
        expect(bindingsState.drafts[rowKey('smcloud', 2)].cleared).toEqual([]);
        expect(bindingsState.buildRequest().destinations).toEqual([]);
    });

    it('UI3d: the re-read after a timed-out save drops them too', async () => {
        served = view();
        await bindingsState.load();
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        bindingsState.setField('smcloud', 1, 'logbook', 'shack');
        putReply = 'timeout';
        served = view(HIDDEN);
        await bindingsState.save();
        expect(bindingsState.drafts[rowKey('smcloud', 2)].credentials.logbook ?? '').toBe('');
        // A row that still uses the field keeps the operator's value.
        expect(bindingsState.drafts[rowKey('smcloud', 1)].credentials.logbook).toBe('shack');
    });

    it('UI3d: the re-read after a timed-out save drops a stale removal mark', async () => {
        served = view();
        await bindingsState.load();
        bindingsState.clear('smcloud', 2, 'logbook');
        putReply = 'timeout';
        served = view(HIDDEN);
        await bindingsState.save();
        expect(bindingsState.drafts[rowKey('smcloud', 2)].cleared).toEqual([]);
        // The next save carries nothing for the hidden field.
        expect(bindingsState.buildRequest().destinations).toEqual([]);
        putReply = view(HIDDEN);
        puts = [];
        bindingsState.setRow('smcloud', 2, true);
        await bindingsState.save();
        expect(sentKeys()).not.toContain('logbook');
    });
});

describe('DestinationsSection — SM Cloud rows born on identity (5F.4)', () => {
    it('UI3a: the hidden field is not rendered; the field a row uses is', async () => {
        served = view(HIDDEN);
        render(DestinationsSection);
        await screen.findByText('Portable');
        const rows = screen.getAllByTestId('binding-row');
        expect(rows[0].textContent).toContain('Cloud logbook name');
        expect(rows[1].textContent).not.toContain('Cloud logbook name');
    });

    it('UI1: every refusal the daemon names shows on its row, the switch disabled', async () => {
        for (const reason of REASONS) {
            served = view({ reason });
            const { unmount } = render(DestinationsSection);
            const el = await screen.findByTestId('row-reason');
            expect(el.textContent?.replace(/\s+/g, ' ').trim()).toBe(
                `Can't be turned on for this logbook: ${reason}.`
            );
            const box = screen.getByLabelText<HTMLInputElement>('SM Cloud backup for Portable');
            expect(box.disabled).toBe(true);
            await fireEvent.click(box);
            expect(bindingsState.drafts[rowKey('smcloud', 2)].enabled).toBe(false);
            unmount();
            _resetBindingsForTests();
        }
    });

    it('UI2: the support line shows only while unverified', async () => {
        served = view({}, { state: 'unverified', message: UNVERIFIED });
        const first = render(DestinationsSection);
        const line = await screen.findByTestId('destination-support');
        expect(line.textContent?.trim()).toBe(UNVERIFIED);
        first.unmount();
        _resetBindingsForTests();
        for (const state of ['supported', 'checking', 'unsupported', 'error', 'changed']) {
            served = view({}, { state, message: '' });
            const { unmount } = render(DestinationsSection);
            await screen.findByText('Portable');
            expect(screen.queryByTestId('destination-support')).toBeNull();
            unmount();
            _resetBindingsForTests();
        }
    });
});

/*
    W-0021 5F.4 commit 4, operator review: support still being checked when the
    tab opens. While the SM Cloud card reads `checking`, its eligibility is
    re-read every 5 s (the in-progress cadence of ruling B1) until it settles,
    keeping the drafts and the newest read.

      SP1 checking → supported: the row's switch enables, a draft elsewhere
          stays, and the re-reads stop.
      SP2 checking → unverified: the line appears; the re-reads stop.
      SP3 checking → error: the row names the error; the re-reads stop.
      SP4 closing the tab stops the re-reads.
      SP5 a support re-read answering after a newer full view cannot roll it
          back; only one re-read is on the wire at a time.
*/

const CHECKING_REASON = 'SM Cloud server support is still being checked; try again shortly';
const ERROR_REASON = 'SM Cloud server support could not be checked; the daemon log has the cause';
const checkingView = () =>
    view({ ...HIDDEN, reason: CHECKING_REASON }, { state: 'checking', message: '' });

async function rereadAfter(ms: number): Promise<number> {
    const before = gets;
    await vi.advanceTimersByTimeAsync(ms);
    return gets - before;
}

describe('DestinationsSection — support still being checked (5F.4 review)', () => {
    beforeEach(() => {
        // Only the interval is faked: testing-library's own waits use timeouts.
        vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    });

    it('SP1: checking → supported enables the row, keeps the drafts, stops', async () => {
        served = checkingView();
        render(DestinationsSection);
        await screen.findByTestId('row-reason');
        const box = screen.getByLabelText<HTMLInputElement>('SM Cloud backup for Portable');
        expect(box.disabled).toBe(true);
        bindingsState.setRow('smcloud', 1, false);
        served = view(HIDDEN);
        expect(await rereadAfter(5000)).toBe(1);
        await vi.waitFor(() => expect(screen.queryByTestId('row-reason')).toBeNull());
        expect(
            screen.getByLabelText<HTMLInputElement>('SM Cloud backup for Portable').disabled
        ).toBe(false);
        expect(bindingsState.drafts[rowKey('smcloud', 1)].enabled).toBe(false);
        expect(await rereadAfter(20000)).toBe(0);
    });

    it('SP2: checking → unverified shows the line and stops', async () => {
        served = checkingView();
        render(DestinationsSection);
        await screen.findByTestId('row-reason');
        served = view(HIDDEN, { state: 'unverified', message: UNVERIFIED });
        expect(await rereadAfter(5000)).toBe(1);
        const line = await screen.findByTestId('destination-support');
        expect(line.textContent?.trim()).toBe(UNVERIFIED);
        expect(await rereadAfter(20000)).toBe(0);
    });

    it('SP3: checking → error names the error and stops', async () => {
        served = checkingView();
        render(DestinationsSection);
        await screen.findByTestId('row-reason');
        served = view({ ...HIDDEN, reason: ERROR_REASON }, { state: 'error', message: '' });
        expect(await rereadAfter(5000)).toBe(1);
        await vi.waitFor(() =>
            expect(screen.getByTestId('row-reason').textContent).toContain(ERROR_REASON)
        );
        expect(await rereadAfter(20000)).toBe(0);
    });

    it('SP4: closing the tab stops the re-reads', async () => {
        served = checkingView();
        const { unmount } = render(DestinationsSection);
        await screen.findByTestId('row-reason');
        expect(await rereadAfter(5000)).toBe(1);
        unmount();
        expect(await rereadAfter(20000)).toBe(0);
    });
});

describe('bindingsState — support re-read ordering (5F.4 review)', () => {
    it('SP5: a late support answer cannot roll back a newer full view; one on the wire', async () => {
        served = checkingView();
        await bindingsState.load();
        holdNext = true;
        const late = bindingsState.refreshSupport();
        expect(await bindingsState.refreshSupport()).toBe(false);
        served = view(HIDDEN);
        await bindingsState.load();
        expect(bindingsState.view!.destinations[0].support?.state).toBe('supported');
        held!(json(checkingView()));
        await late;
        expect(bindingsState.view!.destinations[0].support?.state).toBe('supported');
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe('');
        expect(bindingsState.supportPollMs).toBe(0);
    });
});
