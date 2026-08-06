/*
    RX audio-level state — classification of the daemon's ft8-audio-level
    measurements (dogfood 2026-08-06).

    ACCEPTANCE CRITERION (operator-agreed 2026-08-06):

        When the FT8 view is open with live capture, I see the incoming audio
        level continuously: GREEN in the good decoding window, RED when the
        rig is driving the input into clipping (or simply running hot),
        ORANGE when it is too low for reliable decode — and I can tell each
        apart from NO AUDIO ARRIVING AT ALL: a dead or idle capture shows as
        its own state, never as merely "too low".

    The daemon publishes measurements only (peak+RMS dBFS, ~4 Hz); THIS
    module classifies, against the config-served window (ft8_audio, resolved
    daemon-side, operator-calibratable in config.json). Clipping is a FIXED
    near-full-scale peak check, not a config knob — full scale is a property
    of int16, not of the operator's station.

    "Dead or idle capture" is a client-side STALENESS rule (no event for
    2 s ≈ 8 missed windows), deliberately: it catches every silent failure
    shape — device gone, daemon released capture, stream stalled — without
    the daemon having to enumerate them. A SILENT band is not stale: silence
    still arrives on cadence, as the -120 floor (reads 'low', truthfully).

    The TX stand-down ('the meter shows nothing useful while keyed') is the
    CARD's concern — it reads ft8State.tx — so this module stays a pure
    classifier of what capture reports.
*/

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    audioLevel,
    onAudioLevel,
    audioLevelStatus,
    setFt8AudioWindow,
    resetAudioLevel,
} from './audioLevel.svelte';

beforeEach(() => {
    vi.useFakeTimers();
    resetAudioLevel(); // defaults window (-60, -10), no reading, not stale
});

afterEach(() => {
    vi.useRealTimers();
});

describe('RX audio level classification', () => {
    // C1 — NO READING YET IS ITS OWN STATE, not a colour: the card renders
    // "no capture", which is the criterion's nearest-confusable guard.
    it('C1: reports off before any measurement arrives', () => {
        expect(audioLevelStatus()).toBe('off');
    });

    // C2 — the good window.
    it('C2: a level inside the window is good', () => {
        onAudioLevel({ peak_dbfs: -20, rms_dbfs: -30 });
        expect(audioLevelStatus()).toBe('good');
    });

    // C3 — running hot on RMS alone (no clipping yet) is already high: the
    // window's upper bound exists to warn BEFORE the peaks hit the ceiling.
    it('C3: RMS above the window is high', () => {
        onAudioLevel({ peak_dbfs: -4, rms_dbfs: -6 });
        expect(audioLevelStatus()).toBe('high');
    });

    // C3b — CLIPPING is high even with a modest RMS: a signal can clip on
    // peaks while its average sits inside the window, and that is exactly
    // the case the peak check exists for. The fixture's RMS is mid-window,
    // so an implementation classifying on RMS alone reads 'good' and fails.
    it('C3b: a near-full-scale peak is high regardless of RMS', () => {
        onAudioLevel({ peak_dbfs: -0.5, rms_dbfs: -30 });
        expect(audioLevelStatus()).toBe('high');
    });

    // C4 — too quiet to decode reliably.
    it('C4: RMS below the window is low', () => {
        onAudioLevel({ peak_dbfs: -60, rms_dbfs: -75 });
        expect(audioLevelStatus()).toBe('low');
    });

    // C4b — SILENCE reads low, not stale/off: the floor value arriving on
    // cadence is a live capture hearing nothing (or a gain at zero) — the
    // distinction the daemon's finite floor exists to carry.
    it('C4b: the silence floor is low, not stale', () => {
        onAudioLevel({ peak_dbfs: -120, rms_dbfs: -120 });
        expect(audioLevelStatus()).toBe('low');
    });

    // C5 — STALENESS: events stop → stale after ~2 s; a fresh event clears
    // it. This is what tells a dead capture apart from a silent one.
    it('C5: goes stale when measurements stop, recovers on the next one', () => {
        onAudioLevel({ peak_dbfs: -20, rms_dbfs: -30 });
        expect(audioLevelStatus()).toBe('good');

        vi.advanceTimersByTime(2100);
        expect(audioLevelStatus()).toBe('stale');

        onAudioLevel({ peak_dbfs: -20, rms_dbfs: -30 });
        expect(audioLevelStatus()).toBe('good');
    });

    // C6 — THE WINDOW IS THE CONFIG'S, not a constant: an RMS of -15 is HIGH
    // under the injected (-70, -20) window and GOOD under the (-60, -10)
    // defaults — the same fixture value classifies differently, which is
    // what distinguishes injection from hardcoding.
    it('C6: classifies against the injected config window', () => {
        setFt8AudioWindow(-70, -20);
        onAudioLevel({ peak_dbfs: -10, rms_dbfs: -15 });
        expect(audioLevelStatus()).toBe('high');
    });

    // C7 — the window is INCLUSIVE at both bounds: sitting exactly on a
    // bound is inside it. Pinned so a later rewrite cannot silently flip a
    // boundary reading between colours.
    it('C7: levels exactly on the bounds are good', () => {
        onAudioLevel({ peak_dbfs: -30, rms_dbfs: -10 });
        expect(audioLevelStatus()).toBe('good');
        onAudioLevel({ peak_dbfs: -50, rms_dbfs: -60 });
        expect(audioLevelStatus()).toBe('good');
    });

    // C8 — the raw numbers are kept for the card's readout, rounded as the
    // wire sent them.
    it('C8: exposes the latest peak and RMS', () => {
        onAudioLevel({ peak_dbfs: -12.3, rms_dbfs: -34.5 });
        expect(audioLevel.peakDbfs).toBe(-12.3);
        expect(audioLevel.rmsDbfs).toBe(-34.5);
    });
});
