<script lang="ts">
    import QsoPanel from '../panels/QsoPanel.svelte';
    import CountryPanel from '../panels/CountryPanel.svelte';
    import SessionTimer from '../components/SessionTimer.svelte';
    import InfoPanel from '../panels/InfoPanel.svelte';
    import { configState } from '../../states/config.svelte';

    const countFormatter = new Intl.NumberFormat();
    const formattedCount = $derived(countFormatter.format(configState.defaultLogbook.qsoCount));
</script>

<!--
    h-13.5 / w-70 are one-off shell dimensions — they exist only
    here and don't anchor a relationship that warrants a design token.
    If a second header strip appears with the same dimensions, lift
    these to tokens at that point.
-->
<header class="flex items-center h-13.5 px-4 border-b border-b-line-soft">
    <div class="flex flex-row items-center w-70">
        <h1 class="text-lg font-bold tracking-tight">Logging Mode:</h1>
        <div class="grid grid-cols-1"></div>
    </div>
    <div class="w-38 text-xs"></div>
    <div class="w-32 text-xs"></div>
    <div class="flex flex-col text-xs font-semibold w-52 text-nowrap">
        <div class="flex flex-row items-center">
            <div class="flex-none w-15">Logbook:</div>
            <div class="text-ellipsis overflow-hidden text-green-800">
                {configState.defaultLogbook.name} ({formattedCount})
            </div>
        </div>
        <div class="flex flex-row items-center">
            <div class="flex-none w-15">Rig:</div>
            <div class="text-ellipsis overflow-hidden text-green-800">
                {configState.station.rigName}
            </div>
        </div>
    </div>
    <div class="flex text-sm font-semibold w-46">
        <div class="w-24">Session Time:</div>
        <div class="text-right w-17"><SessionTimer /></div>
    </div>
</header>
<div class="flex">
    <QsoPanel />
    <CountryPanel />
</div>
<div class="flex">
    <InfoPanel />
</div>
