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
     tile-visibility state, and each card's X (hideTile) closes it here too. RigPanel
     takes the default selectBand pick for now — the FT8 watering-hole pick lands
     with the ft8_frequencies config. -->
{#if router.mode !== 'phone' && (isVisible('rig') || isVisible('session'))}
    <div
        class="fixed top-20 right-16 z-40 flex max-h-[calc(100vh-6rem)] flex-col gap-3 overflow-auto"
    >
        {#if isVisible('rig')}<RigPanel />{/if}
        {#if isVisible('session')}<SessionPanel />{/if}
    </div>
{/if}

<!-- Rig-control keyboard shortcuts — Operate-wide (Phone/CW + FT8). -->
<RigKeys />
