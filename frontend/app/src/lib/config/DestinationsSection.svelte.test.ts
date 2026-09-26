import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import DestinationsSection from './DestinationsSection.svelte';
import { _resetBindingsForTests } from './bindings.svelte';
import { toasts, toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import { takeLogbookMissingFrom, takeLogbookHandoffLogbook } from '../router.svelte';

/*
    ADR 0082 part 9 (W-0021 5E): the rendered "Destinations for <archive>".
    The pill is what the daemon holds; the switches are the draft; a switch
    never claims more than the daemon saved; per-logbook fields are masked;
    the queue belongs to each logbook's binding.
*/

const TYPES = {
    types: [
        {
            type: 'qrz',
            display_name: 'QRZ Logbook',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'api_key', label: 'API key', kind: 'password', scope: 'logbook' },
            ],
        },
        {
            type: 'smcloud',
            display_name: 'SM Cloud backup',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'Service URL', kind: 'text', scope: 'station' },
                {
                    key: 'logbook',
                    label: 'Cloud logbook name',
                    kind: 'text',
                    clearable: true,
                    scope: 'logbook',
                },
            ],
        },
        {
            type: 'clublog',
            display_name: 'ClubLog',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'email', label: 'Account email', kind: 'text', scope: 'logbook' },
                {
                    key: 'callsign',
                    label: 'Callsign',
                    kind: 'text',
                    scope: 'logbook',
                    defaults_to: 'logbook_callsign',
                },
            ],
        },
    ],
};

const row = (id: number, name: string, over: Record<string, unknown> = {}) => ({
    logbook_id: id,
    logbook_uuid: `u${id}`,
    logbook_name: name,
    logbook_callsign: 'M0ABC',
    bound: false,
    enabled: false,
    forwarder_name: '',
    credentials_set: [],
    queue: { waiting: 0, failed: 0, in_flight: 0 },
    ...over,
});

const EXTRA = 'qrz.u2';

function view(over: Record<string, unknown> = {}) {
    return {
        archive_id: 'A1',
        archive_label: 'Home',
        restart_required: false,
        destinations: [
            {
                type: 'qrz',
                display_name: 'QRZ Logbook',
                account: { configured: true },
                state: 'mixed',
                reason: '',
                logbooks: [
                    row(1, 'Main', {
                        bound: true,
                        enabled: true,
                        forwarder_name: 'qrz',
                        credentials_set: ['api_key'],
                        queue: { waiting: 1, failed: 2, in_flight: 0 },
                    }),
                    row(2, 'Second', { bound: true, forwarder_name: EXTRA }),
                ],
            },
            {
                type: 'smcloud',
                display_name: 'SM Cloud backup',
                account: { configured: false },
                state: 'off',
                reason: 'its station account is incomplete: a required station field is not set',
                logbooks: [row(1, 'Main'), row(2, 'Second')],
            },
            {
                type: 'clublog',
                display_name: 'ClubLog',
                account: { configured: true, build_key: 'absent' },
                state: 'off',
                reason: '',
                logbooks: [row(1, 'Main'), row(2, 'Second')],
            },
        ],
        ...over,
    };
}

type Call = { url: string; method: string; body: unknown };
let calls: Call[] = [];
let current: () => unknown = () => view();
let putAnswer: () => Response | Promise<Response> = () => json(view());

function json(body: unknown, status = 200): Response {
    return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
    });
}

beforeEach(() => {
    calls = [];
    current = () => view();
    putAnswer = () => json(view());
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            const url = input;
            const method = init?.method ?? 'GET';
            calls.push({
                url,
                method,
                body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
            });
            if (url === '/v1/version')
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings')
                return Promise.resolve(method === 'PUT' ? putAnswer() : json(current()));
            if (url.endsWith('/queue/retry')) return Promise.resolve(json({ rearmed: 2 }));
            if (url.endsWith('/queue/clear')) return Promise.resolve(json({ discarded: 3 }));
            return Promise.resolve(json({}, 404));
        })
    );
});

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    _resetBindingsForTests();
    resetToasts();
    takeLogbookMissingFrom();
    takeLogbookHandoffLogbook();
});

