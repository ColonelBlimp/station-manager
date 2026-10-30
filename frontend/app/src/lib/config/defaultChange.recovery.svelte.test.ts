import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../api/default-change', () => ({
    fetchDefaultChangeStatus: vi.fn(),
    requestDefaultChange: vi.fn(),
    defaultChangeAction: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));
vi.mock('../api/archive-bindings', () => ({
    fetchArchiveBindings: vi.fn(),
    saveArchiveBindings: vi.fn(),
}));

import {
    defaultChangeAction,
    fetchDefaultChangeStatus,
    requestDefaultChange,
    type DefaultChangeActionOutcome,
    type DefaultChangeStatusOutcome,
} from '../api/default-change';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { fetchArchiveBindings } from '../api/archive-bindings';
import { setBoundDefault } from '../api/archive-guard';
import { TEST_ARCHIVE } from '../../test/archive';
import {
    _resetArchivesForTests,
    _setBootIdentityForTests,
    _setReloadForTests,
    archivesState,
} from './archives.svelte';
import { logbooksState, _resetLogbooksForTests } from './logbooks.svelte';
import { submitState, clearDraft } from '../operate/qso.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import {
    _resetDefaultChangeForTests,
    bootDefaultChange,
    changeNotice,
    defaultChangeGate,
    defaultChangeState,
    loadDefaultChange,
    pendingTargetBlock,
    recheckDefaultChange,
    runChangeAction,
    setAsDefault,
} from './defaultChange.svelte';

/*
    W-0024 slice 5b — the change's state and its recovery actions (ADR 0097
    §1–§4; operator rulings 2026-10-30). The tab's established context asserts
    Home (A); the change in play names Contest (C).
*/

const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const C = '01a187bc-0300-7000-8000-000000000003';
const P = '01a187bc-0200-7000-8000-000000000002';
const REQ = '01a187bc-0400-7000-8000-000000000004';
const REQ2 = '01a187bc-0500-7000-8000-000000000005';

