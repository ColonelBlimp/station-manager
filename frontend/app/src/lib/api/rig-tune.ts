/*
    Thin daemon-side wrapper for `POST /v1/rig/tune` (ADR 0027 tune-carrier
    control). Toggles the daemon-owned tune carrier on/off. The daemon owns the
    guaranteed stop (hard auto-off + release-on-disconnect), so this reports
    only the *write* (202 Accepted, no body); the resulting carrier state
    arrives out-of-band as a `tune-state` SSE event (confirm-by-push). An 'ok'
    outcome means "the daemon accepted the request", not "the carrier is now
    on/off".

    Errors carry the daemon's `{code, message}` envelope — e.g.
    `rig_not_connected` / `rig_state_unknown` (503), `rig_identity_unverified`
    (409 — the connected rig hasn't been confirmed as the configured driver),
    or `rig_tune_failed` (5xx). This is the app's first rig-control write seam;
    the discriminated-union outcome shape carries forward to the command client
    (VFO / band / freq / mode) in the later rig-control slices.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type RigTuneOutcome =
    | { kind: 'ok' }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    // `timedOut` marks the AMBIGUOUS write (F-04 confirm-by-push, ADR 0078): no
    // response arrived before the deadline, which proves only that — the request
    // may or may not have reached the daemon, so the carrier may already have
    // keyed/dropped, or nothing may have happened. The seam MUST reconcile
    // against the tune-state SSE rather than declaring a failure. A non-timeout
    // network error carries no marker (not proven to have committed OR failed).
    | { kind: 'network'; message: string; timedOut?: boolean };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

/**
 * POST the tune-carrier toggle to the daemon. active=true keys the carrier;
 * active=false drops it and restores the pre-tune mode + power. Returns 'ok'
 * on the daemon's 202 Accepted; the carrier state itself arrives over the SSE
 * `tune-state` event, not in this response.
 */
export async function sendRigTune(active: boolean, signal?: AbortSignal): Promise<RigTuneOutcome> {
    const fetched = await safeFetch('/v1/rig/tune', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ active }),
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
