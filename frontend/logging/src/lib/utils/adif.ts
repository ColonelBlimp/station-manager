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
 *   - `TIME_ON` / `TIME_OFF` is HHMM or HHMMSS (input `HH:MM` or
 *     `HH:MM:SS` minus the colons). The visible form fields stay `HH:MM`;
 *     qsoDraft.submitTimeOn/Off() supply the seconds-bearing `HH:MM:SS` at
 *     submit so the exported record carries the real seconds (the QSL
 *     manager's OQRS matches on the full timestamp).
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
    /** ADIF TIME_ON — required. HH:MM or HH:MM:SS; emitted HHMM/HHMMSS. */
    timeOn: string;
    /** ADIF TIME_OFF — required. HH:MM or HH:MM:SS; emitted HHMM/HHMMSS. */
    timeOff: string;
    /**
     * ADIF QSO_DATE_OFF — the date at TIME_OFF, input YYYY-MM-DD → emitted
     * YYYYMMDD. The live-logging path always supplies it (QSO_DATE for a same-day
     * contact, the next day when the exchange crossed UTC midnight); the builder
     * emits it only when non-empty. A next-day value is what lets the daemon accept
     * a midnight-spanning QSO (TIME_ON > TIME_OFF) instead of rejecting it as
     * invalid_time_range.
     */
    qsoDateOff?: string;
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

    /**
     * ADIF QSO_RANDOM — Boolean (Y/N) indicating whether the QSO was
     * random vs scheduled. Omitted entirely when undefined; the
     * `qsoDefaults.qsoRandom` operator preference of 'off' becomes
     * undefined here, matching the omit-when-empty rule for every
     * other optional ADIF field.
     */
    qsoRandom?: 'Y' | 'N';

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

    /** ADIF ANT_AZ — bearing for the operator's selected path, computed per QSO
     *  from MY_LAT/MY_LON + the contacted grid. */
    antAz?: string;

    /** ADIF ANT_PATH — the operator's antenna-path choice: "S" (short) or "L"
     *  (long). Paired with antAz so the recorded bearing and path agree. */
    antPath?: string;

    // Contacted-station enrichment, captured at submit from the values the
    // Country/Details panels resolved (enrichment already ran on Tab — these
    // add no lookup and never block logging). Omitted when empty.
    /** ADIF COUNTRY — contacted station's DXCC entity name (country-layer enrichment). */
    country?: string;
    /** ADIF CQZ — contacted station's CQ zone (country-layer enrichment). */
    cqZone?: string;
    /** ADIF ITUZ — contacted station's ITU zone (country-layer enrichment). */
    ituZone?: string;
    /** ADIF DXCC — contacted station's numeric DXCC code (QRZ station lookup; empty when the station is unknown). */
    dxcc?: string;
    /** ADIF GRIDSQUARE — contacted station's Maidenhead grid (QRZ station lookup; empty when unknown). */
    gridsquare?: string;

    /**
     * ADIF RX_PWR — contacted station's TX power in watts as estimated
     * by the operator. Digit-only string; emitted as-is. Omitted when
     * empty.
     */
    rxPwr?: string;

    /**
     * ADIF RIG — contacted station's rig / working conditions, free
     * text. Operator-typed; not auto-populated from enrichment. Omitted
     * when empty.
     */
    rig?: string;

    /**
     * ADIF NOTES — operator's personal notes about this contact.
     * Distinct from COMMENT (which is for things to share during the
     * QSO). Omitted when empty.
     */
    notes?: string;

    /**
     * APP_SM_REQUEST_QSL — custom field, station-manager-specific.
     * 'Y' when the operator wants to request a QSL card from this
     * contact (a future "QSOs awaiting QSL request" view consumes
     * this). Omit-when-false rather than emitting 'N' so the blob
     * stays minimal for the common case.
     */
    appSmRequestQsl?: boolean;
}

// Reused across every tag emit; module-scope so we don't allocate one
// per call. TextEncoder is always UTF-8 per spec.
const adifUtf8 = new TextEncoder();

