import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { _activeSourceForTests, bridgeState, startBridge, stopBridge } from './bridge.svelte';
import { catState, DEFAULT_VFO_HZ, DEFAULT_MODE } from './cat.svelte';
import { manualState } from './manual.svelte';
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
    bridgeState.tuneActive = false;
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
            })
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
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_250_000, mode: 'USB' }));
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
    // The disconnect handler schedules the warn toast 800ms in the
    // future to suppress the flash UX when rig-state arrives close
    // behind rig-disconnected. Use fake timers so tests can deterministically
    // step past the suppression window when they want to assert on
    // the visible toast.
    beforeEach(() => {
        vi.useFakeTimers();
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(bridgeState.rigResponding).toBe(true);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('defers rigResponding=false to the suppression window, with the warn toast', () => {
        currentSource().emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        // Deferred: rigResponding (and thus isLive) stays true immediately,
        // so the CAT fields don't flicker on a blip the probe will recover.
        expect(bridgeState.rigResponding).toBe(true);
        expect(toastsState.items).toHaveLength(0);

        // No recovery within the window → the flip + warn fire together.
        vi.advanceTimersByTime(800);
        expect(bridgeState.rigResponding).toBe(false);
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('warn');
        // ttl=0 — sticky until reconnect (or operator manual dismiss).
        expect(toastsState.items[0].ttl).toBe(0);
        expect(toastsState.items[0].message).toContain('rig has gone quiet');
    });

    it('leaves connected=true (transport still up; rig went away)', () => {
        expect(bridgeState.connected).toBe(true);
        currentSource().emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        expect(bridgeState.connected).toBe(true);
    });

    it('keeps rigResponding=true when rig-state recovers within the window (no flicker)', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        expect(bridgeState.rigResponding).toBe(true); // deferred, not yet flipped
        vi.advanceTimersByTime(200);
        src.emit('rig-state', JSON.stringify({ vfoA: 14_250_000 })); // probe recovers
        expect(bridgeState.rigResponding).toBe(true);
        // Past the would-be flip moment: still true (the timer was cancelled).
        vi.advanceTimersByTime(1000);
        expect(bridgeState.rigResponding).toBe(true);
        expect(toastsState.items).toHaveLength(0);
    });

    it('flips rigResponding=false then back to true across a genuine outage', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        vi.advanceTimersByTime(800); // no recovery in window → flip fires
        expect(bridgeState.rigResponding).toBe(false);
        src.emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(bridgeState.rigResponding).toBe(true);
    });

    it('substitutes details into the template for serial_port_error (after suppression window)', () => {
        currentSource().emit(
            'rig-disconnected',
            JSON.stringify({ code: 'serial_port_error', details: { error: 'i/o timeout' } })
        );
        vi.advanceTimersByTime(800);
        // Template: 'Lost the serial connection to the rig ({error})'
        expect(toastsState.items[0].message).toContain('i/o timeout');
    });
});

