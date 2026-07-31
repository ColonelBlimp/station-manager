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

let skewMs = $state(0);

/** Browser-now minus daemon-now, in ms. Zero until the first response is seen. */
export function daemonSkewMs(): number {
    return skewMs;
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
    skewMs = Date.now() - t;
}

/** Test seam: restore the uncalibrated state. */
export function _resetDaemonClockForTests(): void {
    skewMs = 0;
}
