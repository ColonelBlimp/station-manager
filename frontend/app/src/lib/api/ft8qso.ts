/*
    Thin daemon-side wrapper for the FT8 manual-sequencer endpoints (ADR 0031
    step e3; ADR 0033 caller side):
      - POST /v1/ft8/qso/start    {their_call, their_grid, slot_utc, offset_hz, operating_freq_mhz}
      - POST /v1/ft8/qso/work     {their_call, their_grid, their_snr, slot_utc, offset_hz, operating_freq_mhz}
      - POST /v1/ft8/cq/start     {offset_hz, operating_freq_mhz}
      - POST /v1/ft8/qso/abandon  (drops any session)

    qso/start begins answering a CQ; qso/work begins working a station calling us;
    cq/start begins calling CQ and working the stations that answer. Either way the
    daemon auto-advances the CQ→73 ladder and confirms progress out-of-band via
    `ft8-qso` SSE events. Our own callsign/grid are resolved server-side from the
    station config, not sent here. All return 202 with no body on success. Errors
    carry the daemon's {code, message} envelope — e.g. `ft8_tx_not_armed` /
    `ft8_qso_in_progress` (409), `ft8_no_offset` / `no_station_callsign` (400).
    Mirrors lib/api/ft8tx.ts.

    The operating-freq convention matters: the logged QSO frequency IS the rig dial
    frequency (FT8 places the signal at dial + audio-offset, but logs the dial), so
    the SPA passes operating_freq_mhz; offset_hz is audio placement only, never
    folded into the logged FREQ.
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

/** Start answering a CQ. slotUtc is the RFC3339 start of the slot the CQ was heard
 *  in (it fixes the worked station's parity); offsetHz is the picked TX audio offset;
 *  operatingFreqMHz is the rig dial frequency. A 'fd' mode answers a CQ FD with the
 *  operator's Field Day class+section (daemon config) — theirSnr is our SNR of the CQ,
 *  logged as RST_SENT since FD exchanges no report. Omit both for a standard answer. */
export function startFt8Qso(
    theirCall: string,
    theirGrid: string,
    slotUtc: string,
    offsetHz: number,
    operatingFreqMHz: number,
    mode: 'standard' | 'fd' = 'standard',
    theirSnr?: number,
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
            ...(mode === 'fd' ? { mode: 'fd', their_snr: theirSnr ?? 0 } : {}),
        },
        signal
    );
}

/** Start working a station that is calling us (ADR 0033 "work a caller"): a
 *  directed-at-me decode the operator clicked. theirCall/theirGrid come from that
 *  decode (`<myCall> <theirCall> <grid>`); theirSnr is our SNR of it — the report we
 *  send back (RST_SENT). slotUtc fixes the caller's parity; offsetHz is the TX offset;
 *  operatingFreqMHz is the rig dial frequency. An fd exchange routes to the FD work
 *  path with the caller's class+section (ours from daemon config). */
export function startFt8WorkCaller(
    theirCall: string,
    theirGrid: string,
    theirSnr: number,
    slotUtc: string,
    offsetHz: number,
    operatingFreqMHz: number,
    fd?: { class: string; section: string },
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
            ...(fd ? { mode: 'fd', their_class: fd.class, their_section: fd.section } : {}),
        },
        signal
    );
}

/** Start calling CQ (ADR 0033): the daemon calls CQ on offsetHz and works the
 *  stations that answer, one at a time, until abandoned. txParity picks the CQ slot
 *  parity (WSJT-X "Tx even/1st"); 'next' fires on the next slot (the daemon default).
 *  Our callsign/grid resolve server-side. operatingFreqMHz is the rig dial frequency. */
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
            ...(txParity === 'next' ? {} : { tx_parity: txParity }),
        },
        signal
    );
}

/** Abandon any active sequenced session — answer-a-CQ, work-a-caller, or Call-CQ. */
export function abandonFt8Qso(signal?: AbortSignal): Promise<Ft8QsoOutcome> {
    return postFt8Qso('/v1/ft8/qso/abandon', {}, signal);
}

/** Arm/disarm skip-if-silent on the active session (the deferred Next,
 *  daemon-side): armed, a silent cycle ends the session INSTEAD of keying the
 *  repeat — no RF at a station the operator decided to drop. The armed state
 *  comes back via the ft8-qso SSE (skip_armed). */
export function skipFt8Qso(armed: boolean, signal?: AbortSignal): Promise<Ft8QsoOutcome> {
    return postFt8Qso('/v1/ft8/qso/skip', { armed }, signal);
}
