import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, cleanup, fireEvent } from '@testing-library/svelte';
import { tick } from 'svelte';
import QsoPanel from './QsoPanel.svelte';
import { qsoDraft, _disposeForTests } from '../../states/qsoDraft.svelte';
import { enrichmentState } from '../../states/enrichment.svelte';
import { contactHistoryState } from '../../states/contactHistory.svelte';
import { callsignStack } from '../../states/callsignStack.svelte';
import { manualState } from '../../states/manual.svelte';
import { configState } from '../../states/config.svelte';
import { bridgeState } from '../../states/bridge.svelte';
import { catState } from '../../states/cat.svelte';
import { submitQso, type SubmitOutcome } from '../../api/qso';
import { sendRigCommand } from '../../api/rigCommand';

/**
 * QsoPanel — in-flight submit guard (review 2026-06-04 M1).
 *
 * A QSO POST is a network round-trip. Without a guard, a double-click on
 * Log Contact (or a held Ctrl+Enter) fires two POSTs for the same draft
 * before the first resolves, leaving the daemon's dedupe as the only
 * thing between that and a duplicate row. These tests pin exactly one
 * POST per submit, the button disabling while in flight, and re-enabling
 * on resolve. The daemon transport (submitQso) is mocked with a deferred
 * promise so the test controls when the round-trip completes.
 */

vi.mock('../../api/qso', () => ({
    submitQso: vi.fn(),
}));
vi.mock('../../api/rigCommand', () => ({
    sendRigCommand: vi.fn().mockResolvedValue({ kind: 'ok' }),
}));

const mockSubmit = vi.mocked(submitQso);
const mockRigCommand = vi.mocked(sendRigCommand);

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => {
        resolve = r;
    });
    return { promise, resolve };
}

// A non-clearing outcome: the draft is preserved (canSubmit stays true),
// so the ONLY thing that re-enables the button is `submitting` flipping
// back to false in submitQso's finally — exactly what the re-enable test
// asserts. 'stored' would clear the draft and confound that signal.
const DUPLICATE: SubmitOutcome = { kind: 'duplicate', uuid: 'dup-uuid' };

function logButton(): HTMLButtonElement {
    const btn = document.getElementById('log-qso-btn') as HTMLButtonElement | null;
    if (btn === null) throw new Error('Log Contact button not rendered');
    return btn;
}

describe('QsoPanel — submit in-flight guard (M1)', () => {
    beforeEach(() => {
        mockSubmit.mockReset();
        qsoDraft.clear();
        enrichmentState.clear();
        contactHistoryState.clear();
        // A configured default logbook is required for submit (M1 guard);
        // a real session has one after config hydrates.
        configState.defaultLogbook.id = 1;
        // A valid callsign is all canSubmit needs — RST defaults to '59'
        // and date/time default to "now" at construction / clear().
        qsoDraft.callsign = 'M0XYZ';
    });

    afterEach(() => {
        cleanup();
        qsoDraft.clear();
        _disposeForTests();
    });

    it('fires exactly one POST when Log Contact is double-clicked', async () => {
        const d = deferred<SubmitOutcome>();
        mockSubmit.mockReturnValue(d.promise);

        render(QsoPanel);
        await tick();

        const btn = logButton();
        expect(btn.disabled).toBe(false);

        // First click: submitting flips true synchronously, button disables.
        await fireEvent.click(btn);
        await tick();
        expect(btn.disabled).toBe(true);

        // Second click lands on a disabled button → inert.
        await fireEvent.click(btn);
        await tick();

        expect(mockSubmit).toHaveBeenCalledTimes(1);

        d.resolve(DUPLICATE); // drain the in-flight promise
        await tick();
    });

    it('fires exactly one POST for repeated Ctrl+Enter while in flight', async () => {
        const d = deferred<SubmitOutcome>();
        mockSubmit.mockReturnValue(d.promise);

        render(QsoPanel);
        await tick();

        // Ctrl+Enter bypasses the button's disabled attribute, so this
        // exercises the internal `submitting` guard directly rather than
        // the button-disable belt.
        await fireEvent.keyDown(window, { key: 'Enter', ctrlKey: true });
        await fireEvent.keyDown(window, { key: 'Enter', ctrlKey: true });
        await tick();

        expect(mockSubmit).toHaveBeenCalledTimes(1);

        d.resolve(DUPLICATE);
        await tick();
    });

    it('re-enables Log Contact after the submit resolves', async () => {
        const d = deferred<SubmitOutcome>();
        mockSubmit.mockReturnValue(d.promise);

        render(QsoPanel);
        await tick();

        const btn = logButton();
        await fireEvent.click(btn);
        await tick();
        expect(btn.disabled).toBe(true);

        d.resolve(DUPLICATE);
        await tick();
        await tick();
        expect(btn.disabled).toBe(false);

        // A fresh submit now goes through (second deferred so it stays
        // pending and doesn't matter for the count assertion).
        const d2 = deferred<SubmitOutcome>();
        mockSubmit.mockReturnValue(d2.promise);
        await fireEvent.click(btn);
        await tick();
        expect(mockSubmit).toHaveBeenCalledTimes(2);

        d2.resolve(DUPLICATE);
        await tick();
    });
});

