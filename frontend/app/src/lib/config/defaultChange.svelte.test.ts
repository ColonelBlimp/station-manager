import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../api/default-change', () => ({
    fetchDefaultChangeStatus: vi.fn(),
    requestDefaultChange: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));
vi.mock('../api/archive-bindings', () => ({
    fetchArchiveBindings: vi.fn(),
    saveArchiveBindings: vi.fn(),
}));

import { fetchDefaultChangeStatus, requestDefaultChange } from '../api/default-change';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { fetchArchiveBindings, type BindingsOutcome } from '../api/archive-bindings';
import { setBoundDefault } from '../api/archive-guard';
import { TEST_ARCHIVE } from '../../test/archive';
import {
    _resetArchivesForTests,
    _setBootIdentityForTests,
    _setReloadForTests,
    archivesState,
} from './archives.svelte';
import { logbooksState, _resetLogbooksForTests } from './logbooks.svelte';
import { stationState } from './station.svelte';
import { clearDraft, draft, submitState } from '../operate/qso.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import {
    _resetDefaultChangeForTests,
    defaultChangeGate,
    defaultChangeState,
    loadDefaultChange,
    setAsDefault,
    setDefaultOffer,
} from './defaultChange.svelte';

/*
    W-0024 slice 5a — Set as default and its confirmation (ADR 0097 §4a, §5;
    ADR 0087 items 2, 3, 8, 9; operator rulings 2026-10-29).

    Fixture: the active archive holds Home (the default, A), Contest (same call)
    and Portable (another call). The tab's established context asserts A.
*/

const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const C = '01a187bc-0300-7000-8000-000000000003';
const P = '01a187bc-0200-7000-8000-000000000002';
const REQ = '01a187bc-0400-7000-8000-000000000004';

