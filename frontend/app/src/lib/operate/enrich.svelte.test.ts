// The SP/LP antenna-path preference is shared session state (a future rotator
// reads it), so it must default to SHORT path for each new QSO rather than
// carrying a previous station's long-path choice over. observeCall is the single
// reaction both hosts drive (Phone/CW callsign + FT8 worked call, via the shared
// EnrichmentCard), so the reset lives there.

import { describe, it, expect, beforeEach } from 'vitest';
import { observeCall, prefs } from './enrich.svelte';

beforeEach(() => {
    observeCall(''); // idle branch clears the module's per-station tracker
    prefs.path = 'sp';
});

describe('SP/LP path resets to short on each new station', () => {
    it('a new station snaps the path back to short', () => {
        prefs.path = 'lp';
        observeCall('W1ABC');
        expect(prefs.path).toBe('sp');
    });

    it('does NOT fight the operator toggling LP for the SAME station', () => {
        observeCall('W1ABC'); // → sp
        prefs.path = 'lp'; // operator deliberately switches to long path
        observeCall('W1ABC'); // same call again (re-render) — must not reset
        expect(prefs.path).toBe('lp');
    });

    it('a different station resets to short again', () => {
        observeCall('W1ABC');
        prefs.path = 'lp';
        observeCall('G3XYZ'); // new station
        expect(prefs.path).toBe('sp');
    });

    it('after a clear, the next station is treated as new', () => {
        observeCall('W1ABC');
        prefs.path = 'lp';
        observeCall(''); // logged / cleared draft
        observeCall('W1ABC'); // same call, but a fresh QSO
        expect(prefs.path).toBe('sp');
    });
});
