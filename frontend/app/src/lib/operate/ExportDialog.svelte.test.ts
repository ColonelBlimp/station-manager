// The export dialog is the end-of-session action surface (download ADIF + email to
// QSL manager). The contact LIST lives on the Session panel, not here — this just
// summarises the count and gates the actions on it.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import ExportDialog from './ExportDialog.svelte';
import { setSessionActionGate } from './archiveStores.svelte';
import { operate, closeExport } from './state.svelte';
import {
    session,
    setSessionPorts,
    sessionStateDrawn,
    _resetSessionForTests,
    type SessionQso,
    type SessionRowLike,
} from './session.svelte';
import { addSessionRow } from '../../test/sessionRows';
import { TEST_ARCHIVE } from '../../test/archive';
import { setMailer } from './mailer.svelte';
import { toasts } from '../ui/toasts.svelte';
import * as sessionExport from '../api/session-export';

const flush = () => new Promise((r) => setTimeout(r, 0));

const urlOf = (input: RequestInfo | URL): string =>
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

// Add a session QSO with only the fields a test cares about.
function addQso(over: Partial<Omit<SessionQso, 'id'>>): void {
    addSessionRow({
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

// The Session as the daemon holds it: an open session whose rows are whatever
// `held.rows` is when the page reads them. `reads()` counts those reads.
const SESSION = '01a1f83c-da20-7000-8000-0000000000a1';
const daemonRow = (uuid: string, emailed = false): SessionRowLike => ({
    uuid,
    call: uuid.toUpperCase(),
    timeOn: '1430',
    band: '20m',
    mode: 'FT8',
    rstSent: '',
    rstRcvd: '',
    name: '',
    country: '',
    comment: '',
    emailed,
});
async function daemonSession(held: {
    rows: SessionRowLike[];
    /** When set, each read of the rows waits for it before answering. */
    hold?: Promise<void>;
    /** When true, a read of the rows fails. */
    fail?: boolean;
}): Promise<() => number> {
    let reads = 0;
    setSessionPorts({
        readRows: async () => {
            reads++;
            if (held.hold) await held.hold;
            if (held.fail) return { kind: 'error', message: 'Cannot reach the daemon.' };
            return Promise.resolve({
                kind: 'ok',
                archiveId: TEST_ARCHIVE,
                generation: 4,
                sessionId: SESSION,
                members: held.rows,
            });
        },
        readState: () => Promise.resolve(),
        archive: () => TEST_ARCHIVE,
    });
    sessionStateDrawn({
        archiveId: TEST_ARCHIVE,
        generation: 4,
        state: 'open',
        session: { id: SESSION, openedAt: '2026-11-25T09:15:00Z', sittingStartedAt: null },
    });
    await flush();
    return () => reads;
}
const emailedNow = (): (string | undefined)[] =>
    session.qsos
        .filter((q) => q.emailed)
        .map((q) => q.uuid)
        .sort();

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
        addSessionRow({
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

    it('defaults a resend to the not-yet-emailed delta, and the mark comes from the daemon’s rows', async () => {
        setMailer(true, 'qsl@example.com');
        const held = { rows: [daemonRow('u2'), daemonRow('u1', true)] };
        const reads = await daemonSession(held);

        let sentUuids: string[] = [];
        vi.stubGlobal(
            'fetch',
            vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
                sentUuids = (JSON.parse(init?.body as string) as { uuids: string[] }).uuids;
                held.rows = [daemonRow('u2', true), daemonRow('u1', true)]; // the daemon stamps it
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
        const before = reads();

        await fireEvent.click(screen.getByRole('button', { name: /Send/ }));
        await flush();
        await flush();
        flushSync();

        // Only the not-yet-emailed QSO was sent…
        expect(sentUuids).toEqual(['u2']);
        // …the rows were read again, and it is now marked because its record
        // says so, so a further resend would exclude it.
        expect(reads()).toBe(before + 1);
        expect(emailedNow()).toEqual(['u1', 'u2']);
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

        it('after an email partition violation the window marks nothing itself', async () => {
            setMailer(true, 'qsl@example.com');
            // The daemon's records: nothing is stamped, whatever the reply claims.
            const held = { rows: ['c', 'b', 'a'].map((u) => daemonRow(u)) };
            await daemonSession(held);
            // Right length, wrong set: c is lost behind a repeated a.
            stubDaemon(() =>
                json({ status: 'sent', sent: ['a', 'a'], omitted: ['b'], emailed: ['a'], date: '' })
            );
            const warn = vi.spyOn(toasts, 'warn');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/Send/);
            expect(emailedNow()).toEqual([]);
            expect(warn).toHaveBeenCalledWith(
                'Email sent to qsl@example.com, but the daemon did not confirm which QSOs it contained. The Session shows the contacts it recorded as emailed.'
            );
        });

        it('a partial send shows exactly the contacts whose RECORD says emailed', async () => {
            setMailer(true, 'qsl@example.com');
            const held = { rows: ['d', 'c', 'b', 'a'].map((u) => daemonRow(u)) };
            await daemonSession(held);
            // The reply's list and the records differ on purpose (the reply says
            // a and b; the records, read afterwards, say a only): what the panel
            // shows must be the records, not the reply written in by the window.
            stubDaemon(() => {
                held.rows = ['d', 'c', 'b', 'a'].map((u) => daemonRow(u, u === 'a'));
                return json({
                    status: 'sent',
                    sent: ['a', 'b', 'c'],
                    omitted: ['d'],
                    emailed: ['a', 'b'],
                    date: '',
                });
            });
            const warn = vi.spyOn(toasts, 'warn');
            operate.exportOpen = true;
            render(ExportDialog);
            await click(/Send/);
            expect(emailedNow()).toEqual(['a']);
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

// The "already emailed" mark is the contact's record, read from the daemon. So
// between a send and the read that shows its stamps the rows still say "not
// emailed", and the default Send would mail the same contacts again (Codex's
// review of the cutover, codex-273). Send is held until a read TAKEN AFTER the
// send has answered: while that read is out, and when it has failed.
describe('ExportDialog after a send, before the Session has been read again', () => {
    const PENDING = /checked against the daemon/;
    const sendButton = (): HTMLButtonElement => screen.getByRole('button', { name: /^Send/ });

    async function sendOnce(held: {
        rows: SessionRowLike[];
        hold?: Promise<void>;
        fail?: boolean;
    }): Promise<{ bodies: string[][]; reads: () => number }> {
        setMailer(true, 'qsl@example.com');
        const reads = await daemonSession(held);
        const d = stubDaemon((uuids) => json(sentReply(uuids)));
        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        return { bodies: d.bodies, reads };
    }

    it('with the read after the send still out, reopening the dialog cannot send the contact again', async () => {
        const held: { rows: SessionRowLike[]; hold?: Promise<void> } = { rows: [daemonRow('u1')] };
        const { bodies } = await sendOnce(held);
        let release: () => void = () => {};
        held.hold = new Promise<void>((r) => (release = r)); // the read after the send is held
        await click(/^Send/);
        expect(bodies).toEqual([['u1']]);

        operate.exportOpen = true; // the operator opens Export again
        flushSync();
        expect(screen.getByText(PENDING)).toBeInTheDocument();
        expect(sendButton()).toBeDisabled();
        await fireEvent.click(sendButton());
        await flush();
        expect(bodies).toEqual([['u1']]); // nothing was sent again

        // The read answers: the record says u1 was emailed, and the hold is lifted.
        held.rows = [daemonRow('u1', true)];
        release();
        await flush();
        await flush();
        flushSync();
        expect(screen.queryByText(PENDING)).not.toBeInTheDocument();
        expect(screen.getByText(/1 already sent this session/)).toBeInTheDocument();
        expect(bodies).toEqual([['u1']]);
    });

    it('with that read failed, Send stays held, and opening the dialog asks for the rows again', async () => {
        const held: { rows: SessionRowLike[]; fail?: boolean } = {
            rows: [daemonRow('u2'), daemonRow('u1')],
        };
        const { bodies, reads } = await sendOnce(held);
        held.fail = true; // the read after the send fails
        await click(/^Send/);
        expect(bodies).toHaveLength(1);
        const afterSend = reads();

        operate.exportOpen = true;
        flushSync();
        await flush();
        expect(reads()).toBe(afterSend + 1); // opening it asked again
        expect(screen.getByText(PENDING)).toBeInTheDocument();
        expect(sendButton()).toBeDisabled();
        await fireEvent.click(sendButton());
        await flush();
        expect(bodies).toHaveLength(1);

        // The daemon can be read again; the next opening shows the records.
        closeExport();
        flushSync();
        held.fail = false;
        held.rows = [daemonRow('u2', true), daemonRow('u1', true)];
        operate.exportOpen = true;
        flushSync();
        await flush();
        await flush();
        flushSync();
        expect(screen.queryByText(PENDING)).not.toBeInTheDocument();
        expect(screen.getByText(/2 already sent this session/)).toBeInTheDocument();
    });

    it('holds Send the same way when the send’s outcome is not known', async () => {
        setMailer(true, 'qsl@example.com');
        const held: { rows: SessionRowLike[]; fail?: boolean } = { rows: [daemonRow('u1')] };
        await daemonSession(held);
        let posts = 0;
        vi.stubGlobal(
            'fetch',
            vi.fn(() => {
                posts++;
                return Promise.reject(new TypeError('network'));
            })
        );
        operate.exportOpen = true;
        render(ExportDialog);
        flushSync();
        held.fail = true; // and the daemon cannot be read either
        await click(/^Send/);
        expect(posts).toBe(1);

        // The email may have gone out. Until the records can be read, no resend.
        expect(screen.getByText(PENDING)).toBeInTheDocument();
        expect(sendButton()).toBeDisabled();
    });
});