const HOME = { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV', description: '', count: 12 };
const PORTABLE = {
    id: 2,
    uuid: P,
    name: 'Portable',
    callsign: '7Q5MLV/P',
    description: '',
    count: 0,
};
const CONTEST = { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV', description: '', count: 0 };

const idle = {
    kind: 'ok' as const,
    status: { effective: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' }, change: null },
};

function smcloudRow(over: Record<string, unknown> = {}) {
    return {
        logbook_id: 1,
        logbook_uuid: A,
        logbook_name: 'Home',
        logbook_callsign: '7Q5MLV',
        bound: true,
        enabled: true,
        forwarder_name: 'smcloud-1',
        credentials_set: [],
        queue: { pending: 0, failed: 0 },
        reason: '',
        locked_fields: [],
        hidden_fields: [],
        adoption: null,
        uploads_held: null,
        ...over,
    };
}

function bindings(row: Record<string, unknown> | null): BindingsOutcome {
    return {
        kind: 'ok',
        bindings: {
            archive_id: TEST_ARCHIVE,
            archive_label: 'Home archive',
            restart_required: false,
            destinations: [
                {
                    type: 'smcloud',
                    display_name: 'SM Cloud',
                    account: { configured: true, label: 'x', fields_set: [], build_key: '' },
                    state: 'available',
                    reason: '',
                    new_logbook_reason: '',
                    support: null,
                    logbooks: row ? [smcloudRow(row)] : [],
                },
            ],
        },
    } as unknown as BindingsOutcome;
}

let reloads = 0;
let listReads = 0;
let prompts: string[];
const yes = (t: string): boolean => {
    prompts.push(t);
    return true;
};
const no = (t: string): boolean => {
    prompts.push(t);
    return false;
};
const toast = (level: string, re: RegExp): boolean =>
    toastsState.items.some((t) => t.level === level && re.test(t.message));

beforeEach(async () => {
    vi.mocked(fetchDefaultChangeStatus).mockReset().mockResolvedValue(idle);
    vi.mocked(requestDefaultChange)
        .mockReset()
        .mockResolvedValue({ kind: 'accepted', requestId: REQ, durability: 'durable' });
    vi.mocked(fetchDaemonInstance).mockReset().mockResolvedValue('i1');
    vi.mocked(waitForDaemonBack).mockReset().mockResolvedValue(true);
    vi.mocked(fetchArchiveBindings).mockReset().mockResolvedValue(bindings(null));
    _resetArchivesForTests();
    _setBootIdentityForTests({ instance: 'i1', archiveId: TEST_ARCHIVE, defaultLogbookUuid: A });
    reloads = 0;
    _setReloadForTests(() => reloads++);
    _resetLogbooksForTests();
    logbooksState.rows = [HOME, PORTABLE, CONTEST];
    logbooksState.defaultId = 1;
    logbooksState.stationCallsign = '7Q5MLV';
    logbooksState.loaded = true;
    listReads = 0;
    vi.spyOn(logbooksState, 'load').mockImplementation(() => {
        listReads++;
        return Promise.resolve();
    });
    setBoundDefault(A);
    clearDraft();
    submitState.busy = false;
    submitState.uncertain = false;
    resetToasts();
    prompts = [];
    _resetDefaultChangeForTests();
    await loadDefaultChange();
});

afterEach(() => {
    stationState.reset();
    vi.restoreAllMocks();
});

const flush = () => new Promise((r) => setTimeout(r, 0));

function heldChange(over: Record<string, unknown> = {}) {
    return {
        kind: 'ok' as const,
        status: {
            effective: idle.status.effective,
            change: {
                requestId: REQ,
                logbookUuid: C,
                state: 'pending_unclear',
                removed: '',
                failureCode: '',
                unconfirmed: true,
                ...over,
            },
        },
    };
}

describe('where Set as default is offered', () => {
    it('on a same-call logbook that is not the default; never on the default', () => {
        expect(setDefaultOffer(CONTEST)).toEqual({ offered: true, reason: '' });
        expect(setDefaultOffer(HOME).offered).toBe(false);
    });

    it('a logbook with another callsign shows it disabled, with the reason', () => {
        const o = setDefaultOffer(PORTABLE);
        expect(o.offered).toBe(false);
        expect(o.reason).toMatch(/callsign/i);
    });

    it('a daemon that does not serve the routes (404) hides it: no reason shown', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({ kind: 'unsupported' });
        await loadDefaultChange();
        expect(defaultChangeState.read).toBe('unsupported');
        expect(setDefaultOffer(CONTEST)).toEqual({ offered: false, reason: '' });
    });

    it.each([
        ['unreadable', { kind: 'unreadable' as const, message: 'HTTP 500' }],
        ['no answer', { kind: 'network' as const, message: 'down', timedOut: true }],
    ])(
        'a status that is %s is not idle: disabled, saying it could not be read',
        async (_n, out) => {
            vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(out);
            await loadDefaultChange();
            expect(defaultChangeState.read).toBe('unreadable');
            const o = setDefaultOffer(CONTEST);
            expect(o.offered).toBe(false);
            expect(o.reason).toMatch(/could not be read/i);
        }
    );

    it('a change already in play disables it for every row', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'ok',
            status: {
                effective: idle.status.effective,
                change: {
                    requestId: REQ,
                    logbookUuid: C,
                    state: 'pending_unclear',
                    removed: '',
                    failureCode: '',
                    unconfirmed: true,
                },
            },
        });
        await loadDefaultChange();
        const o = setDefaultOffer(CONTEST);
        expect(o.offered).toBe(false);
        expect(o.reason).not.toBe('');
    });

    it.each([
        ['cancelled', 'cancelled'],
        ['withdrawn after a failed restart', 'withdrawn'],
        ['abandoned', 'not_applied_abandoned'],
        ['restart failed, record gone', 'restart_failed'],
    ])(
        'a settled change (%s, unconfirmed false) is history: another may be made',
        async (_n, state) => {
            vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
                heldChange({ state, removed: 'withdrawn', unconfirmed: false })
            );
            await loadDefaultChange();
            expect(setDefaultOffer(CONTEST)).toEqual({ offered: true, reason: '' });
        }
    );

    it('a logbook without a usable uuid cannot be named: disabled', () => {
        const o = setDefaultOffer({ ...CONTEST, uuid: '' });
        expect(o.offered).toBe(false);
        expect(o.reason).not.toBe('');
    });
});

