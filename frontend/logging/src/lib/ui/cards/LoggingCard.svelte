<script lang="ts">
    import QsoPanel from '../panels/QsoPanel.svelte';
    import CountryPanel from '../panels/CountryPanel.svelte';
    import SessionTimer from '../components/SessionTimer.svelte';
    import InfoPanel from '../panels/InfoPanel.svelte';
    import {configState} from "../../states/config.svelte";
</script>

<!--
    h-13.5 / w-72.5 are one-off shell dimensions — they exist only
    here and don't anchor a relationship that warrants a design token.
    If a second header strip appears with the same dimensions, lift
    these to tokens at that point.
-->
<header class="flex items-center h-13.5 px-4 border-b border-b-line-soft">
    <div class="flex flex-row items-center w-70">
        <h1 class="text-xl font-bold tracking-tight">Logging Mode:</h1>
        <div class="grid grid-cols-1">
        </div>
    </div>
    <div class="w-38 text-xs">
    </div>
    <div class="w-38 text-xs">
    </div>
    <div class="flex flex-col text-xs font-semibold w-44">
        <div class="flex flex-row items-center">
            <div class="w-15">Logbook:</div>
            <div class="w-25 text-ellipsis overflow-hidden text-indigo-700">{configState.defaultLogbook.name}</div>
        </div>
        <div class="flex flex-row items-center">
            <div class="w-15">Rig:</div>
            <div class="w-25 text-ellipsis overflow-hidden text-indigo-700">{configState.station.rigName}</div>
        </div>
    </div>
    <div class="flex text-sm font-semibold w-46 ml-auto">
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
