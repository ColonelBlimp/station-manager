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
      - `safeFetch(input, init, opts)` — wraps `fetch`. Applies a default
        request timeout (bounds a wedged daemon) and distinguishes operator
        cancellation (`signal.abort()`) — surfaced as `'aborted'`, dropped
        silently by callers — from a timeout or genuine network failure —
        surfaced as `'network'`, retriable — returning a discriminated
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

import { noteDaemonDate } from './daemonClock.svelte';

/**
 * Discriminated outcome of `safeFetch`. The `ok: true` arm hands the
 * `Response` back so the caller can inspect status / headers; the
 * failure arms collapse the transport-level error modes the way every
 * API wrapper in this directory surfaces them — `'aborted'` for operator
 * cancellation (dropped silently), `'network'` for a timeout or genuine
 * network failure (surfaced / retriable).
 */
export type FetchOutcome =
    | { ok: true; response: Response }
    | { ok: false; kind: 'aborted'; message: string }
    /** `timedOut` distinguishes the AMBIGUOUS network failure: the request
     *  reached (or may have reached) the daemon and the RESPONSE never came,
     *  so a write may already have committed. A plain connection failure
     *  (refused/unreachable) definitely did not commit.
     *
     *  A write caller SHOULD use this to say "outcome unknown" instead of
     *  "failed", and reconcile with a fresh GET. As of 2026-08-05 exactly one
     *  does — `api/ft8-config.ts` + `config/ft8.svelte.ts` (clean-room review
     *  569b2236 P2); every other write path still flattens it into a generic
     *  failure. This note says "one" rather than "callers" on purpose: it read
     *  as an established contract while nothing honoured it, which is how the
     *  gap survived. Widening it is an open piece of work, and each write needs
     *  its own answer — a config PUT can be reconciled by re-reading, a QSO
     *  POST cannot be retried blindly. */
    | { ok: false; kind: 'network'; message: string; timedOut?: boolean };

/**
 * Default request timeout (ms) applied to every `safeFetch` unless the
 * caller overrides it. The SPA only ever talks to the LOCAL daemon, so a
 * healthy request resolves in well under a second; this ceiling exists to
 * bound a wedged / half-open daemon (accepts the connection, never
 * responds) that would otherwise hang a fetch forever — the failure mode
 * that left boot on a blank page and the submit latch stuck for minutes.
 * Generous enough never to trip a legitimately-slow local query.
 */
export const DEFAULT_TIMEOUT_MS = 15_000;

/**
 * Longer timeout for state-mutating writes (esp. the QSO-log POST). A
 * timed-out write is AMBIGUOUS — the daemon may have committed it before
 * the response was lost — so a retry risks a double-write; give writes more
 * room to actually complete before giving up. Still bounded so a wedged
 * daemon can't hang the submit latch indefinitely.
 */
export const WRITE_TIMEOUT_MS = 30_000;

/**
 * Timeout for the session-email send. The daemon's SMTP send is allowed up
 * to 30 s of its own, and email is the worst write to double-fire (a real
 * message lands in a real inbox twice), so the SPA must outlast the daemon's
 * ceiling rather than give up first and tempt a re-send while the original
 * is still completing.
 */
export const EMAIL_TIMEOUT_MS = 45_000;

/**
 * Combine an optional caller abort signal with a timeout. Most calls pass
 * no signal, so they get the bare timeout; a caller that DOES pass one (an
 * operator-cancellable request, e.g. enrichment superseded by the next
 * keystroke) gets `AbortSignal.any` of the two, so either source aborts the
 * fetch. `timeoutMs <= 0` opts out of the timeout entirely (returns the
 * caller signal, if any) — for a deliberately unbounded request.
 */
function withTimeout(caller: AbortSignal | undefined, timeoutMs: number): AbortSignal | undefined {
    if (timeoutMs <= 0) return caller;
    const timeout = AbortSignal.timeout(timeoutMs);
    return caller ? AbortSignal.any([caller, timeout]) : timeout;
}

/**
 * Centralised `fetch` wrapper. Three responsibilities:
 *
 *   1. Catch the exception fetch throws on transport failure so each
 *      API helper doesn't repeat the try/catch boilerplate.
 *   2. Apply a default timeout (`opts.timeoutMs`, default
 *      `DEFAULT_TIMEOUT_MS`) so no call can hang forever on a wedged
 *      daemon — the whole point of this wrapper existing.
 *   3. Classify the failure so callers can react:
 *        - operator cancellation (their OWN `AbortController.abort()`, or
 *          `signal.aborted`) → `'aborted'`, which callers drop silently
 *          ("I moved on, ignore this");
 *        - a fired timeout or a genuine network error → `'network'`, which
 *          callers surface/retry. A timeout is a FAILURE, not a cancel, so
 *          it must NOT land in `'aborted'` (a caller silently dropping that
 *          arm would swallow the hung request — the original bug).
 *
 * Detection is belt-and-braces: error name (`AbortError` from manual abort,
 * `TimeoutError` from `AbortSignal.timeout()`) plus the caller `signal.aborted`
 * flag, because the platform isn't entirely consistent about which exception
 * class shows up first.
 */
export async function safeFetch(
    input: RequestInfo,
    init?: RequestInit,
    opts?: { timeoutMs?: number }
): Promise<FetchOutcome> {
    const timeoutMs = opts?.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    const signal = withTimeout(init?.signal ?? undefined, timeoutMs);
    try {
        const response = await fetch(input, signal ? { ...init, signal } : init);
        // Sampled here because this is the ONE place every daemon request passes
        // through, and because `Date` is stamped at send time — unlike an SSE frame,
        // it cannot arrive replayed from a cache (see daemonClock).
        noteDaemonDate(response.headers.get('date'));
        return { ok: true, response };
    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        // Operator cancellation via the caller's OWN signal → 'aborted'.
        // Checked before the error name so a caller abort is never misread.
        if (init?.signal?.aborted) {
            return { ok: false, kind: 'aborted', message };
        }
        if (err instanceof Error && err.name === 'AbortError') {
            return { ok: false, kind: 'aborted', message };
        }
        // A fired timeout is a failure the caller must see, not a silent
        // cancel — surface it as retriable 'network' with a clear message.
        if (err instanceof Error && err.name === 'TimeoutError') {
            return {
                ok: false,
                kind: 'network',
                message: `request timed out after ${timeoutMs} ms`,
                timedOut: true,
            };
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

/**
 * The operator-facing message for a non-2xx daemon response.
 *
 * The daemon's error envelope carries a human `message` and a machine `code`;
 * prefer the message, fall back to the code, and only then to the bare status.
 * A raw "Daemon error (400)." tells the operator that something was rejected but
 * not what or why — they cannot act on it, and it hides an explanation the
 * daemon already went to the trouble of writing (dogfood 2026-07-27: picking an
 * upload destination that cannot be filtered showed exactly that, with the real
 * reason sitting unread in the body).
 *
 * `body` is the already-parsed `readJsonBody` result, so a caller that needs the
 * body on the success path parses once and passes it here on the failure path.
 */
export function daemonErrorMessage(status: number, body: unknown): string {
    if (isPlainObject(body)) {
        if (typeof body.message === 'string' && body.message !== '') return body.message;
        if (typeof body.code === 'string' && body.code !== '') return body.code;
    }
    return `Daemon error (${status}).`;
}
