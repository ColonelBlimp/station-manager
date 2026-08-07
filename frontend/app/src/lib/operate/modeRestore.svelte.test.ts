/*
    Operating-state restore across a Phone/CW ↔ FT8 switch (modeRestore.svelte).

    ACCEPTANCE CRITERIA — what the operator observes when this works. Drafted
    before the mechanism was chosen; the three judgement calls in it were the
    operator's (2026-08-05), not inferred here.

      A1  On Phone/CW at 14.255 USB, I switch to FT8 (which tunes 14.074
          DATA-U) and switch back: the rig returns to 14.255 USB. I can tell
          this apart from the pre-feature behaviour, where it stayed parked on
          the FT8 watering hole.
      A2  Symmetric: Phone → FT8 returns me to my last FT8 band, not to the
          Phone frequency and not to a default.
      A3  The first switch into a mode I have not used this session re-tunes
          NOTHING — the rig stays exactly where the other mode left it. Apart
          from "restored to a stale default", which would move it.
      A4  After a page reload the first switch re-tunes nothing either — a
          refresh must never move the rig to where I was an hour ago.
      A5  With the knob off and CAT live, a switch leaves the rig alone. Apart
          from the knob being ignored (rig moves), and from CAT being down.
      A6  With no rig connected, the DISPLAYED band/mode/frequency still
          restore and nothing is sent to a rig. Apart from nothing restoring.
      A7  Only values that actually differ are commanded — no set_mode goes out
          when only the frequency moved.
      A8  On a rig with no set_mode, the frequency still restores and no error
          is raised. Apart from the whole restore aborting.
      A9  While transmitting (FT8 TX armed, a contact in flight, or the tune
          carrier up) the switch is allowed but the rig is NOT re-tuned, and I
          am told why. Apart from the switch being blocked outright.
      A10 A restore that changes band re-arms the Set/Confirm gate, so I
          re-assert the band before logging against it.
      A11 A frequency key pressed straight after a switch steps from where I
          have just been returned to — not from the other mode's frequency.

      A25 (operator-directed 2026-08-06, after FT8 entry left the rig on
          14.225 USB) Switching to FT8 with no FT8 state to return to tunes
          the rig to the CURRENT band's configured FT8 frequency and asserts
          the data mode — I land ready to operate, not parked on the phone
          frequency. I can tell a SEED (the watering hole) from a RESTORE (my
          last FT8 dial): only the latter follows a previous FT8 visit. A
          seed the rig refused or TX blocked says so, and never lets the
          phone position masquerade as FT8's state — the next entry tries
          again, unless I really operated FT8 in between (then THAT state is
          kept). A band with no configured FT8 frequency stays the
          pre-feature no-op, silently — an unconfigured FT8 is a steady
          state, not a fault to nag about.
          AMENDS A3/A4: their "first switch re-tunes nothing" now covers the
          phone direction (no canonical home to establish) and an
          unconfigured FT8 only. The reload rationale survives — a seed goes
          to the band's configured home, never to a stale snapshot.
          CAT-off seeds nothing — RATIFIED (operator, 2026-08-06): "Cat-off -
          ft8 cannot work." There is nothing to establish for a mode that
          cannot operate; the FT8 rig panel's requiresCat encodes the same
          fact. Judgement calls still drafted, not operator-ratified: the
          same restore_rig_on_mode_switch knob gates the seed (it is a rig
          move on a mode switch); a rig with no set_freq seeds nothing at all
          — asserting a data mode on a phone frequency is worse than nothing.

    A26 (drafted 2026-08-07 from the dogfood report "changing from ft8 to
        phone/cw the mode and freq are not reset"; judgement calls drafted,
        NOT yet operator-ratified — flagged in the session report): a page
        session that BEGINS on the FT8 view (reload or deep link — routine
        straight after a deploy) captures the rig's position as first
        reported after boot, and the first switch to Phone/CW returns the
        rig there. I can tell it from the old behaviour, where the rig
        silently stayed on the FT8 dial in a data mode: the dial and mode
        move back.
        AMENDS A4 for exactly this direction. A4's rationale — never move
        the rig to a STALE pre-reload position — survives: the captured
        point is where the rig physically was when THIS session started,
        not an hour-old snapshot. What A4 still forbids (and its rule still
        pins) is restoring a pre-reload SNAPSHOT.
        Confusable states, each pinned below:
        - a session that entered FT8 FROM phone: the REAL phone snapshot
          wins — the boot capture only ever fills the never-switched case;
        - the rig moved during FT8: the restore returns to the BOOT
          position, not a later mid-session report (later reports are FT8
          activity, exactly what the return must undo);
        - the rig already parked on the FT8 point at boot: the restore
          returns it there — indistinguishable from a no-op, which is the
          old behaviour, never worse;
        - no full report before the switch (CAT down, or dial-without-mode):
          nothing captured, nothing restores — the old behaviour.

    WHICH STEP "the switch" MEANS. A mode change is a sequence: the router
    updates, the view swaps, the rig is commanded, the rig confirms by push.
    The snapshot is taken at the FIRST step, before anything is commanded —
    reading it later would capture values the restore itself had begun to
    change. The distinction is load-bearing for A9: a refused re-tune still
    takes the outgoing snapshot, so the NEXT switch restores correctly. Rule
    R9c is the one that pins it, and it is the half a "no commands sent"
    assertion alone would miss.

    …and the last step of that sequence is where A12 lives: the rig's confirming
    push arrives AFTER the command is acknowledged, so between the two, rig.vfoA
    still reads the frequency the rig has been told to leave. Every rule below
    that survives a fast second switch depends on that gap being handled, not on
    it being rare.

    NOT ASSERTED HERE: that Back/Forward reaches this at all — that is the
    router's wiring, and it is pinned in router.svelte.test.ts where the
    popstate path lives.
*/

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    rig,
    rigGate,
    confirmRig,
    resetCatLink,
    setRigCaps,
    setCommandSender,
    nudgeFreqCoarse,
    catLink,
    setFt8Frequencies,
    setFt8Mode,
} from './rig.svelte';
import { ft8State, resetFt8ForTests } from './ft8.svelte';
import { toasts } from '../ui/toasts.svelte';
import {
    onOperatingModeChange,
    setRestoreOnModeSwitch,
    resetModeRestore,
    noteRigReport,
} from './modeRestore.svelte';

