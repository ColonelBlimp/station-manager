import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

/*
    SETTINGS → LOGBOOKS — Set as default, what the operator sees (W-0024 slice
    5a). A same-call logbook other than the default offers it; another call shows
    it disabled with the reason; a daemon that does not serve it hides it; a
    status that cannot be read never reads as idle.
*/

vi.mock('../api/logbooks', () => ({
    fetchLogbooks: vi.fn(),
    fetchLogbookCount: vi.fn(),
    createLogbook: vi.fn(),
    updateLogbook: vi.fn(),
    deleteLogbook: vi.fn(),
}));
vi.mock('../api/seams', () => ({ fetchStationContext: vi.fn() }));
vi.mock('../api/qso-archives', () => ({
    fetchDaemonIdentity: vi.fn(),
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
}));
vi.mock('../api/archive-bindings', () => ({
    fetchArchiveBindings: vi.fn(),
    saveArchiveBindings: vi.fn(),
}));
vi.mock('../api/default-change', () => ({
    fetchDefaultChangeStatus: vi.fn(),
    requestDefaultChange: vi.fn(),
    defaultChangeAction: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import LogbooksSection from './LogbooksSection.svelte';
import { fetchLogbookCount, fetchLogbooks } from '../api/logbooks';
import { fetchStationContext } from '../api/seams';
import { fetchDaemonIdentity } from '../api/qso-archives';
import { fetchArchiveBindings } from '../api/archive-bindings';
import {
    defaultChangeAction,
    fetchDefaultChangeStatus,
    requestDefaultChange,
    type DefaultChangeActionOutcome,
} from '../api/default-change';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { setBoundDefault } from '../api/archive-guard';
import { _resetLogbooksForTests } from './logbooks.svelte';
import { _resetArchivesForTests, _setReloadForTests } from './archives.svelte';
import { _resetDefaultChangeForTests } from './defaultChange.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const P = '01a187bc-0200-7000-8000-000000000002';
const C = '01a187bc-0300-7000-8000-000000000003';
const ARCHIVE = '01a0e79a-7b32-7cf5-a72c-57cf437a362d';

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
    _resetLogbooksForTests();
    _resetArchivesForTests();
    _resetDefaultChangeForTests();
    _setReloadForTests(() => {});
    resetToasts();
    setBoundDefault(A);
    vi.mocked(fetchLogbooks).mockResolvedValue({
        kind: 'ok',
        logbooks: [
            { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV', description: '' },
            { id: 2, uuid: P, name: 'Portable', callsign: '7Q5MLV/P', description: '' },
            { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV', description: '' },
        ],
    });
    vi.mocked(fetchLogbookCount).mockResolvedValue({ kind: 'ok', count: 0 });
    vi.mocked(fetchStationContext).mockResolvedValue({
        configOk: true,
        stationCallsign: '7Q5MLV',
        logbookId: 1,
        logbookUuid: A,
    } as never);
    vi.mocked(fetchDaemonIdentity).mockResolvedValue({
        instance: 'i1',
        archiveId: ARCHIVE,
        defaultLogbookUuid: A,
    });
    vi.mocked(fetchArchiveBindings).mockResolvedValue({ kind: 'error', message: 'n/a' } as never);
    vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
        kind: 'ok',
        status: { effective: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' }, change: null },
    });
    vi.mocked(requestDefaultChange).mockResolvedValue({
        kind: 'refused',
        code: 'tx_busy',
        message: 'FT8 transmit is armed',
    });
    vi.mocked(fetchDaemonInstance).mockResolvedValue('i1');
    vi.mocked(waitForDaemonBack).mockResolvedValue(false);
});

afterEach(() => {
    vi.unstubAllGlobals();
});

describe('Set as default on each row', () => {
    it('offered on a same-call logbook, absent on the default, disabled with its reason on another call', async () => {
        render(LogbooksSection);
        await flush();
        await flush();
        const contest = screen.getByTestId('logbook-set-default-3');
        expect(contest).toBeEnabled();
        expect(screen.queryByTestId('logbook-set-default-1')).toBeNull();
        const portable = screen.getByTestId('logbook-set-default-2');
        expect(portable).toBeDisabled();
        expect(portable.getAttribute('title') ?? '').toMatch(/callsign/i);
    });

    it('hidden everywhere when the daemon does not serve it', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({ kind: 'unsupported' });
        render(LogbooksSection);
        await flush();
        await flush();
        expect(screen.queryByTestId('logbook-set-default-3')).toBeNull();
        expect(screen.queryByTestId('logbook-set-default-2')).toBeNull();
    });

    it('a status that cannot be read shows it disabled, saying so — never as idle', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'unreadable',
            message: 'HTTP 500',
        });
        render(LogbooksSection);
        await flush();
        await flush();
        const contest = screen.getByTestId('logbook-set-default-3');
        expect(contest).toBeDisabled();
        expect(contest.getAttribute('title') ?? '').toMatch(/could not be read/i);
    });

    it('clicking asks first, then requests the change', async () => {
        const confirm = vi.fn(() => true);
        vi.stubGlobal('confirm', confirm);
        render(LogbooksSection);
        await flush();
        await flush();
        await fireEvent.click(screen.getByTestId('logbook-set-default-3'));
        await flush();
        expect(confirm).toHaveBeenCalledTimes(1);
        expect(String((confirm.mock.calls[0] as unknown[])[0])).toMatch(
            /^Set ‘Contest \(7Q5MLV\)’ as default/
        );
        expect(requestDefaultChange).toHaveBeenCalledWith(C, A);
    });
});

describe('the change in play (W-0024 slice 5b)', () => {
    const REQ = '01a187bc-0400-7000-8000-000000000004';
    const effective = { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' };
    const held = (state: string) => ({
        kind: 'ok' as const,
        status: {
            effective,
            change: {
                requestId: REQ,
                logbookUuid: C,
                state,
                removed: '',
                failureCode: '',
                unconfirmed: true,
            },
        },
    });

    it('shows the state, and its action labelled with the target; no confirmation dialog', async () => {
        const dialog = vi.fn(() => true);
        vi.stubGlobal('confirm', dialog);
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(held('pending_unclear'));
        let answer: (v: DefaultChangeActionOutcome) => void = () => {};
        vi.mocked(defaultChangeAction).mockReturnValue(new Promise((r) => (answer = r)));
        render(LogbooksSection);
        await flush();
        await flush();
        expect(screen.getByTestId('default-change-notice').textContent ?? '').toMatch(
            /next restart/
        );
        const button = screen.getByTestId('default-change-action');
        expect(button.textContent ?? '').toMatch(/Cancel change.*Contest/);
        await fireEvent.click(button);
        expect(dialog).not.toHaveBeenCalled();
        expect(defaultChangeAction).toHaveBeenCalledWith('cancel', REQ);
        // Overlapping actions are prevented while it is on the wire.
        expect(screen.getByTestId('default-change-action')).toBeDisabled();
        answer({ kind: 'ok', status: { effective, change: null } });
        await flush();
    });

    it('Delete is not offered for the change’s target while config holds it', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(held('pending_unclear'));
        render(LogbooksSection);
        await flush();
        await flush();
        const del = screen.getByRole('button', { name: 'Delete Contest' });
        expect(del).toBeDisabled();
        expect(del.getAttribute('title') ?? '').toMatch(/change/i);
    });
});
