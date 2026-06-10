/*
    Thin daemon-side wrapper for the FT8 manual-sequencer endpoints (ADR 0031,
    step e3):
      - POST /v1/ft8/qso/start   {their_call, their_grid, slot_utc, offset_hz}
      - POST /v1/ft8/qso/abandon

    Start begins answering a CQ; the daemon then auto-advances the CQ→73 ladder.
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

async function postFt8Qso(path: string, body: unknown, signal?: AbortSignal): Promise<Ft8QsoOutcome> {
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
 *  operatingFreqMHz is the rig dial frequency (the logged QSO freq = dial + offset). */
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

/** Abandon any active sequenced QSO. */
export function abandonFt8Qso(signal?: AbortSignal): Promise<Ft8QsoOutcome> {
    return postFt8Qso('/v1/ft8/qso/abandon', {}, signal);
}
