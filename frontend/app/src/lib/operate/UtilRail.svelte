<script lang="ts">
    // Right util rail — panel show/hide (Worked/Session/Rig) + Pile-up. The rail
    // toggles a panel's VISIBILITY (it does not swap a single info slot). The
    // Arrange control went with the tile board (ADR 0058).
    // Collapsible full↔narrow like the left nav (data-util); shown only in
    // Operate → Phone/CW (render gate in Operate; visibility on data-rail).
    import { toggleUtil } from '../ui/state.svelte';
    import { router } from '../router.svelte';
    import { setPileup, setCallStack, focusCallsign } from './state.svelte';
    import { operate } from './state.svelte';
    import { ft8State } from './ft8.svelte';
    import { callsignStack } from './callsignStack.svelte';
    import { toggleTile, isVisible, RAIL_TILES, type TileId } from './layout.svelte';
    import { rig } from './rig.svelte';
    import { session } from './session.svelte';

    // The rail's pile-up control drives whichever list the current workspace
    // owns: FT8's caller queue, or Phone/CW's callsign stack. Two different
    // lists behind one affordance — they are never both on screen. In FT8 the
    // count also carries the daemon's operator_pick answerers (ADR 0065): the
    // badge rising is the ratified discovery mechanism for a new answerer
    // ("badge only" — no toast, no auto-open), so it must count them.
    const pileupCount = $derived(
        router.mode === 'ft8'
            ? ft8State.qso.answerers.length + ft8State.qso.queue.length
            : callsignStack.count
    );

    // Worked/Session are read-only: after showing one the operator's next act is
    // typing, so focus goes home to the callsign field. The Rig tile is read-only
    // too while CAT is live (fields locked) — same hand-back; CAT-off/lost it
    // keeps focus (opened to edit values or confirm). Only hand back on a SHOW
    // (hiding shouldn't yank focus).
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

    // The Worked panel is Phone/CW-only — FT8's Band Activity already carries
    // worked-before context (dupe tint + the Q/● badges) (operator, 2026-07-13).
    // Disabled rather than wired: clicking it in FT8 would flip Phone/CW state
    // with no visible effect here.
    const phoneOnly = $derived(router.mode !== 'phone');
</script>

{#snippet countBadge(n: number, label: string)}
    <!-- Glanceable indicator, not a readout. Two things keep it on screen:
         (1) rail-badge right-anchors it in NARROW mode. The rail is 3.5rem (56px)
             and fixed to right:0, so the viewport edge IS the rail edge. Left-anchored
             at left-6 the badge starts 32px in with only 24px of room, and at 10px
             bold every character costs ~6px + 8px of px-1 padding — so 4 characters
             need ~32px and hang 8px off-screen. Growing LEFTWARD instead can never
             clip (1-digit lands in the identical spot, so the usual look is unchanged).
         (2) The DISPLAY caps at 999+, bounding how far it can grow back over the icon.
         Note the cap alone does NOT fix (1): `999+` is exactly as wide as `1000`, and
         a clipped `999+` is worse than a clipped `1000` — it reads as an exact 999
         (codex review of 4e223176). The exact figure stays in the tooltip, and the
         Session panel header carries it in full. -->
    <span
        class="rail-badge absolute top-0.5 left-6 min-w-4 rounded-full bg-focus px-1 text-center text-[10px] font-bold text-white tabular-nums"
        title={label}
    >
        {n > 999 ? '999+' : n}
    </span>
{/snippet}

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
            {@const offHere = key === 'worked' && phoneOnly}
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
                    {@render countBadge(
                        session.qsos.length,
                        `${session.qsos.length} QSO${session.qsos.length === 1 ? '' : 's'} this session`
                    )}
                {/if}
            </button>
        {/each}

        <div class="mt-auto flex flex-col gap-1">
            <!-- FT8 only: nothing outside Band Activity can add to that queue, so
                 in Phone/CW this offered a drawer that could never fill — and
                 opening it used to disable the logging shortcuts. Phone/CW's own
                 pile-up appears by itself when calls are stacked. -->
            <button
                class="rail-item relative"
                title="Pile-up"
                data-active={(router.mode === 'ft8' ? operate.pileup : operate.callStack)
                    ? 'true'
                    : 'false'}
                onclick={() =>
                    router.mode === 'ft8'
                        ? setPileup(!operate.pileup)
                        : setCallStack(!operate.callStack)}
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
                {#if pileupCount > 0}
                    {@render countBadge(
                        pileupCount,
                        `${pileupCount} ${router.mode === 'ft8' ? 'caller' : 'call'}${pileupCount === 1 ? '' : 's'} ${router.mode === 'ft8' ? 'queued' : 'stacked'}`
                    )}
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
