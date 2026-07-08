<script lang="ts">
    /*
        Toast renderer — single-mount (App.svelte); the rest of the SPA
        pushes via lib/ui/toasts.svelte.ts.

        Design ADAPTED from Tailwind Plus overlays/notifications/01-simple
        (licensed reference — adapt to @theme tokens, never copy verbatim):
        bottom-centre panel column, leading level icon, message, discrete
        close button. THE place to iterate on the toast look.

        Accessibility: severity is conveyed three ways — icon colour, an
        sr-only severity prefix in the spoken text, and per-toast live-region
        role (alert for errors → assertive; status otherwise → polite).
        Stacking: oldest-at-top DOM order; with the bottom anchor a new toast
        appears nearest the screen edge and nudges older ones up — acceptable,
        simultaneous toasts are rare.
    */
    import { fade } from 'svelte/transition';
    import { dismissToast, toastsState, type ToastLevel } from './toasts.svelte';

    // sr-only prefix so severity survives without the colour palette.
    const levelLabel: Record<ToastLevel, string> = {
        info: 'Info: ',
        warn: 'Warning: ',
        error: 'Error: ',
    };
</script>

{#snippet levelIcon(level: ToastLevel)}
    <!-- Heroicons outline: check-circle / exclamation-triangle / x-circle. -->
    {#if level === 'info'}
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
            class="size-6 text-green-400"
        >
            <path
                d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"
                stroke-linecap="round"
                stroke-linejoin="round"
            />
        </svg>
    {:else if level === 'warn'}
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
            class="size-6 text-amber-400"
        >
            <path
                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"
                stroke-linecap="round"
                stroke-linejoin="round"
            />
        </svg>
    {:else}
        <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            aria-hidden="true"
            class="size-6 text-invalid"
        >
            <path
                d="m9.75 9.75 4.5 4.5m0-4.5-4.5 4.5M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"
                stroke-linecap="round"
                stroke-linejoin="round"
            />
        </svg>
    {/if}
{/snippet}

<!-- Global notification region: full-viewport, pointer-transparent (clicks
     pass through the empty space); each panel re-enables pointer events.
     Bottom-centre placement (operator choice 2026-07-08): unobtrusive, off
     the logging card and clear of the header chip. -->
<div class="pointer-events-none fixed inset-0 z-50 flex items-end px-4 py-6 sm:p-6">
    <div class="flex w-full flex-col items-center space-y-4">
        {#each toastsState.items as toast (toast.id)}
            <div
                class="pointer-events-auto w-full max-w-sm rounded-lg bg-surface shadow-lg outline-1 outline-line"
                role={toast.level === 'error' ? 'alert' : 'status'}
                transition:fade={{ duration: 150 }}
            >
                <div class="p-4">
                    <div class="flex items-start">
                        <div class="shrink-0">
                            {@render levelIcon(toast.level)}
                        </div>
                        <div class="ml-3 w-0 flex-1 pt-0.5">
                            <p class="text-sm font-medium text-ink">
                                <span class="sr-only">{levelLabel[toast.level]}</span
                                >{toast.message}
                            </p>
                        </div>
                        <div class="ml-4 flex shrink-0">
                            <button
                                type="button"
                                class="inline-flex cursor-pointer rounded-md text-muted hover:text-ink focus:outline-2 focus:outline-offset-2 focus:outline-focus"
                                onclick={() => dismissToast(toast.id)}
                            >
                                <span class="sr-only">Dismiss notification</span>
                                <svg
                                    viewBox="0 0 20 20"
                                    fill="currentColor"
                                    aria-hidden="true"
                                    class="size-5"
                                >
                                    <path
                                        d="M6.28 5.22a.75.75 0 0 0-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 1 0 1.06 1.06L10 11.06l3.72 3.72a.75.75 0 1 0 1.06-1.06L11.06 10l3.72-3.72a.75.75 0 0 0-1.06-1.06L10 8.94 6.28 5.22Z"
                                    />
                                </svg>
                            </button>
                        </div>
                    </div>
                </div>
            </div>
        {/each}
    </div>
</div>
