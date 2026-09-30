import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import ArchiveSwitchGate from './ArchiveSwitchGate.svelte';
import { ft8State, setFt8TxActions, resetFt8ForTests } from '../operate/ft8.svelte';
import { rig, setTuneSender } from '../operate/rig.svelte';
import {
    archivesState,
    setDraftPreserver,
    _resetArchivesForTests,
    _setReloadForTests,
} from '../config/archives.svelte';
import { sampleRecord } from '../drafts/savedDraft.fixture';
import { toastsState, _resetForTests as resetToasts } from './toasts.svelte';

// The unresolved-switch gate covers every view and offers exactly one way out.
beforeEach(() => _resetArchivesForTests());
afterEach(() => {
    vi.restoreAllMocks();
    resetFt8ForTests();
    rig.tuneActive = false;
});

describe('ArchiveSwitchGate', () => {
    it('is absent while no switch is unresolved', () => {
        render(ArchiveSwitchGate);
        expect(screen.queryByRole('alertdialog')).toBeNull();
    });

    it('blocks with the detail and reloads on its one control', async () => {
        let reloads = 0;
        _setReloadForTests(() => reloads++);
        archivesState.switchUnresolved = true;
        archivesState.switchDetail = 'The daemon did not answer as a new instance within the wait.';
        render(ArchiveSwitchGate);
        flushSync();
        const dialog = screen.getByRole('alertdialog');
        expect(dialog).toHaveTextContent('Archive binding unproven');
        expect(dialog).toHaveTextContent('did not answer as a new instance');
        await fireEvent.click(screen.getByRole('button', { name: 'Reload now' }));
        expect(reloads).toBe(1);
    });
});

// The stop paths stay reachable through the gate (review P1): each is offered
// only while it can be acted on, and reaches its seam although the rest of the
// app is inert.
describe('ArchiveSwitchGate stop controls', () => {
    it('offers no stop control while nothing is keyed', () => {
        archivesState.switchUnresolved = true;
        render(ArchiveSwitchGate);
        flushSync();
        expect(screen.queryByRole('button', { name: 'Disable FT8 TX' })).toBeNull();
        expect(screen.queryByRole('button', { name: 'Stop tune' })).toBeNull();
    });

    it('Disable FT8 TX reaches the disarm seam while FT8 is armed', async () => {
        const armed: boolean[] = [];
        const ok = Promise.resolve({ ok: true, message: '' });
        setFt8TxActions({
            arm: (a) => (armed.push(a), Promise.resolve({ kind: 'accepted' as const })),
            callCq: () => ok,
            answerCq: () => ok,
            workCaller: () => ok,
            abandon: () => ok,
            next: () => ok,
            stopAutoWork: () => ok,
            pickAnswerer: () => ok,
            bagAnswerer: () => ok,
            unbagAnswerer: () => ok,
            resumeDrain: () => ok,
            skip: () => ok,
        });
        ft8State.tx.armed = true;
        archivesState.switchUnresolved = true;
        render(ArchiveSwitchGate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Disable FT8 TX' }));
        await new Promise((r) => setTimeout(r, 0));
        expect(armed).toEqual([false]);
    });

    it('Stop tune reaches the tune seam while the carrier is keyed', async () => {
        const sent: boolean[] = [];
        setTuneSender((active) => {
            sent.push(active);
            return Promise.resolve({ kind: 'accepted' });
        });
        rig.tuneActive = true;
        archivesState.switchUnresolved = true;
        render(ArchiveSwitchGate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Stop tune' }));
        await new Promise((r) => setTimeout(r, 0));
        expect(sent).toEqual([false]);
    });
});

// An unconfirmed stop (timed out, no confirming push within the grace) is said
// on the gate surface: the transmission may still be up (review P2).
describe('ArchiveSwitchGate unconfirmed stops', () => {
    beforeEach(() => vi.useFakeTimers());
    afterEach(() => vi.useRealTimers());

    it('an unconfirmed FT8 disable is reported on the gate and the control stays usable', async () => {
        const ok = Promise.resolve({ ok: true, message: '' });
        setFt8TxActions({
            arm: () => Promise.resolve({ kind: 'timedOut' as const, message: 'request timed out' }),
            callCq: () => ok,
            answerCq: () => ok,
            workCaller: () => ok,
            abandon: () => ok,
            next: () => ok,
            stopAutoWork: () => ok,
            pickAnswerer: () => ok,
            bagAnswerer: () => ok,
            unbagAnswerer: () => ok,
            resumeDrain: () => ok,
            skip: () => ok,
        });
        ft8State.tx.armed = true;
        archivesState.switchUnresolved = true;
        render(ArchiveSwitchGate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Disable FT8 TX' }));
        await vi.advanceTimersByTimeAsync(2000); // the confirm grace, no matching push
        flushSync();
        expect(screen.getByTestId('stop-note')).toHaveTextContent(/confirm|unknown|still/i);
        expect(screen.getByRole('button', { name: 'Disable FT8 TX' })).toBeEnabled();
    });

    it('an unconfirmed tune stop is reported on the gate', async () => {
        setTuneSender(() => Promise.resolve({ kind: 'timedOut', message: 'request timed out' }));
        rig.tuneActive = true;
        archivesState.switchUnresolved = true;
        render(ArchiveSwitchGate);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: 'Stop tune' }));
        await vi.advanceTimersByTimeAsync(2000);
        flushSync();
        expect(screen.getByTestId('stop-note').textContent).not.toBe('');
    });
});

