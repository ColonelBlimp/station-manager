import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import EnrichmentSection from './EnrichmentSection.svelte';
import { enrichmentState } from './enrichment.svelte';

/*
    ENRICHMENT SECTION — WHAT THE OPERATOR SEES.

    enrichment.svelte.test.ts pins the wire. These rules pin the half that is
    only observable in the browser:

        N1  I can tell "a QRZ password is stored" from "none is stored" without
            either showing me a value.
        J1  I can tell "this save will REMOVE the stored password" from "this
            save will KEEP it" — the box looks identical in both.
        J2  I can tell a TTL of 0 apart from a blank one, because they are
            OPPOSITE instructions: 0 means never re-fetch, blank means use the
            daemon's default.

    U4 IS THE ONE WITH NO EQUIVALENT IN THE EMAIL SECTION, and it is the one a
    reasonable person gets wrong. In Email a blank number and a 0 meant the same
    thing (use the default). Here they are opposites — an explicit 0 disables
    staleness entirely, so a cached QRZ record is never refreshed again. An
    operator who types 0 expecting "no caching" would get the exact reverse, and
    nothing in a bare numeric box would tell them. So the UI has to say which
    reading applies, and say it only when 0 is actually entered.
*/

function qrzDraft() {
    return enrichmentState.draft.providers.find((p) => p.name === 'qrzlookupservice')!;
}

afterEach(() => {
    vi.restoreAllMocks();
    enrichmentState.loaded = false;
    enrichmentState.loading = false;
    enrichmentState.saving = false;
    enrichmentState.error = '';
});

function mockConfig(passwordSet: boolean, countryTtl = 365) {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(
                    JSON.stringify({
                        lookup: {
                            hamnut: {
                                name: 'hamnutlookupservice',
                                enabled: true,
                                url: 'https://api.hamnut.com/v1/call-signs/prefixes',
                                timeout_sec: 10,
                                password_set: false,
                            },
                            chain: [
                                {
                                    name: 'qrzlookupservice',
                                    enabled: true,
                                    url: 'https://xmldata.qrz.com/xml/current/',
                                    username: 'M0ABC',
                                    password_set: passwordSet,
                                    timeout_sec: 10,
                                },
                                // A provider this build has no specific
                                // knowledge of. It must be VISIBLE, not merely
                                // preserved — see D4.
                                {
                                    name: 'hamqth',
                                    enabled: false,
                                    url: 'https://www.hamqth.com/xml.php',
                                    username: 'someone',
                                    password_set: true,
                                    timeout_sec: 15,
                                },
                            ],
                            country_ttl_days: countryTtl,
                            station_ttl_days: 90,
                            refresh_max_in_flight: 4,
                        },
                    }),
                    { status: 200, headers: { 'Content-Type': 'application/json' } }
                )
            )
        )
    );
}

async function renderLoaded(passwordSet: boolean, countryTtl?: number) {
    mockConfig(passwordSet, countryTtl);
    render(EnrichmentSection);
    await vi.waitFor(() => expect(enrichmentState.loaded).toBe(true));
}

/**
 * The disclosure for one source, by its summary text. Every query about a
 * provider's fields goes through this: with several sources on the page, an
 * unscoped getByPlaceholderText finds whichever card happens to come first,
 * which is how a rule about QRZ silently becomes a rule about something else.
 */
function card(container: HTMLElement, label: string): HTMLElement {
    const found = [...container.querySelectorAll('details')].find((d) =>
        (d.querySelector('summary')?.textContent ?? '').includes(label)
    );
    if (!found) throw new Error(`no disclosure for ${label}`);
    return found;
}

async function renderLoadedWithContainer(passwordSet: boolean, countryTtl?: number) {
    mockConfig(passwordSet, countryTtl);
    const rendered = render(EnrichmentSection);
    await vi.waitFor(() => expect(enrichmentState.loaded).toBe(true));
    return rendered;
}

