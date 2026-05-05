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

    // Operator-station fields (MY_* family). Sourced from
    // configState.loggingStation at QSO submit time. All optional —
    // omitted when empty. Recommended: set at least `stationCallsign`
    // so the QSO record identifies who logged it.

    /** ADIF STATION_CALLSIGN — the logging station's callsign. */
    stationCallsign?: string;
    /** ADIF OPERATOR — the operating callsign (may differ from station). */
    operator?: string;
    /** ADIF OWNER_CALLSIGN — licensee/club owner callsign. */
    ownerCallsign?: string;

    /** ADIF MY_GRIDSQUARE — operator's Maidenhead grid (e.g. "IO91vl"). */
    myGridSquare?: string;
    /** ADIF MY_LAT — daemon-derived latitude in "XDDD MM.MMM" form. */
    myLat?: string;
    /** ADIF MY_LON — daemon-derived longitude in "XDDD MM.MMM" form. */
    myLon?: string;
    /** ADIF MY_STREET. */
    myStreet?: string;
    /** ADIF MY_CITY. */
    myCity?: string;
    /** ADIF MY_POSTAL_CODE. */
    myPostalCode?: string;
    /** ADIF MY_COUNTRY. */
    myCountry?: string;
    /** ADIF MY_ALTITUDE — metres above sea level. */
    myAltitude?: string;
    /** ADIF MY_CQ_ZONE — operator-typed string. */
    myCqZone?: string;
    /** ADIF MY_ITU_ZONE — operator-typed string. */
    myItuZone?: string;
    /** ADIF MY_DXCC — operator-typed string. */
    myDxcc?: string;

    /** ADIF MY_NAME — operator's name. */
    myName?: string;
    /** ADIF MY_RIG — rig name (e.g. "IC-7300"). */
    myRig?: string;
    /** ADIF MY_ANTENNA — antenna description. */
    myAntenna?: string;

    /** ADIF MY_MORSE_KEY_TYPE — e.g. "PADDLE", "BUG", "SK". */
    myMorseKeyType?: string;
    /** ADIF MY_MORSE_KEY_INFO — free-form. */
    myMorseKeyInfo?: string;

    /** ADIF ANT_AZ — short-path bearing, computed per QSO from MY_LAT/MY_LON. */
    antAz?: string;
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

    // Operator-station block (MY_*). Order is stable — identity →
    // location → personal → equipment → CW → per-QSO bearing — so
    // the byte-identical-output spec tests pin the wire shape.
    if (f.stationCallsign && f.stationCallsign.length > 0) {
        lines.push(adifTag('STATION_CALLSIGN', f.stationCallsign));
    }
    if (f.operator && f.operator.length > 0) {
        lines.push(adifTag('OPERATOR', f.operator));
    }
    if (f.ownerCallsign && f.ownerCallsign.length > 0) {
        lines.push(adifTag('OWNER_CALLSIGN', f.ownerCallsign));
    }
    if (f.myGridSquare && f.myGridSquare.length > 0) {
        lines.push(adifTag('MY_GRIDSQUARE', f.myGridSquare));
    }
    if (f.myLat && f.myLat.length > 0) {
        lines.push(adifTag('MY_LAT', f.myLat));
    }
    if (f.myLon && f.myLon.length > 0) {
        lines.push(adifTag('MY_LON', f.myLon));
    }
    if (f.myStreet && f.myStreet.length > 0) {
        lines.push(adifTag('MY_STREET', f.myStreet));
    }
    if (f.myCity && f.myCity.length > 0) {
        lines.push(adifTag('MY_CITY', f.myCity));
    }
    if (f.myPostalCode && f.myPostalCode.length > 0) {
        lines.push(adifTag('MY_POSTAL_CODE', f.myPostalCode));
    }
    if (f.myCountry && f.myCountry.length > 0) {
        lines.push(adifTag('MY_COUNTRY', f.myCountry));
    }
    if (f.myAltitude && f.myAltitude.length > 0) {
        lines.push(adifTag('MY_ALTITUDE', f.myAltitude));
    }
    if (f.myCqZone && f.myCqZone.length > 0) {
        lines.push(adifTag('MY_CQ_ZONE', f.myCqZone));
    }
    if (f.myItuZone && f.myItuZone.length > 0) {
        lines.push(adifTag('MY_ITU_ZONE', f.myItuZone));
    }
    if (f.myDxcc && f.myDxcc.length > 0) {
        lines.push(adifTag('MY_DXCC', f.myDxcc));
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
    if (f.myMorseKeyType && f.myMorseKeyType.length > 0) {
        lines.push(adifTag('MY_MORSE_KEY_TYPE', f.myMorseKeyType));
    }
    if (f.myMorseKeyInfo && f.myMorseKeyInfo.length > 0) {
        lines.push(adifTag('MY_MORSE_KEY_INFO', f.myMorseKeyInfo));
    }
    if (f.antAz && f.antAz.length > 0) {
        lines.push(adifTag('ANT_AZ', f.antAz));
    }

    lines.push('<EOR>');
    return lines.join('\n');
}
