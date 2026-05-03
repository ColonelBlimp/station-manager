<script lang="ts">
    import { onDestroy } from 'svelte';
    import { get } from 'svelte/store';
    import Callsign from "../components/Callsign.svelte";
    import Rst from "../components/Rst.svelte";
    import Mode from "../components/Mode.svelte";
    import Vfos from "../components/Vfos.svelte";
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import { station } from '../../stores/station';
    import TextInput from "../components/TextInput.svelte";
    import Comment from "../components/Comment.svelte";
    import DateInput from "../components/DateInput.svelte";
    import TimeInput from "../components/TimeInput.svelte";
    import FormControls from "../components/FormControls.svelte";
    import { formatUtcDate, formatUtcTime } from '../../utils/time';
    import { formatAdifRecord } from '../../utils/adif';
    import { frequencyToBand } from '../../utils/frequency';
    import { resolveModeAndSubmode } from '../../utils/mode';
    import { isValidCallsign } from '../../validators/callsign';
    import { isValidRst } from '../../validators/rst';
    import { submitQso as submitQsoToDaemon } from '../../api/qso';
    import { toasts } from '../../states/toasts.svelte';

    /*
        Hardcoded until a logbook switcher lands. The daemon seeds a
        default logbook at id=1 on first run, so this matches the
        running-daemon default. When the switcher arrives this becomes
        a derived value from a logbook store seeded from `GET /v1/logbook`.
    */
    const DEFAULT_LOGBOOK_ID = 1;

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
        QSO timer model (settled session 28).

        Two states, one always-running ticker:

          - Pre-QSO  (qsoStarted === false): qsoDate, timeOn and
                     timeOff all paired-tick to current UTC. The form
                     reflects "right now" without the operator having
                     to click into a field.
          - Active   (qsoStarted === true): qsoDate and timeOn are
                     pinned at the Tab moment; timeOff continues to
                     tick. ADIF QSO_DATE is the date the QSO STARTED,
                     so pinning across midnight is semantically right.

        Transitions:
          - Tab on a valid callsign: snap qsoDate/timeOn/timeOff to
            current UTC (avoids the up-to-60s staleness window from
            the paired ticker), then set qsoStarted=true.
          - Clear / Submit: set qsoStarted=false; the paired ticker
            resumes naturally on the next tick.

        Tick rate is 60s, matching the HH:MM display granularity. The
        single ticker runs from panel mount to onDestroy. Manual edits
        to a tick-driven field get clobbered on the next minute
        boundary, which gives the operator up to 59s to type and
        Submit / Tab — plenty for back-logging.

        The Start/Stop button affordances were considered and dropped
        (session 28): the lookup-only F2 path covers the DX-pile-up
        case without needing a separate stop, and "abandon a QSO
        without clearing" isn't a real workflow.
    */
    let qsoStarted = false;

    function tick(): void {
        const now = new Date();
        const utcTime = formatUtcTime(now);
        if (!qsoStarted) {
            const utcDate = formatUtcDate(now);
            if (utcDate !== qsoDate) qsoDate = utcDate;
            if (utcTime !== timeOn) timeOn = utcTime;
        }
        if (utcTime !== timeOff) timeOff = utcTime;
    }

    const tickerId = setInterval(tick, 60_000);
    onDestroy(() => clearInterval(tickerId));

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
        if (!qsoStarted) {
            // Tab on a valid callsign = QSO start. Snap qsoDate /
            // timeOn / timeOff to current UTC at this exact moment
            // (the paired ticker may be up to 60s stale; re-snap so
            // the pinned values are accurate). Then set qsoStarted
            // so qsoDate and timeOn pin; timeOff keeps ticking.
            const fresh = new Date();
            qsoDate = formatUtcDate(fresh);
            const freshTime = formatUtcTime(fresh);
            timeOn = freshTime;
            timeOff = freshTime;
            qsoStarted = true;
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
        qsoStarted = false;
        callsign = '';
        name = '';
        qth = '';
        comment = '';
        rstSent = displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE;
        rstRcvd = displayedState.mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE;
        // Snap to current UTC so the paired ticker doesn't show stale
        // values for up to 60s after Clear.
        const fresh = new Date();
        qsoDate = formatUtcDate(fresh);
        const freshTime = formatUtcTime(fresh);
        timeOn = freshTime;
        timeOff = freshTime;
    }

    /*
        submitQso — build the ADIF record from the current draft + rig
        state, POST it to the daemon, and branch on outcome.

        Outcome handling (per api.md §4.2 + ADR 0008):
          - stored      → clearDraft(); operator continues to next QSO.
                          No toast on the happy path; QSO clearing is
                          itself the visible feedback.
          - duplicate   → preserve draft; warn-level toast naming the
                          existing QSO id. The dedupe match is a
                          feature, not an error — operator likely
                          already has the contact logged.
          - validation  → preserve draft; error-level toast with the
                          daemon's code+message. Inline per-field error
                          slots (Fix 13) come later for the validators
                          that map to a specific field.
          - server      → preserve draft; error-level toast with the
                          daemon's generic 5xx code. The daemon already
                          logged the full chain; the SPA's job is to
                          NOT lose the operator's typing.
          - network     → preserve draft; error-level toast with the
                          fetch error message. Same rationale — daemon
                          may be restarting.

        Split-mode TX/RX freq derivation: in split mode the SELECTED
        VFO is RX (per Vfos.svelte's snippet logic); the OTHER VFO is
        TX. ADIF FREQ is the TX frequency; FREQ_RX is set only when
        split. In non-split mode TX freq = the selected VFO's freq;
        FREQ_RX is omitted.
    */
    async function submitQso(): Promise<void> {
        if (!canSubmit) return;

        const selectedHz = displayedState.selectedVfo === 'A'
            ? displayedState.vfoA
            : displayedState.vfoB;
        const otherHz = displayedState.selectedVfo === 'A'
            ? displayedState.vfoB
            : displayedState.vfoA;

        const txFreqHz = displayedState.split ? otherHz : selectedHz;
        const rxFreqHz = displayedState.split ? selectedHz : undefined;

        // Logging-station fields — one-shot read of the store at
        // submit time. The store is populated at app start (by hand-
        // edit of `stores/station.ts` until the daemon `/v1/config`
        // endpoint lands) and rarely mutates during the page life,
        // so a reactive subscription buys nothing for QSO submit.
        //
        // TODO: when the bridge SSE adds a rig-name field (distinct
        // from `catState.rigIdentity` which is the CAT model
        // identifier — they're typically not the same string), fall
        // back to it here when `stn.equipment.rig` is empty:
        //   myRig: stn.equipment.rig || bridgeState.rigName || '',
        const stn = get(station);

        // Operators and rigs speak in submode names (USB, FT8, PSK31).
        // ADIF requires MODE to be the parent family (SSB, MFSK, PSK)
        // with the submode value carried in SUBMODE. Resolve here so
        // the daemon's strict MODE-enum validation accepts the record.
        const resolved = resolveModeAndSubmode(displayedState.mode, displayedState.subMode);

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
            mode: resolved.mode,
            subMode: resolved.subMode,
            txFreqHz,
            rxFreqHz,
            band: frequencyToBand(txFreqHz),
            txPower: displayedState.effectivePower,
            stationCallsign: stn.stationCallsign,
            myGridSquare: stn.location.gridSquare,
            myName: stn.location.name,
            myRig: stn.equipment.rig,
            myAntenna: stn.equipment.antenna,
        });

        const outcome = await submitQsoToDaemon(adif, DEFAULT_LOGBOOK_ID);
        switch (outcome.kind) {
            case 'stored':
                clearDraft();
                break;
            case 'duplicate':
                toasts.warn(`Duplicate of QSO #${outcome.id}; not re-logged.`);
                break;
            case 'validation':
                // Daemon's `message` is already operator-readable; the
                // `code` is a wire-protocol identifier (snake_case
                // `logbook_not_found` etc.) that's noise to the user
                // but useful in the dev console for grepping daemon
                // logs.
                console.warn(`[QSO submit] ${outcome.code}: ${outcome.message}`);
                toasts.error(outcome.message);
                break;
            case 'server':
                console.error(`[QSO submit] ${outcome.code}: ${outcome.message}`);
                toasts.error(`${outcome.message}. Try again.`);
                break;
            case 'network':
                // The fetch-error detail (e.g. "Failed to fetch") is
                // useful in the dev console but doesn't help the
                // operator; the toast just names what they need to do.
                console.error(`[QSO submit] daemon unreachable: ${outcome.message}`);
                toasts.error('Cannot reach the daemon — check it is running.');
                break;
        }
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
