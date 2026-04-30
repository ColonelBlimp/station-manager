<script lang="ts">
import Callsign from "../components/Callsign.svelte";
import Rst from "../components/Rst.svelte";
</script>

<div class="flex space-x-2 border border-blue-500">
    <Callsign id="call" label="Callsign" value=""/>
    <Rst id="rst_sent" label="RST Sent" value=""/>
    <Rst id="rst_rcvd" label="RST Rcvd" value=""/>
</div>