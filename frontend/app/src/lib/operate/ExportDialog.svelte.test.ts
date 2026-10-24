// The export dialog is the end-of-session action surface (download ADIF + email to
// QSL manager). The contact LIST lives on the Session panel, not here — this just
// summarises the count and gates the actions on it.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import ExportDialog from './ExportDialog.svelte';
import { setSessionActionGate } from './archiveStores.svelte';
import { operate, closeExport } from './state.svelte';
import { addSessionQso, session, _resetSessionForTests } from './session.svelte';
import { setMailer } from './mailer.svelte';
import { toasts } from '../ui/toasts.svelte';
import * as sessionExport from '../api/session-export';

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

// A daemon email reply that sent and stamped every requested UUID.
const sentReply = (uuids: string[]) => ({
    status: 'sent',
    sent: uuids,
    omitted: [],
    emailed: uuids,
    date: '',
});

const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });

const adif = (requested: number, exported: number, omitted: number) =>
    new Response('<EOH>', {
        status: 200,
        headers: {
            'X-SM-Requested-Count': String(requested),
            'X-SM-Exported-Count': String(exported),
            'X-SM-Omitted-Count': String(omitted),
        },
    });

// A fetch stub that records each request body and answers from `reply`, which
// may hold the response back until the test releases it.
function stubDaemon(reply: (uuids: string[]) => Response | Promise<Response>): {
    bodies: string[][];
} {
    const bodies: string[][] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
            if (urlOf(input) === '/v1/notifications') {
                return Promise.resolve(new Response(null, { status: 204 }));
            }
            const uuids = (JSON.parse(init?.body as string) as { uuids: string[] }).uuids;
            bodies.push(uuids);
            return Promise.resolve(reply(uuids));
        })
    );
    return { bodies };
}

