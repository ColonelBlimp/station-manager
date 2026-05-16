import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import MyStationPanel from './MyStationPanel.svelte';
import { configState } from '../../states/config.svelte';

/**
 * Mode-mappings clobber regression (review finding C1).
 *
 * Pins that an external mutation to `configState.bridge.modeMappings`
 * while the operator is editing the Mode Mappings sub-tab does NOT
 * stomp the in-progress edits. Pre-fix the `$effect.pre` had
 * `configState.bridge.modeMappings` as an implicit tracked dependency
 * via the snap function it called — any reactive update wiped
 * `editingModes`. The untrack() fix tracks only `activeSection`.
 */

const STORAGE_KEY = 'sm.myStation.activeSection';

function resetState(): void {
    try {
        sessionStorage.clear();
        localStorage.clear();
    } catch {
        // jsdom should provide these; in-memory snapshot is what
        // matters for the assertion.
    }
    // Minimal CAT/bridge state — one rig, one mapping.
    configState.station.enabled = true;
    configState.station.rigName = 'TEST-RIG';
    configState.bridge.driver = 'test';
    configState.bridge.rigModes = ['LSB'];
    configState.bridge.modeMappings = { LSB: { mode: 'SSB', submode: 'LSB' } };
}

describe('MyStationPanel — mode-mappings clobber regression (C1)', () => {
    beforeEach(() => {
        resetState();
    });

    afterEach(() => {
        cleanup();
        resetState();
    });

    it('preserves in-progress mode-mapping edits when configState.bridge.modeMappings is replaced', async () => {
        // Land directly on the 'modes' tab so the $effect.pre snaps
        // editingModes on mount — same shape as the operator clicking
        // the Mode Mappings button.
        sessionStorage.setItem(STORAGE_KEY, 'modes');

        render(MyStationPanel);
        await tick();
        await tick();

        // The mode input for the LSB row should reflect the daemon's
        // current mapping (SSB).
        const modeInput = document.getElementById('mode-map-LSB-mode') as HTMLInputElement | null;
        expect(modeInput).not.toBeNull();
        expect(modeInput!.value).toBe('SSB');

        // Operator types a new value into the input. fireEvent.input
        // updates the DOM AND the bind:value target (editingModes
        // entry).
        await fireEvent.input(modeInput!, { target: { value: 'TESTMODE' } });
        await tick();
        expect(modeInput!.value).toBe('TESTMODE');

        // External mutation: simulates a periodic /v1/config refresh
        // (or any future actor that re-hydrates the bridge view) writing
        // a different default mapping. With the pre-fix code this re-
        // fires the $effect.pre and wipes 'TESTMODE' back to 'CW'.
        configState.bridge.modeMappings = { LSB: { mode: 'CW', submode: '' } };
        await tick();
        await tick();

        // The operator's edit must survive.
        expect(modeInput!.value).toBe('TESTMODE');
    });

    it('snaps fresh values when transitioning INTO the modes tab', async () => {
        // Start on a different tab — the snap should not have run yet.
        sessionStorage.setItem(STORAGE_KEY, 'identity');

        const { container } = render(MyStationPanel);
        await tick();

        // Click the Mode Mappings tab.
        const tabs = Array.from(
            container.querySelectorAll<HTMLButtonElement>('button[role="tab"]')
        );
        const modesTab = tabs.find((b) => b.textContent?.includes('Mode Mappings'));
        expect(modesTab).toBeDefined();
        await fireEvent.click(modesTab!);
        await tick();
        await tick();

        // First-snap on the INTO-modes transition: input must reflect
        // configState's current value.
        const modeInput = document.getElementById('mode-map-LSB-mode') as HTMLInputElement | null;
        expect(modeInput).not.toBeNull();
        expect(modeInput!.value).toBe('SSB');
    });
});

describe('MyStationPanel — tablist keyboard nav (I15)', () => {
    beforeEach(() => {
        resetState();
    });

    afterEach(() => {
        cleanup();
        resetState();
    });

    function activeTabId(container: HTMLElement): string | null {
        const active = container.querySelector<HTMLButtonElement>(
            'button[role="tab"][aria-selected="true"]'
        );
        return active?.id ?? null;
    }

    it('uses roving tabindex (active=0, inactive=-1)', async () => {
        sessionStorage.setItem(STORAGE_KEY, 'identity');
        const { container } = render(MyStationPanel);
        await tick();

        const tabs = Array.from(
            container.querySelectorAll<HTMLButtonElement>('button[role="tab"]')
        );
        const activeTabs = tabs.filter((b) => b.tabIndex === 0);
        const inactiveTabs = tabs.filter((b) => b.tabIndex === -1);
        expect(activeTabs.length).toBe(1);
        expect(inactiveTabs.length).toBe(tabs.length - 1);
        expect(activeTabs[0].id).toBe('my-station-tab-identity');
    });

    it('ArrowRight advances to the next tab and moves focus', async () => {
        sessionStorage.setItem(STORAGE_KEY, 'identity');
        const { container } = render(MyStationPanel);
        await tick();

        const identityTab = document.getElementById('my-station-tab-identity') as HTMLButtonElement;
        identityTab.focus();
        await fireEvent.keyDown(identityTab, { key: 'ArrowRight' });
        await tick();

        expect(activeTabId(container)).toBe('my-station-tab-location');
        expect(document.activeElement?.id).toBe('my-station-tab-location');
    });

    it('ArrowLeft from first wraps to last', async () => {
        sessionStorage.setItem(STORAGE_KEY, 'identity');
        const { container } = render(MyStationPanel);
        await tick();

        const identityTab = document.getElementById('my-station-tab-identity') as HTMLButtonElement;
        identityTab.focus();
        await fireEvent.keyDown(identityTab, { key: 'ArrowLeft' });
        await tick();

        // Sections order is identity → location → equipment → modes → cw → qso,
        // so ArrowLeft from identity wraps to qso.
        expect(activeTabId(container)).toBe('my-station-tab-qso');
        expect(document.activeElement?.id).toBe('my-station-tab-qso');
    });

    it('End jumps to last tab', async () => {
        sessionStorage.setItem(STORAGE_KEY, 'identity');
        const { container } = render(MyStationPanel);
        await tick();

        const identityTab = document.getElementById('my-station-tab-identity') as HTMLButtonElement;
        identityTab.focus();
        await fireEvent.keyDown(identityTab, { key: 'End' });
        await tick();

        expect(activeTabId(container)).toBe('my-station-tab-qso');
    });

    it('Home jumps to first tab', async () => {
        sessionStorage.setItem(STORAGE_KEY, 'qso');
        const { container } = render(MyStationPanel);
        await tick();

        const qsoTab = document.getElementById('my-station-tab-qso') as HTMLButtonElement;
        qsoTab.focus();
        await fireEvent.keyDown(qsoTab, { key: 'Home' });
        await tick();

        expect(activeTabId(container)).toBe('my-station-tab-identity');
    });

    it('tabpanel has aria-labelledby pointing at the active tab', async () => {
        sessionStorage.setItem(STORAGE_KEY, 'equipment');
        const { container } = render(MyStationPanel);
        await tick();

        const panel = container.querySelector<HTMLElement>('#my-station-equipment');
        expect(panel?.getAttribute('aria-labelledby')).toBe('my-station-tab-equipment');
    });
});