async function renderLoaded(props: Record<string, unknown> = {}) {
    render(DestinationsSection, { props });
    await screen.findByText('QRZ Logbook');
}

// Rendered text with template line breaks collapsed, as a reader sees it.
const flat = (el: Element | null): string => (el?.textContent ?? '').replace(/\s+/g, ' ');

const card = (label: string): HTMLElement => {
    const summary = screen.getByText(label, { selector: 'span.font-semibold' }).closest('details');
    if (!summary) throw new Error(`no card for ${label}`);
    return summary;
};

describe('DestinationsSection', () => {
    it('D1: one card per destination; the pill is what the daemon holds', async () => {
        await renderLoaded();
        expect(screen.getAllByTestId('destination-card')).toHaveLength(3);
        expect(within(card('QRZ Logbook')).getByTestId('destination-state').textContent).toBe(
            'mixed'
        );
        expect(within(card('ClubLog')).getByTestId('destination-state').textContent).toBe(
            'disabled'
        );
        // The collapsed summary carries the destination's totals across its logbooks.
        expect(flat(card('QRZ Logbook').querySelector('summary'))).toContain(
            '1 waiting · 2 failed · 0 in flight'
        );
    });

    // Station review 2026-09-26: the heading names the archive itself (the
    // daemon's label) and carries no explanation paragraph — the manual owns
    // the how (the tab links it). The old paragraph also rendered "archiveHome".
    it('D1a: the heading names the active archive, with no paragraph under it', async () => {
        await renderLoaded();
        const heading = screen.getByRole('heading', { level: 2 });
        expect(heading.textContent?.trim()).toBe('Destinations for Home');
        expect(heading.closest('section')!.textContent).not.toMatch(/logbook by logbook/);
    });

    // Operator ruling 2026-09-26: the manual link is an ⓘ beside the heading,
    // named "How forwarding works" (tooltip and accessible name), opening the
    // Forwarding chapter in a new tab like the sidebar's Manual link — and it
    // sits OUTSIDE the h2, so the section's name stays the heading alone.
    it('D1d: an ⓘ beside the heading links the manual chapter in a new tab', async () => {
        await renderLoaded();
        const link = screen.getByRole('link', { name: 'How forwarding works' });
        expect(link.getAttribute('href')).toBe('/manual/#forwarding');
        expect(link.getAttribute('target')).toBe('_blank');
        expect(link.getAttribute('rel')).toBe('noopener');
        expect(link.getAttribute('title')).toBe('How forwarding works');
        expect(link.closest('h2')).toBeNull();
        expect(screen.getByRole('region', { name: 'Destinations for Home' })).toContainElement(
            link
        );
    });

    it('D1c: before the daemon names the archive, the heading says "this archive"', async () => {
        current = () => view({ archive_label: '' });
        await renderLoaded();
        expect(screen.getByRole('heading', { level: 2 }).textContent?.trim()).toBe(
            'Destinations for this archive'
        );
    });

    it('D1b: a mixed destination names the logbooks it is off for; an all-off one names none', async () => {
        await renderLoaded();
        expect(flat(within(card('QRZ Logbook')).getByTestId('destination-off-for'))).toBe(
            'off for Second'
        );
        expect(within(card('ClubLog')).queryByTestId('destination-off-for')).toBeNull();
    });

    it('D2: the every-logbook switch is tri-state and sets the rows; the pill does not move until saved', async () => {
        await renderLoaded();
        const all = screen.getByRole<HTMLInputElement>('checkbox', {
            name: 'QRZ Logbook: every logbook in this archive',
        });
        expect(all.indeterminate).toBe(true);
        await fireEvent.click(all);
        expect(
            screen.getByRole<HTMLInputElement>('checkbox', { name: 'QRZ Logbook for Main' }).checked
        ).toBe(true);
        expect(
            screen.getByRole<HTMLInputElement>('checkbox', { name: 'QRZ Logbook for Second' })
                .checked
        ).toBe(true);
        expect(within(card('QRZ Logbook')).getByTestId('destination-state').textContent).toBe(
            'mixed'
        );
        expect(card('QRZ Logbook').textContent).toContain('*');
    });

    it('D3: a destination that cannot be turned on says why and offers the fix only where it exists', async () => {
        const opened: string[] = [];
        await renderLoaded({
            hasAccountCard: (t: string) => t === 'smcloud',
            onOpenAccount: (t: string) => opened.push(t),
        });
        const note = within(card('SM Cloud backup')).getByTestId('destination-reason');
        expect(flat(note)).toMatch(/Can't be turned on here: its station account is incomplete/);
        expect(
            screen.getByRole<HTMLInputElement>('checkbox', { name: 'SM Cloud backup for Main' })
                .disabled
        ).toBe(true);
        await fireEvent.click(
            within(note).getByRole('button', { name: 'Open its station account' })
        );
        expect(opened).toEqual(['smcloud']);
        // The refusal carries the link, so no second pointer repeats it.
        expect(within(card('SM Cloud backup')).queryByTestId('account-pointer')).toBeNull();
        // Without a card to open, no button promises one.
        _resetBindingsForTests();
        document.body.innerHTML = '';
        await renderLoaded();
        expect(
            within(card('SM Cloud backup')).queryByRole('button', { name: /station account/ })
        ).toBeNull();
    });

    it('D3b: a refusal on a COMPLETE station account offers no account link — the account is not the gap', async () => {
        const base = view();
        const destinations = base.destinations.map((d) =>
            d.type === 'smcloud'
                ? {
                      ...d,
                      account: { configured: true },
                      reason: "SM Cloud can be bound only on the adopted Home archive until the identity-aware server lands; this archive's QSOs stay local until then",
                  }
                : d
        );
        current = () => ({ ...base, destinations });
        await renderLoaded({ hasAccountCard: (t: string) => t === 'smcloud' });
        const note = within(card('SM Cloud backup')).getByTestId('destination-reason');
        expect(flat(note)).toMatch(/only on the adopted Home archive/);
        expect(within(note).queryByRole('button', { name: /station account/ })).toBeNull();
    });

    it('D3c: a field that defaults to the logbook callsign shows that callsign until one is stored', async () => {
        const base = view();
        current = () => ({
            ...base,
            destinations: base.destinations.map((d) =>
                d.type === 'clublog'
                    ? {
                          ...d,
                          logbooks: [
                              row(1, 'Main'),
                              row(2, 'Second', { bound: true, credentials_set: ['callsign'] }),
                          ],
                      }
                    : d
            ),
        });
        await renderLoaded();
        const rows = within(card('ClubLog')).getAllByTestId('binding-row');
        expect(within(rows[0]).getByLabelText('Callsign').getAttribute('placeholder')).toBe(
            'M0ABC — the logbook’s callsign unless you type another'
        );
        // Stored: a status line, no box to put a placeholder in (ruling 2026-09-26).
        expect(within(rows[1]).queryByLabelText('Callsign')).toBeNull();
        expect(within(rows[1]).getAllByTestId('saved-status')).toHaveLength(1);
    });

    // Declutter ruling 2026-09-26: an idle row (nothing queued, failed or in
    // flight) shows no queue line and no disabled Retry (0) / Clear (0).
    it('D1e: an idle row shows no queue line; a row with work shows it', async () => {
        await renderLoaded();
        const rows = within(card('QRZ Logbook')).getAllByTestId('binding-row');
        expect(flat(rows[0])).toMatch(/1 waiting · 2 failed · 0 in flight/);
        expect(within(rows[1]).queryByText(/waiting ·/)).toBeNull();
        expect(within(rows[1]).queryByRole('button', { name: /Retry failed/ })).toBeNull();
    });

    // Declutter ruling 2026-09-26: one unsaved-change star, on the card title.
    it('D1f: an edit stars the card title only, not the row as well', async () => {
        await renderLoaded();
        await fireEvent.click(screen.getByRole('checkbox', { name: 'QRZ Logbook for Second' }));
        const stars = within(card('QRZ Logbook')).getAllByTitle('Unsaved changes');
        expect(stars).toHaveLength(1);
        expect(stars[0].closest('summary')).not.toBeNull();
    });

    // Declutter ruling 2026-09-26: the restart banner is one short line with
    // its own Restart daemon button, handed down from Settings.
    it('D1g: the restart banner is one line with its own Restart daemon button', async () => {
        current = () => view({ restart_required: true });
        let restarts = 0;
        await renderLoaded({ onRestart: () => restarts++ });
        const banner = screen.getByTestId('bindings-restart');
        expect(flat(banner).trim()).toMatch(
            /^⚠?\s*Saved changes apply after a restart\.\s*Restart daemon$/
        );
        await fireEvent.click(within(banner).getByRole('button', { name: 'Restart daemon' }));
        expect(restarts).toBe(1);
    });

    // Ruling 2026-09-26: with a complete station account there is no refusal
    // to carry the link, so the card points at where its account lives.
    it('D3d: a destination with a station card points at it; one without a card does not', async () => {
        const base = view();
        current = () => ({
            ...base,
            destinations: base.destinations.map((d) =>
                d.type === 'smcloud' ? { ...d, account: { configured: true }, reason: '' } : d
            ),
        });
        const opened: string[] = [];
        await renderLoaded({
            hasAccountCard: (t: string) => t === 'smcloud',
            accountTitle: () => 'SM Cloud service and token',
            onOpenAccount: (t: string) => opened.push(t),
        });
        const pointer = within(card('SM Cloud backup')).getByTestId('account-pointer');
        await fireEvent.click(
            within(pointer).getByRole('button', { name: 'Show SM Cloud service and token' })
        );
        expect(opened).toEqual(['smcloud']);
        expect(within(card('QRZ Logbook')).queryByTestId('account-pointer')).toBeNull();
    });

    it('D4: turning a row on without its key marks the field, restores the switch, and sends nothing', async () => {
        await renderLoaded();
        const second = screen.getByRole<HTMLInputElement>('checkbox', {
            name: 'QRZ Logbook for Second',
        });
        await fireEvent.click(second);
        await fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }));
        expect(calls.some((c) => c.method === 'PUT')).toBe(false);
        expect(second.checked).toBe(false);
        // An outcome is a toast (ruling 2026-09-26); no inline box.
        await vi.waitFor(() =>
            expect(
                toastSaid(
                    'error',
                    (m) =>
                        m ===
                        'Save failed: QRZ Logbook for Second: API key is required to turn it on.'
                )
            ).toBe(true)
        );
        expect(screen.queryByTestId('bindings-refusal')).toBeNull();
        // The switch went back, but the card stays OPEN: it holds the marked
        // field the message names (station drill C.5 found it collapsing).
        expect((card('QRZ Logbook') as HTMLDetailsElement).open).toBe(true);
        const rows = within(card('QRZ Logbook')).getAllByTestId('binding-row');
        const keyInput = within(rows[1]).getByLabelText('API key');
        expect(keyInput.getAttribute('aria-invalid')).toBe('true');
        expect(within(rows[1]).getByText('Required to turn this on.')).toBeInTheDocument();
    });

    it('D5: a stored key reads as saved, not as a box; a save sends the changed rows and the restart banner follows', async () => {
        await renderLoaded();
        putAnswer = () => json(view({ restart_required: true }));
        vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        const rows = within(card('QRZ Logbook')).getAllByTestId('binding-row');
        expect(within(rows[0]).getByTestId('saved-status').textContent).toMatch(/✓\s*Saved/);
        expect(within(rows[0]).queryByLabelText('API key')).toBeNull();
        expect(document.body.textContent).not.toMatch(/leave blank to keep/);
        await fireEvent.click(screen.getByRole('checkbox', { name: 'QRZ Logbook for Second' }));
        const typed = within(rows[1]).getByLabelText('API key');
        expect(typed.getAttribute('type')).toBe('password');
        await fireEvent.input(typed, { target: { value: 'KEY-2' } });
        await fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }));
        await screen.findByTestId('bindings-restart');
        const put = calls.find((c) => c.method === 'PUT');
        expect(put?.body).toEqual({
            destinations: [
                {
                    type: 'qrz',
                    logbooks: [{ logbook_id: 2, enabled: true, credentials: { api_key: 'KEY-2' } }],
                },
            ],
        });
    });

    it('D5b: every binding editor is disabled for the whole save', async () => {
        await renderLoaded();
        let release!: () => void;
        const gate = new Promise<void>((r) => (release = r));
        putAnswer = async () => {
            await gate;
            return json(view());
        };
        vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        await fireEvent.click(screen.getByRole('checkbox', { name: 'QRZ Logbook for Main' }));
        await fireEvent.click(screen.getByRole('button', { name: 'Save destinations' }));
        await vi.waitFor(() =>
            expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled()
        );

        expect(
            screen.getByRole('checkbox', { name: 'QRZ Logbook: every logbook in this archive' })
        ).toBeDisabled();
        expect(screen.getByRole('checkbox', { name: 'QRZ Logbook for Second' })).toBeDisabled();
        for (const input of within(card('QRZ Logbook')).getAllByLabelText('API key')) {
            expect(input).toBeDisabled();
        }
        for (const button of within(card('QRZ Logbook')).getAllByRole('button', {
            name: 'Show value',
        })) {
            expect(button).toBeDisabled();
        }
        expect(
            within(card('QRZ Logbook')).getByRole('button', { name: 'Remove API key' })
        ).toBeDisabled();
        expect(
            within(card('QRZ Logbook')).getByRole('button', { name: 'Replace API key' })
        ).toBeDisabled();

        release();
        await vi.waitFor(() =>
            expect(screen.getByRole('button', { name: 'Save destinations' })).toBeDisabled()
        );
    });

    it('D6: each binding carries its own queue: retry and clear go to ITS name and refresh the counts', async () => {
        await renderLoaded();
        vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        const retry = screen.getByRole('button', {
            name: 'Retry failed uploads for QRZ Logbook for Main',
        });
        expect(retry.textContent).toContain('Retry failed (2)');
        await fireEvent.click(retry);
        await vi.waitFor(() =>
            expect(calls.some((c) => c.url === '/v1/forwarder/qrz/queue/retry')).toBe(true)
        );
        // Nothing queued for Second: no queue controls at all (declutter ruling).
        expect(
            screen.queryByRole('button', { name: 'Clear the queue for QRZ Logbook for Second' })
        ).toBeNull();
        await fireEvent.click(
            screen.getByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' })
        );
        await vi.waitFor(() =>
            expect(calls.some((c) => c.url === '/v1/forwarder/qrz/queue/clear')).toBe(true)
        );
        // Load, then a refresh after each action.
        await vi.waitFor(() =>
            expect(
                calls.filter((c) => c.url === '/v1/qso-archives/A1/bindings' && c.method === 'GET')
                    .length
            ).toBeGreaterThanOrEqual(3)
        );
    });

    it('D7: the gap link opens THIS logbook with its own binding', async () => {
        await renderLoaded();
        const link = within(card('QRZ Logbook')).getByRole('link', {
            name: 'Show the QSOs of Main not on QRZ Logbook',
        });
        expect(link.getAttribute('href')).toContain('missing_from=qrz&logbook=1');
    });

    it('D8: a build without the application key says its uploads wait', async () => {
        await renderLoaded();
        expect(flat(within(card('ClubLog')).getByTestId('build-key-absent'))).toMatch(
            /uploads wait in the queue/
        );
        expect(within(card('QRZ Logbook')).queryByTestId('build-key-absent')).toBeNull();
    });

    it('D9: a stored value can be removed only from a row that is off', async () => {
        await renderLoaded();
        const rows = within(card('QRZ Logbook')).getAllByTestId('binding-row');
        expect(within(rows[0]).queryByRole('button', { name: 'Remove API key' })).toBeNull();
        await fireEvent.click(screen.getByRole('checkbox', { name: 'QRZ Logbook for Main' }));
        await fireEvent.click(within(rows[0]).getByRole('button', { name: 'Remove API key' }));
        expect(within(rows[0]).getByTestId('removal-pending').textContent).toMatch(
            /Removed when you save/
        );
    });
});

