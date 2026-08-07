// The Session panel is the QSO-list surface (this sitting's contacts) with its own
// Export… button. Shared by Phone/CW (a tile) and FT8 (a rail-toggled overlay);
// Export… opens the download/email dialog.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

const fetchQso = vi.fn();
const patchQso = vi.fn();
vi.mock('../api/qso-patch', () => ({
    fetchQso: (...a: unknown[]): unknown => fetchQso(...a),
    patchQso: (...a: unknown[]): unknown => patchQso(...a),
}));

import SessionPanel from './SessionPanel.svelte';
import { addSessionQso, _resetSessionForTests } from './session.svelte';
import { sessionEdit } from './sessionEdit.svelte';
import { operate, closeExport } from './state.svelte';

beforeEach(() => {
    _resetSessionForTests();
    closeExport();
    sessionEdit.close();
    sessionEdit.openError = null;
    fetchQso.mockReset();
});

describe('SessionPanel', () => {
    it('shows the empty state with Export disabled', () => {
        render(SessionPanel);
        expect(screen.getByText(/No QSOs logged/)).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /Export/ })).toBeDisabled();
    });

    it('lists logged QSOs and Export opens the export dialog', async () => {
        addSessionQso({
            callsign: 'W1ABC',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: 'United States',
            comment: '',
        });
        render(SessionPanel);

        expect(screen.getByText('W1ABC')).toBeInTheDocument();
        const exportBtn = screen.getByRole('button', { name: /Export/ });
        expect(exportBtn).not.toBeDisabled();

        await fireEvent.click(exportBtn);
        expect(operate.exportOpen).toBe(true);
    });

    // The Map button was removed as redundant (operator, 2026-07-25): the sidebar's
    // always-visible Map link opens the identical tab. Coverage for the link itself
    // moved with it, to Sidebar.svelte.test.ts — it did not disappear.
    it('does not duplicate the sidebar Map link on the tile', () => {
        render(SessionPanel);
        expect(screen.queryByRole('link', { name: /Map/ })).toBeNull();
    });

    it('opens the in-place edit modal from a row callsign (no route change)', async () => {
        addSessionQso({
            uuid: 'u-9',
            callsign: 'ZS6DX',
            timeOn: '15:00:00',
            band: '40m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: 'South Africa',
            comment: '',
        });
        fetchQso.mockResolvedValue({
            kind: 'ok',
            qso: { id: 3, uuid: 'u-9', call: 'ZS6DX', band: '40m' },
        });
        render(SessionPanel);

        await fireEvent.click(screen.getByRole('button', { name: 'ZS6DX' }));
        expect(fetchQso).toHaveBeenCalledWith('u-9');
        // The shared EditQsoModal mounts in place, seeded with the row.
        expect(await screen.findByRole('dialog', { name: 'Edit QSO' })).toBeInTheDocument();
    });

    it('renders a uuid-less row as plain text (no edit affordance)', () => {
        addSessionQso({
            callsign: 'OLDROW',
            timeOn: '15:01:00',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '59',
            name: '',
            country: '',
            comment: '',
        });
        render(SessionPanel);
        expect(screen.getByText('OLDROW')).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: 'OLDROW' })).toBeNull();
    });

    it('surfaces a hydration failure as a panel error line', async () => {
        addSessionQso({
            uuid: 'u-10',
            callsign: 'G0FAIL',
            timeOn: '15:02:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });
        fetchQso.mockResolvedValue({ kind: 'error', message: 'Cannot reach the daemon.' });
        render(SessionPanel);

        await fireEvent.click(screen.getByRole('button', { name: 'G0FAIL' }));
        expect(await screen.findByTestId('session-edit-error')).toHaveTextContent('G0FAIL');
    });
});

describe('SessionPanel header count', () => {
    const qso = (callsign: string, mode: string) => ({
        callsign,
        timeOn: '14:30:00',
        band: '20m',
        mode,
        rstSent: '',
        rstRcvd: '',
        name: '',
        country: '',
        comment: '',
    });

    it('shows (0) on an empty session', () => {
        render(SessionPanel);
        expect(screen.getByRole('heading', { name: /Session \(0\)/ })).toBeInTheDocument();
    });

    // The tally spans BOTH modes — session.qsos is fed by the Phone/CW submit sink
    // and the FT8 ft8-logged SSE alike, so the header must not read as per-mode.
    it('counts FT8 and Phone/CW contacts together', () => {
        addSessionQso(qso('W1ABC', 'FT8'));
        addSessionQso(qso('G0XYZ', 'SSB'));
        addSessionQso(qso('DL1ABC', 'CW'));
        render(SessionPanel);
        expect(screen.getByRole('heading', { name: /Session \(3\)/ })).toBeInTheDocument();
    });
});

/*
    CALLSIGN SEARCH — dogfood 2026-08-07 ("add a simple search as you type
    field to the session panel for callsigns"). A long FT8 sitting runs to
    dozens of rows; finding whether/when a call was worked means scrolling.
    Rules:
    - substring match, case-insensitive (subsumes prefix; "5gr" finds VK5GR —
      the fixture differentiates a prefix-only or case-sensitive cut);
    - the filter is a VIEW: the header count stays the sitting's total;
    - the no-match state must be tellable from the nearest confusable state,
      an EMPTY SESSION — different message, and only while a filter is typed.
*/
describe('SessionPanel callsign search', () => {
    const qso = (callsign: string) => ({
        callsign,
        timeOn: '14:30:00',
        band: '20m',
        mode: 'FT8',
        rstSent: '',
        rstRcvd: '',
        name: '',
        country: '',
        comment: '',
    });

    it('narrows rows to callsigns containing the typed text, case-insensitive', async () => {
        addSessionQso(qso('W1ABC'));
        addSessionQso(qso('VK5GR'));
        addSessionQso(qso('G0XYZ'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: '5gr' } });

        expect(screen.getByText('VK5GR')).toBeInTheDocument();
        expect(screen.queryByText('W1ABC')).toBeNull();
        expect(screen.queryByText('G0XYZ')).toBeNull();
    });

    it('clearing the search restores every row', async () => {
        addSessionQso(qso('W1ABC'));
        addSessionQso(qso('VK5GR'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'VK' } });
        expect(screen.queryByText('W1ABC')).toBeNull();
        await fireEvent.input(box, { target: { value: '' } });

        expect(screen.getByText('W1ABC')).toBeInTheDocument();
        expect(screen.getByText('VK5GR')).toBeInTheDocument();
    });

    it('keeps the header count at the sitting total while filtering', async () => {
        addSessionQso(qso('W1ABC'));
        addSessionQso(qso('VK5GR'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'VK' } });

        expect(screen.getByRole('heading', { name: /Session \(2\)/ })).toBeInTheDocument();
    });

    it('a no-match search reads differently from an empty session', async () => {
        addSessionQso(qso('W1ABC'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'ZZZZ' } });

        expect(screen.getByText(/No callsigns match/)).toBeInTheDocument();
        expect(screen.queryByText(/No QSOs logged/)).toBeNull();
    });
});
