<script lang="ts">
    // Continue's list, as a dialog over the workspace (W-0029 part 6, criteria
    // F; the operator's rulings 3 of 2026-11-25 and of 2026-11-27). A dialog
    // and not a route: the FT8 view stays mounted under it, and with it the
    // event stream that is the app's presence for a run.
    //
    // Pure presentation over continueList.svelte.ts, which holds every rule of
    // the list. What this adds is the words for each thing that can be so, and
    // they never run together: still being read; could not be read; read, and
    // empty. Only the last says there is no earlier session.
    //
    // A line is chosen without a further question (ruling 3). It can be
    // pressed only while the list is the one the workspace's state goes with
    // (continueList.held) and no control is out anywhere on the page
    // (session.control): the module and the recovery gate refuse it then too.
    //
    // While it is open it owns the window (codex-290). Three things make that so:
    //   - it is drawn at the end of <body>, outside the app it covers, above
    //     everything (the sidebar included), and the app's root is `inert`
    //     meanwhile: nothing under it takes a click or the focus;
    //   - it takes the focus when it opens, keeps Tab and Shift+Tab inside
    //     itself, and gives the focus back to where it was when it closes;
    //   - it hears every key FIRST (the capture phase, at the window) and lets
    //     none through to the handlers under it. Window listeners otherwise
    //     run in the order they were added: when this one ran before the
    //     logging card's, Escape closed the dialog and the card, finding no
    //     dialog open, cleared the contact being entered.
    //
    // One screen is never covered: the archive gate (ADR 0071), whose "Reload
    // now", "Stop tune" and "Disable FT8 TX" must be reachable the moment it
    // shows. It lives inside the app root this dialog makes inert and draws
    // over, so when the gate takes over the dialog closes at once, its focus
    // trap goes with it, and the focus is handed to the gate (codex-291).
    import { archivesState } from '../config/archives.svelte';
    import { tick } from 'svelte';
    import { fade } from 'svelte/transition';
    import {
        continueList,
        closeContinue,
        chooseContinue,
        continueSendable,
        showMoreContinue,
        retryContinue,
    } from './continueList.svelte';
    import { session } from './session.svelte';
    import { formatClosedSession } from '../utils/time';

    const choosable = $derived(
        continueList.held === '' && session.control === '' && continueSendable()
    );

    let panel: HTMLDivElement | undefined = $state();

    // Draw the dialog at the end of <body>: outside the app's root (which is
    // inert while it is open) and above every stacking context inside it.
    function overTheWindow(node: HTMLElement): { destroy: () => void } {
        document.body.appendChild(node);
        return { destroy: () => node.remove() };
    }

    $effect(() => {
        if (!continueList.open) return;
        const before =
            document.activeElement instanceof HTMLElement ? document.activeElement : null;
        const app = document.getElementById('app');
        app?.setAttribute('inert', '');
        void tick().then(() => panel?.focus());
        return () => {
            app?.removeAttribute('inert');
            // To where it was; or, with the archive gate up, to the gate.
            const gate = archivesState.switchUnresolved
                ? document.querySelector<HTMLElement>('[data-archive-gate-reload]')
                : null;
            (gate ?? before)?.focus();
        };
    });

    $effect(() => {
        if (archivesState.switchUnresolved && continueList.open) closeContinue();
    });

    const FOCUSABLE = 'button:not([disabled]):not([tabindex="-1"])';

    function onKeydown(e: KeyboardEvent): void {
        if (!continueList.open) return;
        // Nothing under the dialog hears a key while it is open.
        e.stopPropagation();
        if (e.key === 'Escape') {
            e.preventDefault();
            closeContinue();
            return;
        }
        if (e.key !== 'Tab' || panel === undefined) return;
        const inside = [...panel.querySelectorAll<HTMLElement>(FOCUSABLE)];
        const at = document.activeElement;
        const outside = !(at instanceof Node) || !panel.contains(at) || at === panel;
        if (inside.length === 0) {
            e.preventDefault();
            panel.focus();
        } else if (e.shiftKey && (outside || at === inside[0])) {
            e.preventDefault();
            inside[inside.length - 1].focus();
        } else if (!e.shiftKey && (outside || at === inside[inside.length - 1])) {
            e.preventDefault();
            inside[0].focus();
        }
    }
