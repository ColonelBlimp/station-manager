import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { bindingsState, rowKey, _resetBindingsForTests } from './bindings.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    W-0021 5F.3 commit 4b3, ruling B1 (2026-10-09): while a row shows an
    adoption attempt in progress (checking or confirming), the adoption status
    is re-read every 5 s.

      A1  (folded into A10, the cadence by state.)
      A2  the re-read lays only the adoption statuses over the view: the
          attempt finishes, its status updates, and the operator's unsaved
          switch and typed value stay exactly as they were.
      A3  one re-read on the wire at a time.
      A4  a failed re-read keeps the status shown; it never reads as done.
      A5  an answer arriving after the tab closed, or after the archive
          changed, is ignored; a reopened tab reads again.
      A6  a full view (a load or a save) newer than the re-read wins over its
          late answer.
      A7  the re-read after a station-account save brings the statuses too:
          adopted, then the account replaced, reads "needs confirmation",
          with the operator's unsaved edits intact (operator review of 4b3).
      A8  an adoption re-read sent before that account re-read cannot
          overwrite it when it answers late.
      A9  nor can an account re-read sent before an adoption re-read that
          has already answered.
      A10 the re-read cadence by state (ruling B3): 5 s while an attempt is
          in progress, 60 s while the daemon retries on its own, none
          otherwise; the fastest row wins.
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

const CHECKING = {
    state: 'checking',
    message: 'Checking whether the legacy cloud logbook can be adopted.',
};
const ADOPTED = { state: 'adopted_restart_required', message: 'Adopted; applies after a restart.' };
const REFUSED = { state: 'unauthorized', message: 'Not adopted: the token was refused.' };
const NEEDS = {
    state: 'needs_confirmation',
    message: 'Adoption needs confirmation for the current station account.',
};

function view(adoption: unknown, over: Record<string, unknown> = {}) {
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
                logbooks: [
                    {
                        logbook_id: 1,
                        logbook_uuid: 'u1',
                        logbook_name: 'Default',
                        logbook_callsign: 'M0ABC',
                        bound: true,
                        enabled: true,
                        forwarder_name: 'smcloud.u1',
                        credentials_set: ['logbook'],
                        queue: { waiting: 0, failed: 0, in_flight: 0 },
                        locked_fields: ['logbook'],
                        adoption,
                    },
                    {
                        logbook_id: 2,
                        logbook_uuid: 'u2',
                        logbook_name: 'Portable',
                        logbook_callsign: 'M0ABC',
                        bound: false,
                        enabled: false,
                        forwarder_name: '',
                        credentials_set: [],
                        queue: { waiting: 0, failed: 0, in_flight: 0 },
                        ...over,
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
let failGets = false;
let holdGets = 0;
let held: ((r: Response) => void)[] = [];
let gets = 0;

beforeEach(() => {
    served = view(CHECKING);
    failGets = false;
    holdGets = 0;
    held = [];
    gets = 0;
    vi.stubGlobal(
        'fetch',
        vi.fn((url: string, init?: RequestInit) => {
            if (url === '/v1/version') {
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            }
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings' && init?.method !== 'PUT') {
                gets++;
                if (holdGets > 0) {
                    holdGets--;
                    return new Promise<Response>((resolve) => held.push(resolve));
                }
                if (failGets) return Promise.resolve(json({ code: 'x', message: 'down' }, 500));
                return Promise.resolve(json(served));
            }
            return Promise.resolve(new Response('{}', { status: 404 }));
        })
    );
});

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    _resetBindingsForTests();
    resetToasts();
});

const adoptionOf = () => bindingsState.view!.destinations[0].logbooks[0].adoption;

