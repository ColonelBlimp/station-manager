/*
    Phone / CW entry is locked while an archive switch is in flight (ADR 0085
    rule 2, kept by ADR 0087 item 7). The switch ends in a page reload, and
    the draft lives only in memory, so anything typed meanwhile would be lost.

      E1  While the injected entry gate names a reason, every field and button
          on the logging card is disabled and the reason shows in the card.
          Apart from a card that looks editable but whose typing the reload
          discards.
      E2  The card's shortcuts are inert while locked: Ctrl+Enter logs nothing,
          Escape clears nothing, Shift+Enter stacks nothing, Shift+↑ loads
          nothing, F3 starts no clock. Apart from a lock that only disables
          the mouse.
      E3  The pile-up panel cannot load a stacked call into the draft.
      E4  When the gate clears (a definite refusal), entry is editable again.
*/

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from './LoggingCard.svelte';
import CallsignStackPanel from './CallsignStackPanel.svelte';
import { callsignStack } from './callsignStack.svelte';
import {
    draft,
    clearDraft,
    entryLock,
    qsoClock,
    setEntryGate,
    setSubmit,
    submitState,
} from './qso.svelte';
import { rig, confirmRig, resetCatLink } from './rig.svelte';
import { operate } from './state.svelte';
import { sessionEdit } from './sessionEdit.svelte';

const REASON = 'An archive switch is in progress — wait for the daemon to restart.';
let lock = $state<string | null>(null); // reactive, as archivesState is
let logged: string[] = [];

beforeEach(() => {
    resetCatLink();
    rig.cat = 'off';
    confirmRig();
    clearDraft();
    callsignStack.clear();
    submitState.duplicate = false;
    operate.exportOpen = false;
    operate.callStack = true;
    sessionEdit.row = null;
    logged = [];
    setSubmit((q) => {
        logged.push(q.callsign);
        return Promise.resolve({ ok: true as const });
    });
    lock = REASON;
    setEntryGate(() => lock);
    flushSync();
});
afterEach(() => {
    setEntryGate(null);
    operate.callStack = false;
});

function key(init: KeyboardEventInit): void {
    window.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, ...init }));
    flushSync();
}

describe('Phone / CW entry lock during an archive switch', () => {
    it('entryLock reports the injected gate, and nothing when none is wired', () => {
        expect(entryLock()).toBe(REASON);
        setEntryGate(null);
        expect(entryLock()).toBeNull();
    });

    it('E1 fields and buttons are disabled and the reason is shown', () => {
        render(LoggingCard);
        expect(screen.getByLabelText('Callsign')).toBeDisabled();
        expect(screen.getByLabelText('RST Sent')).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Stack callsign' })).toBeDisabled();
        expect(screen.getByTestId('entry-lock')).toHaveTextContent(REASON);
    });

    it('E2 the shortcuts do nothing while locked', () => {
        render(LoggingCard);
        draft.callsign = 'G0ABC';
        draft.dateOn = '2026-09-30';
        draft.timeOn = '12:00';
        callsignStack.push('M0XYZ');
        flushSync();

        key({ key: 'Enter', ctrlKey: true });
        key({ key: 'Escape' });
        key({ key: 'Enter', shiftKey: true });
        key({ key: 'ArrowUp', shiftKey: true });
        key({ key: 'F3' });

        expect(logged).toEqual([]);
        expect(draft.callsign).toBe('G0ABC');
        expect(callsignStack.items).toEqual(['M0XYZ']);
        expect(qsoClock.started).toBe(false);
    });

    it('E3 the pile-up panel cannot load a call into the draft', async () => {
        callsignStack.push('M0XYZ');
        render(CallsignStackPanel);
        const load = screen.getByRole('button', { name: 'M0XYZ' });
        expect(load).toBeDisabled();
        await fireEvent.click(load);
        expect(draft.callsign).toBe('');
        expect(callsignStack.items).toEqual(['M0XYZ']);
    });

    it('E4 clearing the gate makes entry editable again', () => {
        render(LoggingCard);
        expect(screen.getByLabelText('Callsign')).toBeDisabled();
        lock = null;
        flushSync();
        expect(screen.getByLabelText('Callsign')).not.toBeDisabled();
        expect(screen.queryByTestId('entry-lock')).toBeNull();
        draft.callsign = 'G0ABC';
        key({ key: 'Escape' });
        expect(draft.callsign).toBe('');
    });
});
