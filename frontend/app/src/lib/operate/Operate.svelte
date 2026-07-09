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
</script>

{#if router.mode === 'phone'}
    <TileBoard />
    <ArrangeBar />
{:else}
    <div class="mx-auto max-w-3xl">
        <div class="card">
            <h2 class="text-sm font-semibold text-ink">FT8</h2>
            <p class="mt-1 text-sm text-muted">
                Band activity, occupancy / offset picker, and the sequencer ladder — built in a
                later pass (its own tile surface).
            </p>
        </div>
    </div>
{/if}

<!-- Rail + drawer + overlays for the whole Operate view. -->
<UtilRail />
<PileupDrawer />
<ExportDialog />
<ContactDialog />
<!-- Rig-control keyboard shortcuts — Operate-wide (Phone/CW + FT8). -->
<RigKeys />
