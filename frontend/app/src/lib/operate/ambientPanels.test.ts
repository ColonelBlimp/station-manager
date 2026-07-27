/*
    AMBIENT PANELS specification, written before the implementation (2026-07-27).

    A panel toggled from the utility rail must appear in the SAME place whatever
    workspace you are in. Today it does not: FT8 renders Rig and Session as a fixed
    overlay anchored to the rail, while Phone/CW renders them as tiles inside the
    arrangeable board. Same icon, same card, same visibility state — different place,
    so the operator has to re-find the panel after switching mode.

    The line is NOT "phone versus FT8". It is what the panel is FOR:

      - WORKFLOW panels are contextual to the contact being entered right now.
        Logging is the anchor; Worked is about the callsign in the box and auto-shows
        on Tab. They belong beside the work, and stay where the workspace puts them.

      - AMBIENT panels are reference: what the radio is doing (Rig), what has been
        worked today (Session). Nothing about them is tied to the contact in progress.
        You glance at them. They get ONE home, in every workspace.

    Consistency here means "the same control puts its output in the same place", not
    "every workspace has the same layout" — the two workspaces have genuinely
    different spatial constraints and forcing identical layout would be the wrong
    kind of consistency. Overlap is accepted deliberately (operator, 2026-07-27):
    these panels already overlap content in FT8 and that has never been a problem.

    Rules:

      1. Rig and Session are AMBIENT: shown, they render in the ambient host in every
         workspace — never as board tiles.
      2. Logging and Worked are WORKFLOW: they stay in the board and are untouched by
         this change.
      3. The rail toggle is the single control. One click shows the panel; a second
         hides it; the card's own close does the same. Identical in every workspace.
      4. Visibility SURVIVES a workspace switch. A panel left open in FT8 is still
         open in Phone/CW, in the same place — switching mode is not a reason to
         change what the operator chose to look at.

      5. A layout PERSISTED before this split is migrated on load: ambient tiles are
         removed from the board's columns, keeping whatever visibility they had.
         Without it an operator with a pinned layout gets the panel in BOTH hosts —
         the two-homes bug this split exists to prevent, shipped to exactly the
         people who had bothered to arrange their workspace.

    Rule 4 is the one that makes rules 1-3 worth having: a single home is no use if
    changing workspace silently reshuffles what is on screen. Rule 5 is the state
    this change CREATED — stored layouts that predate it — and is the reason to look
    for such states rather than only at the new code path.
*/

import { describe, it, expect, beforeEach } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Operate from './Operate.svelte';
import { router } from '../router.svelte';
import {
    AMBIENT_TILES,
    WORKFLOW_TILES,
    isVisible,
    showTile,
    hideTile,
    resetToDefault,
    setLayoutPersistence,
    layout,
    type LayoutValue,
    type TileId,
} from './layout.svelte';

beforeEach(() => {
    resetToDefault();
    router.mode = 'phone';
});

/** The ambient host is one element; a panel is "in it" if it renders inside. */
function ambientHost(): HTMLElement | null {
    return document.querySelector('[data-ambient-host]');
}

function inAmbientHost(name: RegExp): boolean {
    const host = ambientHost();
    if (host === null) return false;
    return new RegExp(name).test(host.textContent ?? '');
}

describe('ambient panels have one home', () => {
    // The heart of it: the same panel, the same place, whichever workspace.
    it.each(['phone', 'ft8'] as const)('renders Rig in the ambient host in %s', (mode) => {
        router.mode = mode;
        showTile('rig');
        render(Operate);
        flushSync();

        expect(ambientHost()).not.toBeNull();
        expect(inAmbientHost(/Rig/)).toBe(true);
        // "In the ambient host" is only half the rule — it must not ALSO be a board
        // tile. Without this the panel can have two homes and every other assertion
        // here still passes, which is the exact bug the split exists to prevent.
        expect(layout.current.columns.flat()).not.toContain<TileId>('rig');
    });

    it.each(['phone', 'ft8'] as const)('renders Session in the ambient host in %s', (mode) => {
        router.mode = mode;
        showTile('session');
        render(Operate);
        flushSync();

        expect(inAmbientHost(/Session/)).toBe(true);
        expect(layout.current.columns.flat()).not.toContain<TileId>('session');
    });

    // Nothing open, nothing occupying the screen. Asserted as a TRANSITION so it
    // cannot pass merely because no host was ever built.
    it('shows no ambient host when nothing ambient is open', () => {
        showTile('rig');
        render(Operate);
        flushSync();
        expect(ambientHost()).not.toBeNull(); // a host exists while something is open

        hideTile('rig');
        flushSync();
        expect(ambientHost()).toBeNull();
    });
});

