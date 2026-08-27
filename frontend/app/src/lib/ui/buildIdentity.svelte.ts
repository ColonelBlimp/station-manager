/*
    Shell build identity (W-0004 AC1/AC2/AC3).

    One always-loaded source of the running daemon's build string + environment,
    read by the Sidebar footer and the document title. Deliberately separate from
    Settings' general.svelte About panel: that one loads lazily when Settings opens,
    while the shell needs identity from first paint.

    FETCH POLICY (operator decision, 2026-08-27): once at boot, then once per SSE
    reconnection TRANSITION — an open that follows a drop. Never on a schedule, never
    on the first connect (the boot fetch already covered that). The shell's rig SSE is
    the transport that drives this; when CAT is off no stream runs, so recovery from a
    transient outage is by the next boot or a reload — both honest (AC3).

    HONEST FALLBACK. An unreachable or malformed /v1/version resolves to 'unavailable';
    info stays null and no plausible version is ever fabricated. isDevDaemon() is false
    for any non-ready or non-dev state, so an unknown identity can never falsely mark a
    daemon DEV (AC2).
*/

import { fetchBuildInfo, type BuildInfo } from '../api/general';

type IdentityStatus = 'loading' | 'ready' | 'unavailable';

interface BuildIdentity {
    status: IdentityStatus;
    info: BuildInfo | null;
}

export const buildIdentity = $state<BuildIdentity>({ status: 'loading', info: null });

// A transport error seen since the last successful open. This is what separates a
// reconnection (error → open) from the very first connect (open with no prior error).
let sawError = false;

/** Fetch the daemon's build identity and settle status. Idempotent; safe to re-run. */
export async function loadBuildIdentity(): Promise<void> {
    const res = await fetchBuildInfo();
    if (res.kind === 'ok') {
        buildIdentity.info = res.info;
        buildIdentity.status = 'ready';
    } else {
        buildIdentity.info = null;
        buildIdentity.status = 'unavailable';
    }
}

/** The shell SSE reported a transport error (stream down / reconnecting). */
export function noteStreamError(): void {
    sawError = true;
}

/** The shell SSE (re)opened. Re-fetch only when this open follows a drop — one
 *  re-fetch per reconnection transition; the boot open is left to the boot fetch. */
export function noteStreamReopen(): void {
    if (!sawError) return;
    sawError = false;
    void loadBuildIdentity();
}

/** Whether the running daemon is a development build. False unless identity is ready
 *  AND explicitly dev — an unknown identity never claims DEV (AC2). */
export function isDevDaemon(): boolean {
    return buildIdentity.status === 'ready' && buildIdentity.info?.env === 'dev';
}

/** Test seam: restore the pre-boot state. */
export function resetBuildIdentityForTests(): void {
    buildIdentity.status = 'loading';
    buildIdentity.info = null;
    sawError = false;
}
