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

    // alpha.2 dogfood Finding 3 (W-0012): the default rig must not be deletable;
    // the operator changes the default first. Before this pin the SPA let the
    // default be deleted and silently repointed default_rig_id to the first
    // survivor — mechanically safe (the daemon refuses an unresolvable default),
    // but a policy the operator rejected. The pin walks the whole ruling as the
    // operator sees it: disabled with the reason on the default, enabled on a
    // non-default rig, and enabled on the former default once "Set as default"
    // has moved the badge — the explicit change the reason asks for.
    it('Delete is disabled on the default rig with the reason, and enabled once the default moves', async () => {
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
        expect(rigsState.selectedId).toBe(1); // load pre-selects the default

        // On the default: disabled, and the reason names the action that unlocks it.
        const del = screen.getByRole('button', { name: 'Delete' });
        expect(del).toBeDisabled();
        expect(del.title).toMatch(/default rig/i);
        expect(del.title).toMatch(/set another rig as default/i);
        // The reason is also stated in the panel, not only in a tooltip: whether a
        // tooltip shows on a disabled control is browser behaviour this test does
        // not vouch for.
        expect(screen.getByText(/set another rig as default first/i)).toBeTruthy();

        // A non-default rig deletes as before. Selected by position: both rigs are
        // the same model, so nameFor() renders them identically.
        const rigButtons = document.querySelectorAll('ul button');
        await fireEvent.click(rigButtons[1]);
        flushSync();
        expect(rigsState.selectedId).toBe(2);
        const del2 = screen.getByRole('button', { name: 'Delete' });
        expect(del2).not.toBeDisabled();
        expect(del2.title).toBe('Delete this rig');
        expect(screen.queryByText(/set another rig as default first/i)).toBeNull();

        // The explicit default change: rig 2 becomes the default, so rig 1 is now
        // an ordinary rig and its Delete comes back.
        await fireEvent.click(screen.getByText('Set as default'));
        await vi.waitFor(() => expect(rigsState.defaultRigId).toBe(2));
        flushSync();
        const del2Now = screen.getByRole('button', { name: 'Delete' });
        expect(del2Now).toBeDisabled(); // rig 2 is the default now
        await fireEvent.click(rigButtons[0]);
        flushSync();
        expect(rigsState.selectedId).toBe(1);
        const del1 = screen.getByRole('button', { name: 'Delete' });
        expect(del1).not.toBeDisabled();
        expect(del1.title).toBe('Delete this rig');
    });

    // The only rig is necessarily undeletable AND (normally) the default. The two
    // reasons differ in what they ask the operator to do, and "set another rig as
    // default" is impossible with one rig, so the only-rig reason must win.
    it('the only rig keeps the only-rig reason, not the default-rig reason', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', rig_modes: ['DATA-U'] }],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        const del = screen.getByRole('button', { name: 'Delete' });
        expect(del).toBeDisabled();
        expect(del.title).toBe('Cannot delete the only rig');
        expect(screen.queryByText(/set another rig as default first/i)).toBeNull();
    });

    it('the detail names the rig once: no manufacturer · model subtitle, no description', async () => {
        // alpha.2 dogfood Finding #18 (W-0012): every shipped rigdef's name IS
        // "<manufacturer> <model>" and the Model select shows the name again, so
        // the subtitle repeated the heading. The description stays unsurfaced
        // here by ruling — this panel configures the rig, it doesn't describe it.
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [
                {
                    id: 'ftdx10',
                    name: 'Yaesu FTdx10',
                    manufacturer: 'Yaesu',
                    model: 'FTdx10',
                    description: 'HF/50 MHz SDR transceiver',
                    rig_modes: ['DATA-U'],
                },
            ],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('Yaesu FTdx10');
        expect(screen.queryByText('Yaesu · FTdx10')).toBeNull();
        expect(screen.queryByText('HF/50 MHz SDR transceiver')).toBeNull();
    });
});
