/*
    Saved QSOs open from a header control (ADR 0086; operator rulings 2026-09-30).

      C1  Nothing saved and nothing wrong: no control.
      C2  The control reads "Unlogged QSOs (N)" (operator choice A, 2026-10-04) — records from inactive archives and
          with unknown outcomes count.
      C3  Storage that cannot be read shows the control with the error, never an
          empty collection.
      C4  The control opens an overlay panel; each saved QSO is its own entry —
          callsign, time, source archive — with the operator's wording; an
          unknown outcome never says "not logged"; no archive switch is promised.
          Closing it keeps every record and returns focus to the control.
      C5  Show details reveals every value as selectable text, with Copy.
      C6  Discard is confirmed and removes only that record; declining keeps it;
          a failed deletion keeps the entry and says so.
      C7  A re-read that fails keeps the entries beside the error.
      C8  Escape closes the panel, returns focus to the control, and does NOT
          clear a Phone / CW draft underneath; the card's shortcuts are inert
          while the panel is open. Holding Escape (its auto-repeat) after the
          dismissal does not reach the card either; a fresh Escape, after the
          key is released, clears the draft as usual (review 2026-09-30).
      C10 A modal dialog in front of the panel (Export, Duplicate, a session
          edit, the archive gate) owns Escape: it closes that dialog, and the
          panel behind it stays open (Codex review 894b5359).
      C11 Restore (ADR 0085, Restore commit 4): an eligible entry on Phone / CW
          offers Restore; it fills the form with that QSO and closes the panel.
      C12 Off Phone / CW the entry offers "Go to Phone / CW", which only
          navigates — nothing is restored until Restore is pressed there.
      C13 An entry from another archive or logbook, or whose MY_RIG differs,
          says why and offers no Restore.
      C14 A Restore refused at the moment it is pressed (a QSO being typed)
          says why in that entry and keeps the panel open and the record.
      C15 A logged entry offers no Restore.
      C16 A page whose Restore is not wired offers none.
      C17 The offer follows the page as it becomes known: a panel opened before
          the boot identity and today's attribution are read offers Restore once
          they are, without being reopened.
      C9  Work newly preserved before a reload is announced ONCE in that tab with
          an ordinary toast, keeping an unknown outcome's wording; the panel does
          not open by itself; a later mount does not repeat it.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import SavedQsosControl from './SavedQsosControl.svelte';
import LoggingCard from '../operate/LoggingCard.svelte';
import ExportDialog from '../operate/ExportDialog.svelte';
import { operate } from '../operate/state.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { _setDraftChannelForTests } from './draftChannel';
import {
    _resetSavedDraftsForTests,
    loadSavedDrafts,
    rememberPreservedForAnnouncement,
    savedQsosPanel,
} from './savedDrafts.svelte';
import { sampleRecord } from './savedDraft.fixture';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import { draft, clearDraft, setSubmit } from '../operate/qso.svelte';
import { confirmRig, rig } from '../operate/rig.svelte';
import { fakeDraftLocks } from './draftLock.fixture';
import { recovered } from './recovered.svelte';
import { setRestoreEnv, _resetRestoreForTests } from './restoreSession';
import { _resetRecoveredSaveForTests } from './recoveredSave.svelte';
import { router } from '../router.svelte';
import type { RestoreEnv } from './restore';
import {
    applyBootAttribution,
    attributionEpoch,
    currentAttribution,
    _resetAttributionForTests,
} from './attributionSource.svelte';
import { bootArchiveId, _setBootIdentityForTests } from '../config/archives.svelte';

let mem = memoryDraftStore();

beforeEach(() => {
    mem = memoryDraftStore();
    _setDraftStoreForTests(mem);
    _setDraftChannelForTests({ post() {}, subscribe: () => () => {} });
    _resetSavedDraftsForTests();
    resetToasts();
    try {
        sessionStorage.clear();
    } catch {
        /* no storage in this environment */
    }
});
afterEach(() => {
    _setDraftStoreForTests(null);
    _setDraftChannelForTests(null);
    vi.restoreAllMocks();
    clearDraft();
});

async function seed(...records: ReturnType<typeof sampleRecord>[]): Promise<void> {
    for (const r of records) await mem.put(r);
}

const SECOND = () =>
    sampleRecord({
        id: 'd-2',
        savedAt: '2026-09-30T12:20:00.000Z',
        archiveId: 'arch-b',
        archiveLabel: 'Contest',
        outcome: 'unknown',
        fields: { ...sampleRecord().fields, callsign: 'M0XYZ', timeOn: '12:15:00' },
    });