/*
    A HELD reload (ADR 0085, rule 3 slice 1; operator ruling 2026-09-30): the
    unlogged QSO could not be saved before the rebind reload.
      G1  The gate says so, gives the reason, and shows EVERY value as text to
          select — the inert form behind it is not enough. Reload now is not
          offered: its way out would lose the QSO.
      G2  Retry save goes through the save again; success reloads.
      G3  Discard and reload is explicit and confirmed; declining keeps the page.
      G4  Copy puts the whole QSO on the clipboard; a refused clipboard says to
          select the text instead.
      G5  The stop controls stay reachable.
      G6  An unknown logging outcome stays visible, and neither the gate nor
          the copied text claims the QSO was saved (review 2026-09-30).
*/
describe('ArchiveSwitchGate with a held reload', () => {
    let reloads = 0;
    beforeEach(() => {
        reloads = 0;
        _setReloadForTests(() => reloads++);
        resetToasts();
        archivesState.switchUnresolved = true;
        archivesState.switchDetail = 'The daemon now serves another archive.';
        archivesState.saveFailed = {
            reason: 'Browser storage did not keep it (QuotaExceededError).',
            record: sampleRecord(),
        };
    });
    afterEach(() => setDraftPreserver(null));

    it('G1 says the QSO could not be saved and shows every value', () => {
        render(ArchiveSwitchGate);
        const dialog = screen.getByRole('alertdialog');
        expect(dialog).toHaveTextContent('This QSO could not be saved');
        expect(dialog).toHaveTextContent('QuotaExceededError');
        const details = screen.getByTestId('saved-draft-details');
        expect(details).toHaveTextContent('G0ABC');
        expect(details).toHaveTextContent('14.255000 MHz');
        expect(details).toHaveTextContent('Home log');
        expect(screen.queryByRole('button', { name: 'Reload now' })).toBeNull();
    });

    it('G2 Retry save saves again and reloads on success', async () => {
        const preserver = vi.fn(() =>
            Promise.resolve({ kind: 'saved' as const, record: sampleRecord() })
        );
        setDraftPreserver(preserver);
        render(ArchiveSwitchGate);
        await fireEvent.click(screen.getByRole('button', { name: 'Retry save' }));
        await vi.waitFor(() => expect(reloads).toBe(1));
        expect(preserver).toHaveBeenCalledTimes(1);
    });

    it('G3 Discard and reload is confirmed', async () => {
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        render(ArchiveSwitchGate);
        const discard = screen.getByRole('button', { name: 'Discard and reload' });
        await fireEvent.click(discard);
        expect(confirm).toHaveBeenCalledTimes(1);
        expect(reloads).toBe(0);
        confirm.mockReturnValue(true);
        await fireEvent.click(discard);
        expect(reloads).toBe(1);
    });

    it('G4 Copy writes the whole QSO; a refused clipboard says to select it', async () => {
        const writeText = vi.fn(() => Promise.resolve());
        Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
        render(ArchiveSwitchGate);
        await fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
        await vi.waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
        expect(writeText.mock.calls[0]).toEqual([expect.stringContaining('Callsign: G0ABC')]);
        writeText.mockRejectedValueOnce(new Error('NotAllowedError'));
        await fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
        await vi.waitFor(() =>
            expect(toastsState.items.some((t) => /select the text/.test(t.message))).toBe(true)
        );
    });

    it('G5 the stop controls stay reachable', () => {
        rig.tuneActive = true;
        render(ArchiveSwitchGate);
        expect(screen.getByRole('button', { name: 'Stop tune' })).toBeInTheDocument();
    });

    it('G6 the unknown outcome is visible and nothing claims a save', async () => {
        archivesState.saveFailed = {
            reason: 'Browser storage did not keep it (QuotaExceededError).',
            record: sampleRecord({ outcome: 'unknown' }),
        };
        const writeText = vi.fn(() => Promise.resolve());
        Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
        render(ArchiveSwitchGate);
        const dialog = screen.getByRole('alertdialog');
        expect(dialog).toHaveTextContent(/logging outcome unknown/i);
        expect(dialog).not.toHaveTextContent(/saved from/);
        expect(screen.getByTestId('saved-draft-details')).not.toHaveTextContent(/Saved/);
        await fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
        await vi.waitFor(() => expect(writeText).toHaveBeenCalledTimes(1));
        const copied = (writeText.mock.calls[0] as unknown as [string])[0];
        expect(copied).not.toContain('QSO saved from');
        expect(copied).toMatch(/not saved/);
        expect(copied).toMatch(/logging outcome unknown/i);
    });
});