describe('workflow panels are untouched', () => {
    // Worked is about the callsign in the box, so it belongs beside the work — the
    // point of the ambient/workflow split is that it does NOT move.
    it('keeps Worked out of the ambient host in phone', () => {
        // Rig is open too, so the host EXISTS — otherwise "Worked is not in it"
        // would be true simply because there is nothing to be in.
        showTile('rig');
        showTile('worked');
        render(Operate);
        flushSync();

        expect(inAmbientHost(/Rig/)).toBe(true);
        expect(inAmbientHost(/Worked/)).toBe(false);
    });

    it('classifies the tiles by purpose, not by workspace', () => {
        expect(AMBIENT_TILES).toEqual(expect.arrayContaining<TileId>(['rig', 'session']));
        expect(WORKFLOW_TILES).toEqual(expect.arrayContaining<TileId>(['logging', 'worked']));
        // A tile is one or the other — a panel with two homes is the bug being fixed.
        for (const id of AMBIENT_TILES) {
            expect(WORKFLOW_TILES).not.toContain(id);
        }
    });
});

describe('one control, both workspaces', () => {
    it.each(['phone', 'ft8'] as const)('show then hide round-trips in %s', (mode) => {
        router.mode = mode;

        showTile('rig');
        render(Operate);
        flushSync();
        expect(inAmbientHost(/Rig/)).toBe(true);

        hideTile('rig');
        flushSync();
        expect(inAmbientHost(/Rig/)).toBe(false);
    });
});

describe('switching workspace does not reshuffle what is open', () => {
    // A single home is no use if changing mode silently changes what is on screen.
    it('carries an open ambient panel across a mode switch', () => {
        router.mode = 'ft8';
        showTile('session');
        render(Operate);
        flushSync();
        expect(inAmbientHost(/Session/)).toBe(true);

        router.mode = 'phone';
        flushSync();

        expect(isVisible('session')).toBe(true);
        expect(inAmbientHost(/Session/)).toBe(true);
    });

    it('carries a closed one across too', () => {
        router.mode = 'ft8';
        showTile('session'); // keeps a host on screen, so the Rig assertion is real
        hideTile('rig');
        render(Operate);
        flushSync();
        expect(inAmbientHost(/Session/)).toBe(true);

        router.mode = 'phone';
        flushSync();

        expect(isVisible('rig')).toBe(false);
        expect(inAmbientHost(/Rig/)).toBe(false);
        expect(inAmbientHost(/Session/)).toBe(true);
    });
});

describe('a layout saved before the split is migrated', () => {
    function loadSaved(saved: LayoutValue): void {
        setLayoutPersistence({ load: () => saved, save: () => {}, clear: () => {} });
    }

    it('takes ambient tiles out of the board columns but keeps them visible', () => {
        // What an operator who pinned a layout yesterday actually has stored.
        loadSaved({ columns: [['logging', 'rig'], ['session']], hidden: ['worked'] });

        const inColumns = layout.current.columns.flat();
        expect(inColumns).not.toContain('rig');
        expect(inColumns).not.toContain('session');
        expect(isVisible('rig')).toBe(true);
        expect(isVisible('session')).toBe(true);

        render(Operate);
        flushSync();
        expect(inAmbientHost(/Rig/)).toBe(true);
        expect(inAmbientHost(/Session/)).toBe(true);
    });

    it('leaves a hidden ambient tile hidden', () => {
        loadSaved({ columns: [['logging'], []], hidden: ['worked', 'rig', 'session'] });

        expect(isVisible('rig')).toBe(false);
        expect(isVisible('session')).toBe(false);
    });

    it('does not disturb the workflow tiles', () => {
        loadSaved({ columns: [['logging'], ['worked', 'rig']], hidden: [] });

        expect(layout.current.columns.flat()).toEqual(
            expect.arrayContaining<TileId>(['logging', 'worked'])
        );
        expect(isVisible('worked')).toBe(true);
    });
});
