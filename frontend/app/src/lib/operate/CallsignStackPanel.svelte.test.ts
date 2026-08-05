/*
    The Phone/CW pile-up panel, and the FT8 drawer's absence from Phone/CW.

    ACCEPTANCE CRITERIA (continuing pileupKeys.svelte.test.ts's A1–A6):

      A7  SUPERSEDED BY A21 (operating, 2026-08-05). It read: "the list is not
          on screen when nothing is stacked, so there is no empty panel to
          open". That was written to keep FT8's trap from recurring — but the
          trap was `operate.pileup` gating the logging shortcuts in
          LoggingCard, not the empty panel, and hiding it when empty made the
          rail icon do nothing until you already knew the shortcut. The list is
          now governed by its toggle; see A21.
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
    // A21, replacing A7's rule. Presence follows the TOGGLE; an empty list is
    // still shown, because that is where the bindings are written down.
    it('P1: is on screen with nothing stacked, and says so', async () => {
        render(CallsignStackPanel);
        flushSync();

        const panel = await screen.findByLabelText('Pile-up');
        expect(panel.textContent).toContain('Nothing set aside');
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

    // Discard-all drops the CALLS, not the panel — the panel is the toggle's to
    // hide. It also stops offering itself once there is nothing left to discard.
    it('P5: discard-all empties the list and leaves the panel showing it is empty', async () => {
        render(CallsignStackPanel);
        callsignStack.push('G0ABC');
        flushSync();

        (await screen.findByLabelText('Discard all stacked callsigns')).click();
        flushSync();

        expect(callsignStack.items).toEqual([]);
        const panel = await screen.findByLabelText('Pile-up');
        expect(panel.textContent).toContain('Nothing set aside');
        expect(screen.queryByLabelText('Discard all stacked callsigns')).toBeNull();
    });
});

/*
    A21, from operating 2026-08-05 — the deploy that removed FT8's rail icon
    from Phone/CW left NO affordance there at all: with an empty stack there was
    nothing on screen to say the pile-up exists. v1 got away with that because it
    never had a rail icon; v2 did, so removing it read as a regression.

      A21 In Phone/CW the rail always shows a pile-up icon with the number of
          calls stacked, and clicking it opens or closes the list. Apart from a
          feature I can only find by already knowing the shortcut, and apart
          from closing the list losing track of what is in it.

    Safe to make it openable-while-empty because the trap was never the empty
    panel — it was `operate.pileup` gating the logging shortcuts inside
    LoggingCard, which is gone. This uses its OWN flag regardless, so toggling
    the Phone/CW list can never move FT8's drawer.
*/
describe('Phone/CW pile-up rail affordance', () => {
    it('P9: shows a pile-up icon in Phone/CW even with nothing stacked', async () => {
        render(UtilRail);
        flushSync();
        expect(await screen.findByTitle('Pile-up')).toBeTruthy();
    });

    it('P10: the icon toggles the list', async () => {
        render(UtilRail);
        render(CallsignStackPanel);
        callsignStack.push('G0ABC');
        flushSync();
        expect(screen.queryByLabelText('Pile-up')).not.toBeNull();

        (await screen.findByTitle('Pile-up')).click();
        flushSync();
        expect(screen.queryByLabelText('Pile-up')).toBeNull();

        (await screen.findByTitle('Pile-up')).click();
        flushSync();
        expect(screen.queryByLabelText('Pile-up')).not.toBeNull();
    });

    /*
        THE EMPTY CASE — the one A21 actually exists for, and the one P10 above
        does not reach because it pushes a call before clicking. With nothing
        stacked the icon toggled a flag and the panel stayed hidden, so clicking
        it did nothing at all: precisely the "findable only if you already know
        the shortcut" state the criterion was written against.

        An empty list is not chrome for its own sake here — it is where the
        bindings are written down. That is the whole payload.
    */
    it('P13: opens an empty list, showing how to fill it, and closes again', async () => {
        render(UtilRail);
        render(CallsignStackPanel);
        flushSync();
        expect(callsignStack.items).toEqual([]);

        // Default is open, so the first click CLOSES.
        (await screen.findByTitle('Pile-up')).click();
        flushSync();
        expect(screen.queryByLabelText('Pile-up')).toBeNull();

        (await screen.findByTitle('Pile-up')).click();
        flushSync();
        const panel = await screen.findByLabelText('Pile-up');
        expect(panel.textContent).toContain('Shift+Enter');
    });

    // Closing the list must not hide the FACT that calls are waiting — that is
    // what makes closing it safe mid-pile-up.
    it('P11: the icon carries the stacked count even while the list is closed', async () => {
        render(UtilRail);
        callsignStack.push('G0ABC');
        callsignStack.push('M0XYZ');
        flushSync();

        (await screen.findByTitle('Pile-up')).click(); // close it
        flushSync();

        expect((await screen.findByTitle('Pile-up')).textContent).toContain('2');
    });

    // No cross-talk: the two modes' toggles are separate pieces of state, so
    // closing one cannot open or close the other.
    it('P12: toggling the Phone/CW list leaves FT8s drawer state alone', async () => {
        render(UtilRail);
        flushSync();
        operate.pileup = true; // FT8's drawer, open

        (await screen.findByTitle('Pile-up')).click();
        flushSync();

        expect(operate.pileup).toBe(true);
    });
});

describe("FT8's pile-up drawer stays in FT8", () => {
    // A9. The drawer's own aria-label is "Pile-up" on an <aside>; the panel
    // above uses the same label on a <section>, so these assert on the RAIL
    // button and the drawer's toggle state, which only FT8 can reach.
    // P6 used to assert Phone/CW had NO rail button at all. A21 reversed that —
    // it now has its own. What must still hold is that the button there drives
    // the CALLSIGN STACK and never FT8's queue; P12 above pins that.

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
