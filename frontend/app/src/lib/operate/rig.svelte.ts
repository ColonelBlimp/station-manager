// Rig state — the rig-provided operating context (band/mode/freq) that merges
// into a QSO at log time; qso.svelte deliberately excludes these fields so the
// draft stays operator-entered data only. Two sources, same as the shipping
// logging SPA: CAT-connected = the bridge's rig-state SSE writes here (rig is
// authoritative, panel fields lock); CAT-off = manual entry in the Rig panel.
// The SSE wiring lands with /v1; until then the link status is dev-simulated
// from the panel.

export type CatLink = 'off' | 'connected' | 'lost';

export const rig: { band: string; mode: string; freq: string; cat: CatLink } = $state({
    band: '20m',
    mode: 'SSB',
    freq: '14.255',
    cat: 'off',
});

// The CAT gate for logging. 'off' = no bridge, the manual values are the
// operator's own entry — trusted. 'connected' = rig-pushed — trusted. 'lost' =
// a bridge is configured but the rig is gone: the displayed context may be
// stale, so logging blocks rather than record a QSO against a wrong band/mode.
export function rigReady(): boolean {
    return rig.cat !== 'lost';
}
