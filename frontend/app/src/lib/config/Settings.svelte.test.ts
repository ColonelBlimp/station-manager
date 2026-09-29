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
// jsdom cannot reload; the seam records the request.
vi.mock('../utils/reload', () => ({ reloadPage: vi.fn() }));

import Settings from './Settings.svelte';
import { restartDaemon, fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { reloadPage } from '../utils/reload';
import { draft, clearDraft } from '../operate/qso.svelte';
import { showFtSettings, takeSettingsTab } from '../router.svelte';
import { toastsState, _resetForTests } from '../ui/toasts.svelte';
import { archivesState, _resetArchivesForTests } from './archives.svelte';

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
        expect(vi.mocked(reloadPage)).toHaveBeenCalledTimes(1); // confirmed ⇒ reload
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
        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled(); // unknown ⇒ no reload
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
        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
    });

    it('a NON-timeout restart error still reports "Restart failed" and does not reconcile', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({
            kind: 'error',
            message: 'Cannot reach the daemon.',
        });

        await clickRestart();

        expect(hasToast('error', /Restart failed/)).toBe(true);
        expect(vi.mocked(waitForDaemonBack)).not.toHaveBeenCalled();
        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
    });
});

// Fresh-install ruling 2026-09-26: after 'Restart daemon' succeeds, reload the
// page, as archive activation already does. The SPA reads restart-bound state
// once at boot (main.ts reads catEnabled at load and opens the rig stream only if
// it was on), so without a reload a CAT change left the header on 'confirm' and
// the restart note shown after the daemon had connected. Success means a NEW
// daemon instance answered; any other outcome leaves the page as it is.
describe('Settings restart — reload once the new daemon answers', () => {
    it('an accepted restart whose new instance answers reloads the page', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({ kind: 'accepted' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);

        await clickRestart();

        expect(vi.mocked(waitForDaemonBack)).toHaveBeenCalledWith('inst-A');
        expect(vi.mocked(reloadPage)).toHaveBeenCalledTimes(1);
    });

    // Clean-room review 8b100ef2 P2: the Phone/CW draft lives only in memory and
    // the leave guard covers Settings sections only, so a reload would silently
    // discard an unlogged QSO. With one in progress the page stays; the operator
    // is told to log or clear it, then reload.
    it('a confirmed restart does not reload over an unlogged QSO draft', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({ kind: 'accepted' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        draft.callsign = 'DL3YA';
        try {
            await clickRestart();
        } finally {
            clearDraft();
        }

        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
        expect(hasToast('warn', /unlogged QSO/)).toBe(true);
        expect(hasToast('warn', /reload/i)).toBe(true);
        expect(screen.getByRole('button', { name: 'Restart daemon' })).not.toBeDisabled();
    });

    it('an accepted restart with NO baseline instance does not reload (the old daemon could answer)', async () => {
        // The pre-restart /v1/version read failed, so there is no instance to diff
        // against. waitForDaemonBack('') would accept the unchanged old daemon,
        // still answering before it exits — the trap is simulated by resolving true.
        vi.mocked(fetchDaemonInstance).mockResolvedValue('');
        vi.mocked(restartDaemon).mockResolvedValue({ kind: 'accepted' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);

        await clickRestart();

        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
        expect(vi.mocked(waitForDaemonBack)).not.toHaveBeenCalled();
        expect(hasToast('info', /reload the page once Station Manager has reconnected/)).toBe(true);
        expect(screen.getByRole('button', { name: 'Restart daemon' })).not.toBeDisabled();
    });

    it('an accepted restart still not back within the wait does not reload', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({ kind: 'accepted' });
        vi.mocked(waitForDaemonBack).mockResolvedValue(false);

        await clickRestart();

        expect(hasToast('info', /taking a while/)).toBe(true);
        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
    });

    it('a restart refused while transmitting does not reload', async () => {
        vi.mocked(restartDaemon).mockResolvedValue({ kind: 'tx_active' });

        await clickRestart();

        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
    });

    it('a cancelled confirm neither restarts nor reloads', async () => {
        vi.mocked(window.confirm).mockReturnValue(false);

        await clickRestart();

        expect(vi.mocked(restartDaemon)).not.toHaveBeenCalled();
        expect(vi.mocked(reloadPage)).not.toHaveBeenCalled();
    });
});

// Fresh-install ruling 2026-09-26: the tab is 'FT8 / FT4' (one switch serves
// both), and the Phone / CW note's link opens Settings on it.
describe('Settings — the FT8 / FT4 tab', () => {
    afterEach(() => takeSettingsTab());

    it('is named FT8 / FT4', () => {
        render(Settings);
        expect(screen.getByRole('button', { name: 'FT8 / FT4' })).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: 'FT8' })).toBeNull();
    });

    it('opens on FT8 / FT4 when the note asked for it, and on Station otherwise', () => {
        showFtSettings();
        const { unmount } = render(Settings);
        expect(screen.getByRole('button', { name: 'FT8 / FT4' }).className).toMatch(/border-focus/);
        unmount();
        render(Settings); // the handoff was taken once
        expect(screen.getByRole('button', { name: 'Station' }).className).toMatch(/border-focus/);
    });
});

// Review of the slice-3 worktree, finding 1: the sections stay mounted and
// hidden, so the Archives list must be read on every opening of its tab, not
// only on first load (operator ruling 2026-09-29: on open, not by polling).
describe('Settings — the Archives tab', () => {
    const isArchivesUrl = (url: unknown): boolean =>
        (typeof url === 'string'
            ? url
            : url instanceof URL
              ? url.href
              : url instanceof Request
                ? url.url
                : ''
        ).includes('/v1/qso-archives');
    const archiveReads = (): number =>
        vi.mocked(fetch).mock.calls.filter(([url]) => isArchivesUrl(url)).length;

    it('reads the archive list each time the tab opens, and not while it is hidden', async () => {
        // A real (empty) catalogue, so the list counts as loaded: a read that is
        // skipped once loaded must not pass this test.
        _resetArchivesForTests();
        vi.mocked(fetch).mockImplementation((url) =>
            Promise.resolve(
                new Response(isArchivesUrl(url) ? '{"archives":[]}' : '{}', {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        );
        render(Settings);
        await flush();
        expect(archivesState.loaded).toBe(true);
        const atMount = archiveReads();

        await fireEvent.click(screen.getByRole('button', { name: 'Archives' }));
        await flush();
        expect(archiveReads()).toBe(atMount + 1);

        await fireEvent.click(screen.getByRole('button', { name: 'Station' }));
        await flush();
        expect(archiveReads()).toBe(atMount + 1);

        await fireEvent.click(screen.getByRole('button', { name: 'Archives' }));
        await flush();
        expect(archiveReads()).toBe(atMount + 2);
    });
});