describe('refusals before anything is asked', () => {
    it('a Log in flight in this tab refuses, before the confirmation', async () => {
        submitState.busy = true;
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(prompts).toHaveLength(0);
        expect(requestDefaultChange).not.toHaveBeenCalled();
        expect(toast('error', /being logged/)).toBe(true);
    });

    it('a Log started while the confirmation is open refuses after it, sending nothing', async () => {
        const ok = (t: string): boolean => {
            prompts.push(t);
            submitState.busy = true;
            return true;
        };
        expect(await setAsDefault(3, ok)).toBe(false);
        expect(prompts).toHaveLength(1);
        expect(requestDefaultChange).not.toHaveBeenCalled();
    });

    it('an unresolved archive switch refuses', async () => {
        archivesState.switchUnresolved = true;
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(prompts).toHaveLength(0);
        expect(requestDefaultChange).not.toHaveBeenCalled();
    });

    it('a tab with no established default refuses: nothing to assert', async () => {
        setBoundDefault(null);
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(requestDefaultChange).not.toHaveBeenCalled();
    });

    it.each([
        ['a Logbooks save on the wire', () => (logbooksState.busy = true), /Logbooks/],
        ['an archive switch in flight', () => (archivesState.activating = true), /archive/],
        ['an archive verification in flight', () => (archivesState.verifying = true), /archive/],
    ] as [string, () => void, RegExp][])(
        '%s refuses, before the confirmation',
        async (_n, set, re) => {
            set();
            expect(await setAsDefault(3, yes)).toBe(false);
            expect(prompts).toHaveLength(0);
            expect(requestDefaultChange).not.toHaveBeenCalled();
            expect(toast('error', re) || toast('info', re)).toBe(true);
        }
    );

    it.each([
        ['a Logbooks save', () => (logbooksState.busy = true)],
        ['an archive switch', () => (archivesState.activating = true)],
        ['an archive verification', () => (archivesState.verifying = true)],
    ] as [string, () => void][])(
        '%s starting during the awaited preparation refuses before asking: nothing is sent',
        async (_n, set) => {
            let release: (v: BindingsOutcome) => void = () => {};
            vi.mocked(fetchArchiveBindings).mockReturnValue(new Promise((r) => (release = r)));
            const run = setAsDefault(3, yes);
            await vi.waitFor(() => expect(fetchArchiveBindings).toHaveBeenCalled());
            set();
            release(bindings(null));
            expect(await run).toBe(false);
            expect(prompts).toHaveLength(0);
            expect(toast('error', /./)).toBe(true);
            expect(requestDefaultChange).not.toHaveBeenCalled();
        }
    );

    it.each([
        ['a Logbooks save', () => (logbooksState.busy = true)],
        ['an archive switch', () => (archivesState.activating = true)],
    ] as [string, () => void][])(
        '%s started while the confirmation is open refuses after it',
        async (_n, set) => {
            const ok = (t: string): boolean => {
                prompts.push(t);
                set();
                return true;
            };
            expect(await setAsDefault(3, ok)).toBe(false);
            expect(prompts).toHaveLength(1);
            expect(requestDefaultChange).not.toHaveBeenCalled();
        }
    );

    it('a second click while the first is still preparing is refused', async () => {
        let release: (v: BindingsOutcome) => void = () => {};
        vi.mocked(fetchArchiveBindings).mockReturnValue(new Promise((r) => (release = r)));
        const first = setAsDefault(3, yes);
        await vi.waitFor(() => expect(fetchArchiveBindings).toHaveBeenCalledTimes(1));
        expect(await setAsDefault(3, yes)).toBe(false);
        release(bindings(null));
        expect(await first).toBe(true);
        expect(prompts).toHaveLength(1);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
    });

    it('while a request is on the wire, a second one is refused', async () => {
        let answer: (v: Awaited<ReturnType<typeof requestDefaultChange>>) => void = () => {};
        vi.mocked(requestDefaultChange).mockReturnValue(new Promise((r) => (answer = r)));
        const first = setAsDefault(3, yes);
        await vi.waitFor(() => expect(requestDefaultChange).toHaveBeenCalledTimes(1));
        expect(await setAsDefault(3, yes)).toBe(false);
        answer({ kind: 'refused', code: 'tx_busy', message: 'FT8 is armed' });
        await first;
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
    });
});

