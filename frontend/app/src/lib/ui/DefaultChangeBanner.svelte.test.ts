// The shell's default-change banner (W-0024 slice 5b; operator rulings
// 2026-10-30). Operate shows the state and a link to Settings → Logbooks,
// where the actions live; the actions themselves are never offered here. An
// unproven status keeps a recovery control: check again, or reload.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';

vi.mock('../api/default-change', () => ({
    fetchDefaultChangeStatus: vi.fn(),
    requestDefaultChange: vi.fn(),
    defaultChangeAction: vi.fn(),
}));

import DefaultChangeBanner from './DefaultChangeBanner.svelte';
import { fetchDefaultChangeStatus } from '../api/default-change';
import { setBoundDefault } from '../api/archive-guard';
import { _setReloadForTests } from '../config/archives.svelte';
import { logbooksState, _resetLogbooksForTests } from '../config/logbooks.svelte';
import {
    _resetDefaultChangeForTests,
    bootDefaultChange,
    defaultChangeGate,
    recheckDefaultChange,
} from '../config/defaultChange.svelte';
import { router, takeSettingsTab } from '../router.svelte';

const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const C = '01a187bc-0300-7000-8000-000000000003';
const REQ = '01a187bc-0400-7000-8000-000000000004';
const effective = { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' };

function held(state: string) {
    return {
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
    };
}

let reloads = 0;
beforeEach(() => {
    _resetDefaultChangeForTests();
    _resetLogbooksForTests();
    logbooksState.rows = [
        { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV', description: '', count: 1 },
        { id: 3, uuid: C, name: 'Contest', callsign: '7Q5MLV', description: '', count: 0 },
    ];
    setBoundDefault(A);
    reloads = 0;
    _setReloadForTests(() => reloads++);
    router.view = 'operate';
});
afterEach(() => {
    vi.restoreAllMocks();
});

describe('DefaultChangeBanner', () => {
    it('pending_restart: live entry paused for the change, with the link to Settings → Logbooks', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(held('pending_restart'));
        await bootDefaultChange();
        render(DefaultChangeBanner);
        const banner = screen.getByTestId('default-change-banner');
        expect(banner.textContent ?? '').toMatch(/Contest/);
        expect(banner.textContent ?? '').toMatch(/paused/i);
        await fireEvent.click(screen.getByRole('button', { name: /Settings → Logbooks/ }));
        expect(router.view).toBe('config');
        expect(takeSettingsTab()).toBe('logbooks');
    });

    it('a change needing an action: a notice linking there — no action buttons in the shell', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(held('pending_unclear'));
        await bootDefaultChange();
        render(DefaultChangeBanner);
        expect(screen.getByTestId('default-change-banner').textContent ?? '').toMatch(
            /next restart/
        );
        expect(screen.queryByTestId('default-change-action')).toBeNull();
        expect(screen.queryByRole('button', { name: /Cancel change/ })).toBeNull();
        expect(screen.getByRole('button', { name: /Settings → Logbooks/ })).toBeTruthy();
    });

    it('an unreadable status: gated, with Check again and Reload', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'unreadable',
            message: 'HTTP 500',
        });
        await bootDefaultChange();
        render(DefaultChangeBanner);
        expect(screen.getByTestId('default-change-banner').textContent ?? '').toMatch(/could not/i);
        await fireEvent.click(screen.getByRole('button', { name: /Reload/ }));
        expect(reloads).toBe(1);
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'ok',
            status: { effective, change: null },
        });
        await fireEvent.click(screen.getByRole('button', { name: /Check again/ }));
        await vi.waitFor(() => expect(defaultChangeGate()).toBeNull());
    });

    it('pending_restart, then a failed read: the recovery banner shows, and Check again recovers', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(held('pending_restart'));
        await bootDefaultChange();
        render(DefaultChangeBanner);
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'unreadable',
            message: 'HTTP 500',
        });
        await recheckDefaultChange();
        // Live work stays sealed; the notice is gone with the status, so the
        // recovery banner must carry it.
        expect(defaultChangeGate() ?? '').toMatch(/default logbook/i);
        const banner = await screen.findByTestId('default-change-banner');
        expect(banner.textContent ?? '').toMatch(/could not/i);

        // Still restarting: the sealed notice returns.
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue(held('pending_restart'));
        await fireEvent.click(screen.getByRole('button', { name: /Check again/ }));
        await vi.waitFor(() =>
            expect(screen.getByTestId('default-change-banner').textContent ?? '').toMatch(/paused/i)
        );
        expect(defaultChangeGate()).not.toBeNull();

        // Fails again, then nothing in play: released.
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'unreadable',
            message: 'HTTP 500',
        });
        await recheckDefaultChange();
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'ok',
            status: { effective, change: null },
        });
        await fireEvent.click(await screen.findByRole('button', { name: /Check again/ }));
        await vi.waitFor(() => expect(defaultChangeGate()).toBeNull());
        await vi.waitFor(() => expect(screen.queryByTestId('default-change-banner')).toBeNull());
    });

    it('nothing in play: nothing shown', async () => {
        vi.mocked(fetchDefaultChangeStatus).mockResolvedValue({
            kind: 'ok',
            status: { effective, change: null },
        });
        await bootDefaultChange();
        render(DefaultChangeBanner);
        expect(screen.queryByTestId('default-change-banner')).toBeNull();
    });
});
