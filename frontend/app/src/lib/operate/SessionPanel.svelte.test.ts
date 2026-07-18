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

    it('offers the contacts map in a new tab regardless of session contents', () => {
        render(SessionPanel);
        const link = screen.getByRole('link', { name: /Map/ });
        expect(link).toHaveAttribute('target', '_blank');
        expect(link.getAttribute('href')).toMatch(/map$/);
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
