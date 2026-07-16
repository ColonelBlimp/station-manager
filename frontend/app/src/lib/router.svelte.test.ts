// The daemon serves this SPA under a base path (/app/) while the shipping logging
// SPA owns '/'. The router must strip that base before parsing and re-add it before
// writing the URL — otherwise the initial normalise reverts '/app/…' to '/', which
// is a DIFFERENT SPA. These pin the base round-trip (the bug: URL jumped off /app/).

import { describe, it, expect } from 'vitest';
import { subPathOf, urlOf } from './router.svelte';

describe('router base-path handling', () => {
    it('strips the /app base before parsing', () => {
        expect(subPathOf('/app/operate/ft8', '/app')).toBe('/operate/ft8');
        expect(subPathOf('/app/logbook', '/app')).toBe('/logbook');
        expect(subPathOf('/app/', '/app')).toBe('/'); // default view
        expect(subPathOf('/app', '/app')).toBe('/'); // no trailing slash
    });

    it('re-adds the /app base when building the address-bar URL', () => {
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