async function click(name: RegExp): Promise<void> {
    await fireEvent.click(screen.getByRole('button', { name }));
    await flush();
    await flush();
    flushSync();
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

    // While the tab's archive is unproven, download and send are held, and the
    // dialog says why (ADR 0094 update) — the list may name another archive's rows.
    it('holds download and send while the archive is unproven, saying why', () => {
        addQso({ callsign: 'W1ABC', uuid: 'u-1' });
        setMailer(true, 'qsl@example.com');
        setSessionActionGate(() => 'Waiting until this tab has confirmed its archive.');
        try {
            operate.exportOpen = true;
            render(ExportDialog);
            expect(screen.getByRole('button', { name: 'ADIF' })).toBeDisabled();
            expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
            expect(screen.getByText(/confirmed its archive/)).toBeInTheDocument();
        } finally {
            setSessionActionGate(() => null);
        }
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
                    new Response(JSON.stringify(sentReply(sentUuids)), {
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
                    new Response(JSON.stringify(sentReply(sentUuids)), {
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

    // Slice 5 (W-0021 revision 3; Finding #6): the toast reports what the daemon
    // confirmed for the selection that was sent — B2-10 showed "Exported 4 QSOs"
    // over a one-record file.
    describe('export and email accounting', () => {
        let saved: string[];
        beforeEach(() => {
            saved = [];
            vi.spyOn(sessionExport, 'downloadTextFile').mockImplementation((name) => {
                saved.push(name);
            });
        });

        it('a short export names every part and still saves the file', async () => {
            for (const u of ['a', 'b', 'c', 'd']) addQso({ uuid: u });
            addQso({ callsign: 'NOID' });
            const d = stubDaemon(() => adif(4, 3, 1));
            const warn = vi.spyOn(toasts, 'warn');
            const info = vi.spyOn(toasts, 'info');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/ADIF/);
            expect(d.bodies.map((b) => [...b].sort())).toEqual([['a', 'b', 'c', 'd']]);
            expect(saved).toHaveLength(1);
            expect(warn).toHaveBeenCalledWith(
                'Exported 3 of 5 selected QSOs. 1 could not be found in this archive. 1 has no QSO ID.'
            );
            expect(info).not.toHaveBeenCalled();
        });

        it('collapses duplicate selections before sending and says so', async () => {
            addQso({ callsign: 'A', uuid: 'a' });
            session.qsos.push({ ...session.qsos[0] }); // a stored list from before merge-by-UUID
            const d = stubDaemon(() => adif(1, 1, 0));
            const info = vi.spyOn(toasts, 'info');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/ADIF/);
            expect(d.bodies).toEqual([['a']]);
            expect(info).toHaveBeenCalledWith(
                'Exported 1 QSO to ADIF. Duplicate selections were counted once.'
            );
        });

        it('a selection with no QSO IDs sends nothing and says why', async () => {
            addQso({ callsign: 'A' });
            const d = stubDaemon(() => adif(0, 0, 0));
            const warn = vi.spyOn(toasts, 'warn');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/ADIF/);
            expect(d.bodies).toEqual([]);
            expect(warn).toHaveBeenCalledWith(
                'None of these QSOs can be exported: they have no QSO ID.'
            );
        });

        it('unconfirmed counts save the file under a warning, never a success', async () => {
            addQso({ uuid: 'a' });
            stubDaemon(() => new Response('<EOH>', { status: 200 }));
            const warn = vi.spyOn(toasts, 'warn');
            const info = vi.spyOn(toasts, 'info');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/ADIF/);
            expect(saved).toHaveLength(1);
            expect(warn).toHaveBeenCalledWith(
                'The file was saved, but the daemon did not confirm how many QSOs it holds — check it before relying on it.'
            );
            expect(info).not.toHaveBeenCalled();
        });

        it('a row logged while the export is in flight does not change its count', async () => {
            addQso({ uuid: 'a' });
            addQso({ uuid: 'b' });
            let release: (r: Response) => void = () => {};
            stubDaemon(() => new Promise<Response>((r) => (release = r)));
            const info = vi.spyOn(toasts, 'info');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/ADIF/);
            addQso({ uuid: 'c' });
            release(adif(2, 2, 0));
            await flush();
            await flush();
            expect(info).toHaveBeenCalledWith('Exported 2 QSOs to ADIF');
        });

        it('an email partition violation marks nothing', async () => {
            setMailer(true, 'qsl@example.com');
            for (const u of ['a', 'b', 'c']) addQso({ uuid: u });
            // Right length, wrong set: c is lost behind a repeated a.
            stubDaemon(() =>
                json({ status: 'sent', sent: ['a', 'a'], omitted: ['b'], emailed: ['a'], date: '' })
            );
            const warn = vi.spyOn(toasts, 'warn');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/Send/);
            expect(session.qsos.filter((q) => q.emailed)).toEqual([]);
            expect(warn).toHaveBeenCalledWith(
                'Email sent to qsl@example.com, but the daemon did not confirm which QSOs it contained. No Session rows were marked as emailed.'
            );
        });

        it('a partial send with partial stamping marks only the stamped rows', async () => {
            setMailer(true, 'qsl@example.com');
            for (const u of ['a', 'b', 'c', 'd']) addQso({ uuid: u });
            stubDaemon(() =>
                json({
                    status: 'sent',
                    sent: ['a', 'b', 'c'],
                    omitted: ['d'],
                    emailed: ['a', 'b'],
                    date: '',
                })
            );
            const warn = vi.spyOn(toasts, 'warn');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/Send/);
            expect(
                session.qsos
                    .filter((q) => q.emailed)
                    .map((q) => q.uuid)
                    .sort()
            ).toEqual(['a', 'b']);
            expect(warn).toHaveBeenCalledWith(
                'Sent 3 of 4 QSOs to qsl@example.com; 1 could not be found in this archive, but only 2 were recorded as emailed; the rest may be offered again.'
            );
        });

        it('a send is accounted against its selection and recipient at the click', async () => {
            setMailer(true, 'qsl@example.com');
            addQso({ uuid: 'a' });
            addQso({ uuid: 'b' });
            let release: (r: Response) => void = () => {};
            const d = stubDaemon(() => new Promise<Response>((r) => (release = r)));
            const info = vi.spyOn(toasts, 'info');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/Send/);
            addQso({ uuid: 'c' });
            await fireEvent.input(screen.getByLabelText('Recipient'), {
                target: { value: 'other@example.com' },
            });
            release(json(sentReply(['a', 'b'])));
            await flush();
            await flush();
            expect(d.bodies.map((b) => [...b].sort())).toEqual([['a', 'b']]);
            expect(info).toHaveBeenCalledWith('Sent 2 QSOs to qsl@example.com');
            expect(session.qsos.find((q) => q.uuid === 'c')?.emailed).toBeFalsy();
        });

        // The label counts what the send is accounted against: distinct UUIDs
        // plus rows with no QSO ID — [A, A, no-ID] is 2, not 3.
        it('Send N shows the denominator, not the raw row count', () => {
            setMailer(true, 'qsl@example.com');
            addQso({ callsign: 'DONE', uuid: 'e', emailed: true });
            addQso({ callsign: 'NOID' });
            addQso({ callsign: 'A', uuid: 'a' });
            session.qsos.push({ ...session.qsos[0] }); // a stored list from before merge-by-UUID
            operate.exportOpen = true;
            render(ExportDialog);
            flushSync();
            expect(screen.getByRole('button', { name: 'Send 2' })).toBeInTheDocument();
        });

        it('a send selection with no QSO IDs sends nothing and says why', async () => {
            setMailer(true, 'qsl@example.com');
            addQso({ callsign: 'A' });
            const d = stubDaemon(() => json(sentReply([])));
            const warn = vi.spyOn(toasts, 'warn');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/Send/);
            expect(d.bodies).toEqual([]);
            expect(warn).toHaveBeenCalledWith(
                'None of these QSOs can be sent: they have no QSO ID.'
            );
        });
    });
});
