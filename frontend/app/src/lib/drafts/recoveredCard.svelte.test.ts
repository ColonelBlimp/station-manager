/*
    The card's Escape and Clear on a recovered QSO (ADR 0085 RS17): they keep
    the last edit, then let the QSO go; a failed save keeps it on the card and
    says so. The capture time reads as UTC, as in the saved-QSO details.

    Logging it from the card (RS18–RS25, Restore commit 4):
      LC1 Log and Ctrl+Enter take the recovered path — never the ordinary
          assembly — and CAT lost does not disable them.
      LC2 Until the rig values are confirmed Log is disabled and says why.
      LC3 A duplicate keeps the form, names the existing QSO, offers it for
          inspection, and "Log anyway" is an explicit, forced separate contact;
          Ctrl+Enter never forces.
      LC4 An unknown-outcome record asks the RS24 question; Cancel sends nothing.
      LC5 Confirmed but both cleanup writes fail: the UUID is shown, Log is
          disabled, and Retry cleanup cleans without sending.
      LC6 An ambiguous outcome says the outcome is unknown.
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
import { setSubmit } from '../operate/qso.svelte';
import {
    setRecoveredSender,
    setRecoveredSubmitEnv,
    UNKNOWN_CONFIRM,
    _resetRecoveredSubmitForTests,
    type RecoveredSendOptions,
} from './recoveredSubmit.svelte';
import type { SubmitOutcome } from '../api/qso';
import { hideTile, isVisible, resetToDefault } from '../operate/layout.svelte';

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
    _resetRecoveredSubmitForTests();
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

describe('logging the recovered QSO from the card', () => {
    const sent: Array<{ adif: string; opts: RecoveredSendOptions }> = [];
    let answer: () => Promise<SubmitOutcome> = () =>
        Promise.resolve({ kind: 'stored', uuid: 'qso-9' });
    const ordinary = vi.fn();
    const ctrlEnter = (): void => {
        window.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true })
        );
        flushSync();
    };
    const logButton = (): HTMLButtonElement =>
        screen.getAllByRole('button', { name: /Log QSO|Logging…/ })[0] as HTMLButtonElement;

    beforeEach(() => {
        resetToDefault();
        hideTile('worked');
        sent.length = 0;
        ordinary.mockReset();
        setSubmit(ordinary);
        answer = () => Promise.resolve({ kind: 'stored', uuid: 'qso-9' });
        rig.cat = 'lost';
        flushSync();
        _resetRecoveredSubmitForTests();
        setRecoveredSubmitEnv(() => ({
            switchGate: null,
            bootArchiveId: 'arch-a',
            activeLogbookUuid: 'lb-a',
            activeLogbookId: 1,
        }));
        setRecoveredSender((adif, _id, opts) => {
            sent.push({ adif, opts });
            return answer();
        });
    });

    it('LC1 Log and Ctrl+Enter take the recovered path with CAT lost', async () => {
        render(LoggingCard);
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        expect(logButton()).toBeEnabled();
        ctrlEnter();
        await vi.waitFor(() => expect(recovered.record).toBeNull());
        expect(sent).toHaveLength(1);
        expect(sent[0].adif).toContain('<CALL:');
        expect(ordinary).not.toHaveBeenCalled();
        expect(mem.rows.has('d-1')).toBe(false);
    });

    it('LC1 the Log button takes the recovered path too', async () => {
        render(LoggingCard);
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        await fireEvent.click(logButton());
        await vi.waitFor(() => expect(recovered.record).toBeNull());
        expect(sent).toHaveLength(1);
        expect(ordinary).not.toHaveBeenCalled();
    });

    it('LC2 Log is disabled until the rig values are confirmed', () => {
        render(LoggingCard);
        expect(logButton()).toBeDisabled();
        expect(logButton()).toHaveAttribute('title', expect.stringMatching(/rig values/));
        ctrlEnter();
        expect(sent).toHaveLength(0);
        expect(ordinary).not.toHaveBeenCalled();
    });

    it('LC3 a duplicate keeps the form and offers inspection and a forced separate contact', async () => {
        answer = () => Promise.resolve({ kind: 'duplicate', uuid: 'qso-old' });
        render(LoggingCard);
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        await fireEvent.click(logButton());
        expect(await screen.findByTestId('recovered-duplicate')).toHaveTextContent('qso-old');
        expect(recovered.record).not.toBeNull();
        expect(draft.callsign).not.toBe('');
        ctrlEnter(); // never forces
        await vi.waitFor(() => expect(sent).toHaveLength(2));
        expect(sent[1].opts.force).toBe(false);
        expect(isVisible('worked')).toBe(false);
        await fireEvent.click(screen.getByRole('button', { name: /Show contacts with/ }));
        expect(isVisible('worked')).toBe(true);
        answer = () => Promise.resolve({ kind: 'stored', uuid: 'qso-new' });
        await fireEvent.click(
            screen.getByRole('button', { name: 'Log anyway — this is a separate contact' })
        );
        await vi.waitFor(() => expect(sent).toHaveLength(3));
        expect(sent[2].opts.force).toBe(true);
        await vi.waitFor(() => expect(recovered.record).toBeNull());
    });

    it('LC4 an unknown-outcome record asks first; Cancel sends nothing', async () => {
        await _resetRestoreForTests();
        const unknown = sampleRecord({ outcome: 'unknown' });
        await mem.put(unknown);
        const env = {
            locksAvailable: true,
            onPhoneCw: true,
            bootArchiveId: 'arch-a',
            activeLogbookUuid: 'lb-a',
            currentAttribution: sampleRecord().attribution,
        };
        expect((await restoreSavedDraft(unknown, () => env)).ok).toBe(true);
        const asked = vi.fn(() => false);
        vi.stubGlobal('confirm', asked);
        render(LoggingCard);
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        await fireEvent.click(logButton());
        expect(asked).toHaveBeenCalledWith(UNKNOWN_CONFIRM);
        expect(sent).toHaveLength(0);
        expect(recovered.record).not.toBeNull();
    });

    it('LC5 a confirmed log whose cleanup fails shows the UUID and retries cleanup only', async () => {
        const remove = vi.spyOn(mem, 'remove').mockRejectedValue(new Error('remove failed'));
        const put = vi.spyOn(mem, 'put');
        render(LoggingCard);
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        // Both cleanup writes fail — but only once the request is out (a failed
        // attempt store would rightly send nothing).
        answer = () => {
            put.mockRejectedValue(new Error('put failed'));
            return Promise.resolve({ kind: 'stored', uuid: 'qso-9' });
        };
        await fireEvent.click(logButton());
        expect(await screen.findByTestId('recovered-confirmed')).toHaveTextContent('qso-9');
        expect(logButton()).toBeDisabled();
        ctrlEnter();
        expect(sent).toHaveLength(1);
        // Both writes have failed (not merely begun) before storage recovers.
        expect(await screen.findByText(/could not be removed/)).toBeInTheDocument();
        remove.mockRestore();
        put.mockRestore();
        await fireEvent.click(screen.getByRole('button', { name: 'Retry cleanup' }));
        await vi.waitFor(() => expect(recovered.record).toBeNull());
        expect(sent).toHaveLength(1);
        expect(mem.rows.has('d-1')).toBe(false);
    });

    it('LC6 an ambiguous outcome says the outcome is unknown', async () => {
        answer = () => Promise.resolve({ kind: 'network', message: 'connection reset' });
        render(LoggingCard);
        await fireEvent.click(screen.getByRole('button', { name: 'Confirm recovered rig values' }));
        await fireEvent.click(logButton());
        expect(await screen.findByTestId('recovered-submit-message')).toHaveTextContent(
            /outcome is unknown/
        );
        expect(recovered.record).not.toBeNull();
        expect(sent).toHaveLength(1);
    });
});
