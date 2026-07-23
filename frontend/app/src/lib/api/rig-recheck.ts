/*
    Thin daemon-side wrapper for `POST /v1/rig/tx/recheck`.

    Re-asks the rig whether it is transmitting, so a standing stuck-TX alarm can
    be resolved on evidence. It exists because the alarm latches itself out of
    every clear path in the daemon: the only thing that retires it is an observed
    TX-status answer, and every path that would normally ask for one is gated by
    the same uncertainty flag the alarm holds (2026-07-21 incident — the operator
    sat in front of an undismissable banner for thirteen minutes).

    An 'ok' outcome means "the daemon put the question on the wire", NOT "the rig
    is safe". The answer comes back asynchronously on the rig's read loop and
    surfaces as a `tx-alarm` SSE event with `active: false` — that event is the
    authoritative all-clear, and nothing in this client may pre-empt it. There is
    deliberately no endpoint to clear the alarm outright: doing so would either
    re-enable keying over a possibly-live PTT or hide the only standing warning
    from every other tab. Hiding the banner locally is what the Dismiss action is
    for, and it does not claim safety.
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
 * Ask the daemon to re-query the rig's transmit state. `alarmActive` in the
 * 'ok' outcome is a status snapshot taken the moment the query went out — it is
 * usually still true, because the rig has not answered yet. Treat it as "was
 * still alarmed when asked", never as a safety verdict.
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
    // 501: this rig definition has no TX-status query, so re-checking can never
    // work — the button should say so rather than invite a retry.
    if (response.status === 501) {
        return { kind: 'unsupported', message };
    }
    return response.status >= 500
        ? { kind: 'server', code, message }
        : { kind: 'validation', code, message };
}
