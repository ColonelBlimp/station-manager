// The header's clock counts the current sitting of the DAEMON's session (W-0029
// part 6, criteria D). The nearest confusable outcome is a clock that counts from
// when the window loaded: it looks the same until a reload, a daemon restart or a
// New. So each case fixes the window's "now" far from its load.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import SessionTimer from './SessionTimer.svelte';
import {
    sessionControlStands,
    sessionStateDrawn,
    setSessionPorts,
    _resetSessionForTests,
} from '../operate/session.svelte';
import { TEST_ARCHIVE } from '../../test/archive';
import { noteDaemonDate, _resetDaemonClockForTests } from '../api/daemonClock.svelte';

const S1 = '01a1f83c-da20-7000-8000-0000000000a1';
const S2 = '01a1f83c-da20-7000-8000-0000000000a2';
const NOW = Date.parse('2026-11-25T12:00:00Z');

function open(
    id: string,
    generation: number,
    sittingStartedAt: string | null,
    openedAt = sittingStartedAt
): void {
    sessionStateDrawn({
        archiveId: TEST_ARCHIVE,
        generation,
        state: 'open',
        session: { id, openedAt: openedAt ?? '2026-11-20T09:15:00Z', sittingStartedAt },
    });
    flushSync();
}
const shown = (): string => screen.getByTestId('session-timer').textContent;

beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    sessionStorage.clear();
    _resetSessionForTests();
    _resetDaemonClockForTests();
    setSessionPorts({
        readRows: () => new Promise(() => {}),
        readState: () => Promise.resolve(true),
        archive: () => TEST_ARCHIVE,
    });
});

afterEach(() => {
    vi.useRealTimers();
    sessionStorage.clear();
});

describe('SessionTimer', () => {
    it('D1: counts from the sitting’s start, to the second', () => {
        open(S1, 4, '2026-11-25T10:29:55Z');
        render(SessionTimer);
        expect(shown()).toBe('01:30:05');

        vi.advanceTimersByTime(2000);
        flushSync();
        expect(shown()).toBe('01:30:07');
    });

    it('D1: for a continued session it counts the sitting, not the session’s first start', () => {
        open(S1, 4, '2026-11-25T11:59:00Z', '2026-11-20T09:15:00Z');
        render(SessionTimer);
        expect(shown()).toBe('00:01:00');
    });

    it('D2: a reload shows the same time, and keeps nothing in the tab’s storage', () => {
        open(S1, 4, '2026-11-25T11:00:00Z');
        const first = render(SessionTimer);
        expect(shown()).toBe('01:00:00');
        first.unmount();

        render(SessionTimer); // the page again, a moment later
        expect(shown()).toBe('01:00:00');
        expect(sessionStorage.length).toBe(0);
    });

    it('D3: New and Continue start it again at zero', () => {
        open(S1, 4, '2026-11-25T10:00:00Z');
        render(SessionTimer);
        expect(shown()).toBe('02:00:00');

        open(S2, 5, '2026-11-25T12:00:00Z'); // New, now
        expect(shown()).toBe('00:00:00');
    });

    it('D4: with no session open it shows no time, never a count from the load', () => {
        render(SessionTimer);
        expect(shown()).toBe('');

        vi.advanceTimersByTime(90_000);
        flushSync();
        expect(shown()).toBe('');

        open(S1, 4, '2026-11-25T11:00:00Z');
        expect(shown()).toBe('01:01:30');
        sessionStateDrawn({ archiveId: TEST_ARCHIVE, generation: 5, state: 'none', session: null });
        flushSync();
        expect(shown()).toBe('');
    });

    // The start is the daemon's, so the time since it is counted on the daemon's
    // clock (codex-273). A window whose own clock is five minutes fast would
    // otherwise show a one-minute sitting as six.
    it('D1: counts on the daemon’s clock, not the window’s', () => {
        noteDaemonDate(new Date(NOW - 5 * 60_000).toUTCString()); // the daemon says it is 11:55
        open(S1, 4, '2026-11-25T11:54:00Z'); // one minute ago, by the daemon
        render(SessionTimer);
        expect(shown()).toBe('00:01:00');

        vi.advanceTimersByTime(3000);
        flushSync();
        expect(shown()).toBe('00:01:03');
    });

    it('shows no time for a sitting whose start the page could not read', () => {
        open(S1, 4, null);
        render(SessionTimer);
        expect(shown()).toBe('');
    });

    // The operator's ruling 1, 2026-11-27: when a control's outcome is not
    // known the sitting on screen may have ended, and the clock must not go on
    // counting it. While the control is only on its way the clock stays.
    it("shows no time while a control's outcome is not known, and counts again once it is", () => {
        open(S1, 4, '2026-11-25T10:29:55Z');
        render(SessionTimer);
        expect(shown()).toBe('01:30:05');

        sessionControlStands('out');
        flushSync();
        expect(shown()).toBe('01:30:05');

        sessionControlStands('unknown');
        flushSync();
        expect(shown()).toBe('');

        // New had been applied: the new sitting is drawn, and the gate says so.
        open(S2, 6, '2026-11-25T11:59:50Z');
        sessionControlStands('');
        flushSync();
        expect(shown()).toBe('00:00:10');
    });
});
