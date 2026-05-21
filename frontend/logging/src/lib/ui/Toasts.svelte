<script lang="ts">
    /*
        Toast renderer — single-mount component (ADR 0008).

        Subscribes to `toastsState.items` and renders each item as a
        styled div in a fixed bottom-right column, stacking bottom-up.
        Mount once at the app shell (`app.svelte`); the imperative push
        API in `lib/states/toasts.svelte.ts` is what the rest of the SPA
        calls.

        Per ADR 0008:
          - Bottom-right keeps the top of the viewport clear for the
            QSO entry area where the operator's eyes live.
          - Click-to-dismiss is always available regardless of TTL.
          - Plain ~150 ms fade in/out, no choreography.
    */
    import { fade } from 'svelte/transition';
    import { dismissToast, toastsState } from '../states/toasts.svelte';

    function levelClass(level: 'info' | 'warn' | 'error'): string {
        switch (level) {
            case 'info':
                return 'toast-info';
            case 'warn':
                return 'toast-warn';
            case 'error':
                return 'toast-error';
        }
    }

    /*
        Severity prefix rendered as a bold inline element inside each
        toast. Two reasons it lives here rather than being baked into
        each call site's message string:

        - Single source of truth for the level → label mapping. Call
          sites pass plain operator-readable text; the prefix is
          derived.
        - Accessibility: `aria-live="polite"` reads the toast text;
          having "Error" / "Warning" in the spoken stream conveys
          severity to screen-reader and colour-blind operators
          without depending on the colour palette.
    */
    function levelLabel(level: 'info' | 'warn' | 'error'): string {
        switch (level) {
            case 'info':
                return 'Info: ';
            case 'warn':
                return 'Warning: ';
            case 'error':
                return 'Error: ';
        }
    }
</script>

<!--
    Container is `fixed top-1.25 left-1/2 -translate-x-1/2` —
    horizontally centred under the top edge of the viewport. Top-right
    placement (the previous default since session 30) was easy to miss
    because the operator's attention sits over the QSO form below;
    top-centre puts the toast directly in the visual scan path.
    `items-center` keeps successive toasts horizontally centred under
    each other rather than left-aligning behind the longest item.
    `pointer-events-none` on the column lets clicks pass through the
    gaps between toasts; each toast re-enables pointer-events so
    click-to-dismiss still works.

    Stacking direction: `flex-col` (DOM order, oldest-at-top). New
    toasts append below the existing stack rather than displacing
    older ones; the queue grows downward from the top centre and
    shrinks back as toasts auto-dismiss. Picked over `flex-col-reverse`
    (which puts newest-at-top and shifts older entries down on each
    push) because no in-place movement is calmer when the operator's
    attention is on the QSO form.

    Accessibility: the outer container is presentational — no aria-live,
    no role. Each toast carries its own live-region semantics via role
    (alert for errors → assertive announce; status for info/warn →
    polite announce). This avoids the "interactive children inside an
    aria-live region" anti-pattern (a child <button> in a live region
    has unspecified behaviour across NVDA / VoiceOver / Orca) and lets
    severity drive announce-urgency without a separate aria-live attr.
-->
<div
    class="fixed top-1.25 left-1/2 -translate-x-1/2 flex flex-col items-center gap-2 z-50 pointer-events-none"
>
    {#each toastsState.items as toast (toast.id)}
        <!--
            Toast itself is non-interactive — role=alert / role=status
            so the message text is the announced content (no aria-label
            override). The dismiss button is a sibling child with its
            own accessible name; screen readers announce the message
            first, then encounter the button as a discrete control.
        -->
        <div
            class="toast-base {levelClass(toast.level)} relative"
            role={toast.level === 'error' ? 'alert' : 'status'}
            transition:fade={{ duration: 150 }}
        >
            <span class="block pr-4">
                <strong>{levelLabel(toast.level)}</strong>{toast.message}
            </span>
            <button
                type="button"
                class="absolute top-0.5 right-1.5 text-current opacity-70 hover:opacity-100 cursor-pointer leading-none"
                aria-label="Dismiss notification"
                onclick={() => dismissToast(toast.id)}
            >
                <!--
                    × glyph as visual affordance. Text is hidden from
                    AT via aria-hidden because the aria-label on the
                    parent button already names the control.
                -->
                <span aria-hidden="true">×</span>
            </button>
        </div>
    {/each}
</div>
