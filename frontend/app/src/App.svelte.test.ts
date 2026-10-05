// The browser-tab title is owned by App (W-0004 AC2). On the first-run surface the
// view router still named a view (then "dashboard"), so the tab read "Dashboard · Station Manager"
// over the welcome card (alpha.2 dogfood Finding #8, W-0012). The title must follow
// the same gate that chooses the welcome card: setup needed, or just completed.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import App from './App.svelte';
import { setup, _resetSetupForTests } from './lib/setup.svelte';
import { navigate } from './lib/router.svelte';
import { archivesState, _resetArchivesForTests } from './lib/config/archives.svelte';
import { screen } from '@testing-library/svelte';
import { draft, resetDraft } from './lib/operate/qso.svelte';
import { installBrowserStorageSpies } from './lib/utils/browserStorageSpies.fixture';

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

// The unresolved-switch gate (ADR 0071) must cover EVERY route branch — the
// full-window Map tab has no shell, and a Map tab whose reconnect identity is
// unreadable is exactly a tab that must not keep operating silently.
describe('ArchiveSwitchGate covers the Map branch', () => {
    beforeEach(() => {
        _resetSetupForTests();
        _resetArchivesForTests();
    });

    it('renders the gate over the map route when a switch is unresolved', () => {
        setup.status = 'complete';
        navigate('map');
        archivesState.switchUnresolved = true;
        archivesState.switchDetail =
            'The connection came back but the daemon’s identity could not be read.';
        render(App);
        flushSync();
        expect(screen.getByRole('alertdialog')).toHaveTextContent('Archive binding unproven');
        navigate('operate');
    });

    it('makes everything under the gate inert while it shows', () => {
        setup.status = 'complete';
        archivesState.switchUnresolved = true;
        const { container } = render(App);
        flushSync();
        // Svelte sets `inert` as the element property (jsdom does not reflect it
        // to an attribute), so read the property.
        const inertDivs = () =>
            [...container.querySelectorAll('div')].filter(
                (d) => d.inert || d.hasAttribute('inert')
            );
        const cover = inertDivs()[0];
        expect(cover).toBeDefined();
        expect(cover.contains(screen.getByRole('alertdialog'))).toBe(false);
        archivesState.switchUnresolved = false;
        flushSync();
        expect(inertDivs()).toHaveLength(0);
    });
});

// ADR 0087: opening Settings clears nothing — only Activate's own prompt may
// discard this window's entry — and the saved-QSO recovery is gone, so no page
// offers an Unlogged QSOs control (the Map toolbar carried one).
describe('ADR 0087 in the shell', () => {
    beforeEach(() => {
        _resetSetupForTests();
        _resetArchivesForTests();
        resetDraft();
    });
    afterEach(() => {
        resetDraft();
        navigate('operate');
    });

    it('C2 Operate → Settings → Operate keeps a Phone / CW entry', () => {
        setup.status = 'complete';
        navigate('operate');
        render(App);
        flushSync();
        draft.callsign = '7Q7CT';
        draft.comment = 'navigation';
        flushSync();
        navigate('config');
        flushSync();
        navigate('operate');
        flushSync();
        expect(draft.callsign).toBe('7Q7CT');
        expect(draft.comment).toBe('navigation');
    });

    it('H1/O1 the Map page offers no Unlogged QSOs control and never opens browser storage', async () => {
        const spies = installBrowserStorageSpies();
        try {
            setup.status = 'complete';
            navigate('map');
            render(App);
            flushSync();
            await new Promise((r) => setTimeout(r, 50));
            flushSync();
            expect(screen.queryByRole('button', { name: /Unlogged QSOs/ })).toBeNull();
            expect(spies.opens).toEqual([]);
            expect(spies.deletes).toEqual([]);
        } finally {
            spies.restore();
        }
    });
});
