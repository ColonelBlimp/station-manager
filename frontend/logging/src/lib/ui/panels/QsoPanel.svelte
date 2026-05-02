<script lang="ts">
    import Callsign from "../components/Callsign.svelte";
    import Rst from "../components/Rst.svelte";
    import Mode from "../components/Mode.svelte";
    import Vfos from "../components/Vfos.svelte";
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';

    const modes = ['USB', 'LSB', 'CW', 'FM', 'AM', 'RTTY', 'FT8', 'FT4', 'PSK31'];

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

    // Mode dropdown is operator-edit territory: writes go to manualState
    // (per ADR 0009 static ownership), reads come from displayedState
    // which picks catState vs manualState based on the three-flag rule.
    // Disabled when CAT is live so the rig owns the mode field.
    let mode = $state(displayedState.mode);
    $effect(() => {
        // Mirror displayedState → local binding when the live source
        // changes (operator switching CAT on, rig pushing a new mode).
        mode = displayedState.mode;
    });
    $effect(() => {
        // Mirror operator edits → manualState. Guarded on `editable`
        // so a programmatic mode change while CAT is live doesn't
        // clobber manualState (kept stable for the snapshot-on-disconnect
        // story in ADR 0009).
        if (displayedState.editable) {
            manualState.mode = mode;
        }
    });
</script>

<!--
    Panel owns the vertical rhythm (`py-4`) and horizontal gaps
    (`space-x-2`). Children (.input-row siblings + Vfos) are layout-naked
    relative to this row — they don't add their own outer margins. See
    `app.css` for the matching note on .input-row.
-->
<div class="flex space-x-2 py-4 px-6 border-red-500">
    <Callsign id="call" label="Callsign" value=""/>
    <Rst id="rst_sent" label="RST Sent" value={defaultRst}/>
    <Rst id="rst_rcvd" label="RST Rcvd" value={defaultRst}/>
    <Mode id="mode" label="Mode" bind:value={mode} list={modes} disabled={!displayedState.editable}/>
    <Vfos/>
</div>
