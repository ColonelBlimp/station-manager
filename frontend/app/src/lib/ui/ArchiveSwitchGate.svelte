<script lang="ts">
    // The unproven-binding gate (ADR 0071, review P1: fail closed). Whenever the
    // SPA cannot prove which archive its stores belong to — after an activation
    // whose new generation was not seen, after a reconnect whose identity could
    // not be read, or after a boot whose identity bracket was incomplete — this
    // overlay blocks every operation until the page reloads: by the operator, or
    // by the store as soon as it can prove the daemon again. Above every route.
    import {
        archivesState,
        discardAndReload,
        reloadNow,
        retrySave,
    } from '../config/archives.svelte';
    import SavedDraftDetails from '../drafts/SavedDraftDetails.svelte';
    import { savedDraftHeadline } from '../drafts/savedDraft';
    import { armTx, ft8State } from '../operate/ft8.svelte';
    import { rig, toggleTune } from '../operate/rig.svelte';
    import { toasts } from '../ui/toasts.svelte';

    // The STOP paths stay reachable through the gate (review P1): an unproven
    // binding does not mean the daemon stopped — an FT8 run or a tune carrier can
    // still be keyed on the daemon — and the covered app is inert. Both stops
    // deliberately bypass the admission gates (a disarm or a tune stop is never
    // refused), and each is offered only while it can be acted on.
    let stopping = $state(false);
    // A stop whose confirmation never came (the request timed out and no push
    // matched within the grace) is UNKNOWN: the transmission may still be up.
    // It is said on the gate surface itself, not only as a toast, because this
    // is the one place the operator can still act (review P2).
    let stopNote = $state('');
    function noteStop(r: { status: string; message?: string }): void {
        if (r.status === 'failed') {
            toasts.error(r.message ?? 'The stop request failed.');
        } else if (r.status === 'unknown') {
            stopNote = r.message ?? 'The stop could not be confirmed — check the rig.';
            toasts.warn(stopNote);
        } else {
            stopNote = '';
        }
    }
    async function disableTx(): Promise<void> {
        stopping = true;
        try {
            noteStop(await armTx(false));
        } finally {
            stopping = false;
        }
    }
    async function stopTune(): Promise<void> {
        stopping = true;
        try {
            noteStop(await toggleTune());
        } finally {
            stopping = false;
        }
    }

    // Focus lands on the one control as the gate appears: with the rest of the
    // app inert, keyboard users are not left on a control they can no longer see.
    let reloadButton = $state<HTMLButtonElement | null>(null);
    $effect(() => {
        if (archivesState.switchUnresolved) reloadButton?.focus();
    });

    // A HELD reload (ADR 0085): the unlogged QSO could not be saved before the
    // rebind reload. Reload now would lose it, so the ways out are Retry save
    // and an explicit, confirmed Discard and reload.
    const held = $derived(archivesState.saveFailed);
    let retrying = $state(false);
    async function retry(): Promise<void> {
        retrying = true;
        try {
            await retrySave();
        } finally {
            retrying = false;
        }
    }
    function discard(): void {
        const ok = window.confirm(
            'Discard this unlogged QSO and reload? It was not saved and cannot be recovered.'
        );
        if (ok) discardAndReload();
    }
</script>

{#if archivesState.switchUnresolved}
    <div
        class="fixed inset-0 z-50 flex items-center justify-center bg-canvas/80 p-6"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="archive-gate-title"
    >
        <div class="card max-h-[90vh] max-w-lg overflow-y-auto">
            <h2 id="archive-gate-title" class="text-base font-semibold text-ink">
                Archive binding unproven
            </h2>
            <p class="mt-2 text-sm text-ink">
                Station Manager cannot tell which archive the daemon serves this page’s data from.
                {archivesState.switchDetail}
            </p>
            <p class="mt-2 text-sm text-muted">
                Logging and transmitting are paused so nothing lands in the wrong archive.{#if !held}
                    Reload to continue on the archive the daemon is serving.{/if}
            </p>
            {#if held}
                <div class="mt-3 border-t border-line pt-3" data-testid="save-failed">
                    <h3 class="text-sm font-semibold text-ink">This QSO could not be saved</h3>
                    <p class="mt-1 text-sm text-ink">
                        {held.reason} The page stays here so it is not lost: copy it, then try saving
                        again or discard it.
                    </p>
                    <p
                        class="mt-2 text-sm font-semibold text-ink"
                        data-testid="save-failed-headline"
                    >
                        {savedDraftHeadline(held.record, 'unsaved')}
                    </p>
                    <SavedDraftDetails record={held.record} state="unsaved" />
                </div>
            {/if}
            {#if stopNote}
                <p class="mt-2 text-sm text-ink" role="status" data-testid="stop-note">
                    {stopNote}
                </p>
            {/if}
            <div class="mt-4 flex flex-wrap gap-2">
                {#if held}
                    <button
                        type="button"
                        class="btn btn-primary"
                        disabled={retrying}
                        bind:this={reloadButton}
                        onclick={() => void retry()}>Retry save</button
                    >
                    <button type="button" class="btn" onclick={discard}>Discard and reload</button>
                {:else}
                    <button
                        type="button"
                        class="btn btn-primary"
                        bind:this={reloadButton}
                        onclick={reloadNow}>Reload now</button
                    >
                {/if}
                {#if ft8State.tx.armed}
                    <button
                        type="button"
                        class="btn"
                        disabled={stopping}
                        onclick={() => void disableTx()}>Disable FT8 TX</button
                    >
                {/if}
                {#if rig.tuneActive}
                    <button
                        type="button"
                        class="btn"
                        disabled={stopping}
                        onclick={() => void stopTune()}>Stop tune</button
                    >
                {/if}
            </div>
        </div>
    </div>
{/if}
