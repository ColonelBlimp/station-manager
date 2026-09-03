/*
    Phone/CW pile-up keyboard — the capture-and-work-them-one-at-a-time flow,
    ported from the retired logging SPA (`QsoPanel.svelte` + `callsignStack`).

    ACCEPTANCE CRITERIA — what the operator observes when this works. The
    bindings are the ones their hands already know; the operator confirmed them
    against v1 rather than the pair they half-remembered (2026-08-05).

      A1  Working a pile-up, I type a call and press Shift+Enter: it is
          captured and the draft clears ready for the next one. Apart from it
          being LOGGED, and apart from the draft surviving.
      A2  Shift+↑ loads the NEWEST captured call into Callsign; Shift+↓ loads
          the OLDEST. With two or more stacked I can tell the two apart.
      A3  A call taken off the stack is gone from it — never in the field and
          the stack at once.
      A4  Typing the same call twice stacks it once.
      A5  Ctrl+Enter still logs and Esc still clears while calls are stacked.
          Apart from the shortcuts going dead, which is what the FT8 drawer did
          to Phone/CW before this — the trap that prompted the port.
      A6  Shift+↑/↓ inside a text field still selects text, and Ctrl+Shift+↑/↓
          still steps the rig frequency. Apart from either being swallowed.

      A27 (keyboard audit, 2026-08-06) While I edit a logged QSO from the
          Session panel, the keyboard belongs to the editor: Escape closes
          only the editor — the draft I was typing below SURVIVES — and
          Ctrl+Enter saves only the edit, logging nothing underneath; the
          pile-up and rig shortcuts are inert until the editor closes. Apart
          from the pre-fix behaviour: EditQsoModal registers its own window
          keydown, but window listeners do not shadow each other, so Escape
          also wiped the draft and Ctrl+Enter could LOG a half-typed QSO
          under the modal. The retired SPA's handleKeydown opened with
          `if (qsoEditState.open) return` — the port dropped that first
          guard, and this criterion restores it.

      A28 (F3, ported by operator direction 2026-08-06) F3 while the QSO
          timer runs FREEZES Time Off — the contact has ended even though I
          am still typing details — and I can tell it from the timer still
          running (Time Off stops advancing). With no QSO started and a call
          typed, F3 starts the clock exactly as Tab does. With no call, or
          once the off time is already held, F3 does nothing, silently —
          re-ticking would overwrite an end time I set by hand. Works
          wherever focus is. Judgement calls, drafted not ratified: the
          start gate is Tab's (a non-empty call), not the retired SPA's
          lookup-committed gate — lookups run automatically here, so that
          gate no longer exists to mirror.

      A29 (callsign Enter/Space, ported by operator direction 2026-08-06)
          Enter in the callsign field commits the call — the QSO clock
          starts and the worked lookup opens — without moving focus. Space
          NEVER types into the field (a callsign is a single token), and
          with a valid call typed it commits like Enter. A malformed call
          commits nothing from either key, and modified variants keep their
          window-level meanings (Shift+Enter stacks, Ctrl+Enter logs).
          Judgement call, drafted not ratified: Enter/Space require a VALID
          call (the retired SPA's gate); Tab keeps its shipped non-empty
          gate untouched.

    WHY THE STACK IS SEPARATE FROM FT8's PILE-UP. They share a name and nothing
    else: FT8's queue carries grid/SNR/slot/parity and is drained by the
    sequencer, this one is a list of callsigns the operator heard. One
    abstraction over both would be the shape lessons-for-v2 warns about.

    NOT ASSERTED HERE: the stack's own contract (order, dedupe, the three pops)
    — that is `callsignStack.svelte.test.ts`, ported verbatim from v1 along with
    the module, and it passed unchanged.

    F2 LOOKUP-ONLY "PEEK" — RESTORED (operator, 2026-08-18, W-0003), reversing the
    2026-08-06 "ruled moot". Enrichment auto-loads, but the worked-before panel only
    auto-opens on a HIT, so F2 gives the operator an explicit peek: reveal prior
    contacts (including the "checked, nothing" case) for a valid call WITHOUT starting
    the QSO clock — Tab is the commit signal, F2 is the peek. Still ruled moot:
    Cmd/metaKey variants of the log shortcut (a Linux station); do not port that
    without a new ruling.
*/

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import RigKeys from './RigKeys.svelte';
import { callsignStack } from './callsignStack.svelte';
import { commentHistory } from './commentHistory.svelte';
import { draft, clearDraft, setSubmit, submitState, qsoClock } from './qso.svelte';
import { rig, confirmRig, resetCatLink, setCommandSender, setRigCaps } from './rig.svelte';
import { operate } from './state.svelte';
import { sessionEdit } from './sessionEdit.svelte';
import { hideTile, isVisible } from './layout.svelte';
import type { LogbookQso } from '../api/logbooks';

