<script lang="ts">
    import Callsign from "../components/Callsign.svelte";
    import Rst from "../components/Rst.svelte";
    import Mode from "../components/Mode.svelte";
    import Vfos from "../components/Vfos.svelte";
    import { displayedState } from '../../states/displayed.svelte';

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

    // RST defaults are mode-dependent ham-radio convention: voice modes
    // use the two-digit Readability-Strength scale (59); CW adds a third
    // "tone" digit (599). These are computed defaults — operator-typed
    // values override per QSO. Not persisted: RST is per-QSO operator
    // activity, not station configuration. (When the QSO submit / draft-
    // state machinery lands, these will move into qsoDraftState as
    // initial values applied at draft creation; for now they live as
    // a $derived prop value on the Rst components.)
    const DEFAULT_RST_VOICE = '59';
    const DEFAULT_RST_CW = '599';
    const defaultRst = $derived(displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE);
</script>

<div class="flex space-x-2 border border-blue-500">
    <Callsign id="call" label="Callsign" value=""/>
    <Rst id="rst_sent" label="RST Sent" value={defaultRst}/>
    <Rst id="rst_rcvd" label="RST Rcvd" value={defaultRst}/>
    <Mode id="mode" label="Mode" value="USB" list={modes}/>
    <Vfos/>
</div>