describe('the confirmation', () => {
    it('opens with the question, then the target and its effect', async () => {
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(
            /^Set ‘Contest \(7Q5MLV\)’ as default and restart Station Manager\?/
        );
        expect(prompts[0]).toMatch(
            /Live contacts — Phone \/ CW and FT8 — will go to “Contest \(7Q5MLV\)” after the restart\./
        );
    });

    it('always warns of other windows’ unlogged work, and not of an FT8 exchange (that refuses)', async () => {
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(/unlogged work in any other open window/);
        expect(prompts[0]).toMatch(/unsaved Settings edits/);
        expect(prompts[0]).not.toMatch(/FT8 \/ FT4 exchange/);
    });

    it('names this tab’s unlogged entry: OK discards it, Cancel keeps it', async () => {
        draft.callsign = 'ZS6BOS';
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(/unlogged QSO on Phone \/ CW: ZS6BOS/);
        expect(prompts[0]).toMatch(/OK discards this window’s entry/);
    });

    it('names a partial entry too', async () => {
        draft.comment = 'half typed';
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(/a partial QSO entry/);
    });

    it('an unknown Log outcome may already be logged, in the current default’s Logbook', async () => {
        draft.callsign = 'ZS6BOS';
        submitState.uncertain = true;
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(/may already be logged/);
        expect(prompts[0]).toMatch(/Logbook “Home”/);
        expect(prompts[0]).not.toMatch(/unlogged QSO/);
    });

    it('names this tab’s unsaved Settings edits, Logbooks drafts included', async () => {
        logbooksState.addName = 'Field Day';
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(/unsaved Settings edits in this window: Logbooks/);
    });

    it('names another section’s unsaved edits with Logbooks clean', async () => {
        stationState.form = { station_callsign: '7Q5MLV/M' };
        expect(logbooksState.dirty).toBe(false);
        await setAsDefault(3, no);
        expect(prompts[0]).toMatch(/unsaved Settings edits in this window: Station/);
        expect(prompts[0]).not.toMatch(/in this window: [^\n]*Logbooks/);
    });

    it('says nothing of this tab’s entry or edits when there are none', async () => {
        await setAsDefault(3, no);
        expect(prompts[0]).not.toMatch(/this window has/i);
        expect(prompts[0]).not.toMatch(/in this window:/);
    });

    describe('the SM Cloud note (ADR 0097 §5)', () => {
        const NOTE = /SM Cloud.*continue.*make “Home” the default again/s;

        it('an enabled, unmarked binding on the current default: uploads continue; adoption later needs it default again', async () => {
            vi.mocked(fetchArchiveBindings).mockResolvedValue(bindings({}));
            await setAsDefault(3, no);
            expect(prompts[0]).toMatch(NOTE);
        });

        it.each([
            ['adopted or reserved (locked)', { locked_fields: ['logbook'] }],
            ['born on identity (hidden)', { hidden_fields: ['logbook'] }],
            ['disabled', { enabled: false }],
            ['on another logbook', { logbook_id: 3, logbook_uuid: C }],
        ])('not when the binding is %s', async (_n, over) => {
            vi.mocked(fetchArchiveBindings).mockResolvedValue(bindings(over));
            await setAsDefault(3, no);
            expect(prompts[0]).not.toMatch(/SM Cloud/);
        });

        it('bindings that cannot be read: the note is given conditionally, never dropped', async () => {
            vi.mocked(fetchArchiveBindings).mockResolvedValue({
                kind: 'error',
                message: 'down',
            });
            await setAsDefault(3, no);
            expect(prompts[0]).toMatch(/If “Home” uploads to SM Cloud/);
            // Uploading alone is not the condition: only a binding not yet
            // adopted, reserved or born on identity would need it default again.
            expect(prompts[0]).toMatch(/not yet adopted/);
        });
    });
});

describe('Cancel', () => {
    it('sends nothing and keeps everything', async () => {
        draft.callsign = 'ZS6BOS';
        logbooksState.addName = 'Field Day';
        expect(await setAsDefault(3, no)).toBe(false);
        expect(requestDefaultChange).not.toHaveBeenCalled();
        expect(draft.callsign).toBe('ZS6BOS');
        expect(logbooksState.addName).toBe('Field Day');
        expect(reloads).toBe(0);
    });
});

