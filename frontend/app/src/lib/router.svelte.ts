// Client-side router — History-API real paths (ADR 0044 sub-decision). A tiny
// hand-rolled router, no dependency. Operate has three sub-routes for its modes
// (/operate/phone, /operate/ft8, /operate/ft4 — ADR 0080) so they deep-link; a bare
// /operate normalises to the last-used mode, and so do the bare root and any unknown
// path (the Dashboard is retired — ADR 0044 amendment 2026-09-14; a configurable
// landing view is a later slice). Deep links + refresh work because both Vite's dev server
// and the daemon's spaHandler index-fall-back unknown paths to index.html.

export type View = 'operate' | 'logbook' | 'events' | 'config' | 'map';
export type OpMode = 'phone' | 'ft8' | 'ft4';

/** The FT-family modes the shared FT view serves (ADR 0080): FT8 and FT4 differ
 *  only in the daemon profile the view claims, so every gate that means "the FT
 *  workspace is up" (queue drawer, util rail, pile-up) reads this, not 'ft8'. */
export function isFtMode(mode: OpMode): boolean {
    return mode === 'ft8' || mode === 'ft4';
}

const MODE_KEY = 'sm-op-mode';

import { storageGet, storageSet } from './utils/storage';

function storedMode(): OpMode {
    const m = storageGet(MODE_KEY);
    return m === 'ft8' || m === 'ft4' ? m : 'phone';
}

interface Loc {
    view: View;
    mode: OpMode;
}

function parse(path: string, fallbackMode: OpMode): Loc {
    switch (path) {
        case '/operate/ft8':
            return { view: 'operate', mode: 'ft8' };
        case '/operate/ft4':
            return { view: 'operate', mode: 'ft4' };
        case '/operate/phone':
        case '/operate':
            return { view: 'operate', mode: path === '/operate' ? fallbackMode : 'phone' };
        case '/logbook':
            return { view: 'logbook', mode: fallbackMode };
        case '/events':
            return { view: 'events', mode: fallbackMode };
        case '/config':
            return { view: 'config', mode: fallbackMode };
        case '/map':
            return { view: 'map', mode: fallbackMode };
        default:
            return { view: 'operate', mode: fallbackMode };
    }
}

function pathFor(view: View, mode: OpMode): string {
    switch (view) {
        case 'operate':
            return `/operate/${mode}`;
        case 'logbook':
            return '/logbook';
        case 'events':
            return '/events';
        case 'config':
            return '/config';
        case 'map':
            return '/map';
    }
}

// This SPA is base-AGNOSTIC: it routes UNDER Vite's BASE_URL, stripping the base
// before parsing and re-adding it before writing. It now serves at the CANONICAL
// ROOT (W-0003), so BASE is '' and every path is already canonical. The base
// stripping stays because the logic must survive any base: under the FORMER '/app/'
// transition mount a missing strip turned '/app/…' into a bare '/operate/ft8' and,
// for the default view, into '/' — a different SPA — so the URL jumped off /app/.
const BASE = import.meta.env.BASE_URL.replace(/\/+$/, '');

// The path WITHIN the app's base — what parse() understands. Pure + base-explicit
// so it's testable without stubbing import.meta.env: '/app/operate/ft8' →
// '/operate/ft8'; '/app/' or '/app' → '/'.
export function subPathOf(pathname: string, base: string): string {
    const sub = pathname.startsWith(base) ? pathname.slice(base.length) : pathname;
    return sub === '' ? '/' : sub;
}

// The full URL (base + route) to push/replace into the address bar. Pure + base-explicit.
// `missingFrom` names a forwarder for the logbook's "not on X" view (below); it is
// written only for the logbook route.
export function urlOf(view: View, mode: OpMode, base: string, missingFrom?: string): string {
    const url = base + pathFor(view, mode);
    return view === 'logbook' && missingFrom
        ? `${url}?missing_from=${encodeURIComponent(missingFrom)}`
        : url;
}

const subPath = (): string => subPathOf(window.location.pathname, BASE);
const urlFor = (view: View, mode: OpMode, missingFrom?: string): string =>
    urlOf(view, mode, BASE, missingFrom);

/*
    Logbook "missing from" handoff (W-0010 outcome 9, slice 4). Settings →
    Forwarding's failed count links to the logbook already filtered to the QSOs
    not on that destination: `/logbook?missing_from=<name>`. The query is a
    ONE-SHOT handoff, not routed state — the logbook's destination picker owns
    that state and never writes the URL, so a query that lingered would re-apply
    a stale filter on the next refresh. The logbook takes it at mount
    (takeLogbookMissingFrom), and the URL is canonicalised back to /logbook.
*/
let pendingMissingFrom: string | undefined;