describe('bindingsState — the adoption status re-read (4b3, B1/B3)', () => {
    it('A2: the attempt finishes, its status updates, and the draft stays intact', async () => {
        await bindingsState.load();
        bindingsState.setRow('smcloud', 2, true);
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        const before = structuredClone($state.snapshot(bindingsState.drafts));
        expect(bindingsState.dirty).toBe(true);

        // The daemon's answer differs in more than the adoption: only the
        // adoption may be taken from it.
        served = view(ADOPTED, {
            bound: true,
            enabled: true,
            forwarder_name: 'smcloud.u2',
            queue: { waiting: 3, failed: 0, in_flight: 0 },
        });
        expect(await bindingsState.refreshAdoption()).toBe(true);

        expect(adoptionOf()).toEqual(ADOPTED);
        expect(bindingsState.adoptionPollMs).toBe(0);
        expect($state.snapshot(bindingsState.drafts)).toEqual(before);
        expect(bindingsState.drafts[rowKey('smcloud', 2)]).toMatchObject({
            enabled: true,
            credentials: { logbook: 'portable' },
        });
        expect(bindingsState.dirty).toBe(true);
        const portable = bindingsState.view!.destinations[0].logbooks[1];
        expect(portable).toMatchObject({ enabled: false, bound: false, forwarder_name: '' });
        expect(portable.queue.waiting).toBe(0);
    });

    it('A3: one re-read on the wire at a time', async () => {
        await bindingsState.load();
        const sent = gets;
        holdGets = 1;
        const first = bindingsState.refreshAdoption();
        expect(await bindingsState.refreshAdoption()).toBe(false);
        expect(gets).toBe(sent + 1);
        served = view(ADOPTED);
        held[0](json(served));
        expect(await first).toBe(true);
        expect(adoptionOf()).toEqual(ADOPTED);
        // The next one may go once the first has answered.
        expect(await bindingsState.refreshAdoption()).toBe(true);
        expect(gets).toBe(sent + 2);
    });

    it('A4: a failed re-read keeps the status shown', async () => {
        await bindingsState.load();
        failGets = true;
        expect(await bindingsState.refreshAdoption()).toBe(false);
        expect(adoptionOf()).toEqual(CHECKING);
        expect(bindingsState.adoptionPollMs).toBe(5_000);
    });

    it('A5: an answer after the tab closed, or after the archive changed, is ignored', async () => {
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshAdoption();
        bindingsState.closeAdoption();
        held[0](json(view(ADOPTED)));
        expect(await late).toBe(false);
        expect(adoptionOf()).toEqual(CHECKING);

        // Reopened: it reads again.
        served = view(CHECKING);
        await bindingsState.load();
        holdGets = 1;
        const other = bindingsState.refreshAdoption();
        bindingsState.archiveId = 'A2';
        held[1](json(view(ADOPTED)));
        expect(await other).toBe(false);
        expect(adoptionOf()).toEqual(CHECKING);
    });

    it('A6: a newer full view wins over a late re-read', async () => {
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshAdoption();
        served = view(REFUSED);
        await bindingsState.load();
        expect(adoptionOf()).toEqual(REFUSED);
        held[0](json(view(CHECKING)));
        await late;
        expect(adoptionOf()).toEqual(REFUSED);
    });

    it('A7: an account save brings the status; the unsaved edits stay', async () => {
        served = view(ADOPTED);
        await bindingsState.load();
        bindingsState.setRow('smcloud', 2, true);
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        const before = structuredClone($state.snapshot(bindingsState.drafts));

        served = view(NEEDS); // the station account was replaced and saved
        expect(await bindingsState.refreshEligibility()).toBe(true);

        expect(adoptionOf()).toEqual(NEEDS);
        expect($state.snapshot(bindingsState.drafts)).toEqual(before);
        expect(bindingsState.dirty).toBe(true);
    });

    it('A8: an adoption re-read sent before the account change cannot overwrite it', async () => {
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshAdoption();
        served = view(NEEDS);
        expect(await bindingsState.refreshEligibility()).toBe(true);
        expect(adoptionOf()).toEqual(NEEDS);
        held[0](json(view(ADOPTED)));
        expect(await late).toBe(false);
        expect(adoptionOf()).toEqual(NEEDS);
    });

    it('A9: an account re-read sent before a newer adoption re-read cannot roll it back', async () => {
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshEligibility();
        served = view(ADOPTED);
        expect(await bindingsState.refreshAdoption()).toBe(true);
        expect(adoptionOf()).toEqual(ADOPTED);
        held[0](json(view(CHECKING)));
        await late;
        expect(adoptionOf()).toEqual(ADOPTED);
    });

    it('A10: the cadence by state', async () => {
        const slow = [
            'unreachable',
            'uncertain',
            'record_failed',
            'local_unreadable',
            'needs_confirmation',
            'unsafe',
            'blocked',
        ];
        const none = [
            'unauthorized',
            'unreadable',
            'refused',
            'conflict',
            'unsupported',
            'adopted',
            'adopted_restart_required',
        ];
        const cases: [unknown, number][] = [
            [CHECKING, 5_000],
            [{ state: 'confirming', message: 'm' }, 5_000],
            ...slow.map((state): [unknown, number] => [{ state, message: 'm' }, 60_000]),
            ...none.map((state): [unknown, number] => [{ state, message: 'm' }, 0]),
            [null, 0],
        ];
        for (const [adoption, want] of cases) {
            served = view(adoption);
            await bindingsState.load();
            expect(bindingsState.adoptionPollMs, JSON.stringify(adoption)).toBe(want);
        }
    });
});

