// The Operate surface after the tile layout was retired (ADR 0058).
//
// ADR 0046 made Phone/CW a tiling board with an explicit arrange mode; 0058 removed
// it, on the evidence that three weeks of operating produced no arrangement friction,
// that the real complaint was CONSISTENCY between workspaces (fixed by giving Rig and
// Session one ambient home), and that with those two ambient the board was left
// arranging two tiles.
//
// These rules pin what must remain true afterwards. They are deliberately about the
// surface still WORKING, not about the board being gone: "no board" is satisfied by a
// blank page, and the risk in a deletion this size is taking something live with it.
// The ambient host's own rules live in ambientPanels.test.ts and must stay green.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Operate from './Operate.svelte';
import UtilRail from './UtilRail.svelte';
import { router } from '../router.svelte';
import { hideTile, showTile, isVisible } from './layout.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

describe('Operate surface without the tile board', () => {
    beforeEach(() => {
        router.mode = 'phone';
        // Start from the shipped default: logging visible, info panels closed.
        showTile('logging');
        for (const id of ['worked', 'session', 'rig'] as const) hideTile(id);
    });

    it('still renders the logging card in Phone/CW', () => {
        render(Operate);
        flushSync();

        // The fast-path anchor. If the deletion took the card with the board, this
        // is what notices.
        expect(document.querySelector('[data-card="logging"]')).not.toBeNull();
    });

    it('still shows and hides a workflow panel from the rail', async () => {
        render(Operate);
        flushSync();
        expect(document.querySelector('[data-card="worked"]')).toBeNull();

        showTile('worked');
        await flush();
        expect(document.querySelector('[data-card="worked"]')).not.toBeNull();
        expect(isVisible('worked')).toBe(true);

        hideTile('worked');
        await flush();
        expect(document.querySelector('[data-card="worked"]')).toBeNull();
    });

    // W1 — A FIXED-WIDTH WORKFLOW CARD CENTRES ITSELF. A fixed-width box in
    // Operate's flex column is placed at the START of the cross axis — hard
    // left — unless it carries its own auto margins. The ADR 0046 tile board
    // used to centre every card; ADR 0058 retired it and each card keeping a
    // fixed width had to inherit the mechanism itself. LoggingCard did
    // (its comment calls mx-auto load-bearing); WorkedPanel did not, and sat
    // off the logging card's axis (operator, 2026-08-06). jsdom does no
    // layout, so this pins the MECHANISM — every card root under a data-card
    // wrapper self-centres — and the geometric outcome (shared vertical axis)
    // belongs to the Playwright layer when it exists.
    it('W1: every workflow card root centres itself in the column', async () => {
        render(Operate);
        showTile('worked');
        await flush();

        for (const id of ['logging', 'worked'] as const) {
            const roots = document.querySelectorAll(`[data-card="${id}"] > .card`);
            expect(roots.length, `${id}: card root present`).toBeGreaterThan(0);
            for (const root of roots) {
                expect(root.classList.contains('mx-auto'), `${id} must self-centre`).toBe(true);
            }
        }
    });

    // W2 — THE CONTACT-DETAILS DISCLOSURE EXTENDS THE CARD WITHOUT REFLOW
    // (operator, 2026-08-06, three rounds: "could it open on-top of the
    // Worked Panel?"; rejecting a detached sheet — "a disclosure EXTENDS the
    // current panel - the Clear and Log QSO buttons should move down"; then
    // rejecting buttons-inside-the-box — "the disclosures' UI should remain
    // unchanged"). The open UI is the ORIGINAL one: content box under the
    // summary, action row below the box at card level, the card frame around
    // it all — the extension REPRODUCES the card's lower half out of flow.
    // The Worked panel does not move and is painted over ("it's almost
    // natural that it should" be obscured). z alone cannot deliver that:
    // reflow is a flow phenomenon, so the expansion must be OUT of flow
    // while the action row rides inside it. This rule pins the mechanism,
    // which survived the round-3 restyle unchanged; the styling that makes
    // it LOOK like the original card is jsdom-invisible and belongs to the
    // Playwright layer. The contract:
    //   - the CARD root carries relative + a z utility — the operator's
    //     "whole logging panel at a greater z", and what paints the
    //     extension over the Worked panel;
    //   - the DETAILS element is relative: the anchor that puts the
    //     expansion directly under the summary, where the old in-flow
    //     content opened;
    //   - the expansion div is absolute + top-full: out of flow, so opening
    //     cannot grow the card and push the column;
    //   - the action row MOVES DOWN with the content: while open, the
    //     in-flow row holds its space invisibly (the card must not shrink —
    //     that would move the Worked panel UP) and the single accessible
    //     Clear/Log row renders at the expansion's bottom.
    // jsdom does no layout: this pins the mechanism; the geometric outcome
    // is Playwright's when that layer exists.
    it('W2: the contact-details disclosure extends the card out of flow', async () => {
        render(Operate);
        await flush();

        const card = document.querySelector('[data-card="logging"] > .card');
        expect(card, 'logging card root present').not.toBeNull();
        expect(card!.classList.contains('relative'), 'card is a stacking anchor').toBe(true);
        expect(
            [...card!.classList].some((c) => /^z-\d+$/.test(c)),
            'card paints over the Worked panel'
        ).toBe(true);

        const details = document.querySelector<HTMLDetailsElement>('[data-card="logging"] details');
        expect(details, 'disclosure present').not.toBeNull();
        expect(details!.classList.contains('relative'), 'details anchors the expansion').toBe(true);

        const panel = details!.querySelector('div');
        expect(panel, 'expansion panel present').not.toBeNull();
        expect(panel!.classList.contains('absolute'), 'expansion out of flow').toBe(true);
        expect(panel!.classList.contains('top-full'), 'expansion under the summary').toBe(true);

        // Closed: ONE action row, in flow, visible. The expansion's copy must
        // not exist yet — jsdom applies no UA hiding to closed details, so a
        // permanently-rendered copy would double every "Log QSO" query.
        const inFlowRow = document.querySelector('[data-action-row]');
        expect(inFlowRow, 'in-flow action row present').not.toBeNull();
        expect(inFlowRow!.classList.contains('invisible')).toBe(false);
        expect(panel!.querySelector('[data-action-row]')).toBeNull();

        // Open: the in-flow row keeps its SPACE but goes invisible (the card
        // must not shrink), and the accessible row is the expansion's.
        details!.open = true;
        details!.dispatchEvent(new Event('toggle'));
        await flush();

        expect(
            inFlowRow!.classList.contains('invisible'),
            'in-flow row holds space invisibly while open'
        ).toBe(true);
        expect(
            panel!.querySelector('[data-action-row]'),
            'action row rides at the expansion bottom'
        ).not.toBeNull();
    });

    it('offers no arrange affordance in either workspace', () => {
        for (const mode of ['phone', 'ft8'] as const) {
            router.mode = mode;
            const { unmount } = render(UtilRail);
            flushSync();

            // Arrange mode existed only to drag tiles between columns. With no
            // columns the control has nothing to do, and a button that enters a mode
            // which no longer exists is worse than no button.
            expect(screen.queryByRole('button', { name: /Arrange/ })).toBeNull();
            unmount();
        }
    });
});
