/**
 * Edit-overlay state singleton — the working copy for the QSO edit
 * overlay opened from a SessionPanel row click.
 *
 * Per the operator's "completely independent of the draft" rule, this
 * module has no shared state with `qsoDraft`. The overlay edits a
 * historical record and must not contaminate the in-progress draft —
 * a half-finished edit must never bleed into "the next QSO I'm logging".
 *
 * Persistence tier: in-memory only. An open overlay is per-page-load
 * UX state; surviving a refresh would mislead. Aligns with `qsoDraft`,
 * `catState`, and `bridgeState` rather than the persisted layers
 * (sessionStorage / localStorage).
 *
 * Reactivity rule: every form-bound field uses `$state` so Svelte 5
 * `bind:value={...}` works on the parent side. Lifecycle flags (open,
 * loading, saving) are also `$state` because the overlay's render and
 * save button reactively branch on them.
 *
 * Date / time format note: the form components (DateInput, TimeInput)
 * expect `YYYY-MM-DD` and `HH:MM` shapes. The daemon stores times at
 * minute precision (the sqlite schema's `length(time_on)=4` CHECK, and
 * the service truncates any accepted HHMMSS to HHMM — review 2026-06-19),
 * so the GET response is always `HHMM`. `open()` converts the daemon's
 * canonical form into the SPA-friendly form on load; the daemon's PATCH
 * path normalises either way (sanitizes dashes and colons), so no inverse
 * conversion is needed on save.
 */

// `DaemonQsoForEdit` is the wire shape for GET/PATCH /v1/qso/{uuid}; it
// belongs to the API layer, not the reactive-state layer, so it's
// defined alongside the fetch wrappers and re-exported here as a
// type for backwards-compatible imports from existing call sites
// (e.g. `qsoEdit.test.ts`).
export type { DaemonQsoForEdit } from '../api/qso-update';
import type { DaemonQsoForEdit } from '../api/qso-update';
import { resolveModeAndSubmode } from '../utils/mode';

/**
 * Convert ADIF canonical date `YYYYMMDD` → `YYYY-MM-DD`. Defensive
 * passthrough for empty / already-dashed values so the function is
 * idempotent against the SPA's mixed-format world.
 */
function adifDateToInput(d: string): string {
    if (d.length === 8 && !d.includes('-')) {
        return `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}`;
    }
    return d;
}

/**
 * Convert ADIF canonical time `HHMM` (or a defensive `HHMMSS`) → `HH:MM`
 * for the TimeInput component. Storage is minute precision (see the
 * format note above), so the daemon sends `HHMM` in practice and there
 * are no seconds to lose; the HHMMSS branch is just tolerance. Edits
 * round-trip at minute precision by design — there is no second-level
 * data to preserve.
 */
function adifTimeToInput(t: string): string {
    if (t.length >= 4 && !t.includes(':')) {
        return `${t.slice(0, 2)}:${t.slice(2, 4)}`;
    }
    return t;
}

class QsoEditState {
    /**
     * UUID of the QSO under edit. Empty when the overlay is closed.
     * Used by `submit` to target the correct row and by SessionPanel
     * to update the matching session-list entry on success.
     */
    uuid: string = $state('');

    /** Whether the modal is rendered. Driven by open() / close(). */
    open: boolean = $state(false);

    /** True between open() and the GET /v1/qso resolving. UI shows a spinner. */
    loading: boolean = $state(false);

    /** True between submit() click and the PATCH resolving. Disables Save. */
    saving: boolean = $state(false);

    // Working copy. Populated by open() from the daemon's GET
    // response; bound to the form inputs.
    callsign: string = $state('');
    name: string = $state('');
    qth: string = $state('');
    country: string = $state('');
    comment: string = $state('');
    mode: string = $state('');
    /** Primary (TX) frequency, MHz string e.g. "14.250". */
    freq: string = $state('');
    /** Secondary (RX) frequency for split QSOs, MHz string. Empty for non-split. */
    freqRx: string = $state('');
    band: string = $state('');
    rstSent: string = $state('');
    rstRcvd: string = $state('');
    qsoDate: string = $state('');
    qsoDateOff: string = $state('');
    timeOn: string = $state('');
    timeOff: string = $state('');

    // Details-panel fields — operator-typed, sparse on most QSOs.
    /** ADIF RIG — contacted station's rig / working conditions. */
    rig: string = $state('');
    /** ADIF RX_PWR — contacted station's TX power in watts (digit string, no unit). */
    rxPwr: string = $state('');
    /** ADIF NOTES — operator's private notes, distinct from COMMENT. */
    notes: string = $state('');
    /** APP_SM_REQUEST_QSL flag — operator's reminder to request a QSL card. */
    requestQsl: boolean = $state(false);