// ADIF length prefixes are byte counts (the daemon parser slices by
// byte at internal/adif/parse.go), not UTF-16 code units. JS
// `string.length` returns code units, which under-counts every
// non-ASCII character (e.g. "José" is length 4 in JS but 5 bytes in
// UTF-8). A wrong prefix causes the daemon to truncate the value or
// break the next tag boundary outright, corrupting QSOs for operators
// whose name / qth / comment / my_country (e.g. "Côte d'Ivoire")
// contains accented characters. Encode + measure.
function adifTag(name: string, value: string): string {
    return `<${name}:${adifUtf8.encode(value).byteLength}>${value}`;
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

    // QSO_DATE_OFF — only when the contact crossed UTC midnight (TIME_OFF is on the
    // following day). The daemon's time-coherence check requires it when
    // TIME_ON > TIME_OFF; omitted for the common same-day QSO.
    if (f.qsoDateOff && f.qsoDateOff.length > 0) {
        lines.push(adifTag('QSO_DATE_OFF', f.qsoDateOff.replace(/-/g, '')));
    }

    // Optional fields, conditional, fixed order.
    if (f.subMode && f.subMode.length > 0) {
        lines.push(adifTag('SUBMODE', f.subMode));
    }
    if (f.rxFreqHz !== undefined && f.rxFreqHz !== f.txFreqHz) {
        lines.push(adifTag('FREQ_RX', freqMhz(f.rxFreqHz)));
    }
    // Omit rather than emit a durable TX_PWR:0 when the value rounds to zero
    // (sub-0.5 W can only be a typo on this station — the FTdx10's floor is
    // 5 W — but a QRP rig's operator would hit it for real).
    if (f.txPower !== undefined && Math.round(f.txPower) > 0) {
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
    if (f.qsoRandom === 'Y' || f.qsoRandom === 'N') {
        lines.push(adifTag('QSO_RANDOM', f.qsoRandom));
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
    if (f.antPath && f.antPath.length > 0) {
        lines.push(adifTag('ANT_PATH', f.antPath));
    }

    // Contacted-station enrichment (Country / Details panels). The daemon
    // maps these ADIF tags onto ContactedStation via its uppercased-json-tag
    // convention (COUNTRY←country, CQZ←cqz, ITUZ←ituz, DXCC←dxcc,
    // GRIDSQUARE←gridsquare). Without these the daemon defaults COUNTRY to
    // "Unknown" and leaves the rest empty.
    if (f.country && f.country.length > 0) {
        lines.push(adifTag('COUNTRY', f.country));
    }
    if (f.cqZone && f.cqZone.length > 0) {
        lines.push(adifTag('CQZ', f.cqZone));
    }
    if (f.ituZone && f.ituZone.length > 0) {
        lines.push(adifTag('ITUZ', f.ituZone));
    }
    if (f.dxcc && f.dxcc.length > 0) {
        lines.push(adifTag('DXCC', f.dxcc));
    }
    if (f.gridsquare && f.gridsquare.length > 0) {
        lines.push(adifTag('GRIDSQUARE', f.gridsquare));
    }

    // Contacted-station / per-QSO operator notes (Details panel).
    // Distinct from COMMENT (above) — NOTES is operator's private
    // record, COMMENT is for things shared during the QSO.
    if (f.rxPwr && f.rxPwr.length > 0) {
        lines.push(adifTag('RX_PWR', f.rxPwr));
    }
    if (f.rig && f.rig.length > 0) {
        lines.push(adifTag('RIG', f.rig));
    }
    if (f.notes && f.notes.length > 0) {
        lines.push(adifTag('NOTES', f.notes));
    }

    // Station-manager-specific reminder flag. Emitted only when true so
    // the absence of the tag means "no reminder set" — saves space in
    // additional_data for the common case.
    if (f.appSmRequestQsl) {
        lines.push(adifTag('APP_SM_REQUEST_QSL', 'Y'));
    }

    lines.push('<EOR>');
    return lines.join('\n');
}