interface Sent {
    op: string;
    value?: string | number;
}

let sent: Sent[] = [];

/** Every op the FTdx10 exposes for this path. tx_on/tx_off are never on an
 *  `exposed` list (ADR 0026/0030) and are deliberately absent here too. */
const ALL_OPS = ['set_freq', 'set_freq_b', 'set_mode', 'set_band', 'swap_vfo', 'select_vfo'];

beforeEach(() => {
    vi.useFakeTimers();
    resetCatLink();
    resetFt8ForTests();
    resetModeRestore();
    sent = [];
    setCommandSender((op, value) => {
        sent.push({ op, value });
        return Promise.resolve({ ok: true, message: '' });
    });
    setRigCaps({ ops: [...ALL_OPS], tune: false, rigModes: [] });
    // The R rules run with an UNCONFIGURED FT8 (no watering holes) — the
    // no-freq seed path is a designed no-op, so their fixtures see exactly the
    // pre-A25 behaviour. The S rules configure FT8 per test via configureFt8().
    setFt8Frequencies({});
    setFt8Mode('');
});

/** The configured-FT8 boot the S rules run under. */
function configureFt8(): void {
    setFt8Frequencies({ '20m': 14_074_000, '40m': 7_074_000 });
    setFt8Mode('DATA-U');
}

afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
});

/** A live rig parked on the Phone/CW frequency the operator was working. */
function livePhone(): void {
    rig.cat = 'connected';
    rig.vfoA = 14_255_000;
    rig.vfoB = 7_100_000;
    rig.selectedVfo = 'A';
    rig.modeLiteral = 'USB';
    rig.band = '20m';
    rig.mode = 'USB';
    rig.freq = '14.255.000';
}

/** …and where FT8 leaves it after a band pick. */
function moveToFt8(): void {
    rig.vfoA = 14_074_000;
    rig.modeLiteral = 'DATA-U';
    rig.band = '20m';
    rig.mode = 'FT8';
    rig.freq = '14.074.000';
}

const freqsSent = (): (string | number | undefined)[] =>
    sent.filter((s) => s.op === 'set_freq').map((s) => s.value);
const modesSent = (): (string | number | undefined)[] =>
    sent.filter((s) => s.op === 'set_mode').map((s) => s.value);

