import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import {
    _activeSourceForTests,
    bridgeState,
    startBridge,
    stopBridge,
} from './bridge.svelte';
import { catState, DEFAULT_VFO_HZ, DEFAULT_MODE } from './cat.svelte';
import { configState } from './config.svelte';
import { _resetForTests as resetToasts, toastsState } from './toasts.svelte';

/**
 * Tests stub a synchronous EventSource: constructing the bridge.svelte
 * module makes `new EventSource(url)` resolve to this fake; tests then
 * drive `open` / `error` / typed events programmatically and assert
 * catState/bridgeState/toasts. Mirrors `internal/bridge/fake_serial_test.go`
 * in spirit — pure synchronous in-memory wiring keeps the tests
 * deterministic.
 */
class FakeEventSource {
    static instances: FakeEventSource[] = [];
    url: string;
    readyState: number = 0; // CONNECTING
    closed: boolean = false;
    private listeners: Map<string, Set<(ev: MessageEvent | Event) => void>> = new Map();

    constructor(url: string) {
        this.url = url;
        FakeEventSource.instances.push(this);
    }

    addEventListener(type: string, cb: (ev: MessageEvent | Event) => void): void {
        if (!this.listeners.has(type)) this.listeners.set(type, new Set());
        this.listeners.get(type)!.add(cb);
    }

    close(): void {
        this.closed = true;
        this.readyState = 2; // CLOSED
    }

    emit(type: string, data?: string): void {
        const set = this.listeners.get(type);
        if (!set) return;
        const ev: Event = data !== undefined ? new MessageEvent(type, { data }) : new Event(type);
        for (const cb of set) cb(ev);
    }

    fireOpen(): void {
        this.readyState = 1; // OPEN
        this.emit('open');
    }

    fireError(): void {
        this.emit('error');
    }
}

beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);

    // Reset shared singletons to defaults.
    bridgeState.connected = false;
    bridgeState.rigResponding = false;
    catState.rigIdentity = '';
    catState.vfoA = DEFAULT_VFO_HZ;
    catState.vfoB = DEFAULT_VFO_HZ;
    catState.mode = DEFAULT_MODE;
    catState.subMode = '';
    catState.selectedVfo = 'A';
    catState.splitOverride = null;
    catState.power = 0;
    configState.station.enabled = false;

    resetToasts();
});

afterEach(() => {
    stopBridge();
    vi.unstubAllGlobals();
});

function currentSource(): FakeEventSource {
    const src = _activeSourceForTests();
    if (!src) throw new Error('expected an active EventSource');
    return src as unknown as FakeEventSource;
}

describe('bridge SSE consumer — lifecycle', () => {
    it('does not open EventSource when station.enabled is false', () => {
        startBridge();
        flushSync();
        expect(FakeEventSource.instances).toHaveLength(0);
        expect(_activeSourceForTests()).toBeNull();
    });

    it('opens EventSource when station.enabled flips true', () => {
        startBridge();
        flushSync();
        configState.station.enabled = true;
        flushSync();
        expect(FakeEventSource.instances).toHaveLength(1);
        expect(FakeEventSource.instances[0].url).toBe('/v1/rig/events');
    });

    it('closes EventSource when station.enabled flips back to false', () => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
        const src = currentSource();
        configState.station.enabled = false;
        flushSync();
        expect(src.closed).toBe(true);
        expect(_activeSourceForTests()).toBeNull();
    });

    it('stopBridge tears down both the source and the effect.root', () => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
        const src = currentSource();
        stopBridge();
        expect(src.closed).toBe(true);
        expect(_activeSourceForTests()).toBeNull();

        // After stopBridge the effect.root is gone; re-enabling
        // should NOT open a new source.
        configState.station.enabled = false;
        configState.station.enabled = true;
        flushSync();
        expect(_activeSourceForTests()).toBeNull();
    });

    it('startBridge is idempotent', () => {
        startBridge();
        startBridge();
        configState.station.enabled = true;
        flushSync();
        expect(FakeEventSource.instances).toHaveLength(1);
    });
});

describe('bridge SSE consumer — connected flag', () => {
    beforeEach(() => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
    });

    it('flips connected=true on `open`', () => {
        expect(bridgeState.connected).toBe(false);
        currentSource().fireOpen();
        expect(bridgeState.connected).toBe(true);
    });

    it('flips connected=false on `error`', () => {
        const src = currentSource();
        src.fireOpen();
        expect(bridgeState.connected).toBe(true);
        src.fireError();
        expect(bridgeState.connected).toBe(false);
    });

    it('also clears rigResponding on transport error', () => {
        const src = currentSource();
        src.fireOpen();
        src.emit('rig-state', JSON.stringify({ vfoA: 14_200_000 }));
        expect(bridgeState.rigResponding).toBe(true);
        src.fireError();
        expect(bridgeState.rigResponding).toBe(false);
    });
});

