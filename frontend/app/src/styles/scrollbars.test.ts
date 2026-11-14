import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// The app-wide scrollbar style (operator, 2026-11-14: narrow, no arrow buttons,
// as the app looked in Firefox). The look itself was measured in the desktop
// app's Chromium, not here: jsdom draws nothing. What this pins is the one
// thing that silently undoes it. Chromium ignores the ::-webkit-scrollbar rules
// as soon as scrollbar-width or scrollbar-color is set on an element, and draws
// its own bar with arrow buttons instead (measured in Electron 44, Chromium 152).

// Held in a variable so Vite does not rewrite the URL into an asset import.
const here = import.meta.url;
const srcDir = fileURLToPath(new URL('../', here));
const css = readFileSync(new URL('./app.css', here), 'utf8');
const withoutComments = (text: string): string => text.replace(/\/\*[\s\S]*?\*\//g, '');

function sources(dir: string): string[] {
    return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
        const path = `${dir}/${e.name}`;
        if (e.isDirectory()) return e.name === 'node_modules' ? [] : sources(path);
        return /\.(css|svelte)$/.test(e.name) ? [path] : [];
    });
}

describe('app-wide scrollbars', () => {
    const rules = withoutComments(css);

    it('styles every scrollbar: a narrow bar, a clear track, a rounded thumb', () => {
        // No selector before the pseudo-element: the rule applies to every
        // scroller, the window's own included.
        expect(rules).toMatch(
            /(^|[},])\s*::-webkit-scrollbar\s*\{[^}]*width:\s*12px;[^}]*height:\s*12px;/
        );
        expect(rules).toMatch(
            /::-webkit-scrollbar-track,\s*::-webkit-scrollbar-corner\s*\{[^}]*background:\s*transparent;/
        );
        expect(rules).toMatch(
            /(^|[},])\s*::-webkit-scrollbar-thumb\s*\{[^}]*border-radius:\s*9999px;/
        );
    });

    it('takes the thumb colour from the theme, so it follows light and dark', () => {
        const thumb = /(^|[},])\s*::-webkit-scrollbar-thumb\s*\{([^}]*)\}/.exec(rules)?.[2] ?? '';
        expect(thumb).toContain('var(--color-muted)');
    });

    it('sets no standard scrollbar property anywhere, which would bring the arrows back', () => {
        const offenders = sources(srcDir.replace(/\/$/, '')).filter((path) =>
            /scrollbar-(width|color)\s*:/.test(withoutComments(readFileSync(path, 'utf8')))
        );
        expect(offenders).toEqual([]);
    });
});
