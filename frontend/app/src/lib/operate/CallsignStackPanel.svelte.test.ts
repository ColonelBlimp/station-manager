/*
    The Phone/CW pile-up panel, and the FT8 drawer's absence from Phone/CW.

    ACCEPTANCE CRITERIA (continuing pileupKeys.svelte.test.ts's A1–A6):

      A7  The pile-up list is not on screen when nothing is stacked, so there is
          no empty panel to open. Apart from a toggleable drawer — which is what
          FT8 has, and what disabled the Phone/CW logging shortcuts when it was
          mounted there.
      A8  Clicking a stacked call loads it and takes it off the list; the small ×
          beside it drops the call WITHOUT loading it. Apart from the two doing
          the same thing, which would make one of them a trap.
      A9  In Phone/CW there is no FT8 pile-up drawer and no rail button for one.

    A7 and A9 are the trap fix expressed as render rules; the keyboard half is
    R10/R11 in pileupKeys.svelte.test.ts.
*/

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import CallsignStackPanel from './CallsignStackPanel.svelte';
import Operate from './Operate.svelte';
import UtilRail from './UtilRail.svelte';
import { callsignStack } from './callsignStack.svelte';
import { draft, clearDraft } from './qso.svelte';
import { router } from '../router.svelte';
import { operate } from './state.svelte';

beforeEach(() => {
    callsignStack.clear();
    clearDraft();
    operate.pileup = false;
    router.mode = 'phone';
    flushSync();
});

describe('Phone/CW pile-up panel', () => {
    // A7
    it('P1: is absent while nothing is stacked', () => {
        render(CallsignStackPanel);
        flushSync();
        expect(screen.queryByLabelText('Pile-up')).toBeNull();
    });

    it('P2: appears with the stacked calls once one is captured', async () => {
        render(CallsignStackPanel);
        callsignStack.push('G0ABC');
        callsignStack.push('M0XYZ');
        flushSync();

        const panel = await screen.findByLabelText('Pile-up');
        expect(panel.textContent).toContain('G0ABC');
        expect(panel.textContent).toContain('M0XYZ');
    });

    // A8 — click loads AND removes (the single-action contract).
    it('P3: clicking a call loads it into the draft and takes it off the list', async () => {
        render(CallsignStackPanel);
        callsignStack.push('G0ABC');
        callsignStack.push('M0XYZ');
        flushSync();

        (await screen.findByTitle(/^Load G0ABC/)).click();
        flushSync();

        expect(draft.callsign).toBe('G0ABC');
        expect(callsignStack.items).toEqual(['M0XYZ']);
    });

    // A8's other half. The × must NOT load — same fixture as P3, opposite
    // expectation on the draft, which is what makes the pair prove a difference.
    it('P4: the per-row × drops the call without loading it', async () => {
        render(CallsignStackPanel);
        callsignStack.push('G0ABC');
        callsignStack.push('M0XYZ');
        flushSync();

        (await screen.findByLabelText('Remove G0ABC from the pile-up')).click();
        flushSync();

        expect(callsignStack.items).toEqual(['M0XYZ']);
        expect(draft.callsign).toBe('');
    });

    it('P5: discard-all empties the list and takes the panel off screen', async () => {
        render(CallsignStackPanel);
        callsignStack.push('G0ABC');
        flushSync();

        (await screen.findByLabelText('Discard all stacked callsigns')).click();
        flushSync();

        expect(callsignStack.items).toEqual([]);
        expect(screen.queryByLabelText('Pile-up')).toBeNull();
    });
});

describe("FT8's pile-up drawer stays in FT8", () => {
    // A9. The drawer's own aria-label is "Pile-up" on an <aside>; the panel
    // above uses the same label on a <section>, so these assert on the RAIL
    // button and the drawer's toggle state, which only FT8 can reach.
    it('P6: Phone/CW offers no rail button for the FT8 pile-up', () => {
        render(UtilRail);
        flushSync();
        expect(screen.queryByTitle('Pile-up')).toBeNull();
    });

    it('P7: FT8 still offers it', async () => {
        router.mode = 'ft8';
        render(UtilRail);
        flushSync();
        expect(await screen.findByTitle('Pile-up')).toBeTruthy();
    });

    it('P8: Phone/CW renders no FT8 pile-up drawer', () => {
        render(Operate);
        flushSync();
        // The drawer marks itself open/closed with data-open; in Phone/CW it is
        // not in the tree at all, so nothing carries that attribute.
        expect(document.querySelector('aside[data-open]')).toBeNull();
    });
});