// ---- Queue actions per binding (ported from the station-entry tab, W-0005 /
// W-0010 outcome 9): each binding row owns its queue now (ADR 0082). ----

interface QueueOpts {
    clearResult?: { status: number; body: unknown };
    clearFails?: 'timeout' | 'network';
    retryResult?: { status: number; body: unknown };
    refreshFails?: boolean;
    deferRefresh?: boolean;
}

// A stateful daemon: the view's counts follow the actions. A clear zeroes
// waiting + failed and PRESERVES in-flight (a clear never touches the batch a
// worker is sending); a retry moves failed into waiting. Only the qrz binding
// of Main is acted on; the smcloud binding of Main carries one failure so the
// gap link's stamping rule can be seen.
function mockQueues(
    start: { waiting: number; failed: number; in_flight: number },
    opts: QueueOpts = {}
) {
    const q = { ...start };
    const actions = { clear: [] as string[], retry: [] as string[] };
    let gets = 0;
    let release: (() => void) | null = null;
    const liveView = () => ({
        archive_id: 'A1',
        archive_label: 'Home',
        restart_required: false,
        destinations: [
            {
                type: 'qrz',
                display_name: 'QRZ Logbook',
                account: { configured: true },
                state: 'on',
                reason: '',
                logbooks: [
                    row(1, 'Main', {
                        bound: true,
                        enabled: true,
                        forwarder_name: 'qrz',
                        credentials_set: ['api_key'],
                        queue: { ...q },
                    }),
                ],
            },
            {
                type: 'smcloud',
                display_name: 'SM Cloud backup',
                account: { configured: true },
                state: 'on',
                reason: '',
                logbooks: [
                    row(1, 'Main', {
                        bound: true,
                        enabled: true,
                        forwarder_name: 'smcloud',
                        queue: { waiting: 0, failed: 1, in_flight: 0 },
                    }),
                ],
            },
        ],
    });
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            const url = input;
            const method = init?.method ?? 'GET';
            if (url === '/v1/version')
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url.endsWith('/queue/retry')) {
                actions.retry.push(
                    decodeURIComponent(url.split('/v1/forwarder/')[1].split('/queue/')[0])
                );
                const r = opts.retryResult ?? { status: 200, body: { rearmed: q.failed } };
                if (r.status < 300) {
                    q.waiting += q.failed;
                    q.failed = 0;
                }
                return Promise.resolve(json(r.body, r.status));
            }
            if (url.endsWith('/queue/clear')) {
                actions.clear.push(
                    decodeURIComponent(url.split('/v1/forwarder/')[1].split('/queue/')[0])
                );
                if (opts.clearFails) {
                    // The daemon deletes before it answers, so a post-dispatch
                    // failure — timeout or reset — may have committed.
                    q.waiting = 0;
                    q.failed = 0;
                    const e = new Error(
                        opts.clearFails === 'timeout' ? 'request timed out' : 'connection reset'
                    );
                    if (opts.clearFails === 'timeout') e.name = 'TimeoutError';
                    return Promise.reject(e);
                }
                const r = opts.clearResult ?? {
                    status: 200,
                    body: { discarded: q.waiting + q.failed },
                };
                if (r.status < 300) {
                    q.waiting = 0;
                    q.failed = 0;
                }
                return Promise.resolve(json(r.body, r.status));
            }
            if (url === '/v1/qso-archives/A1/bindings' && method === 'GET') {
                gets++;
                if (gets > 1 && opts.refreshFails)
                    return Promise.reject(new Error('connection refused'));
                if (gets > 1 && opts.deferRefresh) {
                    return new Promise<Response>((resolve) => {
                        release = () => resolve(json(liveView()));
                    });
                }
                return Promise.resolve(json(liveView()));
            }
            return Promise.resolve(json({}, 404));
        })
    );
    return { actions, release: () => release?.() };
}

