import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
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

describe('EnrichmentSection', () => {
    // U1 — N1. The stored-or-not distinction, carried by the placeholder.
    it('U1: says a QRZ password is stored without showing one', async () => {
        await renderLoaded(true);
        const box = screen.getByPlaceholderText(/set — leave blank to keep/i);
        expect((box as HTMLInputElement).value).toBe('');
    });

    it('U1b: shows no "set" hint when none is stored', async () => {
        await renderLoaded(false);
        expect(screen.queryByPlaceholderText(/set — leave blank to keep/i)).toBeNull();
    });

    // U2 — J1. Remove is offered only when there is something to remove; a
    // control that appears to work and does nothing teaches the operator that
    // the password WAS removed.
    it('U2: offers Remove only when a password is stored', async () => {
        await renderLoaded(false);
        expect(screen.queryByRole('button', { name: /remove stored password/i })).toBeNull();
    });

    it('U2b: offers Remove when one is stored', async () => {
        await renderLoaded(true);
        expect(screen.getByRole('button', { name: /remove stored password/i })).toBeTruthy();
    });

    // U3 — J1's discriminator. A pending removal must be visible and reversible.
    it('U3: shows a pending removal, and can undo it', async () => {
        await renderLoaded(true);
        await fireEvent.click(screen.getByRole('button', { name: /remove stored password/i }));

        expect(screen.getByText(/will be removed when you save/i)).toBeTruthy();
        expect(enrichmentState.draft.qrzPasswordCleared).toBe(true);

        await fireEvent.click(screen.getByRole('button', { name: /keep stored password/i }));
        expect(enrichmentState.draft.qrzPasswordCleared).toBe(false);
        expect(screen.queryByText(/will be removed when you save/i)).toBeNull();
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

    // U5 — restart-only, so "saved" is not read as "in effect".
    it('U5: warns that changes apply at daemon restart', async () => {
        await renderLoaded(true);
        expect(screen.queryByText(/apply when the daemon restarts/i)).toBeNull();
        // Two providers each carry an "Enabled" toggle; [0] is QRZ's.
        await fireEvent.click(screen.getAllByLabelText(/enabled/i)[0]);
        expect(screen.getByText(/apply when the daemon restarts/i)).toBeTruthy();
    });
});
