import { describe, it, expect, beforeEach } from 'vitest';
import { layout, resetToDefault, showTile, toggleTile, isVisible } from './layout.svelte';

// What survives of this suite after ADR 0058. The column model, the drag seam and the
// global pin were the tile layout; their tests went with them rather than being
// rewritten, because their subject no longer exists. Panel VISIBILITY is what was
// load-bearing underneath, and it is what the rail and the Tab-auto-show still drive.

beforeEach(() => {
    resetToDefault();
});

describe('layout — show / hide', () => {
    it('Default shows only the logging card', () => {
        expect(isVisible('logging')).toBe(true);
        expect(isVisible('worked')).toBe(false);
        expect(isVisible('session')).toBe(false);
        expect(isVisible('rig')).toBe(false);
    });

    it('showTile opens a hidden panel', () => {
        showTile('worked');
        expect(isVisible('worked')).toBe(true);
        expect(layout.hidden).not.toContain('worked');
    });

    it('showTile is idempotent', () => {
        showTile('worked');
        showTile('worked');
        expect(layout.hidden.filter((id) => id === 'worked')).toHaveLength(0);
        expect(isVisible('worked')).toBe(true);
    });

    it('toggleTile hides a visible panel and reopens it', () => {
        showTile('session');
        toggleTile('session');
        expect(isVisible('session')).toBe(false);
        toggleTile('session');
        expect(isVisible('session')).toBe(true);
    });
});

describe('layout — resetToDefault', () => {
    it('closes every info panel again', () => {
        showTile('worked');
        showTile('session');
        showTile('rig');

        resetToDefault();

        expect(isVisible('logging')).toBe(true);
        expect(isVisible('worked')).toBe(false);
        expect(isVisible('session')).toBe(false);
        expect(isVisible('rig')).toBe(false);
    });
});
