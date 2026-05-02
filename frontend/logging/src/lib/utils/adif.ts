/**
 * ADIF QSO record formatter.
 *
 * The daemon's submit endpoint accepts raw ADIF as the request body
 * (per `invariants.md` — "ADIF as raw POST body, no JSON wrapping").
 * This module produces a single ADIF QSO record terminated by `<EOR>`.
 * Tag set is the v1 starter — `CALL`, `QSO_DATE`, `TIME_ON`, `TIME_OFF`,
 * `MODE`, `FREQ`, `BAND`, `RST_SENT`, `RST_RCVD` always; optional
 * fields (`SUBMODE`, `FREQ_RX`, `TX_PWR`, `NAME`, `QTH`, `COMMENT`)
 * are emitted only when present / meaningful.
 *
 * Wire-shape decisions (settled with operator before code):
 *
 *   - ADIF tag names + format on the wire (not JSON). Pre-translation
 *     here means the SPA ↔ daemon contract is one shape end-to-end.
 *   - Frequency in MHz with 6 decimal places: `(hz / 1_000_000).toFixed(6)`.
 *   - `QSO_DATE` is YYYYMMDD (input `YYYY-MM-DD` minus the dashes).
 *   - `TIME_ON` / `TIME_OFF` is HHMM (input `HH:MM` minus the colon).
 *     4-digit HHMM is sufficient for personal logging; the 6-digit
 *     HHMMSS variant is not used.
 *   - `TX_PWR` is the EFFECTIVE radiated power (rig power × amp
 *     multiplier, per `displayedState.effectivePower`), rounded to
 *     integer. Omitted when 0 (means "not yet reported / not set").
 *   - `FREQ_RX` only emitted when split (rxFreqHz differs from
 *     txFreqHz). ADIF `FREQ` is the TX frequency.
 *   - Empty / undefined optional fields are omitted entirely (no
 *     `<NAME:0>` tags).
 *
 * Tag emission order is fixed (see formatAdifRecord) so the produced
 * record is byte-identical for the same input — useful for debugging
 * and for the spec test that pins the wire shape.
 */

export interface AdifQsoFields {
    /** ADIF CALL — required. The CONTACT's callsign. */
    callsign: string;
    /** ADIF RST_SENT — required. */
    rstSent: string;
    /** ADIF RST_RCVD — required. */
    rstRcvd: string;
    /** ADIF QSO_DATE — required. Input format YYYY-MM-DD; emitted YYYYMMDD. */
    qsoDate: string;
    /** ADIF TIME_ON — required. Input format HH:MM; emitted HHMM. */
    timeOn: string;
    /** ADIF TIME_OFF — required. Input format HH:MM; emitted HHMM. */
    timeOff: string;
    /** ADIF MODE — required. Pass-through. */
    mode: string;
    /** ADIF BAND — required. */
    band: string;
    /** TX frequency in Hz — required. Emitted as ADIF FREQ in MHz with 6 decimal places. */
    txFreqHz: number;
    /** Contact's name — ADIF NAME. Omitted when empty. */
    name?: string;
    /** Contact's QTH — ADIF QTH. Omitted when empty. */
    qth?: string;
    /** Free-form notes — ADIF COMMENT. Omitted when empty. */
    comment?: string;
    /** ADIF SUBMODE. Omitted when empty. */
    subMode?: string;
    /**
     * RX frequency in Hz, when different from TX (split mode).
     * Emitted as ADIF FREQ_RX. Omitted when undefined or equal to txFreqHz.
     */
    rxFreqHz?: number;
    /**
     * Effective TX power in watts. Emitted as ADIF TX_PWR rounded to
     * integer. Omitted when 0 (treated as "not set").
     */
    txPower?: number;

    // Operator-station fields (MY_* family). Populated from the
    // `operator` Svelte store at QSO submit time. All optional —
    // omitted when empty. Recommended (but not enforced): set at
    // least `stationCallsign` so the QSO record identifies who
    // logged it.

    /** OPERATOR's callsign — ADIF STATION_CALLSIGN. Omitted when empty. */
    stationCallsign?: string;
    /** Operator's Maidenhead grid (e.g. "IO91vl") — ADIF MY_GRIDSQUARE. */
    myGridSquare?: string;
    /** Operator's name — ADIF MY_NAME. */
    myName?: string;
    /** Operator's rig (human-friendly name, e.g. "IC-7300") — ADIF MY_RIG. */
    myRig?: string;
    /** Operator's antenna description — ADIF MY_ANTENNA. */
    myAntenna?: string;
}

function adifTag(name: string, value: string): string {
    return `<${name}:${value.length}>${value}`;
}

function freqMhz(hz: number): string {
    return (hz / 1_000_000).toFixed(6);
}

export function formatAdifRecord(f: AdifQsoFields): string {
    const lines: string[] = [];

    // Required fields, fixed order.
    lines.push(adifTag('CALL', f.callsign));
    lines.push(adifTag('QSO_DATE', f.qsoDate.replace(/-/g, '')));
    lines.push(adifTag('TIME_ON', f.timeOn.replace(/:/g, '')));
    lines.push(adifTag('TIME_OFF', f.timeOff.replace(/:/g, '')));
    lines.push(adifTag('MODE', f.mode));
    lines.push(adifTag('FREQ', freqMhz(f.txFreqHz)));
    lines.push(adifTag('BAND', f.band));
    lines.push(adifTag('RST_SENT', f.rstSent));
    lines.push(adifTag('RST_RCVD', f.rstRcvd));

    // Optional fields, conditional, fixed order.
    if (f.subMode && f.subMode.length > 0) {
        lines.push(adifTag('SUBMODE', f.subMode));
    }
    if (f.rxFreqHz !== undefined && f.rxFreqHz !== f.txFreqHz) {
        lines.push(adifTag('FREQ_RX', freqMhz(f.rxFreqHz)));
    }
    if (f.txPower !== undefined && f.txPower > 0) {
        lines.push(adifTag('TX_PWR', Math.round(f.txPower).toString()));
    }
    if (f.name && f.name.length > 0) {
        lines.push(adifTag('NAME', f.name));
    }
    if (f.qth && f.qth.length > 0) {
        lines.push(adifTag('QTH', f.qth));
    }
    if (f.comment && f.comment.length > 0) {
        lines.push(adifTag('COMMENT', f.comment));
    }

    // Operator-station block (MY_*). Logical grouping puts contact
    // info before operator info — matches typical ADIF reader habits
    // (the most distinctive field for THIS QSO comes first).
    if (f.stationCallsign && f.stationCallsign.length > 0) {
        lines.push(adifTag('STATION_CALLSIGN', f.stationCallsign));
    }
    if (f.myGridSquare && f.myGridSquare.length > 0) {
        lines.push(adifTag('MY_GRIDSQUARE', f.myGridSquare));
    }
    if (f.myName && f.myName.length > 0) {
        lines.push(adifTag('MY_NAME', f.myName));
    }
    if (f.myRig && f.myRig.length > 0) {
        lines.push(adifTag('MY_RIG', f.myRig));
    }
    if (f.myAntenna && f.myAntenna.length > 0) {
        lines.push(adifTag('MY_ANTENNA', f.myAntenna));
    }

    lines.push('<EOR>');
    return lines.join('\n');
}
