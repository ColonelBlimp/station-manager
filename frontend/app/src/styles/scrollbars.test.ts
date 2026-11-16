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

// The two pages the app does not style (operator, 2026-11-16: "All scrollbars
// should match"). The manual is one self-contained file and the desktop app's
// waiting page loads nothing, so neither can share app.css: each carries its
// own copy of the rules, with the theme's muted colour written out. This holds
// the copies to the original. The manual has a light theme only; the waiting
// page follows the system's light or dark setting.
describe('the manual and the waiting page carry the same scrollbars', () => {
    const repo = fileURLToPath(new URL('../../../../', here));
    // Without comments or whitespace, and without the optional last semicolon of a
    // rule, so each page may lay its copy out in its own way.
    const squash = (text: string): string =>
        withoutComments(text).replace(/\s+/g, '').replace(/;\}/g, '}');
    const pages = {
        manual: squash(readFileSync(`${repo}manual/layouts/index.html`, 'utf8')),
        waiting: squash(readFileSync(`${repo}desktop/src/waiting.mjs`, 'utf8')),
    };

    // The theme's muted colour, as app.css states it: light first, then dark.
    const muted = [...withoutComments(css).matchAll(/--color-muted:\s*([^;]+);/g)].map((m) =>
        m[1].replace(/\s+/g, '')
    );
    const [light, dark] = muted;
    const thumbs = (colour: string): string =>
        '::-webkit-scrollbar-thumb{border:3pxsolidtransparent;border-radius:9999px;' +
        `background-color:color-mix(inoklab,${colour}50%,transparent);background-clip:content-box}` +
        `::-webkit-scrollbar-thumb:hover{background-color:color-mix(inoklab,${colour}75%,transparent)}` +
        `::-webkit-scrollbar-thumb:active{background-color:${colour}}`;

    it('reads a light and a dark muted colour from the theme', () => {
        expect(muted).toHaveLength(2);
        expect(light).not.toEqual(dark);
    });

    it.each(Object.entries(pages))('%s: the same bar, track and thumb shape', (_name, page) => {
        expect(page).toContain('::-webkit-scrollbar{width:12px;height:12px}');
        expect(page).toContain(
            '::-webkit-scrollbar-track,::-webkit-scrollbar-corner{background:transparent}'
        );
        expect(page).toContain(thumbs(light));
    });

    it('waiting: the dark thumb under the system dark setting', () => {
        expect(pages.waiting).toMatch(
            new RegExp(
                '@media\\(prefers-color-scheme:dark\\)\\{[^@]*' +
                    thumbs(dark).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
            )
        );
    });

    it.each(Object.entries(pages))('%s: no standard scrollbar property', (_name, page) => {
        expect(page).not.toMatch(/scrollbar-(width|color):/);
    });
});
