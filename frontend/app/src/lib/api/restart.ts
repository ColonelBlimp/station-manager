/*
    POST /v1/restart — request a graceful daemon restart (the "Requires a restart"
    config-apply flows: active rig, connection, mode mappings, serial overrides).

    The daemon replies 202, then runs its normal graceful shutdown and exits with
    the restart code so systemd respawns it (~5s); the app's SSE clients then
    auto-reconnect. Refused 409 `tx_active` while a tune/FT8 carrier is keyed (stop
    transmitting first); 503 when the daemon has no service-manager restart wired
    (split-host / non-systemd run).
*/
import { safeFetch, readJsonBody, isPlainObject, WRITE_TIMEOUT_MS } from './_helpers';

export type RestartOutcome =
    | { kind: 'accepted' }
    | { kind: 'tx_active' }
    | { kind: 'unavailable' }
    // `timedOut` marks the AMBIGUOUS restart (F-04b, ADR 0078): the daemon replies
    // 202 then exits, so the response can be lost while it is already respawning.
    // The caller MUST confirm via the new-instance signal (waitForDaemonBack)
    // rather than declaring a definite "Restart failed". A definite (non-timeout)
    // transport failure carries no marker and keeps its existing wording.
    | { kind: 'error'; message: string; timedOut?: boolean };

export async function restartDaemon(signal?: AbortSignal): Promise<RestartOutcome> {
    // A restart is a state-mutating write; give it the write-class window so a
    // slow-to-shut-down daemon isn't abandoned early on the read default.
    const fetched = await safeFetch(
        '/v1/restart',
        { method: 'POST', signal },
        { timeoutMs: WRITE_TIMEOUT_MS }
    );
    if (!fetched.ok) {
        const timedOut = fetched.kind === 'network' ? fetched.timedOut : undefined;
        return { kind: 'error', message: fetched.message, timedOut };
    }
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
 * The daemon's per-process instance id (`/v1/version.instance`), or '' when the
 * daemon isn't reachable or doesn't report one.
 */
export async function fetchDaemonInstance(): Promise<string> {
    const res = await safeFetch('/v1/version', { method: 'GET' });
    if (!res.ok || !res.response.ok) return '';
    const body = await readJsonBody(res.response);
    return isPlainObject(body) && typeof body.instance === 'string' ? body.instance : '';
}

/**
 * Poll GET /v1/version until a DIFFERENT process answers than `prevInstance` (the
 * instance captured before the restart), then resolve true — so we don't mistake
 * the still-shutting-down OLD daemon, answering on a reused keep-alive connection,
 * for the replacement (codex 85997b79 P2). Resolves false past the cap. When
 * `prevInstance` is '' (it couldn't be read), any reachable instance counts as
 * back. The short initial delay just avoids an immediate needless poll.
 */
export async function waitForDaemonBack(
    prevInstance: string,
    timeoutMs = 30_000
): Promise<boolean> {
    const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
    await sleep(2000);
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
        const inst = await fetchDaemonInstance();
        if (inst !== '' && inst !== prevInstance) return true;
        await sleep(1500);
    }
    return false;
}
