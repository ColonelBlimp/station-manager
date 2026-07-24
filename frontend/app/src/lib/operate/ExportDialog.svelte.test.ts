// The export dialog is the end-of-session action surface (download ADIF + email to
// QSL manager). The contact LIST lives on the Session panel, not here — this just
// summarises the count and gates the actions on it.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import ExportDialog from './ExportDialog.svelte';
import { operate, closeExport } from './state.svelte';
import { addSessionQso, session, _resetSessionForTests } from './session.svelte';
import { setMailer } from './mailer.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

// Add a session QSO with only the fields a test cares about.
function addQso(over: Partial<Parameters<typeof addSessionQso>[0]>): void {
    addSessionQso({
        callsign: 'X',
        timeOn: '',
        band: '',
        mode: 'FT8',
        rstSent: '',
        rstRcvd: '',
        name: '',
        country: '',
        comment: '',
        ...over,
    });
}

beforeEach(() => {
    _resetSessionForTests();
    closeExport();
});

afterEach(() => {
    vi.unstubAllGlobals();
    setMailer(false, '');
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

    it('defaults a resend to the not-yet-emailed delta and flags the sent rows', async () => {
        setMailer(true, 'qsl@example.com');
        addQso({ callsign: 'DONE', uuid: 'u1', emailed: true });
        addQso({ callsign: 'NEW', uuid: 'u2' });

        let sentUuids: string[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
                sentUuids = (JSON.parse(init?.body as string) as { uuids: string[] }).uuids;
                return Promise.resolve(
                    new Response(JSON.stringify({ status: 'sent', emailed: sentUuids, date: '' }), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();

        // The delta checkbox shows the already-sent count and defaults to on.
        expect(screen.getByText(/1 already sent this session/)).toBeInTheDocument();

        await fireEvent.click(screen.getByRole('button', { name: /Send/ }));
        await flush();
        await flush();
        flushSync();

        // Only the not-yet-emailed QSO was sent…
        expect(sentUuids).toEqual(['u2']);
        // …and it's now flagged, so a further resend would exclude it.
        expect(session.qsos.find((q) => q.uuid === 'u2')?.emailed).toBe(true);
    });

    it('sends the whole session and shows no delta checkbox when nothing is emailed yet', async () => {
        setMailer(true, 'qsl@example.com');
        addQso({ callsign: 'A', uuid: 'u1' });
        addQso({ callsign: 'B', uuid: 'u2' });

        let sentUuids: string[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
                sentUuids = (JSON.parse(init?.body as string) as { uuids: string[] }).uuids;
                return Promise.resolve(
                    new Response(JSON.stringify({ status: 'sent', emailed: sentUuids, date: '' }), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();

        expect(screen.queryByText(/already sent this session/)).toBeNull();

        await fireEvent.click(screen.getByRole('button', { name: /Send/ }));
        await flush();
        await flush();
        flushSync();

        expect([...sentUuids].sort()).toEqual(['u1', 'u2']);
    });
});