describe('bridge SSE consumer — implicit reconnect + flash suppression', () => {
    beforeEach(() => {
        vi.useFakeTimers();
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        expect(bridgeState.rigResponding).toBe(true);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('full disconnect cycle: after suppression window, rig-state replaces warn with info', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        // Past the suppression window — warn is visible.
        vi.advanceTimersByTime(800);
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('warn');

        src.emit('rig-state', JSON.stringify({ vfoA: 14_300_000 }));
        // Warn dismissed, info reconnect surfaced.
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('info');
        expect(toastsState.items[0].message.toLowerCase()).toContain('reconnected');
    });

    it('SUPPRESSES the flash when rig-state arrives within the suppression window', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        // Inside the suppression window — no toast pushed yet.
        vi.advanceTimersByTime(200);
        expect(toastsState.items).toHaveLength(0);

        // Rig recovers fast — scheduled warn is cancelled, no
        // reconnect info either (no visible disconnect to "reconnect
        // from"). End result: no UI churn at all.
        src.emit('rig-state', JSON.stringify({ vfoA: 14_300_000 }));
        expect(toastsState.items).toHaveLength(0);

        // Advancing past what would have been the original push
        // moment confirms the timer was cancelled, not just deferred.
        vi.advanceTimersByTime(1000);
        expect(toastsState.items).toHaveLength(0);
    });

    it('does not re-toast on subsequent steady-state rig-state events', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        vi.advanceTimersByTime(800);
        src.emit('rig-state', JSON.stringify({ vfoA: 14_300_000 }));
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('info');

        src.emit('rig-state', JSON.stringify({ vfoA: 14_310_000 }));
        src.emit('rig-state', JSON.stringify({ vfoA: 14_320_000 }));
        src.emit('rig-state', JSON.stringify({ mode: 'USB' }));
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('info');
    });

    it('does not emit a reconnect toast on a rig-state with no prior disconnect', () => {
        const baseCount = toastsState.items.length;
        currentSource().emit('rig-state', JSON.stringify({ vfoA: 14_999_999 }));
        expect(toastsState.items.length).toBe(baseCount);
    });

    it('latest disconnect wins when a new disconnect replaces a pending one', () => {
        const src = currentSource();
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        // Inside suppression — first disconnect not yet visible.
        vi.advanceTimersByTime(200);
        expect(toastsState.items).toHaveLength(0);

        // Second disconnect arrives with a different code — cancels
        // the first scheduled push, schedules its own.
        src.emit(
            'rig-disconnected',
            JSON.stringify({ code: 'serial_port_error', details: { error: 'i/o timeout' } })
        );
        vi.advanceTimersByTime(800);
        expect(toastsState.items).toHaveLength(1);
        // The visible toast carries the LATER disconnect's message,
        // not the earlier one.
        expect(toastsState.items[0].message).toContain('i/o timeout');
    });

    it('dismisses the visible sticky warn toast when CAT is disabled mid-disconnect (regression M1)', () => {
        const src = currentSource();
        // Drive into state C: a disconnect has fired and the
        // suppression window has elapsed, so the sticky warn toast is
        // on screen.
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        vi.advanceTimersByTime(800);
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('warn');
        expect(toastsState.items[0].ttl).toBe(0); // sticky — never auto-expires

        // Operator disables CAT (or any other path that calls
        // closeSource): the sticky warn must be dismissed, not just
        // dropped from the module-local tracker. Without the dismiss
        // the toast would persist forever in the queue with no way
        // for the operator to clear it short of a page reload.
        configState.station.enabled = false;
        flushSync();
        expect(toastsState.items).toHaveLength(0);
    });

    it('handles disconnect → reconnect → disconnect → reconnect cycles', () => {
        const src = currentSource();

        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        vi.advanceTimersByTime(800);
        src.emit('rig-state', JSON.stringify({ vfoA: 14_300_000 }));
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0].level).toBe('info');

        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        vi.advanceTimersByTime(800);
        // Two items now: the prior info toast (kept) + the new warn.
        expect(toastsState.items).toHaveLength(2);
        expect(toastsState.items[toastsState.items.length - 1].level).toBe('warn');

        src.emit('rig-state', JSON.stringify({ vfoA: 14_310_000 }));
        // Warn dismissed, second info added — both items are info now.
        expect(toastsState.items).toHaveLength(2);
        expect(toastsState.items.every((it) => it.level === 'info')).toBe(true);
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
            })
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
            JSON.stringify({ code: 'unknown_driver', details: { driver: 'yaesu-foo' } })
        );
        expect(bridgeState.connected).toBe(connectedBefore);
        expect(bridgeState.rigResponding).toBe(respondingBefore);
    });
});

describe('bridge SSE consumer — tune-state', () => {
    beforeEach(() => {
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
    });

    it('sets tuneActive=true on a tune-state active event', () => {
        expect(bridgeState.tuneActive).toBe(false);
        currentSource().emit('tune-state', JSON.stringify({ active: true }));
        expect(bridgeState.tuneActive).toBe(true);
    });

    it('clears tuneActive on a tune-state inactive event (e.g. daemon auto-off)', () => {
        const src = currentSource();
        src.emit('tune-state', JSON.stringify({ active: true }));
        expect(bridgeState.tuneActive).toBe(true);
        // The daemon's hard auto-off / disconnect-release publishes inactive;
        // the button reflects it without the operator clicking.
        src.emit('tune-state', JSON.stringify({ active: false }));
        expect(bridgeState.tuneActive).toBe(false);
    });

    it('does not change connected / rigResponding', () => {
        const src = currentSource();
        src.emit('rig-state', JSON.stringify({ vfoA: 14_250_000 }));
        const connectedBefore = bridgeState.connected;
        const respondingBefore = bridgeState.rigResponding;
        src.emit('tune-state', JSON.stringify({ active: true }));
        expect(bridgeState.connected).toBe(connectedBefore);
        expect(bridgeState.rigResponding).toBe(respondingBefore);
    });

    it('ignores invalid JSON without breaking the stream', () => {
        const src = currentSource();
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
        src.emit('tune-state', '{not valid json');
        expect(warnSpy).toHaveBeenCalled();
        expect(bridgeState.tuneActive).toBe(false);
        warnSpy.mockRestore();
    });

    it('resets tuneActive=false when CAT is disabled (deliberate teardown)', () => {
        currentSource().emit('tune-state', JSON.stringify({ active: true }));
        expect(bridgeState.tuneActive).toBe(true);
        configState.station.enabled = false;
        flushSync();
        expect(bridgeState.tuneActive).toBe(false);
    });

    it('does NOT reset tuneActive on a transport error (daemon stays authoritative)', () => {
        const src = currentSource();
        src.emit('tune-state', JSON.stringify({ active: true }));
        expect(bridgeState.tuneActive).toBe(true);
        // A transport blip → browser auto-reconnects and the daemon replays the
        // cached tune-state; the SPA must not optimistically clear it here.
        src.fireError();
        expect(bridgeState.tuneActive).toBe(true);
    });
});

