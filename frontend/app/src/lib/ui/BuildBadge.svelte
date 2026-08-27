<script lang="ts">
    // Sidebar footer build identity (W-0004 AC1/AC2/AC3). Reads the shell store, so it
    // shows the RUNNING daemon's build — never a source constant — and marks a
    // development daemon with a DEV pill. During the brief boot 'loading' it renders
    // nothing (the fetch settles in tens of ms on localhost); an unreachable/malformed
    // /v1/version shows an honest "Version unavailable", never a fabricated version.
    // Keeps the `footer-version` class so the narrow-rail collapse rule hides it.
    import { buildIdentity, isDevDaemon } from './buildIdentity.svelte';
</script>

{#if buildIdentity.status === 'ready' && buildIdentity.info}
    <span
        class="footer-version inline-flex items-center gap-1.5 rounded bg-surface-muted px-2 py-1 text-[11px] text-muted"
    >
        {buildIdentity.info.daemon}
        {#if isDevDaemon()}
            <!-- The text "DEV" carries the marker (not colour alone); amber reads as
                 "not the deployed build" in both themes. A one-off literal — there is
                 no shared warn token, and inventing one for a single marker is worse. -->
            <span
                class="rounded bg-amber-400/20 px-1 text-[9px] font-bold tracking-wide text-amber-700 dark:text-amber-300"
                >DEV</span
            >
        {/if}
    </span>
{:else if buildIdentity.status === 'unavailable'}
    <span class="footer-version rounded bg-surface-muted px-2 py-1 text-[11px] text-muted"
        >Version unavailable</span
    >
{/if}