/*
    EVERY SOURCE IS A DISCLOSURE (operator, 2026-08-03) — the Forwarding shape.

    Criterion:

        When I open Enrichment I see every lookup source the daemon has, one
        row each, and can tell which are on without opening any of them — and a
        source this build doesn't recognise is listed like the rest rather than
        hidden.

    D4 IS WHY THIS IS MORE THAN A RESTYLE. The wire rule W5 already made an
    unrecognised provider SURVIVE a save, but the flat layout rendered only QRZ
    and Hamnut, so such a provider was invisible: preserved, uneditable, and
    impossible to know about from the UI. Listing every source makes the thing
    W5 protects something the operator can actually see — and matches how
    Forwarding treats a destination whose type this build has no descriptor for.
*/
describe('EnrichmentSection disclosures', () => {
    // D1 — one row per source the daemon reports: hamnut + every chain entry.
    it('D1: renders one disclosure per source, including unrecognised ones', async () => {
        const { container } = await renderLoadedWithContainer(true);
        expect(container.querySelectorAll('details')).toHaveLength(3);
    });

    // D2 — the summary carries the on/off state, so a collapsed page still
    // answers "what is running" without a click.
    it('D2: each summary names the service and shows its state', async () => {
        const { container } = await renderLoadedWithContainer(true);
        const summaries = [...container.querySelectorAll('summary')].map(
            (s) => s.textContent ?? ''
        );

        expect(summaries.some((t) => /QRZ\.com/.test(t) && /enabled/i.test(t))).toBe(true);
        expect(summaries.some((t) => /Hamnut/.test(t) && /enabled/i.test(t))).toBe(true);
        expect(summaries.some((t) => /hamqth/.test(t) && /disabled/i.test(t))).toBe(true);
    });

    // D3 — an unrecognised source is LABELLED as such, so "no fields to edit"
    // reads as a known limitation rather than a broken row.
    it('D3: an unrecognised source says so', async () => {
        await renderLoaded(true);
        expect(screen.getByText(/not recognised by this build/i)).toBeTruthy();
    });

    // D4 — and it still carries its enable toggle: recognised or not, turning a
    // source off is the one action that must always work.
    it('D4: every source has an enable toggle, recognised or not', async () => {
        await renderLoaded(true);
        expect(screen.getAllByLabelText(/^enabled$/i)).toHaveLength(3);
    });
});

