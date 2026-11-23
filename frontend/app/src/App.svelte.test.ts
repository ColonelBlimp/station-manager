// The browser-tab title is owned by App (W-0004 AC2). On the first-run surface the
// view router still named a view (then "dashboard"), so the tab read "Dashboard · Station Manager"
// over the welcome card (alpha.2 dogfood Finding #8, W-0012). The title must follow
// the same gate that chooses the welcome card: setup needed, or just completed.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import App from './App.svelte';
import { setup, _resetSetupForTests } from './lib/setup.svelte';
import { recovery, _resetRecoveryForTests } from './lib/recovery.svelte';
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

// W-0029 part 4, E1, E2 and E9: the recovery gate sits where the workspace
// would be. Until the session's state has been read nothing of the workspace is
// shown; a question or an unreadable state replaces it; setup comes first; and
// the full-window map is never asked.
describe('the recovery gate replaces the workspace', () => {
    const workspace = (): Element | null => document.querySelector('.content-wrap');
    const asked = (): HTMLElement | null =>
        screen.queryByRole('heading', { name: 'The last session is still open' });
    const unread = (): HTMLElement | null =>
        screen.queryByRole('heading', { name: 'The session could not be read' });
    const session = {
        id: '01a1f83c-da20-751f-80b7-ec9a3f1f93f3',
        openedAt: '2026-11-20T09:15:00Z',
        contactCount: 3,
    };

    beforeEach(() => {
        _resetSetupForTests();
        _resetArchivesForTests();
        _resetRecoveryForTests();
        setup.status = 'complete';
        navigate('operate');
        document.title = '';
    });

    it('before the state has been read: neither the workspace nor a question', () => {
        render(App);
        flushSync();
        expect(workspace()).toBeNull();
        expect(asked()).toBeNull();
        expect(unread()).toBeNull();
    });

    it('recovery pending: the question, and nothing of the workspace', () => {
        recovery.status = 'question';
        recovery.session = session;
        render(App);
        flushSync();
        expect(asked()).not.toBeNull();
        expect(workspace()).toBeNull();
        expect(screen.queryByRole('navigation')).toBeNull();
        expect(document.title).toBe('Session · Station Manager');
    });

    it('the state could not be read: the page says so, and shows no workspace', () => {
        recovery.status = 'unreadable';
        recovery.error = 'Cannot reach the daemon.';
        render(App);
        flushSync();
        expect(unread()).not.toBeNull();
        expect(workspace()).toBeNull();
    });

    it('any other state: the workspace, and no question', () => {
        recovery.status = 'clear';
        render(App);
        flushSync();
        expect(workspace()).not.toBeNull();
        expect(asked()).toBeNull();
        expect(document.title).toBe('Operate · Station Manager');
    });

    it('setup comes first: the first-run card, whatever the session read said', () => {
        setup.status = 'needed';
        recovery.status = 'unreadable';
        render(App);
        flushSync();
        expect(screen.getByText(/Welcome to Station Manager/)).toBeInTheDocument();
        expect(unread()).toBeNull();
    });

    it('the full-window map is never asked', () => {
        recovery.status = 'question';
        recovery.session = session;
        navigate('map');
        render(App);
        flushSync();
        expect(asked()).toBeNull();
        navigate('operate');
    });
});
