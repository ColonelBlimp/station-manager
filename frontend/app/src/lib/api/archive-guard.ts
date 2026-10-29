// Archive request guard, client half (ADR 0094 update, 2026-10-23; W-0021
// "alpha.4 hold: fix design", revision 3). A request that acts on the archive or
// admits transmission names the archive this tab has PROVEN it serves, so a
// daemon that now serves another archive refuses it before any side effect
// (409 archive_mismatch). Provenance on a response cannot stop an action reaching
// the wrong archive; this can.
//
// The binding is pushed in by the archive layer (archives.svelte.ts) — this
// module imports nothing from it, so lib/api stays below config in the graph.
// Stop and reduce controls (disarm, abandon, autowork stop, unbag, skip-on) and
// the profile claim never come through here: a stale tab must always be able
// to stop.

import { safeFetch, type FetchOutcome } from './_helpers';

export const ARCHIVE_HEADER = 'X-SM-Archive-Id';

let bound: string | null = null;
let unproven: () => boolean = () => false;
let onMismatch: () => void = () => {};
let boundDefaultUuid: string | null = null;
let onDefaultMismatch: () => void = () => {};

/** The archive the boot bracket proved; null (or '') = nothing proven. */
export function setBoundArchive(id: string | null): void {
    bound = id !== null && id !== '' ? id : null;
}

/** The archive guarded requests name now; null when none is proven. */
export function boundArchive(): string | null {
    return bound;
}

/** True while the binding is being re-checked or a switch is unresolved. */
export function setArchiveBindingGate(isUnproven: () => boolean): void {
    unproven = isUnproven;
}

/** Called once per daemon archive_mismatch: the archive layer verifies and reloads. */
export function setArchiveMismatchSink(sink: () => void): void {
    onMismatch = sink;
}

const UNPROVEN_MESSAGE =
    'This page has not confirmed which archive the daemon serves, so nothing was sent. It re-checks by itself.';

// A local refusal shaped like a daemon error envelope, so every caller's existing
// error path reports it — with wording that says nothing was sent.
function unprovenRefusal(): FetchOutcome {
    return {
        ok: true,
        response: new Response(
            JSON.stringify({ code: 'archive_unproven', message: UNPROVEN_MESSAGE }),
            { status: 409, headers: { 'Content-Type': 'application/json' } }
        ),
    };
}

async function noteMismatch(response: Response): Promise<void> {
    try {
        const body: unknown = await response.json();
        const code =
            body !== null && typeof body === 'object' ? (body as { code?: unknown }).code : null;
        if (code === 'archive_mismatch') onMismatch();
        else if (code === 'default_logbook_mismatch') onDefaultMismatch();
    } catch {
        // Not a JSON envelope: not a guard's refusal.
    }
}

/**
 * safeFetch for a guarded request: adds the proven archive, or sends nothing
 * when none is proven. Only a 409 archive_mismatch starts a rebind; the request
 * is never retried (the replacement archive is not the one it was meant for).
 */
export async function guardedFetch(
    input: string,
    init: RequestInit = {},
    opts?: { timeoutMs?: number }
): Promise<FetchOutcome> {
    if (bound === null || unproven()) return unprovenRefusal();
    const headers = new Headers(init.headers);
    headers.set(ARCHIVE_HEADER, bound);
    const out = await safeFetch(input, { ...init, headers }, opts);
    if (out.ok && out.response.status === 409) {
        void noteMismatch(out.response.clone());
    }
    return out;
}

// ---- Default guard (W-0024 slice 4, ADR 0097 §4) ----------------------------
// A live request — the Phone / CW submit and every FT8 request that admits a
// new session or transmission — also names the default logbook this tab's
// established station context holds, so a daemon now serving another default
// refuses it (409 default_logbook_mismatch) before any side effect. The submit
// carries its own pair (lib/api/qso.ts); FT8, which names no logbook, asserts
// the bound default, set from that same context.

export const DEFAULT_HEADER = 'X-SM-Default-Logbook';

/** The default logbook UUID of the established station context; null = none. */
export function setBoundDefault(uuid: string | null): void {
    boundDefaultUuid = uuid !== null && uuid !== '' ? uuid : null;
}

/** The default live FT8 requests assert now; null when the context has none. */
export function boundDefault(): string | null {
    return boundDefaultUuid;
}

/** Called once per daemon default_logbook_mismatch: the tab reloads. */
export function setDefaultMismatchSink(sink: () => void): void {
    onDefaultMismatch = sink;
}

const NO_DEFAULT_MESSAGE =
    'This page has no confirmed default logbook, so nothing was sent. Reload the page.';

/**
 * guardedFetch for a live request: also names `defaultUuid`, the default the
 * tab asserts, or sends nothing when there is none. A 409
 * default_logbook_mismatch reloads the tab; the request is never retried.
 */
export async function liveFetch(
    input: string,
    init: RequestInit,
    defaultUuid: string | null,
    opts?: { timeoutMs?: number }
): Promise<FetchOutcome> {
    if (defaultUuid === null || defaultUuid === '') {
        return {
            ok: true,
            response: new Response(
                JSON.stringify({ code: 'default_unproven', message: NO_DEFAULT_MESSAGE }),
                { status: 409, headers: { 'Content-Type': 'application/json' } }
            ),
        };
    }
    const headers = new Headers(init.headers);
    headers.set(DEFAULT_HEADER, defaultUuid);
    return guardedFetch(input, { ...init, headers }, opts);
}
