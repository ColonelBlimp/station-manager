<script lang="ts">
    import { onDestroy, onMount } from 'svelte';
    import Callsign from '../components/Callsign.svelte';
    import Rst from '../components/Rst.svelte';
    import Mode from '../components/Mode.svelte';
    import Vfos from '../components/Vfos.svelte';
    import { configState } from '../../states/config.svelte';
    import { displayedState } from '../../states/displayed.svelte';
    import { manualState } from '../../states/manual.svelte';
    import { qsoDefaults } from '../../states/qsoDefaults.svelte';
    import { qsoDraft } from '../../states/qsoDraft.svelte';
    import { commentHistory } from '../../states/commentHistory.svelte';
    import TextInput from '../components/TextInput.svelte';
    import Comment from '../components/Comment.svelte';
    import DateInput from '../components/DateInput.svelte';
    import TimeInput from '../components/TimeInput.svelte';
    import FormControls from '../components/FormControls.svelte';
    import { formatAdifRecord } from '../../utils/adif';
    import { frequencyToBand } from '../../utils/frequency';
    import { submitQso as submitQsoToDaemon } from '../../api/qso';
    import { enrichCallsign } from '../../api/enrichment';
    import { fetchContactHistory } from '../../api/contact-history';
    import { enrichmentState } from '../../states/enrichment.svelte';
    import { contactHistoryState } from '../../states/contactHistory.svelte';
    import { sessionQsosState } from '../../states/sessionQsos.svelte';
    import { qsoEditState } from '../../states/qsoEdit.svelte';
    import { toasts } from '../../states/toasts.svelte';
    import { isValidCallsign } from '../../validators/callsign';
    import { callsignStack } from '../../states/callsignStack.svelte';
    import { swapVfo, bandUp, bandDown, selectBand } from '../../actions/rigControl';
    import TimerControls from '../components/TimerControls.svelte';

    /*
        Hardcoded until a logbook switcher lands. The daemon seeds a
        default logbook at id=1 on first run, so this matches the
        running-daemon default. When the switcher arrives this becomes
        a derived value from a logbook store seeded from `GET /v1/logbook`.
    */
    const DEFAULT_LOGBOOK_ID = 1;

    /*
        Operator-friendly mode list for the dropdown. These are the
        names operators say at the mic (USB, FT8, PSK31, CW) — the
        ADIF main-vs-submode split happens inside displayedState's
        derivations via resolveModeAndSubmode. List grows / shrinks
        with operator preference; if a rig pushes a mode mapped to a
        value outside this list (e.g. operator's My Station mapping
        says DATA-U → "JS8" and JS8 isn't in baseModes), the dynamic
        `modes` $derived below adds it so the dropdown can display
        the live value when CAT is in charge.
    */
    const baseModes = ['USB', 'LSB', 'CW', 'FM', 'AM', 'RTTY', 'FT8', 'FT4', 'PSK31'];

    /*
        Operator-friendly view of the current mode for the dropdown:
        SUBMODE wins over MODE when present (so a {SSB, USB} pair
        shows as "USB" not "SSB"; an {FT8, ''} pair shows as "FT8").
        This collapses the ADIF MODE/SUBMODE pair back into the
        single string the operator picks from.
    */
    // Two-way bind across two reactive stores: read from displayedState
    // (which already produces ADIF values via either the rig-mode
    // mapping lookup or resolveModeAndSubmode of manualState's friendly
    // pick); operator writes route back into manualState's friendly
    // form only when `editable`, so a CAT-driven mode change while the
    // rig is live can't clobber the snapshot kept for the disconnect
    // path. `let mode = $state(...)` + mirror-effect is the standard
    // Svelte 5 idiom for this — a plain $derived would be read-only
    // and break the <Mode bind:value>.
    // eslint-disable-next-line svelte/prefer-writable-derived
    let mode = $state(displayedState.subMode || displayedState.mode);
    $effect(() => {
        mode = displayedState.subMode || displayedState.mode;
    });
    $effect(() => {
        if (displayedState.editable) {
            manualState.mode = mode;
        }
    });

    // Dynamically include the current value in the dropdown options
    // so a CAT-pushed mode the operator's friendly list doesn't
    // already contain (e.g. mapping points at "JS8") still displays.
    const modes = $derived(baseModes.includes(mode) ? baseModes : [...baseModes, mode]);

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
        TimerControls gating. The Start / Stop buttons are NOT a
        pure `qsoStarted` toggle — there's an intermediate state
        ("F2 has run a lookup for this callsign, operator hasn't
        committed yet") that Start must be enabled in and Stop
        disabled in. Three-state machine:

          1. No callsign typed OR no lookup done for current callsign
             → both buttons disabled (nothing to start, nothing to
             stop).
          2. Callsign typed + F2 lookup done (or post-Stop)
             → Start enabled, Stop disabled. Operator can commit.
          3. Tab pressed OR Start clicked OR F3 while stopped+ready
             → Stop enabled, Start disabled. Timer running.

        `lookupDone` compares the normalized typed callsign against
        `qsoDraft.lookupCallsign` (the call F2 / Tab last fired for).
        Editing the callsign field auto-invalidates the gate the
        moment the comparison fails; re-typing the original value
        back in re-enables it. F3 mirrors the button gates exactly —
        no lookup done → F3 is a silent no-op while stopped.
    */
    const lookupDone = $derived(
        qsoDraft.lookupCallsign !== '' &&
            qsoDraft.lookupCallsign === qsoDraft.callsign.trim().toUpperCase()
    );
    const startEnabled = $derived(lookupDone && !qsoDraft.qsoStarted);
    const stopEnabled = $derived(qsoDraft.qsoStarted);

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

    /*
        runLookup — pure enrichment + contact-history fetch, no
        QSO-timer side effect. Shared between Tab (handleEnrich, which
        also calls startQso) and F2 (handleLookupShortcut, which does
        not). Splitting these is what makes F2's "look up the call
        but don't commit yet" semantics possible — the operator in a
        pile-up can scan station info before deciding whether the
        contact is worth the timer flip.
    */
    function runLookup(call: string): void {
        // Record the looked-up callsign synchronously, before any
        // fetch returns. The TimerControls Start-button gate (and F3
        // while stopped) is derived from `qsoDraft.lookupCallsign ===
        // current normalized callsign`, so setting it here is what
        // flips Start from disabled → enabled after F2 / Tab. We
        // don't wait for the network because: (a) cache hits return
        // in <100ms and the operator can't react that fast anyway;
        // (b) enrichment is allowed to fail — the "Enrichment never
        // blocks logging" invariant means the operator should still
        // be able to commit (press Start, or hit Tab/F3) even if
        // QRZ/hamnut are down.
        qsoDraft.lookupCallsign = call;
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

    function handleEnrich(call: string): void {
        if (!qsoDraft.qsoStarted) {
            qsoDraft.startQso();
        }
        runLookup(call);
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

        // displayedState.mode / .subMode are already ADIF-resolved
        // — when CAT is live they come from the per-rig mode-mappings
        // table (rigdef defaults + operator overrides merged at the
        // daemon); when CAT is off they come from
        // resolveModeAndSubmode running inside displayedState over
        // the operator's manualState pick. Either way the values
        // here are the ones the QSO record should carry.
        const resolved = { mode: displayedState.mode, subMode: displayedState.subMode };

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
            // MY_RIG falls back to the rigdef's human-readable name
            // (configState.station.rigName, resolved daemon-side from
            // bridge.cat.driver and exposed via /v1/config). The
            // operator's My Station → My Rig text override stays
            // authoritative; when both are empty MY_RIG is omitted
            // from the ADIF record.
            myRig: ls.myRig || displayedState.rigName,
            myAntenna: ls.myAntenna,
            myMorseKeyType: ls.myMorseKeyType,
            myMorseKeyInfo: ls.myMorseKeyInfo,
            // ANT_AZ — bearing for the operator's currently-selected
            // path (short or long) from the country panel. Empty when
            // either grid is missing; the ADIF emitter omits ANT_AZ on
            // empty so the record is clean rather than carrying a
            // fabricated zero.
            antAz: enrichmentState.activeBearing || undefined,
            // Contacted-station enrichment — capture what the Country/Details
            // panels resolved so the logged QSO (and its QRZ/ClubLog upload +
            // ADIF export) carries the entity, zones, DXCC and grid instead of
            // defaulting COUNTRY to "Unknown". COUNTRY/CQZ/ITUZ come from the
            // country layer (what the panels show, always present once enriched);
            // DXCC + grid come from the QRZ station lookup and are empty when the
            // station is unknown. Each omitted when empty.
            country: enrichmentState.result?.country?.name || undefined,
            cqZone: enrichmentState.result?.country?.cq_zone || undefined,
            ituZone: enrichmentState.result?.country?.itu_zone || undefined,
            dxcc: enrichmentState.result?.station?.dxcc || undefined,
            gridsquare: enrichmentState.result?.station?.gridsquare || undefined,
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
                // Save the logged comment to the paste list (newest at
                // top, deduped, capped) so it's offered back on the
                // Comment field's ▾ dropdown for the next QSO. add()
                // ignores empty, so a blank comment doesn't pollute the
                // list. Only the 'stored' path feeds it — duplicates /
                // failures don't add a comment that wasn't actually
                // logged.
                commentHistory.add(qsoDraft.comment.trim());
                qsoDraft.clear();
                // Country + Worked panels return to the empty state —
                // every QSO is a clean slate. Operator's next Tab
                // populates the panels afresh with the new callsign's
                // data (and the freshly-stored QSO will show up in
                // the next Worked-panel fetch if the operator re-Tabs
                // the same call).
                enrichmentState.clear();
                contactHistoryState.clear();
                focusCallsign();
                // Refresh the default-logbook QSO count so the
                // LoggingCard header reflects the new row. Fire-and-forget
                // — a refresh failure leaves the previous count visible
                // until the next submit succeeds.
                void configState.refreshLogbookCount();
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

    /*
        Clear handler — shared between the FormControls Clear button
        and the ESC keyboard shortcut. Wipes the draft, enrichment
        result, and contact history so the next QSO starts from a
        clean slate (matching the post-stored UX rule). Refocuses
        the Callsign field so the operator can immediately start
        typing the next call.
    */
    function clearForm(): void {
        qsoDraft.clear();
        enrichmentState.clear();
        contactHistoryState.clear();
        focusCallsign();
    }

    /*
        focusCallsign — imperative focus on the Callsign input.

        Bypasses component encapsulation by reaching for the input by
        its stable DOM id (`call` — passed unchanged as the `id` prop
        below). Pragmatic for a personal logging app where the
        operator's hands stay on the keyboard between QSOs:
        post-clear and post-stored focus restoration is what makes the
        single-handed flow work. If a second `Callsign` instance ever
        lands on the page, this becomes ambiguous — promote to a
        component-exported `focus()` then.

        Called by:
          - onMount, so the cursor lands in Callsign on first render.
          - clearForm, so ESC / the Clear button both restore focus.
          - submitQso `case 'stored'`, so Ctrl+Enter / Submit on a
            successful log return focus for the next QSO. Failure
            paths (duplicate, validation, server, network) leave the
            form populated and don't move focus — the operator
            decides what to fix.
    */
    function focusCallsign(): void {
        document.getElementById('call')?.focus();
    }

    onMount(() => {
        focusCallsign();
    });

    /*
        handleLookupShortcut — F2's lookup-only path. Reads the
        currently-typed callsign, validates it the same way the
        Callsign component does on Tab, and fires runLookup WITHOUT
        starting the QSO. The pile-up workflow: operator types a
        call, hits F2 to scan station info / DXCC / contact history,
        decides whether to commit. Tab is the commit signal; F2 is
        the peek signal.

        Empty or invalid callsign → silent no-op. The Callsign
        component's own inline error UI is enough; an extra toast
        would be noise in the at-speed pile-up case this shortcut
        exists for.
    */
    function handleLookupShortcut(): void {
        const trimmed = qsoDraft.callsign.trim();
        if (trimmed === '' || isValidCallsign(trimmed) !== null) return;
        runLookup(trimmed.toUpperCase());
    }

    /*
        handleStack — Shift+Enter (and the Callsign icon click). Push
        the currently-typed callsign onto callsignStack and reuse the
        existing Clear flow to reset the per-callsign draft state.

        Symmetric with the operator's existing ESC muscle memory:
        "set aside this contact and start fresh." Identical effect on
        the draft + enrichment + history; only the side effect of
        pushing the call onto the stack differs.

        Validation matches F2 / Tab — empty or invalid is a silent
        no-op. The icon affordance in Callsign fires this same handler
        via the onstack callback, so both keyboard and mouse paths go
        through one place.
    */
    function handleStack(): void {
        const trimmed = qsoDraft.callsign.trim();
        if (trimmed === '' || isValidCallsign(trimmed) !== null) return;
        callsignStack.push(trimmed.toUpperCase());
        clearForm();
    }

    /*
        loadPopped — shared tail for the three pop affordances
        (Shift+Up, Shift+Down, click an entry in StackingDrawer; the
        click path lives in StackingDrawer itself but mirrors this
        behaviour). Writes the popped call into qsoDraft.callsign and
        focuses the input so the operator can immediately Tab / F2.

        Pop is destructive overwrite of qsoDraft.callsign by design —
        if there was a half-typed call in the field, it's lost. The
        operator who wants to preserve it must Shift+Enter first. The
        "no undo" rule keeps the verb count at two (stack / pop) and
        avoids a swap semantics that would surprise more often than
        it would help. Other draft fields are untouched.
    */
    function loadPopped(call: string | undefined): void {
        if (call === undefined) return;
        qsoDraft.callsign = call;
        focusCallsign();
    }

    function handlePopTop(): void {
        loadPopped(callsignStack.popTop());
    }

    function handlePopBottom(): void {
        loadPopped(callsignStack.popBottom());
    }

    /*
        Window-level keyboard shortcuts.

          - ESC               → Clear (same as the FormControls Clear button).
          - Ctrl/Cmd+Enter    → Submit (same as Log QSO; metaKey supports Cmd
                                on macOS so the shortcut feels native there).
          - Shift+Enter       → Stack: push current callsign onto the
                                pile-up stack and reset the per-callsign
                                draft (same effect as ESC, plus the
                                push). Empty / invalid is a silent no-op.
          - Shift+ArrowUp     → Pop the newest stacked callsign into
                                the Callsign field. No-op when the
                                stack is empty.
          - Shift+ArrowDown   → Pop the oldest stacked callsign into
                                the Callsign field. No-op when the
                                stack is empty.
          - F2                → Lookup-only: enrichment + contact-history
                                fetch for the typed callsign without
                                starting the QSO timer (ADR 0007 amendment).
                                Function key, never collides with typing,
                                so it fires regardless of focus.
          - F3                → Mirror whichever TimerControls button is
                                currently enabled. Stop if running; Start
                                if stopped + lookup-done; silent no-op
                                otherwise (no callsign + lookup, nothing
                                to start). Function key, no typing
                                collision, fires regardless of focus.

        All are no-ops when the QsoEditOverlay is open — the
        overlay's own ESC handler should win that case (otherwise
        pressing ESC to dismiss the overlay would also wipe the live
        draft beneath it). Submit is gated on `qsoDraft.canSubmit` to
        mirror the button's disabled state — Ctrl+Enter doesn't
        bypass validation.

        Shift+Enter is matched explicitly without Ctrl/Meta so the
        order of the Ctrl/Meta-Enter and Shift-Enter branches doesn't
        matter — they're disjoint.

        preventDefault on Ctrl+Enter as belt-and-braces against any
        future surrounding <form> default; not strictly needed today
        because QsoPanel renders no <form> element. preventDefault on
        F2 / F3 defangs any browser-level binding (some browsers map
        F2 to in-page rename / focus-bookmarks-bar; F3 is "find next"
        in most browsers — we explicitly want the timer toggle, not
        find-next). preventDefault on Shift+ArrowUp/Down stops the
        browser's native shift-select-line scroll from firing
        alongside the pop.
    */
    // Shift+Ctrl+<digit> → direct band jump (Firefox grabs Alt+digit for tabs,
    // so the whole rig-control family lives on Shift+Ctrl). Keyed by physical
    // key code (e.code) because Shift+digit yields a symbol (!@#…), not the
    // digit. 60m (BS 02) is omitted here — reached via band-step.
    const bandByDigit: Record<string, string> = {
        Digit1: '160m',
        Digit2: '80m',
        Digit3: '40m',
        Digit4: '30m',
        Digit5: '20m',
        Digit6: '17m',
        Digit7: '15m',
        Digit8: '12m',
        Digit9: '10m',
        Digit0: '6m',
    };

    function handleKeydown(e: KeyboardEvent): void {
        if (qsoEditState.open) return;

        if (e.key === 'Escape') {
            clearForm();
            return;
        }
        if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            if (qsoDraft.canSubmit) {
                void submitQso();
            }
            return;
        }
        if (e.key === 'Enter' && e.shiftKey && !e.ctrlKey && !e.metaKey) {
            e.preventDefault();
            handleStack();
            return;
        }
        if (e.shiftKey && e.key === 'ArrowUp') {
            e.preventDefault();
            handlePopTop();
            return;
        }
        if (e.shiftKey && e.key === 'ArrowDown') {
            e.preventDefault();
            handlePopBottom();
            return;
        }
        if (e.key === 'F2') {
            e.preventDefault();
            handleLookupShortcut();
            return;
        }
        if (e.key === 'F3') {
            e.preventDefault();
            // Mirror the button gates exactly: F3 is the
            // focus-independent equivalent of clicking whichever
            // TimerControls button is currently enabled. If neither
            // is enabled (no lookup done yet), F3 is a silent no-op
            // — same as clicking a disabled button.
            if (stopEnabled) {
                qsoDraft.stopQso();
            } else if (startEnabled) {
                qsoDraft.startQso();
            }
        }
        // Rig-control commands — one family, all Shift+Ctrl+<key>, dispatched
        // on the PHYSICAL key (e.code) because Shift mutates the character
        // (Shift+] = }, Shift+1 = !). Keeps CAT control in its own modifier
        // namespace, distinct from the logging shortcuts (ADR 0007); Alt+digit
        // is Firefox tab-switch, so the family lives on Ctrl+Shift. CAT-live +
        // capability gating lives in the rigControl actions.
        if (e.ctrlKey && e.shiftKey) {
            if (e.code === 'Backslash') {
                e.preventDefault();
                swapVfo();
                return;
            }
            if (e.code === 'BracketRight') {
                e.preventDefault();
                bandUp();
                return;
            }
            if (e.code === 'BracketLeft') {
                e.preventDefault();
                bandDown();
                return;
            }
            if (bandByDigit[e.code]) {
                e.preventDefault();
                selectBand(bandByDigit[e.code]);
                return;
            }
        }
    }
</script>

<svelte:window onkeydown={handleKeydown} />

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
            onstack={handleStack}
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
        <Comment
            id="comment"
            label="Comment"
            bind:value={qsoDraft.comment}
            history={commentHistory.items}
            onpick={(text: string) => {
            qsoDraft.comment = text;
        }}
        />
    </div>
    <div class="flex flex-row space-x-2 -mt-2">
        <DateInput id="qso_date" label="Date" bind:value={qsoDraft.qsoDate} />
        <TimeInput id="time_on" label="Time On (UTC)" bind:value={qsoDraft.timeOn} />
        <TimeInput id="time_off" label="Time Off (UTC)" bind:value={qsoDraft.timeOff} />
        <TimerControls
            startDisabled={!startEnabled}
            stopDisabled={!stopEnabled}
            onStart={() => qsoDraft.startQso()}
            onStop={() => qsoDraft.stopQso()}
        />
        <div class="flex flex-row">
            <FormControls
                onClear={clearForm}
                onSubmit={submitQso}
                submitDisabled={!qsoDraft.canSubmit}
            />
        </div>
    </div>
</div>
