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

<!-- FT8 has no tile board, so the rail-toggled Session panel (QSO list + its own
     Export… button) renders here as an overlay when shown. Phone/CW shows the same
     panel in the TileBoard instead; the rail's Session icon toggles both via the
     shared tile-visibility state, so its X (hideTile) closes this too. -->
{#if router.mode !== 'phone' && isVisible('session')}
    <div class="fixed top-20 right-16 z-40 max-h-[calc(100vh-6rem)] overflow-auto">
        <SessionPanel />
    </div>
{/if}

<!-- Rig-control keyboard shortcuts — Operate-wide (Phone/CW + FT8). -->
<RigKeys />
