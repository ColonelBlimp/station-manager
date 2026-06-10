<script lang="ts">
    import Ft8OccupancyStrip from './Ft8OccupancyStrip.svelte';
    import { ft8State } from '../../states/ft8.svelte';

    // Daemon's #1 ranked clear offset (suggested[0], pre-sort) — the strip marks
    // it as the recommendation.
    const topPick = $derived(ft8State.suggested[0] ?? null);
</script>

<!--
    TX-offset picker: a spatial view of the clear offsets laid out across the
    passband against the busy bands. Clicking a marker only sets the selected TX
    base offset (ft8State.selectedOffset) — it is inert until the TX controller
    (ADR 0029 step d/e) keys the rig. Sibling to the Clear Offsets list in
    Ft8Panel; both drive the one selection. The presentational strip lives in
    Ft8OccupancyStrip; this panel wires it to ft8State.
-->
<div class="m-0 border-t border-gray-300">
    <Ft8OccupancyStrip
        passbandLow={ft8State.passbandLow}
        passbandHigh={ft8State.passbandHigh}
        signalWidth={ft8State.signalWidth}
        occupied={ft8State.occupied}
        recommended={topPick}
        selected={ft8State.selectedOffset}
        hasSlot={ft8State.slot !== null}
        onselect={(hz: number) => ft8State.selectOffset(hz)}
    />
</div>
