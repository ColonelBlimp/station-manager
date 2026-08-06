/*
    A reviving EventSource wrapper — shared by all three SSE clients.

    WHY. The browser owns reconnect for TRANSIENT drops, and that is still true.
    What it does not own is a stream that died for good, and there are two ways
    that happens:

    - The tab was HIDDEN: a backgrounded tab gets throttled and can be frozen
      outright, and the stream can come back CLOSED, or stuck retrying forever.
      Nothing recreated it, so a surface went quiet until a manual reload
      (dogfood 2026-07-18, the map in a background tab). Trigger: the tab
      becoming visible again.
    - The NETWORK bounced with the tab visible the whole time: a router swap
      dropped the interface for 44 s and the browser tore down its connections
      on the OS network-change signal — the LOOPBACK one to the daemon
      included, which no router can break at TCP level — and never reconnected;
      the CAT banner sat on 'lost' until a manual reload (dogfood 2026-08-06).
      Visibility never changed, so the first trigger could not fire. Trigger:
      the window 'online' event, the browser's own signal for this moment.
      A spurious 'online' (navigator.onLine is unreliable) is harmless because
      a healthy stream is left alone.

    ONLY WHEN DEAD, never on a schedule and never unconditionally. Tearing down a
    HEALTHY stream would be actively dangerous on one of the three: closing
    /v1/ft8/events starts the daemon's 5 s capture linger, and if the reopen then
    failed, onLingerExpired disarms TX — dropping PTT and abandoning the QSO
    (internal/ft8/service.go). The moment we would be recreating is exactly the
    moment a reopen is least likely to succeed, so a working stream is left alone.

    NO THRESHOLDS. "Dead" is read from readyState plus whether an error has
    arrived since the last open — never from a timer or an age. The one case that
    needs the error flag is a stream stuck in CONNECTING: readyState never reaches
    CLOSED, so a CLOSED-only check would miss precisely the silent failure. The
    flag also keeps the FIRST connect from looking dead, since it is CONNECTING
    with no error yet.

    BOTH triggers revive only while the tab is VISIBLE. 'online' in a hidden
    tab deliberately does nothing (a judgement call, flagged in the test
    header): return-to-visible already revives, and a hidden FT8 tab must not
    re-grab the audio capture device in the background.
*/

/** Attaches the caller's event listeners to a freshly created stream. */
export type WireFn = (src: EventSource) => void;

// EventSource.readyState values, spelled out because the constants live on the
// constructor and a stubbed EventSource in tests need not carry them.
const CONNECTING = 0;
const OPEN = 1;
const CLOSED = 2;

export function openReviving(url: string, wire: WireFn): () => void {
    let src: EventSource | null = null;
    // Set by an 'error' with no 'open' since. This is what separates a stream
    // stuck retrying from one that is simply making its first connection —
    // both sit in CONNECTING and only this tells them apart.
    let errored = false;

    const create = (): EventSource => {
        const es = new EventSource(url);
        // Registered BEFORE wire() so the caller cannot displace them, and kept
        // separate from the caller's own onOpen/onTransportError: those are for
        // showing the operator a state, these are for deciding whether the
        // stream is worth keeping.
        es.addEventListener('open', () => {
            errored = false;
        });
        es.addEventListener('error', () => {
            errored = true;
        });
        wire(es);
        return es;
    };

    // Deliberately conservative — the ONLY states that count as dead are a
    // stream the browser has given up on, and one retrying after a failure.
    // Anything else, including a first connect in progress, is left alone.
    const isDead = (): boolean => {
        if (src === null) return false; // torn down; nothing to revive
        if (src.readyState === OPEN) return false;
        return src.readyState === CLOSED || (src.readyState === CONNECTING && errored);
    };

    // ONE handler for both triggers — becoming visible and coming back
    // online are the same decision: revive only what is visible AND dead.
    const reviveIfVisibleAndDead = (): void => {
        if (document.visibilityState !== 'visible') return;
        if (!isDead()) return;
        // close() on an already-dead stream is a no-op, but it is not skipped:
        // the CONNECTING-after-error case still has a retry timer running
        // inside the browser, and leaving it would race the replacement.
        src?.close();
        errored = false;
        src = create();
    };

    src = create();
    document.addEventListener('visibilitychange', reviveIfVisibleAndDead);
    window.addEventListener('online', reviveIfVisibleAndDead);

    return () => {
        // TWO mechanisms guard teardown and they are not equivalent, so be
        // careful before deleting either as redundant:
        //   - removing the listeners stops the handler running at all, and is
        //     the only thing preventing a listener leaking onto `document` and
        //     `window` for every mount/unmount cycle (the map re-mounts on
        //     every route change). That leak is invisible to a behaviour test —
        //     nothing observable changes — so no rule pins it alone,
        //     deliberately.
        //   - `src === null` in isDead() is the BEHAVIOURAL guard: even with a
        //     leaked listener, a torn-down wrapper revives nothing.
        // Removing either alone leaves V6/O6 green; removing both turns them
        // red, which is what proves the pair rather than each half.
        document.removeEventListener('visibilitychange', reviveIfVisibleAndDead);
        window.removeEventListener('online', reviveIfVisibleAndDead);
        src?.close();
        src = null;
    };
}
