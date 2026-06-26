/*
    Thin daemon-side wrapper for the FT8 manual-sequencer endpoints (ADR 0031,
    step e3; ADR 0033 caller side):
      - POST /v1/ft8/qso/start    {their_call, their_grid, slot_utc, offset_hz}
      - POST /v1/ft8/qso/work     {their_call, their_grid, their_snr, slot_utc, offset_hz}
      - POST /v1/ft8/cq/start     {offset_hz, operating_freq_mhz}
      - POST /v1/ft8/qso/abandon  (drops any session)

    qso/start begins answering a CQ; qso/work begins working a station calling us
    (picked from the pile-up); cq/start begins calling CQ and working the stations
    that answer. Either way the daemon auto-advances the CQ→73 ladder.
    Both return 202 with no body on success; the contact's progress arrives
    out-of-band as `ft8-qso` SSE events. Our own callsign/grid are resolved
    server-side from the station config, not sent here. Errors carry the daemon's
    {code, message} envelope — e.g. `ft8_tx_not_armed` / `ft8_qso_in_progress`
    (409), `ft8_no_offset` / `no_station_callsign` (400). Mirrors lib/api/ft8tx.ts.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type Ft8QsoOutcome =
    | { kind: 'ok' }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

async function postFt8Qso(
    path: string,
    body: unknown,
    signal?: AbortSignal
): Promise<Ft8QsoOutcome> {
    const fetched = await safeFetch(path, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }
    const { response } = fetched;
    if (response.ok) {
        return { kind: 'ok' };
    }
    const errBody = await readJsonBody(response);
    const err = isPlainObject(errBody) ? (errBody as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    return response.status >= 500
        ? { kind: 'server', code, message }
        : { kind: 'validation', code, message };
}

/** Start answering a CQ. slotUtc is the RFC3339 start of the slot the CQ was
 *  heard in (it fixes the worked station's parity); offsetHz is the picked offset;
 *  operatingFreqMHz is the rig dial frequency (the logged QSO freq IS the dial;
 *  offsetHz is TX audio placement only, never folded into FREQ). */
export function startFt8Qso(
    theirCall: string,
    theirGrid: string,
    slotUtc: string,
    offsetHz: number,
    operatingFreqMHz: number,
    signal?: AbortSignal
): Promise<Ft8QsoOutcome> {
    return postFt8Qso(
        '/v1/ft8/qso/start',
        {
            their_call: theirCall,
            their_grid: theirGrid,
            slot_utc: slotUtc,
            offset_hz: offsetHz,
            operating_freq_mhz: operatingFreqMHz,
        },
        signal
    );
}

/** Start calling CQ (ADR 0033): the daemon calls CQ on offsetHz and works the
 *  stations that answer, one at a time, until abandoned (auto_first / operator_pick
 *  per the daemon's ft8.tx.caller_answer_mode). operatingFreqMHz is the rig dial
 *  frequency (logged QSO freq IS the dial; offset is TX placement only). Our callsign/grid are resolved
 *  server-side from the station config. */
export function startFt8Cq(
    offsetHz: number,
    operatingFreqMHz: number,
    txParity: 'next' | 'even' | 'odd' = 'next',
    signal?: AbortSignal
): Promise<Ft8QsoOutcome> {
    return postFt8Qso(
        '/v1/ft8/cq/start',
        {
            offset_hz: offsetHz,
            operating_freq_mhz: operatingFreqMHz,
            // 'next' = fire on the next slot regardless of parity (the daemon
            // default); only send a parity when the operator chose even/odd.
            ...(txParity === 'next' ? {} : { tx_parity: txParity }),
        },
        signal
    );
}

/** Start working a station that is calling us (ADR 0033 "work a caller"): the
 *  operator picked it from the pile-up. theirCall/theirGrid come from the picked
 *  decode (`<myCall> <theirCall> <grid>`); theirSnr is our SNR of that decode — the
 *  report we send back (RST_SENT). slotUtc is the RFC3339 start of the slot it was
 *  heard in (fixes the caller's parity); offsetHz is the picked TX offset;
 *  operatingFreqMHz is the rig dial frequency. Our callsign/grid resolve server-side. */
export function startFt8WorkCaller(
    theirCall: string,
    theirGrid: string,
    theirSnr: number,
    slotUtc: string,
    offsetHz: number,
    operatingFreqMHz: number,
    signal?: AbortSignal
): Promise<Ft8QsoOutcome> {
    return postFt8Qso(
        '/v1/ft8/qso/work',
        {
            their_call: theirCall,
            their_grid: theirGrid,
            their_snr: theirSnr,
            slot_utc: slotUtc,
            offset_hz: offsetHz,
            operating_freq_mhz: operatingFreqMHz,
        },
        signal
    );
}

/** Set the operator's antenna-path choice for the active exchange — 'short' or
 *  'long'. Logging-only: it annotates the logged QSO (ADIF ANT_PATH + the matching
 *  bearing/distance) and never affects the on-air signal. Mirrors the Phone/CW
 *  short/long radio, but FT8 QSOs are built daemon-side, so the choice is POSTed
 *  here rather than carried on a SPA submit. The daemon resets to short per exchange. */
export function setFt8QsoPath(
    path: 'short' | 'long',
    signal?: AbortSignal
): Promise<Ft8QsoOutcome> {
    return postFt8Qso('/v1/ft8/qso/path', { path: path === 'long' ? 'L' : 'S' }, signal);
}

/** Abandon any active sequenced session — answer-a-CQ or Call-CQ. */
export function abandonFt8Qso(signal?: AbortSignal): Promise<Ft8QsoOutcome> {
    return postFt8Qso('/v1/ft8/qso/abandon', {}, signal);
}