async function openPanel(): Promise<HTMLElement> {
    await fireEvent.click(await screen.findByRole('button', { name: /Unlogged QSOs/ }));
    return screen.getByRole('dialog', { name: 'Unlogged QSOs' });
}

describe('SavedQsosControl', () => {
    it('C1 nothing saved, no control', async () => {
        render(SavedQsosControl);
        await loadSavedDrafts();
        expect(screen.queryByRole('button', { name: /Unlogged QSOs/ })).toBeNull();
    });

    it('C2 counts every record, inactive archives and unknown outcomes included', async () => {
        await seed(sampleRecord(), SECOND());
        render(SavedQsosControl);
        expect(
            await screen.findByRole('button', { name: 'Unlogged QSOs (2)' })
        ).toBeInTheDocument();
    });

    it('C3 unreadable storage shows the control with the error, not an empty list', async () => {
        vi.spyOn(mem, 'list').mockRejectedValue(new Error('the storage read failed'));
        render(SavedQsosControl);
        const panel = await openPanel();
        expect(within(panel).getByTestId('saved-drafts-error')).toHaveTextContent(
            'the storage read failed'
        );
    });

    it('C4 the panel lists each record with its wording; closing keeps them and refocuses', async () => {
        await seed(sampleRecord(), SECOND());
        render(SavedQsosControl);
        const panel = await openPanel();
        const first = within(panel).getByTestId('saved-draft-d-1');
        const second = within(panel).getByTestId('saved-draft-d-2');
        expect(first).toHaveTextContent('G0ABC · 2026-09-30 12:00:00 UTC');
        expect(first).toHaveTextContent('Not logged — QSO from ‘Home’, kept in this browser.');
        expect(second).toHaveTextContent(
            'Logging outcome unknown — QSO from ‘Contest’, kept in this browser. Check the Logbook in ‘Contest’ before logging it.'
        );
        expect(second).not.toHaveTextContent('not logged');
        expect(panel).not.toHaveTextContent(/switch back/i);
        await fireEvent.click(within(panel).getByRole('button', { name: 'Close' }));
        expect(screen.queryByRole('dialog')).toBeNull();
        expect(mem.rows.size).toBe(2);
        expect(screen.getByRole('button', { name: 'Unlogged QSOs (2)' })).toHaveFocus();
    });

    it('C5 details show every value, with Copy', async () => {
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        expect(within(entry).queryByTestId('saved-draft-details')).toBeNull();
        await fireEvent.click(within(entry).getByRole('button', { name: 'Show details' }));
        const details = within(entry).getByTestId('saved-draft-details');
        expect(details).toHaveTextContent('14.255000 MHz');
        expect(within(entry).getByRole('button', { name: 'Copy' })).toBeInTheDocument();
    });

    it('C6 Discard is confirmed, removes only that record, and a failure keeps it', async () => {
        await seed(sampleRecord(), SECOND());
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        render(SavedQsosControl);
        const panel = await openPanel();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        await fireEvent.click(within(entry).getByRole('button', { name: 'Discard' }));
        expect(confirm.mock.calls[0][0]).toMatch(/only the copy saved in this browser/);
        expect(mem.rows.has('d-1')).toBe(true);
        confirm.mockReturnValue(true);
        await fireEvent.click(within(entry).getByRole('button', { name: 'Discard' }));
        await vi.waitFor(() => expect(screen.queryByTestId('saved-draft-d-1')).toBeNull());
        expect(mem.rows.has('d-2')).toBe(true);
        expect(screen.getByRole('button', { name: 'Unlogged QSOs (1)' })).toBeInTheDocument();

        vi.spyOn(mem, 'remove').mockRejectedValue(new Error('storage busy'));
        const other = screen.getByTestId('saved-draft-d-2');
        await fireEvent.click(within(other).getByRole('button', { name: 'Discard' }));
        await vi.waitFor(() =>
            expect(toastsState.items.some((t) => /could not be discarded/.test(t.message))).toBe(
                true
            )
        );
        expect(screen.getByTestId('saved-draft-d-2')).toBeInTheDocument();
    });

    it('C7 a failed re-read keeps the entries beside the error', async () => {
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        vi.spyOn(mem, 'list').mockRejectedValueOnce(new Error('the storage read failed'));
        await loadSavedDrafts();
        expect(await within(panel).findByTestId('saved-drafts-error')).toBeInTheDocument();
        expect(within(panel).getByTestId('saved-draft-d-1')).toBeInTheDocument();
    });

    it('C8 Escape closes the panel and never clears the Phone / CW draft', async () => {
        await seed(sampleRecord());
        const logged: string[] = [];
        setSubmit((q) => {
            logged.push(q.callsign);
            return Promise.resolve({ ok: true as const });
        });
        rig.cat = 'off';
        confirmRig();
        render(LoggingCard);
        render(SavedQsosControl);
        draft.callsign = 'G0ABC';
        draft.dateOn = '2026-09-30';
        draft.timeOn = '12:00';
        flushSync();
        await openPanel();
        expect(savedQsosPanel.open).toBe(true);

        window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true }));
        flushSync();
        expect(logged).toEqual([]); // the card's shortcuts are inert under the panel

        document.activeElement?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
        );
        flushSync();
        expect(screen.queryByRole('dialog')).toBeNull();
        expect(draft.callsign).toBe('G0ABC');
        expect(screen.getByRole('button', { name: 'Unlogged QSOs (1)' })).toHaveFocus();

        // Held: the key's auto-repeat must not reach the card.
        document.activeElement?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, repeat: true })
        );
        flushSync();
        expect(draft.callsign).toBe('G0ABC');

        // Released and pressed again: an ordinary Escape clears the draft.
        document.activeElement?.dispatchEvent(
            new KeyboardEvent('keyup', { key: 'Escape', bubbles: true })
        );
        document.activeElement?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
        );
        flushSync();
        expect(draft.callsign).toBe('');
    });

    it('C9 newly preserved work is announced once, with an action that opens the panel', async () => {
        const unknown = SECOND();
        await seed(unknown);
        rememberPreservedForAnnouncement(unknown);
        const first = render(SavedQsosControl);
        await screen.findByRole('button', { name: 'Unlogged QSOs (1)' });
        const toast = toastsState.items.find((t) => /Unlogged QSOs/.test(t.message));
        expect(toast?.message).toBe(
            'Logging outcome unknown — QSO from ‘Contest’, kept in this browser. Check the Logbook in ‘Contest’ before logging it. It is under Unlogged QSOs.'
        );
        // No automatic timeout, and its action opens the list (choice A).
        expect(toast?.ttl).toBe(0);
        expect(toast?.action?.label).toBe('Open Unlogged QSOs');
        expect(screen.queryByRole('dialog')).toBeNull();
        toast?.action?.run();
        expect(await screen.findByRole('dialog', { name: 'Unlogged QSOs' })).toBeInTheDocument();
        await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus());
        await fireEvent.click(screen.getByRole('button', { name: 'Close' }));
        first.unmount();
        resetToasts();
        render(SavedQsosControl);
        await screen.findByRole('button', { name: 'Unlogged QSOs (1)' });
        expect(toastsState.items).toHaveLength(0);
    });

    it('C10 a modal in front of the panel owns Escape; the panel stays open', async () => {
        await seed(sampleRecord());
        render(LoggingCard);
        render(ExportDialog);
        render(SavedQsosControl);
        draft.callsign = 'G0ABC';
        flushSync();
        await openPanel();
        operate.exportOpen = true;
        flushSync();
        expect(screen.getByRole('dialog', { name: /export/i })).toBeInTheDocument();

        document.activeElement?.dispatchEvent(
            new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
        );
        flushSync();
        expect(operate.exportOpen).toBe(false);
        expect(savedQsosPanel.open).toBe(true);
        expect(screen.getByRole('dialog', { name: 'Unlogged QSOs' })).toBeInTheDocument();
        expect(draft.callsign).toBe('G0ABC');
    });
});

