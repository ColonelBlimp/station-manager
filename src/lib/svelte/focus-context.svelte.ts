/**
 * Focus Context for Cross-Component Focus Management in Svelte 5
 *
 * @module focus-context
 * @description
 * This module provides a Svelte context-based approach for managing focus
 * across components, replacing direct DOM manipulation via document.getElementById().
 *
 * The focus context system enables any component in the tree to:
 * - Register focusable elements (inputs, buttons, etc.)
 * - Request focus on any registered element from anywhere in the tree
 * - Optionally select the content when focusing
 *
 * This approach is preferred over direct DOM manipulation because:
 * - Type-safe: All focusable elements are defined in a typed interface
 * - Reactive: Uses Svelte 5's $state for reactive element references
 * - Decoupled: Components don't need to know DOM IDs or query the document
 * - Testable: Focus operations can be easily mocked and tested
 *
 * @example
 * // ======================================================
 * // SETUP: In root layout (+layout.svelte or App.svelte)
 * // ======================================================
 * <script lang="ts">
 *   import { setFocusContext } from '@station-manager/shared-utils/svelte';
 *
 *   // Initialize the focus context at the root of your app
 *   // This must be called during component initialization
 *   setFocusContext();
 * </script>
 *
 * <slot />
 *
 * @example
 * // ======================================================
 * // REGISTER: In component with focusable elements
 * // ======================================================
 * <script lang="ts">
 *   import { getFocusContext } from '@station-manager/shared-utils/svelte';
 *
 *   // Get the focus context (must be descendant of where setFocusContext was called)
 *   const focusContext = getFocusContext();
 * </script>
 *
 * <!-- Bind the element reference to the context's refs -->
 * <!-- The ref name must match a key in FocusRefs interface -->
 * <input
 *   type="text"
 *   bind:this={focusContext.refs.callsignInput}
 *   placeholder="Enter callsign"
 * />
 *
 * @example
 * // ======================================================
 * // FOCUS: In component that needs to trigger focus
 * // ======================================================
 * <script lang="ts">
 *   import { getFocusContext } from '@station-manager/shared-utils/svelte';
 *
 *   const focusContext = getFocusContext();
 *
 *   async function handleSubmit() {
 *     // ... do something
 *
 *     // Focus the callsign input after submission
 *     await focusContext.focus('callsignInput');
 *   }
 *
 *   async function handleReset() {
 *     // Focus and select all text in the input
 *     await focusContext.focus('callsignInput', true);
 *   }
 * </script>
 *
 * <button onclick={handleSubmit}>Submit</button>
 * <button onclick={handleReset}>Reset</button>
 *
 * @example
 * // ======================================================
 * // EXTEND: Adding new focusable elements
 * // ======================================================
 * // To add new focusable elements, modify the FocusRefs interface
 * // and the FocusRefsClass in this file:
 * //
 * // 1. Add to FocusRefs interface:
 * //    myNewInput: HTMLInputElement | null;
 * //
 * // 2. Add to FocusRefsClass:
 * //    myNewInput: HTMLInputElement | null = $state(null);
 * //
 * // 3. Use in your component:
 * //    <input bind:this={focusContext.refs.myNewInput} />
 * //
 * // 4. Focus from anywhere:
 * //    await focusContext.focus('myNewInput');
 *
 * @see https://svelte.dev/docs/svelte/context
 * @see https://svelte.dev/docs/svelte/$state
 */

import { getContext, setContext, tick } from 'svelte';

/**
 * Symbol key for the focus context.
 * Using a Symbol ensures the context key cannot conflict with other contexts.
 * @internal
 */
const FOCUS_CONTEXT_KEY = Symbol('focus-context');

/**
 * Interface defining all focusable element refs managed by the context.
 *
 * Add new focusable elements here as the application grows.
 * Each property should be typed as the specific HTML element type or null.
 *
 * @remarks
 * When adding new refs:
 * - Use descriptive names that indicate the element's purpose
 * - Group related elements with comments
 * - Always type as `ElementType | null` since elements may not be mounted
 *
 * @example
 * // Adding a new focusable element:
 * // 1. Add to this interface
 * searchInput: HTMLInputElement | null;
 *
 * // 2. Add to FocusRefsClass with $state
 * searchInput: HTMLInputElement | null = $state(null);
 */
export interface FocusRefs {
    // ──────────────────────────────────────────────────────────────
    // QSO Panel inputs
    // ──────────────────────────────────────────────────────────────
    /** Primary callsign input in the QSO logging panel */
    callsignInput: HTMLInputElement | null;
    /** Serial number received input */
    srxRcvdInput: HTMLInputElement | null;

    // ──────────────────────────────────────────────────────────────
    // Station Panel inputs
    // ──────────────────────────────────────────────────────────────
    /** Operator's callsign input in station settings */
    operatorCallsignInput: HTMLInputElement | null;

