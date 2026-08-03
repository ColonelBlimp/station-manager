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
                label: 'QRZ (club account)',
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

/** What GET /v1/lookup-types serves — the daemon's provider descriptors. The
 *  section reads display names and credential facts from here now, not from a
 *  map in the SPA (ADR 0062). `hamqth` deliberately has NO descriptor: that is
 *  the unrecognised-provider case. */
const TYPES = {
    types: [
        {
            name: 'hamnutlookupservice',
            display_name: 'Hamnut',
            help: 'Resolves DXCC / CQ / ITU zones from the callsign prefix.',
            kind: 'country',
            needs_credentials: false,
        },
        {
            name: 'qrzlookupservice',
            display_name: 'QRZ.com',
            help: 'Fills name, grid and address from QRZ.',
            kind: 'callsign',
            needs_credentials: true,
        },
    ],
};

function mockDaemon(putResponse: unknown = CONFIG, putStatus = 200) {
    const puts: Record<string, unknown>[] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
            if (u.includes('lookup-types')) {
                return Promise.resolve(
                    new Response(JSON.stringify(TYPES), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            }
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

const QRZ = 'qrzlookupservice';
const HAMNUT = 'hamnutlookupservice';

/** The live QRZ draft. Every source is a draft now, so edits go through these. */
function qrzDraft() {
    return enrichmentState.draft.providers.find((p) => p.name === QRZ)!;
}
function hamnutDraft() {
    return enrichmentState.draft.providers.find((p) => p.name === HAMNUT)!;
}

describe('enrichmentState wire behaviour', () => {
    // W1 — the secret never arrives; only that one is stored.
    it('W1: loads QRZ password_set without any value', async () => {
        await loadFresh();
        const qrz = qrzDraft();
        expect(qrz.passwordSet).toBe(true);
        expect(qrz.password).toBe('');
        expect(qrz.username).toBe('M0ABC');
    });

    // W2 — N2. An untouched password box puts no `password` on the wire.
    it('W2: saving an unrelated edit sends no QRZ password field', async () => {
        const puts = await loadFresh();
        qrzDraft().username = 'M0XYZ';
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.username).toBe('M0XYZ');
        expect('password' in qrz).toBe(false);
        expect('password_clear' in qrz).toBe(false);
    });

    // W3 — N3. Wire-only by necessity; see the header.
    it('W3: a typed QRZ password rides', async () => {
        const puts = await loadFresh();
        enrichmentState.setPassword(QRZ, 'typed-pw');
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.password).toBe('typed-pw');
        expect('password_clear' in qrz).toBe(false);
    });

    // W4 — J1. The remove signal reaches the daemon, and discards a half-typed
    // value so the two intents can never both be on the wire.
    it('W4: an explicit remove sends password_clear and no password', async () => {
        const puts = await loadFresh();
        enrichmentState.setPassword(QRZ, 'half-typed');
        enrichmentState.clearPassword(QRZ);
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.password_clear).toBe(true);
        expect('password' in qrz).toBe(false);
    });

    /*
        W4c — A PENDING REMOVAL FORCES enabled:false ON THE WIRE, WITHOUT
        DESTROYING THE OPERATOR'S SETTING.

        The daemon refuses an enabled credentialed provider with no password
        (it used to accept it and then fail to START — review 9732ab7914af), so
        the payload has to switch the source off. The first version did that by
        MUTATING draft.enabled, and neither way back restored it, so changing
        your mind saved QRZ disabled (review a6a3b1fcb40d).

        Deriving it instead means the draft keeps what the operator chose and
        every reversal is automatic. This rule pins BOTH halves — the wire says
        off, the draft still says on — because either alone is satisfiable by
        the version that was wrong.
    */
    it('W4c: a pending removal sends enabled:false but leaves the draft intact', async () => {
        const puts = await loadFresh();
        expect(qrzDraft().enabled).toBe(true);

        enrichmentState.clearPassword(QRZ);
        // Checked BEFORE the save: a successful save re-hydrates the draft from
        // the response, so asserting afterwards would pass whether or not
        // clearPassword had mutated it — the fixture would make both paths
        // agree, which is no rule at all.
        expect(qrzDraft().enabled).toBe(true);

        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.enabled).toBe(false);
        expect(qrz.password_clear).toBe(true);
    });

    // W4d — a source that needs no credentials is never force-disabled: hamnut
    // is anonymous, so a removal there says nothing about whether it can run.
    it('W4d: an anonymous source is not disabled by a removal', async () => {
        const puts = await loadFresh();
        enrichmentState.clearPassword(HAMNUT);
        await enrichmentState.save();

        expect((lookupOf(puts[0]).hamnut as Provider).enabled).toBe(true);
    });

    /*
        W11/W11b — THE OPERATOR'S LABEL IS DISPLAY-ONLY AND MUST NOT RIDE.

        `label` is config.json-only: mergeLookupProvider takes it from the
        STORED entry, never the payload, so sending it is at best a no-op. It
        must still be absent from the payload — a field that the daemon
        deliberately ignores is one refactor away from being honoured, and then
        this section would be able to rename a source it has no control for.
    */
    it('W11: the label is not sent on save', async () => {
        const puts = await loadFresh();
        qrzDraft().username = 'M0XYZ';
        await enrichmentState.save();

        expect('label' in providerNamed(puts[0], 'qrzlookupservice')!).toBe(false);
        expect('label' in (lookupOf(puts[0]).hamnut as Provider)).toBe(false);
    });

    // W11b — but it IS loaded, or there is nothing to display.
    it('W11b: the label is loaded from the daemon', async () => {
        await loadFresh();
        expect(qrzDraft().label).toBe('QRZ (club account)');
    });

    // W4b — and typing after a remove cancels the remove (last intent wins).
    it('W4b: typing after a remove cancels the remove', async () => {
        const puts = await loadFresh();
        enrichmentState.clearPassword(QRZ);
        enrichmentState.setPassword(QRZ, 'changed-mind');
        await enrichmentState.save();

        const qrz = providerNamed(puts[0], 'qrzlookupservice')!;
        expect(qrz.password).toBe('changed-mind');
        expect('password_clear' in qrz).toBe(false);
    });

    // W5 — N5. THE LOAD-BEARING RULE. A provider with no UI in this build must
    // survive the save.
    it('W5: a provider this build cannot render is preserved, not deleted', async () => {
        const puts = await loadFresh();
        qrzDraft().enabled = false;
        await enrichmentState.save();

        const names = chainOf(puts[0]).map((p) => p.name);
        expect(names).toContain('hamqth');
    });

    // W5b — and preserved INTACT. Re-sending it with an empty url/timeout would
    // pass W5 and leave a provider the daemon rejects the moment it is enabled.
    it('W5b: the preserved provider keeps its url, username and timeout', async () => {
        const puts = await loadFresh();
        qrzDraft().enabled = false;
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
        qrzDraft().username = 'M0XYZ';
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
        hamnutDraft().enabled = false;
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
        qrzDraft().username = 'EDITED';
        enrichmentState.setPassword(QRZ, 'typed');
        enrichmentState.clearPassword(QRZ);
        enrichmentState.reset();

        expect(qrzDraft().username).toBe('M0ABC');
        expect(qrzDraft().password).toBe('');
        expect(qrzDraft().passwordCleared).toBe(false);
        expect(enrichmentState.dirty).toBe(false);
    });

    // W9 — a pending remove alone is an unsaved change. Without this the
    // operator could press Remove, see nothing, and leave believing it was gone.
    it('W9: a pending remove alone marks the form dirty', async () => {
        await loadFresh();
        expect(enrichmentState.dirty).toBe(false);
        enrichmentState.clearPassword(QRZ);
        expect(enrichmentState.dirty).toBe(true);
    });

    // W10 — a rejected save keeps what the operator typed.
    it('W10: a rejected save preserves the operator’s entries', async () => {
        const puts = await loadFresh(
            { code: 'invalid_lookup', message: 'lookup.chain[0]: url is empty' },
            400
        );
        qrzDraft().username = 'M0XYZ';
        enrichmentState.setPassword(QRZ, 'kept-pw');
        await enrichmentState.save();

        expect(puts).toHaveLength(1);
        expect(qrzDraft().username).toBe('M0XYZ');
        expect(qrzDraft().password).toBe('kept-pw');
        expect(enrichmentState.dirty).toBe(true);
    });
});
