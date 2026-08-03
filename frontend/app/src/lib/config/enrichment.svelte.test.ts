import { afterEach, describe, expect, it, vi } from 'vitest';
import { enrichmentState } from './enrichment.svelte';

/*
    ENRICHMENT SECTION — WHAT GOES ON THE WIRE.

    ACCEPTANCE CRITERIA (operator-checked 2026-08-03, before any mechanism):

        N2  Changing the QRZ username and saving without touching the password
            leaves the stored password intact.
        N3  A typed password replaces it. (Wire-only — password_set reads true
            either way, so nothing in the browser can tell those apart.)
        N4  Saving Enrichment does not disturb my station identity or the
            operational station block.
        N5  Saving does not delete a provider I cannot see.
        N8  Cancel restores exactly what is stored.
        J1  An explicit Remove deletes the stored QRZ password (password_clear).
        J2  A blank TTL means "use the default"; an explicit 0 means "never goes
            stale" — and the two must reach the daemon as different payloads.

    N5 IS THE ONE THAT BITES. `mergeLookup` (internal/api/handler_config.go)
    rebuilds the chain PURELY from the payload — there is no merge-by-name that
    keeps absent entries. So a section that renders QRZ and sends `chain: [qrz]`
    DELETES every other provider, silently, on the first save. The standalone
    config SPA gets this right and says so at the code site ("preserving
    daemon-defaulted URLs/timeouts and any other chain entries"); that care is
    the easiest thing in the whole port to drop, because nothing in a
    QRZ-only UI hints that the payload is a whole-list replace.

    The fixture therefore carries a THIRD provider (`hamqth`) that this build
    has no UI for at all. W5/W5b assert it survives a save with its URL and
    timeout intact — not merely that it is present, because a payload that
    re-sent it with empty url/timeout would pass a presence check and then fail
    the daemon's own validateLookupProvider the moment anyone enabled it.

    J2's two states are indistinguishable in the draft if TTLs are numbers —
    blank and 0 both become 0 — which is why the draft holds STRINGS. W7/W7b
    are the pair; either alone is satisfiable by always sending, or never
    sending, the field.
*/

afterEach(() => {
    vi.restoreAllMocks();
    enrichmentState.loaded = false;
    enrichmentState.loading = false;
    enrichmentState.saving = false;
    enrichmentState.error = '';
});

const CONFIG = {
    logging_station: { station_callsign: 'M0ABC' },
    station: { tx_power: '100' },
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
                password_set: true,
                timeout_sec: 10,
                view_url: 'https://www.qrz.com/db/',
            },
            // A provider this build renders no UI for. It must survive a save.
            {
                name: 'hamqth',
                enabled: false,
                url: 'https://www.hamqth.com/xml.php',
                username: 'someone',
                password_set: true,
                timeout_sec: 15,
            },
        ],
        country_ttl_days: 30,
        station_ttl_days: 7,
        refresh_max_in_flight: 4,
    },
};

