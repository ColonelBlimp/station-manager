/*
    Thin daemon-side wrapper for `POST /v1/rig/tx/recheck`.

    Obtains fresh rig evidence so a standing stuck-TX alarm can be resolved.
    Rigs with a TX-status query are asked whether they are receiving; CI-V rigs
    safely re-assert tx_off and require the addressed radio's ACK.

    An 'ok' outcome means the evidence operation succeeded, but the authoritative
    all-clear is still the `tx-alarm` SSE event with `active: false`; nothing in
    this client may pre-empt it. There is deliberately no operator-asserted clear.
    Hiding the banner locally is what Dismiss is for, and it does not claim safety.
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type RigRecheckOutcome =
    | { kind: 'ok'; alarmActive: boolean }
    | { kind: 'unsupported'; message: string }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface DaemonError {
    code: string;
    message: string;
    op?: string;
}

/**
 * Ask the daemon to obtain fresh transmit-state evidence. `alarmActive` in the
 * 'ok' outcome is only a snapshot; the tx-alarm SSE stream is authoritative.
 */
export async function recheckRigTx(signal?: AbortSignal): Promise<RigRecheckOutcome> {
    const fetched = await safeFetch('/v1/rig/tx/recheck', { method: 'POST', signal });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
    }

    const { response } = fetched;
    if (response.ok) {
        const body = await readJsonBody(response);
        const alarmActive = isPlainObject(body) && body.alarm_active === true;
        return { kind: 'ok', alarmActive };
    }

    const body = await readJsonBody(response);
    const err = isPlainObject(body) ? (body as unknown as DaemonError) : null;
    const code = err?.code ?? 'unknown_error';
    const message = err?.message ?? `HTTP ${response.status}`;
    // 501: this rig has neither a TX-status query nor an ACK-confirmed safe
    // unkey recovery operation, so the button should not invite another retry.
    if (response.status === 501) {
        return { kind: 'unsupported', message };
    }
    return response.status >= 500
        ? { kind: 'server', code, message }
        : { kind: 'validation', code, message };
}