let logged: string[] = [];

beforeEach(() => {
    resetCatLink();
    rig.cat = 'off';
    confirmRig();
    clearDraft();
    callsignStack.clear();
    operate.pileup = false;
    submitState.duplicate = false;
    operate.exportOpen = false;
    sessionEdit.row = null;
    logged = [];
    setSubmit((q) => {
        logged.push(q.callsign);
        return Promise.resolve({ ok: true as const });
    });
    flushSync();
});

/** A draft complete enough that Ctrl+Enter would really log it. */
function workableDraft(call: string): void {
    draft.callsign = call;
    draft.dateOn = '2026-08-05';
    draft.timeOn = '12:00';
    flushSync();
}

function key(init: KeyboardEventInit): void {
    window.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, ...init }));
    flushSync();
}

describe('Phone/CW pile-up keyboard', () => {
    // A1
    it('R1: Shift+Enter captures the typed call and clears the draft', () => {
        render(LoggingCard);
        workableDraft('G0ABC');

        key({ key: 'Enter', shiftKey: true });

        expect(callsignStack.items).toEqual(['G0ABC']);
        expect(draft.callsign).toBe('');
        // Captured, NOT logged — the whole point of setting one aside.
        expect(logged).toEqual([]);
    });

    // A2 + A3
    it('R2: Shift+ArrowUp loads the newest stacked call and removes it', () => {
        render(LoggingCard);
        callsignStack.push('G0ABC');
        callsignStack.push('M0XYZ'); // newest
        flushSync();

        key({ key: 'ArrowUp', shiftKey: true });

        expect(draft.callsign).toBe('M0XYZ');
        expect(callsignStack.items).toEqual(['G0ABC']);
    });

    // A2 + A3, the other end. Same fixture, opposite answer — which is what
    // makes the pair prove a direction rather than "a pop happened".
    it('R3: Shift+ArrowDown loads the oldest stacked call and removes it', () => {
        render(LoggingCard);
        callsignStack.push('G0ABC'); // oldest
        callsignStack.push('M0XYZ');
        flushSync();

        key({ key: 'ArrowDown', shiftKey: true });

        expect(draft.callsign).toBe('G0ABC');
        expect(callsignStack.items).toEqual(['M0XYZ']);
    });

    // A5 — the trap, stated as a rule. Calls ARE stacked here; before the port
    // the equivalent state (the FT8 drawer open) killed both shortcuts.
    it('R4: Ctrl+Enter still logs while calls are stacked', async () => {
        render(LoggingCard);
        callsignStack.push('M0XYZ');
        workableDraft('G0ABC');

        key({ key: 'Enter', ctrlKey: true });
        await vi.waitFor(() => expect(logged).toEqual(['G0ABC']));

        // …and the stack is untouched by logging.
        expect(callsignStack.items).toEqual(['M0XYZ']);
    });

    it('R5: Escape still clears the draft while calls are stacked', () => {
        render(LoggingCard);
        callsignStack.push('M0XYZ');
        workableDraft('G0ABC');

        key({ key: 'Escape' });

        expect(draft.callsign).toBe('');
        // Esc clears the DRAFT, not the capture — losing a pile-up to a
        // reflexive Esc would be worse than the trap it replaced.
        expect(callsignStack.items).toEqual(['M0XYZ']);
    });

    // A6. Shift+Arrow in a field is native select-to-line.
    it('R6: Shift+ArrowUp inside a text field does not pop the stack', () => {
        const { container } = render(LoggingCard);
        callsignStack.push('M0XYZ');
        flushSync();
        const input = container.querySelector('input');
        expect(input).not.toBeNull();
        input?.focus();

        input?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'ArrowUp', shiftKey: true, bubbles: true })
        );
        flushSync();

        expect(callsignStack.items).toEqual(['M0XYZ']);
        expect(draft.callsign).toBe('');
    });

    // A6. Ctrl+Shift+Arrow belongs to the rig freq-step family (RigKeys).
    it('R7: Ctrl+Shift+ArrowUp is left to the rig, not treated as a pop', () => {
        render(LoggingCard);
        callsignStack.push('M0XYZ');
        flushSync();

        key({ key: 'ArrowUp', shiftKey: true, ctrlKey: true });

        expect(callsignStack.items).toEqual(['M0XYZ']);
        expect(draft.callsign).toBe('');
    });

    /*
        THE TRAP ITSELF. R4/R5 above stack calls, which is not what disabled the
        shortcuts — `operate.pileup` was, and that flag belongs to FT8's drawer.
        It is view state that survives a mode switch, so Phone/CW could inherit
        it set with no drawer on screen to explain why logging had gone quiet.

        These two feed the flag directly, which is the only fixture where the
        guarded and unguarded code differ.
    */
    it('R10: Ctrl+Enter logs in Phone/CW even with the FT8 pile-up flag set', async () => {
        render(LoggingCard);
        operate.pileup = true; // as a switch back from FT8 can leave it
        workableDraft('G0ABC');

        key({ key: 'Enter', ctrlKey: true });

        await vi.waitFor(() => expect(logged).toEqual(['G0ABC']));
    });

    it('R11: Escape clears in Phone/CW even with the FT8 pile-up flag set', () => {
        render(LoggingCard);
        operate.pileup = true;
        workableDraft('G0ABC');

        key({ key: 'Escape' });

        expect(draft.callsign).toBe('');
    });

    /*
        A20, from clean-room review 70f3a178. Shift+Enter was captured at the
        WINDOW with no check on where the keystroke came from — so pressing it in
        Notes, where Shift+Enter is an ordinary newline, stacked the call and
        called clearDraft(), erasing every field of a QSO in progress.

        A20  Shift+Enter only sets a call aside when I am not typing into a
             field. In Notes it puts in a newline; in any other field it does
             nothing at all. Apart from it clearing the QSO I am part-way
             through — which is what it did, silently, from any control.

        Inherited from v1, whose comment reasoned that Shift+Enter has "no
        text-editing meaning, so it stays live even in a field". True of an
        <input>; false of a <textarea>, where it is a newline.
    */
    it('R12: Shift+Enter in the Notes textarea neither stacks nor clears the draft', () => {
        const { container } = render(LoggingCard);
        workableDraft('G0ABC');
        draft.notes = 'part-way through';
        flushSync();
        const notes = container.querySelector('#lc-notes');
        expect(notes).not.toBeNull();

        notes?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true })
        );
        flushSync();

        expect(callsignStack.items).toEqual([]);
        expect(draft.callsign).toBe('G0ABC');
        expect(draft.notes).toBe('part-way through');
    });

    it('R13: Shift+Enter in another entry field neither stacks nor clears', () => {
        const { container } = render(LoggingCard);
        workableDraft('G0ABC');
        draft.name = 'Marc';
        flushSync();
        const name = container.querySelector('#lc-name');
        expect(name).not.toBeNull();

        name?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true })
        );
        flushSync();

        expect(callsignStack.items).toEqual([]);
        expect(draft.name).toBe('Marc');
    });

    // …but the field the call is TYPED in is exactly where the operator presses
    // it, so that one must still work.
    it('R14: Shift+Enter in the callsign field still sets the call aside', () => {
        const { container } = render(LoggingCard);
        workableDraft('G0ABC');
        const call = container.querySelector('#lc-call');
        expect(call).not.toBeNull();

        call?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true, bubbles: true })
        );
        flushSync();

        expect(callsignStack.items).toEqual(['G0ABC']);
        expect(draft.callsign).toBe('');
    });

    // A1's guard: an empty or malformed field must not stack a junk entry.
    it('R8: Shift+Enter on an empty callsign stacks nothing', () => {
        render(LoggingCard);

        key({ key: 'Enter', shiftKey: true });

        expect(callsignStack.items).toEqual([]);
    });

    // A3's converse — popping an empty stack must not blank a call being typed.
    it('R9: a pop from an empty stack leaves the typed callsign alone', () => {
        render(LoggingCard);
        workableDraft('G0ABC');

        key({ key: 'ArrowUp', shiftKey: true });

        expect(draft.callsign).toBe('G0ABC');
    });

    // ------------------------------------------------------------------
    // K rules — A27: the session-edit modal owns the keyboard.
    // Each has a closed-modal control half, so a broken shortcut cannot
    // masquerade as a working guard.
    // ------------------------------------------------------------------

    // K1 — Escape under the open editor leaves the draft alone; after the
    // editor closes, the same key clears it again.
    it('K1: Escape does not clear the draft while the session editor is open', () => {
        render(LoggingCard);
        workableDraft('G0ABC');
        sessionEdit.row = { callsign: 'M0EDIT' } as unknown as LogbookQso;

        key({ key: 'Escape' });
        expect(draft.callsign).toBe('G0ABC');

        sessionEdit.row = null;
        key({ key: 'Escape' });
        expect(draft.callsign).toBe('');
    });

    // K2 — Ctrl+Enter under the open editor logs NOTHING; the draft is still
    // there and loggable once the editor closes.
    it('K2: Ctrl+Enter does not log the draft while the session editor is open', async () => {
        render(LoggingCard);
        workableDraft('G0ABC');
        sessionEdit.row = { callsign: 'M0EDIT' } as unknown as LogbookQso;

        key({ key: 'Enter', ctrlKey: true });
        await Promise.resolve();
        expect(logged).toEqual([]);

        sessionEdit.row = null;
        key({ key: 'Enter', ctrlKey: true });
        await vi.waitFor(() => expect(logged).toEqual(['G0ABC']));
    });

    // K3 — the pile-up keys are inert under the editor too: Shift+Enter must
    // not stack-and-wipe a draft the operator cannot see being destroyed.
    it('K3: Shift+Enter does not stack while the session editor is open', () => {
        render(LoggingCard);
        workableDraft('G0ABC');
        sessionEdit.row = { callsign: 'M0EDIT' } as unknown as LogbookQso;

        key({ key: 'Enter', shiftKey: true });

        expect(callsignStack.items).toEqual([]);
        expect(draft.callsign).toBe('G0ABC');
    });

    // ------------------------------------------------------------------
    // T rules — A28: the F3 timer toggle.
    // ------------------------------------------------------------------

    // T1 — F3 with a call typed and no QSO started starts the clock (Tab's
    // start half, focus-independent).
    it('T1: F3 starts the QSO clock for a typed call', () => {
        render(LoggingCard);
        workableDraft('G0ABC');
        expect(qsoClock.started).toBe(false);

        key({ key: 'F3' });

        expect(qsoClock.started).toBe(true);
        expect(qsoClock.ticking).toBe(true);
        expect(draft.timeOn).not.toBe('');
    });

    // T2 — F3 while ticking freezes Time Off: the QSO has ended. The frozen
    // value survives — nothing resumes ticking over it.
    it('T2: F3 while ticking holds the off time', () => {
        render(LoggingCard);
        workableDraft('G0ABC');
        key({ key: 'F3' }); // start
        const frozenOff = draft.timeOff;

        key({ key: 'F3' }); // stop

        expect(qsoClock.ticking).toBe(false);
        expect(qsoClock.started).toBe(true); // the QSO happened; TIME_ON stands
        expect(draft.timeOff).toBe(frozenOff);
    });

    // T3 — a third F3 is a SILENT no-op: re-ticking would overwrite an end
    // time the hold exists to protect.
    it('T3: F3 after a hold restarts nothing', () => {
        render(LoggingCard);
        workableDraft('G0ABC');
        key({ key: 'F3' });
        key({ key: 'F3' });

        key({ key: 'F3' });

        expect(qsoClock.ticking).toBe(false);
        expect(qsoClock.started).toBe(true);
    });

    // T5 — a HELD F3 is ONE press (clean-room review 6af12ca9): the key
    // auto-repeats, and without a repeat guard the first repeated keydown
    // lands while the clock is ticking and freezes Time Off milliseconds
    // after starting it — a slightly long press silently becomes
    // start-and-stop.
    it('T5: F3 auto-repeat does not toggle the timer it just started', () => {
        render(LoggingCard);
        workableDraft('G0ABC');

        key({ key: 'F3' });
        key({ key: 'F3', repeat: true });
        key({ key: 'F3', repeat: true });

        expect(qsoClock.ticking).toBe(true);
    });

    // T4 — no call, no clock: a timer with nobody on the other end is
    // meaningless (Tab's gate, mirrored).
    it('T4: F3 with an empty callsign starts nothing', () => {
        render(LoggingCard);

        key({ key: 'F3' });

        expect(qsoClock.started).toBe(false);
    });

    // ------------------------------------------------------------------
    // E rules — A29: Enter/Space in the callsign field.
    // ------------------------------------------------------------------

    /** Dispatch a key on the callsign input itself (callKeydown, not window). */
    function callFieldKey(init: KeyboardEventInit): boolean {
        const input = document.querySelector<HTMLInputElement>('#lc-call')!;
        const ev = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init });
        const notPrevented = input.dispatchEvent(ev);
        flushSync();
        return notPrevented;
    }

    // E1 — Enter commits a valid call: the clock starts, as Tab does.
    it('E1: Enter in the callsign field commits a valid call', () => {
        render(LoggingCard);
        workableDraft('G0ABC');

        callFieldKey({ key: 'Enter' });

        expect(qsoClock.started).toBe(true);
    });

    // E2 — a malformed call commits nothing (pure letters carry no digit and
    // the validator rejects them).
    it('E2: Enter on a malformed call starts no QSO', () => {
        render(LoggingCard);
        workableDraft('ABCDEF');

        callFieldKey({ key: 'Enter' });

        expect(qsoClock.started).toBe(false);
    });

    // E3 — Space with a valid call commits AND is swallowed: a callsign is a
    // single token, so the literal space must never reach the field.
    it('E3: Space commits a valid call and never types', () => {
        render(LoggingCard);
        workableDraft('G0ABC');

        const notPrevented = callFieldKey({ key: ' ', code: 'Space' });

        expect(notPrevented).toBe(false); // preventDefault fired
        expect(qsoClock.started).toBe(true);
    });

    // E4 — Space is swallowed even mid-edit of an invalid call, and commits
    // nothing: the swallow is about the field's shape, not the call's state.
    it('E4: Space on a malformed call is swallowed and commits nothing', () => {
        render(LoggingCard);
        workableDraft('ABCDEF');

        const notPrevented = callFieldKey({ key: ' ', code: 'Space' });

        expect(notPrevented).toBe(false);
        expect(qsoClock.started).toBe(false);
    });

    // E5 — modified variants pass through untouched: Shift+Enter from the
    // field must still reach the window-level stack handler (R14's path), so
    // the field handler may not commit on it. The clock alone cannot pin
    // this — the stack path's clearDraft resets it, hiding an illicit
    // commit (the first probe proved exactly that) — so the assertion is the
    // commit's surviving side effect: the worked panel auto-opening for a
    // call that was being STACKED, not worked.
    it('E5: modified Enter is left for the window-level shortcuts', () => {
        render(LoggingCard);
        hideTile('worked');
        workableDraft('G0ABC');

        callFieldKey({ key: 'Enter', shiftKey: true });

        expect(callsignStack.items).toEqual(['G0ABC']);
        expect(qsoClock.started).toBe(false);
        expect(isVisible('worked'), 'no commit happened on the way').toBe(false);
    });

    // K4 — the rig family stands down as well: Ctrl+Shift+Arrow while editing
    // must not detune a live rig under the modal. Control half: the same key
    // steps the frequency once the editor closes.
    it('K4: rig shortcuts do not drive the rig while the session editor is open', async () => {
        const sent: string[] = [];
        setCommandSender((op) => {
            sent.push(op);
            return Promise.resolve({ kind: 'accepted' });
        });
        setRigCaps({ ops: ['set_freq', 'set_freq_b'], tune: false, rigModes: [] });
        rig.cat = 'connected';
        rig.vfoA = 14_255_000;
        rig.selectedVfo = 'A';
        render(RigKeys);
        sessionEdit.row = { callsign: 'M0EDIT' } as unknown as LogbookQso;

        key({ code: 'ArrowUp', key: 'ArrowUp', ctrlKey: true, shiftKey: true });
        await Promise.resolve();
        expect(sent).toEqual([]);

        sessionEdit.row = null;
        key({ code: 'ArrowUp', key: 'ArrowUp', ctrlKey: true, shiftKey: true });
        await vi.waitFor(() => expect(sent).toEqual(['set_freq']));
    });
});

