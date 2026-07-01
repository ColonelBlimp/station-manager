/**
 * In-progress QSO draft — the form fields the operator is typing into
 * before pressing Submit.
 *
 * Persistence tier: in-memory only. A half-typed QSO is per-page-load
 * activity; surviving a refresh would mislead more than help. Aligns
 * with `catState` / `bridgeState` rather than the persisted layers.
 *
 * **Reactivity rule (per the patterns memory).** Every form-bound field
 * uses `$state` because Svelte 5 `bind:value={...}` requires a reactive
 * source on the parent side. `qsoStarted` is also `$state` — it gates
 * the TimerControls Start/Stop button enable state (via QsoPanel's
 * `stopEnabled`/`startEnabled` $derived) and the F3 toggle shortcut.
 * `lookupCallsign` is `$state` for the same reason — it feeds the
 * `lookupDone` derived alongside `callsign`. Future enrichment-
 * populated fields (gridsquare, dxcc, country, cqZone, ituZone…)
 * default to plain unless a `bind:value` lands or a reactive consumer
 * appears; that's the leverage the module gives us as ADIF coverage
 * grows.
 *
 * **Lifecycle (settled session 28, captured here on the lift).**
 *   - Pre-QSO  (`qsoStarted === false`): `qsoDate`, `timeOn`, and
 *               `timeOff` paired-tick to current UTC via `tick()`.
 *               The form reflects "right now" without operator input.
 *   - Active   (`qsoStarted === true`): `qsoDate` and `timeOn` are
 *               pinned at the `startQso()` moment; `timeOff` continues
 *               to tick. ADIF QSO_DATE is the date the QSO STARTED, so
 *               pinning across midnight is semantically right.
 *
 * The ticker interval lives in `QsoPanel.svelte`'s component scope so
 * module load is side-effect-free; the panel calls `tick()` every 60s.
 *
 * **What stays out of qsoDraft.**
 *   - `mode` — it mirrors `displayedState` / `manualState` per ADR
 *     0009; it is a CAT-state concern, not a draft field. The panel
 *     keeps the local `let mode` and its two effects.
 *   - Identity fields (station_callsign, operator, owner_callsign,
 *     gridsquare, name, rig, antenna) — read from `configState` /
 *     the `station` store at submit time; not part of the draft.
 */

import { displayedState } from './displayed.svelte';
import { isValidCallsign } from '../validators/callsign';
import { isValidRst } from '../validators/rst';
import { formatUtcDate, formatUtcTime, formatUtcClock } from '../utils/time';

/**
 * RST default values — voice modes use the two-digit Readability /
 * Strength scale; CW adds a third tone digit.
 */
export const DEFAULT_RST_VOICE = '59';
export const DEFAULT_RST_CW = '599';

function rstDefaultFor(mode: string): string {
    return mode === 'CW' ? DEFAULT_RST_CW : DEFAULT_RST_VOICE;
}

class QsoDraft {
    /** Worked-station callsign. Tab on a valid value triggers `startQso()` + enrichment. */
    callsign: string = $state('');

    /** Worked operator's name. Enrichment may pre-fill; operator may edit. */
    name: string = $state('');

    /** Worked station's QTH (location). Enrichment may pre-fill; operator may edit. */
    qth: string = $state('');

    /** Operator's free-text comment. Maps to ADIF COMMENT. */
    comment: string = $state('');

    /**
     * Contacted station's TX power as estimated by the operator (watts).
     * Maps to ADIF RX_PWR. Operator-typed; QRZ doesn't reliably return
     * this. Digit-only string ("100", not "100W"); empty omits the
     * field on submit.
     */
    rxPwr: string = $state('');

    /**
     * Contacted station's rig / working conditions. Maps to ADIF RIG.
     * Operator-typed (QRZ's per-call rig data is sparse and
     * inconsistent — leaving it operator-only avoids stale auto-fill).
     */
    rig: string = $state('');

    /**
     * Operator's personal notes about this contact. Maps to ADIF NOTES
     * — distinct from `comment` (ADIF COMMENT, things to share during
     * the QSO). NOTES is for the operator's private record (e.g.,
     * "had bad QSB", "first SSB QSO with this op").
     */
    notes: string = $state('');

    /**
     * Operator's reminder flag — "I want to request a QSL card from
     * this contact". Custom field, not a standard ADIF QSL_* tag.
     * Stamped onto the QSO row via APP_SM_REQUEST_QSL='Y' on submit so
     * a future "QSOs awaiting QSL request" view can list them.
     */
    requestQsl: boolean = $state(false);

    /**
     * RST sent to the worked station. Defaults to the current mode's
     * canonical value at construction. On mode flips between voice and
     * CW the value is overwritten (see the effects below) — the
     * tradeoff is that an operator-typed value gets clobbered if mode
     * flips after they typed. Acceptable because `'59'` is meaningless
     * on CW and `'599'` is meaningless on voice; RST is typically set
     * after the contact, by which point the mode is settled.
     */
    rstSent: string = $state(rstDefaultFor(displayedState.mode));

