/*
    The saved-QSO notice below the header (ADR 0085, rule 3 slice 1; operator
    rulings 2026-09-30).

      N1  Nothing saved: no notice.
      N2  Each saved QSO is its own entry — callsign, time and source archive —
          with the operator's wording; an unknown outcome never says "not
          logged". No restore action is promised.
      N3  Show details reveals every value as selectable text, with Copy.
      N4  Discard is confirmed and removes only that saved record; declining
          keeps it.
      N5  A failed deletion keeps the entry visible and says so.
      N6  Unreadable browser storage is said, not silently shown as nothing.
          (Storage that does not exist at all holds nothing: see draftStore.)
      N7  A re-read that fails keeps the entries already shown, beside the error.
*/
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import SavedDraftsNotice from './SavedDraftsNotice.svelte';
import { _setDraftStoreForTests, memoryDraftStore } from './draftStore';
import { _resetSavedDraftsForTests, loadSavedDrafts } from './savedDrafts.svelte';
import { sampleRecord } from './savedDraft.fixture';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

let mem = memoryDraftStore();

beforeEach(() => {
    mem = memoryDraftStore();
    _setDraftStoreForTests(mem);
    _resetSavedDraftsForTests();
    resetToasts();
});
afterEach(() => {
    _setDraftStoreForTests(null);
    vi.restoreAllMocks();
});

async function seed(...records: ReturnType<typeof sampleRecord>[]): Promise<void> {
    for (const r of records) await mem.put(r);
}

const SECOND = () =>
    sampleRecord({
        id: 'd-2',
        savedAt: '2026-09-30T12:20:00.000Z',
        outcome: 'unknown',
        fields: { ...sampleRecord().fields, callsign: 'M0XYZ', timeOn: '12:15:00' },
    });

describe('SavedDraftsNotice', () => {
    it('N1 nothing saved, no notice', async () => {
        render(SavedDraftsNotice);
        await vi.waitFor(() => expect(screen.queryByTestId('saved-drafts')).toBeNull());
    });

    it('N2 distinct entries with the operator wording, no restore promise', async () => {
        await seed(sampleRecord(), SECOND());
        render(SavedDraftsNotice);
        const first = await screen.findByTestId('saved-draft-d-1');
        const second = screen.getByTestId('saved-draft-d-2');
        expect(first).toHaveTextContent('G0ABC · 2026-09-30 12:00:00 UTC');
        expect(first).toHaveTextContent('Unlogged QSO saved from ‘Home’ — not logged.');
        expect(second).toHaveTextContent('M0XYZ');
        expect(second).toHaveTextContent(
            'QSO draft saved from ‘Home’ — logging outcome unknown. Check the Logbook in ‘Home’ before logging it.'
        );
        expect(second).not.toHaveTextContent('not logged');
        const notice = screen.getByTestId('saved-drafts');
        expect(notice).not.toHaveTextContent(/restore|switch back/i);
        expect(within(notice).queryByRole('button', { name: /restore/i })).toBeNull();
    });

    it('N3 details show every value, with Copy', async () => {
        await seed(sampleRecord());
        render(SavedDraftsNotice);
        const entry = await screen.findByTestId('saved-draft-d-1');
        expect(within(entry).queryByTestId('saved-draft-details')).toBeNull();
        await fireEvent.click(within(entry).getByRole('button', { name: 'Show details' }));
        const details = within(entry).getByTestId('saved-draft-details');
        expect(details).toHaveTextContent('14.255000 MHz');
        expect(details).toHaveTextContent('Home log');
        expect(within(entry).getByRole('button', { name: 'Copy' })).toBeInTheDocument();
    });

    it('N4 Discard is confirmed and removes only that record', async () => {
        await seed(sampleRecord(), SECOND());
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        render(SavedDraftsNotice);
        const entry = await screen.findByTestId('saved-draft-d-1');
        await fireEvent.click(within(entry).getByRole('button', { name: 'Discard' }));
        expect(confirm.mock.calls[0][0]).toMatch(/only the copy saved in this browser/);
        expect(mem.rows.has('d-1')).toBe(true);
        confirm.mockReturnValue(true);
        await fireEvent.click(within(entry).getByRole('button', { name: 'Discard' }));
        await vi.waitFor(() => expect(screen.queryByTestId('saved-draft-d-1')).toBeNull());
        expect(mem.rows.has('d-1')).toBe(false);
        expect(mem.rows.has('d-2')).toBe(true);
        expect(screen.getByTestId('saved-draft-d-2')).toBeInTheDocument();
    });

    it('N5 a failed deletion keeps the entry and says so', async () => {
        await seed(sampleRecord());
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        vi.spyOn(mem, 'remove').mockRejectedValue(new Error('storage busy'));
        render(SavedDraftsNotice);
        const entry = await screen.findByTestId('saved-draft-d-1');
        await fireEvent.click(within(entry).getByRole('button', { name: 'Discard' }));
        await vi.waitFor(() =>
            expect(toastsState.items.some((t) => /could not be discarded/.test(t.message))).toBe(
                true
            )
        );
        expect(screen.getByTestId('saved-draft-d-1')).toBeInTheDocument();
    });

    it('N6 unreadable storage is said', async () => {
        vi.spyOn(mem, 'list').mockRejectedValue(new Error('the storage read failed'));
        render(SavedDraftsNotice);
        expect(await screen.findByTestId('saved-drafts-error')).toHaveTextContent(
            'the storage read failed'
        );
    });

    it('N7 a failed re-read keeps the entries shown beside the error', async () => {
        await seed(sampleRecord());
        render(SavedDraftsNotice);
        await screen.findByTestId('saved-draft-d-1');
        vi.spyOn(mem, 'list').mockRejectedValueOnce(new Error('the storage read failed'));
        await loadSavedDrafts();
        expect(await screen.findByTestId('saved-drafts-error')).toBeInTheDocument();
        expect(screen.getByTestId('saved-draft-d-1')).toBeInTheDocument();
    });
});
