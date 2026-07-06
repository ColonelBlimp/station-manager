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

function initialTheme(): Theme {
    const s = localStorage.getItem(THEME_KEY);
    if (s === 'light' || s === 'dark') return s;
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function initialNav(): NavMode {
    return localStorage.getItem(NAV_KEY) === 'narrow' ? 'narrow' : 'full';
}

function initialUtil(): NavMode {
    return localStorage.getItem(UTIL_KEY) === 'narrow' ? 'narrow' : 'full';
}

export const ui = $state({
    theme: initialTheme(),
    navMode: initialNav(),
    utilMode: initialUtil(),
});

export function toggleTheme(): void {
    ui.theme = ui.theme === 'dark' ? 'light' : 'dark';
}

export function toggleNav(): void {
    ui.navMode = ui.navMode === 'narrow' ? 'full' : 'narrow';
}

export function toggleUtil(): void {
    ui.utilMode = ui.utilMode === 'narrow' ? 'full' : 'narrow';
}