    /**
     * Hydrate the working copy from a daemon GET /v1/qso response and
     * flip open=true. The component renders on the open transition.
     * Called by SessionPanel after fetchQso resolves.
     *
     * Daemon's response is flat (see DaemonQsoForEdit) — every field
     * is a top-level key on the JSON object.
     */
    populate(qso: DaemonQsoForEdit): void {
        this.uuid = qso.uuid ?? '';
        this.callsign = qso.call ?? '';
        this.name = qso.name ?? '';
        this.qth = qso.qth ?? '';
        this.country = qso.country ?? '';

        this.comment = qso.comment ?? '';
        // Operator-facing form: SUBMODE wins, so a stored {MODE:SSB, SUBMODE:USB}
        // shows "USB" in the dropdown (and saving resolves it back to the pair);
        // a submode-less mode (CW, FT8) shows the MODE. Fixes the blank dropdown
        // on SSB QSOs + the lost-SUBMODE round-trip (review 2026-06-04 H3).
        this.mode = qso.submode || qso.mode || '';
        this.freq = qso.freq ?? '';
        this.freqRx = qso.freq_rx ?? '';
        this.band = qso.band ?? '';
        this.rstSent = qso.rst_sent ?? '';
        this.rstRcvd = qso.rst_rcvd ?? '';
        this.qsoDate = adifDateToInput(qso.qso_date ?? '');
        this.qsoDateOff = adifDateToInput(qso.qso_date_off ?? '');
        this.timeOn = adifTimeToInput(qso.time_on ?? '');
        this.timeOff = adifTimeToInput(qso.time_off ?? '');

        this.rig = qso.rig ?? '';
        this.rxPwr = qso.rx_pwr ?? '';
        this.notes = qso.notes ?? '';
        this.requestQsl = qso.app_sm_request_qsl ?? false;

        this.loading = false;
        this.open = true;
    }

    /**
     * Mark the overlay as opening + loading; SessionPanel calls this
     * before firing the GET so the modal renders immediately with a
     * spinner rather than waiting for the round-trip.
     *
     * Sets `uuid` so SessionPanel's open-race guard can compare against
     * it: if the operator closes the overlay (ESC / cancel) or opens a
     * different row before the GET resolves, the stale GET sees either
     * `open === false` or a changed `uuid` and drops its result rather
     * than populating (review 2026-06-04 H2). SessionPanel also aborts
     * the prior GET when a new open starts.
     */
    beginOpen(uuid: string): void {
        this.uuid = uuid;
        this.open = true;
        this.loading = true;
    }

    /**
     * Reset and hide. Called from the cancel button, ESC, click-
     * outside, or after a successful save. Wipes the working copy so
     * a stale value can never leak into the next open() — defence
     * against a future bug where two overlays open in quick
     * succession.
     */
    close(): void {
        this.open = false;
        this.loading = false;
        this.saving = false;
        this.uuid = '';
        this.callsign = '';
        this.name = '';
        this.qth = '';
        this.country = '';
        this.comment = '';
        this.mode = '';
        this.freq = '';
        this.freqRx = '';
        this.band = '';
        this.rstSent = '';
        this.rstRcvd = '';
        this.qsoDate = '';
        this.qsoDateOff = '';
        this.timeOn = '';
        this.timeOff = '';
        this.rig = '';
        this.rxPwr = '';
        this.notes = '';
        this.requestQsl = false;
    }

    /**
     * Build the JSON body for PATCH /v1/qso/{uuid}. Includes only
     * the editable fields so the daemon's "restore immutables" logic
     * doesn't have to re-zero sm_*_upload_status etc. Country is
     * included so the daemon's required-field check passes (the field
     * was set at submit time from enrichment; the operator can edit
     * it but cannot blank it).
     *
     * Wire shape is FLAT (see DaemonQsoForEdit). qsoservice.Update
     * unmarshals the body onto an existing types.Qso whose embedded
     * structs flatten on the wire, so PATCH must send sibling keys.
     */
    toPatchBody(): DaemonQsoForEdit {
        // Resolve the operator-facing mode back to the ADIF MODE/SUBMODE pair
        // and send BOTH. submode '' explicitly clears a stale submode on a mode
        // change (e.g. SSB+USB → CW): the daemon's PATCH preserves absent keys,
        // so omitting submode (the old behaviour) left stale data behind
        // (review 2026-06-04 H3).
        const resolved = resolveModeAndSubmode(this.mode);
        return {
            app_sm_request_qsl: this.requestQsl,
            call: this.callsign,
            name: this.name,
            qth: this.qth,
            country: this.country,
            rst_sent: this.rstSent,
            rst_rcvd: this.rstRcvd,
            mode: resolved.mode,
            submode: resolved.subMode,
            freq: this.freq,
            freq_rx: this.freqRx,
            band: this.band,
            qso_date: this.qsoDate,
            qso_date_off: this.qsoDateOff,
            time_on: this.timeOn,
            time_off: this.timeOff,
            comment: this.comment,
            rig: this.rig,
            rx_pwr: this.rxPwr,
            notes: this.notes,
        };
    }
}

export const qsoEditState = new QsoEditState();