describe('the request and its outcome', () => {
    it('names the target and the established context’s default', async () => {
        await setAsDefault(3, yes);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
        expect(requestDefaultChange).toHaveBeenCalledWith(C, A);
    });

    it('the established context’s default, never a fresh read: when status and list name others', async () => {
        // The context asserts A; the fresh status says P is effective; the list
        // marks Portable as the default. Send A, or reload/block — never adopt P.
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'ok',
            status: {
                effective: { id: 2, uuid: P, name: 'Portable', callsign: '7Q5MLV/P' },
                change: null,
            },
        });
        await loadDefaultChange();
        logbooksState.defaultId = 2;
        await setAsDefault(3, yes);
        const calls = vi.mocked(requestDefaultChange).mock.calls;
        for (const c of calls) expect(c[1]).toBe(A);
        if (calls.length === 0) {
            expect(reloads > 0 || archivesState.switchUnresolved || toast('error', /./)).toBe(true);
        }
    });

    it('the list alone disagreeing still sends the context’s A', async () => {
        logbooksState.defaultId = 2;
        logbooksState.rows = [HOME, PORTABLE, CONTEST];
        await setAsDefault(3, yes);
        expect(requestDefaultChange).toHaveBeenCalledWith(C, A);
    });

    it('the baseline instance is read to completion before the request is sent', async () => {
        let baseline: (v: string) => void = () => {};
        vi.mocked(fetchDaemonInstance).mockReturnValue(new Promise((r) => (baseline = r)));
        const run = setAsDefault(3, yes);
        await vi.waitFor(() => expect(fetchDaemonInstance).toHaveBeenCalledTimes(1));
        await flush();
        await flush();
        expect(requestDefaultChange).not.toHaveBeenCalled();
        baseline('i1');
        expect(await run).toBe(true);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
        expect(waitForDaemonBack).toHaveBeenCalledWith('i1');
    });

    it('an unreadable baseline cannot prove a restart: nothing is sent', async () => {
        vi.mocked(fetchDaemonInstance).mockResolvedValue('');
        draft.callsign = 'ZS6BOS';
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(requestDefaultChange).not.toHaveBeenCalled();
        expect(waitForDaemonBack).not.toHaveBeenCalledWith('');
        expect(reloads).toBe(0);
        expect(toast('error', /could not read|could not be read/i)).toBe(true);
        expect(draft.callsign).toBe('ZS6BOS');
    });

    it('accepted: waits for a new daemon instance, then reloads; the drafts are not erased first', async () => {
        draft.callsign = 'ZS6BOS';
        logbooksState.addName = 'Field Day';
        expect(await setAsDefault(3, yes)).toBe(true);
        expect(fetchDaemonInstance).toHaveBeenCalled();
        expect(waitForDaemonBack).toHaveBeenCalledWith('i1');
        expect(reloads).toBe(1);
        expect(draft.callsign).toBe('ZS6BOS');
        expect(logbooksState.addName).toBe('Field Day');
    });

    it('accepted but no new instance seen: gated, never reported as done', async () => {
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        await setAsDefault(3, yes);
        expect(reloads).toBe(0);
        expect(archivesState.switchUnresolved).toBe(true);
    });

    it('default_stale: this tab’s context is stale — reload', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({
            kind: 'refused',
            code: 'default_stale',
            message: 'the default changed',
        });
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(reloads).toBe(1);
    });

    it('another refusal: the daemon’s words; drafts kept; list and status re-read', async () => {
        draft.callsign = 'ZS6BOS';
        logbooksState.addName = 'Field Day';
        vi.mocked(requestDefaultChange).mockResolvedValue({
            kind: 'refused',
            code: 'tx_busy',
            message: 'FT8 transmit is armed',
        });
        const statusReads = vi.mocked(fetchDefaultChangeStatus).mock.calls.length;
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(toast('error', /FT8 transmit is armed/)).toBe(true);
        expect(reloads).toBe(0);
        expect(draft.callsign).toBe('ZS6BOS');
        expect(logbooksState.addName).toBe('Field Day');
        expect(listReads).toBeGreaterThan(0);
        expect(vi.mocked(fetchDefaultChangeStatus).mock.calls.length).toBeGreaterThan(statusReads);
    });

    it.each([
        [
            'restart_failed',
            heldChange({ state: 'restart_failed', removed: 'withdrawn', unconfirmed: false }),
        ],
        ['restart_failed_withdrawal_unconfirmed', heldChange({ state: 'withdrawal_unconfirmed' })],
        ['default_change_pending_unclear', heldChange({ state: 'pending_unclear' })],
    ])(
        'a 500 %s: the daemon’s words, and the store holds the status re-read after it',
        async (code, after) => {
            vi.mocked(requestDefaultChange).mockResolvedValue({
                kind: 'refused',
                code,
                message: `words for ${code}`,
            });
            vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(after);
            expect(await setAsDefault(3, yes)).toBe(false);
            expect(toast('error', new RegExp(`words for ${code}`))).toBe(true);
            expect(defaultChangeState.read).toBe('ok');
            expect(defaultChangeState.status).toEqual(after.status);
            expect(setDefaultOffer(CONTEST).offered).toBe(
                after.status.change.unconfirmed === false
            );
            expect(reloads).toBe(0);
            expect(requestDefaultChange).toHaveBeenCalledTimes(1);
        }
    );

    it('a restart failure whose reconciliation read fails: unreadable, never the old idle', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({
            kind: 'refused',
            code: 'restart_failed',
            message: 'the restart failed',
        });
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'unreadable',
            message: 'HTTP 500',
        });
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(defaultChangeState.read).toBe('unreadable');
        expect(defaultChangeState.status).toBeNull();
        expect(setDefaultOffer(CONTEST).reason).toMatch(/could not be read/i);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
    });

    it('an unknown outcome is reconciled, never retried: a new instance → reload', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({
            kind: 'unknown',
            message: 'timed out',
            timedOut: true,
        });
        await setAsDefault(3, yes);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
        expect(toast('warn', /outcome is unknown/)).toBe(true);
        expect(reloads).toBe(1);
    });

    it('an unknown outcome with the old daemon still answering: the status is read and shown, nothing claimed', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({ kind: 'unknown', message: 'reset' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        const pending = {
            kind: 'ok' as const,
            status: {
                effective: idle.status.effective,
                change: {
                    requestId: REQ,
                    logbookUuid: C,
                    state: 'pending_unclear',
                    removed: '',
                    failureCode: 'default_change_pending_unclear',
                    unconfirmed: true,
                },
            },
        };
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(pending);
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
        expect(defaultChangeState.status).toEqual(pending.status);
        expect(toast('error', /./)).toBe(false);
        expect(toast('info', /default.*(set|changed)/i)).toBe(false);
        // pending_unclear: the daemon reopened admission — live work resumes.
        expect(defaultChangeGate()).toBeNull();
    });

    it('an unknown outcome whose status is pending_restart keeps live work gated: admission is still sealed', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({ kind: 'unknown', message: 'reset' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        const sealed = heldChange({ state: 'pending_restart' });
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(sealed);
        expect(await setAsDefault(3, yes)).toBe(false);
        expect(requestDefaultChange).toHaveBeenCalledTimes(1);
        expect(defaultChangeState.status).toEqual(sealed.status);
        expect(defaultChangeState.requesting).toBe(false);
        expect(defaultChangeGate() ?? '').toMatch(/default logbook/i);
        expect(reloads).toBe(0);
    });
});