describe('operating-state restore across a mode switch', () => {
    // A1
    it('R1: returns the rig to the Phone/CW frequency and mode after an FT8 excursion', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(modesSent()).toContain('USB');
    });

    // A2
    it('R2: returns the rig to the last FT8 frequency and mode on the way back in', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone');
        // The rig would now be confirming the Phone restore.
        rig.vfoA = 14_255_000;
        rig.modeLiteral = 'USB';
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(freqsSent()).toContain('14074000');
        expect(modesSent()).toContain('DATA-U');
    });

    // A3 — and A4, which is the same rule seen after a reload: the module
    // starts with no snapshots, exactly as resetModeRestore leaves it.
    // Since A25 this holds only because the harness leaves FT8 UNCONFIGURED
    // (no watering holes to seed); a configured boot seeds instead — S1. The
    // phone direction stays a no-op either way — S3b.
    it('R3: commands nothing on the first switch into a mode this session', async () => {
        livePhone();

        await onOperatingModeChange('phone', 'ft8');

        expect(sent).toEqual([]);
        // …and the rig is left where it was, not moved to a default.
        expect(rig.vfoA).toBe(14_255_000);
        expect(rig.modeLiteral).toBe('USB');
    });

    // A7. The mode is deliberately UNCHANGED while the frequency moves, so a
    // blanket re-send of the whole snapshot would show up as a set_mode.
    it('R4: commands only the values that actually differ', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        rig.vfoA = 14_074_000; // frequency moved…
        rig.modeLiteral = 'USB'; // …mode did not
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(modesSent()).toEqual([]);
    });

    // A8. The mode DOES differ here — so the only reason no set_mode goes out
    // is the missing capability, not R4's "nothing changed".
    it('R5: restores the frequency and stays silent on a rig with no set_mode', async () => {
        const err = vi.spyOn(toasts, 'error');
        setRigCaps({ ops: ['set_freq', 'set_freq_b'], tune: false, rigModes: [] });
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(modesSent()).toEqual([]);
        expect(err).not.toHaveBeenCalled();
    });

    // A5. Both the frequency and the mode differ, so an ignored knob moves the rig.
    it('R6: commands nothing at a live rig when the restore knob is off', async () => {
        setRestoreOnModeSwitch(false);
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
    });

    // A6
    it('R7: restores the displayed context with no rig connected, sending nothing', async () => {
        rig.cat = 'off';
        rig.band = '20m';
        rig.mode = 'USB';
        rig.freq = '14.255.000';
        await onOperatingModeChange('phone', 'ft8');
        rig.band = '40m';
        rig.mode = 'FT8';
        rig.freq = '7.074.000';
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
        expect(rig.band).toBe('20m');
        expect(rig.mode).toBe('USB');
        expect(rig.freq).toBe('14.255.000');
    });

    // A6's second half. The knob is about not commanding a RIG; with none
    // connected there is nothing to opt out of, so a knob that gated this
    // would leave the operator looking at the other mode's values.
    it('R8: restores the displayed context with the knob off too (it gates only the rig)', async () => {
        setRestoreOnModeSwitch(false);
        rig.cat = 'off';
        rig.band = '20m';
        rig.mode = 'USB';
        rig.freq = '14.255.000';
        await onOperatingModeChange('phone', 'ft8');
        rig.band = '40m';
        rig.mode = 'FT8';
        rig.freq = '7.074.000';

        await onOperatingModeChange('ft8', 'phone');

        expect(rig.band).toBe('20m');
        expect(rig.mode).toBe('USB');
        expect(rig.freq).toBe('14.255.000');
    });

    // A9
    it('R9a: commands nothing while FT8 TX is armed, and says why', async () => {
        const info = vi.spyOn(toasts, 'info');
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        ft8State.tx.armed = true;
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
        expect(info).toHaveBeenCalled();
    });

    // A9's load-bearing half, part one: refusing the RE-TUNE is not abandoning
    // the switch. The OUTGOING snapshot must still be taken, or the mode we
    // just left has nothing to return to.
    //
    // The operator hand-tunes back to Phone at step 4 — which is what someone
    // looking at the Phone view on an FT8 frequency actually does, and is what
    // makes the restore at step 5 command something rather than agree with the
    // rig by accident.
    it('R9c: still snapshots the outgoing mode when the re-tune is refused', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        ft8State.tx.armed = true;

        await onOperatingModeChange('ft8', 'phone'); // refused; rig stays on FT8
        ft8State.tx.armed = false;
        rig.vfoA = 14_255_000; // operator tunes back by hand
        rig.modeLiteral = 'USB';
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        // 14074000 is the FT8 state captured DURING the refused switch.
        expect(freqsSent()).toContain('14074000');
        expect(modesSent()).toContain('DATA-U');
    });

    // …and part two, the state the refusal CREATES. After a refusal the rig is
    // still on the other mode's frequencies while the app shows the mode it
    // refused to reach. Leaving that mode again must NOT snapshot the rig,
    // because those are not that mode's operating values — they are the ones
    // the refusal was protecting the operator from losing.
    //
    // Without the guard, step 4 overwrites the Phone snapshot with 14.074
    // DATA-U, and step 5 then commands nothing at all (the rig already agrees).
    it('R9d: does not overwrite the snapshot of a mode it never returned the rig to', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        ft8State.tx.armed = true;

        await onOperatingModeChange('ft8', 'phone'); // refused; rig stays on FT8
        ft8State.tx.armed = false;
        await onOperatingModeChange('phone', 'ft8'); // rig already there: a no-op
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(modesSent()).toContain('USB');
    });

    it('R10: commands nothing while the tune carrier is up', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        rig.tuneActive = true;
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
    });

    it('R11: commands nothing while an FT8 contact is in flight', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        ft8State.qso.active = true;
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
    });

    it('R11b: commands nothing while FT8 is actually transmitting', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        ft8State.tx.transmitting = true;
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
    });

    // A10. CAT off, so the gate is live: confirm on 40m, restore to 20m.
    it('R12: re-arms the Set/Confirm gate when the restore changes band', async () => {
        rig.cat = 'off';
        rig.band = '20m';
        rig.mode = 'USB';
        rig.freq = '14.255.000';
        await onOperatingModeChange('phone', 'ft8');
        rig.band = '40m';
        rig.freq = '7.074.000';
        confirmRig();
        expect(rigGate()).toBe('manual'); // confirmed HERE, before the restore

        await onOperatingModeChange('ft8', 'phone');

        expect(rig.band).toBe('20m');
        expect(rigGate()).toBe('unconfirmed');
    });

    /*
        A12–A14, added after clean-room review c52e9b80 filed two P1s.

        A12  Clicking Phone then FT8 faster than the rig can answer leaves the
             rig in the mode I ended on, with both modes' frequencies still
             remembered correctly. Apart from the rig ending up on the mode I
             clicked THROUGH, and from a snapshot silently taking the other
             mode's frequency as its own.
        A13  When the rig rejects a restore command, the rest of the restore is
             abandoned rather than half-applied. Apart from a rig sitting in the
             new mode on the old frequency — which is what "keep going" produces,
             and is the shape that puts a data mode on a phone frequency.
        A14  A rejected restore tells me, and does not cost me the frequency it
             failed to return to.

        WHY THESE READ THE RIG STATE. The commanded values are reflected into
        rig.* on success and rolled back on rejection — the idiom setMode and
        swapVfoLive already use for SPA-issued commands (rig.svelte:297–330).
        Confirm-by-push is the rule for DAEMON-owned state (tune, TX alarms),
        not for commands this SPA issued and has an answer for. Without it the
        next snapshot reads a frequency the rig has already been told to leave,
        which is P1 one's second half.
    */

    // A12. The gate holds every command pending so a second switch can arrive
    // mid-restore — the ordering is the assertion, not the values.
    it('R14: does not interleave two restores when a switch arrives mid-flight', async () => {
        const gate: (() => void)[] = [];
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return new Promise((res) => gate.push(() => res({ ok: true, message: '' })));
        });
        // Fixed number of turns, not "until the gate empties": a released
        // command's continuation queues the NEXT one a microtask later, so
        // stopping at the first empty gate strands the rest of the restore.
        const drain = async (): Promise<void> => {
            for (let i = 0; i < 40; i++) {
                if (gate.length > 0) gate.shift()?.();
                await Promise.resolve();
                await Promise.resolve();
            }
        };

        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        await drain();
        // FT8 on a DIFFERENT band from where Phone sat, so the two restores
        // cannot agree by accident and the last command names its own mode.
        rig.vfoA = 21_074_000;
        rig.modeLiteral = 'DATA-U';
        sent = [];

        const a = onOperatingModeChange('ft8', 'phone');
        await Promise.resolve();
        const b = onOperatingModeChange('phone', 'ft8');
        await drain();
        await a;
        await b;
        await drain();

        // The operator ended on FT8, so the LAST thing sent to the rig must be
        // FT8's frequency. Overlapping restores end the other way round: the
        // Phone run resumes after the FT8 one and leaves the rig on 14.255 —
        // the mode that was only ever clicked through.
        expect(freqsSent().at(-1)).toBe('21074000');
    });

    // A12's second half. Nothing has pushed since the restore was commanded, so
    // an implementation that re-reads the rig would snapshot Phone as 14.074 —
    // the frequency it has just told the rig to leave — and the final switch
    // would then have nothing to command.
    it('R15: snapshots a mode at the frequency just restored, not the one it left', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone'); // commands 14.255 USB
        await onOperatingModeChange('phone', 'ft8'); // snapshots Phone…
        sent = [];

        await onOperatingModeChange('ft8', 'phone'); // …and must return to it

        expect(freqsSent()).toContain('14255000');
    });

    // A13. set_freq is rejected, so the rig is still on the FT8 frequency —
    // sending set_mode now would put it in USB there.
    it('R16: abandons the rest of the restore when a command is rejected', async () => {
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                op === 'set_freq'
                    ? { ok: false, message: 'rig said no' }
                    : { ok: true, message: '' }
            );
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(modesSent()).toEqual([]);
        expect(sent.filter((s) => s.op === 'set_freq_b')).toEqual([]);
    });

    // The mode readout must follow the rig back, not sit on FT8 until the push
    // lands. This is rig.setMode's optimistic write, which the restore gets by
    // CALLING it — a hand-rolled driveRig('set_mode') would leave the readout
    // reading DATA-U/FT8 on a rig that has been returned to USB, and would drop
    // its rollback with it.
    it('R17: shows the restored mode as soon as the rig accepts it', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();

        await onOperatingModeChange('ft8', 'phone');

        expect(rig.modeLiteral).toBe('USB');
        expect(rig.mode).toBe('USB');
    });

    // A13. The rejected target must not become the nudge base either, or the
    // next frequency key steps from a frequency the rig never reached.
    it('R18: does not seed a rejected frequency as the nudge target', async () => {
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                op === 'set_freq'
                    ? { ok: false, message: 'rig said no' }
                    : { ok: true, message: '' }
            );
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone');
        sent = [];

        await nudgeFreqCoarse(1);

        // Steps from where the rig actually is (14.074), not the refused 14.255.
        expect(freqsSent()).toEqual(['14074100']);
    });

    // A14
    it('R19: reports a rejected restore', async () => {
        const err = vi.spyOn(toasts, 'error');
        setCommandSender(() => Promise.resolve({ ok: false, message: 'rig said no' }));
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();

        await onOperatingModeChange('ft8', 'phone');

        expect(err).toHaveBeenCalled();
    });

    // A14's second half. A failed restore leaves the rig on the other mode's
    // frequencies — the same state a TX refusal leaves — so the mode it failed
    // to reach needs the same snapshot protection (R9d).
    it('R20: protects the snapshot of a mode whose restore was rejected', async () => {
        let reject = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                reject ? { ok: false, message: 'rig said no' } : { ok: true, message: '' }
            );
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone'); // rejected; rig stays on FT8
        reject = false;
        await onOperatingModeChange('phone', 'ft8'); // rig already there
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
    });

    /*
        A15, from clean-room review c89db207 — a defect the A12 fix introduced.

        A15  A rig that confirms my mode change before it confirms my frequency
             change does not cost me the frequency. Apart from the rig later
             returning to the frequency it was on BEFORE the restore, which is
             the original A12 defect wearing a narrower window.

        Rig-state frames carry only what changed (every field of
        RigStatePayload is optional), so "the rig has reported" is not one fact
        but one PER FIELD. A mode confirmation says nothing about the dial. The
        restore's own sequence makes this routine rather than exotic: it sends
        set_freq then set_mode, and the two confirmations arrive independently.
    */
    it('R21: keeps the restored frequency when the rig confirms only the mode', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone'); // commands 14.255 + USB

        // The mode confirmation lands first; the dial has not been reported yet,
        // so rig.vfoA still reads the FT8 frequency.
        catLink.onRigState({ mode: 'USB' });
        expect(rig.vfoA).toBe(14_074_000);

        await onOperatingModeChange('phone', 'ft8');
        sent = [];
        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
    });

    // …and the other direction, so the invalidation is not simply switched off:
    // once the rig DOES report a frequency, that is the operator turning the
    // dial, and it must win over anything we commanded earlier.
    it('R22: adopts a frequency the rig reports after a restore', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone'); // commands 14.255

        catLink.onRigState({ vfoA: 14_260_000 }); // operator turns the dial

        await onOperatingModeChange('phone', 'ft8');
        sent = [];
        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14260000');
    });

    /*
        A16, from clean-room review b8d422c9 — a defect the A15 fix introduced.

        A16  The app never sends my rig to a frequency it was never on. Apart
             from it being sent to one I really did operate, which is the whole
             point of the feature.

        What we commanded and what we WANTED are different things: each command
        is capability-gated, so a rig that cannot set VFO B leaves that VFO
        wherever it was. Recording the wish as though it were the outcome makes
        the app believe the rig is somewhere it has never been — and the belief
        outlives the capability that suppressed the command, because rig
        capabilities are re-applied whenever the station context reloads.
    */
    it('R23: never adopts a value the rig was not actually commanded to', async () => {
        setRigCaps({ ops: ['set_freq', 'set_mode'], tune: false, rigModes: [] });
        livePhone(); // VFO B on 7.100
        await onOperatingModeChange('phone', 'ft8');

        // FT8 moves both VFOs and the mode.
        rig.vfoA = 14_074_000;
        rig.vfoB = 3_500_000;
        rig.modeLiteral = 'DATA-U';

        // VFO B cannot be commanded, so the rig stays on 3.500 — it never
        // reaches Phone's 7.100.
        await onOperatingModeChange('ft8', 'phone');
        await onOperatingModeChange('phone', 'ft8');

        // The rig definition is refreshed and VFO B becomes settable, exactly as
        // a station-context reload does.
        setRigCaps({ ops: [...ALL_OPS], tune: false, rigModes: [] });
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent.filter((s) => s.value === '7100000')).toEqual([]);
    });

    /*
        A17  If I turn the dial while the app is mid-restore, my dial wins.
             Apart from the app quietly keeping the frequency it was restoring
             to and putting the rig back there at the next switch.

        The restore is a sequence with the rig answering in between, so a report
        can land inside it. Recording "has the rig spoken?" AFTER the last
        command counts that report as one of our own confirmations and lets the
        optimistic value outlive the report that superseded it.
    */
    it('R24: lets a dial report that lands mid-restore win over the restore', async () => {
        // Only the FIRST command is held, so the restore is reliably suspended
        // at a known point; everything after it resolves at once.
        // A holder, not a bare `let`: TypeScript narrows a local assigned only
        // inside a callback to `null` at the call site.
        const held: { release: (() => void) | null } = { release: null };
        let holdNext = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            if (!holdNext) return Promise.resolve({ ok: true, message: '' });
            holdNext = false;
            return new Promise((res) => {
                held.release = () => res({ ok: true, message: '' });
            });
        });
        const settle = async (): Promise<void> => {
            for (let i = 0; i < 20; i++) await Promise.resolve();
        };

        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();

        const back = onOperatingModeChange('ft8', 'phone'); // commands 14.255…
        await settle();
        expect(held.release).not.toBeNull(); // suspended inside the restore
        catLink.onRigState({ vfoA: 14_260_000 }); // …operator turns the dial
        held.release?.();
        await back;

        await onOperatingModeChange('phone', 'ft8');
        sent = [];
        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14260000');
    });

    /*
        A18, from clean-room review e0dad4c7 — R24 was too narrow. It covered a
        report for the field whose command was already in flight, and left the
        commoner case untested: a report for a field whose command has not been
        SENT yet. One baseline taken before the whole restore mis-dates those:
        the report looks newer than the command that follows it, so the app
        records the reported value while the rig has been sent somewhere else.

        A18  Whatever the rig reports mid-restore, the app ends up believing the
             rig is where it was last COMMANDED — not where an earlier report
             found it. Apart from the next switch quietly sending the rig to
             that earlier reading.

        Baselines are therefore per field AND taken as each command goes out,
        which is the only moment that answers "has the rig spoken since THIS
        value was sent".
    */
    it('R25: dates each field against its own command, not the start of the restore', async () => {
        const held: { release: (() => void) | null } = { release: null };
        let holdNext = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            if (!holdNext) return Promise.resolve({ ok: true, message: '' });
            holdNext = false;
            return new Promise((res) => {
                held.release = () => res({ ok: true, message: '' });
            });
        });
        const settle = async (): Promise<void> => {
            for (let i = 0; i < 20; i++) await Promise.resolve();
        };

        livePhone(); // Phone VFO B on 7.100
        await onOperatingModeChange('phone', 'ft8');
        rig.vfoA = 14_074_000;
        rig.vfoB = 3_500_000;
        rig.modeLiteral = 'DATA-U';

        const back = onOperatingModeChange('ft8', 'phone');
        await settle();
        expect(held.release).not.toBeNull(); // suspended on the VFO-A command
        // A VFO-B report lands BEFORE the VFO-B command has been sent.
        catLink.onRigState({ vfoB: 3_600_000 });
        held.release?.();
        await back;

        // VFO B was then commanded to Phone's 7.100, so that is where the app
        // must believe it is — a round trip has to bring it back there.
        await onOperatingModeChange('phone', 'ft8');
        sent = [];
        await onOperatingModeChange('ft8', 'phone');

        const vfoB = sent.filter((s) => s.op === 'set_freq_b').map((s) => s.value);
        expect(vfoB).toContain('7100000');
        expect(vfoB).not.toContain('3600000');
    });

    // A18's other half — the comparison, not just the dating. A18 says each
    // field is settled against the rig's position AT ITS OWN COMMAND; a field
    // the rig has meanwhile arrived at by itself needs no command at all, and
    // sending one anyway is a CAT write the operator did not ask for (A7 at the
    // right moment).
    it('R26: skips a field the rig reaches on its own while an earlier command is pending', async () => {
        const held: { release: (() => void) | null } = { release: null };
        let holdNext = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            if (!holdNext) return Promise.resolve({ ok: true, message: '' });
            holdNext = false;
            return new Promise((res) => {
                held.release = () => res({ ok: true, message: '' });
            });
        });
        const settle = async (): Promise<void> => {
            for (let i = 0; i < 20; i++) await Promise.resolve();
        };

        livePhone(); // Phone VFO B on 7.100
        await onOperatingModeChange('phone', 'ft8');
        rig.vfoA = 14_074_000;
        rig.vfoB = 3_500_000;
        rig.modeLiteral = 'DATA-U';

        const back = onOperatingModeChange('ft8', 'phone');
        await settle();
        // VFO B arrives at Phone's value by itself, before its command is sent.
        catLink.onRigState({ vfoB: 7_100_000 });
        held.release?.();
        await back;

        expect(sent.filter((s) => s.op === 'set_freq_b')).toEqual([]);
    });

    // A19  A restore that fails partway still leaves the app knowing how far it
    //      got. Apart from it forgetting the commands that DID land and then
    //      reading the rig's stale report as current — at which point the next
    //      switch decides the rig is already where it needs to be and sends
    //      nothing, stranding it in the mode I have just left.
    it('R27: keeps what landed when a later command in the same restore fails', async () => {
        let failB = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                failB && op === 'set_freq_b'
                    ? { ok: false, message: 'rig said no' }
                    : { ok: true, message: '' }
            );
        });

        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        rig.vfoA = 14_074_000;
        rig.vfoB = 3_500_000;
        rig.modeLiteral = 'DATA-U';

        // VFO A lands on 14.255; VFO B is refused, abandoning the rest.
        await onOperatingModeChange('ft8', 'phone');
        expect(freqsSent()).toContain('14255000');

        failB = false;
        sent = [];
        await onOperatingModeChange('phone', 'ft8');

        // The rig really is on 14.255, so going back to FT8 must move it.
        expect(freqsSent()).toContain('14074000');
    });

    // A11. Without the seed the nudge would step from rig.vfoA, which the
    // restore has just superseded.
    it('R13: a frequency nudge straight after a restore steps from the restored frequency', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone');
        sent = [];

        await nudgeFreqCoarse(1); // +100 Hz, within the repeat window

        expect(freqsSent()).toEqual(['14255100']);
    });

    // ------------------------------------------------------------------
    // S rules — the A25 first-entry seed (configured FT8; see configureFt8).
    // ------------------------------------------------------------------

    // S1 — THE REPORTED CASE: phone on 20m, first FT8 entry, rig moves to the
    // band's configured FT8 dial and then the data mode — frequency FIRST,
    // matching ft8SelectBand's rationale (a refused mode write must not cost
    // the dial move; a data mode on a phone frequency is the bad outcome).
    it('S1: seeds the current band FT8 frequency and data mode on first entry', async () => {
        configureFt8();
        livePhone();

        await onOperatingModeChange('phone', 'ft8');

        expect(sent.map((s) => [s.op, s.value])).toEqual([
            ['set_freq', '14074000'],
            ['set_mode', 'DATA-U'],
        ]);
    });

    // S2 — A SEED IS NOT A RESTORE. Once FT8 has real state (the operator
    // nudged off the watering hole), coming back restores THAT dial; a seed
    // here would drag them back to 14.074 and erase the nudge.
    it('S2: restores the last FT8 dial instead of re-seeding the watering hole', async () => {
        configureFt8();
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        // The rig confirms the seed, then the operator nudges off the hole.
        catLink.onRigState({ vfoA: 14_075_500, mode: 'DATA-U' });
        await onOperatingModeChange('ft8', 'phone');
        catLink.onRigState({ vfoA: 14_255_000, mode: 'USB' });
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(freqsSent()).toContain('14075500');
        expect(freqsSent()).not.toContain('14074000');
    });

    // S3 — NO CONFIGURED FREQUENCY = THE PRE-A25 NO-OP, silently and stably:
    // no commands, no toast (an unconfigured FT8 is a steady state, and a
    // nag on every entry would train the operator to ignore toasts), and no
    // retry loop — the exit snapshot takes over exactly as before A25.
    it('S3: stays the silent pre-feature no-op when the band has no FT8 frequency', async () => {
        const info = vi.spyOn(toasts, 'info');
        const err = vi.spyOn(toasts, 'error');
        livePhone(); // harness leaves FT8 unconfigured
        await onOperatingModeChange('phone', 'ft8');
        await onOperatingModeChange('ft8', 'phone');
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(sent).toEqual([]);
        expect(info).not.toHaveBeenCalled();
        expect(err).not.toHaveBeenCalled();
    });

    // S3b — THE PHONE DIRECTION HAS NO SEED: there is no canonical phone home
    // to establish, configured FT8 or not. First entry to phone stays a no-op.
    // The rig sits OFF the watering hole (a hand-tuned 14.076) deliberately:
    // parked exactly on 14.074, a wrong implementation that seeds phone too
    // would command nothing and this fixture could not tell it from the rule.
    it('S3b: seeds nothing on the first switch into phone', async () => {
        configureFt8();
        livePhone();
        rig.vfoA = 14_076_000;
        rig.modeLiteral = 'DATA-U'; // the rig sits where FT8 left it

        await onOperatingModeChange('ft8', 'phone');

        expect(sent).toEqual([]);
    });

    // S4 — THE KNOB GATES THE SEED (A5's promise: knob off, CAT live, no
    // switch moves the rig — a seed is a rig move on a mode switch).
    it('S4: seeds nothing with the restore knob off', async () => {
        configureFt8();
        setRestoreOnModeSwitch(false);
        livePhone();

        await onOperatingModeChange('phone', 'ft8');

        expect(sent).toEqual([]);
    });

    // S5 — CAT OFF SEEDS NOTHING, because CAT-off FT8 cannot work at all
    // (operator, 2026-08-06) — there is no operating point to establish for a
    // mode that cannot operate. Same fact the FT8 rig panel's requiresCat
    // encodes.
    it('S5: seeds nothing without a live CAT link', async () => {
        configureFt8();
        livePhone();
        rig.cat = 'lost';

        await onOperatingModeChange('phone', 'ft8');

        expect(sent).toEqual([]);
    });

    // S6 — TX BLOCKS THE SEED AND SAYS SO, and the block does not poison:
    // FT8 never got its state, so the next entry tries again rather than
    // restoring the phone position the rig happened to sit on.
    it('S6: skips the seed while the tune carrier is up, then retries next entry', async () => {
        configureFt8();
        const info = vi.spyOn(toasts, 'info');
        livePhone();
        rig.tuneActive = true;
        await onOperatingModeChange('phone', 'ft8');
        expect(sent).toEqual([]);
        expect(info).toHaveBeenCalledOnce();

        rig.tuneActive = false;
        await onOperatingModeChange('ft8', 'phone');
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(freqsSent()).toContain('14074000');
    });

    // S7 — A REFUSED SEED IS REPORTED AND RETRIED. Same shape as S6 but the
    // rig said no: the operator is told (they asked for nothing and the rig
    // refused something they cannot see), and FT8 stays unestablished.
    it('S7: reports a refused seed and retries on the next entry', async () => {
        configureFt8();
        const err = vi.spyOn(toasts, 'error');
        let refuse = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                refuse && op === 'set_freq'
                    ? { ok: false, message: 'rig said no' }
                    : { ok: true, message: '' }
            );
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        expect(err).toHaveBeenCalledOnce();

        refuse = false;
        await onOperatingModeChange('ft8', 'phone');
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(freqsSent()).toContain('14074000');
    });

    // S8 — THE SEED RECORDS THE OUTCOME IT COMMANDED (the held-value rule the
    // restore already obeys). Switch back before the rig confirms: without a
    // hold, effective() still reads the phone frequency, the phone restore
    // sees "already there" and sends NOTHING — the operator lands in phone
    // view with the rig on its way to 14.074.
    it('S8: a fast switch-back still returns the rig to the phone frequency', async () => {
        configureFt8();
        livePhone();
        await onOperatingModeChange('phone', 'ft8'); // seed sent, NOT confirmed
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(modesSent()).toContain('USB');
    });

    // S9 — EMPTY ft8Mode MEANS "LEAVE THE MODE ALONE" (the configured
    // contract ft8SelectBand documents): the dial still seeds, no set_mode
    // goes out.
    it('S9: seeds the dial only when no FT8 mode literal is configured', async () => {
        setFt8Frequencies({ '20m': 14_074_000 });
        setFt8Mode('');
        livePhone();

        await onOperatingModeChange('phone', 'ft8');

        expect(sent.map((s) => [s.op, s.value])).toEqual([['set_freq', '14074000']]);
    });

    // S10 — REAL FT8 OPERATION DURING A FAILED-SEED VISIT IS KEPT. The
    // retry-next-entry rule (S6/S7) must not discard state the operator
    // established by hand after the refusal — the rig reporting is the
    // evidence that FT8 really was operated there.
    it('S10: keeps a hand-tuned FT8 state made after a refused seed', async () => {
        configureFt8();
        let refuse = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                refuse && op === 'set_freq'
                    ? { ok: false, message: 'rig said no' }
                    : { ok: true, message: '' }
            );
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8'); // seed refused
        catLink.onRigState({ vfoA: 14_076_000, mode: 'DATA-U' }); // operator tunes by hand
        refuse = false;
        await onOperatingModeChange('ft8', 'phone');
        catLink.onRigState({ vfoA: 14_255_000, mode: 'USB' });
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(freqsSent()).toContain('14076000');
        expect(freqsSent()).not.toContain('14074000');
    });

    // S11 — A RIG WITHOUT set_mode STILL GETS ITS DIAL SEEDED, silently (the
    // R5 tolerance, seed edition): the missing capability is not a fault.
    it('S11: seeds the dial and stays silent on a rig with no set_mode', async () => {
        configureFt8();
        const err = vi.spyOn(toasts, 'error');
        setRigCaps({ ops: ['set_freq', 'set_freq_b'], tune: false, rigModes: [] });
        livePhone();

        await onOperatingModeChange('phone', 'ft8');

        expect(sent.map((s) => [s.op, s.value])).toEqual([['set_freq', '14074000']]);
        expect(err).not.toHaveBeenCalled();
    });

    // S12 — A RIG WITHOUT set_freq SEEDS NOTHING AT ALL. Establishing FT8
    // starts at the dial; asserting a data mode on a phone frequency is the
    // exact outcome the freq-first ordering exists to prevent.
    it('S12: seeds nothing on a rig that cannot set frequency', async () => {
        configureFt8();
        setRigCaps({ ops: ['set_mode'], tune: false, rigModes: [] });
        livePhone();

        await onOperatingModeChange('phone', 'ft8');

        expect(sent).toEqual([]);
    });

    // S13 — THE SEED DRIVES THE SELECTED VFO (clean-room review c0df1c8a).
    // FT8 operates on the selected VFO — rig.band derives from it and
    // ft8SelectBand/setFreq route the dial move by it. A seed that always
    // sends set_freq under a B selection tunes the WRONG dial and then
    // asserts the data mode on the phone frequency still selected.
    it('S13: seeds the selected VFO, not always VFO A', async () => {
        configureFt8();
        livePhone();
        rig.selectedVfo = 'B';
        rig.vfoB = 14_255_000; // phone on 20m, operating VFO B
        rig.vfoA = 7_100_000;

        await onOperatingModeChange('phone', 'ft8');

        expect(sent.map((s) => [s.op, s.value])).toEqual([
            ['set_freq_b', '14074000'],
            ['set_mode', 'DATA-U'],
        ]);
    });

    // S13b — …and the no-dial-no-seed gate (S12) is the SELECTED VFO's
    // capability: a rig that can only set VFO A cannot establish FT8 while B
    // is selected, so nothing is sent — not a VFO-A tune the operator isn't on.
    it('S13b: seeds nothing when the selected VFO cannot be set', async () => {
        configureFt8();
        setRigCaps({ ops: ['set_freq', 'set_mode'], tune: false, rigModes: [] });
        livePhone();
        rig.selectedVfo = 'B';
        rig.vfoB = 14_255_000;
        rig.vfoA = 7_100_000;

        await onOperatingModeChange('phone', 'ft8');

        expect(sent).toEqual([]);
    });

    // S14 — A REFUSED MODE IS RETRIED even when the landed dial command
    // confirms before the operator leaves FT8 (clean-room review c0df1c8a).
    // The confirmation is the seed's own doing: counting it as "the operator
    // established FT8" keeps a snapshot of {FT8 dial, phone mode}, and every
    // later entry restores that instead of retrying the data mode — the
    // half-seeded state becomes permanent.
    it('S14: retries a refused data mode after its own dial confirmation', async () => {
        configureFt8();
        let refuse = true;
        setCommandSender((op, value) => {
            sent.push({ op, value });
            return Promise.resolve(
                refuse && op === 'set_mode'
                    ? { ok: false, message: 'rig said no' }
                    : { ok: true, message: '' }
            );
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8'); // dial lands, mode refused
        catLink.onRigState({ vfoA: 14_074_000 }); // the seed's OWN dial confirm
        refuse = false;
        await onOperatingModeChange('ft8', 'phone');
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        expect(modesSent()).toContain('DATA-U');
    });
});

