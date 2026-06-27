import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import MyStationPanel from './MyStationPanel.svelte';

const STORAGE_KEY = 'sm.myStation.activeSection';

function resetState(): void {
    try {
        sessionStorage.clear();
        localStorage.clear();
    } catch {
        // jsdom should provide these; in-memory snapshot is what
        // matters for the assertion.
    }
}

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

        // Sections order is identity → location → equipment → qso (About moved to
        // the config SPA's General tab), so ArrowLeft from identity wraps to qso.
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