describe('QsoPanel — submits to the configured default logbook (M1)', () => {
    beforeEach(() => {
        mockSubmit.mockReset();
        qsoDraft.clear();
        enrichmentState.clear();
        contactHistoryState.clear();
        qsoDraft.callsign = 'M0XYZ';
    });

    afterEach(() => {
        cleanup();
        qsoDraft.clear();
        configState.defaultLogbook.id = 0;
        _disposeForTests();
    });

    it('posts the configured default_logbook id, not a hardcoded 1', async () => {
        configState.defaultLogbook.id = 7;
        const d = deferred<SubmitOutcome>();
        mockSubmit.mockReturnValue(d.promise);

        render(QsoPanel);
        await tick();
        await fireEvent.click(logButton());
        await tick();

        expect(mockSubmit).toHaveBeenCalledTimes(1);
        // submitQso(adif, logbookID, signal?) — assert the second arg is the
        // configured id.
        expect(mockSubmit).toHaveBeenCalledWith(expect.any(String), 7);

        d.resolve(DUPLICATE);
        await tick();
    });

    it('blocks submit (no POST) when no default logbook is configured', async () => {
        configState.defaultLogbook.id = 0;

        render(QsoPanel);
        await tick();
        await fireEvent.click(logButton());
        await tick();

        expect(mockSubmit).not.toHaveBeenCalled();
    });
});

/**
 * Frequency-step keys (CAT off → manualState): Shift+Ctrl+↑/↓ = ±100 Hz,
 * Shift+Ctrl+→/← = ±10 Hz. The handler matches on `e.code`, so the events set
 * it. Also pins that Shift+Ctrl+↑ does NOT fire the Shift+↑ stack-pop — the
 * stack handlers were widened with a `!ctrlKey && !metaKey` guard so the Ctrl
 * variant falls through to the rig block, and that a plain Shift+↑ still pops.
 */