    /** RST received from the worked station. Same default-fill / mode-flip behaviour as `rstSent`. */
    rstRcvd: string = $state(rstDefaultFor(displayedState.mode));

    /**
     * Date the QSO started, ADIF QSO_DATE — `YYYY-MM-DD`. Paired-ticks
     * to current UTC pre-QSO; pinned at `startQso()`.
     */
    qsoDate: string = $state(formatUtcDate(new Date()));

    /** Time the QSO started, ADIF TIME_ON — `HH:MM` UTC. Pinned at `startQso()`. */
    timeOn: string = $state(formatUtcTime(new Date()));

    /** Time the QSO ended, ADIF TIME_OFF — `HH:MM` UTC. Always ticks. */
    timeOff: string = $state(formatUtcTime(new Date()));

    /**
     * Full-precision UTC capture of the QSO-start instant, `HH:MM:SS`. Pinned at
     * `startQso()` and reset alongside the HH:MM fields. The visible `timeOn` stays
     * `HH:MM` for the uncluttered time input; `submitTimeOn()` reconciles the two so
     * the STORED/EXPORTED TIME_ON carries the real seconds when the minute is
     * unchanged (`:00` when the operator hand-edited the minute). The QSL manager's
     * OQRS matches on the full timestamp, so the seconds are load-bearing even
     * though they never show in the form. Imperative-only (read at submit), so a
     * plain field — no reactive consumer.
     */
    timeOnFull: string = formatUtcClock(new Date());

    /**
     * Lifecycle flag — true once the operator has Tabbed off a valid
     * callsign. Read only by imperative method bodies; no reactive
     * consumer, so plain field (no `$state` overhead).
     */
    qsoStarted: boolean = $state(false);

    /**
     * The normalized callsign (uppercased + trimmed) that `runLookup`
     * last fired for. Empty until F2 / Tab fires an enrichment+history
     * fetch. Compared against the current `callsign` in QsoPanel's
     * `lookupDone` derived: when they match, the Start button (and F3
     * while stopped) is enabled; when they don't, Start is disabled.
     *
     * Editing the callsign field naturally invalidates the gate
     * without any extra plumbing — the comparison fails the moment
     * the field diverges from the looked-up value. Re-typing the
     * original value back in re-enables the gate (cheap undo-typo).
     *
     * Stays set after `stopQso()` — Stop returns to "lookup done,
     * not started," from which Start is the natural next step. Only
     * `clear()` and a successful submit fully reset it.
     */
    lookupCallsign: string = $state('');

    /**
     * Default RST for the current mode — re-evaluates when
     * `displayedState.mode` flips between CW and voice. Consumed by the
     * default-fill effects below and by `clear()` to reset to a
     * mode-appropriate value.
     */
    defaultRst: string = $derived(rstDefaultFor(displayedState.mode));

    /**
     * Form-level required-ness gate. Per the patterns memory, validators
     * don't enforce presence; the form layer does. Required fields:
     * callsign (non-empty + valid), rstSent / rstRcvd (non-empty +
     * valid), qsoDate / timeOn / timeOff (non-empty). Mode is always
     * set (defaults). Frequency is always set (VFO defaults).
     */
    canSubmit: boolean = $derived(
        this.callsign.trim() !== '' &&
            isValidCallsign(this.callsign) === null &&
            this.rstSent.trim() !== '' &&
            isValidRst(this.rstSent) === null &&
            this.rstRcvd.trim() !== '' &&
            isValidRst(this.rstRcvd) === null &&
            this.qsoDate !== '' &&
            this.timeOn !== '' &&
            this.timeOff !== ''
    );

    /**
     * Reset every draft field to its initial value. Date / time are
     * recomputed against the current UTC moment so the next QSO starts
     * "now"; RST falls back to the current mode's default. Called by
     * `FormControls.onClear` and after a successful submit.
     */
    clear(): void {
        this.qsoStarted = false;
        this.lookupCallsign = '';
        this.callsign = '';
        this.name = '';
        this.qth = '';
        this.comment = '';
        this.rxPwr = '';
        this.rig = '';
        this.notes = '';
        this.requestQsl = false;
        this.rstSent = this.defaultRst;
        this.rstRcvd = this.defaultRst;
        const fresh = new Date();
        this.qsoDate = formatUtcDate(fresh);
        const freshTime = formatUtcTime(fresh);
        this.timeOn = freshTime;
        this.timeOff = freshTime;
        this.timeOnFull = formatUtcClock(fresh);
    }

    /**
     * Tab on a valid callsign = QSO start. Snap qsoDate / timeOn /
     * timeOff to current UTC at this exact moment (the paired ticker
     * may be up to 60s stale; re-snap so the pinned values are
     * accurate). Then set `qsoStarted` so qsoDate and timeOn pin while
     * timeOff keeps ticking.
     */
    startQso(): void {
        const fresh = new Date();
        this.qsoDate = formatUtcDate(fresh);
        const freshTime = formatUtcTime(fresh);
        this.timeOn = freshTime;
        this.timeOff = freshTime;
        this.timeOnFull = formatUtcClock(fresh); // pin the real start second
        this.qsoStarted = true;
    }

