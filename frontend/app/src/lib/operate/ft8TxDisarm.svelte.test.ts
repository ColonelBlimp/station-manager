// Dial-disarm visibility (dogfood 2026-08-07): an arm-only safety disarm was
// invisible — smd.log said `ft8 tx: disarmed, cause:dial_moved`, the SPA showed
// only the armed chip flipping, and the operator had to ask what happened. The
// daemon now stamps `disarm_cause` on the ft8-tx frame; these rules pin what the
// SPA does with it.
//
// CRITERION (confusable-state form): when TX disarms underneath the operator, a
// notice names why — and it is distinguishable from the operator's OWN disarm
// (cause "operator": silent), from a replayed stale frame after a reload (no
// armed→disarmed edge was observed: silent), from an older daemon that sends no
// cause (silent — nothing truthful to say), and from a dial guard that ALSO
// ended an active session (the session-ended notice already explains the event:
// exactly ONE notice, not two).

import { describe, it, expect, beforeEach } from 'vitest';
import {
    ft8State,
    ft8Link,
    resetFt8ForTests,
    setFt8SessionEndedSink,
    setFt8TxDisarmedSink,
} from './ft8.svelte';

beforeEach(() => {
    resetFt8ForTests();
});

describe('TX disarm cause visibility', () => {
    it('S1: a safety disarm underneath the operator reaches the sink with its cause', () => {
        const seen: string[] = [];
        setFt8TxDisarmedSink((cause) => seen.push(cause));

        ft8Link.onTx({ armed: true });
        ft8Link.onTx({ armed: false, disarm_cause: 'dial_moved' });

        expect(seen).toEqual(['dial_moved']);
        expect(ft8State.tx.disarmCause).toBe('dial_moved');
    });

    it("S2: the operator's own disarm is silent — they pressed the button", () => {
        const seen: string[] = [];
        setFt8TxDisarmedSink((cause) => seen.push(cause));

        ft8Link.onTx({ armed: true });
        ft8Link.onTx({ armed: false, disarm_cause: 'operator' });

        expect(seen).toEqual([]);
    });

    it('S3: a replayed disarmed frame on reconnect is silent — no edge was observed', () => {
        const seen: string[] = [];
        setFt8TxDisarmedSink((cause) => seen.push(cause));

        // Fresh state starts disarmed; the hub replays the cached frame on
        // subscribe. armed:false → armed:false is not a disarm happening now.
        ft8Link.onTx({ armed: false, disarm_cause: 'dial_moved' });

        expect(seen).toEqual([]);
    });

    it('S4: one event, one notice — a dial move that ends a session suppresses the disarm notice, once', () => {
        const disarms: string[] = [];
        const ends: string[] = [];
        setFt8TxDisarmedSink((cause) => disarms.push(cause));
        setFt8SessionEndedSink((reason) => ends.push(reason));

        // The daemon's teardown order: terminal ft8-qso first (published under
        // the sequencer gate, inside the disarm), then the ft8-tx frame.
        ft8Link.onTx({ armed: true });
        ft8Link.onQso({ active: true, role: 'caller', their_call: 'K1ABC' });
        ft8Link.onQso({ active: false, end_reason: 'dial_moved' });
        ft8Link.onTx({ armed: false, disarm_cause: 'dial_moved' });

        expect(ends).toEqual(['dial_moved']);
        expect(disarms).toEqual([]);

        // The suppression is one-shot: a LATER arm-only dial disarm (no session
        // this time — the morning's actual incident) must announce itself.
        ft8Link.onTx({ armed: true });
        ft8Link.onTx({ armed: false, disarm_cause: 'dial_moved' });
        expect(disarms).toEqual(['dial_moved']);
    });

    it('S5: an older daemon sending no cause is silent — there is nothing truthful to say', () => {
        const seen: string[] = [];
        setFt8TxDisarmedSink((cause) => seen.push(cause));

        ft8Link.onTx({ armed: true });
        ft8Link.onTx({ armed: false });

        expect(seen).toEqual([]);
    });

    // The hub replays cached events tx-BEFORE-qso (internal/ft8/hub.go), so a
    // reconnect after an outage presents the S4 event in the OPPOSITE order to
    // live publication: the disarm edge arrives while the session still shows
    // active, and the terminal frame with the explanation follows (codex P2 on
    // a11079b8). A disarm edge with a locally-active session therefore HOLDS
    // its notice and lets the next qso frame decide.
    it('S6: replayed order — disarm frame first, then the terminal session frame: still one notice', () => {
        const disarms: string[] = [];
        const ends: string[] = [];
        setFt8TxDisarmedSink((cause) => disarms.push(cause));
        setFt8SessionEndedSink((reason) => ends.push(reason));

        ft8Link.onTx({ armed: true });
        ft8Link.onQso({ active: true, role: 'caller', their_call: 'K1ABC' });
        // Reconnect replay: cached ft8-tx first, cached terminal ft8-qso second.
        ft8Link.onTx({ armed: false, disarm_cause: 'dial_moved' });
        expect(disarms).toEqual([]);
        ft8Link.onQso({ active: false, end_reason: 'dial_moved' });

        expect(ends).toEqual(['dial_moved']);
        expect(disarms).toEqual([]);
    });

    // The hold must never LOSE the notice: kills the suppress-outright
    // alternative, which trades the double toast for a silent safety stop.
    // Passes against the pre-fix code too (which fires early) — its teeth are
    // the pairing with S6, which that code fails.
    it('S7: a held notice fires when the following session frame carries no reason', () => {
        const disarms: string[] = [];
        const ends: string[] = [];
        setFt8TxDisarmedSink((cause) => disarms.push(cause));
        setFt8SessionEndedSink((reason) => ends.push(reason));

        ft8Link.onTx({ armed: true });
        ft8Link.onQso({ active: true, role: 'caller', their_call: 'K1ABC' });
        // Replay after an outage in which the operator abandoned (no end_reason
        // on the cached terminal frame) and a dial nudge then disarmed TX.
        ft8Link.onTx({ armed: false, disarm_cause: 'dial_moved' });
        ft8Link.onQso({ active: false });

        expect(ends).toEqual([]);
        // The safety stop must not go unannounced.
        expect(disarms).toEqual(['dial_moved']);

        // And the hold is gone once used: a fresh arm + reasonless frame later
        // must not resurrect it.
        ft8Link.onTx({ armed: true });
        ft8Link.onQso({ active: false });
        expect(disarms).toEqual(['dial_moved']);
    });

    // The tx and qso replay caches are INDEPENDENT, so one outage can contain
    // two separate events: the session ended for one reason, and a later
    // arm-only disarm for another (re-arm, then cat drop — the newer tx frame
    // replaces the cached one). A held notice whose cause does not match the
    // terminal frame's reason is a DIFFERENT event, not the same-teardown
    // duplicate: both must be said (codex P2 on 3f4afcf3). Same-cause matching
    // stays the dedup key, mirroring the live-order suppression.
    it('S8: a held notice with a different cause than the session end announces both', () => {
        const disarms: string[] = [];
        const ends: string[] = [];
        setFt8TxDisarmedSink((cause) => disarms.push(cause));
        setFt8SessionEndedSink((reason) => ends.push(reason));

        ft8Link.onTx({ armed: true });
        ft8Link.onQso({ active: true, role: 'caller', their_call: 'K1ABC' });
        // Replay: the LATEST tx frame is a cat-lost disarm from after a re-arm;
        // the cached terminal frame is the earlier dial-moved session end.
        ft8Link.onTx({ armed: false, disarm_cause: 'cat_lost' });
        ft8Link.onQso({ active: false, end_reason: 'dial_moved' });

        expect(ends).toEqual(['dial_moved']);
        expect(disarms).toEqual(['cat_lost']);
    });
});
