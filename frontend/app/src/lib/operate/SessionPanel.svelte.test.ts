// The Session panel is the QSO-list surface (this sitting's contacts) with its own
// Export… button. Shared by Phone/CW (a tile) and FT8 (a rail-toggled overlay);
// Export… opens the download/email dialog.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import SessionPanel from './SessionPanel.svelte';
import { addSessionQso, _resetSessionForTests } from './session.svelte';
import { operate, closeExport } from './state.svelte';

beforeEach(() => {
    _resetSessionForTests();
    closeExport();
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
});
