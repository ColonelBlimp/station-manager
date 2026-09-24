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
    _resetArchivesForTests,
    _setReloadForTests,
} from '../config/archives.svelte';

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
