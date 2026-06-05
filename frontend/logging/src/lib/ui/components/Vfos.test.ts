import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import Vfos from './Vfos.svelte';
import { catState } from '../../states/cat.svelte';
import { manualState } from '../../states/manual.svelte';
import { configState } from '../../states/config.svelte';
import { bridgeState } from '../../states/bridge.svelte';

/**
 * Vfos display tests — exercises the four state combinations of
 * (selectedVfo: 'A' | 'B') × (split: true | false) and asserts that
 * the rendered DOM places the right VFO in the RX position and shows
 * the action labels (RX/TX) only when split is on.
 *
 * Per ADR 0009's four-object decomposition, displayedState is what
 * Vfos reads. With CAT off in beforeEach (configState.station.enabled
 * = false), displayedState reads from manualState — so test setup
 * writes manualState fields, and commit-routing tests assert against
 * manualState. Split is structurally derived from VFO frequency
 * divergence in CAT-off mode (manualState.vfoA !== manualState.vfoB);
 * tests force split=true by setting different frequencies.
 *
 * All four state singletons are reset in beforeEach to avoid bleed.
 */

describe('Vfos', () => {
    beforeEach(() => {
        // Clear localStorage so persisted manualState fields from a
        // prior test don't bleed into the current one (ADR 0011).
        try {
            localStorage.clear();
        } catch {
            // jsdom should always provide localStorage; if not, in-memory
            // state alone is what matters for these tests.
        }

        // Reset catState (even though Vfos no longer reads it directly,
        // a previous test in the run may have written to it).
        catState.rigIdentity = '';
        catState.vfoA = 14_250_000;
        catState.vfoB = 14_250_000;
        catState.mode = 'USB';
        catState.subMode = '';
        catState.selectedVfo = 'A';
        catState.splitOverride = null;
        catState.power = 0;

        // Reset manualState — defaults make split=false (vfoA === vfoB).
        manualState.vfoA = 14_250_000;
        manualState.vfoB = 14_250_000;
        manualState.mode = 'USB';
        manualState.subMode = '';
        manualState.selectedVfo = 'A';

        // Reset configState — CAT off by default, so displayedState reads
        // from manualState (vfo/mode/etc) and configState.station.defaultPower (power).
        configState.station.enabled = false;
        configState.station.ampEnabled = false;
        configState.station.ampMultiplier = 1.0;
        configState.station.defaultPower = 100;

        // No exposed rig ops by default — VFO boxes stay inert when CAT is
        // live, so the existing CAT-live tests below see disabled boxes.
        configState.bridge.ops = [];

        // Reset bridgeState — disconnected by default.
        bridgeState.connected = false;
        bridgeState.rigResponding = false;
    });

    afterEach(() => {
        cleanup();
    });

    describe('selectedVfo=A', () => {
        it('renders VFO-A in the top (RX) position when split is off', () => {
            manualState.selectedVfo = 'A';
            // beforeEach defaults give vfoA === vfoB → split derives to false
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('VFO-A');
            expect(text).toContain('VFO-B');
            expect(text.indexOf('VFO-A')).toBeLessThan(text.indexOf('VFO-B'));
        });

        it('does not render RX/TX action labels when split is off', () => {
            manualState.selectedVfo = 'A';
            // vfoA === vfoB by default → split=false
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).not.toContain('RX');
            expect(text).not.toContain('TX');
        });

        it('renders VFO-A in RX position with RX/TX labels visible when split is on', () => {
            manualState.selectedVfo = 'A';
            // Force frequency divergence → split derives to true
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 14_300_000;
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('RX');
            expect(text).toContain('TX');
            expect(text).toContain('VFO-A');
            expect(text).toContain('VFO-B');
            // DOM order: RX → VFO-A → (freq) → TX → VFO-B → (freq)
            expect(text.indexOf('RX')).toBeLessThan(text.indexOf('VFO-A'));
            expect(text.indexOf('VFO-A')).toBeLessThan(text.indexOf('TX'));
            expect(text.indexOf('TX')).toBeLessThan(text.indexOf('VFO-B'));
        });

        it('places vfoA frequency in the first input and vfoB in the second', () => {
            manualState.selectedVfo = 'A';
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 7_100_000;
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');

            expect(inputs).toHaveLength(2);
            expect(inputs[0].value).toBe('14.250.000');
            expect(inputs[1].value).toBe('7.100.000');
        });
    });

    describe('selectedVfo=B', () => {
        it('renders VFO-B in the top (RX) position when split is off', () => {
            manualState.selectedVfo = 'B';
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('VFO-A');
            expect(text).toContain('VFO-B');
            expect(text.indexOf('VFO-B')).toBeLessThan(text.indexOf('VFO-A'));
        });

        it('does not render RX/TX action labels when split is off', () => {
            manualState.selectedVfo = 'B';
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).not.toContain('RX');
            expect(text).not.toContain('TX');
        });

        it('renders VFO-B in RX position with RX/TX labels visible when split is on', () => {
            manualState.selectedVfo = 'B';
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 14_300_000;
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('RX');
            expect(text).toContain('TX');
            // DOM order: RX → VFO-B → (freq) → TX → VFO-A → (freq)
            expect(text.indexOf('RX')).toBeLessThan(text.indexOf('VFO-B'));
            expect(text.indexOf('VFO-B')).toBeLessThan(text.indexOf('TX'));
            expect(text.indexOf('TX')).toBeLessThan(text.indexOf('VFO-A'));
        });

        it('places vfoB frequency in the first input and vfoA in the second', () => {
            manualState.selectedVfo = 'B';
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 7_100_000;
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');

            expect(inputs).toHaveLength(2);
            expect(inputs[0].value).toBe('7.100.000');
            expect(inputs[1].value).toBe('14.250.000');
        });
    });

    describe('band display', () => {
        it('shows the band name next to each VFO when in-band', () => {
            manualState.selectedVfo = 'A';
            manualState.vfoA = 14_250_000; // 20m
            manualState.vfoB = 7_100_000; // 40m
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('20m');
            expect(text).toContain('40m');
        });

        it('shows no band name for an out-of-band frequency', () => {
            manualState.selectedVfo = 'A';
            manualState.vfoA = 5_000_000; // between 80m (3.5–4.0 MHz) and 60m (5.25–5.45 MHz) — out of band
            manualState.vfoB = 7_100_000; // 40m
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('40m'); // vfoB still shows its band
            expect(text).not.toContain('20m'); // vfoA is out of band; no band text
            expect(text).not.toContain('60m');
            expect(text).not.toContain('80m');
        });
    });

    /**
     * Commit-routing tests — verify that the closure passed into the `box`
     * snippet writes to the correct manualState slot regardless of which
     * VFO is in the RX vs TX position. The id of each input ("vfo-a" /
     * "vfo-b") stays tied to its manualState slot; the rendering position
     * changes with selectedVfo, but the routing must not.
     */
    describe('commit routing', () => {
        it('committing to vfo-a updates manualState.vfoA when selectedVfo=A', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const input = container.querySelector('#vfo-a') as HTMLInputElement;

            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '21.200' } });
            await fireEvent.blur(input);

            expect(manualState.vfoA).toBe(21_200_000);
            expect(manualState.vfoB).toBe(14_250_000); // untouched (default)
        });

        it('committing to vfo-b updates manualState.vfoB when selectedVfo=A', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const input = container.querySelector('#vfo-b') as HTMLInputElement;

            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '3.750' } });
            await fireEvent.blur(input);

            expect(manualState.vfoB).toBe(3_750_000);
            expect(manualState.vfoA).toBe(14_250_000); // untouched
        });

        it('committing to vfo-a updates manualState.vfoA when selectedVfo=B (RX/TX positions reversed)', async () => {
            manualState.selectedVfo = 'B';
            const { container } = render(Vfos);
            const input = container.querySelector('#vfo-a') as HTMLInputElement;

            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '28.500' } });
            await fireEvent.blur(input);

            expect(manualState.vfoA).toBe(28_500_000);
            expect(manualState.vfoB).toBe(14_250_000);
        });

        it('committing to vfo-b updates manualState.vfoB when selectedVfo=B', async () => {
            manualState.selectedVfo = 'B';
            const { container } = render(Vfos);
            const input = container.querySelector('#vfo-b') as HTMLInputElement;

            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '50.150' } });
            await fireEvent.blur(input);

            expect(manualState.vfoB).toBe(50_150_000);
            expect(manualState.vfoA).toBe(14_250_000);
        });

        it('does not update manualState when an invalid value is committed', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const input = container.querySelector('#vfo-a') as HTMLInputElement;

            await fireEvent.focus(input);
            await fireEvent.input(input, { target: { value: '14.' } });
            await fireEvent.blur(input);

            expect(manualState.vfoA).toBe(14_250_000); // unchanged
        });
    });

    /**
     * Select-VFO tests — clicking a VfoBox selects that VFO. Gated on
     * `canSelectVfo` = CAT off (local `manualState` swap) OR CAT live + the
     * rig exposes `swap_vfo`. With CAT off and no ops set (the default here),
     * a click writes `manualState.selectedVfo`. Keyboard equivalent (Enter /
     * Space) works when focused and not disabled.
     */
    describe('select VFO', () => {
        it('clicking VFO-B box swaps selectedVfo from A to B', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            await fireEvent.click(vfoBBox);

            expect(manualState.selectedVfo).toBe('B');
        });

        it('clicking VFO-A box swaps selectedVfo from B to A', async () => {
            manualState.selectedVfo = 'B';
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;

            await fireEvent.click(vfoABox);

            expect(manualState.selectedVfo).toBe('A');
        });

        it('clicking the currently-selected VFO is a no-op (stays selected)', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;

            await fireEvent.click(vfoABox);

            expect(manualState.selectedVfo).toBe('A');
        });

        it('top (selected) VfoBox has tabindex=-1 even when CAT is off', () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;

            expect(vfoABox.getAttribute('tabindex')).toBe('-1');
        });

        it('top (selected) VfoBox has no title (no affordance) even when CAT is off', () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;

            expect(vfoABox.hasAttribute('title')).toBe(false);
        });

        it('top (selected) VfoBox uses cursor-default, not cursor-pointer or cursor-not-allowed, when CAT is off', () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;

            expect(vfoABox.classList.contains('cursor-default')).toBe(true);
            expect(vfoABox.classList.contains('cursor-pointer')).toBe(false);
            expect(vfoABox.classList.contains('cursor-not-allowed')).toBe(false);
        });

        it('bottom (unselected) VfoBox has title="Select VFO" when CAT is off', () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            expect(vfoBBox.getAttribute('title')).toBe('Select VFO');
        });

        it('does not write to manualState when clicking the already-selected VFO', async () => {
            manualState.selectedVfo = 'A';
            // Spy on localStorage.setItem to detect any persistence side-effect
            // from a redundant manualState write triggering the $effect mirror.
            // (A meaningful click writes a NEW value; a no-op click should not.)
            // Set up: clear any pending writes, then click the selected box.
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;

            const before = localStorage.getItem('sm.manual.selectedVfo');
            await fireEvent.click(vfoABox);
            const after = localStorage.getItem('sm.manual.selectedVfo');

            // Either both null (effect hadn't run) or both equal — but
            // crucially manualState.selectedVfo is still 'A' and any
            // localStorage write would have set it to 'A' (same value).
            expect(manualState.selectedVfo).toBe('A');
            // The before/after relationship is non-strict because the
            // initial $effect.root may have written on render. The key
            // assertion is that the click itself didn't toggle state.
            expect(after).toBe(before === null ? null : 'A');
        });

        it('does not swap when CAT is operating (disabled)', async () => {
            manualState.selectedVfo = 'A';
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            await fireEvent.click(vfoBBox);

            expect(manualState.selectedVfo).toBe('A'); // unchanged
        });

        it('Enter key on focused VfoBox triggers select', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            await fireEvent.keyDown(vfoBBox, { key: 'Enter' });

            expect(manualState.selectedVfo).toBe('B');
        });

        it('Space key on focused VfoBox triggers select', async () => {
            manualState.selectedVfo = 'A';
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            await fireEvent.keyDown(vfoBBox, { key: ' ' });

            expect(manualState.selectedVfo).toBe('B');
        });

        it('Enter key does not trigger select when disabled', async () => {
            manualState.selectedVfo = 'A';
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            await fireEvent.keyDown(vfoBBox, { key: 'Enter' });

            expect(manualState.selectedVfo).toBe('A');
        });

        it('all VfoBoxes have role="button" and tabindex=-1 when editable (Tab skips boxes per session-28 design)', () => {
            // VfoBox is intentionally NOT in the tab order — operators
            // reach the swap action via mouse click or the planned
            // Ctrl+\ keyboard shortcut. Tab navigates from VfoInput
            // straight to the next field.
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            expect(vfoABox.getAttribute('role')).toBe('button');
            expect(vfoABox.getAttribute('tabindex')).toBe('-1');
            expect(vfoABox.getAttribute('aria-disabled')).toBe('false');
            expect(vfoBBox.getAttribute('role')).toBe('button');
            expect(vfoBBox.getAttribute('tabindex')).toBe('-1');
            expect(vfoBBox.getAttribute('aria-disabled')).toBe('false');
        });

        it('all VfoBoxes have tabindex=-1 and aria-disabled=true when CAT is operating', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            const { container } = render(Vfos);
            const vfoABox = container.querySelector('[data-vfo="VFO-A"]') as HTMLElement;
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;

            expect(vfoABox.getAttribute('tabindex')).toBe('-1');
            expect(vfoABox.getAttribute('aria-disabled')).toBe('true');
            expect(vfoBBox.getAttribute('tabindex')).toBe('-1');
            expect(vfoBBox.getAttribute('aria-disabled')).toBe('true');
        });

        it('makes the unselected box actionable when CAT is live and the rig exposes swap_vfo', () => {
            manualState.selectedVfo = 'A';
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            configState.bridge.ops = ['swap_vfo'];
            const { container } = render(Vfos);
            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;
            // CAT live but the rig CAN swap VFO → the unselected box is live
            // (the click sends swap_vfo; that path is covered in rigControl.test.ts).
            expect(vfoBBox.getAttribute('aria-disabled')).toBe('false');
            expect(vfoBBox.getAttribute('title')).toBe('Select VFO');
        });

        it('after swapping, the new selected VFO renders in the RX position', async () => {
            manualState.selectedVfo = 'A';
            const { container, rerender } = render(Vfos);

            const vfoBBox = container.querySelector('[data-vfo="VFO-B"]') as HTMLElement;
            await fireEvent.click(vfoBBox);
            await rerender({});

            const text = container.textContent ?? '';
            // After swap: VFO-B is now in RX (top) position, VFO-A in TX (bottom)
            expect(text.indexOf('VFO-B')).toBeLessThan(text.indexOf('VFO-A'));
        });
    });

    /**
     * Tab-order tests — VfoInput tabindex per session-28 design:
     *   - top input (selectedVfo's) is always Tab-reachable (tabindex=0).
     *   - bottom input (non-selected VFO's) is Tab-reachable only when
     *     split is on; non-split it gets tabindex=-1 so Tab skips past
     *     to Name.
     */
    describe('tab order — VfoInput', () => {
        it('top (selected) input has tabindex=0 when not split', () => {
            manualState.selectedVfo = 'A';
            // vfoA === vfoB → split=false
            const { container } = render(Vfos);
            const vfoAInput = container.querySelector('#vfo-a') as HTMLInputElement;
            expect(vfoAInput.getAttribute('tabindex')).toBe('0');
        });

        it('bottom (unselected) input has tabindex=-1 when not split', () => {
            manualState.selectedVfo = 'A';
            // vfoA === vfoB → split=false
            const { container } = render(Vfos);
            const vfoBInput = container.querySelector('#vfo-b') as HTMLInputElement;
            expect(vfoBInput.getAttribute('tabindex')).toBe('-1');
        });

        it('both inputs have tabindex=0 when split', () => {
            manualState.selectedVfo = 'A';
            // Force frequency divergence → split=true
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 14_300_000;
            const { container } = render(Vfos);
            const vfoAInput = container.querySelector('#vfo-a') as HTMLInputElement;
            const vfoBInput = container.querySelector('#vfo-b') as HTMLInputElement;
            expect(vfoAInput.getAttribute('tabindex')).toBe('0');
            expect(vfoBInput.getAttribute('tabindex')).toBe('0');
        });

        it('selectedVfo=B → vfo-b is the top (tabindex=0), vfo-a is bottom (tabindex=-1) when not split', () => {
            manualState.selectedVfo = 'B';
            const { container } = render(Vfos);
            const vfoBInput = container.querySelector('#vfo-b') as HTMLInputElement;
            const vfoAInput = container.querySelector('#vfo-a') as HTMLInputElement;
            expect(vfoBInput.getAttribute('tabindex')).toBe('0');
            expect(vfoAInput.getAttribute('tabindex')).toBe('-1');
        });
    });

    /**
     * Edit-affordance tests — verify that VFO inputs are disabled when
     * displayedState.editable is false (CAT enabled + bridge connected
     * + rig responding) and enabled otherwise. Per ADR 0006 / 0009 /
     * 0010, the rule is: operator can edit when ANY of the three
     * conditions is false.
     */
    describe('edit affordance', () => {
        it('enables inputs when CAT is off (default)', () => {
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');
            expect(inputs[0].disabled).toBe(false);
            expect(inputs[1].disabled).toBe(false);
        });

        it('disables inputs when CAT is enabled, bridge connected, and rig responding', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');
            expect(inputs[0].disabled).toBe(true);
            expect(inputs[1].disabled).toBe(true);
        });

        it('keeps inputs enabled when CAT is enabled but bridge is disconnected', () => {
            configState.station.enabled = true;
            bridgeState.connected = false; // transport down
            bridgeState.rigResponding = false;
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');
            expect(inputs[0].disabled).toBe(false);
        });

        it('keeps inputs enabled when CAT is enabled and bridge connected but rig not responding', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = false; // rig off
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');
            expect(inputs[0].disabled).toBe(false);
        });
    });
});
