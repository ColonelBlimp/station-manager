<script lang="ts">
    // Right util rail — tile show/hide (Worked/Session/Rig) + Arrange + Pile-up.
    // Under the tile layout (ADR 0046) the rail toggles a tile's VISIBILITY (it
    // no longer swaps a single info slot); Arrange enters the drag/pin mode.
    // Collapsible full↔narrow like the left nav (data-util); shown only in
    // Operate → Phone/CW (render gate in Operate; visibility on data-rail).
    import { toggleUtil } from '../ui/state.svelte';
    import { router } from '../router.svelte';
    import { setPileup, focusCallsign } from './state.svelte';
    import { operate } from './state.svelte';
    import { ft8PileupStack } from './ft8Pileup.svelte';
    import {
        layout,
        toggleTile,
        toggleArranging,
        isVisible,
        RAIL_TILES,
        type TileId,
    } from './layout.svelte';
    import { rig } from './rig.svelte';
    import { session } from './session.svelte';

    // Worked/Session are read-only: after showing one the operator's next act is
    // typing, so focus goes home to the callsign field. The Rig tile is read-only
    // too while CAT is live (fields locked) — same hand-back; CAT-off/lost it
    // keeps focus (opened to edit values or confirm). Only hand back on a SHOW
    // (hiding, or entering arrange, shouldn't yank focus).
    function onTileClick(id: TileId): void {
        const willShow = !isVisible(id);
        toggleTile(id);
        const readOnly =
            id === 'worked' || id === 'session' || (id === 'rig' && rig.cat === 'connected');
        if (willShow && readOnly) focusCallsign();
    }

    const labels: Record<string, string> = {
        worked: 'Worked',
        session: 'Session',
        rig: 'Rig',
    };

    // The Worked panel is a Phone/CW tile only — FT8's Band Activity already
    // carries worked-before context (dupe tint + the Q/● badges), so the panel
    // adds nothing there (operator, 2026-07-13). Disabled rather than wired:
    // clicking it in FT8 would silently flip the Phone/CW tile board's state.
    const workedDisabled = $derived(router.mode !== 'phone');
</script>

{#snippet railIcon(key: string)}
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
        {#each RAIL_TILES as key (key)}
            {@const offHere = key === 'worked' && workedDisabled}
            <button
                class="rail-item relative disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                title={offHere
                    ? 'Worked — not available in FT8'
                    : `${labels[key]} — ${isVisible(key) ? 'hide' : 'show'}`}
                data-active={!offHere && isVisible(key) ? 'true' : 'false'}
                disabled={offHere}
                onclick={() => onTileClick(key)}
            >
                {@render railIcon(key)}
                <span class="rail-label">{labels[key]}</span>
                {#if key === 'session' && session.qsos.length > 0}
                    <span
                        class="absolute top-0.5 left-6 min-w-4 rounded-full bg-focus px-1 text-center text-[10px] font-bold text-white tabular-nums"
                        title={`${session.qsos.length} QSO${session.qsos.length === 1 ? '' : 's'} this session`}
                    >
                        {session.qsos.length}
                    </span>
                {/if}
            </button>
        {/each}

        <div class="mt-auto flex flex-col gap-1">
            <button
                class="rail-item"
                title="Arrange layout"
                data-active={layout.arranging ? 'true' : 'false'}
                onclick={toggleArranging}
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
                        d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z"
                    />
                </svg>
                <span class="rail-label">Arrange</span>
            </button>
            <button
                class="rail-item relative"
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
                {#if ft8PileupStack.count > 0}
                    <span
                        class="absolute top-0.5 left-6 min-w-4 rounded-full bg-focus px-1 text-center text-[10px] font-bold text-white tabular-nums"
                        title={`${ft8PileupStack.count} caller${ft8PileupStack.count === 1 ? '' : 's'} queued`}
                    >
                        {ft8PileupStack.count}
                    </span>
                {/if}
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
