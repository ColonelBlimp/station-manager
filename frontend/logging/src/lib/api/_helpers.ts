/*
    Shared internals for the six API helpers in this directory. Three
    pieces of envelope-handling boilerplate were repeated across every
    wrapper — fetch-with-network/abort-classification, JSON body
    parsing, and runtime object-shape guards — so this module owns the
    primitives and each helper composes them.

    The boundary kept across these primitives is: the API layer is the
    last hop before SPA code sees a typed result, so any runtime
    sloppiness about response shape stops here, not in a panel mid-render.

    Helpers:
      - `safeFetch(input, init)` — wraps `fetch`. Distinguishes operator
        cancellation (`signal.abort()` or `AbortSignal.timeout(...)`)
        from a genuine network failure, returning a discriminated
        `FetchOutcome` rather than throwing. Callers that don't pass a
        signal never see the `'aborted'` arm.
      - `readJsonBody(response)` — wraps `response.json()`, returning
        `unknown | null`. Never throws; a malformed payload becomes
        `null` and the caller decides whether that's fatal.
      - `isPlainObject(value)` — primitive runtime guard. Narrows
        `unknown` to `Record<string, unknown>` for any non-null
        non-array object. Arrays are excluded by design: every daemon
        response in this codebase is a top-level object envelope.
      - `isShape<T>(value, requiredKeys)` — composite guard. Confirms
        the body is an object AND every key in `requiredKeys` is
        present. The key check is presence-only; type validation of
        individual fields is left to the cast/use site, which is what
        TypeScript's structural typing already does at the call seam.
*/

/**
 * Discriminated outcome of `safeFetch`. The `ok: true` arm hands the
 * `Response` back so the caller can inspect status / headers; the
 * failure arms collapse the two transport-level error modes (operator
 * cancellation vs network unreachability) the way every API wrapper
 * in this directory surfaces them.
 */
export type FetchOutcome =
    | { ok: true; response: Response }
    | { ok: false; kind: 'aborted'; message: string }
    | { ok: false; kind: 'network'; message: string };

/**
 * Centralised `fetch` wrapper. Two responsibilities:
 *
 *   1. Catch the exception fetch throws on transport failure so each
 *      API helper doesn't repeat the try/catch boilerplate.
 *   2. Distinguish `AbortError` (operator-cancelled or timeout-elapsed)
 *      from a generic network error so callers can surface the
 *      difference via the `'aborted'` outcome arm.
 *
 * Detection is belt-and-braces: error name (`AbortError` from manual
 * `AbortController.abort()`, `TimeoutError` from `AbortSignal.timeout()`)
 * plus the `signal.aborted` flag, because the platform's not entirely
 * consistent about which exception class shows up first.
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
 * Parse a response body as JSON. Returns `null` on parse failure
 * rather than throwing — every caller in this directory wants to
 * downgrade an unparseable body to a synthesised error envelope, not
 * propagate a `SyntaxError` up the stack.
 */
export async function readJsonBody(response: Response): Promise<unknown> {
    try {
        return await response.json();
    } catch {
        return null;
    }
}

/**
 * True when `value` is a plain JSON-style object (non-null,
 * non-array). Narrows `unknown` to `Record<string, unknown>` so
 * subsequent `'fieldName' in value` checks and bracket reads are
 * type-safe without further casts.
 *
 * Arrays are deliberately excluded: every daemon response envelope
 * in this codebase is `{...}`, never `[...]`. If a future endpoint
 * returns a bare array at the top level, give it its own guard.
 */
export function isPlainObject(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
}

/**
 * Runtime guard for a typed response body. Confirms `value` is a
 * plain object AND contains every key listed in `requiredKeys`. On
 * success, narrows to `T` so the caller can read fields without an
 * `as T` cast.
 *
 * Required-key check is presence-only (`key in value`), not type
 * validation. The point is to fail loudly when the daemon's payload
 * is structurally wrong (proxy interference, regression, dev/prod
 * schema drift) — not to enforce that, say, `uuid` is a non-empty
 * string. That last-mile semantic check belongs at the call site
 * (e.g. `qso.ts` checks `typeof ok.uuid === 'string' && ok.uuid !== ''`)
 * because the right downgrade differs per endpoint.
 */
export function isShape<T>(value: unknown, requiredKeys: readonly (keyof T & string)[]): value is T {
    if (!isPlainObject(value)) return false;
    for (const key of requiredKeys) {
        if (!(key in value)) return false;
    }
    return true;
}
