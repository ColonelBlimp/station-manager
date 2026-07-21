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
    // Only the handler's own 503 means "no restart wired". A 503 from the load
    // limiter (code "server_busy") is a transient, retryable error — don't mislabel
    // it as unsupported (codex 088bdb84 P3).
    if (res.status === 503 && err?.code === 'restart_unavailable') return { kind: 'unavailable' };
    return { kind: 'error', message: err?.message ?? `HTTP ${res.status}` };
}

/**
 * Poll GET /v1/version until the daemon answers again after a restart, so the UI
 * can re-enable its control instead of staying stuck disabled (codex 088bdb84 P2).
 * Resolves true once it's back, false if it doesn't return within the cap. The
 * short initial delay lets the old process release the listener before the first
 * poll (so we don't see the still-up old daemon and declare it "back" early).
 */
export async function waitForDaemonBack(timeoutMs = 30_000): Promise<boolean> {
    const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
    await sleep(2000);
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        const res = await safeFetch('/v1/version', { method: 'GET' });
        if (res.ok && res.response.ok) return true;
        await sleep(1500);
    }
    return false;
}