    /**
     * Stop button / F3 toggle / explicit abandon — revert to pre-QSO
     * paired-ticking state WITHOUT clearing typed fields (callsign /
     * name / RST / etc.). Symmetric to `startQso()`: resnap the three
     * time fields to current UTC so the operator sees an instant
     * "fresh start from now" rather than a stale pinned snapshot
     * lingering until the next 1s tick. The next paired tick
     * (qsoStarted=false branch) keeps all three advancing in lockstep
     * until the operator decides to commit (Tab / Start) or clear
     * (ESC / Clear button).
     */
    stopQso(): void {
        const fresh = new Date();
        this.qsoDate = formatUtcDate(fresh);
        const freshTime = formatUtcTime(fresh);
        this.timeOn = freshTime;
        this.timeOff = freshTime;
        this.timeOnFull = formatUtcClock(fresh);
        this.qsoStarted = false;
    }

    /**
     * Paired-tick advance. The panel runs a 1s `setInterval` and calls
     * this. Pre-QSO: qsoDate / timeOn / timeOff all advance. Active:
     * only timeOff advances. Writes only happen on minute crossings
     * (the comparison is against the HH:MM string), so per-second
     * ticks are no-ops most of the time — the work is one Date+format
     * per second. The 1s rate ensures the field catches the minute
     * boundary within ~1s of it actually crossing rather than up to
     * 60s late.
     *
     * Manual edits to a tick-driven field get clobbered on the next
     * minute boundary, which gives the operator up to 59s to type
     * and Submit / Tab — plenty for back-logging.
     */
    tick(): void {
        const now = new Date();
        const utcTime = formatUtcTime(now);
        if (!this.qsoStarted) {
            const utcDate = formatUtcDate(now);
            if (utcDate !== this.qsoDate) this.qsoDate = utcDate;
            if (utcTime !== this.timeOn) {
                this.timeOn = utcTime;
                this.timeOnFull = formatUtcClock(now);
            }
        }
        if (utcTime !== this.timeOff) this.timeOff = utcTime;
    }

    /**
     * TIME_ON for storage/export as `HH:MM:SS`. Reconciles the visible `HH:MM`
     * with the full-precision start capture (`timeOnFull`): same minute → the real
     * seconds; a hand-edited minute → `:00`. Called at submit only.
     */
    submitTimeOn(): string {
        return reconcileSeconds(this.timeOn, this.timeOnFull);
    }

    /**
     * TIME_OFF for storage/export as `HH:MM:SS`. TIME_OFF is the log instant, so
     * the seconds are captured NOW at submit: if the visible minute still matches
     * the current minute, the real current seconds; otherwise `:00`.
     */
    submitTimeOff(): string {
        return reconcileSeconds(this.timeOff, formatUtcClock(new Date()));
    }
}

/**
 * Combine a visible `HH:MM` with a full-precision `HH:MM:SS` capture: same minute
 * → return the seconds-bearing capture; otherwise the minute was hand-changed, so
 * return `HH:MM:00`. A non-`HH:MM` visible value (blank/partial) is returned
 * unchanged — the form's presence gate handles emptiness. The ADIF submit strips
 * the colons, so `HH:MM:SS` → `HHMMSS`.
 */
function reconcileSeconds(visible: string, full: string): string {
    if (visible.length !== 5) return visible;
    if (full.length === 8 && full.slice(0, 5) === visible) return full;
    return visible + ':00';
}

export const qsoDraft = new QsoDraft();

/**
 * RST default-fill effect. `$effect.root` is needed because this runs
 * at module level (outside a component lifecycle).
 *
 * Mode-flip overwrite — refill both fields when `defaultRst` changes
 * (i.e. mode crosses the CW ↔ voice boundary). Tracks `defaultRst`
 * only, so it does not re-fire on same-family mode changes
 * (USB ↔ LSB).
 *
 * Note on what's intentionally NOT here: an "if rstSent === '' refill
 * to default" effect was removed because it interfered with normal
 * editing — deleting digits right-to-left would snap the field back
 * to the default the moment the last digit went, frustrating the
 * operator. The form-level submit gate already requires non-empty
 * RST, so an empty field can't slip through unnoticed.
 */
let rootDispose: (() => void) | null = $effect.root(() => {
    $effect(() => {
        const d = qsoDraft.defaultRst;
        qsoDraft.rstSent = d;
        qsoDraft.rstRcvd = d;
    });
});

/**
 * Test-time teardown. Production code never calls this (module
 * lifetime = page lifetime). Vitest cases that exercise this state
 * module call it between cases so the singleton `$effect.root` doesn't
 * leak listeners across tests. Mirrors `bridge.svelte.ts → stopBridge()`.
 */
export function _disposeForTests(): void {
    if (rootDispose) {
        rootDispose();
        rootDispose = null;
    }
}
