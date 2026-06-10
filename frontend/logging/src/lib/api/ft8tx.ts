/*
    Thin daemon-side wrapper for the FT8 transmit endpoints (ADR 0030 step e1):
      - POST /v1/ft8/tx/arm   {armed}              — arm / disarm the TX path
      - POST /v1/ft8/tx/send  {message, offset_hz}  — queue one message on the next slot

    Both return 202 Accepted with no body on success; the resulting TX state
    (armed / transmitting / error) arrives out-of-band as an `ft8-tx` SSE event
    (confirm-by-push, same shape as the tune carrier). An 'ok' outcome means "the
    daemon accepted the request", not "the rig is now keyed".

    Errors carry the daemon's {code, message} envelope — e.g. `ft8_tx_unavailable`
    / `rig_not_ready` (503), `ft8_tx_not_armed` / `ft8_tx_in_flight` (409),
    `ft8_tx_bad_message` (400). Same conventions as lib/api/rigTune.ts.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type Ft8TxOutcome =
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

async function postFt8Tx(path: string, body: unknown, signal?: AbortSignal): Promise<Ft8TxOutcome> {
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

/** Arm (true) or disarm (false) the FT8 transmit path. */
export function armFt8Tx(armed: boolean, signal?: AbortSignal): Promise<Ft8TxOutcome> {
    return postFt8Tx('/v1/ft8/tx/arm', { armed }, signal);
}

/** Queue one standard FT8 message to transmit on the next slot at the given offset. */
export function sendFt8Tx(
    message: string,
    offsetHz: number,
    signal?: AbortSignal
): Promise<Ft8TxOutcome> {
    return postFt8Tx('/v1/ft8/tx/send', { message, offset_hz: offsetHz }, signal);
}
