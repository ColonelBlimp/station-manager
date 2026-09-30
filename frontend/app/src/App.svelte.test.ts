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
import { _setDraftStoreForTests, memoryDraftStore } from './lib/drafts/draftStore';
import {
    _resetSavedDraftsForTests,
    rememberPreservedForAnnouncement,
} from './lib/drafts/savedDrafts.svelte';
import { sampleRecord } from './lib/drafts/savedDraft.fixture';

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

// Saved QSOs are reachable on EVERY page (ADR 0086) — the full-window Map tab
// included, which has no header: the control sits in the map's own toolbar.
describe('the Saved QSOs control on the Map route', () => {
    beforeEach(() => {
        _resetSetupForTests();
        _resetArchivesForTests();
        _resetSavedDraftsForTests();
    });
    afterEach(() => {
        _setDraftStoreForTests(null);
        navigate('operate');
    });

    it('shows the saved-QSO count in the map toolbar', async () => {
        const mem = memoryDraftStore();
        await mem.put(sampleRecord());
        _setDraftStoreForTests(mem);
        setup.status = 'complete';
        navigate('map');
        render(App);
        expect(
            await screen.findByRole('button', { name: 'Saved QSOs (1)' }, { timeout: 3000 })
        ).toBeInTheDocument();
    });

    // Review 2026-09-30: the Map branch mounted no toast renderer, so the
    // announcement was consumed and never shown (Copy and a failed Discard
    // were silent too). Checked in the DOM, not in toast state.
    it('shows the one-time announcement on the map', async () => {
        const mem = memoryDraftStore();
        const record = sampleRecord();
        await mem.put(record);
        _setDraftStoreForTests(mem);
        sessionStorage.clear();
        rememberPreservedForAnnouncement(record);
        setup.status = 'complete';
        navigate('map');
        render(App);
        await screen.findByRole('button', { name: 'Saved QSOs (1)' }, { timeout: 3000 });
        expect(
            await screen.findByText(
                'Unlogged QSO saved from ‘Home’ — not logged. It is under Saved QSOs.'
            )
        ).toBeInTheDocument();
    });
});