const qrzRow = (): HTMLElement => within(card('QRZ Logbook')).getAllByTestId('binding-row')[0];
const toastSaid = (level: string, pred: (m: string) => boolean) =>
    toastsState.items.some((t) => t.level === level && pred(t.message));

describe('DestinationsSection — each binding owns its queue', () => {
    it('D10 (U12): clears after confirm, reports, stays disabled through the refresh, keeps in-flight', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        const { actions, release } = mockQueues(
            { waiting: 5, failed: 0, in_flight: 2 },
            { deferRefresh: true }
        );
        render(DestinationsSection);
        const btn = await screen.findByRole('button', {
            name: 'Clear the queue for QRZ Logbook for Main',
        });
        expect(btn.textContent).toContain('Clear queue (5)');
        await fireEvent.click(btn);
        await vi.waitFor(() => expect(actions.clear).toEqual(['qrz']));
        await vi.waitFor(() =>
            expect(toastSaid('info', (m) => m.includes('Cleared 5'))).toBe(true)
        );
        // Refresh in flight: disabled "Clearing…", never a stale re-enabled count.
        expect(
            within(qrzRow()).getByRole('button', {
                name: 'Clear the queue for QRZ Logbook for Main',
            })
        ).toBeDisabled();
        expect(within(qrzRow()).getByText('Clearing…')).toBeInTheDocument();
        release();
        await vi.waitFor(() =>
            expect(
                within(qrzRow()).getByText('0 waiting · 0 failed · 2 in flight')
            ).toBeInTheDocument()
        );
        expect(
            within(qrzRow()).getByRole('button', {
                name: 'Clear the queue for QRZ Logbook for Main',
            })
        ).toBeDisabled();
        expect(actions.clear).toEqual(['qrz']);
    });

    it('D11 (U12b): cancelling the confirm sends nothing', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(false);
        const { actions } = mockQueues({ waiting: 5, failed: 0, in_flight: 0 });
        render(DestinationsSection);
        await fireEvent.click(
            await screen.findByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' })
        );
        expect(actions.clear).toEqual([]);
    });

    it('D12 (U13): a failed clear surfaces the daemon message', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        mockQueues(
            { waiting: 3, failed: 0, in_flight: 0 },
            {
                clearResult: {
                    status: 404,
                    body: {
                        code: 'unknown_forwarder',
                        message: 'no such destination binding in the active archive',
                    },
                },
            }
        );
        render(DestinationsSection);
        await fireEvent.click(
            await screen.findByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' })
        );
        await vi.waitFor(() =>
            expect(
                toastSaid('error', (m) => m === 'no such destination binding in the active archive')
            ).toBe(true)
        );
    });

    it('D13 (U14): a failed refresh after a clear hides the stale count and its actions', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        mockQueues({ waiting: 5, failed: 0, in_flight: 0 }, { refreshFails: true });
        render(DestinationsSection);
        await fireEvent.click(
            await screen.findByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' })
        );
        await vi.waitFor(() => expect(toastSaid('warn', () => true)).toBe(true));
        expect(within(qrzRow()).queryByRole('button', { name: /Clear the queue/ })).toBeNull();
        expect(within(qrzRow()).queryByText(/waiting ·/)).toBeNull();
        expect(within(qrzRow()).getByTestId('queue-stale')).toBeInTheDocument();
        // …nor in the card's summary, whose total included it.
        expect(
            within(card('QRZ Logbook')).queryByTitle(
                'Uploads waiting to send · failed and not retried · currently being sent'
            )
        ).toBeNull();
    });

    it('D14 (U15): a clear timeout is reconciled, never reported as a failure', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        mockQueues({ waiting: 5, failed: 0, in_flight: 2 }, { clearFails: 'timeout' });
        render(DestinationsSection);
        await fireEvent.click(
            await screen.findByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' })
        );
        await vi.waitFor(() =>
            expect(
                within(qrzRow()).getByText('0 waiting · 0 failed · 2 in flight')
            ).toBeInTheDocument()
        );
        expect(toastSaid('warn', () => true)).toBe(true);
        expect(toastSaid('error', () => true)).toBe(false);
    });

    it('D15 (U16): a post-dispatch connection failure is reconciled too', async () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        mockQueues({ waiting: 5, failed: 0, in_flight: 2 }, { clearFails: 'network' });
        render(DestinationsSection);
        await fireEvent.click(
            await screen.findByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' })
        );
        await vi.waitFor(() =>
            expect(
                within(qrzRow()).getByText('0 waiting · 0 failed · 2 in flight')
            ).toBeInTheDocument()
        );
        expect(toastSaid('warn', () => true)).toBe(true);
        expect(toastSaid('error', () => true)).toBe(false);
    });

    it('D16 (U17): retry needs no confirm, reports, stays disabled through the refresh, failed moves to waiting', async () => {
        const confirm = vi.spyOn(window, 'confirm');
        const { actions, release } = mockQueues(
            { waiting: 1, failed: 2, in_flight: 0 },
            { deferRefresh: true }
        );
        render(DestinationsSection);
        const btn = await screen.findByRole('button', {
            name: 'Retry failed uploads for QRZ Logbook for Main',
        });
        await fireEvent.click(btn);
        expect(confirm).not.toHaveBeenCalled();
        await vi.waitFor(() => expect(actions.retry).toEqual(['qrz']));
        await vi.waitFor(() =>
            expect(
                toastSaid(
                    'info',
                    (m) => m === 'Re-queued 2 failed uploads for QRZ Logbook for Main.'
                )
            ).toBe(true)
        );
        expect(within(qrzRow()).getByText('Retrying…')).toBeInTheDocument();
        release();
        await vi.waitFor(() =>
            expect(
                within(qrzRow()).getByText('3 waiting · 0 failed · 0 in flight')
            ).toBeInTheDocument()
        );
        expect(
            within(qrzRow()).getByRole('button', {
                name: 'Retry failed uploads for QRZ Logbook for Main',
            })
        ).toBeDisabled();
    });

    it('D17 (U17b): a refused retry surfaces the daemon message', async () => {
        mockQueues(
            { waiting: 0, failed: 1, in_flight: 0 },
            {
                retryResult: {
                    status: 400,
                    body: {
                        code: 'forwarder_disabled',
                        message: 'forwarder has no running worker',
                    },
                },
            }
        );
        render(DestinationsSection);
        await fireEvent.click(
            await screen.findByRole('button', {
                name: 'Retry failed uploads for QRZ Logbook for Main',
            })
        );
        await vi.waitFor(() =>
            expect(toastSaid('error', (m) => m === 'forwarder has no running worker')).toBe(true)
        );
    });

    it('D18 (U18): the gap link is offered for a stamping type only and hands the logbook over', async () => {
        mockQueues({ waiting: 0, failed: 2, in_flight: 0 });
        render(DestinationsSection);
        const link = await screen.findByRole('link', {
            name: 'Show the QSOs of Main not on QRZ Logbook',
        });
        expect(link.getAttribute('href')).toBe('/logbook?missing_from=qrz&logbook=1');
        expect(screen.queryByRole('link', { name: /not on SM Cloud/ })).toBeNull();
        expect(flat(link.parentElement)).toMatch(
            /every QSO not on QRZ Logbook, not only the failed uploads/
        );
        await fireEvent.click(link);
        expect(takeLogbookMissingFrom()).toBe('qrz');
        expect(takeLogbookHandoffLogbook()).toBe(1);
    });

    it('D19 (U18b): no gap link when nothing has failed', async () => {
        mockQueues({ waiting: 3, failed: 0, in_flight: 0 });
        render(DestinationsSection);
        await screen.findByRole('button', { name: 'Clear the queue for QRZ Logbook for Main' });
        expect(screen.queryByRole('link', { name: /not on QRZ/ })).toBeNull();
    });
});
