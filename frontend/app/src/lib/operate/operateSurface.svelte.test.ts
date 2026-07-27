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