describe('Restore from the panel', () => {
    let env: Omit<RestoreEnv, 'locksAvailable'>;
    beforeEach(() => {
        vi.stubGlobal('navigator', { locks: fakeDraftLocks().manager });
        router.view = 'operate';
        router.mode = 'phone';
        env = {
            onPhoneCw: true,
            bootArchiveId: 'arch-a',
            activeLogbookUuid: 'lb-a',
            currentAttribution: sampleRecord().attribution,
        };
        setRestoreEnv(() => ({
            ...env,
            locksAvailable: true,
            onPhoneCw: router.view === 'operate' && router.mode === 'phone',
        }));
    });
    afterEach(async () => {
        setRestoreEnv(null);
        _resetRecoveredSaveForTests();
        await _resetRestoreForTests();
        vi.unstubAllGlobals();
    });

    it('C11 Restore fills the form with that QSO and closes the panel', async () => {
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        await fireEvent.click(within(entry).getByRole('button', { name: 'Restore' }));
        await vi.waitFor(() => expect(recovered.record?.id).toBe('d-1'));
        expect(draft.callsign).toBe('g0abc');
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('C12 off Phone / CW the entry only navigates there', async () => {
        router.mode = 'ft8';
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        expect(within(entry).queryByRole('button', { name: 'Restore' })).toBeNull();
        await fireEvent.click(within(entry).getByRole('button', { name: 'Go to Phone / CW' }));
        expect(router.mode).toBe('phone');
        expect(recovered.record).toBeNull();
        expect(draft.callsign).toBe('');
        expect(within(entry).getByRole('button', { name: 'Restore' })).toBeInTheDocument();
    });

    it('C13 another archive, or a changed MY_RIG, says why and offers no Restore', async () => {
        env.currentAttribution = { ...sampleRecord().attribution!, myRig: 'FTdx10' };
        await seed(sampleRecord(), SECOND());
        render(SavedQsosControl);
        const panel = await openPanel();
        const other = within(panel).getByTestId('saved-draft-d-2');
        expect(other).toHaveTextContent(/belongs to ‘Contest’/);
        expect(within(other).queryByRole('button', { name: 'Restore' })).toBeNull();
        const rigChanged = within(panel).getByTestId('saved-draft-d-1');
        expect(rigChanged).toHaveTextContent(/My rig differs.*FTdx10/);
        expect(within(rigChanged).queryByRole('button', { name: 'Restore' })).toBeNull();
    });

    it('C14 a Restore refused when pressed says why and keeps the panel and record', async () => {
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        draft.callsign = 'K1ABC'; // typed after the panel opened
        flushSync();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        await fireEvent.click(within(entry).getByRole('button', { name: 'Restore' }));
        expect(await within(entry).findByRole('alert')).toHaveTextContent(/Clear the current QSO/);
        expect(recovered.record).toBeNull();
        expect(draft.callsign).toBe('K1ABC');
        expect(screen.getByRole('dialog', { name: 'Unlogged QSOs' })).toBeInTheDocument();
        expect(mem.rows.has('d-1')).toBe(true);
    });

    it('C15 a logged entry offers no Restore', async () => {
        await seed(sampleRecord({ state: 'logged', loggedQsoUuid: 'qso-1' }));
        render(SavedQsosControl);
        const panel = await openPanel();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        expect(within(entry).queryByRole('button', { name: 'Restore' })).toBeNull();
        expect(within(entry).queryByRole('button', { name: 'Go to Phone / CW' })).toBeNull();
    });

    it('C16 a page whose Restore is not wired offers none', async () => {
        setRestoreEnv(null);
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        expect(within(panel).queryByRole('button', { name: /Restore|Go to Phone/ })).toBeNull();
    });
});

describe('Restore follows the page as it becomes known', () => {
    beforeEach(() => {
        vi.stubGlobal('navigator', { locks: fakeDraftLocks().manager });
        router.view = 'operate';
        router.mode = 'phone';
        _resetAttributionForTests();
        _setBootIdentityForTests(null);
        setRestoreEnv(() => ({
            locksAvailable: true,
            onPhoneCw: true,
            bootArchiveId: bootArchiveId(),
            activeLogbookUuid: 'lb-a',
            currentAttribution: currentAttribution('7Q5MLV'),
        }));
    });
    afterEach(async () => {
        setRestoreEnv(null);
        _resetAttributionForTests();
        _setBootIdentityForTests(null);
        await _resetRestoreForTests();
        vi.unstubAllGlobals();
    });

    it('C17 a panel opened early offers Restore once identity and attribution land', async () => {
        await seed(sampleRecord());
        render(SavedQsosControl);
        const panel = await openPanel();
        const entry = within(panel).getByTestId('saved-draft-d-1');
        expect(within(entry).queryByRole('button', { name: 'Restore' })).toBeNull();
        _setBootIdentityForTests({ instance: 'i1', archiveId: 'arch-a' });
        flushSync();
        expect(entry).toHaveTextContent(/could not be read/);
        applyBootAttribution(sampleRecord().attribution, attributionEpoch(), '7Q5MLV');
        flushSync();
        expect(within(entry).getByRole('button', { name: 'Restore' })).toBeInTheDocument();
    });
});
