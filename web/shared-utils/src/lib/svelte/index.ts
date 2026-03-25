/**
 * Svelte-specific utilities for Station Manager applications.
 *
 * This module exports utilities that depend on Svelte 5 features
 * such as $state, context, and lifecycle functions.
 *
 * @module svelte
 */

export {
    setFocusContext,
    getFocusContext,
    type FocusContext,
    type FocusRefs,
} from './focus-context.svelte';