/*
    W-0021 5F.3 commit 5b, ruling C2 (2026-10-09): a binding the daemon started
    held carries uploads_held, apart from its adoption status.

      H1  the adoption re-read brings uploads_held too; the drafts stay.
      H2  so does the re-read after a station-account save; an adoption re-read
          sent before it cannot overwrite it when it answers late.
      H3  nor can an account re-read sent before a newer adoption re-read.
      H4  a newer full view wins over a late re-read.
      H5  the cadence: 60 s while waiting for confirmation, on any row; none
          for restart required or disabled by themselves; the fastest row or
          field wins.
*/

const WAITING = {
    state: 'waiting_for_confirmation',
    message: 'Uploads are held until adoption is confirmed for the current station account.',
};
const RESTART = { state: 'restart_required', message: 'Uploads resume after a restart.' };
const DISABLED = {
    state: 'disabled',
    message:
        'Uploads are held; this binding is off, so its queued uploads are discarded at the next restart.',
};

/** view(adoption) with uploads_held on the default row, or on Portable (bound). */
function heldView(adoption: unknown, held: unknown, onPortable = false) {
    const v = onPortable
        ? view(adoption, { bound: true, enabled: true, forwarder_name: 'smcloud.u2' })
        : view(adoption);
    (v.destinations[0].logbooks[onPortable ? 1 : 0] as Record<string, unknown>).uploads_held = held;
    return v;
}

const heldOf = (i = 0) => bindingsState.view!.destinations[0].logbooks[i].uploads_held;

describe('bindingsState — uploads_held (5b, C2)', () => {
    it('H1: the adoption re-read brings it; the drafts stay', async () => {
        served = heldView(NEEDS, WAITING);
        await bindingsState.load();
        bindingsState.setRow('smcloud', 2, true);
        bindingsState.setField('smcloud', 2, 'logbook', 'portable');
        const before = structuredClone($state.snapshot(bindingsState.drafts));
        expect(heldOf()).toEqual(WAITING);

        served = heldView(ADOPTED, RESTART);
        expect(await bindingsState.refreshAdoption()).toBe(true);
        expect(heldOf()).toEqual(RESTART);
        expect(adoptionOf()).toEqual(ADOPTED);
        expect($state.snapshot(bindingsState.drafts)).toEqual(before);
        expect(bindingsState.dirty).toBe(true);
    });

    it('H2: the account re-read brings it; an older adoption re-read cannot overwrite it', async () => {
        served = heldView(NEEDS, WAITING);
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshAdoption();
        served = heldView(ADOPTED, RESTART); // the account was saved back
        expect(await bindingsState.refreshEligibility()).toBe(true);
        expect(heldOf()).toEqual(RESTART);
        held[0](json(heldView(NEEDS, WAITING)));
        expect(await late).toBe(false);
        expect(heldOf()).toEqual(RESTART);
    });

    it('H3: an account re-read sent before a newer adoption re-read cannot roll it back', async () => {
        served = heldView(NEEDS, WAITING);
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshEligibility();
        served = heldView(ADOPTED, RESTART);
        expect(await bindingsState.refreshAdoption()).toBe(true);
        held[0](json(heldView(NEEDS, WAITING)));
        await late;
        expect(heldOf()).toEqual(RESTART);
    });

    it('H4: a newer full view wins over a late re-read', async () => {
        served = heldView(NEEDS, WAITING);
        await bindingsState.load();
        holdGets = 1;
        const late = bindingsState.refreshAdoption();
        served = heldView(ADOPTED, RESTART);
        await bindingsState.load();
        held[0](json(heldView(NEEDS, WAITING)));
        await late;
        expect(heldOf()).toEqual(RESTART);
    });

    it('H5: the cadence', async () => {
        const cases: [unknown, unknown, boolean, number][] = [
            [null, WAITING, false, 60_000],
            [{ state: 'adopted', message: 'Adopted.' }, WAITING, false, 60_000],
            [null, WAITING, true, 60_000],
            [ADOPTED, RESTART, false, 0],
            [ADOPTED, DISABLED, false, 0],
            [null, DISABLED, true, 0],
            [CHECKING, WAITING, false, 5_000],
            [NEEDS, RESTART, false, 60_000],
        ];
        for (const [adoption, held, onPortable, want] of cases) {
            served = heldView(adoption, held, onPortable);
            await bindingsState.load();
            expect(bindingsState.adoptionPollMs, JSON.stringify([adoption, held, onPortable])).toBe(
                want
            );
        }
    });
});
