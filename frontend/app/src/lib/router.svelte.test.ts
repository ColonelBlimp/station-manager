// The router is base-AGNOSTIC: it strips Vite's BASE_URL before parsing and re-adds
// it before writing the URL, so it routes correctly under ANY base. It now serves at
// the canonical root (base '' — the root case below); the '/app' cases pin that same
// round-trip under a NON-EMPTY base — the FORMER '/app/' transition mount, where a
// missing strip reverted '/app/…' to '/' (a different SPA) and the URL jumped off.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
    subPathOf,
    urlOf,
    router,
    navigate,
    setMode,
    setModeChangeHook,
    isFtMode,
    takeLogbookMissingFrom,
} from './router.svelte';

describe('router base-path handling', () => {
    it('strips a non-empty base before parsing (the former /app mount)', () => {
        expect(subPathOf('/app/operate/ft8', '/app')).toBe('/operate/ft8');
        expect(subPathOf('/app/logbook', '/app')).toBe('/logbook');
        expect(subPathOf('/app/events', '/app')).toBe('/events');
        expect(subPathOf('/app/', '/app')).toBe('/'); // default view
        expect(subPathOf('/app', '/app')).toBe('/'); // no trailing slash
    });

    it('re-adds a non-empty base when building the URL (the former /app mount)', () => {
        expect(urlOf('operate', 'ft8', '/app')).toBe('/app/operate/ft8');
        expect(urlOf('operate', 'phone', '/app')).toBe('/app/operate/phone');
        expect(urlOf('logbook', 'phone', '/app')).toBe('/app/logbook');
        expect(urlOf('events', 'phone', '/app')).toBe('/app/events'); // Station Events (W-0020)
        expect(urlOf('config', 'phone', '/app')).toBe('/app/config');
        expect(urlOf('map', 'phone', '/app')).toBe('/app/map'); // contacts-map tab
    });

    it('is base-agnostic — works unchanged when served at the root', () => {
        expect(subPathOf('/operate/ft8', '')).toBe('/operate/ft8');
        expect(subPathOf('/', '')).toBe('/');
        expect(urlOf('operate', 'ft8', '')).toBe('/operate/ft8');
    });
});

// The Dashboard is retired (ADR 0044 amendment 2026-09-14): every tile it was to
// carry already lives in the header, the rail, the Logbook or Settings, and its
// one real job — a landing view that starts nothing — Phone/CW does as well. The
// bare root now lands on the last-used Operate mode, and so does any path the
// router does not know; a configurable landing view is a later slice.
describe('landing without a Dashboard', () => {
    afterEach(() => {
        setModeChangeHook(null);
    });

    it('the bare root lands on the last-used Operate mode', () => {
        setMode('ft4');
        window.history.pushState({}, '', '/');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(router.view).toBe('operate');
        expect(router.mode).toBe('ft4');
    });

    it('an unknown path lands on Operate too, not on a blank view', () => {
        setMode('phone');
        window.history.pushState({}, '', '/nothing-here');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(router.view).toBe('operate');
        expect(router.mode).toBe('phone');
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

/*
    The Settings → Forwarding card's failed count links to the logbook's
    "not on X" view (W-0010 outcome 9, slice 4). The router carries the
    destination as `/logbook?missing_from=<name>`: a ONE-SHOT handoff the
    logbook takes at mount and the router then canonicalises away, because the
    logbook's destination picker owns that state and never writes the URL — a
    query that lingered would re-apply a stale filter on the next refresh.
*/
describe('logbook missing-from handoff', () => {
    afterEach(() => {
        takeLogbookMissingFrom();
        navigate('operate');
    });

    it('urlOf writes the query for a logbook destination and nothing else', () => {
        expect(urlOf('logbook', 'phone', '', 'qrz')).toBe('/logbook?missing_from=qrz');
        expect(urlOf('logbook', 'phone', '', ' qrz ')).toBe('/logbook?missing_from=%20qrz%20');
        expect(urlOf('logbook', 'phone', '')).toBe('/logbook');
        expect(urlOf('operate', 'phone', '', 'qrz')).toBe('/operate/phone');
    });

    it('navigate carries the destination into the URL and the logbook takes it ONCE', () => {
        navigate('logbook', { missingFrom: 'qrz' });
        expect(router.view).toBe('logbook');
        expect(window.location.pathname + window.location.search).toBe('/logbook?missing_from=qrz');
        expect(takeLogbookMissingFrom()).toBe('qrz');
        // Taken: a second read is empty and the URL is canonical again.
        expect(takeLogbookMissingFrom()).toBeUndefined();
        expect(window.location.pathname + window.location.search).toBe('/logbook');
    });

    it('a deep link with the query lands on the logbook with the destination pending', () => {
        window.history.pushState({}, '', '/logbook?missing_from=clublog');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(router.view).toBe('logbook');
        expect(takeLogbookMissingFrom()).toBe('clublog');
    });

    it('a plain /logbook has nothing pending', () => {
        navigate('logbook');
        expect(takeLogbookMissingFrom()).toBeUndefined();
    });
});
