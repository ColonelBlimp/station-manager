<script lang="ts">
    // The default-change banner (W-0024 slice 5b; ADR 0097 §4). Rendered at
    // shell level, beside the TX alarm banner, so every view says why live entry
    // is paused or what a change in play needs. The actions themselves live in
    // Settings → Logbooks (operator ruling 2026-10-30): this banner only links
    // there — except when the status cannot be read, when it carries the
    // recovery control (check again, or reload).
    import {
        changeNotice,
        defaultChangeState,
        recheckDefaultChange,
    } from '../config/defaultChange.svelte';
    import { reloadNow } from '../config/archives.svelte';
    import { showLogbooksSettings } from '../router.svelte';

    // Whenever a status that cannot be read leaves live work gated — unproven,
    // or sealed by an earlier pending_restart whose notice went with the status.
    const unreadable = $derived(
        defaultChangeState.read === 'unreadable' &&
            (defaultChangeState.unproven || defaultChangeState.sealed)
    );
    const notice = $derived(changeNotice());
    // History (cancelled, applied…) belongs to Settings; the shell shows only
    // what pauses live entry or needs the operator.
    const shown = $derived(notice !== null && notice.tone !== 'info' ? notice : null);

    let checking = $state(false);
    async function checkAgain(): Promise<void> {
        checking = true;
        try {
            await recheckDefaultChange();
        } finally {
            checking = false;
        }
    }
</script>

{#if unreadable}
    <div
        data-testid="default-change-banner"
        class="flex items-center gap-x-3 border-b border-amber-700 bg-amber-500 px-4 py-2 text-sm font-medium text-black"
        role="alert"
    >
        <span>
            Station Manager could not read the state of a default logbook change. Live entry is
            paused until it can.
        </span>
        <button
            type="button"
            class="ml-auto shrink-0 cursor-pointer rounded border border-black/30 px-2 py-0.5 text-xs hover:bg-amber-600 disabled:cursor-default disabled:opacity-60"
            disabled={checking}
            onclick={checkAgain}>{checking ? 'Checking…' : 'Check again'}</button
        >
        <button
            type="button"
            class="shrink-0 cursor-pointer rounded border border-black/30 px-2 py-0.5 text-xs hover:bg-amber-600"
            onclick={reloadNow}>Reload</button
        >
    </div>
{:else if shown}
    <div
        data-testid="default-change-banner"
        class={shown.tone === 'sealed'
            ? 'flex items-center gap-x-3 border-b border-amber-700 bg-amber-500 px-4 py-2 text-sm font-medium text-black'
            : 'flex items-center gap-x-3 border-b border-sky-700 bg-sky-100 px-4 py-2 text-sm text-sky-950'}
        role="status"
    >
        <span>{shown.text}</span>
        <button
            type="button"
            class="ml-auto shrink-0 cursor-pointer rounded border border-black/30 px-2 py-0.5 text-xs"
            onclick={showLogbooksSettings}>Settings → Logbooks</button
        >
    </div>
{/if}
