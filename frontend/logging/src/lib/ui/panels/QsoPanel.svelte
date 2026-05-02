<script lang="ts">
    import Callsign from "../components/Callsign.svelte";
    import Rst from "../components/Rst.svelte";
    import Mode from "../components/Mode.svelte";
    import Vfos from "../components/Vfos.svelte";
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import TextInput from "../components/TextInput.svelte";
    import Comment from "../components/Comment.svelte";
    import DateInput from "../components/DateInput.svelte";
    import TimeInput from "../components/TimeInput.svelte";
    import FormControls from "../components/FormControls.svelte";
    import { formatUtcDate, formatUtcTime } from '../../utils/time';

    const modes = ['USB', 'LSB', 'CW', 'FM', 'AM', 'RTTY', 'FT8', 'FT4', 'PSK31'];

    /*
        QSO draft fields — held as local $state in QsoPanel for now. A
        forthcoming `lib/states/qsoDraft.svelte.ts` will own these as a
        proper module-level singleton (see open question in
        frontend-spa.md). Until then this panel owns the in-progress
        QSO and bind:value's through to each field component. Field
        components remain $bindable so they're standalone-testable.
    */

    let callsign = $state('');
    let name = $state('');
    let qth = $state('');
    let comment = $state('');

    /*
        Date / time defaults — UTC; ham QSOs are logged in UTC universally.
        Snapshotted once when this panel mounts. Time-on and time-off
        both start at the current UTC time; operator typically advances
        time-off as the QSO concludes. Snapshots don't auto-update — the
        operator owns the values once the panel is mounted. When the QSO
        draft store lands with a "new QSO" / reset action, these will
        recompute at that point.
    */
    const now = new Date();
    const initialUtcDate = formatUtcDate(now);
    const initialUtcTime = formatUtcTime(now);

    let qsoDate = $state(initialUtcDate);
    let timeOn = $state(initialUtcTime);
    let timeOff = $state(initialUtcTime);

    /*
        RST defaults are mode-dependent ham-radio convention: voice modes
        use the two-digit Readability-Strength scale (59); CW adds a
        third "tone" digit (599). Initialized to the current mode's
        default at mount so there's no flash of empty content. The
        $effect below re-fills if the field is cleared (empty) when the
        mode changes — so an empty field tracks the current mode while
        operator-typed values stick.

        Not persisted: RST is per-QSO operator activity, not station
        configuration.
    */
    const DEFAULT_RST_VOICE = '59';
    const DEFAULT_RST_CW = '599';
    const defaultRst = $derived(displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE);

    let rstSent = $state(displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE);
    let rstRcvd = $state(displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE);

    $effect(() => {
        if (rstSent === '') rstSent = defaultRst;
    });
    $effect(() => {
        if (rstRcvd === '') rstRcvd = defaultRst;
    });

    /*
        Mode dropdown is operator-edit territory: writes go to manualState
        (per ADR 0009 static ownership), reads come from displayedState
        which picks catState vs manualState based on the three-flag rule.
        Disabled when CAT is live so the rig owns the mode field.
    */
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

    /*
        Enrichment trigger — fires when the operator Tabs out of the
        Callsign field with a valid callsign. Per ADR 0005, the daemon's
        `/v1/enrich/callsign?call=X` endpoint returns aggregated JSON
        from hamnut/QRZ/etc.; the SPA's role is a thin fetch wrapper
        (`lib/enrichment.svelte.ts`, not yet built — daemon endpoint is
        a deferred item).

        Boundary is wired today: Callsign → onenrich → handleEnrich
        populates name/qth. The body is a TODO until the daemon endpoint
        and SPA fetch wrapper land. Overwrite-on-new-callsign is the
        chosen UX — a fresh callsign means a different QSO; operator
        can re-edit if they want.
    */
    function handleEnrich(call: string): void {
        // TODO(/v1/enrich/callsign): fetch enrichment via
        // lib/enrichment.svelte.ts and assign name/qth from the
        // response. e.g.
        //   const r = await enrichCallsign(call);
        //   name = r.name ?? '';
        //   qth = r.qth ?? '';
        void call;
    }
</script>

<!--
    Panel owns the layout — outer column with `px-6` horizontal padding,
    three rows stacked vertically. Row 1 carries `pt-4` (top breathing
    room from the panel header above); rows 2 and 3 carry `mt-2` (gap
    to the row above). Each row uses `space-x-2` for horizontal gaps
    between fields.

    Children (.input-row siblings + Vfos + new field components) are
    layout-naked relative to this column — they don't add their own
    outer margins. See `app.css` for the matching note on .input-row,
    and `frontend-spa.md` §"Layout positioning — parent owns vertical
    rhythm" for the convention.
-->
<div class="flex flex-col px-6">
    <div class="flex flex-row space-x-2 pt-4">
        <Callsign id="call" label="Callsign" bind:value={callsign} onenrich={handleEnrich}/>
        <Rst id="rst_sent" label="RST Sent" bind:value={rstSent}/>
        <Rst id="rst_rcvd" label="RST Rcvd" bind:value={rstRcvd}/>
        <Mode id="mode" label="Mode" bind:value={mode} list={modes} disabled={!displayedState.editable}/>
        <Vfos/>
    </div>
    <div class="flex flex-row space-x-2 mt-2">
        <TextInput id="name" label="Name" bind:value={name}/>
        <TextInput id="qth" label="QTH" widthClass="w-46" bind:value={qth}/>
        <Comment id="comment" label="Comment" bind:value={comment}/>
    </div>
    <div class="flex flex-row space-x-2 -mt-2">
        <DateInput id="qso_date" label="Date" bind:value={qsoDate}/>
        <TimeInput id="time_on" label="Time On (UTC)" bind:value={timeOn}/>
        <TimeInput id="time_off" label="Time Off (UTC)" bind:value={timeOff}/>
        <div class="flex flex-row space-x-2">
            <FormControls/>
        </div>
    </div>
</div>
