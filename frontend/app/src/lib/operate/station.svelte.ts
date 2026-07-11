// Always-visible station identity for the header — the default logbook this
// session writes to and the configured rig, both from /v1/config (ADR 0045: this
// module never imports lib/api; main.ts injects via setStationInfo at boot).
//
// Reactive $state rather than the plain-let-accessor pattern the FT8 prefs use,
// because the Header mounts synchronously at app start — BEFORE the async config
// fetch resolves — so it must re-render when the values arrive.

export const station: { logbookName: string; rigName: string } = $state({
    logbookName: '',
    rigName: '',
});

export function setStationInfo(p: Partial<typeof station>): void {
    if (p.logbookName !== undefined) station.logbookName = p.logbookName;
    if (p.rigName !== undefined) station.rigName = p.rigName;
}

/** Test seam — restore the singleton between cases. */
export function _resetStationForTests(): void {
    station.logbookName = '';
    station.rigName = '';
}