describe('boot-into-FT8 phone fallback (A26)', () => {
    /** The session began ON the FT8 route: no switch has fired; the first rig
     *  report arrives with the app already in ft8 mode (main.ts passes
     *  router.mode to noteRigReport on every rig-state). */
    function bootIntoFt8AtPhonePosition(): void {
        livePhone(); // where the rig physically was when the session began
        noteRigReport('ft8');
    }

    it('MB1: the first switch to phone returns the rig to its boot position', async () => {
        bootIntoFt8AtPhonePosition();
        moveToFt8(); // FT8 activity moved the rig (band button / operator)
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(modesSent()).toContain('USB');
    });

    it('MB2: a REAL phone snapshot from a later entry wins over the boot capture', async () => {
        bootIntoFt8AtPhonePosition(); // boot position 14.255
        moveToFt8();
        await onOperatingModeChange('ft8', 'phone'); // restores boot position
        // The operator then works phone somewhere else. A rig REPORT, not a
        // direct field write: the restore's outstanding command holds until
        // the rig speaks (the per-field hold), and an unreported mutation is
        // exactly what effective() is designed to ignore.
        catLink.onRigState({ vfoA: 14_310_000, mode: 'USB' });
        await onOperatingModeChange('phone', 'ft8'); // real entry snapshots 14.310
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14310000');
        expect(freqsSent()).not.toContain('14255000'); // the boot capture must not resurface
    });

    it('MB3: booting into PHONE captures nothing — the FT8 seed still owns the first entry', async () => {
        configureFt8();
        livePhone();
        noteRigReport('phone'); // reports arriving while the app sits in phone mode
        sent = [];

        await onOperatingModeChange('phone', 'ft8');

        // A capture wrongly filed under FT8's slot would turn this into a
        // restore toward the phone position (nothing to send); the seed sends
        // the watering hole + data mode.
        expect(freqsSent()).toContain('14074000');
        expect(modesSent()).toContain('DATA-U');
    });

    it('MB4: the capture is the FIRST report — later reports are FT8 activity, not the home', async () => {
        bootIntoFt8AtPhonePosition(); // 14.255 USB
        // The operator tunes within FT8; more reports arrive.
        rig.vfoA = 14_074_000;
        rig.modeLiteral = 'DATA-U';
        noteRigReport('ft8'); // must NOT re-capture
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(freqsSent()).toContain('14255000');
        expect(freqsSent()).not.toContain('14074000');
    });

    it('MB5: a partial boot report (dial without mode) captures nothing', async () => {
        rig.cat = 'connected';
        rig.vfoA = 14_255_000; // dial known, mode never reported
        noteRigReport('ft8');
        moveToFt8();
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        // Restoring the dial while leaving DATA-U asserted would be half the
        // reported bug; with no full picture the old no-op is the honest state.
        expect(sent).toEqual([]);
    });
});

