import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import ForwardingSection from './ForwardingSection.svelte';
import { forwardingState } from './forwarding.svelte';
import { bindingsState, _resetBindingsForTests } from './bindings.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    FORWARDING TAB — WHAT THE OPERATOR SEES (ADR 0082, W-0021 5E).

    The tab holds two sections. "Destinations for <archive>" — the on/off
    switches, each logbook's own account fields and its queue — is pinned by
    DestinationsSection.svelte.test.ts. These rules pin the other one, "Station
    accounts": what every archive shares, from config.json.

    forwarding.svelte.test.ts pins what goes ON THE WIRE. These rules pin the
    half of the criterion that is only observable in the browser:

        …for a field the daemon marks clearable, I can also reset it to its
        default and see which default I got.

    The load-bearing one is U2. "Reset" must not be offered for a credential the
    daemon will refuse to clear — a control that silently does nothing is worse
    than no control, because the operator concludes the value WAS cleared. The
    state module refuses such a reset anyway (F5b/F5c), so without U2 the UI
    could sprout the button on every field and every wire rule would stay green.

    The new rules (U9–U11) pin what the split decides: a destination whose
    account is wholly per logbook (QRZ) has no station card; ClubLog's card says
    whether this build carries its application key; and a destination refused
    for an incomplete station account links to the card that fixes it.
*/

afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    resetToasts();
    _resetBindingsForTests();
    forwardingState.drafts = [];
    forwardingState.types = [];
    forwardingState.loaded = false;
    forwardingState.loading = false;
    forwardingState.saving = false;
    forwardingState.error = '';
});

const TYPES = {
    types: [
        {
            type: 'qrz',
            display_name: 'QRZ.com',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'api_key', label: 'API key', kind: 'password', scope: 'logbook' },
            ],
        },
        {
            type: 'smcloud',
            display_name: 'SM Cloud',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'URL', kind: 'text', scope: 'station' },
                { key: 'token', label: 'Bearer token', kind: 'password', scope: 'station' },
                // A station-scoped clearable field (fixture): the one reset control.
                {
                    key: 'region',
                    label: 'Region',
                    kind: 'text',
                    clearable: true,
                    help: 'Defaults to "eu".',
                    scope: 'station',
                },
                // Logbook-scoped AND clearable: owned by the bindings, never a
                // station field.
                {
                    key: 'logbook',
                    label: 'Cloud logbook',
                    kind: 'text',
                    clearable: true,
                    help: 'Defaults to "main".',
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
            ],
        },
    ],
};

const CONFIG = {
    forwarders: [
        { name: 'qrz', type: 'qrz', enabled: true, credentials_set: ['api_key'] },
        {
            name: 'smcloud',
            type: 'smcloud',
            enabled: true,
            // region stored: Reset is offered only on a stored value (ruling
            // 2026-09-26 — a control on nothing appears to work and does nothing).
            credentials_set: ['url', 'token', 'region', 'logbook'],
        },
        { name: 'clublog', type: 'clublog', enabled: false, credentials_set: [] },
        { name: 'mystery', type: 'mystery', enabled: false, credentials_set: ['token'] },
    ],
};

const row = (id: number, name: string) => ({
    logbook_id: id,
    logbook_uuid: `u${id}`,
    logbook_name: name,
    logbook_callsign: 'M0ABC',
    bound: false,
    enabled: false,
    forwarder_name: '',
    credentials_set: [],
    queue: { waiting: 0, failed: 0, in_flight: 0 },
});

type Dest = {
    type: string;
    display_name: string;
    account: Record<string, unknown>;
    state: string;
    reason: string;
};

function bindingsView(destinations: Dest[]) {
    return {
        archive_id: 'A1',
        archive_label: 'Home',
        restart_required: false,
        destinations: destinations.map((d) => ({ ...d, logbooks: [row(1, 'Main')] })),
    };
}

const DESTS: Dest[] = [
    {
        type: 'qrz',
        display_name: 'QRZ.com',
        account: { configured: true },
        state: 'off',
        reason: '',
    },
    {
        type: 'smcloud',
        display_name: 'SM Cloud',
        account: { configured: true, fields_set: ['url', 'token'] },
        state: 'off',
        reason: '',
    },
    {
        type: 'clublog',
        display_name: 'ClubLog',
        account: { configured: true, build_key: 'present' },
        state: 'off',
        reason: '',
    },
];

