// Browser-tab title grammar (W-0004 AC2, operator-ratified 2026-08-27):
//   release:  "{View} · Station Manager"
//   dev:      "DEV · {View} · Station Manager"
//
// The DEV marker is a PREFIX, not a suffix: browsers truncate a tab label from the
// right, so a leading "DEV ·" stays visible even when the tab is narrow — the whole
// point of the marker. "·" matches the app's existing separator (occupancy captions
// read "1500 Hz · clear"). The full build string is NOT in the tab (it lives in the
// Sidebar footer); the tab carries the current view plus the marker.
//
// On an unknown identity the caller passes isDev=false, so the title is the neutral
// release form — a release daemon, or an unreachable one, is never falsely DEV.

const PRODUCT = 'Station Manager';

export function computeTitle(view: string, isDev: boolean): string {
    const base = `${view} · ${PRODUCT}`;
    return isDev ? `DEV · ${base}` : base;
}