describe('selection restore (codex ec2fd42d P1)', () => {
    /*
        Until select_vfo existed (2026-08-07) the selection genuinely could not
        change during an excursion — the UI only swapped CONTENTS — so the
        restore never touched it. Now it can, and the stakes are more than the
        dot: set_mode (MD0) acts on the OPERATING VFO, so a restore that
        rewrites contents and asserts the mode while the wrong VFO is selected
        writes the mode onto the wrong VFO and leaves the rig operating — and
        logging — from the other one.

        SEL1 — the selection is restored, FIRST: it must precede the mode
               write (ordering is load-bearing, not cosmetic — MD0 targets
               whatever is selected when it lands).
        SEL2 — an unchanged selection sends nothing (no select chatter on
               every switch).
        SEL3 — a rig WITHOUT select_vfo ABANDONS the restore for a drifted
               selection (a foreign VS push, e.g. front-panel A/B): no swap
               (it would exchange the very contents the restore sets), and no
               continuing either — set_freq/set_mode past a wrong selection
               writes the mode onto the wrong VFO, the exact corruption SEL1
               orders against (codex 8092fa81 P1: the first shape of this
               rule skipped the selection but carried on). The abandon toast
               names the front panel as the fix; the unrestored protection
               makes the next switch retry once the operator has pressed A/B.
        SEL4 — a refused select abandons the rest, per the existing rule: the
               commands put the rig at one operating point together, and a
               mode asserted onto the wrong VFO is exactly the state the
               abandon exists to prevent.
    */
    it('SEL1: restores the selected VFO before the mode write', async () => {
        livePhone(); // phone on VFO-A
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        catLink.onRigState({ selectedVfo: 'B' }); // operator selects B mid-FT8
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        const ops = sent.map((s) => s.op);
        expect(sent).toContainEqual({ op: 'select_vfo', value: 'VFO-A' });
        expect(rig.selectedVfo).toBe('A');
        expect(ops.indexOf('select_vfo')).toBeLessThan(ops.indexOf('set_mode'));
    });

    it('SEL2: an unchanged selection sends no select command', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8(); // selection stays A throughout
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        expect(sent.map((s) => s.op)).not.toContain('select_vfo');
    });

    it('SEL3: a rig without select_vfo ABANDONS the restore for a drifted selection', async () => {
        setRigCaps({
            ops: ['set_freq', 'set_freq_b', 'set_mode', 'swap_vfo'],
            tune: false,
            rigModes: [],
        });
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        catLink.onRigState({ selectedVfo: 'B' }); // front-panel A/B, pushed
        sent = [];

        await onOperatingModeChange('ft8', 'phone');

        const ops = sent.map((s) => s.op);
        expect(ops).not.toContain('select_vfo');
        expect(ops).not.toContain('swap_vfo'); // the fallback would corrupt contents
        expect(ops).not.toContain('set_freq'); // …and carrying on past the wrong
        expect(ops).not.toContain('set_mode'); // selection writes the mode onto the
        expect(sent).toEqual([]); //              wrong VFO — abandon sends NOTHING
    });

    it('SEL4: a refused select abandons the rest of the restore', async () => {
        livePhone();
        await onOperatingModeChange('phone', 'ft8');
        moveToFt8();
        catLink.onRigState({ selectedVfo: 'B' });
        sent = [];
        setCommandSender((op, value) => {
            sent.push({ op, value });
            if (op === 'select_vfo') return Promise.resolve({ ok: false, message: 'refused' });
            return Promise.resolve({ ok: true, message: '' });
        });

        await onOperatingModeChange('ft8', 'phone');

        const ops = sent.map((s) => s.op);
        expect(ops).toContain('select_vfo');
        expect(ops).not.toContain('set_freq'); // nothing after the refusal —
        expect(ops).not.toContain('set_mode'); // MD0 would land on the wrong VFO
    });
});
