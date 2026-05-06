<script lang="ts">
    import { onDestroy } from 'svelte';
    import Callsign from "../components/Callsign.svelte";
    import Rst from "../components/Rst.svelte";
    import Mode from "../components/Mode.svelte";
    import Vfos from "../components/Vfos.svelte";
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import { qsoDefaults } from '../../states/qsoDefaults.svelte';
    import { qsoDraft } from '../../states/qsoDraft.svelte';
    import TextInput from "../components/TextInput.svelte";
    import Comment from "../components/Comment.svelte";
    import DateInput from "../components/DateInput.svelte";
    import TimeInput from "../components/TimeInput.svelte";
    import FormControls from "../components/FormControls.svelte";
    import { formatAdifRecord } from '../../utils/adif';
    import { frequencyToBand } from '../../utils/frequency';
    import { resolveModeAndSubmode } from '../../utils/mode';
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
        Mode dropdown is operator-edit territory: writes go to manualState
        (per ADR 0009 static ownership), reads come from displayedState
        which picks catState vs manualState based on the three-flag rule.
        Disabled when CAT is live so the rig owns the mode field. Mode
        stays a panel-local because it is a CAT-state concern (mirrors
        displayedState/manualState), not a draft field — keeping it out
        of qsoDraft preserves the ADR 0009 ownership boundary.
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
        QSO timer ticker. Lifecycle and pre-QSO/active branching live
        on qsoDraft itself (`tick()`, `startQso()`, `qsoStarted`). The
        panel just runs the 1s interval and ties cleanup to onDestroy
        so module load is side-effect-free. Per-second ticks are
        no-ops most of the time (writes are guarded on HH:MM string
        change), but the rate keeps the displayed time within ~1s of
        the actual minute boundary instead of lagging up to 60s.
    */
    const tickerId = setInterval(() => qsoDraft.tick(), 1_000);
    onDestroy(() => clearInterval(tickerId));

    /*
        Enrichment trigger — fires when the operator Tabs out of the
        Callsign field with a valid callsign. Per ADR 0005, the daemon's
        `/v1/enrich/callsign?call=X` endpoint returns aggregated JSON
        from hamnut/QRZ/etc.; the SPA's role is a thin fetch wrapper
        (`lib/enrichment.svelte.ts`, not yet built — daemon endpoint is
        a deferred item).

        Boundary is wired today: Callsign → onenrich → handleEnrich
        calls qsoDraft.startQso() (snap dates + flip qsoStarted) and
        will populate name/qth from the enrichment response. The
        populate body is a TODO until the daemon endpoint and SPA
        fetch wrapper land. Overwrite-on-new-callsign is the chosen
        UX — a fresh callsign means a different QSO; operator can
        re-edit if they want.
    */
    function handleEnrich(call: string): void {
        if (!qsoDraft.qsoStarted) {
            qsoDraft.startQso();
        }
        // TODO(/v1/enrich/callsign): fetch enrichment via
        // lib/enrichment.svelte.ts and call qsoDraft.populateFromEnrichment(...)
        // when that method lands alongside the fetch wrapper. e.g.
        //   const r = await enrichCallsign(call);
        //   qsoDraft.populateFromEnrichment(r);
        void call;
    }

    /*
        submitQso — build the ADIF record from the current draft + rig
        state, POST it to the daemon, and branch on outcome.

        Outcome handling (per api.md §4.2 + ADR 0008):
          - stored      → qsoDraft.clear(); operator continues to next QSO.
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
        if (!qsoDraft.canSubmit) return;

        const selectedHz = displayedState.selectedVfo === 'A'
            ? displayedState.vfoA
            : displayedState.vfoB;
        const otherHz = displayedState.selectedVfo === 'A'
            ? displayedState.vfoB
            : displayedState.vfoA;

        const txFreqHz = displayedState.split ? otherHz : selectedHz;
        const rxFreqHz = displayedState.split ? selectedHz : undefined;

        // Identity fields — single source: configState.loggingStation
        // (daemon-authoritative via /v1/config; written by the My
        // Station panel; validated by the daemon on PUT). Read at
        // submit time; ADIF emits omit-when-empty so unset fields
        // simply don't appear in the record.
        const ls = configState.loggingStation;

        // Operators and rigs speak in submode names (USB, FT8, PSK31).
        // ADIF requires MODE to be the parent family (SSB, MFSK, PSK)
        // with the submode value carried in SUBMODE. Resolve here so
        // the daemon's strict MODE-enum validation accepts the record.
        const resolved = resolveModeAndSubmode(displayedState.mode, displayedState.subMode);

        // Captured before submit so the post-store toast can name the
        // contact even after qsoDraft.clear() has wiped the form.
        const submittedCall = qsoDraft.callsign.trim().toUpperCase();

        const adif = formatAdifRecord({
            callsign: submittedCall,
            rstSent: qsoDraft.rstSent,
            rstRcvd: qsoDraft.rstRcvd,
            name: qsoDraft.name.trim(),
            qth: qsoDraft.qth.trim(),
            comment: qsoDraft.comment.trim(),
            qsoDate: qsoDraft.qsoDate,
            timeOn: qsoDraft.timeOn,
            timeOff: qsoDraft.timeOff,
            mode: resolved.mode,
            subMode: resolved.subMode,
            txFreqHz,
            rxFreqHz,
            band: frequencyToBand(txFreqHz),
            txPower: displayedState.effectivePower,
            qsoRandom: qsoDefaults.qsoRandom === 'off' ? undefined : qsoDefaults.qsoRandom,
            stationCallsign: ls.stationCallsign,
            operator: ls.operator,
            ownerCallsign: ls.ownerCallsign,
            myGridSquare: ls.myGridsquare,
            myLat: ls.myLat,
            myLon: ls.myLon,
            myStreet: ls.myStreet,
            myCity: ls.myCity,
            myPostalCode: ls.myPostalCode,
            myCountry: ls.myCountry,
            myAltitude: ls.myAltitude,
            myCqZone: ls.myCqZone,
            myItuZone: ls.myItuZone,
            myDxcc: ls.myDxcc,
            myName: ls.myName,
            myRig: ls.myRig,
            myAntenna: ls.myAntenna,
            myMorseKeyType: ls.myMorseKeyType,
            myMorseKeyInfo: ls.myMorseKeyInfo,
        });

        const outcome = await submitQsoToDaemon(adif, DEFAULT_LOGBOOK_ID);
        switch (outcome.kind) {
            case 'stored':
                qsoDraft.clear();
                if (qsoDefaults.notifyQsoStored) {
                    toasts.info(`QSO with ${submittedCall} stored.`);
                }
                break;
            case 'duplicate':
                // Toast surfaces the operator-readable callsign rather
                // than the 36-char UUID; the UUID lands in the dev
                // console for cross-referencing with daemon logs.
                console.info(`[QSO submit] duplicate uuid=${outcome.uuid}`);
                toasts.warn(`QSO with ${submittedCall} already logged; not re-logged.`);
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
        <Callsign id="call" label="Callsign" bind:value={qsoDraft.callsign} onenrich={handleEnrich}/>
        <Rst id="rst_sent" label="RST Sent" bind:value={qsoDraft.rstSent}/>
        <Rst id="rst_rcvd" label="RST Rcvd" bind:value={qsoDraft.rstRcvd}/>
        <Mode id="mode" label="Mode" bind:value={mode} list={modes} disabled={!displayedState.editable}/>
        <Vfos/>
    </div>
    <div class="flex flex-row space-x-2 mt-2">
        <TextInput id="name" label="Name" bind:value={qsoDraft.name}/>
        <TextInput id="qth" label="QTH" widthClass="w-46" bind:value={qsoDraft.qth}/>
        <Comment id="comment" label="Comment" bind:value={qsoDraft.comment}/>
    </div>
    <div class="flex flex-row space-x-2 -mt-2">
        <DateInput id="qso_date" label="Date" bind:value={qsoDraft.qsoDate}/>
        <TimeInput id="time_on" label="Time On (UTC)" bind:value={qsoDraft.timeOn}/>
        <TimeInput id="time_off" label="Time Off (UTC)" bind:value={qsoDraft.timeOff}/>
        <div class="flex flex-row space-x-2">
            <FormControls
                onClear={() => qsoDraft.clear()}
                onSubmit={submitQso}
                submitDisabled={!qsoDraft.canSubmit}
            />
        </div>
    </div>
</div>
