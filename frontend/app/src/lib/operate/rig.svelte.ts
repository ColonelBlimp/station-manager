// Rig state — the rig-provided operating context (band/mode/freq) that merges
// into a QSO at log time; qso.svelte deliberately excludes these fields so the
// draft stays operator-entered data only. Stub values for now: the Rig panel +
// the bridge's rig-state SSE (CAT) fill this for real, and manual entry covers
// the CAT-off case — same split as the shipping logging SPA.

export const rig: { band: string; mode: string } = $state({
    band: '20m',
    mode: 'SSB',
});
