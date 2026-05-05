import { describe, it, expect, beforeEach } from 'vitest';
import { catState } from './cat.svelte';
import { manualState } from './manual.svelte';
import { configState } from './config.svelte';
import { bridgeState } from './bridge.svelte';
import { displayedState } from './displayed.svelte';

/**
 * Direct tests for the four-object decomposition's derived view
 * (`displayedState`). Vfos.test.ts exercises this indirectly via DOM
 * rendering; this file pins down the math/logic without going through
 * the DOM.
 *
 * The derivations under test (per ADR 0009):
 *
 *   isLive          = enabled && connected && rigResponding   (three-flag rule)
 *   editable        = !isLive
 *   vfoA / vfoB     = isLive ? catState.X : manualState.X
 *   mode / subMode  = isLive ? catState.X : manualState.X
 *   selectedVfo     = isLive ? catState.X : manualState.X
 *   rigIdentity     = isLive ? catState.rigIdentity : ''
 *   split           = isLive
 *                       ? (catState.splitOverride ?? false)
 *                       : (manualState.vfoA !== manualState.vfoB)
 *   rawPower        = isLive ? catState.power : manualState.power
 *   effectivePower  = ampEnabled ? rawPower * ampMultiplier : rawPower
 */

describe('displayedState (ADR 0009 four-object decomposition)', () => {
    beforeEach(() => {
        // Reset all four singletons to their structural defaults so each
        // test starts from a known baseline. This mirrors Vfos.test.ts's
        // beforeEach so the conventions stay aligned.
        try { localStorage.clear(); } catch { /* noop */ }

        catState.rigIdentity = '';
        catState.vfoA = 14_250_000;
        catState.vfoB = 14_250_000;
        catState.mode = 'USB';
        catState.subMode = '';
        catState.selectedVfo = 'A';
        catState.splitOverride = null;
        catState.power = 0;

        manualState.vfoA = 14_250_000;
        manualState.vfoB = 14_250_000;
        manualState.mode = 'USB';
        manualState.subMode = '';
        manualState.selectedVfo = 'A';
        manualState.power = 100;

        configState.station.enabled = false;
        configState.station.ampEnabled = false;
        configState.station.ampMultiplier = 1.0;

        bridgeState.connected = false;
        bridgeState.rigResponding = false;
    });

    describe('isLive (three-flag rule)', () => {
        it('is false when all three flags are false (default)', () => {
            expect(displayedState.isLive).toBe(false);
            expect(displayedState.editable).toBe(true);
        });

        it('is true only when all three flags are true', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            expect(displayedState.isLive).toBe(true);
            expect(displayedState.editable).toBe(false);
        });

        it('is false when only enabled is true', () => {
            configState.station.enabled = true;
            expect(displayedState.isLive).toBe(false);
        });

        it('is false when only connected is true', () => {
            bridgeState.connected = true;
            expect(displayedState.isLive).toBe(false);
        });

        it('is false when only rigResponding is true', () => {
            bridgeState.rigResponding = true;
            expect(displayedState.isLive).toBe(false);
        });

        it('is false when enabled+connected but not rigResponding (rig off)', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = false;
            expect(displayedState.isLive).toBe(false);
        });

        it('is false when enabled+rigResponding but not connected (transport down)', () => {
            configState.station.enabled = true;
            bridgeState.connected = false;
            bridgeState.rigResponding = true;
            expect(displayedState.isLive).toBe(false);
        });

        it('is false when connected+rigResponding but not enabled (CAT off in config)', () => {
            configState.station.enabled = false;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            expect(displayedState.isLive).toBe(false);
        });
    });

    describe('field source switching', () => {
        it('reads VFO/mode/subMode/selectedVfo from manualState when not live', () => {
            manualState.vfoA = 7_100_000;
            manualState.vfoB = 7_200_000;
            manualState.mode = 'CW';
            manualState.subMode = 'CW-N';
            manualState.selectedVfo = 'B';
            // catState is set to a different value; isLive=false so it
            // should be ignored.
            catState.vfoA = 21_000_000;
            catState.mode = 'USB';

            expect(displayedState.vfoA).toBe(7_100_000);
            expect(displayedState.vfoB).toBe(7_200_000);
            expect(displayedState.mode).toBe('CW');
            expect(displayedState.subMode).toBe('CW-N');
            expect(displayedState.selectedVfo).toBe('B');
        });

        it('reads VFO/mode/subMode/selectedVfo from catState when live', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;

            catState.vfoA = 21_000_000;
            catState.vfoB = 21_300_000;
            catState.mode = 'USB';
            catState.subMode = '';
            catState.selectedVfo = 'A';

            // manualState is set differently; isLive=true so it should
            // be ignored.
            manualState.vfoA = 7_100_000;

            expect(displayedState.vfoA).toBe(21_000_000);
            expect(displayedState.vfoB).toBe(21_300_000);
            expect(displayedState.mode).toBe('USB');
            expect(displayedState.selectedVfo).toBe('A');
        });

        it('rigIdentity reads from catState when live, empty string otherwise', () => {
            // Not live → empty regardless of catState
            catState.rigIdentity = 'IC-7300';
            expect(displayedState.rigIdentity).toBe('');

            // Live → reads catState
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            expect(displayedState.rigIdentity).toBe('IC-7300');
        });
    });

    describe('split derivation', () => {
        it('is false when not live and vfoA === vfoB (default)', () => {
            expect(displayedState.split).toBe(false);
        });

        it('is true when not live and vfoA !== vfoB', () => {
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 14_300_000;
            expect(displayedState.split).toBe(true);
        });

        it('is false when live and rig has not reported splitOverride', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            catState.splitOverride = null;
            expect(displayedState.split).toBe(false);
        });

        it('is true when live and rig reports splitOverride=true', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            catState.splitOverride = true;
            expect(displayedState.split).toBe(true);
        });

        it('is false when live and rig reports splitOverride=false even if VFOs differ', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            catState.vfoA = 14_250_000;
            catState.vfoB = 14_300_000;
            catState.splitOverride = false;
            // Rig says no split; trust the rig.
            expect(displayedState.split).toBe(false);
        });

        it('frequency-divergence rule does NOT apply in CAT-on mode', () => {
            // When live, only catState.splitOverride determines split.
            // manualState.vfoA/vfoB divergence is ignored.
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            catState.splitOverride = null;
            manualState.vfoA = 14_250_000;
            manualState.vfoB = 14_300_000; // diverges, but live mode ignores this
            expect(displayedState.split).toBe(false);
        });
    });

    describe('power and effectivePower', () => {
        it('rawPower reads from manualState when not live', () => {
            manualState.power = 75;
            catState.power = 999;
            expect(displayedState.rawPower).toBe(75);
        });

        it('rawPower reads from catState when live', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            catState.power = 100;
            manualState.power = 75;
            expect(displayedState.rawPower).toBe(100);
        });

        it('effectivePower passes raw power through when amp is disabled', () => {
            manualState.power = 100;
            configState.station.ampEnabled = false;
            configState.station.ampMultiplier = 10;
            expect(displayedState.effectivePower).toBe(100);
        });

        it('effectivePower applies the amp multiplier when enabled (200W amp on 100W rig)', () => {
            manualState.power = 100;
            configState.station.ampEnabled = true;
            configState.station.ampMultiplier = 2.0;
            expect(displayedState.effectivePower).toBe(200);
        });

        it('effectivePower applies the amp multiplier in CAT-live mode', () => {
            configState.station.enabled = true;
            bridgeState.connected = true;
            bridgeState.rigResponding = true;
            catState.power = 50;
            configState.station.ampEnabled = true;
            configState.station.ampMultiplier = 10.0;
            expect(displayedState.effectivePower).toBe(500);
        });

        it('effectivePower handles fractional multipliers when enabled', () => {
            manualState.power = 100;
            configState.station.ampEnabled = true;
            configState.station.ampMultiplier = 0.5;
            expect(displayedState.effectivePower).toBe(50);
        });

        it('effectivePower ignores ampMultiplier when ampEnabled is false', () => {
            manualState.power = 100;
            configState.station.ampEnabled = false;
            configState.station.ampMultiplier = 5;
            expect(displayedState.effectivePower).toBe(100);
        });
    });
});
