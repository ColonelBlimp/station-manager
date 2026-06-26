// Pure helpers for the FT8 "Spectrum" occupancy view — a continuous (un-channelised)
// presentation of the same per-slot occupancy snapshot the channelised strip uses.
// Kept here (not in the component) so the proximity grading + click→offset mapping
// are unit-testable without rendering.
import type { Ft8Band } from '../states/ft8.svelte';

/** How the selected TX footprint relates to the occupied signals:
 *  - 'sharing'  — the footprint overlaps a signal (usually fine in FT8: strong FEC,
 *                 both typically still decode — softer than the channelised "busy/red").
 *  - 'near'     — a signal sits within `nearMargin` Hz but does NOT overlap.
 *  - 'clear'    — nearest signal is further than `nearMargin` (or none at all). */
export type Proximity = 'clear' | 'near' | 'sharing';

export interface ProximityResult {
    kind: Proximity;
    /** Hz to the nearest signal edge: 0 when overlapping (sharing); the gap when
     *  near/clear with signals present; null when no signals are present. */
    gapHz: number | null;
}

/** Grade where the footprint [offset, offset+signalWidth] sits relative to the
 *  occupied bands. Position-only (Ft8Band carries no strength), so it can't tell a
 *  loud signal from a weak one — that needs FFT magnitudes (the waterfall). */
export function signalProximity(
    offset: number,
    signalWidth: number,
    occupied: Ft8Band[],
    nearMargin: number
): ProximityResult {
    if (occupied.length === 0) return { kind: 'clear', gapHz: null };
    const lo = offset;
    const hi = offset + signalWidth;
    let nearest = Infinity;
    for (const b of occupied) {
        if (lo < b.high_hz && hi > b.low_hz) return { kind: 'sharing', gapHz: 0 };
        // Footprint is entirely below (b.low_hz - hi) or above (lo - b.high_hz) this band.
        const gap = lo >= b.high_hz ? lo - b.high_hz : b.low_hz - hi;
        if (gap >= 0 && gap < nearest) nearest = gap;
    }
    if (nearest === Infinity) return { kind: 'clear', gapHz: null };
    const gapHz = Math.round(nearest);
    return { kind: gapHz <= nearMargin ? 'near' : 'clear', gapHz };
}

/** Map a 0..1 fraction across the passband to a TX offset (whole Hz), clamped so
 *  the signal footprint stays inside the passband. Used by the click-anywhere bar
 *  and the keyboard nudge. */
export function offsetFromFraction(
    frac: number,
    passbandLow: number,
    passbandHigh: number,
    signalWidth: number
): number {
    const span = Math.max(1, passbandHigh - passbandLow);
    const raw = passbandLow + frac * span;
    return clampOffset(raw, passbandLow, passbandHigh, signalWidth);
}

/** Clamp an offset (whole Hz) so [offset, offset+signalWidth] stays in the passband. */
export function clampOffset(
    hz: number,
    passbandLow: number,
    passbandHigh: number,
    signalWidth: number
): number {
    const maxOffset = Math.max(passbandLow, passbandHigh - signalWidth);
    return Math.round(Math.min(Math.max(hz, passbandLow), maxOffset));
}