function mockDaemon(opts: { config?: unknown; dests?: Dest[] } = {}) {
    const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), {
            status,
            headers: { 'Content-Type': 'application/json' },
        });
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string) => {
            const url = input;
            if (url === '/v1/version')
                return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
            if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
            if (url === '/v1/qso-archives/A1/bindings')
                return Promise.resolve(json(bindingsView(opts.dests ?? DESTS)));
            return Promise.resolve(json(opts.config ?? CONFIG));
        })
    );
}

async function renderLoaded(opts: { config?: unknown; dests?: Dest[] } = {}) {
    mockDaemon(opts);
    render(ForwardingSection);
    await vi.waitFor(() => {
        expect(forwardingState.loaded).toBe(true);
        expect(bindingsState.loaded).toBe(true);
    });
}

const station = (): HTMLElement => screen.getByRole('region', { name: 'Station accounts' });
const destinations = (): HTMLElement =>
    screen.getByRole('region', { name: 'Destinations for Home' });

// Rendered text with template line breaks collapsed, as a reader sees it.
const flat = (el: Element | null): string => (el?.textContent ?? '').replace(/\s+/g, ' ');

// The station card whose title is exactly `label` (the star excluded).
function accountCard(label: string): HTMLDetailsElement {
    const found = within(station())
        .getAllByTestId('account-card')
        .find((c) => titleOf(c) === label);
    if (!found) throw new Error(`no station account card for ${label}`);
    return found as HTMLDetailsElement;
}

function titleOf(card: Element): string {
    return (card.querySelector('summary span.font-semibold')?.textContent ?? '')
        .replace('*', '')
        .trim();
}

