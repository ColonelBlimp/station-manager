// W-0023 weekend slice (operator, 2026-10-31): contest timers in the shell
// header, contest mode only — "Last" (full minutes since the last contest QSO,
// green once it counts as an off period: UK/EI 60 min) and "Op" (operating time
// since the first contest QSO).

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Header from './Header.svelte';
import { contest } from '../operate/contest.svelte';
import { contestHistory, setContestHistoryLoader } from '../operate/contestHistory.svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({ fetchDaemonInstance: vi.fn(), waitForDaemonBack: vi.fn() }));

const at = (h: number, m: number) => Date.UTC(2026, 9, 31, h, m);

beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] });
    contest.on = true;
    contest.id = 'UKEIDXSSB';
    contest.start = '2026-10-31';
    Object.assign(contestHistory, { key: '', completions: [], failed: false });
});
afterEach(() => {
    contest.on = false;
    setContestHistoryLoader(null);
    vi.useRealTimers();
});

async function renderWith(completions: number[], now: number) {
    vi.setSystemTime(now);
    setContestHistoryLoader(() => Promise.resolve(completions));
    render(Header);
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
}

describe('Header contest timers', () => {
    it('shows Last and Op in contest mode, from the occurrence history', async () => {
        await renderWith([at(12, 10)], at(12, 31));
        expect(contestHistory.key).toBe('UKEIDXSSB|20261031');
        // Labelled, so each timer says what it is (operator, 2026-10-31).
        expect(screen.getByTestId('timer-last')).toHaveTextContent('Last QSO');
        expect(screen.getByTestId('timer-last-value')).toHaveTextContent(/^20 min$/);
        expect(screen.getByTestId('timer-rate')).toHaveTextContent('Rate');
        expect(screen.getByTestId('timer-rate-value')).toHaveTextContent(/^1\/hr$/);
        expect(screen.getByTestId('timer-rate')).toHaveAttribute('title');
        expect(screen.getByTestId('timer-elapsed')).toHaveTextContent('Elapsed');
        expect(screen.getByTestId('timer-elapsed-value')).toHaveTextContent(/^0:21$/);
        expect(screen.queryByTestId('timer-op')).toBeNull();
        expect(screen.getByTestId('timer-last')).toHaveAttribute('title');
        expect(screen.getByTestId('timer-elapsed')).toHaveAttribute('title');
    });

    it('ticks, and Last turns green as an off period at 60 minutes', async () => {
        await renderWith([at(12, 10)], at(13, 10));
        expect(screen.getByTestId('timer-last-value')).toHaveTextContent(/^59 min$/);
        expect(screen.getByTestId('timer-last-value')).not.toHaveClass('text-green-500');
        await vi.advanceTimersByTimeAsync(60_000);
        flushSync();
        expect(screen.getByTestId('timer-last-value')).toHaveTextContent(/^60 min ✓$/);
        expect(screen.getByTestId('timer-rate-value')).toHaveTextContent(/^0\/hr$/);
        expect(screen.getByTestId('timer-last-value')).toHaveClass('text-green-500');
    });

    it('nothing logged yet: Last is a dash; no timers outside contest mode', async () => {
        await renderWith([], at(12, 5));
        expect(screen.getByTestId('timer-last-value')).toHaveTextContent(/^–$/);
        expect(screen.getByTestId('timer-elapsed-value')).toHaveTextContent(/^–$/);
        contest.on = false;
        flushSync();
        expect(screen.queryByTestId('timer-last')).toBeNull();
        expect(screen.queryByTestId('timer-elapsed')).toBeNull();
    });

    // Codex review ad746e2f: a failed history load must not show zeros as if
    // nothing had been logged; it says so and offers a retry.
    it('a failed history load shows no timers, and a retry that reloads', async () => {
        vi.setSystemTime(at(12, 31));
        const loader = vi
            .fn()
            .mockResolvedValueOnce(null)
            .mockResolvedValueOnce([at(12, 10)]);
        setContestHistoryLoader(loader);
        render(Header);
        await vi.advanceTimersByTimeAsync(0);
        flushSync();
        expect(screen.queryByTestId('timer-last')).toBeNull();
        expect(screen.queryByTestId('timer-op')).toBeNull();
        const retry = screen.getByRole('button', { name: /retry/i });
        expect(screen.getByTestId('timers-unavailable')).toHaveTextContent('Timers unavailable');
        retry.click();
        await vi.advanceTimersByTimeAsync(0);
        flushSync();
        expect(loader).toHaveBeenCalledTimes(2);
        expect(screen.getByTestId('timer-last-value')).toHaveTextContent(/^20 min$/);
    });
});
