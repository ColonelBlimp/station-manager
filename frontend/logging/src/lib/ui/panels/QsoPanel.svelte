<script lang="ts">
    import { onDestroy } from 'svelte';
    import Callsign from '../components/Callsign.svelte';
    import Rst from '../components/Rst.svelte';
    import Mode from '../components/Mode.svelte';
    import Vfos from '../components/Vfos.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import { qsoDefaults } from '../../states/qsoDefaults.svelte';
    import { qsoDraft } from '../../states/qsoDraft.svelte';
    import TextInput from '../components/TextInput.svelte';
    import Comment from '../components/Comment.svelte';
    import DateInput from '../components/DateInput.svelte';
    import TimeInput from '../components/TimeInput.svelte';
    import FormControls from '../components/FormControls.svelte';
    import { formatAdifRecord } from '../../utils/adif';
    import { frequencyToBand } from '../../utils/frequency';
    import { resolveModeAndSubmode } from '../../utils/mode';
    import { submitQso as submitQsoToDaemon } from '../../api/qso';
    import { enrichCallsign } from '../../api/enrichment';
    import { fetchContactHistory } from '../../api/contact-history';
    import { enrichmentState } from '../../states/enrichment.svelte';
    import { contactHistoryState } from '../../states/contactHistory.svelte';
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
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
    // Two-way bind across two reactive stores: read from displayedState
    // (rig-mirror or manualState, picked by the ADR 0009 flag rule);
    // operator writes route back into manualState only when `editable`,
    // so a CAT-driven mode change while the rig is live can't clobber
    // the snapshot kept for the disconnect path. `let mode = $state(...)`
    // + mirror-effect is the standard Svelte 5 idiom for this — a plain
    // $derived would be read-only and break the <Mode bind:value>.
    // eslint-disable-next-line svelte/prefer-writable-derived
    let mode = $state(displayedState.mode);
    $effect(() => {
        mode = displayedState.mode;
    });
    $effect(() => {
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
        Callsign field with a valid callsign. Per ADR 0017, the daemon's
        `GET /v1/enrich/callsign?call=X` endpoint always returns 200 with
        a Result envelope (provider failures collapse to source=none,
        empty payloads).

        Populate policy: overwrite name/qth on every ok response. A
        fresh Tab means the operator wants enrichment for this call;
        if they had typed a value they wanted preserved, they would
        not be Tabbing. They can re-edit either field after population
        to suit personal taste (case, abbreviation, etc.).

        Failure outcomes (network/server/validation) leave the form
        untouched — the "enrichment never blocks logging" invariant.
        No toast on enrichment failure: a flaky daemon or upstream
        must not distract the operator from logging. The startQso()
        flip happens unconditionally because Tab is the QSO-start
        signal regardless of the network result.
    */
    /*
        SLOW_LOOKUP_THRESHOLD_MS — only show the "Looking up..."
        toast if the daemon takes longer than this. Cache hits return
        in <100ms; flashing a toast every Tab is noise. The threshold
        is generous enough that a healthy local-cache hit doesn't
        trigger it but tight enough that an operator on a slow link
        gets feedback before they wonder if anything is happening.
    */
    const SLOW_LOOKUP_THRESHOLD_MS = 500;

    function handleEnrich(call: string): void {
        if (!qsoDraft.qsoStarted) {
            qsoDraft.startQso();
        }
        // Sticky info-toast for slow lookups so the operator (on a
        // flaky internet link) can distinguish "still working" from
        // "panel didn't update because nothing happened." Delayed by
        // SLOW_LOOKUP_THRESHOLD_MS so cache hits never see it; both
        // the timer and the toast are cleaned up when the response
        // lands, regardless of outcome.
        let lookingUpId: number | null = null;
        const showToastTimer = setTimeout(() => {
            lookingUpId = toasts.info(`Looking up ${call}...`, 0);
        }, SLOW_LOOKUP_THRESHOLD_MS);
        // Contact-history fetch runs in parallel with enrichment.
        // It's cheap (single indexed query, no upstream calls) and
        // the WorkedPanel needs to populate independently of whether
        // QRZ/hamnut respond. Failures are silent — empty
        // history is the same operator-visible outcome as a network
        // error here, and a toast on every flaky-link Tab would be
        // noise.
        void fetchContactHistory(call).then((outcome) => {
            if (outcome.kind !== 'ok') return;
            contactHistoryState.setResult(outcome.items);
        });
        void enrichCallsign(call).then((outcome) => {
            clearTimeout(showToastTimer);
            if (lookingUpId !== null) {
                toasts.dismiss(lookingUpId);
            }
            if (outcome.kind !== 'ok') return;
            const r = outcome.result;
            // Push to enrichment state regardless of station result —
            // the country panel renders country info even when the
            // station layer is empty (long-prefix-match still gives
            // us country/zone/local-time data the operator may want).
            enrichmentState.setResult(r);
            // Station not found in any callsign-class provider AND
            // no cached row — the form's name/QTH won't auto-fill.
            // Surface as a warn so the operator can distinguish
            // "providers responded with no record" from "network is
            // down, retry in a moment" (an operator on a flaky link
            // would otherwise assume the latter and wait). Country
            // status doesn't gate the toast: country comes from a
            // longest-prefix-match, so almost any callsign hits the
            // country layer even when the station is unknown — using
            // station_source as the signal matches what the operator
            // actually cares about (the QSO form auto-fill).
            if (r.station_source === 'none') {
                toasts.warn(`Lookup: ${call} not found`);
                return;
            }
            const station = r.station;
            if (station === undefined) return;
            if (typeof station.name === 'string') {
                qsoDraft.name = station.name;
            }
            if (typeof station.qth === 'string') {
                qsoDraft.qth = station.qth;
            }
        });
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

        const selectedHz =
            displayedState.selectedVfo === 'A' ? displayedState.vfoA : displayedState.vfoB;
        const otherHz =
            displayedState.selectedVfo === 'A' ? displayedState.vfoB : displayedState.vfoA;

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
            // ANT_AZ — bearing for the operator's currently-selected
            // path (short or long) from the country panel. Empty when
            // either grid is missing; the ADIF emitter omits ANT_AZ on
            // empty so the record is clean rather than carrying a
            // fabricated zero.
            antAz: enrichmentState.activeBearing || undefined,
            // Per-QSO Details panel fields. Emitter omits each when
            // empty / false; the operator can leave any of them blank.
            rxPwr: qsoDraft.rxPwr.trim() || undefined,
            rig: qsoDraft.rig.trim() || undefined,
            notes: qsoDraft.notes.trim() || undefined,
            appSmRequestQsl: qsoDraft.requestQsl,
        });

        const outcome = await submitQsoToDaemon(adif, DEFAULT_LOGBOOK_ID);
        switch (outcome.kind) {
            case 'stored':
                // Snapshot session-row fields BEFORE the clears below
                // wipe the draft + enrichment state. country and
                // distanceKm in particular live on enrichmentState
                // and would otherwise be empty by the time the row
                // renders.
                sessionQsosState.add({
                    uuid: outcome.uuid,
                    callsign: submittedCall,
                    name: qsoDraft.name.trim(),
                    freqHz: txFreqHz,
                    band: frequencyToBand(txFreqHz),
                    rstSent: qsoDraft.rstSent,
                    rstRcvd: qsoDraft.rstRcvd,
                    mode: displayedState.mode,
                    timeOn: qsoDraft.timeOn,
                    qsoDate: qsoDraft.qsoDate,
                    country: enrichmentState.result?.country?.name ?? '',
                    distanceKm: enrichmentState.activeDistanceKm,
                    adif,
                });
                qsoDraft.clear();
                // Country + Worked panels return to the empty state —
                // every QSO is a clean slate. Operator's next Tab
                // populates the panels afresh with the new callsign's
                // data (and the freshly-stored QSO will show up in
                // the next Worked-panel fetch if the operator re-Tabs
                // the same call).
                enrichmentState.clear();
                contactHistoryState.clear();
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
        <Callsign
            id="call"
            label="Callsign"
            bind:value={qsoDraft.callsign}
            onenrich={handleEnrich}
        />
        <Rst id="rst_sent" label="RST Sent" bind:value={qsoDraft.rstSent} />
        <Rst id="rst_rcvd" label="RST Rcvd" bind:value={qsoDraft.rstRcvd} />
        <Mode
            id="mode"
            label="Mode"
            bind:value={mode}
            list={modes}
            disabled={!displayedState.editable}
        />
        <Vfos />
    </div>
    <div class="flex flex-row space-x-2 mt-2">
        <TextInput id="name" label="Name" bind:value={qsoDraft.name} />
        <TextInput id="qth" label="QTH" widthClass="w-46" bind:value={qsoDraft.qth} />
        <Comment id="comment" label="Comment" bind:value={qsoDraft.comment} />
    </div>
    <div class="flex flex-row space-x-2 -mt-2">
        <DateInput id="qso_date" label="Date" bind:value={qsoDraft.qsoDate} />
        <TimeInput id="time_on" label="Time On (UTC)" bind:value={qsoDraft.timeOn} />
        <TimeInput id="time_off" label="Time Off (UTC)" bind:value={qsoDraft.timeOff} />
        <div class="flex flex-row space-x-2">
            <FormControls
                onClear={() => {
                    qsoDraft.clear();
                    enrichmentState.clear();
                    contactHistoryState.clear();
                }}
                onSubmit={submitQso}
                submitDisabled={!qsoDraft.canSubmit}
            />
        </div>
    </div>
</div>
