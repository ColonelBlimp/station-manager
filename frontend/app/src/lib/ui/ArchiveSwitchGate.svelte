<script lang="ts">
    // The unproven-binding gate (ADR 0071, review P1: fail closed). Whenever the
    // SPA cannot prove which archive its stores belong to — after an activation
    // whose new generation was not seen, after a reconnect whose identity could
    // not be read, or after a boot whose identity bracket was incomplete — this
    // overlay blocks every operation until the page reloads: by the operator, or
    // by the store as soon as it can prove the daemon again. Above every route.
    import { archivesState, reloadNow } from '../config/archives.svelte';
</script>

{#if archivesState.switchUnresolved}
    <div
        class="fixed inset-0 z-50 flex items-center justify-center bg-canvas/80 p-6"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="archive-gate-title"
    >
        <div class="card max-w-md">
            <h2 id="archive-gate-title" class="text-base font-semibold text-ink">
                Archive binding unproven
            </h2>
            <p class="mt-2 text-sm text-ink">
                Station Manager cannot tell which archive the daemon serves this page’s data from.
                {archivesState.switchDetail}
            </p>
            <p class="mt-2 text-sm text-muted">
                Logging and transmitting are paused so nothing lands in the wrong archive. Reload to
                continue on the archive the daemon is serving.
            </p>
            <button type="button" class="btn btn-primary mt-4" onclick={reloadNow}
                >Reload now</button
            >
        </div>
    </div>
{/if}
