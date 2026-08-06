import { afterEach, describe, expect, it, vi } from 'vitest';
import { openReviving } from './sse-reviving';

/*
    SSE REVIVAL — ON RETURNING TO A HIDDEN TAB, AND ON THE NETWORK COMING BACK.

    ACCEPTANCE CRITERION 1 (operator-checked 2026-08-02, scope narrowed against
    smd.log before any mechanism was chosen):

        When I come back to a tab that was hidden long enough for its stream to
        die, the live surfaces come back on their own without a reload — and I
        can tell that apart from a stream that is merely still connecting, and
        from one that is perfectly healthy and must not be touched.

    ACCEPTANCE CRITERION 2 (operator-directed 2026-08-06, drafted from the
    router-swap incident of that morning):

        When the network bounces while I am watching the tab, the live surfaces
        come back on their own once the OS reports the network is back — no
        reload — and I can tell that apart from a healthy stream (never
        touched), a first connect still in progress, and a bounce while the tab
        is hidden (nothing new happens; returning to the tab already revives).

    The incident behind criterion 2: a router swap bounced enp10s0 for 44 s
    (NetworkManager: carrier-changed 05:23:33, new lease 05:24:17). The browser
    tore down its connections on the OS network-change signal — including the
    LOOPBACK one to the daemon, which no router can break at the TCP level —
    and never reconnected: zero attempts in smd.log, no established sockets.
    The tab was visible the whole time, so visibilitychange (the only revive
    trigger then) never fired, and the CAT banner sat on 'lost' until a manual
    reload. 'online' is the browser's own signal for exactly this moment.

    JUDGEMENT CALL, flagged for the operator: 'online' with the tab HIDDEN
    deliberately revives nothing (O5). Return-to-visible already covers it, and
    reviving a hidden FT8 tab would re-grab the audio capture device from the
    background. If a hidden tab should reconnect eagerly, widen O5 — the
    exclusion is one predicate, not a design constraint.

    WHAT THIS IS NOT. It is not a TX-safety fix. The 2026-07-28 "screen blanked
    mid-run" report is already closed by idle inhibition (65dbcee5): a logind
    idle:sleep block is held for as long as FT8 TX is armed. And a dropped stream
    has never silently ended a run — the 07-28 disarm that looked automatic was
    four operator clicks (POST qso/abandon, cq/start, qso/abandon, tx/arm), and
    both "disconnects" that day were page RELOADS. This is a stale-map fix.

    V2 IS THE LOAD-BEARING RULE, and it is a safety rule rather than an
    efficiency one. Closing /v1/ft8/events starts the daemon's 5 s capture
    linger; if the reopen failed, onLingerExpired disarms TX and abandons the
    active QSO. Tab-switching during a run must therefore never touch a healthy
    stream — and "always recreate on visible", the simpler design, does exactly
    that. An implementation that revives unconditionally passes every other rule
    here; V2 is the only one that stops it.

    FIXTURE NOTE. jsdom cannot suspend a tab, so these drive visibilitychange
    directly against a fake EventSource. They pin the POLICY — which states
    revive and which do not. Whether a real hidden tab reaches those states, and
    whether visibilitychange fires at all on this operator's desktop, is an
    on-hardware question these cannot answer.
*/

const CONNECTING = 0;
const OPEN = 1;
const CLOSED = 2;

class FakeES {
    static instances: FakeES[] = [];
    url: string;
    readyState = CONNECTING;
    closed = false;
    listeners = new Map<string, ((ev: unknown) => void)[]>();

    constructor(url: string) {
        this.url = url;
        FakeES.instances.push(this);
    }
    addEventListener(name: string, fn: (ev: unknown) => void) {
        const l = this.listeners.get(name) ?? [];
        l.push(fn);
        this.listeners.set(name, l);
    }
    close() {
        this.closed = true;
        this.readyState = CLOSED;
    }
    /** Drive the states a real EventSource moves through. */
    emitOpen() {
        this.readyState = OPEN;
        for (const fn of this.listeners.get('open') ?? []) fn({});
    }
    emitError(nextState = CONNECTING) {
        this.readyState = nextState;
        for (const fn of this.listeners.get('error') ?? []) fn({});
    }
}

