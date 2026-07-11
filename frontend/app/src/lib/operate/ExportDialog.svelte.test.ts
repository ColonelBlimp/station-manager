// The Export/Session dialog doubles as the FT8 view's session panel (the FT8
// layout has no Session tile), so it must LIST the session's contacts — not just
// a count — for the operator to review before exporting/emailing.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import ExportDialog from './ExportDialog.svelte';
import { operate, closeExport } from './state.svelte';
import { session, addSessionQso } from './session.svelte';

beforeEach(() => {
    session.qsos.length = 0;
    closeExport();
});

function logged(over: Partial<Parameters<typeof addSessionQso>[0]> = {}) {
    addSessionQso({
        callsign: 'W1ABC',
        timeOn: '14:30:00',
        band: '20m',
        mode: 'FT8',
        rstSent: '-12',
        rstRcvd: '-08',
        name: '',
        country: 'United States',
        comment: '',
        ...over,
    });
}

describe('ExportDialog session review', () => {
    it('lists the session contacts when open', () => {
        logged();
        logged({ callsign: 'G3XYZ', band: '40m', country: 'England' });
        operate.exportOpen = true;
        render(ExportDialog);

        expect(screen.getByText(/2 QSOs logged/)).toBeInTheDocument();
        expect(screen.getByText('W1ABC')).toBeInTheDocument();
        expect(screen.getByText('United States')).toBeInTheDocument();
        expect(screen.getByText('G3XYZ')).toBeInTheDocument();
    });

    it('shows the empty count and no contact rows for an empty session', () => {
        operate.exportOpen = true;
        render(ExportDialog);

        expect(screen.getByText(/0 QSOs logged/)).toBeInTheDocument();
        expect(screen.queryByText('W1ABC')).toBeNull();
    });
});
