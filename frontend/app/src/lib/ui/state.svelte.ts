// Shell UI state — theme, left-nav collapse, and the current view. Universal
// reactive state (Svelte 5 runes in a .svelte.ts module) shared across the shell
// components. Persistence + reflection onto <html data-theme / data-nav> is done
// by the root component's $effect (see App.svelte); the initial attributes are
// set pre-mount by the inline script in index.html to avoid a flash.

export type Theme = 'light' | 'dark';
export type NavMode = 'full' | 'narrow';

export const THEME_KEY = 'sm-theme';
export const NAV_KEY = 'sm-nav';
export const UTIL_KEY = 'sm-util';

import { storageGet } from '../utils/storage';

function initialTheme(): Theme {
    const s = storageGet(THEME_KEY);
    if (s === 'light' || s === 'dark') return s;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function initialNav(): NavMode {
    return storageGet(NAV_KEY) === 'narrow' ? 'narrow' : 'full';
}

function initialUtil(): NavMode {
    return storageGet(UTIL_KEY) === 'narrow' ? 'narrow' : 'full';
}

export const ui = $state({
    theme: initialTheme(),
    navMode: initialNav(),
    utilMode: initialUtil(),
    // The notification-history slide-over. Not persisted — it's an ambient
    // glance surface, opened on demand and re-fetched fresh each time (W-0001).
    notificationsOpen: false,
});

export function toggleNotifications(): void {
    ui.notificationsOpen = !ui.notificationsOpen;
}

export function closeNotifications(): void {
    ui.notificationsOpen = false;
}

export function toggleTheme(): void {
    ui.theme = ui.theme === 'dark' ? 'light' : 'dark';
}

// Cross-tab theme sync. The contacts map opens in its own browser tab, and a
// toggle only stamps <html data-theme> in the tab that toggled — the map tab
// would keep its load-time theme forever. `storage` fires in every OTHER tab
// when the toggling tab persists the preference; mirroring it into ui.theme
// lets App.svelte's reflection $effect restamp this tab. No echo loop: the
// receiving tab's setItem writes an identical value, which fires no event.
window.addEventListener('storage', (e) => {
    if (e.key === THEME_KEY && (e.newValue === 'light' || e.newValue === 'dark')) {
        ui.theme = e.newValue;
    }
});

export function toggleNav(): void {
    ui.navMode = ui.navMode === 'narrow' ? 'full' : 'narrow';
}

export function toggleUtil(): void {
    ui.utilMode = ui.utilMode === 'narrow' ? 'full' : 'narrow';
}

// --- Responsive auto-collapse ------------------------------------------------
// Below these widths a rail is forced narrow regardless of the saved preference,
// to keep the logging card fitting as the window shrinks. The util rail (secondary)
// collapses first, then the left nav. The *preference* (ui.navMode/utilMode) is
// never touched — so widening the window restores whatever the operator had set.
// effective = preference-narrow OR forced-by-width.
// (Keep these thresholds in sync with the pre-paint script in index.html.)
const navMq = window.matchMedia('(max-width: 61rem)');
const utilMq = window.matchMedia('(max-width: 72rem)');

const forced = $state({ nav: navMq.matches, util: utilMq.matches });
navMq.addEventListener('change', (e) => (forced.nav = e.matches));
utilMq.addEventListener('change', (e) => (forced.util = e.matches));

export function effectiveNav(): NavMode {
    return ui.navMode === 'narrow' || forced.nav ? 'narrow' : 'full';
}

export function effectiveUtil(): NavMode {
    return ui.utilMode === 'narrow' || forced.util ? 'narrow' : 'full';
}
