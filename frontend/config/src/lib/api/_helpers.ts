/*
    Shared internals for the config SPA's API helpers — the same fetch /
    JSON / shape-guard primitives the logging SPA uses (frontend/logging/
    src/lib/api/_helpers.ts), kept in sync deliberately. The boundary: the
    API layer is the last hop before SPA code sees a typed result, so any
    runtime sloppiness about response shape stops here, not in a panel
    mid-render.
*/

/**
 * Discriminated outcome of `safeFetch`. The `ok: true` arm hands the
 * `Response` back so the caller can inspect status / headers; the
 * failure arms collapse the two transport-level error modes (operator
 * cancellation vs network unreachability).
 */
export type FetchOutcome =
    | { ok: true; response: Response }
    | { ok: false; kind: 'aborted'; message: string }
    | { ok: false; kind: 'network'; message: string };

/**
 * Centralised `fetch` wrapper: catches the transport-failure exception
 * and distinguishes an abort/timeout from a generic network error so
 * callers can surface the difference. Callers that pass no signal never
 * see the `'aborted'` arm.
 */
export async function safeFetch(input: RequestInfo, init?: RequestInit): Promise<FetchOutcome> {
    try {
        const response = await fetch(input, init);
        return { ok: true, response };
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        if (err instanceof Error && (err.name === 'AbortError' || err.name === 'TimeoutError')) {
            return { ok: false, kind: 'aborted', message };
        }
        if (init?.signal?.aborted) {
            return { ok: false, kind: 'aborted', message };
        }
        return { ok: false, kind: 'network', message };
    }
}

/**
 * Parse a response body as JSON, returning `null` on parse failure
 * rather than throwing — every caller downgrades an unparseable body to
 * a synthesised error envelope, not a propagated `SyntaxError`.
 */
export async function readJsonBody(response: Response): Promise<unknown> {
    try {
        return await response.json();
    } catch {
        return null;
    }
}

/**
 * True when `value` is a plain JSON-style object (non-null, non-array).
 * Narrows `unknown` to `Record<string, unknown>` so subsequent
 * `'field' in value` checks are type-safe. Arrays are excluded: every
 * daemon response envelope here is `{...}`.
 */
export function isPlainObject(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/**
 * Runtime guard for a typed response body: a plain object containing
 * every key in `requiredKeys`. Presence-only — last-mile semantic
 * checks belong at the call site.
 */
export function isShape<T>(
    value: unknown,
    requiredKeys: readonly (keyof T & string)[]
): value is T {
    if (!isPlainObject(value)) return false;
    for (const key of requiredKeys) {
        if (!(key in value)) return false;
    }
    return true;
}
