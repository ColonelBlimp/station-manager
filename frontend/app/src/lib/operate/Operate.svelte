<script lang="ts">
    // Operate surface — a thin composer. Phone/CW lays its workflow cards out in
    // responsive flow and FT8 renders its own view; the shared rail, drawer and
    // overlays are positioned here, and the cards stay self-contained.
    //
    // The tile board and arrange mode were removed by ADR 0058 (superseding 0046):
    // no arrangement friction appeared in three weeks of operating, the complaint
    // that did appear was CONSISTENCY between workspaces — answered by the ambient
    // host below, not by tiling — and with Rig and Session ambient the board was
    // arranging two tiles.
    import { router } from '../router.svelte';
    import UtilRail from './UtilRail.svelte';
    import PileupDrawer from './PileupDrawer.svelte';
    import CallsignStackPanel from './CallsignStackPanel.svelte';
    import ExportDialog from './ExportDialog.svelte';
    import RigKeys from './RigKeys.svelte';
    import Ft8View from './Ft8View.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import RigPanel from './RigPanel.svelte';
    import { isVisible, AMBIENT_TILES, WORKFLOW_TILES, TILES } from './layout.svelte';
    import { ft8SelectBand } from './rig.svelte';
</script>

{#if router.mode === 'phone'}
    <!-- Responsive flow, matching FT8 (ADR 0058 retired the tile board). The
         data-card marker is the test seam for "this panel is on the surface";
         nothing positions by it. -->
    <div data-surface="workflow" class="mx-auto flex w-full max-w-3xl flex-col gap-3 px-3 py-3">
        {#each WORKFLOW_TILES as id (id)}
            {#if isVisible(id)}
                {@const Card = TILES[id].component}
                <div data-card={id}><Card /></div>
            {/if}
        {/each}
    </div>
{:else}
    <Ft8View />
{/if}

<!-- Rail + drawer + overlays for the whole Operate view. -->
<UtilRail />
<!-- FT8's queue drawer is FT8-ONLY. It renders nothing useful in Phone/CW —
     nothing can add to that queue outside Band Activity — and while it was
     mounted there, opening it disabled Ctrl+Enter and Esc on the logging card.
     Phone/CW has its own pile-up (CallsignStackPanel), which needs no toggle. -->
{#if router.mode === 'ft8'}
    <PileupDrawer />
{:else}
    <CallsignStackPanel />
{/if}
<ExportDialog />

<!-- The AMBIENT host: the one home for the rail-toggled reference panels
     (Rig · Session), in EVERY workspace. They used to be board tiles in Phone/CW
     and an overlay only in FT8, so the same rail click produced a panel in a
     different place depending on mode. Overlap with the content beneath is
     accepted deliberately (operator, 2026-07-27) — it is how FT8 has always
     behaved and has never been a problem.

     Each workspace keeps its own rig-card behaviour inside that shared host — see
     the panel below. -->
{#if AMBIENT_TILES.some(isVisible)}
    <!-- Anchored to the rail's INNER edge (right: rail width + 1rem) so the gap
         beside the rail matches the 1rem below the 4rem header (top-20), at
         either rail width — a fixed right offset only matched the narrow rail. -->
    <div
        data-ambient-host
        class="fixed top-20 right-[calc(var(--util-rail-w)+1rem)] z-40 flex max-h-[calc(100vh-6rem)] flex-col gap-3 overflow-auto"
    >
        {#if isVisible('rig')}
            <!-- Shared HOST, not shared BEHAVIOUR. FT8 jumps the band buttons to the
                 configured watering-hole freq and requires CAT, because FT8 cannot
                 run without it. Phone/CW can: frequency and mode are settable by
                 hand and confirmed, and the band buttons restore the rig's own
                 band-stack. Applying FT8's rules there disabled the controls an
                 operator without CAT needs to establish rig state for logging —
                 noticed while building this, written into a comment, and shipped
                 anyway (codex P1 on d4233b64). A comment is not a decision. -->
            {#if router.mode === 'phone'}
                <RigPanel />
            {:else}
                <RigPanel pickBand={ft8SelectBand} requiresCat />
            {/if}
        {/if}
        {#if isVisible('session')}<SessionPanel />{/if}
    </div>
{/if}

<!-- Rig-control keyboard shortcuts — Operate-wide (Phone/CW + FT8). -->
<RigKeys />