function missingFromOf(search: string): string | undefined {
    const v = new URLSearchParams(search).get('missing_from');
    return v ? v : undefined;
}

/** The pending "not on X" destination, if a logbook link carried one. Taking it
 *  clears it and drops the query from the address bar. */
export function takeLogbookMissingFrom(): string | undefined {
    const v = pendingMissingFrom;
    pendingMissingFrom = undefined;
    if (v !== undefined && router.view === 'logbook' && window.location.search !== '') {
        window.history.replaceState({}, '', urlFor('logbook', router.mode));
    }
    return v;
}

/** The href for the logbook's "not on `name`" view, for a real link. */
export function logbookMissingFromUrl(name: string): string {
    return urlFor('logbook', router.mode, name);
}

const initial = parse(subPath(), storedMode());
export const router = $state<Loc>(initial);
storageSet(MODE_KEY, router.mode); // remember a deep-linked mode
if (initial.view === 'logbook') pendingMissingFrom = missingFromOf(window.location.search);

// Normalise the URL (e.g. a bare /operate → /operate/phone) to the canonical path
// without adding a history entry.
{
    const canonical = urlFor(router.view, router.mode);
    if (window.location.pathname !== canonical) {
        window.history.replaceState({}, '', canonical);
    }
}

// A view may refuse to be left. Settings uses this to ask before its unsaved
// edits are discarded (lib/config/unsaved.ts) — the discard happens on RETURN,
// when the remount reloads over the draft, so leaving is the last moment at
// which the operator can still act on it.
//
// ONE slot, not a registry of guards: there is exactly one guarded view, and a
// framework for a single caller is the shape lessons-for-v2 warns against.
type LeaveGuard = () => boolean;
let leaveGuard: LeaveGuard | null = null;

export function setLeaveGuard(g: LeaveGuard | null): void {
    leaveGuard = g;
}

/*
    Operating-mode change notification. A Phone/CW ↔ FT8 switch is decided HERE,
    so this is the only place that sees every one of them — the sidebar buttons
    and browser Back/Forward alike. lib/operate/modeRestore listens, and returns
    the rig to where that mode left it.

    ONE slot again, for the same reason as the leave guard above: one listener
    exists, and a registry for a single caller is the shape lessons-for-v2 warns
    against. Fired only when the mode actually CHANGES, and only after router
    state has moved — the listener reads it.
*/
type ModeChangeHook = (from: OpMode, to: OpMode) => void;
let modeChangeHook: ModeChangeHook | null = null;

export function setModeChangeHook(h: ModeChangeHook | null): void {
    modeChangeHook = h;
}

function modeChanged(from: OpMode, to: OpMode): void {
    if (from !== to) modeChangeHook?.(from, to);
}

// Asked only when config is genuinely being LEFT. Re-navigating to config (the
// Settings tab strip does not route, but the sidebar item is clickable while
// already there) is not leaving, and must not prompt.
function mayLeave(to: View): boolean {
    if (router.view !== 'config' || to === 'config') return true;
    return leaveGuard === null || leaveGuard();
}

export function navigate(view: View, opts?: { missingFrom?: string }): void {
    if (!mayLeave(view)) return;
    router.view = view;
    pendingMissingFrom = view === 'logbook' ? opts?.missingFrom : undefined;
    const url = urlFor(view, router.mode, pendingMissingFrom);
    if (window.location.pathname + window.location.search !== url) {
        window.history.pushState({}, '', url);
    }
}

export function setMode(mode: OpMode): void {
    // Also an exit from Settings: OperateNav lives in the always-visible
    // sidebar, so its mode buttons leave config without going through
    // navigate(). Guarding only navigate() would leave this door open.
    if (!mayLeave('operate')) return;
    const from = router.mode;
    router.view = 'operate';
    router.mode = mode;
    storageSet(MODE_KEY, mode);
    const path = urlFor('operate', mode);
    if (window.location.pathname !== path) window.history.pushState({}, '', path);
    modeChanged(from, mode);
}

// Sync on browser back/forward.
window.addEventListener('popstate', () => {
    const loc = parse(subPath(), router.mode);
    if (!mayLeave(loc.view)) {
        // popstate fires AFTER the address bar has already moved, so refusing
        // is not enough — the URL has to be put back, or the view stays on
        // Settings underneath the previous entry's path and a reload would
        // then land somewhere the operator never chose.
        window.history.pushState({}, '', urlFor('config', router.mode));
        return;
    }
    const from = router.mode;
    router.view = loc.view;
    router.mode = loc.mode;
    pendingMissingFrom = loc.view === 'logbook' ? missingFromOf(window.location.search) : undefined;
    modeChanged(from, loc.mode);
});
