// Client-side router — History-API real paths (ADR 0044 sub-decision). A tiny
// hand-rolled router, no dependency. Operate has two sub-routes for its modes
// (/operate/phone, /operate/ft8) so they deep-link; a bare /operate normalises to
// the last-used mode. Deep links + refresh work because both Vite's dev server
// and the daemon's spaHandler index-fall-back unknown paths to index.html.

export type View = 'dashboard' | 'operate' | 'logbook' | 'config';
export type OpMode = 'phone' | 'ft8';

const MODE_KEY = 'sm-op-mode';

function storedMode(): OpMode {
    return localStorage.getItem(MODE_KEY) === 'ft8' ? 'ft8' : 'phone';
}

interface Loc {
    view: View;
    mode: OpMode;
}

function parse(path: string, fallbackMode: OpMode): Loc {
    switch (path) {
        case '/operate/ft8':
            return { view: 'operate', mode: 'ft8' };
        case '/operate/phone':
        case '/operate':
            return { view: 'operate', mode: path === '/operate' ? fallbackMode : 'phone' };
        case '/logbook':
            return { view: 'logbook', mode: fallbackMode };
        case '/config':
            return { view: 'config', mode: fallbackMode };
        default:
            return { view: 'dashboard', mode: fallbackMode };
    }
}

function pathFor(view: View, mode: OpMode): string {
    switch (view) {
        case 'operate':
            return `/operate/${mode}`;
        case 'logbook':
            return '/logbook';
        case 'config':
            return '/config';
        default:
            return '/';
    }
}

// The daemon serves this SPA UNDER a base path (/app/ — Vite's BASE_URL) while the
// shipping logging SPA still owns '/'. Real-path routing must therefore live under
// that base: strip it before parsing, re-add it before writing. Without this the
// initial normalise turned '/app/…' into a bare '/operate/ft8' etc. and, for the
// default view, into '/' — which is a DIFFERENT SPA, so the URL reverted off /app/.
// BASE is '' when served at the root (no base), so this is base-agnostic.
const BASE = import.meta.env.BASE_URL.replace(/\/+$/, '');

// The path WITHIN the app's base — what parse() understands. Pure + base-explicit
// so it's testable without stubbing import.meta.env: '/app/operate/ft8' →
// '/operate/ft8'; '/app/' or '/app' → '/'.
export function subPathOf(pathname: string, base: string): string {
    const sub = pathname.startsWith(base) ? pathname.slice(base.length) : pathname;
    return sub === '' ? '/' : sub;
}

// The full URL (base + route) to push/replace into the address bar. Pure + base-explicit.
export function urlOf(view: View, mode: OpMode, base: string): string {
    return base + pathFor(view, mode);
}

const subPath = (): string => subPathOf(window.location.pathname, BASE);
const urlFor = (view: View, mode: OpMode): string => urlOf(view, mode, BASE);

const initial = parse(subPath(), storedMode());
export const router = $state<Loc>(initial);
localStorage.setItem(MODE_KEY, router.mode); // remember a deep-linked mode

// Normalise the URL (e.g. a bare /operate, or /app → /app/) to the canonical path
// without adding a history entry.
{
    const canonical = urlFor(router.view, router.mode);
    if (window.location.pathname !== canonical) {
        window.history.replaceState({}, '', canonical);
    }
}

export function navigate(view: View): void {
    router.view = view;
    const path = urlFor(view, router.mode);
    if (window.location.pathname !== path) window.history.pushState({}, '', path);
}

export function setMode(mode: OpMode): void {
    router.view = 'operate';
    router.mode = mode;
    localStorage.setItem(MODE_KEY, mode);
    const path = urlFor('operate', mode);
    if (window.location.pathname !== path) window.history.pushState({}, '', path);
}

// Sync on browser back/forward.
window.addEventListener('popstate', () => {
    const loc = parse(subPath(), router.mode);
    router.view = loc.view;
    router.mode = loc.mode;
});
