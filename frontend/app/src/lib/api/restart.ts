/*
    POST /v1/restart — request a graceful daemon restart (the "Requires a restart"
    config-apply flows: active rig, connection, mode mappings, serial overrides).

    The daemon replies 202, then runs its normal graceful shutdown and exits with
    the restart code so systemd respawns it (~5s); the app's SSE clients then
    auto-reconnect. Refused 409 `tx_active` while a tune/FT8 carrier is keyed (stop
    transmitting first); 503 when the daemon has no service-manager restart wired
    (split-host / non-systemd run).
*/
import { safeFetch, readJsonBody, isPlainObject } from './_helpers';

export type RestartOutcome =
    | { kind: 'accepted' }
    | { kind: 'tx_active' }
    | { kind: 'unavailable' }
    | { kind: 'error'; message: string };

export async function restartDaemon(signal?: AbortSignal): Promise<RestartOutcome> {
    const fetched = await safeFetch('/v1/restart', { method: 'POST', signal });
    if (!fetched.ok) return { kind: 'error', message: fetched.message };
    const res = fetched.response;
    if (res.status === 202) return { kind: 'accepted' };
    const body = await readJsonBody(res);
    const err = isPlainObject(body) ? (body as { code?: string; message?: string }) : null;
    if (res.status === 409 && err?.code === 'tx_active') return { kind: 'tx_active' };
    if (res.status === 503) return { kind: 'unavailable' };
    return { kind: 'error', message: err?.message ?? `HTTP ${res.status}` };
}
