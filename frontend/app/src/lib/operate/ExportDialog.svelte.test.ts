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
import { toasts } from '../ui/toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

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
    vi.restoreAllMocks();
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

    it('resets the resend-all override when the dialog is reopened', async () => {
        setMailer(true, 'qsl@example.com');
        addQso({ callsign: 'DONE', uuid: 'u1', emailed: true });
        addQso({ callsign: 'NEW', uuid: 'u2' });

        operate.exportOpen = true;
        render(ExportDialog); // stays mounted across close/reopen, like Operate.svelte
        flushSync();

        // Uncheck "only not-yet-emailed" to resend all.
        const checkbox = () => screen.getByRole('checkbox');
        expect(checkbox()).toBeChecked();
        await fireEvent.click(checkbox());
        flushSync();
        expect(checkbox()).not.toBeChecked();

        // Close and reopen (component is NOT re-rendered — same instance).
        closeExport();
        flushSync();
        operate.exportOpen = true;
        flushSync();

        // The override is back to the safe delta default (pre-fix it stayed off,
        // re-mailing already-sent QSOs on the next send).
        expect(checkbox()).toBeChecked();
    });

    it('records export.adif_failed on a failed export and still shows the toast', async () => {
        addQso({ callsign: 'A', uuid: 'u1' });
        addQso({ callsign: 'B', uuid: 'u2' });

        let notifyBody: Record<string, unknown> | undefined;
        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
                if (urlOf(input) === '/v1/notifications') {
                    notifyBody = JSON.parse(init?.body as string) as Record<string, unknown>;
                    return Promise.resolve(new Response(null, { status: 204 }));
                }
                // The export itself fails with a 500 server error.
                return Promise.resolve(
                    new Response(JSON.stringify({ code: 'boom', message: 'kaboom' }), {
                        status: 500,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        const errSpy = vi.spyOn(toasts, 'error');

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: /ADIF/ }));
        await flush();
        await flush();
        flushSync();

        // The toast fired…
        expect(errSpy).toHaveBeenCalledTimes(1);
        // …and the durable record carries only the allowlisted typed fields: the
        // submitted UUID count (2) and the mapped outcome, never the message.
        expect(notifyBody).toEqual({ kind: 'export.adif_failed', count: 2, outcome: 'server' });
    });

    it('does not record when an export is aborted', async () => {
        addQso({ callsign: 'A', uuid: 'u1' });

        const fetchSpy = vi.fn((input: RequestInfo | URL) => {
            if (urlOf(input) === '/v1/notifications') {
                return Promise.resolve(new Response(null, { status: 204 }));
            }
            // The export is cancelled → safeFetch maps AbortError to 'aborted'.
            return Promise.reject(Object.assign(new Error('cancelled'), { name: 'AbortError' }));
        });
        vi.stubGlobal('fetch', fetchSpy);
        const errSpy = vi.spyOn(toasts, 'error');

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: /ADIF/ }));
        await flush();
        await flush();
        flushSync();

        // A cancel is not a failure: no toast, and crucially no notification POST.
        expect(errSpy).not.toHaveBeenCalled();
        const posted = fetchSpy.mock.calls.some((c) => urlOf(c[0]) === '/v1/notifications');
        expect(posted).toBe(false);
    });

    // F-04b (ADR 0078): a timed-out export is ambiguous about its server-side
    // backup, so it is NOT a definite failure — it must warn "outcome unknown;
    // export again" and must NOT record an export.adif_failed notification.
    it('a timed-out export reports outcome-unknown (not "Export failed") and records no failure', async () => {
        addQso({ callsign: 'A', uuid: 'u1' });

        const fetchSpy = vi.fn((input: RequestInfo | URL) => {
            if (urlOf(input) === '/v1/notifications') {
                return Promise.resolve(new Response(null, { status: 204 }));
            }
            // The export POST times out → safeFetch maps TimeoutError to network+timedOut.
            return Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }));
        });
        vi.stubGlobal('fetch', fetchSpy);
        const errSpy = vi.spyOn(toasts, 'error');
        const warnSpy = vi.spyOn(toasts, 'warn');

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: /ADIF/ }));
        await flush();
        await flush();
        flushSync();

        // Outcome-unknown warning, never a definite "Export failed" error…
        expect(warnSpy).toHaveBeenCalledTimes(1);
        expect(warnSpy.mock.calls[0][0]).toMatch(/the outcome is unknown/);
        expect(errSpy).not.toHaveBeenCalled();
        // …and no durable failure record (it did not definitely fail).
        const recorded = fetchSpy.mock.calls.some((c) => urlOf(c[0]) === '/v1/notifications');
        expect(recorded).toBe(false);
    });

    // F-04b: a timed-out EMAIL is ambiguous (SMTP may have accepted before the
    // response was lost) → the shared "outcome unknown" lead.
    it('a timed-out email send reports the shared outcome-unknown lead', async () => {
        setMailer(true, 'qsl@example.com');
        addQso({ callsign: 'A', uuid: 'u1' });

        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const errSpy = vi.spyOn(toasts, 'error');
        const warnSpy = vi.spyOn(toasts, 'warn');

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: /Send/ }));
        await flush();
        await flush();
        flushSync();

        expect(warnSpy).toHaveBeenCalledTimes(1);
        expect(warnSpy.mock.calls[0][0]).toMatch(/the outcome is unknown/);
        expect(warnSpy.mock.calls[0][0]).toMatch(/may already have been sent/);
        expect(errSpy).not.toHaveBeenCalled();
    });

    // A GENERIC (non-timeout) network failure on a send keeps its existing
    // cautious wording unchanged (ruling 2): still an error toast, still
    // "may still have gone out".
    it('a generic network failure on a send keeps the existing cautious wording', async () => {
        setMailer(true, 'qsl@example.com');
        addQso({ callsign: 'A', uuid: 'u1' });

        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('connection refused')))
        );
        const errSpy = vi.spyOn(toasts, 'error');

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: /Send/ }));
        await flush();
        await flush();
        flushSync();

        expect(errSpy).toHaveBeenCalledTimes(1);
        expect(errSpy.mock.calls[0][0]).toMatch(/may still have gone out/);
    });

    it('still shows the toast when the notification POST itself fails', async () => {
        addQso({ callsign: 'A', uuid: 'u1' });

        vi.stubGlobal(
            'fetch',
            vi.fn((input: RequestInfo | URL) => {
                if (urlOf(input) === '/v1/notifications') {
                    return Promise.reject(new Error('connection refused'));
                }
                return Promise.resolve(
                    new Response(JSON.stringify({ code: 'boom', message: 'kaboom' }), {
                        status: 500,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        const errSpy = vi.spyOn(toasts, 'error');

        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        await fireEvent.click(screen.getByRole('button', { name: /ADIF/ }));
        await flush();
        await flush();
        flushSync();

        // The record POST rejected, but the operator's toast still fired.
        expect(errSpy).toHaveBeenCalledTimes(1);
    });
});
