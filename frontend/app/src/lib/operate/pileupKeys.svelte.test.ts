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

    WHY THE STACK IS SEPARATE FROM FT8's PILE-UP. They share a name and nothing
    else: FT8's queue carries grid/SNR/slot/parity and is drained by the
    sequencer, this one is a list of callsigns the operator heard. One
    abstraction over both would be the shape lessons-for-v2 warns about.

    NOT ASSERTED HERE: the stack's own contract (order, dedupe, the three pops)
    — that is `callsignStack.svelte.test.ts`, ported verbatim from v1 along with
    the module, and it passed unchanged.
*/

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import { callsignStack } from './callsignStack.svelte';
import { draft, clearDraft, setSubmit, submitState } from './qso.svelte';
import { rig, confirmRig, resetCatLink } from './rig.svelte';
import { operate } from './state.svelte';

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
});