describe('F2 lookup-only peek + mouse stack icon (restored 2026-08-18, W-0003)', () => {
    it('F2 reveals the worked-before panel for a valid call WITHOUT starting the QSO clock', () => {
        render(LoggingCard);
        hideTile('worked');
        draft.callsign = 'G0ABC';
        flushSync();

        key({ key: 'F2' });

        expect(isVisible('worked')).toBe(true); // the peek is shown
        expect(qsoClock.started).toBe(false); // but it is NOT a commit — Tab does that
    });

    it('F2 is a silent no-op for an empty or malformed call', () => {
        render(LoggingCard);
        hideTile('worked');
        draft.callsign = 'X'; // too short to be a callsign
        flushSync();

        key({ key: 'F2' });

        expect(isVisible('worked')).toBe(false);
        expect(qsoClock.started).toBe(false);
    });

    it('the ≡ stack icon stacks the typed call — the mouse equivalent of Shift+Enter', async () => {
        render(LoggingCard);
        workableDraft('G0ABC');

        await fireEvent.click(screen.getByLabelText('Stack callsign'));

        expect(callsignStack.items).toEqual(['G0ABC']);
        expect(draft.callsign).toBe(''); // stacked and cleared, exactly like Shift+Enter
    });

    it('logging a QSO records its comment in the recent-comments list', async () => {
        localStorage.clear();
        commentHistory.clear();
        render(LoggingCard);
        workableDraft('G0ABC');
        draft.comment = 'Tnx QSO 73';
        flushSync();

        key({ key: 'Enter', ctrlKey: true }); // Ctrl+Enter logs

        await vi.waitFor(() => expect(commentHistory.items).toContain('Tnx QSO 73'));
    });
});