function mockDaemon(putResponse: unknown = CONFIG, putStatus = 200) {
    const puts: Record<string, unknown>[] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            if (init?.method === 'PUT') {
                puts.push(
                    JSON.parse(typeof init.body === 'string' ? init.body : '{}') as Record<
                        string,
                        unknown
                    >
                );
                return Promise.resolve(
                    new Response(JSON.stringify(putResponse), {
                        status: putStatus,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            }
            return Promise.resolve(
                new Response(JSON.stringify(CONFIG), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
    return puts;
}

async function loadFresh(putResponse?: unknown, putStatus?: number) {
    const puts = mockDaemon(putResponse, putStatus);
    await enrichmentState.load();
    return puts;
}

type Provider = Record<string, unknown>;
function lookupOf(body: Record<string, unknown>): Record<string, unknown> {
    return body.lookup as Record<string, unknown>;
}
function chainOf(body: Record<string, unknown>): Provider[] {
    return lookupOf(body).chain as Provider[];
}
function providerNamed(body: Record<string, unknown>, name: string): Provider | undefined {
    return chainOf(body).find((p) => p.name === name);
}

describe('enrichmentState wire behaviour', () => {
    // W1 — the secret never arrives; only that one is stored.
    it('W1: loads QRZ password_set without any value', async () => {
        await loadFresh();
        expect(enrichmentState.draft.qrzPasswordSet).toBe(true);
        expect(enrichmentState.draft.qrzPassword).toBe('');
        expect(enrichmentState.draft.qrzUsername).toBe('M0ABC');
    });

    // W2 — N2. An untouched password box puts no `password` on the wire.
    it('W2: saving an unrelated edit sends no QRZ password field', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.qrzUsername = 'M0XYZ';
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.username).toBe('M0XYZ');
        expect('password' in qrz).toBe(false);
        expect('password_clear' in qrz).toBe(false);
    });

    // W3 — N3. Wire-only by necessity; see the header.
    it('W3: a typed QRZ password rides', async () => {
        const puts = await loadFresh();
        enrichmentState.setQrzPassword('typed-pw');
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.password).toBe('typed-pw');
        expect('password_clear' in qrz).toBe(false);
    });

    // W4 — J1. The remove signal reaches the daemon, and discards a half-typed
    // value so the two intents can never both be on the wire.
    it('W4: an explicit remove sends password_clear and no password', async () => {
        const puts = await loadFresh();
        enrichmentState.setQrzPassword('half-typed');
        enrichmentState.clearQrzPassword();
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.password_clear).toBe(true);
        expect('password' in qrz).toBe(false);
    });

    // W4b — and typing after a remove cancels the remove (last intent wins).
    it('W4b: typing after a remove cancels the remove', async () => {
        const puts = await loadFresh();
        enrichmentState.clearQrzPassword();
        enrichmentState.setQrzPassword('changed-mind');
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.password).toBe('changed-mind');
        expect('password_clear' in qrz).toBe(false);
    });

    // W5 — N5. THE LOAD-BEARING RULE. A provider with no UI in this build must
    // survive the save.
    it('W5: a provider this build cannot render is preserved, not deleted', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.qrzEnabled = false;
        await enrichmentState.save();

        const names = chainOf(puts[0]).map((p) => p.name);
        expect(names).toContain('hamqth');
    });

    // W5b — and preserved INTACT. Re-sending it with an empty url/timeout would
    // pass W5 and leave a provider the daemon rejects the moment it is enabled.
    it('W5b: the preserved provider keeps its url, username and timeout', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.qrzEnabled = false;
        await enrichmentState.save();

        const other = providerNamed(puts[0], 'hamqth')!;
        expect(other.url).toBe('https://www.hamqth.com/xml.php');
        expect(other.username).toBe('someone');
        expect(other.timeout_sec).toBe(15);
        // ...and no credential is invented for it.
        expect('password' in other).toBe(false);
        expect('password_clear' in other).toBe(false);
    });

    // W5c — the same care for QRZ's own daemon-defaulted fields. The section
    // renders no URL box, so an omitted url would be stored as "" and only
    // re-defaulted because QRZ happens to be a provider Normalize knows.
    it('W5c: QRZ keeps its daemon-defaulted url and view_url', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.qrzUsername = 'M0XYZ';
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.url).toBe('https://xmldata.qrz.com/xml/current/');
        expect(qrz.view_url).toBe('https://www.qrz.com/db/');
        expect(qrz.timeout_sec).toBe(10);
    });

    // W6 — N4. The PUT carries only `lookup`. The config SPA echoes
    // logging_station and station (config.svelte.ts:976), which would clobber a
    // concurrent identity or power change made between our GET and our PUT.
    it('W6: the PUT carries only lookup — no station blocks to clobber', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.hamnutEnabled = false;
        await enrichmentState.save();

        expect(Object.keys(puts[0])).toEqual(['lookup']);
    });

    // W7 — J2. A blank TTL is OMITTED, which is how the wire says "use the
    // default". Sending 0 instead would mean "never goes stale" — the opposite
    // of what a cleared box intends.
    it('W7: a blank TTL is omitted from the payload', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.countryTtlDays = '';
        await enrichmentState.save();

        const lookup = lookupOf(puts[0]);
        expect('country_ttl_days' in lookup).toBe(false);
        // The untouched one still rides, so "omitted" is a property of the blank
        // field and not of the whole block.
        expect(lookup.station_ttl_days).toBe(7);
    });

    // W7b — and an explicit 0 IS sent, because 0 is a real instruction.
    it('W7b: an explicit 0 TTL is sent as 0', async () => {
        const puts = await loadFresh();
        enrichmentState.draft.countryTtlDays = '0';
        await enrichmentState.save();

        expect(lookupOf(puts[0]).country_ttl_days).toBe(0);
    });

    // W8 — N8. Cancel drops both transient intents, not just the visible fields.
    it('W8: cancel discards a typed password and a pending remove', async () => {
        await loadFresh();
        enrichmentState.draft.qrzUsername = 'EDITED';
        enrichmentState.setQrzPassword('typed');
        enrichmentState.clearQrzPassword();
        enrichmentState.reset();

        expect(enrichmentState.draft.qrzUsername).toBe('M0ABC');
        expect(enrichmentState.draft.qrzPassword).toBe('');
        expect(enrichmentState.draft.qrzPasswordCleared).toBe(false);
        expect(enrichmentState.dirty).toBe(false);
    });

    // W9 — a pending remove alone is an unsaved change. Without this the
    // operator could press Remove, see nothing, and leave believing it was gone.
    it('W9: a pending remove alone marks the form dirty', async () => {
        await loadFresh();
        expect(enrichmentState.dirty).toBe(false);
        enrichmentState.clearQrzPassword();
        expect(enrichmentState.dirty).toBe(true);
    });

    // W10 — a rejected save keeps what the operator typed.
    it('W10: a rejected save preserves the operator’s entries', async () => {
        const puts = await loadFresh(
            { code: 'invalid_lookup', message: 'lookup.chain[0]: url is empty' },
            400
        );
        enrichmentState.draft.qrzUsername = 'M0XYZ';
        enrichmentState.setQrzPassword('kept-pw');
        await enrichmentState.save();

        expect(puts).toHaveLength(1);
        expect(enrichmentState.draft.qrzUsername).toBe('M0XYZ');
        expect(enrichmentState.draft.qrzPassword).toBe('kept-pw');
        expect(enrichmentState.dirty).toBe(true);
    });
});
