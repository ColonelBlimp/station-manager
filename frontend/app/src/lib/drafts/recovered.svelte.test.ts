// RS12–RS15: real QSO state, card, validators and enrichment; synthetic saved
// records and no daemon/hardware. Ordinary behavior is covered by the existing
// QSO/stack/keyboard tests. Restore remains unoffered (commit 4's boundary).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, cleanup } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from '../operate/LoggingCard.svelte';
import CallsignStackPanel from '../operate/CallsignStackPanel.svelte';
import SavedQsosControl from './SavedQsosControl.svelte';
import {
    draft,
    clearDraft,
    resetDraft,
    startQso,
    stampOn,
    stampOff,
    qsoClock,
    draftProblems,
    canLog,
    logDraft,
    setSubmit,
    installRecoveredDraft,
} from '../operate/qso.svelte';
import { rig, confirmRig, setCommandSender } from '../operate/rig.svelte';
import { callsignStack } from '../operate/callsignStack.svelte';
import { operate } from '../operate/state.svelte';
import { observeCall, setEnricher, type Enrichment } from '../operate/enrich.svelte';
import { sampleRecord } from './savedDraft.fixture';
import {
    recovered,
    correctRecoveredRig,
    confirmRecoveredRig,
    recoveredRigProblem,
} from './recovered.svelte';
import { _resetRestoreForTests, restoreSavedDraft } from './restoreSession';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { fakeDraftLocks } from './draftLock.fixture';
import { reserveSavedDraft } from './draftLock';
import { loadSavedDrafts, savedQsosPanel } from './savedDrafts.svelte';

beforeEach(() => {
    vi.useFakeTimers();
    rig.mode = 'USB';
    rig.freq = '7.100.000';
    rig.band = '40m';
    rig.cat = 'off';
    confirmRig();
    flushSync();
    clearDraft();
    callsignStack.clear();
});
afterEach(async () => {
    cleanup();
    await _resetRestoreForTests();
    clearDraft();
    _setDraftStoreForTests(null);
    vi.unstubAllGlobals();
    vi.useRealTimers();
    vi.restoreAllMocks();
});