describe('bridge SSE consumer — disconnect snapshot (M2)', () => {
    // On an involuntary disconnect, manualState should adopt catState's
    // last-known values so displayedState's CAT-off fallback shows continuity
    // rather than stale localStorage/defaults (the rule manual.svelte.ts
    // documents). manualState is seeded with sentinels distinct from the rig
    // values each test pushes, so a snapshot (or its absence) is unambiguous.
    beforeEach(() => {
        vi.useFakeTimers();
        manualState.vfoA = 1_000_000;
        manualState.vfoB = 2_000_000;
        manualState.mode = 'FM';
        manualState.subMode = 'SENTINEL';
        manualState.selectedVfo = 'B';
        configState.bridge.modeMappings = {};
        startBridge();
        configState.station.enabled = true;
        flushSync();
        currentSource().fireOpen();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('snapshots the last-known rig state into manualState on transport error', () => {
        const src = currentSource();
        src.emit(
            'rig-state',
            JSON.stringify({ vfoA: 14_200_000, vfoB: 14_210_000, mode: 'USB', selectedVfo: 'A' })
        );
        expect(bridgeState.rigResponding).toBe(true);
        src.fireError();
        expect(manualState.vfoA).toBe(14_200_000);
        expect(manualState.vfoB).toBe(14_210_000);
        expect(manualState.selectedVfo).toBe('A');
        // No mappings → the rig literal passes through as the friendly value;
        // subMode is reset (manualState's friendly model holds mode only).
        expect(manualState.mode).toBe('USB');
        expect(manualState.subMode).toBe('');
    });

    it('stores mode in operator-friendly form via the mode mappings', () => {
        // The rig pushes a data literal the mapping resolves to {SSB, USB};
        // the snapshot must store the FRIENDLY "USB", not the literal or "SSB".
        configState.bridge.modeMappings = { 'DATA-U': { mode: 'SSB', submode: 'USB' } };
        const src = currentSource();
        src.emit('rig-state', JSON.stringify({ vfoA: 14_074_000, mode: 'DATA-U' }));
        src.fireError();
        expect(manualState.mode).toBe('USB');
        expect(manualState.subMode).toBe('');
    });

    it('snapshots on a genuine outage (disconnect with no recovery in the window)', () => {
        const src = currentSource();
        src.emit('rig-state', JSON.stringify({ vfoA: 21_300_000, selectedVfo: 'A', mode: 'CW' }));
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        // Deferred — not yet snapshotted (the blip might recover).
        expect(manualState.vfoA).toBe(1_000_000);
        vi.advanceTimersByTime(800);
        expect(bridgeState.rigResponding).toBe(false);
        expect(manualState.vfoA).toBe(21_300_000);
        expect(manualState.selectedVfo).toBe('A');
        expect(manualState.mode).toBe('CW');
    });

    it('does NOT snapshot when the rig recovers within the suppression window', () => {
        const src = currentSource();
        src.emit('rig-state', JSON.stringify({ vfoA: 21_300_000 }));
        src.emit('rig-disconnected', JSON.stringify({ code: 'rig_no_data' }));
        vi.advanceTimersByTime(200);
        src.emit('rig-state', JSON.stringify({ vfoA: 21_310_000 })); // recovers
        vi.advanceTimersByTime(1000);
        // The timer was cancelled → snapshot never fired → manualState untouched.
        expect(manualState.vfoA).toBe(1_000_000);
        expect(manualState.selectedVfo).toBe('B');
        expect(manualState.mode).toBe('FM');
    });

    it('does NOT clobber manualState on a transport error before any rig-state', () => {
        // rigResponding is false (no rig-state yet) → the guard skips the
        // snapshot, so the operator's manual edits survive a dead-rig error
        // rather than being overwritten with default catState.
        expect(bridgeState.rigResponding).toBe(false);
        currentSource().fireError();
        expect(manualState.vfoA).toBe(1_000_000);
        expect(manualState.vfoB).toBe(2_000_000);
        expect(manualState.selectedVfo).toBe('B');
        expect(manualState.mode).toBe('FM');
    });
});
