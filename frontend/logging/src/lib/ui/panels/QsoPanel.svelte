<script lang="ts">
    import { onDestroy } from 'svelte';
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
    import { formatAdifRecord } from '../../utils/adif';
    import { frequencyToBand } from '../../utils/frequency';
    import { isValidCallsign } from '../../validators/callsign';
    import { isValidRst } from '../../validators/rst';

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
        Time-off ticker. The first Tab-on-valid-callsign is treated as
        the QSO start (per the operator's session 28 plan): both
        timeOn and timeOff snap to "now" at that moment, then a
        per-second interval keeps timeOff updated against the wall
        clock. Subsequent Tabs are no-ops so timeOn doesn't drift if
        the operator re-Tabs (e.g. correcting a typo).

        The ticker is stopped by `clearDraft()` (which runs on Clear
        and after submit) and on component unmount. Operator-typed
        edits to timeOff during an active timer get clobbered on the
        next minute boundary — the planned Start / Stop buttons will
        give explicit pause / resume control.
    */
    let timeOffTickerId: ReturnType<typeof setInterval> | null = null;

    function startTimeOffTicker(): void {
        if (timeOffTickerId !== null) return;
        timeOffTickerId = setInterval(() => {
            const next = formatUtcTime(new Date());
            if (next !== timeOff) timeOff = next;
        }, 1000);
    }

    function stopTimeOffTicker(): void {
        if (timeOffTickerId !== null) {
            clearInterval(timeOffTickerId);
            timeOffTickerId = null;
        }
    }

    onDestroy(stopTimeOffTicker);

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

        Side-effect: starts the time-off ticker on the FIRST valid Tab.
        Snaps timeOn / timeOff to "now" at that moment. Subsequent
        Tabs are no-ops on the ticker (it's already running), so
        repeated callsign edits don't reset timeOn.
    */
    function handleEnrich(call: string): void {
        if (timeOffTickerId === null) {
            // First Tab on a valid callsign — treat as QSO start.
            const fresh = formatUtcTime(new Date());
            timeOn = fresh;
            timeOff = fresh;
            startTimeOffTicker();
        }
        // TODO(/v1/enrich/callsign): fetch enrichment via
        // lib/enrichment.svelte.ts and assign name/qth from the
        // response. e.g.
        //   const r = await enrichCallsign(call);
        //   name = r.name ?? '';
        //   qth = r.qth ?? '';
        void call;
    }

    /*
        canSubmit — form-level required-ness gate. Per the patterns
        memory's Rule 3 (validators don't enforce presence), validators
        treat empty as not-invalid; required-ness lives here. Pass to
        FormControls's `submitDisabled={!canSubmit}` to gate the Log
        Contact button.

        Required fields (must be non-empty AND well-formed where applicable):
          - callsign (non-empty, valid)
          - rstSent / rstRcvd (non-empty, valid)
          - qsoDate, timeOn, timeOff (non-empty)

        Mode is always set (defaults). Frequency is always set (VFO
        defaults). Band derives; out-of-band is allowed (operator may
        intentionally tune outside an allocation for receive).
    */
    const canSubmit = $derived(
        callsign.trim() !== '' && isValidCallsign(callsign) &&
        rstSent.trim() !== '' && isValidRst(rstSent) &&
        rstRcvd.trim() !== '' && isValidRst(rstRcvd) &&
        qsoDate !== '' &&
        timeOn !== '' &&
        timeOff !== ''
    );

    /*
        clearDraft — reset every draft field to its initial value.
        Date / time are recomputed against the current UTC moment so
        the next QSO starts "now"; RST fall back to the current
        mode's default; mode is left alone (it mirrors manualState /
        displayedState and is not panel-owned).

        Called from FormControls's onClear and after a successful
        submit so the operator can immediately start the next contact.
    */
    function clearDraft(): void {
        stopTimeOffTicker();
        callsign = '';
        name = '';
        qth = '';
        comment = '';
        rstSent = displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE;
        rstRcvd = displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE;
        const fresh = new Date();
        qsoDate = formatUtcDate(fresh);
        const freshTime = formatUtcTime(fresh);
        timeOn = freshTime;
        timeOff = freshTime;
    }

    /*
        submitQso — build the ADIF record from the current draft + rig
        state, then `console.log` it. Daemon `/v1/qso` POST is the next
        step (path b from the session 27 fork in the road); the
        intermediate console.log shape is so we can verify the wire
        format end-to-end before adding the network round-trip.

        Split-mode TX/RX freq derivation: in split mode the SELECTED
        VFO is RX (per Vfos.svelte's snippet logic); the OTHER VFO is
        TX. ADIF FREQ is the TX frequency; FREQ_RX is set only when
        split. In non-split mode TX freq = the selected VFO's freq;
        FREQ_RX is omitted.
    */
    function submitQso(): void {
        if (!canSubmit) return;

        const selectedHz = displayedState.selectedVfo === 'A'
            ? displayedState.vfoA
            : displayedState.vfoB;
        const otherHz = displayedState.selectedVfo === 'A'
            ? displayedState.vfoB
            : displayedState.vfoA;

        const txFreqHz = displayedState.split ? otherHz : selectedHz;
        const rxFreqHz = displayedState.split ? selectedHz : undefined;

        const adif = formatAdifRecord({
            callsign: callsign.trim().toUpperCase(),
            rstSent,
            rstRcvd,
            name: name.trim(),
            qth: qth.trim(),
            comment: comment.trim(),
            qsoDate,
            timeOn,
            timeOff,
            mode: displayedState.mode,
            subMode: displayedState.subMode,
            txFreqHz,
            rxFreqHz,
            band: frequencyToBand(txFreqHz),
            txPower: displayedState.effectivePower,
        });

        console.log('[QSO submit] ADIF payload:\n' + adif);

        // TODO(/v1/qso): POST adif as raw body (Content-Type:
        // application/x-adif or text/plain) to the daemon when the
        // endpoint lands. On success, clearDraft(); on failure,
        // surface a toast (per ADR 0008, also pending) and leave
        // the draft for retry.

        // For now: clear after submit so the operator can immediately
        // begin the next QSO. When the daemon path is wired, move
        // this call into the success branch.
        clearDraft();
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
            <FormControls
                onClear={clearDraft}
                onSubmit={submitQso}
                submitDisabled={!canSubmit}
            />
        </div>
    </div>
</div>
