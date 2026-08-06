/*
    RX audio-level state (dogfood 2026-08-06) — the classification half of the
    FT8 level meter. The daemon measures (ft8-audio-level, peak+RMS dBFS,
    ~4 Hz while capture is live); this module classifies against the
    config-served window (ft8_audio on /v1/config, resolved daemon-side,
    calibratable in config.json) plus a FIXED near-full-scale clipping check —
    full scale is a property of int16 audio, not of the operator's station.

    Staleness is client-side by design: no event for STALE_MS (~8 missed
    windows) means capture is not delivering — device gone, daemon released
    the mic, stream stalled — and the card must show that as its own state,
    never as "too low". A silent BAND is different: silence still arrives on
    cadence as the daemon's finite -120 floor and truthfully reads 'low'.

    Criterion + rules: audioLevel.svelte.test.ts. The card (AudioLevelCard)
    owns presentation and the TX stand-down; this module never reads TX state.
*/

export interface AudioLevelPayload {
    peak_dbfs: number;
    rms_dbfs: number;
}

/** Peaks within 1 dB of full scale are clipping territory — fixed, not config. */
const CLIP_PEAK_DBFS = -1;

/** ~8 missed 250 ms windows: long enough to ride out SSE jitter, short
 *  enough that a dead capture is flagged before the operator keys anything. */
const STALE_MS = 2000;

export const audioLevel = $state({
    peakDbfs: null as number | null,
    rmsDbfs: null as number | null,
    stale: false,
    /** Card open/collapsed — module-level so it survives FT8 view remounts. */
    open: false,
});

// The config-served window. Injected at boot (main.ts) from /v1/config's
// resolved ft8_audio; the defaults here only cover a failed config fetch.
let lowDbfs = -60;
let highDbfs = -10;

export function setFt8AudioWindow(low: number, high: number): void {
    lowDbfs = low;
    highDbfs = high;
}

let staleTimer: ReturnType<typeof setTimeout> | undefined;

export function onAudioLevel(p: AudioLevelPayload): void {
    audioLevel.peakDbfs = p.peak_dbfs;
    audioLevel.rmsDbfs = p.rms_dbfs;
    audioLevel.stale = false;
    clearTimeout(staleTimer);
    staleTimer = setTimeout(() => {
        audioLevel.stale = true;
    }, STALE_MS);
}

export type AudioLevelStatus = 'off' | 'stale' | 'high' | 'low' | 'good';

/** Classify the latest reading. Reactive when called from a $derived. */
export function audioLevelStatus(): AudioLevelStatus {
    if (audioLevel.peakDbfs === null || audioLevel.rmsDbfs === null) return 'off';
    if (audioLevel.stale) return 'stale';
    // Order matters: clipping wins over everything — a clipped signal can
    // carry any RMS. The window is inclusive at both bounds (C7).
    if (audioLevel.peakDbfs >= CLIP_PEAK_DBFS || audioLevel.rmsDbfs > highDbfs) return 'high';
    if (audioLevel.rmsDbfs < lowDbfs) return 'low';
    return 'good';
}

export function setAudioLevelOpen(on: boolean): void {
    audioLevel.open = on;
}

/** Test seam — no reading, defaults window, timer cleared. */
export function resetAudioLevel(): void {
    clearTimeout(staleTimer);
    staleTimer = undefined;
    audioLevel.peakDbfs = null;
    audioLevel.rmsDbfs = null;
    audioLevel.stale = false;
    audioLevel.open = false;
    lowDbfs = -60;
    highDbfs = -10;
}
