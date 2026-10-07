import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/svelte';
import DestinationsSection from './DestinationsSection.svelte';
import { bindingsState, rowKey, _resetBindingsForTests } from './bindings.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    W-0021 5F.0 (ruled 2026-10-07): in Home, a NEW SM Cloud enable is allowed
    only on the default logbook until per-binding identity lands. The daemon
    names the refusal per row (`reason` on a row that is not enabled) while the
    destination itself stays enable-able; an existing enabled row carries no
    reason and stays editable.

      F1  a refused row's switch cannot be turned on; the default row's can.
      F2  the every-logbook switch turns on only the rows that may be turned on.
      F3  an existing enabled row may be turned off and back on in the draft
          (it is not a new enable until a disable is saved).
      F4  the rendered row says why, with its switch disabled.
      F5  refreshing eligibility after a station-account save brings the row
          refusals too (codex P2 on 4daf2a2e): while the account was
          incomplete the daemon named only the destination's reason.
      F6  an eligibility read that started before a binding save cannot undo
          that save's row refusal when its answer arrives late (codex P2 on
          956a01db).
      F7  discarding edits restores the cached view; it is not a newer view,
          so an eligibility read in flight still applies (codex P2 on 205f57ec).
      F8  an eligibility read SENT before a load's own read cannot patch the
          loaded view when it answers late (codex P2 on 264335f7).
*/

const REASON =
    'SM Cloud can be turned on in Home only for the default logbook until per-logbook SM Cloud identity lands; another logbook would upload into the same cloud logbook';

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

const row = (id: number, name: string, over: Record<string, unknown> = {}) => ({
    logbook_id: id,
    logbook_uuid: `u${id}`,
    logbook_name: name,
    logbook_callsign: 'M0ABC',
    bound: false,
    enabled: false,
    forwarder_name: '',
    credentials_set: [],
    queue: { waiting: 0, failed: 0, in_flight: 0 },
    ...over,
});

const VIEW = {
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
            new_logbook_reason: REASON,
            logbooks: [
                row(1, 'Default'),
                row(2, 'Portable', { bound: true, enabled: true, forwarder_name: 'smcloud.u2' }),
                row(3, 'Contest', { reason: REASON }),
            ],
        },
    ],
};

function json(body: unknown): Response {
    return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
    });
}

let served: unknown = VIEW;
let held: ((r: Response) => void) | null = null;
let holdNextGet = false;
let putReply: unknown = VIEW;
let identityHeld: ((r: Response) => void) | null = null;
let holdIdentity = false;

