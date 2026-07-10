<script lang="ts">
    // Arrange-mode toolbar (ADR 0046). Shown only while arranging — carries the
    // GLOBAL pin (one switch for the whole arrangement, not per-card), reset to
    // the non-destructive Default, and Done. Tile show/hide + entering arrange
    // mode live on the right rail; this bar is the pin/reset surface.
    import { layout, togglePin, resetToDefault, setArranging } from './layout.svelte';
</script>

{#if layout.arranging}
    <!-- Fixed near the bottom of the window so the pin/reset controls stay put
         while arranging. Inset past the sidebar + rail (shell vars) so it lines
         up with the work area rather than sitting under the chrome. -->
    <div
        class="fixed bottom-6 left-[calc(var(--sidebar-w)+1rem)] right-[calc(var(--util-rail-w)+1rem)] z-30 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-line bg-surface px-4 py-2 shadow-lg"
    >
        <span class="text-sm font-semibold text-ink">Arrange layout</span>
        <span class="text-xs {layout.pinned ? 'font-medium text-focus' : 'text-muted'}">
            {layout.pinned
                ? '● Pinned — layout saved (survives restart)'
                : '○ Unpinned — session only (restart → Default)'}
        </span>
        <span class="flex-1"></span>
        <button class="btn text-xs" onclick={resetToDefault}>Reset to Default</button>
        <button class="btn text-xs {layout.pinned ? '' : 'btn-primary'}" onclick={togglePin}>
            {layout.pinned ? 'Unpin' : 'Pin layout'}
        </button>
        <button class="btn text-xs" onclick={() => setArranging(false)}>Done</button>
    </div>
{/if}
