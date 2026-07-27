<script lang="ts">
    // Operate surface — a thin composer. Phone/CW renders the tile board (ADR
    // 0046: logging + info tiles the operator can arrange/pin); FT8 is still a
    // placeholder (its own tile surface lands in the FT8 pass). Shared rail +
    // drawer + overlays are positioned here; the cards are self-contained.
    import { router } from '../router.svelte';
    import UtilRail from './UtilRail.svelte';
    import TileBoard from './TileBoard.svelte';
    import ArrangeBar from './ArrangeBar.svelte';
    import PileupDrawer from './PileupDrawer.svelte';
    import ExportDialog from './ExportDialog.svelte';
    import RigKeys from './RigKeys.svelte';
    import Ft8View from './Ft8View.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import RigPanel from './RigPanel.svelte';
    import { isVisible, AMBIENT_TILES } from './layout.svelte';
    import { ft8SelectBand } from './rig.svelte';
</script>

{#if router.mode === 'phone'}
    <TileBoard />
    <ArrangeBar />
{:else}
    <Ft8View />
{/if}

<!-- Rail + drawer + overlays for the whole Operate view. -->
<UtilRail />
<PileupDrawer />
<ExportDialog />

<!-- The AMBIENT host: the one home for the rail-toggled reference panels
     (Rig · Session), in EVERY workspace. They used to be board tiles in Phone/CW
     and an overlay only in FT8, so the same rail click produced a panel in a
     different place depending on mode. Overlap with the content beneath is
     accepted deliberately (operator, 2026-07-27) — it is how FT8 has always
     behaved and has never been a problem.

     The rig card's band buttons jump to the configured FT8 watering-hole freq
     (ft8SelectBand → set_freq of ft8_frequencies[band]) rather than the rig's
     band-stack freq that Phone/CW's default selectBand restores; requiresCat
     disables the card's controls with the rig away. Both are FT8-shaped choices
     that now apply in Phone/CW too — worth revisiting when the Phone/CW rig
     workflow is looked at, but not silently different per workspace. -->
{#if AMBIENT_TILES.some(isVisible)}
    <!-- Anchored to the rail's INNER edge (right: rail width + 1rem) so the gap
         beside the rail matches the 1rem below the 4rem header (top-20), at
         either rail width — a fixed right offset only matched the narrow rail. -->
    <div
        data-ambient-host
        class="fixed top-20 right-[calc(var(--util-rail-w)+1rem)] z-40 flex max-h-[calc(100vh-6rem)] flex-col gap-3 overflow-auto"
    >
        {#if isVisible('rig')}<RigPanel pickBand={ft8SelectBand} requiresCat />{/if}
        {#if isVisible('session')}<SessionPanel />{/if}
    </div>
{/if}

<!-- Rig-control keyboard shortcuts — Operate-wide (Phone/CW + FT8). -->
<RigKeys />