describe('ForwardingSection', () => {
    // U1 — WHAT EVERY ARCHIVE SHARES IS LISTED, including an entry this build
    // cannot edit, which is round-tripped on save rather than dropped.
    it('U1: station accounts list the shared entries and name the unsupported one', async () => {
        await renderLoaded();
        // The station card says what it holds, so it no longer reads as a
        // second "SM Cloud" beside the destination (station drill C.4).
        expect(within(station()).getAllByTestId('account-card').map(titleOf)).toEqual([
            'SM Cloud service and token',
            'mystery',
        ]);
        expect(within(station()).getByText(/can't be edited here/)).toBeTruthy();
        // The real rule: it is present AND flagged as uneditable, so the
        // operator is not left wondering why it has no fields.
        expect(forwardingState.drafts.map((d) => d.name)).toContain('mystery');
    });

    // U2 — RESET IS OFFERED ONLY WHERE THE DAEMON WILL HONOUR IT. Exactly one
    // clearable STATION field exists in this fixture, so exactly one control
    // may appear in this section.
    it('U2: the reset control appears only for a clearable field', async () => {
        await renderLoaded();
        const resets = within(station()).queryAllByRole('button', { name: /reset to default/i });
        expect(resets).toHaveLength(1);

        // …and it belongs to the clearable STATION field, not merely to the same card.
        expect(resets[0].getAttribute('aria-label')).toBe('Reset to default Region');
        expect(
            within(station()).queryByRole('button', { name: 'Reset to default URL' })
        ).toBeNull();
    });

    // U3 — A PENDING RESET IS VISIBLE BEFORE SAVING, and reversible. Otherwise
    // "cleared" and "left blank" look identical right up until the save lands,
    // which is the confusion this whole feature exists to remove.
    it('U3: clicking reset shows a pending state that can be undone', async () => {
        await renderLoaded();
        await fireEvent.click(within(station()).getByRole('button', { name: /reset to default/i }));

        expect(within(station()).getByTestId('removal-pending').textContent).toMatch(
            /Resets to the default when you save/
        );
        const smcloud = forwardingState.drafts.find((d) => d.name === 'smcloud');
        expect(smcloud?.cleared).toContain('region');

        await fireEvent.click(
            within(station()).getByRole('button', { name: 'Undo removing Region' })
        );
        expect(within(station()).queryByTestId('removal-pending')).toBeNull();
        expect(forwardingState.drafts.find((d) => d.name === 'smcloud')?.cleared).not.toContain(
            'region'
        );
    });

    // U4 — A SET CREDENTIAL SAYS SO WITHOUT REVEALING ANYTHING. The placeholder
    // is the ONLY signal the daemon gives, and it must say "keep", because a
    // blank box is what preserves the stored value.
    it('U4: a stored credential reads as saved, with Replace; replacing it is masked and empty', async () => {
        await renderLoaded();
        const card = accountCard('SM Cloud service and token');
        expect(within(card).queryByLabelText('Bearer token')).toBeNull();
        expect(document.body.textContent).not.toMatch(/leave blank to keep/);
        await fireEvent.click(within(card).getByRole('button', { name: 'Replace Bearer token' }));
        const input = within(card).getByLabelText('Bearer token');
        // Masked, and never pre-filled with anything resembling the secret.
        expect(input.getAttribute('type')).toBe('password');
        expect(input.getAttribute('value') ?? '').toBe('');
    });

    it('U4b: cancelling an untouched replacement restores the pristine station draft', async () => {
        await renderLoaded();
        const card = accountCard('SM Cloud service and token');
        await fireEvent.click(within(card).getByRole('button', { name: 'Replace Bearer token' }));
        await fireEvent.click(
            within(card).getByRole('button', { name: 'Cancel replacing Bearer token' })
        );

        expect(forwardingState.dirty).toBe(false);
        expect(forwardingState.drafts.find((d) => d.name === 'smcloud')?.credentials).toEqual({});
        expect(within(station()).getByRole('button', { name: /^save$/i })).toBeDisabled();
    });

    // U5 — THE RESTART CAVEAT APPEARS ONLY WHEN THERE IS SOMETHING TO APPLY.
    // The workers bind their accounts at startup, so a saved change that is
    // not yet live is a state the operator has to be told about.
    it('U5: the restart notice is tied to unsaved changes', async () => {
        await renderLoaded();
        expect(within(station()).queryByText(/apply when the daemon restarts/i)).toBeNull();

        await fireEvent.click(within(station()).getByRole('button', { name: /reset to default/i }));
        expect(within(station()).getByText(/apply when the daemon restarts/i)).toBeTruthy();
    });

    // U6 — ACCOUNTS COLLAPSE, AND THE SUMMARY STILL ANSWERS THE SCAN. Collapsing
    // is only safe if the closed row still says which service it is — and it
    // claims no on/off: that is a binding of the archive, shown above.
    it('U6: each station account is a collapsed disclosure showing its name', async () => {
        await renderLoaded();
        const cards = within(station()).getAllByTestId<HTMLDetailsElement>('account-card');
        expect(cards).toHaveLength(2);
        for (const c of cards) expect(c.open).toBe(false);

        const summary = accountCard('SM Cloud service and token').querySelector('summary');
        // The service name, deliberately NOT the raw `name` key: with one entry
        // per type it always equals the type, a second rendering of one word.
        expect(summary?.textContent).not.toMatch(/\bsmcloud\b/);
        expect(summary?.textContent).not.toMatch(/\b(enabled|disabled)\b/);
        expect(within(station()).queryByRole('checkbox')).toBeNull();
        expect(accountCard('mystery').querySelector('summary')?.textContent).toContain(
            'unsupported'
        );
    });

    // U7 — AN EDITED CARD IS MARKED AND CANNOT BE COLLAPSED. This is the state
    // collapsing CREATES: an edit the operator cannot see, while the footer
    // reports only that something somewhere is unsaved. The marker names which
    // card; refusing to close means it cannot be hidden again by accident.
    it('U7: an edited account is starred and refuses to collapse', async () => {
        await renderLoaded();
        await fireEvent.click(within(station()).getByRole('button', { name: /reset to default/i }));

        const card = accountCard('SM Cloud service and token');
        const summary = card.querySelector('summary')!;
        expect(summary.textContent).toContain('*');
        expect(card.open).toBe(true);

        // Clicking the summary must NOT close it while edits are pending.
        await fireEvent.click(summary);
        expect(card.open).toBe(true);

        // …and Cancel is the way out. It clears the edit, which releases the
        // forced-open — the card collapses on its own, no second click needed.
        // (jsdom really does toggle <details> on summary click, verified
        // separately, so the assertion above is not passing vacuously.)
        await fireEvent.click(within(station()).getByRole('button', { name: /^cancel$/i }));
        expect(summary.textContent).not.toContain('*');
        expect(card.open).toBe(false);

        // And normal disclosure behaviour is restored, not left disabled.
        await fireEvent.click(summary);
        expect(card.open).toBe(true);
        await fireEvent.click(summary);
        expect(card.open).toBe(false);
    });

    // U7b — AND AN UNEDITED CARD IS NOT STARRED, or the marker means nothing.
    it('U7b: an untouched account carries no marker', async () => {
        await renderLoaded();
        expect(
            accountCard('SM Cloud service and token').querySelector('summary')?.textContent
        ).not.toContain('*');
    });

    // U8 — THE OPERATOR'S config.json LABEL WINS OVER THE BUILT-IN NAME, and an
    // unlabelled entry still shows the built-in rather than a blank. The whole
    // point is that the built-in lives in the binary, so changing it is a
    // release the operator cannot perform.
    it('U8: a config.json label overrides the built-in display name', async () => {
        await renderLoaded({
            config: {
                forwarders: [
                    { name: 'smcloud', type: 'smcloud', enabled: true, label: 'Shack cloud' },
                    { name: 'clublog', type: 'clublog', enabled: false },
                ],
            },
        });
        expect(within(station()).getAllByTestId('account-card').map(titleOf)).toEqual([
            'Shack cloud service and token',
        ]);
    });

    // U9 — A DESTINATION WHOSE ACCOUNT IS WHOLLY PER LOGBOOK HAS NO STATION
    // CARD. QRZ's key belongs to a logbook: it is edited on that logbook's row
    // above, and a station card for it would be an empty box. Its config.json
    // entry is still held, so a save round-trips it.
    it('U9: QRZ appears under the destinations, never as a station account', async () => {
        await renderLoaded();
        expect(within(station()).queryByText('QRZ.com')).toBeNull();
        expect(within(station()).queryByText('API key')).toBeNull();
        expect(within(station()).queryByText('Cloud logbook')).toBeNull();
        expect(
            within(destinations()).getByRole('checkbox', { name: 'QRZ.com for Main' })
        ).toBeInTheDocument();
        expect(forwardingState.drafts.map((d) => d.name)).toContain('qrz');
    });

    // U10 — CLUBLOG IS NOT A STATION ACCOUNT (operator ruling 2026-09-26, a
    // departure from ADR 0082 part 9). Its API key identifies the SOFTWARE and
    // is built in; its application password is the USER's, per logbook. With
    // the key present nothing is said; without it, ClubLog's own destination
    // card says so — never a station card.
    it('U10: ClubLog is never a station account, with or without the built-in key', async () => {
        await renderLoaded();
        expect(within(station()).queryByText('ClubLog')).toBeNull();
        expect(within(destinations()).queryByTestId('build-key-absent')).toBeNull();
    });

    it('U10b: a build without the key says so on ClubLog’s destination card only', async () => {
        const dests = DESTS.map((d) =>
            d.type === 'clublog' ? { ...d, account: { configured: true, build_key: 'absent' } } : d
        );
        await renderLoaded({ dests });
        expect(within(station()).queryByText('ClubLog')).toBeNull();
        expect(within(destinations()).getByTestId('build-key-absent')).toBeInTheDocument();
    });

    // U12 — THE TWO HALVES LOAD APART. A failed config.json read must not hide
    // the destinations (their own read succeeded), and retrying it must not
    // remount them — a remount reloads the bindings over the operator's edits.
    it('U12: a failed station-account load leaves the destinations working; its retry keeps their drafts', async () => {
        let configFails = true;
        let bindingGets = 0;
        const json = (body: unknown, status = 200) =>
            new Response(JSON.stringify(body), {
                status,
                headers: { 'Content-Type': 'application/json' },
            });
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string) => {
                if (url === '/v1/version')
                    return Promise.resolve(json({ instance: 'i', archive: { id: 'A1' } }));
                if (url === '/v1/forwarder-types') return Promise.resolve(json(TYPES));
                if (url === '/v1/qso-archives/A1/bindings') {
                    bindingGets++;
                    return Promise.resolve(json(bindingsView(DESTS)));
                }
                return Promise.resolve(configFails ? json({ message: 'boom' }, 500) : json(CONFIG));
            })
        );
        render(ForwardingSection);
        await vi.waitFor(() => expect(bindingsState.loaded).toBe(true));
        await vi.waitFor(() =>
            expect(within(station()).getByText(/Couldn’t load the station accounts/)).toBeTruthy()
        );
        const qrzMain = within(destinations()).getByRole<HTMLInputElement>('checkbox', {
            name: 'QRZ.com for Main',
        });
        await fireEvent.click(qrzMain);
        expect(bindingsState.dirty).toBe(true);

        configFails = false;
        await fireEvent.click(within(station()).getByRole('button', { name: 'Retry' }));
        await vi.waitFor(() => expect(forwardingState.loaded).toBe(true));
        expect(within(station()).getAllByTestId('account-card').length).toBeGreaterThan(0);
        expect(bindingGets).toBe(1);
        expect(bindingsState.dirty).toBe(true);
        expect(
            within(destinations()).getByRole<HTMLInputElement>('checkbox', {
                name: 'QRZ.com for Main',
            }).checked
        ).toBe(true);
    });

    // U14 — THE TAB EXPLAINS BY LINK, NOT BY PARAGRAPH (station review
    // 2026-09-26, second pass): the one "How forwarding works" link is the ⓘ
    // beside the destinations heading — no line of its own above the sections —
    // and the station section says only what it is.
    it('U14: one manual link, beside the destinations heading; Station accounts explains nothing itself', async () => {
        await renderLoaded();
        const links = screen.getAllByRole('link', { name: 'How forwarding works' });
        expect(links).toHaveLength(1);
        expect(destinations().contains(links[0])).toBe(true);
        expect(document.body.textContent).not.toMatch(/Forwarding uploads each/);
        // Inbox 2026-09-26: not even one line — the ⓘ's manual section says it.
        expect(station().textContent).not.toMatch(/Shared by every archive/);
    });

    // U15 — STATION ACCOUNTS GETS ITS OWN ⓘ (operator ruling 2026-09-26): the
    // manual's Station accounts section, in a new tab like the sidebar's
    // Manual link, beside the heading and outside it so the section keeps its
    // name.
    it('U15: an ⓘ beside the Station accounts heading links its manual section', async () => {
        await renderLoaded();
        const link = within(station()).getByRole('link', { name: 'How station accounts work' });
        expect(link.getAttribute('href')).toBe('/manual/#station-accounts');
        expect(link.getAttribute('target')).toBe('_blank');
        expect(link.getAttribute('rel')).toBe('noopener');
        expect(link.getAttribute('title')).toBe('How station accounts work');
        expect(link.closest('h2')).toBeNull();
    });

    // U16 — THE SM CLOUD DESTINATION POINTS AT ITS STATION CARD (ruling
    // 2026-09-26): with a complete account there is no refusal to carry the
    // link, so a pointer line says where its service and token live and opens
    // that card.
    it('U16: the SM Cloud destination points at its service and token card', async () => {
        await renderLoaded();
        const pointer = within(destinations()).getByTestId('account-pointer');
        expect(flat(pointer)).toMatch(/under Station accounts/);
        await fireEvent.click(
            within(pointer).getByRole('button', { name: 'Show SM Cloud service and token' })
        );
        await vi.waitFor(() => expect(accountCard('SM Cloud service and token').open).toBe(true));
        expect(within(destinations()).getAllByTestId('account-pointer')).toHaveLength(1);
    });

    // U11 — A DESTINATION REFUSED FOR AN INCOMPLETE STATION ACCOUNT LINKS TO
    // THE CARD THAT FIXES IT: the link opens the card and brings it into view.
    // A destination without a station card (QRZ) gets no link, whatever its
    // reason says — there is nothing on this tab to open.
    it('U11: "Open its station account" opens and scrolls to that card', async () => {
        const scrolled: string[] = [];
        const had = Object.prototype.hasOwnProperty.call(HTMLElement.prototype, 'scrollIntoView');
        HTMLElement.prototype.scrollIntoView = function (this: HTMLElement) {
            scrolled.push(this.id);
        };
        try {
            const incomplete =
                'its station account is incomplete: a required station field is not set';
            const dests = DESTS.map((d) =>
                d.type === 'smcloud' || d.type === 'qrz'
                    ? { ...d, account: { configured: false }, reason: incomplete }
                    : d
            );
            await renderLoaded({ dests });
            expect(accountCard('SM Cloud service and token').open).toBe(false);

            const smNote = within(destinations())
                .getAllByTestId('destination-reason')
                .find((n) =>
                    flat(n.closest('details')?.querySelector('summary') ?? null).includes(
                        'SM Cloud'
                    )
                );
            await fireEvent.click(
                within(smNote!).getByRole('button', { name: 'Open its station account' })
            );
            await vi.waitFor(() =>
                expect(accountCard('SM Cloud service and token').open).toBe(true)
            );
            expect(scrolled).toEqual(['account-smcloud']);

            // Collapsed by the operator, the card opens again on the next click.
            await fireEvent.click(
                accountCard('SM Cloud service and token').querySelector('summary')!
            );
            expect(accountCard('SM Cloud service and token').open).toBe(false);
            await fireEvent.click(
                within(smNote!).getByRole('button', { name: 'Open its station account' })
            );
            expect(accountCard('SM Cloud service and token').open).toBe(true);
            expect(scrolled).toEqual(['account-smcloud', 'account-smcloud']);

            const qrzNote = within(destinations())
                .getAllByTestId('destination-reason')
                .find((n) =>
                    flat(n.closest('details')?.querySelector('summary') ?? null).includes('QRZ.com')
                );
            expect(qrzNote).toBeTruthy();
            expect(within(qrzNote!).queryByRole('button', { name: /station account/ })).toBeNull();
        } finally {
            if (!had) delete (HTMLElement.prototype as { scrollIntoView?: unknown }).scrollIntoView;
        }
    });

    it('U13: saving a station account refreshes its destination reason without losing drafts', async () => {
        let accountSaved = false;
        let bindingGets = 0;
        const incomplete = 'its station account is incomplete: a required station field is not set';
        const configBefore = {
            forwarders: CONFIG.forwarders.map((f) =>
                f.type === 'smcloud' ? { ...f, credentials_set: ['url'] } : f
            ),
        };
        const configAfter = {
            forwarders: CONFIG.forwarders.map((f) =>
                f.type === 'smcloud' ? { ...f, credentials_set: ['url', 'token'] } : f
            ),
        };
        const destinationsNow = (): Dest[] =>
            DESTS.map((d) =>
                d.type === 'smcloud'
                    ? {
                          ...d,
                          account: {
                              configured: accountSaved,
                              fields_set: accountSaved ? ['url', 'token'] : ['url'],
                          },
                          reason: accountSaved ? '' : incomplete,
                      }
                    : d
            );
        const response = (body: unknown, status = 200) =>
            new Response(JSON.stringify(body), {
                status,
                headers: { 'Content-Type': 'application/json' },
            });
        vi.stubGlobal(
            'fetch',
            vi.fn((url: string, init?: RequestInit) => {
                const method = init?.method ?? 'GET';
                if (url === '/v1/version')
                    return Promise.resolve(response({ instance: 'i', archive: { id: 'A1' } }));
                if (url === '/v1/forwarder-types') return Promise.resolve(response(TYPES));
                if (url === '/v1/qso-archives/A1/bindings') {
                    bindingGets++;
                    return Promise.resolve(response(bindingsView(destinationsNow())));
                }
                if (url === '/v1/config' && method === 'PUT') {
                    accountSaved = true;
                    return Promise.resolve(response(configAfter));
                }
                if (url === '/v1/config') return Promise.resolve(response(configBefore));
                return Promise.resolve(response({}, 404));
            })
        );
        render(ForwardingSection);
        await vi.waitFor(() => {
            expect(forwardingState.loaded).toBe(true);
            expect(bindingsState.loaded).toBe(true);
        });

        // An unrelated destination draft must survive the account refresh.
        await fireEvent.click(
            within(destinations()).getByRole('checkbox', { name: 'QRZ.com for Main' })
        );
        const smcloudSwitch = within(destinations()).getByRole<HTMLInputElement>('checkbox', {
            name: 'SM Cloud for Main',
        });
        expect(smcloudSwitch).toBeDisabled();

        const token = within(accountCard('SM Cloud service and token')).getByLabelText(
            'Bearer token'
        );
        await fireEvent.input(token, { target: { value: 'new-token' } });
        await fireEvent.click(within(station()).getByRole('button', { name: /^save$/i }));

        await vi.waitFor(() => expect(bindingGets).toBe(2));
        expect(smcloudSwitch).toBeEnabled();
        expect(within(destinations()).queryByTestId('destination-reason')).toBeNull();
        expect(bindingsState.dirty).toBe(true);
        expect(
            within(destinations()).getByRole<HTMLInputElement>('checkbox', {
                name: 'QRZ.com for Main',
            }).checked
        ).toBe(true);
    });
});
