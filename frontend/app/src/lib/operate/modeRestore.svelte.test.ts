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
} from './rig.svelte';
import { ft8State, resetFt8ForTests } from './ft8.svelte';
import { toasts } from '../ui/toasts.svelte';
import {
    onOperatingModeChange,
    setRestoreOnModeSwitch,
    resetModeRestore,
} from './modeRestore.svelte';

interface Sent {
    op: string;
    value?: string | number;
}

let sent: Sent[] = [];

/** Every op the FTdx10 exposes for this path. tx_on/tx_off are never on an
 *  `exposed` list (ADR 0026/0030) and are deliberately absent here too. */
const ALL_OPS = ['set_freq', 'set_freq_b', 'set_mode', 'set_band', 'swap_vfo'];

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
});

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
});