describe('EnrichmentSection', () => {
    // U1 — N1. The stored-or-not distinction, carried by the placeholder.
    it('U1: says a QRZ password is stored without showing one', async () => {
        const { container } = await renderLoadedWithContainer(true);
        const box = within(card(container, 'QRZ.com')).getByPlaceholderText(
            /set — leave blank to keep/i
        );
        expect((box as HTMLInputElement).value).toBe('');
    });

    it('U1b: shows no "set" hint when none is stored', async () => {
        const { container } = await renderLoadedWithContainer(false);
        expect(
            within(card(container, 'QRZ.com')).queryByPlaceholderText(/set — leave blank to keep/i)
        ).toBeNull();
    });

    // U2 — J1. Remove is offered only when there is something to remove; a
    // control that appears to work and does nothing teaches the operator that
    // the password WAS removed.
    it('U2: offers Remove only when a password is stored', async () => {
        const { container } = await renderLoadedWithContainer(false);
        expect(
            within(card(container, 'QRZ.com')).queryByRole('button', {
                name: /remove stored password/i,
            })
        ).toBeNull();
    });

    it('U2b: offers Remove when one is stored', async () => {
        const { container } = await renderLoadedWithContainer(true);
        expect(
            within(card(container, 'QRZ.com')).getByRole('button', {
                name: /remove stored password/i,
            })
        ).toBeTruthy();
    });

    // U3 — J1's discriminator. A pending removal must be visible and reversible.
    it('U3: shows a pending removal, and can undo it', async () => {
        const { container } = await renderLoadedWithContainer(true);
        const qrz = () => within(card(container, 'QRZ.com'));

        await fireEvent.click(qrz().getByRole('button', { name: /remove stored password/i }));
        expect(qrz().getByText(/will be removed when you save/i)).toBeTruthy();
        expect(qrzDraft().passwordCleared).toBe(true);

        await fireEvent.click(qrz().getByRole('button', { name: /keep stored password/i }));
        expect(qrzDraft().passwordCleared).toBe(false);
        expect(qrz().queryByText(/will be removed when you save/i)).toBeNull();

        // The OTHER credentialed source is untouched — a per-card control must
        // not be a page-wide one wearing a card's clothes.
        expect(
            within(card(container, 'hamqth')).getByRole('button', {
                name: /remove stored password/i,
            })
        ).toBeTruthy();
    });

    // U4 — J2. A TTL of 0 must announce what it means, because it is the
    // OPPOSITE of the blank in the same box.
    it('U4: a TTL of 0 says it means never re-fetch', async () => {
        await renderLoaded(true);
        const country = screen.getByDisplayValue('365');
        await fireEvent.input(country, { target: { value: '0' } });

        expect(screen.getByText(/never goes stale/i)).toBeTruthy();
    });

    // U4b — and the notice is absent for an ordinary value, so it reads as a
    // statement about THIS entry rather than permanent decoration.
    it('U4b: no never-stale notice for a normal TTL', async () => {
        await renderLoaded(true);
        expect(screen.queryByText(/never goes stale/i)).toBeNull();
    });

    // U4c — a blank box offers the default instead, so the two states are
    // legible as different. Without this, clearing the field looks like 0.
    it('U4c: a blank TTL shows the default as a placeholder, not a never-stale notice', async () => {
        await renderLoaded(true);
        const country = screen.getByDisplayValue('365');
        await fireEvent.input(country, { target: { value: '' } });

        expect(screen.queryByText(/never goes stale/i)).toBeNull();
        expect(screen.getByPlaceholderText('365')).toBeTruthy();
    });

    /*
        U6 — REMOVING A CREDENTIAL ALSO SWITCHES THE SOURCE OFF.

        Criterion: when I remove a source's stored password, that source is
        turned off in the same action and I can see it happen — I never save a
        combination the daemon will refuse.

        The daemon now REFUSES an enabled QRZ with no password (clean-room
        review 9732ab7914af: it used to accept it and then fail to start). So
        "remove but leave it enabled" is not a state the operator can reach any
        more, and offering it would guarantee a 400 on save. Turning the source
        off is what removing its credentials MEANS — the alternative readings
        (rotate the password) are served by typing a new one.

        U6b is the pair: Cancel must put BOTH back, or an abandoned removal
        leaves the source mysteriously switched off.
    */
    it('U6: removing a stored password also disables that source', async () => {
        const { container } = await renderLoadedWithContainer(true);
        expect(qrzDraft().enabled).toBe(true);

        await fireEvent.click(
            within(card(container, 'QRZ.com')).getByRole('button', {
                name: /remove stored password/i,
            })
        );

        expect(qrzDraft().passwordCleared).toBe(true);
        expect(qrzDraft().enabled).toBe(false);
        // ...and says so, rather than silently flipping a toggle the operator
        // did not touch.
        expect(
            within(card(container, 'QRZ.com')).getByText(/switched off|turned off/i)
        ).toBeTruthy();
    });

    it('U6b: cancelling restores both the password and the enabled state', async () => {
        const { container } = await renderLoadedWithContainer(true);
        await fireEvent.click(
            within(card(container, 'QRZ.com')).getByRole('button', {
                name: /remove stored password/i,
            })
        );
        enrichmentState.reset();

        expect(qrzDraft().passwordCleared).toBe(false);
        expect(qrzDraft().enabled).toBe(true);
    });

    // U5 — restart-only, so "saved" is not read as "in effect".
    it('U5: warns that changes apply at daemon restart', async () => {
        await renderLoaded(true);
        expect(screen.queryByText(/apply when the daemon restarts/i)).toBeNull();
        // Two providers each carry an "Enabled" toggle; [0] is QRZ's.
        await fireEvent.click(screen.getAllByLabelText(/enabled/i)[0]);
        expect(screen.getByText(/apply when the daemon restarts/i)).toBeTruthy();
    });
});
