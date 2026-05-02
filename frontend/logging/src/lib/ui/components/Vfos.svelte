<script lang="ts">
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import VfoBox from "./VfoBox.svelte";
    import VfoInput from "./VfoInput.svelte";
    import { frequencyToBand } from '../../utils/frequency';

    function formatFrequency(hz: number): string {
        const mhz = Math.floor(hz / 1_000_000);
        const khz = Math.floor((hz % 1_000_000) / 1_000);
        const hzPart = hz % 1_000;
        return `${mhz}.${khz.toString().padStart(3, '0')}.${hzPart.toString().padStart(3, '0')}`;
    }
</script>

<!--
    Internal layout only:
      - flex flex-col space-y-2  → children stack vertically with a small gap
      - w-56                     → fixed width sized to the boxes + inputs
      - pt-2                     → INTERNAL label-compensation. Vfos has no
                                   <label> of its own, so its first row of
                                   content sits 0.5rem above sibling inputs
                                   that DO have labels. pt-2 brings it back
                                   onto the same baseline. This is intrinsic
                                   to Vfos, not external positioning.
    External vertical rhythm (where Vfos sits relative to siblings, panel
    padding, etc.) is the parent's responsibility.
-->
<div class="flex flex-col space-y-2 pt-2 w-56">
    {#if displayedState.selectedVfo === 'A'}
        {@render box('A', 'RX')}
        {@render box('B', 'TX')}
    {:else}
        {@render box('B', 'RX')}
        {@render box('A', 'TX')}
    {/if}
</div>

{#snippet box(vfo: 'A' | 'B', action: 'RX' | 'TX')}
    {@const frequency = vfo === 'A' ? displayedState.vfoA : displayedState.vfoB}
    {@const label = `VFO-${vfo}`}
    {@const isSelected = vfo === displayedState.selectedVfo}
    <!--
        Tab order: top input (the selected VFO) is always Tab-reachable.
        The bottom input (non-selected) is only Tab-reachable in split
        mode — when not split, the bottom VFO carries no operating
        meaning and Tab should skip past it to Name.
    -->
    {@const tabindex = isSelected || displayedState.split ? 0 : -1}
    <div class="flex">
        <VfoBox
            {label}
            isSplit={displayedState.split}
            {action}
            disabled={!displayedState.editable}
            {isSelected}
            onSelect={() => manualState.selectedVfo = vfo}
        />
        <VfoInput
            id={`vfo-${vfo.toLowerCase()}`}
            value={formatFrequency(frequency)}
            band={frequencyToBand(frequency)}
            disabled={!displayedState.editable}
            {tabindex}
            onCommit={(hz) => {
                if (vfo === 'A') manualState.vfoA = hz;
                else manualState.vfoB = hz;
            }}
        />
    </div>
{/snippet}
