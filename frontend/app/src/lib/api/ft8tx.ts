/*
    Thin daemon-side wrapper for the FT8 transmit ARM endpoint (ADR 0029/0030):
      - POST /v1/ft8/tx/arm  {armed}  — arm / disarm the TX path

    Arming is the operator's explicit consent to key the rig; the daemon owns the
    guaranteed stop (hard auto-off + release-on-disconnect), so this reports only
    the *write* (202 Accepted, no body). The resulting TX state (armed /
    transmitting / error) arrives out-of-band as an `ft8-tx` SSE event
    (confirm-by-push, same discipline as the tune carrier). An 'ok' outcome means
    "the daemon accepted the request", not "the rig is now armed".

    The daemon never exposes the raw send endpoint to this SPA — messages are
    driven by the manual sequencer (lib/api/ft8qso.ts), not queued here. Errors
    carry the daemon's {code, message} envelope — e.g. `ft8_tx_unavailable` /
    `rig_not_ready` (503). Same conventions as lib/api/rig-tune.ts.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type Ft8TxOutcome =
    | { kind: 'ok' }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    // `timedOut` marks the AMBIGUOUS write (F-04 confirm-by-push, ADR 0078): no
    // response arrived before the deadline, which proves only that — the request
    // may or may not have reached the daemon, so TX may already be armed (or
    // disarmed), or nothing may have happened. The seam reconciles it against the
    // ft8-tx SSE rather than declaring a failure.
    | { kind: 'network'; message: string; timedOut?: boolean };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

/** Arm (true) or disarm (false) the FT8 transmit path. */
export async function armFt8Tx(armed: boolean, signal?: AbortSignal): Promise<Ft8TxOutcome> {
    const fetched = await safeFetch('/v1/ft8/tx/arm', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ armed }),
        signal,
    });
    if (!fetched.ok) {
        // Carry the fired-timeout marker outward so the seam can reconcile it.
        return fetched.kind === 'network'
            ? { kind: 'network', message: fetched.message, timedOut: fetched.timedOut }
            : { kind: 'aborted', message: fetched.message };
    }

    const { response } = fetched;
    if (response.ok) {
        return { kind: 'ok' };
    }

    const body = await readJsonBody(response);
    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    return response.status >= 500
        ? { kind: 'server', code, message }
        : { kind: 'validation', code, message };
}
