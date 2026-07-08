<script lang="ts">
    // Duplicate-refusal decision dialog — the one submit outcome that is a
    // stop-and-decide moment ("same station, same minute: log again or not?"),
    // so it gets a true modal with backdrop instead of a toast or popover
    // (operator decision 2026-07-08, replacing the anchored popover).
    //
    // Design ADAPTED from Tailwind Plus overlays/modal-dialogs (licensed
    // reference — adapt to @theme tokens, never copy verbatim). Driven purely
    // by submitState.duplicate; LoggingCard hosts it and owns the keyboard
    // handling (its window listener makes Esc dismiss THIS, not the draft).
    // Cancel is the focused-by-default safe action; a backdrop click cancels.
    // Known a11y limit, deliberate for now: no full focus trap — Tab can
    // reach the background, but the backdrop blocks pointer interaction.
    import { fade } from 'svelte/transition';
    import { submitState, logDraft, dismissDuplicate } from './qso.svelte';
    import { focusCallsign } from './state.svelte';
    import { rigReady, rigGate } from './rig.svelte';

    let cancelBtn: HTMLButtonElement | undefined;

    // Focus lands on the safe action when the dialog appears.
    $effect(() => {
        if (submitState.duplicate) cancelBtn?.focus();
    });

    function cancel(): void {
        dismissDuplicate();
        focusCallsign();
    }

    async function force(): Promise<void> {
        if (await logDraft(true)) focusCallsign();
    }

    // Same CAT gate as the primary Log button — a force must not slip past it.
    const gateTitle = $derived(
        rigGate() === 'lost'
            ? 'CAT link lost — rig context may be stale'
            : rigGate() === 'unconfirmed'
              ? 'Confirm band/mode/frequency in the Rig panel'
              : undefined
    );
</script>

{#if submitState.duplicate}
    <div
        class="fixed inset-0 z-50"
        role="dialog"
        aria-modal="true"
        aria-labelledby="dup-title"
        transition:fade={{ duration: 150 }}
    >
        <!-- Backdrop: click = the safe choice (cancel). -->
        <button
            type="button"
            class="absolute inset-0 cursor-default bg-gray-500/75 dark:bg-gray-900/50"
            aria-label="Dismiss"
            onclick={cancel}
        ></button>

        <div class="pointer-events-none relative flex min-h-full items-center justify-center p-4">
            <div
                class="pointer-events-auto w-full max-w-sm rounded-lg bg-surface p-6 shadow-xl outline-1 outline-line"
            >
                <div class="flex items-start gap-x-4">
                    <div
                        class="flex size-10 shrink-0 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-500/10"
                    >
                        <svg
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="1.5"
                            aria-hidden="true"
                            class="size-6 text-amber-500"
                        >
                            <path
                                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z"
                                stroke-linecap="round"
                                stroke-linejoin="round"
                            />
                        </svg>
                    </div>
                    <div>
                        <h3 id="dup-title" class="text-base font-semibold text-ink">
                            Duplicate QSO
                        </h3>
                        <p class="mt-1 text-sm text-muted">{submitState.error}</p>
                    </div>
                </div>
                <div class="mt-5 flex justify-end gap-x-2">
                    <button bind:this={cancelBtn} class="btn" onclick={cancel}>Cancel</button>
                    <button
                        class="btn btn-primary"
                        disabled={!rigReady() || submitState.busy}
                        title={gateTitle}
                        onclick={force}
                    >
                        Log anyway
                    </button>
                </div>
            </div>
        </div>
    </div>
{/if}
