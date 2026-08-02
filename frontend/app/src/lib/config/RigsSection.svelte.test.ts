import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import RigsSection from './RigsSection.svelte';
import { rigsState } from './rigs.svelte';

// Reset the rigsState singleton between cases (RigsSection.onMount → load()).
afterEach(() => {
    vi.restoreAllMocks();
    rigsState.rigs = [];
    rigsState.defaultRigId = 0;
    rigsState.selectedId = null;
    rigsState.loading = false;
    rigsState.loaded = false;
    rigsState.error = '';
    rigsState.catalogue = {};
    rigsState.drafts = {};
    rigsState.baselines = {};
    rigsState.saving = false;
    rigsState.settingDefault = false;
    rigsState.serialPorts = [];
    rigsState.audioAvailable = false;
    rigsState.capture = [];
    rigsState.playback = [];
});

function mockCluster(rigsBody: object): void {
    const resp = (body: unknown) =>
        Promise.resolve(
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
    vi.stubGlobal(
        'fetch',
        vi.fn((url: string, init?: RequestInit) => {
            if ((init?.method ?? 'GET') === 'PUT') return resp({});
            if (url.includes('/v1/hardware'))
                return resp({ serial_ports: [], audio: { available: false } });
            return resp(rigsBody);
        })
    );
}

describe('RigsSection advanced editors', () => {
    // codex 55d85876 P1: the advanced editors take a one-time snapshot on mount,
    // so the {#key} must remount them when the draft is REPLACED. Keying on
    // id:model (constant across Cancel) left the editors writing their stale
    // snapshot back into the fresh draft, so Cancel didn't revert. This pins the
    // fix (key on the draft object).
    it('Cancel reverts a mode-mapping edit and clears dirty', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [
                {
                    id: 'ftdx10',
                    name: 'FTdx10',
                    rig_modes: ['DATA-U'],
                    mode_mappings: { 'DATA-U': { mode: 'FT8' } },
                },
            ],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();

        // Edit the DATA-U mode literal away from its rigdef default (FT8 → RTTY).
        const modeInput = screen.getByPlaceholderText('MODE');
        await fireEvent.input(modeInput, { target: { value: 'RTTY' } });
        flushSync();
        await vi.waitFor(() => expect(rigsState.dirty).toBe(true));
        expect(rigsState.draft?.mode_mappings).toEqual({ 'DATA-U': { mode: 'RTTY' } });

        // Cancel must drop the override and go clean — the fix remounts the editor
        // (fresh draft object) so it re-snapshots the pristine value instead of
        // writing 'RTTY' back.
        await fireEvent.click(screen.getByText('Cancel'));
        flushSync();
        expect(rigsState.dirty).toBe(false);
        expect(rigsState.draft?.mode_mappings).toBeUndefined();
    });

    // THE PILL NAMES WHAT IT ACTUALLY TESTS. It branches on default_rig_id — the
    // rig the daemon connects to at its NEXT start — but read "active", which
    // claims the daemon has that rig open RIGHT NOW. Those are different things
    // and the daemon keeps them apart deliberately: the active rig is pinned at
    // boot (qsoservice SetActiveRig) and is what stamps MY_RIG on a QSO, while
    // "Set as default" only takes effect on restart. So from the moment the
    // operator pressed that button the badge asserted the one thing that had
    // just become false.
    //
    // No test asserted this string before, which is exactly why a wrong label
    // could sit there. Until activeRigID reaches the SPA (dogfood inbox
    // 2026-08-02) the honest word is the one the button uses.
    it('the default rig is badged "default", never "active"', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [
                { id: 1, model: 'ftdx10', port: '/dev/a' },
                { id: 2, model: 'ftdx10', port: '/dev/b' },
            ],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', rig_modes: ['DATA-U'] }],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();

        // Scoped by the badge's own tooltip: "default" alone also matches other
        // copy on the panel, and a loose match would make this rule pass on
        // text that has nothing to do with the pill.
        const badge = screen.getByTitle(/connects to at startup/i);
        expect(badge.textContent?.trim()).toBe('default');
        expect(screen.queryByText('active')).toBeNull();

        // The fixture holds a SECOND, non-default rig, so the badge is proven to
        // mark a state rather than to appear on every rig. Selecting it shows
        // the button instead — and that button's own wording is the reason
        // "default" is the right word for the badge.
        //
        // Selected by POSITION: both rigs are the same model, so nameFor()
        // renders them identically and there is no distinguishing text to
        // click. The list order is the fixture's order.
        const rigButtons = document.querySelectorAll('ul button');
        expect(rigButtons).toHaveLength(2);
        await fireEvent.click(rigButtons[1]);
        flushSync();
        expect(rigsState.selectedId).toBe(2);
        expect(screen.queryByTitle(/connects to at startup/i)).toBeNull();
        expect(screen.getByText('Set as default')).toBeTruthy();
    });
});