</script>

<svelte:window onkeydowncapture={onKeydown} />

{#if continueList.open}
    <div
        class="fixed inset-0 z-[100]"
        role="dialog"
        aria-modal="true"
        aria-labelledby="continue-title"
        use:overTheWindow
        transition:fade={{ duration: 150 }}
    >
        <button
            type="button"
            class="absolute inset-0 cursor-default bg-gray-500/75 dark:bg-gray-900/50"
            aria-label="Close"
            tabindex="-1"
            onclick={closeContinue}
        ></button>

        <div class="pointer-events-none relative flex min-h-full items-center justify-center p-4">
            <div
                bind:this={panel}
                tabindex="-1"
                class="pointer-events-auto w-full max-w-lg rounded-lg bg-surface p-6 shadow-xl outline-1 outline-line focus:outline-1"
            >
                <div class="flex items-start justify-between">
                    <h3 id="continue-title" class="text-base font-semibold text-ink">
                        Continue an earlier session
                    </h3>
                    <button
                        class="cursor-pointer rounded-md text-muted hover:text-ink"
                        aria-label="Close"
                        onclick={closeContinue}
                    >
                        <svg
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="currentColor"
                            stroke-width="1.5"
                            aria-hidden="true"
                            class="size-5"
                        >
                            <path
                                d="M6 18 18 6M6 6l12 12"
                                stroke-linecap="round"
                                stroke-linejoin="round"
                            />
                        </svg>
                    </button>
                </div>
                <p class="mt-1 text-sm text-muted">
                    Choosing a session reopens it. Any open session will close.
                </p>

                <div class="mt-4">
                    {#if continueList.lines.length === 0}
                        {#if continueList.reading}
                            <p class="text-sm text-muted">Reading the earlier sessions…</p>
                        {:else if continueList.error !== ''}
                            <p class="text-sm text-invalid" role="alert">
                                The earlier sessions could not be read. {continueList.error}
                            </p>
                            <button class="btn mt-2 text-xs" onclick={retryContinue}>
                                Try again
                            </button>
                        {:else if continueList.held === 'moved'}
                            <!-- An empty list that is known to be an old one is not
                                 "no earlier session" (codex-295). -->
                            <p class="text-sm text-warning" role="status">
                                The session has changed since this list was read.
                            </p>
                            <button class="btn mt-2 text-xs" onclick={retryContinue}>
                                Read it again
                            </button>
                        {:else}
                            <p class="text-sm text-muted">
                                There is no earlier session to continue.
                            </p>
                        {/if}
                    {:else}
                        <ul class="max-h-96 overflow-y-auto">
                            {#each continueList.lines as line (line.id)}
                                <li class="border-b border-line-soft last:border-0">
                                    <button
                                        type="button"
                                        class="w-full cursor-pointer px-2 py-1.5 text-left text-sm text-ink tabular-nums hover:bg-surface-raised disabled:cursor-not-allowed disabled:opacity-50"
                                        disabled={!choosable}
                                        onclick={() => void chooseContinue(line.id)}
                                    >
                                        {formatClosedSession(line)}
                                    </button>
                                </li>
                            {/each}
                        </ul>
                        {#if continueList.error !== ''}
                            <p class="mt-2 text-sm text-invalid" role="alert">
                                The earlier sessions could not be read again; this list may be out
                                of date. {continueList.error}
                            </p>
                            <button class="btn mt-2 text-xs" onclick={retryContinue}>
                                Try again
                            </button>
                        {:else if continueList.held === 'moved'}
                            <p class="mt-2 text-sm text-warning" role="status">
                                The session has changed since this list was read.
                            </p>
                            <button class="btn mt-2 text-xs" onclick={retryContinue}>
                                Read it again
                            </button>
                        {/if}
                        {#if continueList.more}
                            <button
                                class="btn mt-3 text-xs"
                                disabled={continueList.reading}
                                onclick={showMoreContinue}
                            >
                                Show more
                            </button>
                        {/if}
                    {/if}
                </div>
            </div>
        </div>
    </div>
{/if}
