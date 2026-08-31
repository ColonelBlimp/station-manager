// Shared primitives for the per-event SSE decoders (F-03, ADR 0077). These are small type
// guards + a throttled diagnostic — NOT a generic schema layer; the per-event validators
// that use them live beside each stream (rig-sse.ts, ft8-sse.ts) and read as the wire
// contract. A frame failing its validator is dropped, leaving the last known good state.

import { isPlainObject } from './_helpers';

export { isPlainObject };

/** Present-or-typed field guards for optional wire fields (a partial-merge payload sends
 *  only the fields that changed, so absence is valid but a present wrong type is not). */
export const optNum = (v: unknown): boolean => v === undefined || typeof v === 'number';
export const optStr = (v: unknown): boolean => v === undefined || typeof v === 'string';
export const optBool = (v: unknown): boolean => v === undefined || typeof v === 'boolean';

/** Every element of an array satisfies the element guard (validates array-ness AND the
 *  element shape, so a consumer never spreads/#each-es a non-array or a malformed element). */
export const isArrayOf = <T>(v: unknown, guard: (x: unknown) => x is T): v is T[] =>
    Array.isArray(v) && v.every(guard);

/** makeSseWarn returns a warn(event, reason) that logs at most once per (event, reason) for
 *  the life of ONE stream subscription. A fresh subscription (a new openXEvents call after
 *  close) gets a fresh instance, resetting the throttle — deterministic, no time interval. */
export function makeSseWarn(
    prefix: string
): (event: string, reason: string, detail?: unknown) => void {
    const seen = new Set<string>();
    return (event, reason, detail) => {
        const key = `${event} ${reason}`;
        if (seen.has(key)) return;
        seen.add(key);
        console.warn(`[${prefix}] dropped ${event}: ${reason}`, detail);
    };
}

/** decodeFrame parses one SSE frame and validates it with a per-event guard. On malformed
 *  JSON or a wrong shape it warns (throttled) and returns null, so the caller drops the
 *  frame and the last known good state stands. */
export function decodeFrame<T>(
    data: string,
    event: string,
    validate: (v: unknown) => v is T,
    warn: (event: string, reason: string, detail?: unknown) => void
): T | null {
    let parsed: unknown;
    try {
        parsed = JSON.parse(data);
    } catch {
        warn(event, 'invalid_json');
        return null;
    }
    if (!validate(parsed)) {
        warn(event, 'invalid_shape', parsed);
        return null;
    }
    return parsed;
}
