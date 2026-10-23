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
        if (
            body !== null &&
            typeof body === 'object' &&
            (body as { code?: unknown }).code === 'archive_mismatch'
        ) {
            onMismatch();
        }
    } catch {
        // Not a JSON envelope: not the guard's refusal.
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
