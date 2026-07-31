/*
    DAEMON CLOCK — elapsed time must come from a MONOTONIC source.

    The skew is calibrated from an HTTP `Date` header, which is sampled only when
    an ordinary request happens. Measured on the dogfood daemon (2026-07-31,
    05:00-06:00): 289 non-SSE requests, but the largest gap between them was 238 s
    — longer than the three-minute staleness limit. So there are real windows
    during operating with no HTTP traffic at all.

    THE FAILURE THAT CAUSED (codex cc032082 P1): if the browser's WALL CLOCK is
    corrected during such a window — NTP stepping a drifted laptop, a timezone or
    clock change — a skew computed against Date.now() becomes wrong by the size of
    the step. Forward by more than the limit and every newly arriving decode is
    marked stale and every FT8 start click is refused.

    And it DEADLOCKS: the requests that would recalibrate are QSO-driven
    (contest-dupe, enrich, logbook), so blocking the clicks blocks the recovery.
    The operator is locked out of transmitting with no indication why.

    THE FIX is to stop measuring elapsed time with the wall clock. performance.now()
    is monotonic and unaffected by clock steps, so the daemon's `Date` supplies the
    EPOCH and the monotonic clock supplies the ELAPSED — a wall-clock step then
    moves neither.
*/

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { daemonNowMs, noteDaemonDate, _resetDaemonClockForTests } from './daemonClock.svelte';

describe('daemonClock', () => {
    beforeEach(() => _resetDaemonClockForTests());
    afterEach(() => {
        vi.restoreAllMocks();
        _resetDaemonClockForTests();
    });

    // D1 — uncalibrated, it is simply the browser clock. Nothing to be clever
    // about before the first response has been seen.
    it('falls back to the browser clock before any calibration', () => {
        expect(Math.abs(daemonNowMs() - Date.now())).toBeLessThan(2_000);
    });

    // D2 — calibrated, it reports the DAEMON's time, not the browser's.
    it('reports daemon time once a Date header has been seen', () => {
        const daemon = Date.now() - 5 * 60_000;
        noteDaemonDate(new Date(daemon).toUTCString());
        expect(Math.abs(daemonNowMs() - daemon)).toBeLessThan(2_000);
    });

    // D3 — THE CRITERION. A wall-clock step must not move daemon time. Without
    // this, a browser corrected forward by more than the staleness limit greys
    // every arriving decode and blocks every start click, with the recalibrating
    // requests locked out behind the same block.
    it('ignores a browser wall-clock step', () => {
        const daemon = Date.now() - 60_000;
        noteDaemonDate(new Date(daemon).toUTCString());
        const before = daemonNowMs();

        // The wall clock jumps ten minutes forward; the monotonic clock does not.
        const stepped = Date.now() + 10 * 60_000;
        vi.spyOn(Date, 'now').mockReturnValue(stepped);

        const after = daemonNowMs();
        expect(Math.abs(after - before)).toBeLessThan(2_000);
        expect(after).toBeLessThan(stepped - 9 * 60_000);
    });
});
