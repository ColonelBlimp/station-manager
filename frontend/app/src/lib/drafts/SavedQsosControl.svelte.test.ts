/*
    Saved QSOs open from a header control (ADR 0086; operator rulings 2026-09-30).

      C1  Nothing saved and nothing wrong: no control.
      C2  The control reads "Saved QSOs (N)" — records from inactive archives and
          with unknown outcomes count.
      C3  Storage that cannot be read shows the control with the error, never an
          empty collection.
      C4  The control opens an overlay panel; each saved QSO is its own entry —
          callsign, time, source archive — with the operator's wording; an
          unknown outcome never says "not logged"; no restore is promised.
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
      C9  Work newly preserved before a reload is announced ONCE in that tab with
          an ordinary toast, keeping an unknown outcome's wording; the panel does
          not open by itself; a later mount does not repeat it.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import SavedQsosControl from './SavedQsosControl.svelte';
import LoggingCard from '../operate/LoggingCard.svelte';
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
    await fireEvent.click(await screen.findByRole('button', { name: /Saved QSOs/ }));
    return screen.getByRole('dialog', { name: 'Saved QSOs' });
}

describe('SavedQsosControl', () => {
    it('C1 nothing saved, no control', async () => {
        render(SavedQsosControl);
        await loadSavedDrafts();
        expect(screen.queryByRole('button', { name: /Saved QSOs/ })).toBeNull();
    });

    it('C2 counts every record, inactive archives and unknown outcomes included', async () => {
        await seed(sampleRecord(), SECOND());
        render(SavedQsosControl);
        expect(await screen.findByRole('button', { name: 'Saved QSOs (2)' })).toBeInTheDocument();
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
        expect(first).toHaveTextContent('Unlogged QSO saved from ‘Home’ — not logged.');
        expect(second).toHaveTextContent(
            'QSO draft saved from ‘Contest’ — logging outcome unknown. Check the Logbook in ‘Contest’ before logging it.'
        );
        expect(second).not.toHaveTextContent('not logged');
        expect(panel).not.toHaveTextContent(/restore|switch back/i);
        await fireEvent.click(within(panel).getByRole('button', { name: 'Close' }));
        expect(screen.queryByRole('dialog')).toBeNull();
        expect(mem.rows.size).toBe(2);
        expect(screen.getByRole('button', { name: 'Saved QSOs (2)' })).toHaveFocus();
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
        expect(screen.getByRole('button', { name: 'Saved QSOs (1)' })).toBeInTheDocument();

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
        expect(screen.getByRole('button', { name: 'Saved QSOs (1)' })).toHaveFocus();

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

    it('C9 newly preserved work is announced once, without opening the panel', async () => {
        const unknown = SECOND();
        await seed(unknown);
        rememberPreservedForAnnouncement(unknown);
        const first = render(SavedQsosControl);
        await screen.findByRole('button', { name: 'Saved QSOs (1)' });
        const toast = toastsState.items.find((t) => /Saved QSOs/.test(t.message));
        expect(toast?.message).toBe(
            'QSO draft saved from ‘Contest’ — logging outcome unknown. Check the Logbook in ‘Contest’ before logging it. It is under Saved QSOs.'
        );
        expect(toast?.ttl).toBeGreaterThan(0); // an ordinary, self-dismissing toast
        expect(screen.queryByRole('dialog')).toBeNull();
        first.unmount();
        resetToasts();
        render(SavedQsosControl);
        await screen.findByRole('button', { name: 'Saved QSOs (1)' });
        expect(toastsState.items).toHaveLength(0);
    });
});
