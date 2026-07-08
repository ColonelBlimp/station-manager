import { describe, it, expect, beforeEach } from 'vitest';
import {
    layout,
    resetToDefault,
    showTile,
    toggleTile,
    isVisible,
    setColumnsLive,
    togglePin,
} from './layout.svelte';

// Each test starts from the built-in Default (and unpinned, so no persistence
// side-effects bleed across tests).
beforeEach(() => {
    if (layout.pinned) togglePin(); // unpin → resets to Default too
    resetToDefault();
});

describe('layout — show / hide', () => {
    it('Default shows only the logging tile', () => {
        expect(layout.current.columns).toEqual([['logging'], []]);
        expect(isVisible('logging')).toBe(true);
        expect(isVisible('worked')).toBe(false);
    });

    it('showTile stacks a hidden tile below the logging card (its column)', () => {
        showTile('worked');
        expect(isVisible('worked')).toBe(true);
        expect(layout.current.columns[0]).toEqual(['logging', 'worked']); // stacked below
        expect(layout.current.columns[1]).toEqual([]); // second column stays empty
        expect(layout.current.hidden).not.toContain('worked');
    });

    it('toggleTile hides a visible tile', () => {
        showTile('rig');
        toggleTile('rig');
        expect(isVisible('rig')).toBe(false);
        expect(layout.current.columns.flat()).not.toContain('rig');
    });
});

describe('layout — resetToDefault', () => {
    it('reverts a rearranged + shown layout back to Default', () => {
        showTile('worked');
        showTile('session');
        showTile('rig');
        // simulate a drag that reshuffled the columns
        setColumnsLive([
            ['worked', 'logging'],
            ['session', 'rig'],
        ]);
        expect(isVisible('worked')).toBe(true);

        resetToDefault();

        expect(layout.current.columns).toEqual([['logging'], []]);
        expect(isVisible('worked')).toBe(false);
        expect(isVisible('session')).toBe(false);
        expect(isVisible('rig')).toBe(false);
    });
});

describe('layout — global pin', () => {
    it('unpin reverts to Default; the current ref is replaced', () => {
        showTile('worked');
        togglePin(); // pin the worked-visible layout
        expect(layout.pinned).toBe(true);

        showTile('session'); // rearrange while pinned
        expect(isVisible('session')).toBe(true);

        togglePin(); // unpin → back to Default
        expect(layout.pinned).toBe(false);
        expect(layout.current.columns).toEqual([['logging'], []]);
        expect(isVisible('worked')).toBe(false);
    });
});
