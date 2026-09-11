// The router is base-AGNOSTIC: it strips Vite's BASE_URL before parsing and re-adds
// it before writing the URL, so it routes correctly under ANY base. It now serves at
// the canonical root (base '' — the root case below); the '/app' cases pin that same
// round-trip under a NON-EMPTY base — the FORMER '/app/' transition mount, where a
// missing strip reverted '/app/…' to '/' (a different SPA) and the URL jumped off.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { subPathOf, urlOf, router, setMode, setModeChangeHook, isFtMode } from './router.svelte';

describe('router base-path handling', () => {
    it('strips a non-empty base before parsing (the former /app mount)', () => {
        expect(subPathOf('/app/operate/ft8', '/app')).toBe('/operate/ft8');
        expect(subPathOf('/app/logbook', '/app')).toBe('/logbook');
        expect(subPathOf('/app/', '/app')).toBe('/'); // default view
        expect(subPathOf('/app', '/app')).toBe('/'); // no trailing slash
    });

    it('re-adds a non-empty base when building the URL (the former /app mount)', () => {
        expect(urlOf('operate', 'ft8', '/app')).toBe('/app/operate/ft8');
        expect(urlOf('operate', 'phone', '/app')).toBe('/app/operate/phone');
        expect(urlOf('logbook', 'phone', '/app')).toBe('/app/logbook');
        expect(urlOf('config', 'phone', '/app')).toBe('/app/config');
        expect(urlOf('map', 'phone', '/app')).toBe('/app/map'); // contacts-map tab
        expect(urlOf('dashboard', 'phone', '/app')).toBe('/app/'); // default → base root
    });

    it('is base-agnostic — works unchanged when served at the root', () => {
        expect(subPathOf('/operate/ft8', '')).toBe('/operate/ft8');
        expect(subPathOf('/', '')).toBe('/');
        expect(urlOf('operate', 'ft8', '')).toBe('/operate/ft8');
        expect(urlOf('dashboard', 'phone', '')).toBe('/');
    });
});

/*
    The operating-mode change notification — the router is where a Phone/CW ↔ FT8
    switch is decided, so it is where the rig's operating-state restore has to be
    told (lib/operate/modeRestore). Two doors reach it, and the operator's ruling
    (2026-08-05) is that BOTH count as a switch: the sidebar buttons and browser
    Back/Forward. Back landing you on Phone with the rig still on the FT8 dial is
    the confusion the feature exists to remove, whichever way you got there.

    What these pin is the WIRING — that both doors notify, that a click which
    changes nothing stays silent, and that the mode has already moved when the
    notification lands. What is DONE with it (snapshot, diff, re-tune, the TX
    refusal) belongs to modeRestore and is pinned in its own tests.
*/
describe('operating-mode change notification', () => {
    let seen: { from: string; to: string; modeThen: string }[] = [];

    beforeEach(() => {
        seen = [];
        setModeChangeHook((from, to) => {
            // Captured INSIDE the hook: the restore reads router state, so it
            // matters that the mode has already moved by the time it runs.
            seen.push({ from, to, modeThen: router.mode });
        });
        setMode('phone');
        seen = [];
    });

    afterEach(() => {
        setModeChangeHook(null);
        vi.restoreAllMocks();
    });

    it('notifies on a mode switch from the sidebar, with the mode already moved', () => {
        setMode('ft8');
        expect(seen).toEqual([{ from: 'phone', to: 'ft8', modeThen: 'ft8' }]);
    });

    // Clicking the mode you are already in is a view change at most (it exits
    // Settings). Re-tuning the rig on it would be a command nobody asked for.
    it('stays silent when the selected mode is the one already active', () => {
        setMode('phone');
        expect(seen).toEqual([]);
    });

    it('notifies on browser Back/Forward', () => {
        window.history.pushState({}, '', '/operate/ft8');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(seen).toEqual([{ from: 'phone', to: 'ft8', modeThen: 'ft8' }]);
    });

    it('stays silent on a Back/Forward that does not change the mode', () => {
        window.history.pushState({}, '', '/logbook');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(seen).toEqual([]);
        expect(router.view).toBe('logbook');
    });
});

// W-0019 slice 4 (ADR 0080): FT4 is a third operating mode beside Phone/CW and
// FT8 — its own path, its own stored-mode value, the same single mode-change
// hook, and an FT-family predicate for the gates the shared FT view relies on.
describe('FT4 as a third operating mode', () => {
    let seen: { from: string; to: string }[] = [];

    beforeEach(() => {
        seen = [];
        setModeChangeHook((from, to) => seen.push({ from, to }));
        setMode('phone');
        seen = [];
    });

    afterEach(() => {
        setModeChangeHook(null);
    });

    it('routes /operate/ft4 and builds the same path back', () => {
        expect(urlOf('operate', 'ft4', '')).toBe('/operate/ft4');
        window.history.pushState({}, '', '/operate/ft4');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(router.view).toBe('operate');
        expect(router.mode).toBe('ft4');
        expect(seen).toEqual([{ from: 'phone', to: 'ft4' }]);
    });

    it('notifies FT8 → FT4 and FT4 → FT8 as real mode changes (the FT view remounts on them)', () => {
        setMode('ft8');
        setMode('ft4');
        setMode('ft8');
        expect(seen).toEqual([
            { from: 'phone', to: 'ft8' },
            { from: 'ft8', to: 'ft4' },
            { from: 'ft4', to: 'ft8' },
        ]);
    });

    it('remembers ft4 as the last-used mode for a bare /operate', () => {
        setMode('ft4');
        window.history.pushState({}, '', '/operate');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(router.mode).toBe('ft4');
    });

    it('isFtMode names the FT-family modes the shared FT view serves', () => {
        expect(isFtMode('ft8')).toBe(true);
        expect(isFtMode('ft4')).toBe(true);
        expect(isFtMode('phone')).toBe(false);
    });
});
