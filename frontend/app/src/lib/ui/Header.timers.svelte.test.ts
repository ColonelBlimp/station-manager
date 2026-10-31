// W-0023 weekend slice (operator, 2026-10-31): contest timers in the shell
// header, contest mode only — "Last" (full minutes since the last contest QSO,
// green once it counts as an off period: UK/EI 60 min) and "Op" (operating time
// so far against the 12-hour limit: amber from 11:00, red past 12:00).

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
        expect(screen.getByTestId('timer-last')).toHaveTextContent('Last 0:20');
        expect(screen.getByTestId('timer-op')).toHaveTextContent('Op 0:21 / 12:00');
    });

    it('ticks, and Last turns green as an off period at 60 minutes', async () => {
        await renderWith([at(12, 10)], at(13, 10));
        expect(screen.getByTestId('timer-last')).toHaveTextContent('Last 0:59');
        expect(screen.getByTestId('timer-last')).not.toHaveClass('text-green-500');
        await vi.advanceTimersByTimeAsync(60_000);
        flushSync();
        expect(screen.getByTestId('timer-last')).toHaveTextContent('Last 1:00 off ✓');
        expect(screen.getByTestId('timer-last')).toHaveClass('text-green-500');
    });

    it('Op turns amber from 11:00 and red past 12:00', async () => {
        const every = (from: number, to: number) => {
            const out = [];
            for (let t = from; t <= to; t += 30 * 60_000) out.push(t);
            return out;
        };
        await renderWith(every(at(12, 0), at(23, 0)), at(23, 1));
        expect(screen.getByTestId('timer-op')).toHaveClass('text-amber-500');
        await vi.advanceTimersByTimeAsync(60 * 60_000); // 00:01: a 61-minute gap is off
        flushSync();
        expect(screen.getByTestId('timer-op')).toHaveClass('text-amber-500');
        contestHistory.completions = every(at(12, 0), at(24, 30));
        await vi.advanceTimersByTimeAsync(60_000);
        flushSync();
        expect(screen.getByTestId('timer-op')).toHaveClass('text-red-500');
    });

    it('nothing logged yet: Last is a dash; no timers outside contest mode', async () => {
        await renderWith([], at(12, 5));
        expect(screen.getByTestId('timer-last')).toHaveTextContent('Last –');
        contest.on = false;
        flushSync();
        expect(screen.queryByTestId('timer-last')).toBeNull();
        expect(screen.queryByTestId('timer-op')).toBeNull();
    });
});
