// The browser-tab title is owned by App (W-0004 AC2). On the first-run surface the
// view router still says "dashboard", so the tab read "Dashboard · Station Manager"
// over the welcome card (alpha.2 dogfood Finding #8, W-0012). The title must follow
// the same gate that chooses the welcome card: setup needed, or just completed.
import { describe, it, expect, beforeEach } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import App from './App.svelte';
import { setup, _resetSetupForTests } from './lib/setup.svelte';

describe('App tab title on the first-run surface', () => {
    beforeEach(() => {
        _resetSetupForTests();
        document.title = '';
    });

    it('reads Welcome while setup is needed', () => {
        setup.status = 'needed';
        render(App);
        flushSync();
        expect(document.title).toBe('Welcome · Station Manager');
    });

    it('still reads Welcome on the "Setup complete" surface', () => {
        setup.status = 'complete';
        setup.justCompleted = true;
        render(App);
        flushSync();
        expect(document.title).toBe('Welcome · Station Manager');
    });
});
