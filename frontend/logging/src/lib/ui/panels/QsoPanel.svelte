<script lang="ts">
    import Callsign from "../components/Callsign.svelte";
    import Rst from "../components/Rst.svelte";
    import Mode from "../components/Mode.svelte";

    const modes = [
        { key: 'USB', value: 'USB' },
        { key: 'LSB', value: 'LSB' },
        { key: 'CW', value: 'CW' },
        { key: 'FM', value: 'FM' },
        { key: 'AM', value: 'AM' },
        { key: 'RTTY', value: 'RTTY' },
        { key: 'FT8', value: 'FT8' },
        { key: 'FT4', value: 'FT4' },
        { key: 'PSK31', value: 'PSK31' },
    ];
</script>

<div class="flex space-x-2">
    <Callsign id="call" label="Callsign" value=""/>
    <Rst id="rst_sent" label="RST Sent" value=""/>
    <Rst id="rst_rcvd" label="RST Rcvd" value=""/>
    <Mode id="mode" label="Mode" value="USB" list={modes}/>
</div>
