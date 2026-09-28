<script lang="ts">
    // Shown on Phone / CW when a way into FT8/FT4 fell back here because the
    // switch is off (fresh-install ruling 2026-09-26) — in place of the FT view's
    // raw 'no such API route'. The link opens the setting; switching it on takes
    // effect after a restart.
    import { ftFeature, showFtSettings } from '../router.svelte';
</script>

{#if ftFeature.offNotice}
    <div
        role="status"
        class="flex items-center gap-3 rounded-md border border-line bg-surface-muted px-3 py-2 text-sm text-ink"
    >
        <span>
            FT8 and FT4 are turned off —
            <a
                href="/config"
                class="text-focus underline"
                onclick={(e) => {
                    e.preventDefault();
                    showFtSettings();
                }}>Settings → FT8 / FT4</a
            >
        </span>
        <button
            class="ml-auto text-xs text-muted hover:text-ink"
            onclick={() => (ftFeature.offNotice = false)}>Dismiss</button
        >
    </div>
{/if}