describe('bridge SSE consumer — rig-state merge', () => {
    beforeEach(() => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
    });

    it('merges a full payload into catState', () => {
        currentSource().emit(
            'rig-state',
            JSON.stringify({
                rigIdentity: 'FT-710',
                vfoA: 14_250_000,
                vfoB: 14_260_000,
                mode: 'USB',
                subMode: '',
                selectedVfo: 'A',
                splitOverride: false,
                power: 100,
            }),
        );
        expect(catState.rigIdentity).toBe('FT-710');
        expect(catState.vfoA).toBe(14_250_000);
        expect(catState.vfoB).toBe(14_260_000);
        expect(catState.mode).toBe('USB');
        expect(catState.selectedVfo).toBe('A');
        expect(catState.splitOverride).toBe(false);
        expect(catState.power).toBe(100);
        expect(bridgeState.rigResponding).toBe(true);
    });

    it('partial payload preserves prior catState values', () => {
        // Seed catState with one round.
        currentSource().emit(
            'rig-state',
            JSON.stringify({ vfoA: 14_250_000, mode: 'USB' }),
        );
        expect(catState.vfoA).toBe(14_250_000);
        expect(catState.mode).toBe('USB');

        // Second event omits mode — value preserved.
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 21_200_000 }));
        expect(catState.vfoA).toBe(21_200_000);
        expect(catState.mode).toBe('USB');
    });

    it('omitted splitOverride leaves catState.splitOverride untouched', () => {
        // First push: splitOverride=true so we have a concrete value
        // to verify omission preserves.
        currentSource().emit('rig-state', JSON.stringify({ splitOverride: true }));
        expect(catState.splitOverride).toBe(true);

        // Second push omits splitOverride entirely.
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(catState.splitOverride).toBe(true);
    });

    it('splitOverride=false is preserved (not collapsed into omission)', () => {
        currentSource().emit('rig-state', JSON.stringify({ splitOverride: true }));
        expect(catState.splitOverride).toBe(true);

        // The wire-protocol-critical case: false must overwrite true.
        currentSource().emit('rig-state', JSON.stringify({ splitOverride: false }));
        expect(catState.splitOverride).toBe(false);
    });

    it('flips rigResponding=true on every rig-state event', () => {
        bridgeState.rigResponding = false;
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(bridgeState.rigResponding).toBe(true);
    });

    it('ignores invalid JSON without breaking the stream', () => {
        const src = currentSource();
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
        src.emit('rig-state', '{not valid json');
        expect(warnSpy).toHaveBeenCalled();
        expect(catState.vfoA).toBe(DEFAULT_VFO_HZ);
        // Next valid event still works.
        src.emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(catState.vfoA).toBe(14_250_000);
        warnSpy.mockRestore();
    });
});

describe('bridge SSE consumer — rig-disconnected', () => {
    beforeEach(() => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(bridgeState.rigResponding).toBe(true);
    });

    it('flips rigResponding=false and toasts at warn level (renders i18n template)', () => {
        currentSource().emit(
            'rig-disconnected',
            JSON.stringify({ code: 'rig_no_data' }),
        );
        expect(bridgeState.rigResponding).toBe(false);
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('warn');
        // Rendered from the en catalogue's bridge.disconnected.rig_no_data
        // template. The exact wording is in lib/i18n/en.ts and is
        // operator-tunable; the test pins a stable substring rather than
        // the full wording so a friendly retune doesn't break the test.
        expect(toastsState.items[0].message).toContain('rig has gone quiet');
    });

    it('leaves connected=true (transport still up; rig went away)', () => {
        expect(bridgeState.connected).toBe(true);
        currentSource().emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        expect(bridgeState.connected).toBe(true);
    });

    it('a subsequent rig-state event flips rigResponding back to true', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        expect(bridgeState.rigResponding).toBe(false);
        src.emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(bridgeState.rigResponding).toBe(true);
    });

    it('substitutes details into the template for serial_port_error', () => {
        currentSource().emit(
            'rig-disconnected',
            JSON.stringify({ code: 'serial_port_error', details: { error: 'i/o timeout' } }),
        );
        // Template: 'Lost the serial connection to the rig ({error})'
        expect(toastsState.items[0].message).toContain('i/o timeout');
    });
});

describe('bridge SSE consumer — bridge-error', () => {
    beforeEach(() => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
    });

    it('toasts at error level using the i18n template + substituted details', () => {
        currentSource().emit(
            'bridge-error',
            JSON.stringify({
                code: 'serial_open_failed',
                details: { port: '/dev/ttyUSB0', error: 'permission denied' },
            }),
        );
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('error');
        expect(toastsState.items[0].message).toContain('/dev/ttyUSB0');
        expect(toastsState.items[0].message).toContain('permission denied');
    });

    it('does not change bridgeState flags', () => {
        const connectedBefore = bridgeState.connected;
        const respondingBefore = bridgeState.rigResponding;
        currentSource().emit(
            'bridge-error',
            JSON.stringify({ code: 'unknown_driver', details: { driver: 'yaesu-foo' } }),
        );
        expect(bridgeState.connected).toBe(connectedBefore);
        expect(bridgeState.rigResponding).toBe(respondingBefore);
    });
});
