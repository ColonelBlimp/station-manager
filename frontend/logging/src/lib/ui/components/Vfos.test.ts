import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, cleanup } from '@testing-library/svelte';
import Vfos from './Vfos.svelte';
import { catState } from '../../states/cat.svelte';

/**
 * Vfos display tests — exercises the four state combinations of
 * (selectedVfo: 'A' | 'B') × (split: true | false) and asserts that
 * the rendered DOM places the right VFO in the RX position and shows
 * the action labels (RX/TX) only when split is on.
 *
 * catState is a module-level singleton, so each test resets it in
 * beforeEach to avoid state bleeding across cases.
 */

describe('Vfos', () => {
    beforeEach(() => {
        catState.enabled = false;
        catState.rigIdentity = '';
        catState.vfoA = 14_250_000;
        catState.vfoB = 7_100_000;
        catState.mode = 'USB';
        catState.subMode = '';
        catState.selectedVfo = 'A';
        catState.split = false;
    });

    afterEach(() => {
        cleanup();
    });

    describe('selectedVfo=A', () => {
        it('renders VFO-A in the top (RX) position when split is off', () => {
            catState.selectedVfo = 'A';
            catState.split = false;
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('VFO-A');
            expect(text).toContain('VFO-B');
            expect(text.indexOf('VFO-A')).toBeLessThan(text.indexOf('VFO-B'));
        });

        it('does not render RX/TX action labels when split is off', () => {
            catState.selectedVfo = 'A';
            catState.split = false;
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).not.toContain('RX');
            expect(text).not.toContain('TX');
        });

        it('renders VFO-A in RX position with RX/TX labels visible when split is on', () => {
            catState.selectedVfo = 'A';
            catState.split = true;
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
            catState.selectedVfo = 'A';
            catState.split = false;
            catState.vfoA = 14_250_000;
            catState.vfoB = 7_100_000;
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');

            expect(inputs).toHaveLength(2);
            expect((inputs[0] as HTMLInputElement).value).toBe('14.250.000');
            expect((inputs[1] as HTMLInputElement).value).toBe('7.100.000');
        });
    });

    describe('selectedVfo=B', () => {
        it('renders VFO-B in the top (RX) position when split is off', () => {
            catState.selectedVfo = 'B';
            catState.split = false;
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('VFO-A');
            expect(text).toContain('VFO-B');
            expect(text.indexOf('VFO-B')).toBeLessThan(text.indexOf('VFO-A'));
        });

        it('does not render RX/TX action labels when split is off', () => {
            catState.selectedVfo = 'B';
            catState.split = false;
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).not.toContain('RX');
            expect(text).not.toContain('TX');
        });

        it('renders VFO-B in RX position with RX/TX labels visible when split is on', () => {
            catState.selectedVfo = 'B';
            catState.split = true;
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
            catState.selectedVfo = 'B';
            catState.split = false;
            catState.vfoA = 14_250_000;
            catState.vfoB = 7_100_000;
            const { container } = render(Vfos);
            const inputs = container.querySelectorAll('input');

            expect(inputs).toHaveLength(2);
            expect((inputs[0] as HTMLInputElement).value).toBe('7.100.000');
            expect((inputs[1] as HTMLInputElement).value).toBe('14.250.000');
        });
    });

    describe('band display', () => {
        it('shows the band name next to each VFO when in-band', () => {
            catState.selectedVfo = 'A';
            catState.vfoA = 14_250_000; // 20m
            catState.vfoB = 7_100_000;  // 40m
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('20m');
            expect(text).toContain('40m');
        });

        it('shows no band name for an out-of-band frequency', () => {
            catState.selectedVfo = 'A';
            catState.vfoA = 5_000_000;  // between 80m (3.5–4.0 MHz) and 60m (5.25–5.45 MHz) — out of band
            catState.vfoB = 7_100_000;  // 40m
            const { container } = render(Vfos);
            const text = container.textContent ?? '';

            expect(text).toContain('40m');     // vfoB still shows its band
            expect(text).not.toContain('20m'); // vfoA is out of band; no band text
            expect(text).not.toContain('60m');
            expect(text).not.toContain('80m');
        });
    });
});