describe('QsoPanel — frequency-step keys + stack-pop guard', () => {
    beforeEach(() => {
        mockSubmit.mockReset();
        qsoDraft.clear();
        callsignStack.clear();
        manualState.selectedVfo = 'A';
        manualState.vfoA = 14_074_000;
    });

    afterEach(() => {
        cleanup();
        qsoDraft.clear();
        callsignStack.clear();
        _disposeForTests();
    });

    it('Shift+Ctrl+ArrowUp = +100 Hz (coarse) and does not pop the stack', async () => {
        callsignStack.push('G3ABC');
        render(QsoPanel);
        await tick();

        await fireEvent.keyDown(window, {
            key: 'ArrowUp',
            code: 'ArrowUp',
            shiftKey: true,
            ctrlKey: true,
        });
        await tick();

        expect(manualState.vfoA).toBe(14_074_100); // +100 Hz
        expect(callsignStack.items).toEqual(['G3ABC']); // NOT popped
        expect(qsoDraft.callsign).toBe(''); // NOT loaded into the form
    });

    it('Shift+Ctrl+ArrowDown = -100 Hz (coarse)', async () => {
        render(QsoPanel);
        await tick();
        await fireEvent.keyDown(window, {
            key: 'ArrowDown',
            code: 'ArrowDown',
            shiftKey: true,
            ctrlKey: true,
        });
        await tick();
        expect(manualState.vfoA).toBe(14_073_900); // -100 Hz
    });

    it('Shift+Ctrl+ArrowRight = +10 Hz (fine), ArrowLeft = -10 Hz', async () => {
        render(QsoPanel);
        await tick();

        await fireEvent.keyDown(window, {
            key: 'ArrowRight',
            code: 'ArrowRight',
            shiftKey: true,
            ctrlKey: true,
        });
        await tick();
        expect(manualState.vfoA).toBe(14_074_010); // +10 Hz

        await fireEvent.keyDown(window, {
            key: 'ArrowLeft',
            code: 'ArrowLeft',
            shiftKey: true,
            ctrlKey: true,
        });
        await tick();
        expect(manualState.vfoA).toBe(14_074_000); // back down -10 Hz
    });

    it('plain Shift+ArrowUp still pops the stack (guard did not break it)', async () => {
        callsignStack.push('G3ABC');
        render(QsoPanel);
        await tick();

        await fireEvent.keyDown(window, { key: 'ArrowUp', code: 'ArrowUp', shiftKey: true });
        await tick();

        expect(callsignStack.items).toEqual([]); // popped
        expect(qsoDraft.callsign).toBe('G3ABC'); // loaded into the form
    });
});

/**
 * Mode dropdown — Option A: when CAT is live the dropdown shows the rig's OWN
 * mode literals (rigModes) and a pick drives the rig via set_mode. The log
 * still records ADIF (submit reads displayedState, not this control).
 */
describe('QsoPanel — live Mode dropdown drives the rig (Option A)', () => {
    beforeEach(() => {
        mockSubmit.mockReset();
        mockRigCommand.mockClear();
        mockRigCommand.mockResolvedValue({ kind: 'ok' });
        qsoDraft.clear();
        // CAT live + a rig that can set_mode, currently on USB.
        configState.station.enabled = true;
        bridgeState.connected = true;
        bridgeState.rigResponding = true;
        configState.bridge.ops = ['set_mode'];
        configState.bridge.rigModes = ['LSB', 'USB', 'CW-U', 'DATA-U'];
        catState.mode = 'USB';
    });

    afterEach(() => {
        cleanup();
        qsoDraft.clear();
        // Reset CAT to off so other suites start from the default.
        configState.station.enabled = false;
        bridgeState.connected = false;
        bridgeState.rigResponding = false;
        configState.bridge.ops = [];
        configState.bridge.rigModes = [];
        _disposeForTests();
    });

    function modeSelect(): HTMLSelectElement {
        const el = document.getElementById('mode') as HTMLSelectElement | null;
        if (el === null) throw new Error('Mode select not rendered');
        return el;
    }

    it('offers the rig mode list and reflects the rig literal, enabled', async () => {
        render(QsoPanel);
        await tick();
        const sel = modeSelect();
        expect(sel.disabled).toBe(false);
        expect([...sel.options].map((o) => o.value)).toEqual(['LSB', 'USB', 'CW-U', 'DATA-U']);
        expect(sel.value).toBe('USB');
    });

    it('drives set_mode with the chosen rig literal on change', async () => {
        render(QsoPanel);
        await tick();
        await fireEvent.change(modeSelect(), { target: { value: 'CW-U' } });
        await tick();
        expect(mockRigCommand).toHaveBeenCalledWith('set_mode', 'CW-U');
    });

    it('disables the dropdown when the live rig cannot set_mode', async () => {
        configState.bridge.ops = [];
        render(QsoPanel);
        await tick();
        expect(modeSelect().disabled).toBe(true);
    });
});
