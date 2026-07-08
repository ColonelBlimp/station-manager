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
