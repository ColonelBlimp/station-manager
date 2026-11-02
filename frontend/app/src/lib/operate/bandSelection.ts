// View-aware band selection (W-0012 Phone/CW band memory). One band action per
// view (ruling 3): buttons, digit keys and next/previous all resolve their
// destination here. Phone/CW goes to its own remembered position on the band,
// else an explicit default (ruling 2; 60m per ruling 6); FT8/FT4 go to their
// configured dial and never touch Phone/CW memory (AC2).
//
// Slice 1 is the pure destination and the CAT-off application only. The live
// sequence — stop-then-retune and AC6's failure and ordering rules — is a
// separate slice, so nothing here sends a rig command.

import { formatFrequency, frequencyToBand, bandLowerEdgeHz } from '../utils/frequency';
import {
    rig,
    operatingBands,
    ftFrequencyFor,
    ftBandsWithDial,
    type FtMode,
    type RigWriteResult,
} from './rig.svelte';

export type BandView = 'phone' | 'ft8' | 'ft4';

/** A Phone/CW position: whole hertz and the operator-friendly mode (USB, LSB, CW …). */
export interface Position {
    hz: number;
    mode: string;
}

export type Destination =
    { kind: 'tune'; band: string; hz: number; mode: string } | { kind: 'none'; message: string };

export type MemoryLookup = (band: string) => Position | undefined;

// Ruling 2 (2026-11-02). Frequencies are BAND_DEFAULT_HZ's; modes per the IARU
// Region 1 HF band plan as cited in W-0012. 60m is deliberately absent:
// ruling 6 leaves its frequency unset, and a band with no entry has no default.
const PHONE_DEFAULTS: Record<string, Position> = {
    '160m': { hz: 1_900_000, mode: 'LSB' },
    '80m': { hz: 3_700_000, mode: 'LSB' },
    '40m': { hz: 7_100_000, mode: 'LSB' },
    '30m': { hz: 10_125_000, mode: 'CW' },
    '20m': { hz: 14_200_000, mode: 'USB' },
    '17m': { hz: 18_130_000, mode: 'USB' },
    '15m': { hz: 21_300_000, mode: 'USB' },
    '12m': { hz: 24_950_000, mode: 'USB' },
    '10m': { hz: 28_400_000, mode: 'USB' },
    '6m': { hz: 50_150_000, mode: 'USB' },
};

// A remembered position counts only if it can be a position on that band:
// whole hertz inside the band, with a mode.
function usableMemory(band: string, p: Position | undefined): p is Position {
    return (
        p !== undefined &&
        Number.isInteger(p.hz) &&
        frequencyToBand(p.hz) === band &&
        typeof p.mode === 'string' &&
        p.mode !== ''
    );
}

function ftLabel(view: FtMode): string {
    return view.toUpperCase();
}

/** Where a selection of `band` goes in `view`. FT views never read `memory`. */
export function resolveBandDestination(
    view: BandView,
    band: string,
    memory: MemoryLookup
): Destination {
    if (view !== 'phone') {
        const hz = ftFrequencyFor(view, band);
        if (hz === undefined) {
            return {
                kind: 'none',
                message: `No ${ftLabel(view)} frequency configured for ${band}.`,
            };
        }
        // Config accepts any positive dial, so one can name another band's
        // frequency; displaying "20m" over 7.074 MHz would be a lie (review
        // 2026-11-02, P2).
        if (frequencyToBand(hz) !== band) {
            return {
                kind: 'none',
                message: `The ${ftLabel(view)} frequency configured for ${band} (${formatFrequency(hz)}) is not on ${band}.`,
            };
        }
        return { kind: 'tune', band, hz, mode: ftLabel(view) };
    }
    const remembered = memory(band);
    const p = usableMemory(band, remembered) ? remembered : PHONE_DEFAULTS[band];
    if (p === undefined) {
        return { kind: 'none', message: `No Phone/CW default configured for ${band}.` };
    }
    return { kind: 'tune', band, hz: p.hz, mode: p.mode };
}

/** The bands next/previous visits, in configured order: the operating bands
 *  (FT8/FT4: those carrying that profile's dial) that have a usable destination.
 *  A Phone/CW band with neither valid memory nor a default, or an FT dial that
 *  is not on its band, is skipped (ruling (a), 2026-11-02); selecting one
 *  directly still explains why nothing moved. */
export function eligibleBands(view: BandView, memory: MemoryLookup): string[] {
    const candidates = view === 'phone' ? operatingBands() : ftBandsWithDial(view);
    return candidates.filter((b) => resolveBandDestination(view, b, memory).kind === 'tune');
}

/**
 * The band one step from `current` (next/previous ruling, 2026-11-02). On-list:
 * configured order, wrapping. Off-list but a known band: nearest eligible band
 * above (next) or below (previous) in frequency order, wrapping to the lowest or
 * highest. Unknown current: the first (next) or last (previous) configured band.
 * Null when nothing is eligible.
 */
export function stepBand(eligible: string[], current: string, dir: 1 | -1): string | null {
    if (eligible.length === 0) return null;
    const i = eligible.indexOf(current);
    if (i !== -1) {
        return eligible[(i + dir + eligible.length) % eligible.length];
    }
    const from = bandLowerEdgeHz(current);
    if (from === undefined) {
        return dir === 1 ? eligible[0] : eligible[eligible.length - 1];
    }
    // Eligible bands the table knows, low to high. A configured name the table
    // cannot place has no frequency order and is reachable only on-list.
    const ordered = eligible
        .map((b) => ({ b, hz: bandLowerEdgeHz(b) }))
        .filter((x): x is { b: string; hz: number } => x.hz !== undefined)
        .sort((x, y) => x.hz - y.hz);
    if (ordered.length === 0) {
        return dir === 1 ? eligible[0] : eligible[eligible.length - 1];
    }
    if (dir === 1) {
        return (ordered.find((x) => x.hz > from) ?? ordered[0]).b;
    }
    const lower = ordered.filter((x) => x.hz < from);
    return (lower[lower.length - 1] ?? ordered[ordered.length - 1]).b;
}

/** The destination of a next (1) or previous (-1) press from `current`. */
export function resolveStepDestination(
    view: BandView,
    current: string,
    dir: 1 | -1,
    memory: MemoryLookup
): Destination {
    const band = stepBand(eligibleBands(view, memory), current, dir);
    if (band === null) {
        return {
            kind: 'none',
            message:
                view === 'phone'
                    ? 'No operating band has a Phone/CW position or default.'
                    : `No operating band has a usable ${ftLabel(view)} frequency configured.`,
        };
    }
    return resolveBandDestination(view, band, memory);
}

/**
 * AC7: with CAT off a selection changes only the displayed context — band,
 * frequency and (Phone/CW) mode. FT keeps today's CAT-off behaviour of leaving
 * the displayed mode alone (operator ruling 2026-11-02, AC7). Refused, and nothing changed, when there is no
 * destination — or when CAT is connected, which is the live slice's path.
 */
export function selectBandCatOff(
    view: BandView,
    band: string,
    memory: MemoryLookup
): RigWriteResult {
    if (rig.cat === 'connected') {
        return {
            status: 'failed',
            kind: 'refused',
            message: 'The rig is connected; CAT-off selection does not apply.',
        };
    }
    const dest = resolveBandDestination(view, band, memory);
    if (dest.kind === 'none') {
        return { status: 'failed', kind: 'refused', message: dest.message };
    }
    rig.band = dest.band;
    rig.freq = formatFrequency(dest.hz);
    if (view === 'phone') rig.mode = dest.mode;
    return { status: 'accepted' };
}
