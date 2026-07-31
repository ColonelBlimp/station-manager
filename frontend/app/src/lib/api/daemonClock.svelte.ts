/*
    Daemon clock skew — how far the browser's clock runs ahead of the daemon's.

    WHY IT EXISTS. FT8 decode staleness compares a decode's slot time (produced by
    the DAEMON) against "now". Using the browser's own clock for that is wrong when
    the two disagree: a browser running more than the staleness limit fast greys
    every Band Activity row the instant it arrives and refuses every click, while
    the daemon — which would have accepted them — never sees the request. Operating
    the SPA from another machine on the LAN is enough to meet it.

    WHY THE HTTP `Date` HEADER, and not an SSE frame. The obvious source is the
    per-slot ft8-decode heartbeat, and it is WRONG: the FT8 hub REPLAYS the last
    decode frame to every new subscriber (internal/ft8/hub.go). A tab opened after
    capture stopped receives an arbitrarily old cached frame, and calibrating from
    its arrival would declare that ancient slot "now" — making its own stale rows
    look fresh and clickable for another three minutes, which is the exact failure
    the staleness guard exists to prevent (codex 0d85428e P2). The hub's comment
    even licenses that caching on the grounds that "a stale decode list is
    cosmetic"; it stopped being cosmetic the moment it drove the clock.

    Every daemon HTTP response carries `Date` (Go's net/http sets it), it is
    CORS-safelisted so it is readable, and it is generated at SEND time — so it
    cannot be replayed stale. safeFetch is the single chokepoint every API helper
    in this directory goes through, which is where it gets sampled.

    SKEW IS A CONSTANT, not a live reading, and that is what makes one sample
    enough. It is a clock OFFSET: callers add the browser's own ticking on top, so
    daemon-now keeps advancing between samples. That matters because the case
    staleness exists for is a band that has gone quiet, where nothing new arrives
    at all.

    Second-granularity (`Date` carries no milliseconds) is immaterial against a
    three-minute threshold.
*/

// The calibration is a PAIR: the daemon's epoch at the sample, and the monotonic
// reading at that same instant. Elapsed time since then comes from the monotonic
// clock, never the wall clock — see daemonNowMs.
let daemonAtMs = $state(0);
let perfAtMs = $state(0);
let calibrated = $state(false);

/**
 * The daemon's current time, in ms since the epoch. Falls back to the browser
 * clock until the first response has been seen — there is nothing better to say.
 *
 * ELAPSED TIME COMES FROM performance.now(), NOT Date.now() (codex cc032082 P1).
 * Calibration only happens when an ordinary HTTP request completes, and measured
 * on the dogfood daemon those can be 238 s apart — longer than the staleness
 * limit. A wall-clock correction inside such a window (NTP stepping a drifted
 * laptop, an operator changing the clock) would otherwise shift this by the whole
 * step: forward past the limit and every arriving decode reads stale and every
 * start click is refused. That DEADLOCKS, because the requests that would
 * recalibrate are QSO-driven — blocking the clicks blocks the recovery.
 *
 * performance.now() is monotonic and unaffected by clock steps, so the daemon's
 * Date supplies the EPOCH and the monotonic clock supplies the ELAPSED. A
 * wall-clock step then moves neither.
 */
export function daemonNowMs(): number {
    if (!calibrated) return Date.now();
    return daemonAtMs + (performance.now() - perfAtMs);
}

/**
 * Record the daemon's clock from a response's `Date` header. Ignores a missing or
 * unparseable value and keeps the last good skew — an unreadable header says
 * nothing about the clock, and guessing would be worse than the previous answer.
 */
export function noteDaemonDate(header: string | null | undefined): void {
    if (!header) return;
    const t = Date.parse(header);
    if (Number.isNaN(t)) return;
    // Both halves captured together: a pair read at different instants would bake
    // the gap between them into every later reading.
    daemonAtMs = t;
    perfAtMs = performance.now();
    calibrated = true;
}

/** Test seam: restore the uncalibrated state. */
export function _resetDaemonClockForTests(): void {
    daemonAtMs = 0;
    perfAtMs = 0;
    calibrated = false;
}
