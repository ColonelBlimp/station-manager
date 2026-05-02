<script lang="ts">
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import VfoBox from "./VfoBox.svelte";
    import VfoInput from "./VfoInput.svelte";

    interface Props {
    }

    let { }: Props = $props();

    function formatFrequency(hz: number): string {
        const mhz = Math.floor(hz / 1_000_000);
        const khz = Math.floor((hz % 1_000_000) / 1_000);
        const hzPart = hz % 1_000;
        return `${mhz}.${khz.toString().padStart(3, '0')}.${hzPart.toString().padStart(3, '0')}`;
    }

    /**
     * Map a frequency in Hz to its amateur-radio band name (ADIF BAND
     * field convention — "20m", "70cm", etc.). Returns '' for
     * out-of-band frequencies. Boundaries follow IARU allocations
     * broad enough to cover Region 1, 2, and 3 for the common bands;
     * 60m is widened to 5.25–5.45 MHz to match most regional
     * variations. Out-of-band frequencies (between bands or below
     * 160m) return an empty string — the UI decides how to render
     * the absence.
     */
    const BANDS: { low: number; high: number; name: string }[] = [
        { low: 1_800_000,     high: 2_000_000,     name: '160m' },
        { low: 3_500_000,     high: 4_000_000,     name: '80m' },
        { low: 5_250_000,     high: 5_450_000,     name: '60m' },
        { low: 7_000_000,     high: 7_300_000,     name: '40m' },
        { low: 10_100_000,    high: 10_150_000,    name: '30m' },
        { low: 14_000_000,    high: 14_350_000,    name: '20m' },
        { low: 18_068_000,    high: 18_168_000,    name: '17m' },
        { low: 21_000_000,    high: 21_450_000,    name: '15m' },
        { low: 24_890_000,    high: 24_990_000,    name: '12m' },
        { low: 28_000_000,    high: 29_700_000,    name: '10m' },
        { low: 50_000_000,    high: 54_000_000,    name: '6m' },
        { low: 70_000_000,    high: 70_500_000,    name: '4m' },
        { low: 144_000_000,   high: 148_000_000,   name: '2m' },
        { low: 222_000_000,   high: 225_000_000,   name: '1.25m' },
        { low: 420_000_000,   high: 450_000_000,   name: '70cm' },
        { low: 902_000_000,   high: 928_000_000,   name: '33cm' },
        { low: 1_240_000_000, high: 1_300_000_000, name: '23cm' },
    ];

    function frequencyToBand(hz: number): string {
        for (const { low, high, name } of BANDS) {
            if (hz >= low && hz <= high) return name;
        }
        return '';
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
            onCommit={(hz) => {
                if (vfo === 'A') manualState.vfoA = hz;
                else manualState.vfoB = hz;
            }}
        />
    </div>
{/snippet}
