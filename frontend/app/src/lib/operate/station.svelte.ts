// Always-visible station identity for the header — the default logbook this
// session writes to and the configured rig, both from /v1/config (ADR 0045: this
// module never imports lib/api; main.ts injects via setStationInfo at boot).
//
// Reactive $state rather than the plain-let-accessor pattern the FT8 prefs use,
// because the Header mounts synchronously at app start — BEFORE the async config
// fetch resolves — so it must re-render when the values arrive.

export const station: { logbookName: string; rigName: string; logbookQsoCount: number } = $state({
    logbookName: '',
    rigName: '',
    // Total QSOs in the default logbook — the header's live "(n)" readout. Seeded
    // at boot and re-fetched after every logged QSO (main.ts), so it ticks up as
    // the operator works stations.
    logbookQsoCount: 0,
});

export function setStationInfo(p: { logbookName?: string; rigName?: string }): void {
    if (p.logbookName !== undefined) station.logbookName = p.logbookName;
    if (p.rigName !== undefined) station.rigName = p.rigName;
}

/** The live logbook QSO count, injected from the /v1/logbook/{id}/count seam. */
export function setLogbookCount(n: number): void {
    station.logbookQsoCount = n;
}

/** Test seam — restore the singleton between cases. */
export function _resetStationForTests(): void {
    station.logbookName = '';
    station.rigName = '';
    station.logbookQsoCount = 0;
}
