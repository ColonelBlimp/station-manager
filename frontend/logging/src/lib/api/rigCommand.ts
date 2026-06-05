/*
    Thin daemon-side wrapper for `POST /v1/rig/command` (ADR 0026 inbound
    command path). Sends one semantic op + scalar value and reports the
    outcome as a discriminated union, same conventions as `lib/api/config.ts`.

    The daemon confirms only the *write* (202 Accepted, no body); the resulting
    rig-state change arrives out-of-band over `/v1/rig/events` (confirm-by-
    push), so an 'ok' outcome means "the command reached the rig", not "the rig
    is now at X".

    Errors carry the daemon's `{code, message}` envelope (api.md §4.6) — e.g.
    `rig_unsupported_command` / `rig_invalid_value` (4xx), `rig_not_connected`
    (503), `rig_identity_unverified` (409 — the connected rig hasn't been
    confirmed as the configured driver), or `rig_command_failed` (5xx).
*/

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type RigCommandOutcome =
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

/**
 * POST a single semantic rig op to the daemon. `value` is a JSON scalar — a
 * number for set_freq, a string for set_mode / set_band — or omitted entirely
 * for valueless ops (band_up / band_down / swap_vfo). Passed through verbatim (the daemon
 * renders it to the wire string). Returns 'ok' on the daemon's 202 Accepted;
 * the new rig state arrives over the SSE stream, not in this response.
 */
export async function sendRigCommand(
    op: string,
    value?: string | number,
    signal?: AbortSignal
): Promise<RigCommandOutcome> {
    const payload: { op: string; value?: string | number } =
        value === undefined ? { op } : { op, value };
    const fetched = await safeFetch('/v1/rig/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
        signal,
    });
    if (!fetched.ok) {
        return { kind: fetched.kind, message: fetched.message };
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
