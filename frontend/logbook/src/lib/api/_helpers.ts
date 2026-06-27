/*
    Shared internals for the logbook SPA's API helpers — the same fetch / JSON /
    shape-guard primitives the logging + config SPAs use (kept in sync deliberately).
    The boundary: the API layer is the last hop before SPA code sees a typed result,
    so any runtime sloppiness about response shape stops here, not mid-render.
*/

export type FetchOutcome =
    | { ok: true; response: Response }
    | { ok: false; kind: 'aborted'; message: string }
    | { ok: false; kind: 'network'; message: string };

/**
 * Centralised `fetch` wrapper: catches the transport-failure exception and
 * distinguishes an abort/timeout from a generic network error so callers can
 * surface the difference. Callers that pass no signal never see the `'aborted'` arm.
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

/** Parse a response body as JSON, returning `null` on parse failure rather than throwing. */
export async function readJsonBody(response: Response): Promise<unknown> {
    try {
        return await response.json();
    } catch {
        return null;
    }
}

/** True when `value` is a plain JSON-style object (non-null, non-array). */
export function isPlainObject(value: unknown): value is Record<string, unknown> {
    return value !== null && typeof value === 'object' && !Array.isArray(value);
}
