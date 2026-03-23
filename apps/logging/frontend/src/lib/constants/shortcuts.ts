/**
 * Centralized keyboard shortcut definitions.
 *
 * All keyboard shortcuts used in the application are defined here for:
 * - Easy discoverability
 * - Conflict detection
 * - Consistent documentation
 * - Single source of truth for shortcut mappings
 */

import type { ShortcutModifier, ShortcutEventDetail } from '@svelte-put/shortcut';

export interface ShortcutDefinition {
    /** Unique identifier for the shortcut */
    id: string;
    /** The key to trigger the shortcut (e.g., 'Escape', 'F5', 's') */
    key: string;
    /** Optional modifier key(s) */
    modifier?: ShortcutModifier | ShortcutModifier[];
    /** Human-readable description for tooltips and help */
    description: string;
    /** Display string for UI (e.g., button titles) */
    displayKey: string;
}

/**
 * Application keyboard shortcuts organized by feature area.
 */
export const SHORTCUTS = {
    // Form Controls
    CLEAR_FORM: {
        id: 'clear-form',
        key: 'Escape',
        description: 'Clear the QSO form and reset state',
        displayKey: 'ESC',
    },
    LOG_CONTACT: {
        id: 'log-contact',
        key: 's',
        modifier: 'ctrl' as ShortcutModifier,
        description: 'Log the current contact',
        displayKey: 'Ctrl+S',
    },

    // Timer Controls
    TOGGLE_TIMER: {
        id: 'toggle-timer',
        key: 'F5',
        description: 'Start or stop the QSO timer',
        displayKey: 'F5',
    },
} as const satisfies Record<string, ShortcutDefinition>;

/**
 * Type for shortcut keys
 */
export type ShortcutKey = keyof typeof SHORTCUTS;

/**
 * Get a shortcut definition by its key
 */
export function getShortcut(key: ShortcutKey): ShortcutDefinition {
    return SHORTCUTS[key];
}

/**
 * Get all shortcuts as an array (useful for help screens)
 */
export function getAllShortcuts(): ShortcutDefinition[] {
    return Object.values(SHORTCUTS);
}

/**
 * Callback type for shortcut handlers.
 * Simple handlers can ignore the detail parameter.
 */
export type ShortcutCallback = (detail: ShortcutEventDetail) => void;

/**
 * Helper to build trigger config for @svelte-put/shortcut
 */
export function buildTrigger(shortcutKey: ShortcutKey, callback: ShortcutCallback) {
    const def = SHORTCUTS[shortcutKey];
    const trigger: {
        key: string;
        modifier?: ShortcutModifier | ShortcutModifier[];
        callback: ShortcutCallback;
    } = {
        key: def.key,
        callback,
    };

    if ('modifier' in def) {
        trigger.modifier = def.modifier;
    }

    return trigger;
}
