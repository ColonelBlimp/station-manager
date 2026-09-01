// F-04b (ADR 0078): a restart POST that TIMES OUT must not be reported as a
// definite "Restart failed" — the daemon replies 202 then exits, so the response
// can be lost while it is already respawning. doRestart reconciles by the SAME
// new-instance signal the accepted path uses (waitForDaemonBack, keyed on the
// pre-restart /v1/version.instance): a DIFFERENT instance confirms the restart; no
// new instance within the cap leaves the outcome unknown. A non-timeout error is
// unchanged. The restart API is mocked so the reconciliation branch is driven
// directly, without waiting on the real 30 s poll.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';

vi.mock('../api/restart', () => ({
    restartDaemon: vi.fn(),
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import Settings from './Settings.svelte';
import { restartDaemon, fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { toastsState, _resetForTests } from '../ui/toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

async function clickRestart(): Promise<void> {
    render(Settings);
    flushSync();
    await fireEvent.click(screen.getByRole('button', { name: /Restart daemon/ }));
    await flush();
    await flush();
    flushSync();
}

const hasToast = (level: string, re: RegExp): boolean =>
    toastsState.items.some((t) => t.level === level && re.test(t.message));

beforeEach(() => {
    // Clear call history on the module-mock fns (restoreAllMocks does not reset a
    // vi.mock factory's fns), so per-test call assertions don't see prior calls.
    vi.clearAllMocks();
    _resetForTests();
    // Child sections load config on mount; keep those fetches inert.
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response('{}', { status: 200, headers: { 'Content-Type': 'application/json' } })
            )
        )
    );
    // doRestart gates on window.confirm; approve it.
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    vi.mocked(fetchDaemonInstance).mockResolvedValue('inst-A');
});

afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    _resetForTests();
});

describe('Settings restart — timed-out reconciliation (F-04b)', () => {
    it('a timed-out restart that a NEW instance confirms reports success, not failure', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({
            kind: 'error',
            message: 'request timed out',
            timedOut: true,
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);

        await clickRestart();

        expect(vi.mocked(waitForDaemonBack)).toHaveBeenCalledWith('inst-A');
        expect(hasToast('info', /Daemon restarted/)).toBe(true);
        expect(hasToast('error', /Restart failed/)).toBe(false);
    });

    it('a timed-out restart with NO new instance reports outcome-unknown, not failure', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({
            kind: 'error',
            message: 'request timed out',
            timedOut: true,
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);

        await clickRestart();

        expect(vi.mocked(waitForDaemonBack)).toHaveBeenCalledWith('inst-A');
        expect(hasToast('warn', /the outcome is unknown/)).toBe(true);
        expect(hasToast('error', /Restart failed/)).toBe(false);
    });

    it('a timed-out restart with NO baseline instance stays outcome-unknown, never false success', async () => {
        // The pre-restart /v1/version read failed, so `before` is '' — there is
        // no baseline id to diff against. waitForDaemonBack('') would count ANY
        // reachable instance, including the UNCHANGED original that never
        // restarted, as "back". A timed-out POST carries no 202 acceptance
        // either, so with no baseline the outcome is simply unknown; the branch
        // must NOT claim "Daemon restarted" (codex ca2ee9b8 P2).
        vi.mocked(fetchDaemonInstance).mockResolvedValue('');
        vi.mocked(restartDaemon).mockResolvedValue({
            kind: 'error',
            message: 'request timed out',
            timedOut: true,
        });
        // Simulate the false-success trap: a reachable instance WOULD satisfy
        // waitForDaemonBack('') and wrongly report success.
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);

        await clickRestart();

        expect(hasToast('warn', /the outcome is unknown/)).toBe(true);
        expect(hasToast('info', /Daemon restarted/)).toBe(false);
        // With no baseline there is nothing to confirm against, so the
        // reconciler must not even attempt the new-instance poll.
        expect(vi.mocked(waitForDaemonBack)).not.toHaveBeenCalled();
    });

    it('a NON-timeout restart error still reports "Restart failed" and does not reconcile', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({
            kind: 'error',
            message: 'Cannot reach the daemon.',
        });

        await clickRestart();

        expect(hasToast('error', /Restart failed/)).toBe(true);
        expect(vi.mocked(waitForDaemonBack)).not.toHaveBeenCalled();
    });
});
