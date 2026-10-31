// W-0023 weekend slice (operator, 2026-10-31): unticking Contest during the
// contest's own hours is the one way to drop contest contacts from the entry
// unnoticed — contacts logged then carry no contest fields. A sticky warning
// says so; re-ticking reloads the occurrence's history (another tab may have
// logged meanwhile).

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import { screen } from '@testing-library/svelte';
import Header from './Header.svelte';
import { contest } from '../operate/contest.svelte';
import { contestHistory, setContestHistoryLoader } from '../operate/contestHistory.svelte';
import { toastsState, dismissToast, _resetForTests as resetToasts } from './toasts.svelte';
import { draft, clearDraft, startQso, setSerialAllocator } from '../operate/qso.svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({ fetchDaemonInstance: vi.fn(), waitForDaemonBack: vi.fn() }));

const at = (h: number, m: number) => Date.UTC(2026, 9, 31, h, m);
const warns = () => toastsState.items.filter((t) => t.level === 'warn');

beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] });
    resetToasts();
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

async function untick() {
    await fireEvent.click(screen.getByTestId('contest-on'));
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
}

describe('unticking Contest', () => {
    it('during the contest: a sticky warning, gone when re-ticked', async () => {
        vi.setSystemTime(at(13, 0));
        render(Header);
        await untick();
        expect(contest.on).toBe(false);
        expect(warns()).toHaveLength(1);
        expect(warns()[0].message).toContain("won't count");
        expect(warns()[0].message).toContain('UK/EI DX SSB');
        expect(warns()[0].ttl).toBe(0);
        await untick(); // re-tick
        expect(warns()).toHaveLength(0);
    });

    it('outside the contest hours: no warning', async () => {
        vi.setSystemTime(at(8, 0));
        render(Header);
        await untick();
        expect(warns()).toHaveLength(0);
        vi.setSystemTime(Date.UTC(2026, 10, 1, 12, 0)); // Sunday 12:00: the end, outside
        await vi.advanceTimersByTimeAsync(1000);
        flushSync();
        expect(warns()).toHaveLength(0);
    });

    it('the warning appears when the contest starts with Contest off, and stays dismissed', async () => {
        vi.setSystemTime(at(11, 59));
        contest.on = false;
        render(Header);
        flushSync();
        expect(warns()).toHaveLength(0);
        await vi.advanceTimersByTimeAsync(60_000); // 12:00:00
        flushSync();
        expect(warns()).toHaveLength(1);
        dismissToast(warns()[0].id);
        await vi.advanceTimersByTimeAsync(5_000);
        flushSync();
        expect(warns()).toHaveLength(0);
    });

    it('re-ticking reloads the occurrence history', async () => {
        vi.setSystemTime(at(13, 0));
        const loader = vi.fn(() => Promise.resolve([at(12, 30)]));
        setContestHistoryLoader(loader);
        render(Header);
        await vi.advanceTimersByTimeAsync(0);
        expect(loader).toHaveBeenCalledTimes(1);
        await untick();
        await untick(); // re-tick
        await vi.advanceTimersByTimeAsync(0);
        expect(loader).toHaveBeenCalledTimes(2);
    });

    it('at the end of the contest, Contest switches itself off and says so', async () => {
        vi.setSystemTime(Date.UTC(2026, 10, 1, 11, 59, 30));
        render(Header);
        flushSync();
        expect(contest.on).toBe(true);
        await vi.advanceTimersByTimeAsync(31_000); // 12:00:01 Sunday
        flushSync();
        expect(contest.on).toBe(false);
        const infos = toastsState.items.filter((t) => t.level === 'info');
        expect(infos.some((t) => t.message.includes('UK/EI DX SSB has ended'))).toBe(true);
        expect(warns()).toHaveLength(0); // not the "contest is off" warning: it is over
    });

    it('never mid-QSO: it waits until the contact is logged or cleared', async () => {
        vi.setSystemTime(Date.UTC(2026, 10, 1, 11, 59, 30));
        setSerialAllocator(() => Promise.resolve({ ok: true as const, serial: 7 }));
        render(Header);
        draft.callsign = 'G4ABC';
        startQso();
        await vi.advanceTimersByTimeAsync(31_000);
        flushSync();
        expect(contest.on).toBe(true);
        clearDraft();
        await vi.advanceTimersByTimeAsync(1_000);
        flushSync();
        expect(contest.on).toBe(false);
        setSerialAllocator(null);
    });

    // Codex review 48af7b77: ticking Contest on an event that is already over
    // must stay on, so another event can be chosen (the dropdown needs it on).
    it('ticking Contest after the chosen event ended keeps it on, to choose another', async () => {
        vi.setSystemTime(Date.UTC(2027, 3, 1, 9, 0)); // long after the 2026 SSB event
        contest.on = false;
        render(Header);
        await fireEvent.click(screen.getByTestId('contest-on'));
        await vi.advanceTimersByTimeAsync(2_000);
        flushSync();
        expect(contest.on).toBe(true);
        const select = screen.getByTestId('contest-select');
        expect(select).toBeEnabled();
        await fireEvent.change(select, { target: { value: 'UKEIDXCW|2027-04-24' } });
        expect(contest.id).toBe('UKEIDXCW');
        await vi.advanceTimersByTimeAsync(2_000);
        flushSync();
        expect(contest.on).toBe(true);
    });

    it('switching to an event that is already over during another does not switch Contest off', async () => {
        vi.setSystemTime(Date.UTC(2027, 3, 24, 13, 0)); // during the April 2027 CW event
        contest.id = 'UKEIDXCW';
        contest.start = '2027-04-24';
        render(Header);
        await vi.advanceTimersByTimeAsync(1_000);
        await fireEvent.change(screen.getByTestId('contest-select'), {
            target: { value: 'UKEIDXSSB|2026-10-31' }, // over
        });
        await vi.advanceTimersByTimeAsync(2_000);
        flushSync();
        expect(contest.on).toBe(true);
    });
});
