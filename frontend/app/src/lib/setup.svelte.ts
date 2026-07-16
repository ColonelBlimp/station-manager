/*
    First-run setup gate — whether the whole app renders the SetupCard
    instead of the shell. Every logbook-backed surface (map, logbook page,
    header count) 404s until the default logbook row exists, so the gate
    sits app-wide, above the router (including the shell-less /map tab).

    Status resolution (main.ts, from the boot /v1/config fetch):
      - loading   → hold blank; the fetch settles in tens of ms on localhost
      - needed    → config reached AND setup_complete=false → SetupCard
      - complete  → set up, OR the daemon was unreachable — an outage must
        render the normal fail-soft shell, not greet a configured operator
        with first-run setup.

    ADR 0045: this module never imports lib/api — main.ts injects the save
    action (PUT + station-context re-fetch/re-wire) via setSetupSave.
*/

export type SetupStatus = 'loading' | 'needed' | 'complete';

export const setup: { status: SetupStatus; justCompleted: boolean } = $state({
    status: 'loading',
    // Post-save interstitial: hold on the "setup complete" screen (offering
    // Settings) instead of dropping straight into the shell. Session-local —
    // only ever true right after THIS session's save, so a returning operator
    // never sees it.
    justCompleted: false,
});

export interface SetupSaveResult {
    ok: boolean;
    message: string;
}

type SetupSave = (callsign: string) => Promise<SetupSaveResult>;

let saveFn: SetupSave | null = null;

/** Inject the save action (main.ts): PUT the callsign, then re-fetch and
 *  re-apply the station context so the freshly-seeded logbook is live. */
export function setSetupSave(fn: SetupSave): void {
    saveFn = fn;
}

/** Complete setup with the operator's callsign (already normalised by the
 *  card). On success the gate opens and the interstitial shows. */
export async function saveSetup(callsign: string): Promise<SetupSaveResult> {
    if (saveFn === null) {
        return { ok: false, message: 'Setup is not available — reload the page.' };
    }
    const out = await saveFn(callsign);
    if (out.ok) {
        setup.status = 'complete';
        setup.justCompleted = true;
    }
    return out;
}

/** Leave the interstitial (Start / Open Settings) — the shell renders. */
export function dismissSetupDone(): void {
    setup.justCompleted = false;
}

/** Test seam — restore the singleton between cases. */
export function _resetSetupForTests(): void {
    setup.status = 'loading';
    setup.justCompleted = false;
    saveFn = null;
}
