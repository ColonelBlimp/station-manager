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
import {
    daemonNowMs,
    daemonClockTrusted,
    noteDaemonDate,
    _resetDaemonClockForTests,
} from './daemonClock.svelte';

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

/*
    SUSPEND — the other half of the clock trade-off (codex 503f31c7 P2).

    performance.now() is monotonic, which is why elapsed time is measured with it,
    but on most non-Windows browsers it does NOT advance while the machine sleeps.
    So the two failure modes are symmetric and we cannot have both:

      wall clock   survives suspend, broken by a clock STEP
      monotonic    survives a clock step, broken by SUSPEND

    Worse, from inside the page a long suspend and a forward clock step look
    IDENTICAL — the same disagreement between the two readings. Guessing which
    happened is what the previous two rounds each got wrong in one direction.

    So this does not guess. When the two clocks disagree the calibration is simply
    not trustworthy, and the client stops claiming to know: nothing is marked
    stale, every click goes through, and the DAEMON adjudicates — which it always
    did, being the actual guarantee. Failing open is right in both directions:

      suspend      stale rows become clickable; the daemon refuses them with a
                   clear message, and THAT RESPONSE RECALIBRATES (safeFetch samples
                   every response, including a 409), so it self-heals in one click.
      clock step   no rows are wrongly greyed, so the deadlock of the previous
                   round — blocked clicks blocking the requests that would
                   recalibrate — cannot recur.
*/
describe('daemonClock trust', () => {
    beforeEach(() => _resetDaemonClockForTests());
    afterEach(() => {
        vi.restoreAllMocks();
        _resetDaemonClockForTests();
    });

    // D4 — the discriminator: in ordinary operation the two clocks agree and the
    // calibration IS trusted, so failing open is a fault response and not the
    // permanent state.
    it('trusts the calibration while both clocks agree', () => {
        noteDaemonDate(new Date().toUTCString());
        expect(daemonClockTrusted()).toBe(true);
    });

    // D5 — THE CRITERION. Monotonic time frozen while wall time advances is
    // exactly what a suspended laptop looks like on resume.
    it('distrusts the calibration after a suspend', () => {
        noteDaemonDate(new Date().toUTCString());
        const perfFrozen = performance.now();
        const wallAfterSleep = Date.now() + 10 * 60_000;

        vi.spyOn(performance, 'now').mockReturnValue(perfFrozen);
        vi.spyOn(Date, 'now').mockReturnValue(wallAfterSleep);

        expect(daemonClockTrusted()).toBe(false);
    });

    // D6 — an UNCALIBRATED clock is not "untrusted": it is the browser clock,
    // which is the best available answer and the state every session starts in.
    // Collapsing the two would disable the staleness guard until the first
    // request completed, which is most of a page load.
    it('treats an uncalibrated clock as trusted', () => {
        expect(daemonClockTrusted()).toBe(true);
    });
});