const HOME = { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV', description: '', count: 12 };
const CONTEST = { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV', description: '', count: 0 };
const EFFECTIVE_A = { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' };

function status(
    change: Record<string, unknown> | null,
    effective = EFFECTIVE_A
): Extract<DefaultChangeStatusOutcome, { kind: 'ok' }> {
    return {
        kind: 'ok',
        status: {
            effective,
            change:
                change === null
                    ? null
                    : {
                          requestId: REQ,
                          logbookUuid: C,
                          state: 'pending_unclear',
                          removed: '',
                          failureCode: '',
                          unconfirmed: true,
                          ...change,
                      },
        },
    };
}
const idle = status(null);
const asAction = (s: { status: unknown }): DefaultChangeActionOutcome =>
    ({ kind: 'ok', status: s.status }) as DefaultChangeActionOutcome;

const GATED = /default logbook/i;
let reloads = 0;
const toast = (level: string, re: RegExp): boolean =>
    toastsState.items.some((t) => t.level === level && re.test(t.message));

beforeEach(() => {
    vi.mocked(fetchDefaultChangeStatus).mockReset().mockResolvedValue(idle);
    vi.mocked(requestDefaultChange)
        .mockReset()
        .mockResolvedValue({ kind: 'accepted', requestId: REQ, durability: 'durable' });
    vi.mocked(defaultChangeAction).mockReset().mockResolvedValue(asAction(idle));
    vi.mocked(fetchDaemonInstance).mockReset().mockResolvedValue('i1');
    vi.mocked(waitForDaemonBack).mockReset().mockResolvedValue(false);
    vi.mocked(fetchArchiveBindings)
        .mockReset()
        .mockResolvedValue({ kind: 'error', message: 'n/a' });
    _resetArchivesForTests();
    _setBootIdentityForTests({ instance: 'i1', archiveId: TEST_ARCHIVE, defaultLogbookUuid: A });
    reloads = 0;
    _setReloadForTests(() => reloads++);
    _resetLogbooksForTests();
    logbooksState.rows = [HOME, CONTEST];
    logbooksState.defaultId = 1;
    logbooksState.stationCallsign = '7Q5MLV';
    logbooksState.loaded = true;
    vi.spyOn(logbooksState, 'load').mockResolvedValue();
    setBoundDefault(A);
    clearDraft();
    submitState.busy = false;
    submitState.uncertain = false;
    resetToasts();
    _resetDefaultChangeForTests();
});

afterEach(() => {
    vi.restoreAllMocks();
});

async function shown(change: Record<string, unknown> | null): Promise<void> {
    vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(status(change));
    await loadDefaultChange();
}

const EFFECTIVE_C = { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV' };

/** A tab built after the restart that applied Contest: Contest is effective
 *  and the established context's default. */
async function shownApplied(change: Record<string, unknown> | null): Promise<void> {
    setBoundDefault(C);
    logbooksState.defaultId = 3;
    vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(status(change, EFFECTIVE_C));
    await loadDefaultChange();
}

describe('what the change in play shows (ADR 0097 state table)', () => {
    it('pending_restart: restarting, live entry paused, no action', async () => {
        await shown({ state: 'pending_restart' });
        const n = changeNotice();
        expect(n?.tone).toBe('sealed');
        expect(n?.text).toMatch(/Contest/);
        expect(n?.text).toMatch(/restart/i);
        expect(n?.action).toBeNull();
    });

    it('pending_unclear: applies at the next restart — Cancel change, naming the target', async () => {
        await shown({ state: 'pending_unclear' });
        const n = changeNotice();
        expect(n?.text).toMatch(/applies at the next restart/);
        expect(n?.action?.kind).toBe('cancel');
        expect(n?.action?.label).toMatch(/^Cancel change/);
        expect(n?.action?.label).toMatch(/Contest/);
        expect(n?.requestId).toBe(REQ);
    });

    it('the start’s not_applied_intent_pending: not applied, still pending, with its reason — Cancel change', async () => {
        await shown({
            state: 'not_applied_intent_pending',
            failureCode: 'default_change_target_ineligible',
        });
        const n = changeNotice();
        expect(n?.text).toMatch(/not applied/);
        expect(n?.text).toMatch(/still pending/);
        expect(n?.text).toMatch(/its callsign no longer matches the current default’s callsign/);
        expect(n?.action?.kind).toBe('cancel');
        expect(n?.action?.label).toMatch(/^Cancel change.*Contest/);
    });

    it('target no longer exists: Contest gone from the list — the notice and Cancel stay usable', async () => {
        logbooksState.rows = [HOME];
        await shown({
            state: 'not_applied_intent_pending',
            failureCode: 'default_change_target_missing',
        });
        const n = changeNotice();
        expect(n?.text).toMatch(/no longer exists/);
        expect(n?.text).not.toMatch(/undefined/);
        expect(n?.action?.kind).toBe('cancel');
        expect(n?.action?.label).toMatch(/^Cancel change/);
        expect(n?.action?.label).not.toMatch(/undefined/);
        await runChangeAction('cancel', n?.requestId ?? '');
        expect(defaultChangeAction).toHaveBeenCalledWith('cancel', REQ);
    });

    it('a start’s reason without a failure code still says something useful', async () => {
        await shown({ state: 'not_applied_intent_pending', failureCode: '' });
        const n = changeNotice();
        expect(n?.text).toMatch(/still pending: no reason was recorded/);
        expect(n?.action?.kind).toBe('cancel');
    });

    it('withdrawal_unconfirmed: not yet on disk, with the power-loss qualification — Confirm', async () => {
        await shown({ state: 'withdrawal_unconfirmed', removed: 'cancelled' });
        const n = changeNotice();
        expect(n?.text).toMatch(/cancelled/);
        expect(n?.text).toMatch(/not yet confirmed on disk/);
        expect(n?.text).toMatch(
            /If power is lost before it is, the change to “Contest” may still apply at a later start\./
        );
        expect(n?.action?.kind).toBe('confirm');
        expect(n?.action?.label).toMatch(/Confirm/);
    });

    it('applied_cleanup_pending (Contest effective): cleanup pending — Retry cleanup', async () => {
        await shownApplied({ state: 'applied_cleanup_pending' });
        expect(reloads).toBe(0);
        const n = changeNotice();
        expect(n?.text).toMatch(/Default: “Contest”/);
        expect(n?.text).toMatch(/Cleanup pending/);
        expect(n?.action?.kind).toBe('retry-cleanup');
        expect(n?.action?.label).toMatch(/Retry cleanup/);
    });

    it.each([
        [
            'not_applied_abandoned',
            'default_change_target_ineligible',
            /was not applied: its callsign no longer matches the current default’s callsign/,
        ],
        [
            'not_applied_abandoned',
            'default_change_wrong_archive',
            /was not applied: .*another archive/,
        ],
        ['not_applied_abandoned', '', /was not applied: no reason was recorded/],
        ['restart_failed', 'restart_failed', /withdrawn: the restart failed/],
        ['cancelled', '', /The change to “Contest” was cancelled\./],
        ['withdrawn', '', /The change to “Contest” was withdrawn\./],
    ])('settled %s (%s): the outcome and its reason, no action', async (state, failureCode, re) => {
        await shown({ state, failureCode, unconfirmed: false });
        const n = changeNotice();
        expect(n?.tone).toBe('info');
        expect(n?.text).toMatch(re);
        expect(n?.action).toBeNull();
    });

    it('settled effective (Contest effective): the change is complete, no action', async () => {
        await shownApplied({ state: 'effective', unconfirmed: false });
        expect(reloads).toBe(0);
        const n = changeNotice();
        expect(n?.tone).toBe('info');
        expect(n?.text).toMatch(/“Contest” is now the default/);
        expect(n?.action).toBeNull();
    });

    it('nothing in play: nothing shown', async () => {
        await shown(null);
        expect(changeNotice()).toBeNull();
    });
});

describe('boot: the status read inside the identity bracket', () => {
    it.each([
        ['unreadable', { kind: 'unreadable' as const, message: 'HTTP 500' }],
        ['no answer', { kind: 'network' as const, message: 'down' }],
    ])('a status that is %s gates live work, in default-change words', async (_n, out) => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(out);
        await bootDefaultChange();
        expect(defaultChangeState.read).toBe('unreadable');
        expect(defaultChangeGate() ?? '').toMatch(GATED);
        expect(archivesState.switchUnresolved).toBe(false);
    });

    it('a 404 is unsupported: no gate', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({ kind: 'unsupported' });
        await bootDefaultChange();
        expect(defaultChangeGate()).toBeNull();
    });

    it('pending_restart at boot: a fresh tab sees the change and sealed entry', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(status({ state: 'pending_restart' }));
        await bootDefaultChange();
        expect(defaultChangeGate() ?? '').toMatch(GATED);
        expect(changeNotice()?.tone).toBe('sealed');
    });

    it('nothing in play at boot: no gate', async () => {
        await bootDefaultChange();
        expect(defaultChangeGate()).toBeNull();
    });

    it('a status naming another default reloads; it never rebinds silently', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
            status(null, { id: 2, uuid: P, name: 'Portable', callsign: '7Q5MLV/P' })
        );
        await bootDefaultChange();
        expect(reloads).toBe(1);
        expect(defaultChangeGate() ?? archivesState.switchDetail).not.toBe('');
    });
});

