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
 * source on the parent side. The lifecycle flag `qsoStarted` is a plain
 * field — it is read only by imperative function bodies (`tick`,
 * `startQso`), never by a template, `$derived`, or `$effect`. Future
 * enrichment-populated fields (gridsquare, dxcc, country, cqZone,
 * ituZone…) default to plain unless a `bind:value` lands; that's the
 * leverage the module gives us as ADIF coverage grows.
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
import { formatUtcDate, formatUtcTime } from '../utils/time';

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
     * Lifecycle flag — true once the operator has Tabbed off a valid
     * callsign. Read only by imperative method bodies; no reactive
     * consumer, so plain field (no `$state` overhead).
     */
    qsoStarted: boolean = false;

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
            isValidCallsign(this.callsign) &&
            this.rstSent.trim() !== '' &&
            isValidRst(this.rstSent) &&
            this.rstRcvd.trim() !== '' &&
            isValidRst(this.rstRcvd) &&
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
        this.qsoStarted = true;
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
            if (utcTime !== this.timeOn) this.timeOn = utcTime;
        }
        if (utcTime !== this.timeOff) this.timeOff = utcTime;
    }
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
$effect.root(() => {
    $effect(() => {
        const d = qsoDraft.defaultRst;
        qsoDraft.rstSent = d;
        qsoDraft.rstRcvd = d;
    });
});
