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
            case 'info': return 'toast-info';
            case 'warn': return 'toast-warn';
            case 'error': return 'toast-error';
        }
    }
</script>

<!--
    Container is `fixed top-4 right-4`. `pointer-events-none` on the
    column lets clicks pass through the gaps between toasts; each toast
    re-enables pointer-events so click-to-dismiss still works.

    Stacking direction: `flex-col` (DOM order, oldest-at-top). New
    toasts append below the existing stack rather than displacing
    older ones; the queue grows downward from the top-right corner
    and shrinks back as toasts auto-dismiss. Picked over
    `flex-col-reverse` (which puts newest-at-top and shifts older
    entries down on each push) because no in-place movement is calmer
    when the operator's attention is on the QSO form.

    `aria-live="polite"` so screen readers announce new toasts without
    interrupting the operator's current focus context. role="status" is
    the right WAI-ARIA mapping for non-critical informational regions.
-->
<div
    class="fixed top-4 right-4 flex flex-col gap-2 z-50 pointer-events-none"
    aria-live="polite"
    role="status"
>
    {#each toastsState.items as toast (toast.id)}
        <button
            type="button"
            class="toast-base {levelClass(toast.level)} pointer-events-auto"
            onclick={() => dismissToast(toast.id)}
            transition:fade={{ duration: 150 }}
            aria-label={`Dismiss ${toast.level} notification`}
        >
            {toast.message}
        </button>
    {/each}
</div>
