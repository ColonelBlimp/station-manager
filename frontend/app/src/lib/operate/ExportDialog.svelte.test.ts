// The export dialog is the end-of-session action surface (download ADIF + email to
// QSL manager). The contact LIST lives on the Session panel, not here — this just
// summarises the count and gates the actions on it.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import ExportDialog from './ExportDialog.svelte';
import { operate, closeExport } from './state.svelte';
import { addSessionQso, _resetSessionForTests } from './session.svelte';

beforeEach(() => {
    _resetSessionForTests();
    closeExport();
});

describe('ExportDialog', () => {
    it('summarises the session count when open', () => {
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
        operate.exportOpen = true;
        render(ExportDialog);
        expect(screen.getByText(/1 QSO logged/)).toBeInTheDocument();
        expect(screen.getByText('Export session')).toBeInTheDocument();
    });

    it('shows the empty count for an empty session', () => {
        operate.exportOpen = true;
        render(ExportDialog);
        expect(screen.getByText(/0 QSOs logged/)).toBeInTheDocument();
    });

    it('renders nothing while closed', () => {
        render(ExportDialog);
        expect(screen.queryByText('Export session')).toBeNull();
    });
});