describe('failed reads preserve an existing gate', () => {
    it('unreadable at boot, unreadable again: still gated; then a readable idle status releases it', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({ kind: 'unreadable', message: 'x' });
        await bootDefaultChange();
        await recheckDefaultChange();
        expect(defaultChangeGate() ?? '').toMatch(GATED);
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(idle);
        await recheckDefaultChange();
        expect(defaultChangeGate()).toBeNull();
    });

    it('the recovery check finding another default reloads, and does not reopen live work', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({ kind: 'unreadable', message: 'x' });
        await bootDefaultChange();
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
            status(null, { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV' })
        );
        await recheckDefaultChange();
        expect(reloads).toBe(1);
        expect(defaultChangeGate() ?? '').toMatch(GATED);
    });

    it('once proven at boot, a later failed read does not impose the gate', async () => {
        await bootDefaultChange();
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({ kind: 'unreadable', message: 'x' });
        await loadDefaultChange();
        expect(defaultChangeGate()).toBeNull();
    });
});

describe('the recovery actions', () => {
    it('Cancel change: no confirmation dialog; names the request it was shown; sent once', async () => {
        const dialog = vi.fn(() => true);
        vi.stubGlobal('confirm', dialog);
        await shown({ state: 'pending_unclear' });
        await runChangeAction('cancel', REQ);
        expect(dialog).not.toHaveBeenCalled();
        expect(defaultChangeAction).toHaveBeenCalledTimes(1);
        expect(defaultChangeAction).toHaveBeenCalledWith('cancel', REQ);
        vi.unstubAllGlobals();
    });

    it('a 200 is rendered as returned: an uncertain removal stays actionable (Confirm)', async () => {
        await shown({ state: 'withdrawal_unconfirmed', removed: 'cancelled' });
        const still = status({ state: 'withdrawal_unconfirmed', removed: 'cancelled' });
        vi.mocked(defaultChangeAction).mockResolvedValue(asAction(still));
        await runChangeAction('confirm', REQ);
        expect(defaultChangeState.status).toEqual(still.status);
        expect(changeNotice()?.action?.kind).toBe('confirm');
        expect(toast('info', /confirmed|done|complete/i)).toBe(false);
    });

    it('a 200 that settles it: the returned state replaces the notice', async () => {
        await shown({ state: 'pending_unclear' });
        const settled = status({ state: 'cancelled', removed: 'cancelled', unconfirmed: false });
        vi.mocked(defaultChangeAction).mockResolvedValue(asAction(settled));
        await runChangeAction('cancel', REQ);
        expect(defaultChangeState.status).toEqual(settled.status);
        expect(changeNotice()?.action ?? null).toBeNull();
    });

    it('a 200 naming another effective default reloads, never rebinds', async () => {
        await shownApplied({ state: 'applied_cleanup_pending' });
        vi.mocked(defaultChangeAction).mockResolvedValue(
            asAction(status(null, { id: 2, uuid: P, name: 'Portable', callsign: '7Q5MLV/P' }))
        );
        await runChangeAction('retry-cleanup', REQ);
        expect(reloads).toBe(1);
    });

    it.each([
        ['already_resolved'],
        ['pending_changed'],
        ['not_permitted'],
        ['default_change_in_progress'],
    ])('%s: the daemon’s words and a status re-read; never retried', async (code) => {
        await shown({ state: 'pending_unclear' });
        vi.mocked(defaultChangeAction).mockResolvedValue({
            kind: 'refused',
            code,
            message: `words for ${code}`,
        });
        const reads = vi.mocked(fetchDefaultChangeStatus).mock.calls.length;
        const after = status({ state: 'pending_restart', requestId: REQ2 });
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(after);
        await runChangeAction('cancel', REQ);
        expect(toast('error', new RegExp(`words for ${code}`))).toBe(true);
        expect(vi.mocked(fetchDefaultChangeStatus).mock.calls.length).toBeGreaterThan(reads);
        expect(defaultChangeState.status).toEqual(after.status);
        expect(defaultChangeAction).toHaveBeenCalledTimes(1);
    });

    it('default_change_write_failed: the daemon’s words; the state shown is unchanged', async () => {
        await shown({ state: 'withdrawal_unconfirmed', removed: 'withdrawn' });
        const before = defaultChangeState.status;
        vi.mocked(defaultChangeAction).mockResolvedValue({
            kind: 'refused',
            code: 'default_change_write_failed',
            message: 'the write failed',
        });
        await runChangeAction('confirm', REQ);
        expect(toast('error', /the write failed/)).toBe(true);
        expect(defaultChangeState.status).toEqual(before);
        expect(changeNotice()?.action?.kind).toBe('confirm');
    });

    it('an unknown outcome is reconciled by the status read, never retried', async () => {
        await shown({ state: 'pending_unclear' });
        vi.mocked(defaultChangeAction).mockResolvedValue({ kind: 'unknown', message: 'timed out' });
        const after = status({ state: 'cancelled', removed: 'cancelled', unconfirmed: false });
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(after);
        await runChangeAction('cancel', REQ);
        expect(defaultChangeAction).toHaveBeenCalledTimes(1);
        expect(toast('warn', /outcome is unknown/)).toBe(true);
        expect(defaultChangeState.status).toEqual(after.status);
    });

    it('overlapping actions are prevented: a second one is not sent', async () => {
        await shown({ state: 'pending_unclear' });
        let answer: (v: DefaultChangeActionOutcome) => void = () => {};
        vi.mocked(defaultChangeAction).mockReturnValue(new Promise((r) => (answer = r)));
        const first = runChangeAction('cancel', REQ);
        await vi.waitFor(() => expect(defaultChangeAction).toHaveBeenCalledTimes(1));
        expect(defaultChangeState.acting).toBe(true);
        await runChangeAction('cancel', REQ);
        answer(asAction(idle));
        await first;
        expect(defaultChangeAction).toHaveBeenCalledTimes(1);
        expect(defaultChangeState.acting).toBe(false);
    });

    it('a stale action response arriving after a newer status does not overwrite it', async () => {
        await shown({ state: 'withdrawal_unconfirmed', removed: 'withdrawn' });
        let answer: (v: DefaultChangeActionOutcome) => void = () => {};
        vi.mocked(defaultChangeAction).mockReturnValue(new Promise((r) => (answer = r)));
        const run = runChangeAction('confirm', REQ);
        await vi.waitFor(() => expect(defaultChangeAction).toHaveBeenCalledTimes(1));
        // A newer status lands while the action is on the wire.
        const newer = status({ state: 'pending_restart', requestId: REQ2 });
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(newer);
        await loadDefaultChange();
        // The action's older answer arrives afterwards.
        answer(asAction(status({ state: 'withdrawal_unconfirmed', removed: 'withdrawn' })));
        await run;
        expect(defaultChangeState.status).toEqual(newer.status);
        // The newer status sealed admission; ignoring the stale answer keeps it.
        expect(defaultChangeGate() ?? '').toMatch(GATED);
    });
});

