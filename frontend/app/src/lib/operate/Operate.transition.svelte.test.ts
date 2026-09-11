// W-0019 slice 4 (ADR 0080), rendered: the shared FT view is keyed on the
// operating mode, so a sidebar click AND a browser Back/Forward between FT8 and
// FT4 each execute close → claim → open — the old stream closed before the new
// profile is claimed, and the stream opened only after the claim.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup } from '@testing-library/svelte';
import Operate from './Operate.svelte';
import { router, setMode } from '../router.svelte';
import { resetFt8ForTests, setFt8Transport, setFt8Claimer } from './ft8.svelte';
import { rig } from './rig.svelte';

describe('FT8 ↔ FT4 transitions remount the view through close → claim → open', () => {
    let events: string[] = [];

    beforeEach(() => {
        resetFt8ForTests();
        events = [];
        setFt8Transport((_h, mode) => {
            events.push(`open:${mode ?? ''}`);
            return () => events.push('close');
        });
        setFt8Claimer((mode) => {
            events.push(`claim:${mode}`);
            return Promise.resolve({ kind: 'ok', mode: mode.toUpperCase() });
        });
        rig.cat = 'connected';
        router.view = 'operate';
        setMode('ft8');
        events = [];
    });

    afterEach(() => {
        cleanup();
        setMode('phone');
    });

    it('sidebar: FT8 → FT4 → FT8', async () => {
        render(Operate);
        await vi.waitFor(() => expect(events).toEqual(['claim:ft8', 'open:ft8']));
        events = [];

        setMode('ft4');
        await vi.waitFor(() => expect(events).toEqual(['close', 'claim:ft4', 'open:ft4']));
        events = [];

        setMode('ft8');
        await vi.waitFor(() => expect(events).toEqual(['close', 'claim:ft8', 'open:ft8']));
    });

    it('Back/Forward: a popstate between the FT modes takes the same path', async () => {
        render(Operate);
        await vi.waitFor(() => expect(events).toEqual(['claim:ft8', 'open:ft8']));
        events = [];

        window.history.pushState({}, '', '/operate/ft4');
        window.dispatchEvent(new PopStateEvent('popstate'));
        await vi.waitFor(() => expect(events).toEqual(['close', 'claim:ft4', 'open:ft4']));
        events = [];

        window.history.pushState({}, '', '/operate/ft8');
        window.dispatchEvent(new PopStateEvent('popstate'));
        await vi.waitFor(() => expect(events).toEqual(['close', 'claim:ft8', 'open:ft8']));
    });
});