describe('a sealed change after an unknown outcome', () => {
    it('keeps watching: the new instance, when it answers, reloads the tab', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({ kind: 'unknown', message: 'reset' });
        vi.mocked(waitForDaemonBack).mockResolvedValueOnce(false).mockResolvedValue(true);
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
            heldChange({ state: 'pending_restart' })
        );
        await setAsDefault(3, yes);
        await vi.waitFor(() => expect(reloads).toBe(1));
        expect(waitForDaemonBack).toHaveBeenLastCalledWith('i1');
    });
});

describe('the sealed gate across later status reads', () => {
    it('a failed re-read keeps live work gated; only a readable status that is no longer pending_restart releases it', async () => {
        vi.mocked(requestDefaultChange).mockResolvedValue({ kind: 'unknown', message: 'reset' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
            heldChange({ state: 'pending_restart' })
        );
        await setAsDefault(3, yes);
        expect(defaultChangeGate() ?? '').toMatch(/default logbook/i);

        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'unreadable',
            message: 'HTTP 500',
        });
        await loadDefaultChange();
        expect(defaultChangeGate() ?? '').toMatch(/default logbook/i);

        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(
            heldChange({ state: 'pending_unclear' })
        );
        await loadDefaultChange();
        expect(defaultChangeGate()).toBeNull();
    });
});

describe('the wait for the baseline instance', () => {
    it.each([
        ['a Logbooks save', () => (logbooksState.busy = true)],
        ['an archive switch', () => (archivesState.activating = true)],
        ['an archive verification', () => (archivesState.verifying = true)],
    ] as [string, () => void][])(
        '%s starting while the baseline is read refuses: nothing is sent',
        async (_n, set) => {
            let baseline: (v: string) => void = () => {};
            vi.mocked(fetchDaemonInstance).mockReturnValue(new Promise((r) => (baseline = r)));
            const run = setAsDefault(3, yes);
            await vi.waitFor(() => expect(fetchDaemonInstance).toHaveBeenCalledTimes(1));
            set();
            baseline('i1');
            expect(await run).toBe(false);
            expect(requestDefaultChange).not.toHaveBeenCalled();
            expect(toast('error', /./)).toBe(true);
        }
    );
});
