/*
    First-run setup writer — the one config write this SPA performs today.

    PUT /v1/config with just the station callsign: when setup is incomplete
    the daemon seeds the default logbook row under that callsign and flips
    setup_complete (internal/api handler_config.go seedDefaultLogbook), which
    is what unblocks every logbook-backed surface (map, logbook page, header
    count). The full config surface stays with the Settings arc — this client
    deliberately knows about nothing but the callsign.
*/

import { isPlainObject, readJsonBody, safeFetch, WRITE_TIMEOUT_MS } from './_helpers';
import { isDurabilityUnconfirmed, noteConfigDurability } from '../config/durability';

export type SetupOutcome = { kind: 'ok' } | { kind: 'error'; message: string };

/** Complete first-run setup with the default logbook's callsign. The caller
 *  normalises (trim + uppercase) so what the operator sees matches the wire. */
export async function completeSetup(callsign: string): Promise<SetupOutcome> {
    const fetched = await safeFetch(
        '/v1/config',
        {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ logging_station: { station_callsign: callsign } }),
        },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        return {
            kind: 'error',
            message: fetched.kind === 'network' ? 'Cannot reach the daemon.' : 'Request failed.',
        };
    }
    if (!fetched.response.ok) {
        const body = await readJsonBody(fetched.response);
        // Error envelope is {code, message}; fall back to a generic line.
        const message =
            isPlainObject(body) && typeof body.message === 'string'
                ? body.message
                : `Daemon error (${fetched.response.status}).`;
        return { kind: 'error', message };
    }
    // Setup is APPLIED, not failed, even when the daemon couldn't confirm the write
    // survives a crash (PT-6) — and setup_complete especially matters across a reboot.
    // Surface the same caveat (there is no success toast here to suppress) and let the
    // caller continue the completion flow.
    noteConfigDurability(isDurabilityUnconfirmed(await readJsonBody(fetched.response)));
    return { kind: 'ok' };
}
