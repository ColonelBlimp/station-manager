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
    import ContactDialog from './ContactDialog.svelte';
    import RigKeys from './RigKeys.svelte';
    import Ft8View from './Ft8View.svelte';
    import SessionPanel from './SessionPanel.svelte';
    import RigPanel from './RigPanel.svelte';
    import { isVisible } from './layout.svelte';
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
<ContactDialog />

<!-- FT8 has no tile board, so the rail-toggled info panels (Rig · Session) render
     here as a stacked overlay when shown. Phone/CW shows the same self-contained
     cards in the TileBoard instead; the rail icons toggle both hosts via the shared
     tile-visibility state, and each card's X (hideTile) closes it here too. In FT8
     the rig card's band buttons jump to the configured FT8 watering-hole freq
     (ft8SelectBand → set_freq of ft8_frequencies[band]), not the rig's band-stack
     freq that Phone/CW's default selectBand restores, and requiresCat disables the
     card's controls with the rig away (FT8 can't run without CAT, so the manual/
     confirm path is meaningless here). -->
{#if router.mode !== 'phone' && (isVisible('rig') || isVisible('session'))}
    <!-- Anchored to the rail's INNER edge (right: rail width + 1rem) so the gap
         beside the rail matches the 1rem below the 4rem header (top-20), at
         either rail width — a fixed right offset only matched the narrow rail. -->
    <div
        class="fixed top-20 right-[calc(var(--util-rail-w)+1rem)] z-40 flex max-h-[calc(100vh-6rem)] flex-col gap-3 overflow-auto"
    >
        {#if isVisible('rig')}<RigPanel pickBand={ft8SelectBand} requiresCat />{/if}
        {#if isVisible('session')}<SessionPanel />{/if}
    </div>
{/if}

<!-- Rig-control keyboard shortcuts — Operate-wide (Phone/CW + FT8). -->
<RigKeys />
