<script lang="ts">
    // Right util rail — logging info panels (Worked/Session/Details/Rig toggle a
    // card below the logging card) + Pile-up (opens the drawer). Collapsible
    // full↔narrow like the left nav (data-util). Shown only in Operate → Phone/CW
    // (the render gate is in Operate; visibility/offset are gated on data-rail).
    import { toggleUtil } from '../ui/state.svelte';
    import { operate, togglePanel, setPileup, focusCallsign, type Panel } from './state.svelte';

    // Worked/Session are read-only: after toggling them the operator's next
    // act is typing, so focus goes home to the callsign field. Details/Rig
    // keep focus — they're opened to interact with their own inputs.
    function onPanelClick(p: Panel): void {
        togglePanel(p);
        if (p === 'worked' || p === 'session') focusCallsign();
    }

    const panels: { key: Panel; label: string }[] = [
        { key: 'worked', label: 'Worked' },
        { key: 'session', label: 'Session' },
        { key: 'details', label: 'Details' },
        { key: 'rig', label: 'Rig' },
    ];
</script>

{#snippet railIcon(key: Panel)}
    {#if key === 'worked'}
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
                d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"
            />
        </svg>
    {:else if key === 'session'}
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
                d="M3.75 12h16.5m-16.5 3.75h16.5M3.75 19.5h16.5M5.625 4.5h12.75a1.875 1.875 0 0 1 0 3.75H5.625a1.875 1.875 0 0 1 0-3.75Z"
            />
        </svg>
    {:else if key === 'details'}
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
                d="M15 9h3.75M15 12h3.75M15 15h3.75M4.5 19.5h15a2.25 2.25 0 0 0 2.25-2.25V6.75A2.25 2.25 0 0 0 19.5 4.5h-15a2.25 2.25 0 0 0-2.25 2.25v10.5A2.25 2.25 0 0 0 4.5 19.5Zm6-10.125a1.875 1.875 0 1 1-3.75 0 1.875 1.875 0 0 1 3.75 0ZM10.5 15.375a3 3 0 0 0-6 0v.75c0 .621.504 1.125 1.125 1.125h3.75c.621 0 1.125-.504 1.125-1.125v-.75Z"
            />
        </svg>
    {:else}
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
                d="M7.5 21 3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5"
            />
        </svg>
    {/if}
{/snippet}

<aside class="util-rail" aria-label="Info panels">
    <div class="flex h-full flex-col gap-1 border-l border-line bg-surface px-2 py-4">
        {#each panels as p (p.key)}
            <button
                class="rail-item"
                title={p.label}
                data-active={operate.panel === p.key ? 'true' : 'false'}
                onclick={() => onPanelClick(p.key)}
            >
                {@render railIcon(p.key)}
                <span class="rail-label">{p.label}</span>
            </button>
        {/each}

        <div class="mt-auto flex flex-col gap-1">
            <button
                class="rail-item"
                title="Pile-up"
                data-active={operate.pileup ? 'true' : 'false'}
                onclick={() => setPileup(!operate.pileup)}
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
                        d="M6 6.878V6a2.25 2.25 0 0 1 2.25-2.25h7.5A2.25 2.25 0 0 1 18 6v.878m-12 0c.235-.083.487-.128.75-.128h10.5c.263 0 .515.045.75.128m-12 0A2.25 2.25 0 0 0 4.5 9v.878m13.5-3A2.25 2.25 0 0 1 19.5 9v.878m0 0a2.246 2.246 0 0 0-.75-.128H5.25c-.263 0-.515.045-.75.128m15 0A2.25 2.25 0 0 1 21 12v6a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 18v-6c0-.98.626-1.813 1.5-2.122"
                    />
                </svg>
                <span class="rail-label">Pile-up</span>
            </button>
            <button class="rail-item" title="Collapse" onclick={toggleUtil}>
                <svg
                    class="util-chevron size-6 shrink-0"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke-width="1.5"
                    stroke="currentColor"
                    aria-hidden="true"
                >
                    <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        d="m5.25 4.5 7.5 7.5-7.5 7.5m6-15 7.5 7.5-7.5 7.5"
                    />
                </svg>
                <span class="rail-label">Collapse</span>
            </button>
        </div>
    </div>
</aside>