describe('recovered form', () => {
    it('R1 saved fields, including empty end times, survive clock entry and all rig mode reports', () => {
        const record = sampleRecord();
        record.fields = {
            ...record.fields,
            name: 'Original name',
            qth: 'Original QTH',
            gridsquare: 'IO91',
            rig: 'Their rig',
            notes: 'Private note',
            rxPwr: '12.5',
        };
        startQso(); // a previously running ticker must be stopped by installation
        installRecoveredDraft(record);
        expect(qsoClock.ticking).toBe(false);
        for (const mode of ['CW', 'USB', 'FT4']) {
            rig.mode = mode;
            startQso();
            stampOn();
            stampOff();
            vi.advanceTimersByTime(2000);
            flushSync();
            expect(draft).toEqual(record.fields);
            expect(qsoClock.ticking).toBe(false);
        }
    });
    it('R2 report validation uses recovered CW/voice/SNR even with the opposite live mode', () => {
        const record = sampleRecord();
        record.rig = { ...record.rig, mode: 'CW', adifMode: 'CW', subMode: '' };
        record.fields.rstSent = '579';
        installRecoveredDraft(record);
        expect(draftProblems().rstSent).toBe(false);
        correctRecoveredRig({ adifMode: 'SSB', subMode: 'USB' });
        rig.mode = 'CW';
        expect(draftProblems().rstSent).toBe(true);
        correctRecoveredRig({ adifMode: 'MFSK', subMode: 'FT4' });
        draft.rstSent = '-12';
        expect(draftProblems().rstSent).toBe(false);
    });
    it.each([
        { freqHz: null },
        { freqHz: Number.NaN },
        { freqHz: 0 },
        { band: '' },
        { band: '40m' },
        { adifMode: '' },
        { adifMode: 'CW', subMode: 'USB' },
        { mode: 'CW' },
    ])('R3 missing/inconsistent saved reading cannot be confirmed: %j', (change) => {
        const record = sampleRecord();
        Object.assign(record.rig, change);
        installRecoveredDraft(record);
        expect(recoveredRigProblem()).not.toBeNull();
        expect(confirmRecoveredRig()).toBe(false);
        expect(recovered.confirmed).toBe(false);
        correctRecoveredRig({ freqHz: 14_255_000, band: '20m', adifMode: 'SSB', subMode: 'USB' });
        expect(recoveredRigProblem()).toBeNull();
        expect(confirmRecoveredRig()).toBe(true);
    });
    it.each([{ freqHz: 14_260_000 }, { band: '40m' }, { adifMode: 'CW' }, { subMode: 'LSB' }])(
        'R4 changing any recovered rig value withdraws confirmation: %j',
        (change) => {
            installRecoveredDraft(sampleRecord());
            expect(confirmRecoveredRig()).toBe(true);
            correctRecoveredRig(change);
            expect(recovered.confirmed).toBe(false);
        }
    );
    it('R5 the card shows the source and basis, corrects without rig commands, and gates Log', async () => {
        const send = vi.fn().mockResolvedValue({ accepted: true });
        setCommandSender(send);
        const submit = vi.fn().mockResolvedValue({ ok: true });
        setSubmit(submit);
        installRecoveredDraft(sampleRecord());
        render(LoggingCard);
        flushSync();
        expect(screen.getByText('Recovered QSO from ‘Home’')).toBeInTheDocument();
        expect(screen.getByLabelText('Recovered frequency (MHz)')).toHaveValue('14.255000');
        expect(screen.getByLabelText('Recovered band')).toHaveValue('20m');
        expect(screen.getByLabelText('Recovered mode')).toHaveValue('SSB');
        expect(screen.getByLabelText('Recovered submode')).toHaveValue('USB');
        expect(screen.getByText(/Last reading before the connection dropped/)).toHaveTextContent(
            '2026-09-30'
        );
        expect(screen.getByRole('button', { name: 'Log QSO' })).toBeDisabled();
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        expect(recovered.confirmed).toBe(true);
        await fireEvent.input(screen.getByLabelText('Recovered frequency (MHz)'), {
            target: { value: '14.260000' },
        });
        expect(recovered.confirmed).toBe(false);
        expect(recovered.rig?.freqHz).toBe(14_260_000);
        expect(rig.freq).toBe('7.100.000');
        expect(rig.band).toBe('40m');
        expect(send).not.toHaveBeenCalled();
        // Commit 2 is deliberately unable to use the ordinary submit seam,
        // including a forced log. Commit 4 supplies the recovered path.
        confirmRecoveredRig();
        expect(canLog()).toBe(false);
        expect(await logDraft()).toBe(false);
        expect(await logDraft(true)).toBe(false);
        expect(submit).not.toHaveBeenCalled();
    });
    it('R6 ordinary reset, stack shortcuts, stack button and pile-up Load keep recovered work', async () => {
        installRecoveredDraft(sampleRecord());
        render(LoggingCard);
        operate.callStack = true;
        render(CallsignStackPanel);
        callsignStack.push('ZS6BOS');
        callsignStack.push('DL3YA');
        flushSync();
        const before = { ...draft };
        const stack = [...callsignStack.items];
        clearDraft();
        expect(draft).toEqual(before);
        resetDraft();
        expect(draft).toEqual(before);
        await fireEvent.keyDown(screen.getByLabelText('Callsign'), {
            key: 'Enter',
            shiftKey: true,
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Stack callsign' }));
        await fireEvent.keyDown(window, { key: 'ArrowUp', shiftKey: true });
        await fireEvent.keyDown(window, { key: 'ArrowDown', shiftKey: true });
        expect(callsignStack.items).toEqual(stack);
        await fireEvent.click(screen.getByTitle(/^Load ZS6BOS/));
        expect(draft).toEqual(before);
        expect(callsignStack.items).toEqual(stack);
    });
    it('R9 malformed frequency stays visible until corrected and cannot be confirmed', async () => {
        installRecoveredDraft(sampleRecord());
        render(LoggingCard);
        flushSync();
        confirmRecoveredRig();
        await fireEvent.input(screen.getByLabelText('Recovered frequency (MHz)'), {
            target: { value: '14.' },
        });
        expect(screen.getByLabelText('Recovered frequency (MHz)')).toHaveValue('14.');
        expect(recovered.confirmed).toBe(false);
        expect(screen.getByRole('button', { name: 'Confirm recovered rig values' })).toBeDisabled();
    });
    it('R10 previous enrichment ownership cannot retract an identical saved value', async () => {
        observeCall('');
        setEnricher(() =>
            Promise.resolve({
                name: 'Bob',
                qth: 'Saved QTH',
                grid: 'IO91',
                country: '',
                ccode: '',
                dxcc: '',
                isNewEntity: null,
                email: '',
                cqZone: '',
                ituZone: '',
            })
        );
        draft.callsign = 'G0ABC';
        observeCall(draft.callsign);
        await vi.advanceTimersByTimeAsync(400);
        expect(draft.name).toBe('Bob');
        expect(draft.qth).toBe('Saved QTH');
        const record = sampleRecord();
        record.fields.qth = 'Saved QTH';
        record.fields.gridsquare = 'IO91';
        installRecoveredDraft(record);
        observeCall(''); // retracts old enrichment when the call is erased
        expect(draft).toEqual(record.fields);
    });
    it('R7 a late enrichment response preserves even saved blank fields', async () => {
        let finish!: (value: Enrichment | null) => void;
        setEnricher(
            () =>
                new Promise((resolve) => {
                    finish = resolve;
                })
        );
        observeCall('g0abc');
        await vi.advanceTimersByTimeAsync(400);
        installRecoveredDraft(sampleRecord());
        finish({
            name: 'New name',
            qth: 'New QTH',
            grid: 'JO22',
            country: 'Test',
            ccode: '',
            dxcc: '',
            isNewEntity: null,
            email: '',
            cqZone: '',
            ituZone: '',
        });
        await Promise.resolve();
        flushSync();
        expect(draft).toEqual(sampleRecord().fields);
    });
    it('R8 ownership and unconfirmed reading survive panel close and card unmount/remount', async () => {
        const mem = memoryDraftStore();
        await mem.put(sampleRecord());
        _setDraftStoreForTests(mem);
        const locks = fakeDraftLocks();
        vi.stubGlobal('navigator', { locks: locks.manager });
        expect(
            (
                await restoreSavedDraft(sampleRecord(), () => ({
                    locksAvailable: true,
                    onPhoneCw: true,
                    bootArchiveId: 'arch-a',
                    activeLogbookUuid: 'lb-a',
                    currentAttribution: sampleRecord().attribution,
                }))
            ).ok
        ).toBe(true);
        const card = render(LoggingCard);
        await loadSavedDrafts();
        render(SavedQsosControl);
        await fireEvent.click(screen.getByRole('button', { name: 'Unlogged QSOs (1)' }));
        expect(savedQsosPanel.open).toBe(true);
        expect(screen.queryByRole('button', { name: 'Restore' })).toBeNull();
        await fireEvent.click(screen.getByRole('button', { name: 'Close' }));
        expect(savedQsosPanel.open).toBe(false);
        correctRecoveredRig({ freqHz: 14_260_000 });
        card.unmount();
        await expect(reserveSavedDraft('d-1')).rejects.toThrow('In use in another tab');
        render(LoggingCard);
        flushSync();
        expect(screen.getByLabelText('Callsign')).toHaveValue('g0abc');
        expect(screen.getByLabelText('Recovered frequency (MHz)')).toHaveValue('14.260000');
        expect(recovered.confirmed).toBe(false);
        expect(locks.held.size).toBe(1);
    });
});