    // ──────────────────────────────────────────────────────────────
    // Info Panel inputs
    // ──────────────────────────────────────────────────────────────
    /** Email input for forwarding session configuration */
    fwdSessionEmailInput: HTMLInputElement | null;

    // ──────────────────────────────────────────────────────────────
    // Modal inputs
    // ──────────────────────────────────────────────────────────────
    /** Callsign input in the session edit modal */
    editCallsignInput: HTMLInputElement | null;
}

/**
 * Interface for the focus context including refs and helper methods.
 *
 * @remarks
 * The context provides:
 * - `refs`: Object containing all focusable element references
 * - `focus()`: Async method to focus and optionally select elements
 *
 * @example
 * const ctx = getFocusContext();
 *
 * // Access refs directly for binding
 * <input bind:this={ctx.refs.myInput} />
 *
 * // Use focus method for programmatic focus
 * await ctx.focus('myInput', true);
 */
export interface FocusContext {
    /**
     * Object containing all focusable element references.
     * Bind elements to these refs using Svelte's bind:this directive.
     */
    refs: FocusRefs;

    /**
     * Focus an element by its ref name, optionally selecting its content.
     *
     * This method uses `tick()` internally to ensure the DOM is updated
     * before attempting to focus, making it safe to call after state changes.
     *
     * @param refName - The name of the ref in FocusRefs to focus
     * @param select - Whether to also select the input content (default: false)
     * @returns Promise that resolves when focus is complete
     *
     * @example
     * // Simple focus
     * await focusContext.focus('callsignInput');
     *
     * // Focus and select all text
     * await focusContext.focus('callsignInput', true);
     *
     * // After a state update
     * showModal = true;
     * await focusContext.focus('editCallsignInput');
     */
    focus(refName: keyof FocusRefs, select?: boolean): Promise<void>;
}

/**
 * Internal class implementing FocusRefs with Svelte 5 $state reactivity.
 *
 * Using a class allows each property to be declared with $state(),
 * enabling Svelte's fine-grained reactivity for element references.
 *
 * @internal
 */
class FocusRefsClass implements FocusRefs {
    // QSO Panel inputs
    callsignInput: HTMLInputElement | null = $state(null);
    srxRcvdInput: HTMLInputElement | null = $state(null);

    // Station Panel inputs
    operatorCallsignInput: HTMLInputElement | null = $state(null);

    // Info Panel inputs and logbook forward by email
    fwdSessionEmailInput: HTMLInputElement | null = $state(null);

    // Modal inputs
    editCallsignInput: HTMLInputElement | null = $state(null);
}

/**
 * Creates and sets the focus context in Svelte's context.
 *
 * **This must be called in a parent/root component** (typically +layout.svelte
 * or App.svelte) during component initialization. All child components can
 * then access the context using `getFocusContext()`.
 *
 * @returns The created FocusContext instance
 *
 * @throws This function must be called during component initialization
 *
 * @example
 * // In +layout.svelte
 * <script lang="ts">
 *   import { setFocusContext } from '@station-manager/shared-utils/svelte';
 *
 *   // Initialize at the root
 *   const focusContext = setFocusContext();
 *
 *   // Optionally use the returned context directly in the layout
 *   focusContext.refs.someElement;
 * </script>
 *
 * <slot />
 *
 * @see getFocusContext
 */
export function setFocusContext(): FocusContext {
    const refs = new FocusRefsClass();

    const context: FocusContext = {
        refs,
        async focus(refName: keyof FocusRefs, select: boolean = false): Promise<void> {
            // Wait for any pending DOM updates to complete
            await tick();

            const element = refs[refName];
            if (element) {
                element.focus();
                if (select && 'select' in element) {
                    element.select();
                }
            }
        },
    };

    setContext(FOCUS_CONTEXT_KEY, context);
    return context;
}

/**
 * Gets the focus context from Svelte's context.
 *
 * **This must be called in a component that is a descendant of the component
 * where `setFocusContext()` was called.** Typically, `setFocusContext()` is
 * called in the root layout, so this will work in any page or component.
 *
 * @returns The FocusContext instance
 *
 * @throws Error if called outside of a component that has FocusContext in its tree
 *
 * @example
 * // In any child component
 * <script lang="ts">
 *   import { getFocusContext } from '@station-manager/shared-utils/svelte';
 *
 *   const focusContext = getFocusContext();
 *
 *   // Register a focusable element
 *   // <input bind:this={focusContext.refs.callsignInput} />
 *
 *   // Or focus an element
 *   async function handleClick() {
 *     await focusContext.focus('callsignInput', true);
 *   }
 * </script>
 *
 * @see setFocusContext
 */
export function getFocusContext(): FocusContext {
    const context = getContext<FocusContext>(FOCUS_CONTEXT_KEY);
    if (!context) {
        throw new Error(
            'FocusContext not found. Make sure setFocusContext() is called in a parent component.'
        );
    }
    return context;
}

