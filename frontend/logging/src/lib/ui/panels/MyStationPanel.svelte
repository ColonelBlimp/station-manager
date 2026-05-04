<script lang="ts">
import {configState} from "../../states/config.svelte";
</script>

<div class="flex flex-col space-y-3 p-2">
    <div class="flex flex-row items-center mt-1 w-68">
        <div class="input-label w-30">Station Callsign</div>
        <div class="input-base w-38">{configState.loggingStation.stationCallsign}</div>
    </div>
    <div class="flex flex-row items-center mt-1 w-68">
        <div class="input-label w-30">Owner's Callsign</div>
        <div class="input-base w-38">{configState.loggingStation.ownerCallsign}</div>
    </div>
</div>
