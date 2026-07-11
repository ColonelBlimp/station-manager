import '@testing-library/jest-dom/vitest';

// jsdom has no Web Animations API; Svelte 5 transitions (transition:fade)
// call element.animate. Minimal stub with the members Svelte touches —
// enough for components using transitions to mount in tests.
if (typeof Element !== 'undefined' && !Element.prototype.animate) {
    Element.prototype.animate = function animate(): Animation {
        return {
            cancel() {},
            finish() {},
            onfinish: null,
            oncancel: null,
            finished: Promise.resolve(),
        } as unknown as Animation;
    };
}

// jsdom has no matchMedia; the theme init (lib/ui/state) reads it at import, so
// any test that mounts a chrome component (UtilRail, header) needs this. Reports
// light (matches:false) — tests that care about theme set data-theme explicitly.
if (typeof window !== 'undefined' && !window.matchMedia) {
    window.matchMedia = (query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener() {},
        removeEventListener() {},
        addListener() {},
        removeListener() {},
        dispatchEvent: () => false,
    });
}
