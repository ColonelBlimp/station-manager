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
});
