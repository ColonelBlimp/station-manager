// W-0023 weekend slice (operator, 2026-10-31): contest mode is chosen in the
// shell header — a Contest checkbox, and when ticked a dropdown of the
// supported contests. Each entry is one occurrence (contest + start date), so
// choosing it sets both. Both lock once the QSO in progress has its serial.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Header from './Header.svelte';
import { contest, CONTESTS, contestId, contestStart } from '../operate/contest.svelte';
import { draft, clearDraft, setSerialAllocator, startQso } from '../operate/qso.svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

beforeEach(() => {
    clearDraft();
    contest.on = false;
    contest.id = 'UKEIDXSSB';
    contest.start = '2026-10-31';
});
afterEach(() => {
    clearDraft();
    contest.on = false;
    setSerialAllocator(null);
});

describe('Header: contest choice', () => {
    it('the catalogue lists the supported contests, each one occurrence with a valid ID and date', () => {
        expect(CONTESTS.length).toBeGreaterThan(0);
        for (const c of CONTESTS) {
            contest.id = c.id;
            contest.start = c.start;
            expect(contestId()).toBe(c.id);
            expect(contestStart()).not.toBe('');
        }
        expect(CONTESTS.some((c) => c.id === 'UKEIDXSSB' && c.start === '2026-10-31')).toBe(true);
    });

    it('the dropdown is disabled until Contest is ticked, and lists every supported contest', async () => {
        render(Header);
        const select = screen.getByTestId('contest-select');
        expect(select).toBeDisabled();
        await fireEvent.click(screen.getByTestId('contest-on'));
        expect(contest.on).toBe(true);
        expect(select).toBeEnabled();
        expect(select.querySelectorAll('option')).toHaveLength(CONTESTS.length);
    });

    it('choosing an entry sets the contest ID and start date together', async () => {
        contest.on = true;
        render(Header);
        const cw = CONTESTS.find((c) => c.id === 'UKEIDXCW');
        expect(cw).toBeDefined();
        const select = screen.getByTestId('contest-select');
        await fireEvent.change(select, { target: { value: `${cw!.id}|${cw!.start}` } });
        expect(contest.id).toBe(cw!.id);
        expect(contest.start).toBe(cw!.start);
    });

    it('locks once the QSO in progress has its serial, and unlocks when it is cleared', async () => {
        setSerialAllocator(() => Promise.resolve({ ok: true as const, serial: 1 }));
        contest.on = true;
        render(Header);
        expect(screen.getByTestId('contest-select')).toBeEnabled();
        draft.callsign = 'G4ABC';
        startQso();
        await new Promise((r) => setTimeout(r, 0));
        flushSync();
        expect(screen.getByTestId('contest-on')).toBeDisabled();
        expect(screen.getByTestId('contest-select')).toBeDisabled();
        clearDraft();
        flushSync();
        expect(screen.getByTestId('contest-on')).toBeEnabled();
        expect(screen.getByTestId('contest-select')).toBeEnabled();
    });
});
