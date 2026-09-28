import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import RigsSection from './RigsSection.svelte';
import { rigsState } from './rigs.svelte';
import { bridgeEnabledState } from './bridgeEnabled.svelte';

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
    rigsState.newRig = null;
    bridgeEnabledState.loaded = false;
    bridgeEnabledState.enabled = false;
});

// GET /v1/config answers the CAT switch (bridge_enabled); the returned array
// collects PUT bodies.
function mockCluster(rigsBody: object, catOn = false): string[] {
    const puts: string[] = [];
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
            if ((init?.method ?? 'GET') === 'PUT') {
                puts.push(init?.body as string);
                return resp({});
            }
            if (url.includes('/v1/hardware'))
                return resp({ serial_ports: [], audio: { available: false } });
            if (url.includes('/v1/config')) return resp({ bridge_enabled: catOn });
            return resp(rigsBody);
        })
    );
    return puts;
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

    // Ruling 2026-09-28 (replaces alpha.2 Finding 3's 'set another rig as default
    // first'): 'no default rig' is a valid setup state, so the default rig is
    // deletable — clearing the default — once CAT is off; CAT needs the default
    // rig. The tooltip names the one thing that unlocks it; a rig that is not the
    // default is not in use and deletes with CAT on.
    it('Delete on the default rig: disabled with the CAT reason while CAT is on, enabled with it off', async () => {
        const body = {
            default_rig_id: 1,
            rigs: [
                { id: 1, model: 'ftdx10', port: '/dev/a' },
                { id: 2, model: 'ftdx10', port: '/dev/b' },
            ],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', rig_modes: ['DATA-U'] }],
        };
        mockCluster(body, true);
        const { unmount } = render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        expect(rigsState.selectedId).toBe(1); // load pre-selects the default
        const del = screen.getByRole('button', { name: 'Delete' });
        expect(del).toBeDisabled();
        expect(del.title).toBe('Turn off the rig connection (CAT) first');
        // A non-default rig deletes with CAT on — it is not the rig in use.
        await fireEvent.click(document.querySelectorAll('ul button')[1]);
        flushSync();
        expect(screen.getByRole('button', { name: 'Delete' })).not.toBeDisabled();
        unmount();

        mockCluster(body, false);
        render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        const delOff = screen.getByRole('button', { name: 'Delete' });
        expect(delOff).not.toBeDisabled();
        expect(delOff.title).toBe('Delete this rig');
    });

    // Ruling 2026-09-28: rigs are profiles until one is set as default. With none
    // set the list says so and what to do; it goes once one is.
    it('says "No default rig — set one to use it." until a default is set', async () => {
        mockCluster({
            default_rig_id: 0,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        expect(screen.getByText('No default rig — set one to use it.')).toBeInTheDocument();
        await fireEvent.click(screen.getByText('Set as default'));
        await vi.waitFor(() => expect(rigsState.defaultRigId).toBe(1));
        flushSync();
        expect(screen.queryByText('No default rig — set one to use it.')).toBeNull();
    });

    // CAT needs the default rig's serial port (validateBridge). Turning it ON is
    // blocked, with the missing step named, until both exist; turning it OFF never is.
    it('the CAT switch: turning it on waits for a default rig with a port; turning it off never waits', async () => {
        const cat = () => screen.getByRole('checkbox', { name: /Enable rig connection/ });
        mockCluster({
            default_rig_id: 0,
            rigs: [{ id: 1, model: 'ftdx10', port: '' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        const { unmount } = render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        expect(cat()).toBeDisabled();
        expect(screen.getByText('Set a default rig first')).toBeInTheDocument();

        rigsState.defaultRigId = 1;
        flushSync();
        expect(cat()).toBeDisabled();
        expect(
            screen.getByText('Choose a serial port for the default rig first')
        ).toBeInTheDocument();

        rigsState.rigs = [{ id: 1, model: 'ftdx10', port: '/dev/a' }];
        flushSync();
        expect(cat()).not.toBeDisabled();
        expect(screen.queryByText(/first$/)).toBeNull();
        unmount();

        // Already on (a hand-edited config, say) with no default: it can still go off.
        mockCluster(
            { default_rig_id: 0, rigs: [{ id: 1, model: 'ftdx10', port: '' }], catalogue: [] },
            true
        );
        render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        expect(cat()).not.toBeDisabled();
    });

    // Fresh-install ruling 2026-09-26 + 2026-09-28: the last rig is deletable, but
    // only with CAT off — the daemon refuses an enabled bridge with no rig. With CAT
    // on the button is disabled and its tooltip names the one thing that unlocks
    // it; it is never the default-rig reason ("set another rig as default" is
    // impossible with one rig).
    it('the only rig: Delete enabled with CAT off, disabled with the CAT reason with CAT on', async () => {
        const body = {
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10', rig_modes: ['DATA-U'] }],
        };
        mockCluster(body, true);
        const { unmount } = render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        const del = screen.getByRole('button', { name: 'Delete' });
        expect(del).toBeDisabled();
        expect(del.title).toBe('Turn off the rig connection (CAT) first');
        unmount();

        mockCluster(body, false);
        render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        const del2 = screen.getByRole('button', { name: 'Delete' });
        expect(del2).not.toBeDisabled();
        expect(del2.title).toBe('Delete this rig');
    });

    it('deleting the last rig (CAT off, confirmed) clears the list to the empty state', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
        render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
        await vi.waitFor(() => expect(puts).toHaveLength(1));
        expect(confirm.mock.calls[0][0]).toMatch(/only rig/i);
        expect(JSON.parse(puts[0])).toEqual({ rigs: [], default_rig_id: 0 });
        flushSync();
        expect(screen.getByText('No rigs configured.')).toBeInTheDocument();
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

// Fresh-install ruling 2026-09-26: '+ Add rig' opens a picker of the catalogue
// models (an already-added model marked, still pickable); picking opens an
// UNSAVED draft. Cancel changes nothing; Save creates it; the first saved rig
// becomes the default. Found: one Add click created an IC-7300 default with no
// port that could not be cancelled or deleted.
describe('RigsSection add-rig picker', () => {
    const catalogue = [
        { id: 'ic7300', name: 'IC-7300' },
        { id: 'ftdx10', name: 'FTdx10' },
    ];

    it('lists the models, marks one already added, and Cancel on the draft writes nothing', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
            catalogue,
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();

        await fireEvent.click(screen.getByRole('button', { name: '+ Add rig' }));
        flushSync();
        const picker = screen.getByRole('group', { name: 'Choose a rig model' });
        const ic = within(picker).getByRole('button', { name: /IC-7300/ });
        expect(ic).toHaveTextContent(/already added/i); // marked…
        expect(ic).not.toBeDisabled(); // …still pickable
        expect(within(picker).getByRole('button', { name: /FTdx10/ })).not.toHaveTextContent(
            /already added/i
        );

        await fireEvent.click(within(picker).getByRole('button', { name: /FTdx10/ }));
        flushSync();
        expect(screen.queryByRole('group', { name: 'Choose a rig model' })).toBeNull();
        expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('FTdx10');
        expect(screen.getByText('not saved — Save to add it')).toBeInTheDocument();
        // No default / delete controls on a rig that doesn't exist yet.
        expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();
        expect(screen.queryByText('Set as default')).toBeNull();
        // The list can't switch away and silently drop the draft.
        for (const b of document.querySelectorAll('ul button')) expect(b).toBeDisabled();

        await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
        flushSync();
        expect(puts).toHaveLength(0);
        expect(rigsState.rigs.map((r) => r.id)).toEqual([1]);
        expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('IC-7300');
    });

    it('closing the picker without choosing changes nothing', async () => {
        const puts = mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ic7300', port: '/dev/a' }],
            catalogue,
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: '+ Add rig' }));
        flushSync();
        const picker = screen.getByRole('group', { name: 'Choose a rig model' });
        await fireEvent.click(within(picker).getByRole('button', { name: 'Cancel' }));
        flushSync();
        expect(screen.queryByRole('group', { name: 'Choose a rig model' })).toBeNull();
        expect(rigsState.newRig).toBeNull();
        expect(puts).toHaveLength(0);
    });

    it('from no rigs: pick and Save adds a profile, not a default', async () => {
        const puts = mockCluster({ default_rig_id: 0, rigs: [], catalogue });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Add rig' }));
        flushSync();
        const picker = screen.getByRole('group', { name: 'Choose a rig model' });
        await fireEvent.click(within(picker).getByRole('button', { name: /IC-7300/ }));
        flushSync();
        expect(puts).toHaveLength(0); // picking writes nothing
        expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('IC-7300');

        await fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await vi.waitFor(() => expect(puts).toHaveLength(1));
        expect(JSON.parse(puts[0])).toEqual({ rigs: [{ id: 1, model: 'ic7300', port: '' }] });
        flushSync();
        expect(screen.queryAllByText(/not saved/i)).toHaveLength(0);
        expect(rigsState.defaultRigId).toBe(0);
        expect(screen.getByText('No default rig — set one to use it.')).toBeInTheDocument();
        expect(screen.getByText('Set as default')).toBeInTheDocument();
    });
});

// Operator 2026-09-28: the serial-port select had its own monospace, smaller
// font; it matches the audio selects beside it.
describe('RigsSection connection pickers', () => {
    it('the serial port select uses the same font as the audio selects', async () => {
        mockCluster({
            default_rig_id: 1,
            rigs: [{ id: 1, model: 'ftdx10', port: '/dev/a' }],
            catalogue: [{ id: 'ftdx10', name: 'FTdx10' }],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        const port = screen.getByRole('combobox', { name: 'Serial port' });
        expect(port.className).not.toMatch(/font-mono|text-xs/);
        expect(port.className).toBe(screen.getByRole('combobox', { name: 'Model' }).className);
    });
});

// Review of the no-default slice (P2): the CAT and rig requests run at once, so
// until the rigs have loaded — or if they failed — whether a default rig with a
// port exists is unknown. Unknown blocks turning CAT on; an already-on switch can
// still be turned off.
describe('RigsSection CAT switch before the rigs load', () => {
    function mockRigsFailing(catOn: boolean): void {
        const resp = (body: unknown, status = 200) =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string) => {
                if (url.includes('/v1/config')) return resp({ bridge_enabled: catOn });
                if (url.includes('/v1/hardware'))
                    return resp({ serial_ports: [], audio: { available: false } });
                return resp({ message: 'boom' }, 500);
            })
        );
    }

    it('rigs failed to load: turning CAT on is blocked, with a reason', async () => {
        mockRigsFailing(false);
        render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.error).not.toBe(''));
        flushSync();
        expect(screen.getByRole('checkbox', { name: /Enable rig connection/ })).toBeDisabled();
        expect(screen.getByText('Load the rigs first')).toBeInTheDocument();
    });

    it('rigs failed to load: an already-on switch can still be turned off', async () => {
        mockRigsFailing(true);
        render(RigsSection);
        await vi.waitFor(() => expect(bridgeEnabledState.loaded).toBe(true));
        await vi.waitFor(() => expect(rigsState.error).not.toBe(''));
        flushSync();
        expect(screen.getByRole('checkbox', { name: /Enable rig connection/ })).not.toBeDisabled();
    });
});

// Review of the no-default slice (P2): a new rig's draft showed 'Changes take
// effect after a daemon restart' — adding never selects the default.
describe('RigsSection new-rig draft', () => {
    it('shows no restart note on a new rig', async () => {
        mockCluster({
            default_rig_id: 0,
            rigs: [],
            catalogue: [{ id: 'ic7300', name: 'IC-7300' }],
        });
        render(RigsSection);
        await vi.waitFor(() => expect(rigsState.loaded).toBe(true));
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Add rig' }));
        flushSync();
        await fireEvent.click(
            within(screen.getByRole('group', { name: 'Choose a rig model' })).getByRole('button', {
                name: /IC-7300/,
            })
        );
        flushSync();
        expect(screen.queryByText(/Changes take effect after a daemon restart/)).toBeNull();
    });
});