describe('an accepted request whose restart is not seen (replaces the archive overlay)', () => {
    it.each([
        ['pending_restart', status({ state: 'pending_restart' })],
        ['a failed status re-read', { kind: 'unreadable' as const, message: 'x' }],
    ] as [string, DefaultChangeStatusOutcome][])(
        'after %s it keeps watching, and reloads when the new instance answers',
        async (_n, after) => {
            await loadDefaultChange();
            vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(after);
            vi.mocked(waitForDaemonBack)
                .mockResolvedValueOnce(false)
                .mockResolvedValueOnce(false)
                .mockResolvedValue(true);
            expect(await setAsDefault(3, () => true)).toBe(true);
            await vi.waitFor(() => expect(reloads).toBe(1));
            expect(waitForDaemonBack).toHaveBeenLastCalledWith('i1');
            expect(vi.mocked(waitForDaemonBack).mock.calls.length).toBeGreaterThanOrEqual(3);
        }
    );

    it.each([
        ['pending_restart: sealed', { state: 'pending_restart' }, GATED],
        ['unreadable', null, GATED],
    ] as [string, Record<string, unknown> | null, RegExp][])(
        '%s → live work gated in default-change words; the app is not made inert',
        async (_n, change, re) => {
            await loadDefaultChange();
            vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
                change === null ? { kind: 'unreadable', message: 'x' } : status(change)
            );
            expect(await setAsDefault(3, () => true)).toBe(true);
            expect(archivesState.switchUnresolved).toBe(false);
            expect(defaultChangeGate() ?? '').toMatch(re);
            expect(reloads).toBe(0);
        }
    );

    it('the old default with admission reopened (pending_unclear): released, recovery shown', async () => {
        await loadDefaultChange();
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(status({ state: 'pending_unclear' }));
        await setAsDefault(3, () => true);
        expect(archivesState.switchUnresolved).toBe(false);
        expect(defaultChangeGate()).toBeNull();
        expect(changeNotice()?.action?.kind).toBe('cancel');
    });
});

describe('Delete while a change holds a logbook', () => {
    it('not offered for the target of a change config still holds', async () => {
        await shown({ state: 'pending_unclear' });
        expect(pendingTargetBlock(CONTEST) ?? '').toMatch(/change/i);
        expect(pendingTargetBlock(HOME)).toBeNull();
    });

    it('a settled change does not hold it', async () => {
        await shown({ state: 'cancelled', removed: 'cancelled', unconfirmed: false });
        expect(pendingTargetBlock(CONTEST)).toBeNull();
    });
});
