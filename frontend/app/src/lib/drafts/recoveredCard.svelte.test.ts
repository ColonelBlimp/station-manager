/*
    The card's Escape and Clear on a recovered QSO (ADR 0085 RS17): they keep
    the last edit, then let the QSO go; a failed save keeps it on the card and
    says so. The capture time reads as UTC, as in the saved-QSO details.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import LoggingCard from '../operate/LoggingCard.svelte';
import { draft, clearDraft } from '../operate/qso.svelte';
import { rig, confirmRig } from '../operate/rig.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { _setDraftChannelForTests } from './draftChannel';
import { sampleRecord } from './savedDraft.fixture';
import { fakeDraftLocks } from './draftLock.fixture';
import { recovered } from './recovered.svelte';
import { restoreSavedDraft, _resetRestoreForTests } from './restoreSession';
import { _resetRecoveredSaveForTests } from './recoveredSave.svelte';

let mem = memoryDraftStore();
let locks = fakeDraftLocks();

beforeEach(async () => {
    mem = memoryDraftStore();
    await mem.put(sampleRecord());
    _setDraftStoreForTests(mem);
    _setDraftChannelForTests({ post() {}, subscribe: () => () => {} });
    locks = fakeDraftLocks();
    vi.stubGlobal('navigator', { locks: locks.manager });
    rig.mode = 'USB';
    rig.cat = 'off';
    confirmRig();
    flushSync();
    clearDraft();
    _resetRecoveredSaveForTests();
    const env = {
        locksAvailable: true,
        onPhoneCw: true,
        bootArchiveId: 'arch-a',
        activeLogbookUuid: 'lb-a',
        currentAttribution: sampleRecord().attribution,
    };
    expect((await restoreSavedDraft(sampleRecord(), () => env)).ok).toBe(true);
    flushSync();
});
afterEach(async () => {
    _resetRecoveredSaveForTests();
    await _resetRestoreForTests();
    clearDraft();
    _setDraftStoreForTests(null);
    _setDraftChannelForTests(null);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

const escape = (): void => {
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    flushSync();
};

describe('the recovered card', () => {
    it('Escape keeps the last edit, then lets the QSO go', async () => {
        render(LoggingCard);
        draft.notes = 'last words';
        flushSync();
        escape();
        await vi.waitFor(() => expect(recovered.record).toBeNull());
        expect(mem.rows.get('d-1')?.fields.notes).toBe('last words');
        expect(draft.callsign).toBe('');
        expect(locks.held.size).toBe(0);
    });

    it('Clear with a failing save keeps the QSO and says so', async () => {
        vi.spyOn(mem, 'put').mockRejectedValue(new Error('storage busy'));
        render(LoggingCard);
        draft.notes = 'unsaved';
        flushSync();
        await fireEvent.click(screen.getAllByRole('button', { name: 'Clear' })[0]);
        expect(await screen.findByTestId('recovered-save-error')).toHaveTextContent('storage busy');
        expect(recovered.record).not.toBeNull();
        expect(draft.notes).toBe('unsaved');
        expect(locks.held.size).toBe(1);
    });

    it('shows the capture time as UTC', () => {
        render(LoggingCard);
        expect(screen.getByText(/2026-09-30 12:05:00 UTC/)).toBeInTheDocument();
    });

    it('a saved correction shows its original reading beside it', async () => {
        await _resetRestoreForTests();
        const corrected = sampleRecord({
            rigCorrection: { ...sampleRecord().rig, freqHz: 14_200_000 },
        });
        await mem.put(corrected);
        const env = {
            locksAvailable: true,
            onPhoneCw: true,
            bootArchiveId: 'arch-a',
            activeLogbookUuid: 'lb-a',
            currentAttribution: sampleRecord().attribution,
        };
        expect((await restoreSavedDraft(corrected, () => env)).ok).toBe(true);
        render(LoggingCard);
        expect(screen.getByTestId('recovered-original')).toHaveTextContent('14.255000 MHz');
        expect(screen.getByLabelText(/Recovered frequency/)).toHaveValue('14.200000');
        expect(recovered.confirmed).toBe(false);
    });
});