/** Set document.visibilityState WITHOUT dispatching visibilitychange. The O
 *  rules need the silent form: dispatching would run the visibility revive
 *  first and attribute its work to 'online' — the vacuous-fixture trap. */
function setVisibilityState(state: 'visible' | 'hidden') {
    Object.defineProperty(document, 'visibilityState', {
        configurable: true,
        get: () => state,
    });
}

function setVisibility(state: 'visible' | 'hidden') {
    setVisibilityState(state);
    document.dispatchEvent(new Event('visibilitychange'));
}

function goOnline() {
    window.dispatchEvent(new Event('online'));
}

afterEach(() => {
    FakeES.instances = [];
    vi.unstubAllGlobals();
    setVisibilityState('visible'); // a test that went 'hidden' must not poison the next
});

function start(wire: (src: EventSource) => void = () => {}) {
    FakeES.instances = [];
    vi.stubGlobal('EventSource', FakeES);
    return openReviving('/v1/test', wire);
}

const live = () => FakeES.instances[FakeES.instances.length - 1];

describe('openReviving', () => {
    // V1 — A DEAD STREAM COMES BACK. The whole point: no reload needed.
    it('V1: revives a CLOSED stream when the tab becomes visible', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CLOSED); // browser gave up

        setVisibility('visible');

        expect(FakeES.instances).toHaveLength(2);
        expect(FakeES.instances[1].url).toBe('/v1/test');
        close();
    });

    // V2 — A HEALTHY STREAM IS NEVER TOUCHED. Safety, not tidiness: tearing one
    // down starts the FT8 capture linger, and a failed reopen disarms TX
    // mid-run. This is the ONLY rule that rejects "always recreate".
    it('V2: leaves an OPEN stream alone', () => {
        const close = start();
        live().emitOpen();

        setVisibility('visible');
        setVisibility('hidden');
        setVisibility('visible');

        expect(FakeES.instances).toHaveLength(1);
        expect(FakeES.instances[0].closed).toBe(false);
        close();
    });

    // V3 — A FIRST CONNECT IS NOT MISTAKEN FOR A CORPSE. CONNECTING with no
    // error yet is the normal opening state; reviving it would restart the
    // stream on every early tab switch.
    it('V3: leaves a stream that is still making its first connection', () => {
        const close = start();
        expect(live().readyState).toBe(CONNECTING);

        setVisibility('visible');

        expect(FakeES.instances).toHaveLength(1);
        close();
    });

    // V4 — A STREAM STUCK RETRYING IS REVIVED. readyState never reaches CLOSED
    // here, so a CLOSED-only check would miss exactly the silent failure this
    // feature exists for.
    it('V4: revives a stream left CONNECTING after an error', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CONNECTING); // dropped, retrying, never recovers

        setVisibility('visible');

        expect(FakeES.instances).toHaveLength(2);
        close();
    });

    // V5 — GOING HIDDEN DOES NOTHING. The handler fires on both transitions, so
    // without the visible check a dead stream would be rebuilt while hidden —
    // straight back into whatever killed it.
    it('V5: does not revive when the tab becomes hidden', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CLOSED);

        setVisibility('hidden');

        expect(FakeES.instances).toHaveLength(1);
        close();
    });

    // V6 — TEARDOWN IS FINAL. A listener left on document would resurrect a
    // stream after its owner had gone: the map's teardown closes the stream on
    // route change, and a later tab switch must not reopen it.
    it('V6: after close, a visibility change revives nothing', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CLOSED);
        close();

        setVisibility('visible');

        expect(FakeES.instances).toHaveLength(1);
    });

    // V7 — THE REVIVED STREAM IS WIRED. A revival that does not reattach the
    // caller's listeners produces a live connection delivering nothing, which
    // looks identical to the bug being fixed.
    it('V7: reattaches the caller handlers to the new stream', () => {
        const wired: EventSource[] = [];
        const close = start((src) => wired.push(src));
        live().emitOpen();
        live().emitError(CLOSED);

        setVisibility('visible');

        expect(wired).toHaveLength(2);
        expect(wired[1]).toBe(FakeES.instances[1]);
        close();
    });

    // ------------------------------------------------------------------
    // O rules — the window 'online' trigger (acceptance criterion 2).
    // ------------------------------------------------------------------

    // O1 — THE ROUTER-SWAP CASE: a dead stream comes back when the OS reports
    // the network is back, with the tab visible the whole time — the state
    // visibilitychange can never reach. Visibility is set SILENTLY here:
    // dispatching visibilitychange would revive via the other trigger and
    // prove nothing about 'online'.
    it('O1: revives a CLOSED stream on online while the tab is visible', () => {
        const close = start();
        setVisibilityState('visible');
        live().emitOpen();
        live().emitError(CLOSED); // browser tore down on the network change

        goOnline();

        expect(FakeES.instances).toHaveLength(2);
        expect(FakeES.instances[1].url).toBe('/v1/test');
        close();
    });

    // O2 — A HEALTHY STREAM IS NEVER TOUCHED, even by REPEATED online events:
    // navigator.onLine is unreliable and spurious firings are normal. Same
    // safety rationale as V2 — tearing down /v1/ft8/events starts the capture
    // linger. This is the only O rule that rejects "always recreate on online".
    it('O2: leaves an OPEN stream alone on online', () => {
        const close = start();
        live().emitOpen();

        goOnline();
        goOnline();

        expect(FakeES.instances).toHaveLength(1);
        expect(FakeES.instances[0].closed).toBe(false);
        close();
    });

    // O3 — A FIRST CONNECT IS NOT A CORPSE (V3 for this trigger): a page can
    // load while the network is still settling, and 'online' firing during the
    // initial CONNECTING must not restart the stream.
    it('O3: leaves a stream still making its first connection on online', () => {
        const close = start();
        expect(live().readyState).toBe(CONNECTING);

        goOnline();

        expect(FakeES.instances).toHaveLength(1);
        close();
    });

    // O4 — A STREAM STUCK RETRYING IS REVIVED, and the OLD one is closed
    // FIRST: its browser retry timer is still running, and leaving it would
    // race the replacement (the same reasoning the visibility path records).
    it('O4: revives a stream left CONNECTING after an error on online', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CONNECTING); // dropped, retrying, never recovers

        goOnline();

        expect(FakeES.instances).toHaveLength(2);
        expect(FakeES.instances[0].closed).toBe(true);
        close();
    });

    // O5 — ONLINE WITH THE TAB HIDDEN REVIVES NOTHING (the judgement call
    // flagged in the header). Return-to-visible already revives, and a hidden
    // FT8 tab must not re-grab the audio capture device in the background.
    it('O5: does not revive on online while the tab is hidden', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CLOSED);
        setVisibility('hidden'); // dispatch is safe: hidden never revives (V5)

        goOnline();

        expect(FakeES.instances).toHaveLength(1);
        close();
    });

    // O6 — TEARDOWN IS FINAL for this trigger too. Like V6, two guards and
    // they are not equivalent: removing the window listener stops the leak;
    // src === null makes even a leaked listener revive nothing.
    it('O6: after close, online revives nothing', () => {
        const close = start();
        live().emitOpen();
        live().emitError(CLOSED);
        close();

        goOnline();

        expect(FakeES.instances).toHaveLength(1);
    });

    // O7 — THE REVIVED STREAM IS WIRED (V7 for this trigger). Guards an
    // implementation that recreates on 'online' through a second path that
    // skips the caller's WireFn.
    it('O7: reattaches the caller handlers to the stream online recreated', () => {
        const wired: EventSource[] = [];
        const close = start((src) => wired.push(src));
        live().emitOpen();
        live().emitError(CLOSED);

        goOnline();

        expect(wired).toHaveLength(2);
        expect(wired[1]).toBe(FakeES.instances[1]);
        close();
    });
});