beforeEach(() => {
    served = VIEW;
    held = null;
    holdNextGet = false;
    putReply = VIEW;
    identityHeld = null;
    holdIdentity = false;
    vi.stubGlobal(
        'fetch',
        vi.fn((url: string, init?: RequestInit) => {
            if (url === '/v1/version') {
                if (holdIdentity) {
                    holdIdentity = false;
                    return new Promise<Response>((resolve) => (identityHeld = resolve));
                }
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            }
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings') {
                if (init?.method === 'PUT') {
                    return putReply === 'timeout'
                        ? Promise.reject(Object.assign(new Error('t'), { name: 'TimeoutError' }))
                        : Promise.resolve(json(putReply));
                }
                if (holdNextGet) {
                    holdNextGet = false;
                    return new Promise<Response>((resolve) => (held = resolve));
                }
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

describe('bindingsState — Home SM Cloud, default logbook only (5F.0)', () => {
    it('F1: a refused row cannot be turned on; the default row can', async () => {
        await bindingsState.load();
        expect(bindingsState.view!.destinations[0].logbooks[2].reason).toBe(REASON);
        expect(bindingsState.view!.destinations[0].new_logbook_reason).toBe(REASON);
        bindingsState.setRow('smcloud', 3, true);
        expect(bindingsState.drafts[rowKey('smcloud', 3)].enabled).toBe(false);
        bindingsState.setRow('smcloud', 1, true);
        expect(bindingsState.drafts[rowKey('smcloud', 1)].enabled).toBe(true);
    });

    it('F2: the every-logbook switch turns on only the rows that may be turned on', async () => {
        await bindingsState.load();
        bindingsState.setAll('smcloud', true);
        expect(bindingsState.drafts[rowKey('smcloud', 1)].enabled).toBe(true);
        expect(bindingsState.drafts[rowKey('smcloud', 2)].enabled).toBe(true);
        expect(bindingsState.drafts[rowKey('smcloud', 3)].enabled).toBe(false);
        expect(bindingsState.draftState(bindingsState.view!.destinations[0])).toBe('mixed');
    });

    it('F3: an existing enabled row may be turned off and back on before a save', async () => {
        await bindingsState.load();
        bindingsState.setRow('smcloud', 2, false);
        expect(bindingsState.drafts[rowKey('smcloud', 2)].enabled).toBe(false);
        bindingsState.setRow('smcloud', 2, true);
        expect(bindingsState.drafts[rowKey('smcloud', 2)].enabled).toBe(true);
        expect(bindingsState.dirty).toBe(false);
    });

    it('F5: an eligibility refresh brings the row refusals as well as the account', async () => {
        const incomplete = 'its station account is incomplete: a required station field is not set';
        const [dest] = VIEW.destinations;
        served = {
            ...VIEW,
            destinations: [
                {
                    ...dest,
                    account: { configured: false },
                    reason: incomplete,
                    new_logbook_reason: incomplete,
                    logbooks: dest.logbooks.map((r) => ({ ...r, reason: '' })),
                },
            ],
        };
        await bindingsState.load();
        served = VIEW; // the account was completed and saved
        expect(await bindingsState.refreshEligibility()).toBe(true);
        const after = bindingsState.view!.destinations[0];
        expect(after.reason).toBe('');
        expect(after.new_logbook_reason).toBe(REASON);
        expect(after.logbooks[2].reason).toBe(REASON);
        bindingsState.setAll('smcloud', true);
        expect(bindingsState.drafts[rowKey('smcloud', 1)].enabled).toBe(true);
        expect(bindingsState.drafts[rowKey('smcloud', 3)].enabled).toBe(false);
    });

    it('F6: a late eligibility answer cannot undo a newer save’s row refusal', async () => {
        await bindingsState.load();
        holdNextGet = true;
        const late = bindingsState.refreshEligibility(); // reads Portable enabled, no reason
        bindingsState.setRow('smcloud', 2, false);
        const [dest] = VIEW.destinations;
        putReply = {
            ...VIEW,
            destinations: [
                {
                    ...dest,
                    logbooks: dest.logbooks.map((r) =>
                        r.logbook_id === 2 ? { ...r, enabled: false, reason: REASON } : r
                    ),
                },
            ],
        };
        await bindingsState.save();
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe(REASON);
        held!(json(VIEW)); // the read from before the save
        await late;
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe(REASON);
        bindingsState.setRow('smcloud', 2, true);
        expect(bindingsState.drafts[rowKey('smcloud', 2)].enabled).toBe(false);
    });

    it('F6b: the same holds when the save timed out and was re-read', async () => {
        await bindingsState.load();
        holdNextGet = true;
        const late = bindingsState.refreshEligibility();
        bindingsState.setRow('smcloud', 2, false);
        const [dest] = VIEW.destinations;
        served = {
            ...VIEW,
            destinations: [
                {
                    ...dest,
                    logbooks: dest.logbooks.map((r) =>
                        r.logbook_id === 2 ? { ...r, enabled: false, reason: REASON } : r
                    ),
                },
            ],
        };
        putReply = 'timeout';
        await bindingsState.save();
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe(REASON);
        held!(json(VIEW));
        await late;
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe(REASON);
    });

    it('F7: discarding edits does not cancel an eligibility read in flight', async () => {
        const incomplete = 'its station account is incomplete: a required station field is not set';
        const [dest] = VIEW.destinations;
        served = {
            ...VIEW,
            destinations: [
                {
                    ...dest,
                    account: { configured: false },
                    reason: incomplete,
                    new_logbook_reason: incomplete,
                    logbooks: dest.logbooks.map((r) => ({ ...r, reason: '' })),
                },
            ],
        };
        await bindingsState.load();
        bindingsState.setField('smcloud', 1, 'logbook', 'shack');
        expect(bindingsState.dirty).toBe(true);
        holdNextGet = true;
        const pending = bindingsState.refreshEligibility(); // the account was completed and saved
        bindingsState.reset();
        held!(json(VIEW));
        expect(await pending).toBe(true);
        expect(bindingsState.view!.destinations[0].reason).toBe('');
        expect(bindingsState.view!.destinations[0].logbooks[2].reason).toBe(REASON);
    });

    it('F8: a read sent before a load’s own read cannot patch the loaded view', async () => {
        await bindingsState.load(); // Portable enabled, no refusal
        holdIdentity = true;
        const loading = bindingsState.load(); // waits for the daemon's identity
        holdNextGet = true;
        const early = bindingsState.refreshEligibility(); // sent now: the older state
        const [dest] = VIEW.destinations;
        served = {
            ...VIEW,
            destinations: [
                {
                    ...dest,
                    logbooks: dest.logbooks.map((r) =>
                        r.logbook_id === 2 ? { ...r, enabled: false, reason: REASON } : r
                    ),
                },
            ],
        };
        identityHeld!(json({ instance: 'i', archive: { id: 'A1' } }));
        await loading;
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe(REASON);
        held!(json(VIEW));
        await early;
        expect(bindingsState.view!.destinations[0].logbooks[1].reason).toBe(REASON);
    });

    it('F4: the refused row says why, with its switch disabled', async () => {
        render(DestinationsSection);
        await screen.findByText('SM Cloud backup');
        const box = (name: string) =>
            screen.getByRole<HTMLInputElement>('checkbox', { name: `SM Cloud backup for ${name}` });
        expect(box('Contest').disabled).toBe(true);
        expect(box('Default').disabled).toBe(false);
        expect(box('Portable').disabled).toBe(false);
        const rows = screen.getAllByTestId('binding-row');
        const contest = rows.find((r) => r.textContent?.includes('Contest'))!;
        expect(within(contest).getByTestId('row-reason').textContent).toContain(
            'only for the default logbook'
        );
        const portable = rows.find((r) => r.textContent?.includes('Portable'))!;
        expect(within(portable).queryByTestId('row-reason')).toBeNull();
    });
});
