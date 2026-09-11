// POST /v1/ft8/claim — the FT view names the profile it is about to subscribe
// on (ADR 0080, W-0019). Claimed BEFORE the stream opens because a native
// EventSource cannot surface a refusal code: this request can, and its 409
// carries retry_after_ms when the cause is the linger of a session the
// operator just left, so the view re-claims on time instead of polling.

import { isPlainObject, readJsonBody, safeFetch } from './_helpers';

export type FtProfileName = 'ft8' | 'ft4';

export type Ft8ClaimOutcome =
    | { kind: 'ok'; mode: string }
    /** 409: the other profile has something live. `retryAfterMs` is 0 when the daemon gave no hint. */
    | { kind: 'refused'; code: string; message: string; retryAfterMs: number }
    | { kind: 'validation'; code: string; message: string }
    | { kind: 'server'; code: string; message: string }
    | { kind: 'aborted'; message: string }
    | { kind: 'network'; message: string };

interface ClaimBody {
    mode?: unknown;
    code?: unknown;
    message?: unknown;
    retry_after_ms?: unknown;
}

const str = (v: unknown): string => (typeof v === 'string' ? v : '');

export async function claimFt8Profile(
    mode: FtProfileName,
    signal?: AbortSignal
): Promise<Ft8ClaimOutcome> {
    const fetched = await safeFetch('/v1/ft8/claim', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mode }),
        signal,
    });
    if (!fetched.ok) {
        return fetched.kind === 'network'
            ? { kind: 'network', message: fetched.message }
            : { kind: 'aborted', message: fetched.message };
    }
    const { response } = fetched;
    const raw = await readJsonBody(response);
    const body: ClaimBody = isPlainObject(raw) ? raw : {};
    if (response.ok) {
        // A grant must NAME the profile it granted, and it must be the one asked
        // for: a missing or contradictory mode is a malformed reply, never a grant
        // (opening ?mode= on it would be the EventSource error loop the claim
        // exists to prevent).
        const granted = str(body.mode).toUpperCase();
        if (granted !== mode.toUpperCase()) {
            return {
                kind: 'validation',
                code: 'ft8_claim_malformed',
                message:
                    granted === ''
                        ? 'the daemon named no profile'
                        : `the daemon granted ${granted}, not ${mode.toUpperCase()}`,
            };
        }
        return { kind: 'ok', mode: granted };
    }
    const code = str(body.code) || 'unknown_error';
    const message = str(body.message) || `HTTP ${response.status}`;
    if (response.status === 409) {
        const retry =
            typeof body.retry_after_ms === 'number' && body.retry_after_ms > 0
                ? body.retry_after_ms
                : 0;
        return { kind: 'refused', code, message, retryAfterMs: retry };
    }
    return response.status >= 500
        ? { kind: 'server', code, message }
        : { kind: 'validation', code, message };
}
