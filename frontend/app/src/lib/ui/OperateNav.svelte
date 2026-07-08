<script lang="ts">
    // Expandable Operate nav item: Phone/CW ↔ FT8 as sub-routes. In the full rail
    // it discloses inline sub-items; in the narrow rail the sub-items move to a
    // hover flyout (CSS, gated on [data-nav='narrow']). Parent is never
    // highlighted — the active sub-mode carries the highlight (TWP convention).
    import { router, navigate, setMode, type OpMode } from '../router.svelte';

    // Start expanded when we're already on Operate.
    let open = $state(router.view === 'operate');

    const modes: { mode: OpMode; label: string }[] = [
        { mode: 'phone', label: 'Phone / CW' },
        { mode: 'ft8', label: 'FT8' },
    ];

    function isActive(mode: OpMode): boolean {
        return router.view === 'operate' && router.mode === mode;
    }
</script>

{#snippet modeIcon(mode: OpMode)}
    {#if mode === 'phone'}
        <svg
            class="size-5 shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M12 18.75a6 6 0 0 0 6-6v-1.5m-6 7.5a6 6 0 0 1-6-6v-1.5m6 7.5v3.75m-3.75 0h7.5M12 15.75a3 3 0 0 1-3-3V4.5a3 3 0 1 1 6 0v8.25a3 3 0 0 1-3 3Z"
            />
        </svg>
    {:else}
        <svg
            class="size-5 shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9.348 14.651a3.75 3.75 0 0 1 0-5.303m5.304 0a3.75 3.75 0 0 1 0 5.303m-7.425 2.122a6.75 6.75 0 0 1 0-9.546m9.546 0a6.75 6.75 0 0 1 0 9.546M5.106 18.894c-3.808-3.808-3.808-9.98 0-13.789m13.788 0c3.808 3.808 3.808 9.981 0 13.79M12 12h.008v.008H12V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
            />
        </svg>
    {/if}
{/snippet}

<li class="nav-operate relative">
    <button
        class="nav-item"
        title="Operate"
        aria-expanded={open}
        onclick={() => {
            navigate('operate');
            open = !open;
        }}
    >
        <svg
            class="size-6 shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            aria-hidden="true"
        >
            <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9.348 14.651a3.75 3.75 0 0 1 0-5.303m5.304 0a3.75 3.75 0 0 1 0 5.303m-7.425 2.122a6.75 6.75 0 0 1 0-9.546m9.546 0a6.75 6.75 0 0 1 0 9.546M5.106 18.894c-3.808-3.808-3.808-9.98 0-13.789m13.788 0c3.808 3.808 3.808 9.981 0 13.79M12 12h.008v.008H12V12Zm.375 0a.375.375 0 1 1-.75 0 .375.375 0 0 1 .75 0Z"
            />
        </svg>
        <span class="nav-label">Operate</span>
        <svg
            class="nav-chevron ml-auto size-5 shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke-width="1.5"
            stroke="currentColor"
            aria-hidden="true"
        >
            <path stroke-linecap="round" stroke-linejoin="round" d="m8.25 4.5 7.5 7.5-7.5 7.5" />
        </svg>
    </button>

    <!-- Inline disclosure (full rail) -->
    <ul class="op-submenu mt-1 space-y-1" hidden={!open}>
        {#each modes as m (m.mode)}
            <li>
                <button
                    class="nav-subitem"
                    data-active={isActive(m.mode) ? 'true' : 'false'}
                    onclick={() => setMode(m.mode)}
                >
                    <span class="nav-label">{m.label}</span>
                </button>
            </li>
        {/each}
    </ul>

    <!-- Hover flyout (narrow rail only) -->
    <div class="op-flyout">
        <div class="op-flyout-panel">
            <p class="op-flyout-header">Operate</p>
            {#each modes as m (m.mode)}
                <button
                    class="op-flyout-item"
                    data-active={isActive(m.mode) ? 'true' : 'false'}
                    onclick={() => setMode(m.mode)}
                >
                    {@render modeIcon(m.mode)}
                    {m.label}
                </button>
            {/each}
        </div>
    </div>
</li>
