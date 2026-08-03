import { afterEach, describe, expect, it, vi } from 'vitest';
import { forwardingState } from './forwarding.svelte';

/*
    FORWARDING SECTION — CREDENTIAL SAFETY.

    ACCEPTANCE CRITERION (operator-checked 2026-08-02, before any mechanism):

        When I save a forwarder's credentials, the daemon stores them and the
        browser never receives them back — and I can tell "saved, value kept"
        apart from "saved, value replaced" and from "never set". For a field the
        daemon marks clearable, I can also reset it to its default and see which
        default I got.

    WHY THE THIRD CLAUSE IS THE WHOLE FEATURE. GET /v1/config echoes no
    credential value ever, only `credentials_set`. So a blank box means "not
    retyped" — and the standalone config SPA makes that safe by STRIPPING empties
    before the PUT (config.svelte.ts:892). That is also exactly why it can never
    express a reset: clearing requires SENDING "", which is the thing it strips.
    Hence "the config SPA cannot express a Clearable reset".

    WHAT CLEARABLE IS NOT. It is not "delete this credential". Only two fields in
    the whole system are clearable — smcloud's `logbook` (defaults to "main") and
    the dev-only stub's `mode` — and both are fields whose CONSTRUCTOR supplies a
    default. No password is clearable anywhere and none may become one: emptying
    a required credential is not a reset, it is a forwarder whose New() rejects
    at startup, which aborts spawnForwarderWorkers and takes the daemon down
    after the PUT already returned 200 (handler_config.go:1269-1275). F5 pins
    that the UI cannot offer it.

    FIXTURE SHAPES DELIBERATELY AVOIDED:
      - F2 and F4 assert the SAME key in the SAME fixture, once untouched and
        once reset. Asserting only that a reset sends "" would pass against the
        naive payload that sends "" for every untouched box too — the exact bug
        this feature exists to prevent.
      - F3 uses a value distinguishable from both "" and the stored state, so
        "replaced" cannot be confused with either.
      - F6 asserts the unknown-type entry SURVIVES the save. Asserting only that
        its credentials are uneditable would pass against a save that dropped it.
*/

afterEach(() => vi.restoreAllMocks());

const TYPES = {
    types: [
        {
            type: 'qrz',
            display_name: 'QRZ.com',
            supported_actions: ['insert', 'update'],
            credential_fields: [
                { key: 'api_key', label: 'API key', kind: 'password' },
                { key: 'username', label: 'Username', kind: 'text' },
            ],
        },
        {
            type: 'smcloud',
            display_name: 'SM Cloud',
            supported_actions: ['insert'],
            credential_fields: [
                { key: 'url', label: 'URL', kind: 'text' },
                // The one real clearable field: New() defaults it to "main".
                { key: 'logbook', label: 'Cloud logbook', kind: 'text', clearable: true },
            ],
        },
    ],
};

const CONFIG = {
    forwarders: [
        { name: 'qrz', type: 'qrz', enabled: true, credentials_set: ['api_key', 'username'] },
        { name: 'smcloud', type: 'smcloud', enabled: true, credentials_set: ['url', 'logbook'] },
        // A type this build does not know about (e.g. a forwarder from a newer
        // daemon). It has no descriptor, so its credentials are uneditable.
        { name: 'mystery', type: 'mystery', enabled: false, credentials_set: ['token'] },
    ],
};

/** Routes /v1/config and /v1/forwarder-types, and records PUT bodies. */
function mockDaemon() {
    const puts: unknown[] = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
            if (init?.method === 'PUT') {
                puts.push(JSON.parse(typeof init.body === 'string' ? init.body : ''));
                return Promise.resolve(
                    new Response(JSON.stringify(CONFIG), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            }
            const body = u.includes('forwarder-types') ? TYPES : CONFIG;
            return Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
    return puts;
}

function creds(payload: unknown, name: string): Record<string, string> | undefined {
    const list = (
        payload as { forwarders: { name: string; credentials?: Record<string, string> }[] }
    ).forwarders;
    return list.find((f) => f.name === name)?.credentials;
}

async function loadFresh() {
    const puts = mockDaemon();
    await forwardingState.load();
    return puts;
}

describe('forwardingState credential safety', () => {
    // F1 — NO CREDENTIAL VALUE EVER ARRIVES. The daemon sends only which keys
    // are set; the form must start with empty inputs, never a pre-filled secret.
    it('F1: loads which keys are set, never their values', async () => {
        await loadFresh();
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz');
        expect(qrz?.credentialsSet).toEqual(['api_key', 'username']);
        expect(qrz?.credentials).toEqual({});
    });

    // F2 — AN UNTOUCHED CREDENTIAL IS NOT SENT. If a blank box rode along as "",
    // every save would reset every clearable field the operator never looked at.
    it('F2: saving without retyping sends no credential for the untouched key', async () => {
        const puts = await loadFresh();
        const smcloud = forwardingState.drafts.find((d) => d.name === 'smcloud')!;
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        // Model what the component actually does: a `bind:value` on every
        // rendered input writes '' into the map for keys the operator never
        // focused. Asserting against an untouched EMPTY map instead would pass
        // whether or not blanks are stripped — the rule could then only fail if
        // two things regressed at once, which is no rule at all.
        smcloud.credentials.logbook = '';
        smcloud.credentials.url = '';
        qrz.credentials.api_key = '';
        smcloud.enabled = false; // make it dirty WITHOUT typing a credential
        await forwardingState.save();

        expect(puts).toHaveLength(1);
        expect(creds(puts[0], 'smcloud')?.logbook).toBeUndefined();
        expect(creds(puts[0], 'qrz')?.api_key).toBeUndefined();
    });

    // F3 — A TYPED VALUE IS SENT. The other half of F2: "kept" and "replaced"
    // must be different wire outcomes, not different intentions.
    it('F3: a retyped credential is sent', async () => {
        const puts = await loadFresh();
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        qrz.credentials.api_key = 'NEWKEY123';
        await forwardingState.save();

        expect(creds(puts[0], 'qrz')?.api_key).toBe('NEWKEY123');
    });

    // F4 — AN EXPLICIT RESET IS SENT AS "", AND IS DISTINGUISHABLE FROM F2.
    // Same field, same fixture, opposite outcome — that contrast IS the
    // criterion's third clause, and it is what the config SPA cannot express.
    it('F4: clearing a clearable field sends an empty value', async () => {
        const puts = await loadFresh();
        forwardingState.clear('smcloud', 'logbook');
        await forwardingState.save();

        expect(creds(puts[0], 'smcloud')).toHaveProperty('logbook');
        expect(creds(puts[0], 'smcloud')?.logbook).toBe('');
    });

    // F5 — A NON-CLEARABLE FIELD CANNOT BE RESET. Emptying a required credential
    // is a daemon that will not restart, so the capability must not exist at all
    // — not merely be discouraged in the UI.
    it('F5: clearable() is false for password and required fields', async () => {
        await loadFresh();
        expect(forwardingState.clearable('smcloud', 'logbook')).toBe(true);
        expect(forwardingState.clearable('qrz', 'api_key')).toBe(false);
        expect(forwardingState.clearable('smcloud', 'url')).toBe(false);
    });

    // F5b — AND A RESET REQUEST ON ONE IS REFUSED, not silently honoured. The
    // state module is the last line: a component bug must not be able to empty
    // a required credential.
    it('F5b: clearing a non-clearable field is refused', async () => {
        const puts = await loadFresh();
        forwardingState.clear('qrz', 'api_key');
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        qrz.enabled = false; // ensure the save proceeds regardless
        await forwardingState.save();

        expect(creds(puts[0], 'qrz')?.api_key).toBeUndefined();
    });

    // F8 — TYPING THEN ERASING IS NOT A RESET. Adding an explicit reset creates
    // a third state the form did not have before: a box that is blank because
    // the operator emptied it, versus blank because they never touched it,
    // versus reset on purpose. Only the last may send "". Without this, backing
    // out of a half-typed edit silently resets a clearable field to its default.
    it('F8: emptying a box by hand does not clear the field', async () => {
        const puts = await loadFresh();
        const smcloud = forwardingState.drafts.find((d) => d.name === 'smcloud')!;
        smcloud.credentials.logbook = 'contest';
        smcloud.credentials.logbook = ''; // thought better of it
        smcloud.enabled = false; // keep the save dirty
        await forwardingState.save();

        expect(creds(puts[0], 'smcloud')?.logbook).toBeUndefined();
    });

    // F8b — AND AN EXPLICIT RESET STILL WINS AFTERWARDS, so the guard above
    // cannot be implemented by simply never sending blanks.
    it('F8b: reset after an abandoned edit still clears', async () => {
        const puts = await loadFresh();
        const smcloud = forwardingState.drafts.find((d) => d.name === 'smcloud')!;
        smcloud.credentials.logbook = 'contest';
        smcloud.credentials.logbook = '';
        forwardingState.clear('smcloud', 'logbook');
        await forwardingState.save();

        expect(creds(puts[0], 'smcloud')?.logbook).toBe('');
    });

    // F5c — THE REFUSAL IS RECORDED AT THE STATE LEVEL, NOT ONLY AT THE WIRE.
    // F5b and this rule guard two DIFFERENT mechanisms that happen to agree:
    // clear() declines to record an impossible intent, buildPayload declines to
    // send one. Removing either alone left the whole suite green, which means a
    // single rule was testing neither. One rule each.
    it('F5c: clear() does not record a reset for a non-clearable field', async () => {
        await loadFresh();
        forwardingState.clear('qrz', 'api_key');
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        expect(qrz.cleared).not.toContain('api_key');
    });

    // F9 — EDITS ARE ATTRIBUTABLE TO A DESTINATION. Collapsing each destination
    // into a disclosure creates a state that did not exist while everything was
    // on screen: an edit the operator cannot see. The footer says "Unsaved
    // changes" but not WHERE, so a collapsed card must be able to say for
    // itself. Each of the three edit kinds counts.
    it('F9: hasEdits reports per-destination, for every kind of edit', async () => {
        await loadFresh();
        expect(forwardingState.hasEdits('qrz')).toBe(false);
        expect(forwardingState.hasEdits('smcloud')).toBe(false);

        // 1. a toggled enable
        forwardingState.drafts.find((d) => d.name === 'qrz')!.enabled = false;
        expect(forwardingState.hasEdits('qrz')).toBe(true);
        expect(forwardingState.hasEdits('smcloud')).toBe(false); // not its neighbour's

        // 2. a typed credential
        forwardingState.drafts.find((d) => d.name === 'smcloud')!.credentials.url = 'https://x';
        expect(forwardingState.hasEdits('smcloud')).toBe(true);

        // 3. a pending reset, on a destination with nothing else changed
        forwardingState.reset();
        expect(forwardingState.hasEdits('smcloud')).toBe(false);
        forwardingState.clear('smcloud', 'logbook');
        expect(forwardingState.hasEdits('smcloud')).toBe(true);
    });

    // F9b — AND A BLANK BOX IS NOT AN EDIT. The component's bind:value writes ''
    // for every rendered input, so counting "a key exists in the map" would mark
    // every card edited the moment it is expanded — which would make the marker
    // mean "you opened this", not "you changed this".
    it('F9b: a blank credential box is not an edit', async () => {
        await loadFresh();
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        qrz.credentials.api_key = '';
        qrz.credentials.username = '';
        expect(forwardingState.hasEdits('qrz')).toBe(false);
    });

    // F6 — AN UNKNOWN TYPE SURVIVES THE SAVE. The forwarders block is replaced
    // WHOLE, so a destination dropped from the payload is a destination removed
    // from config until the daemon re-seeds it at restart.
    it('F6: a forwarder whose type this build lacks still round-trips', async () => {
        const puts = await loadFresh();
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        qrz.enabled = false;
        await forwardingState.save();

        const names = (puts[0] as { forwarders: { name: string }[] }).forwarders.map((f) => f.name);
        expect(names).toContain('mystery');
        expect(forwardingState.typeFor('mystery')).toBeUndefined();
    });

    // F7 — THE SAVE TOUCHES ONLY `forwarders`. Echoing logging_station/station
    // back (as the config SPA does) would clobber a concurrent edit made in
    // another tab between our GET and our PUT.
    it('F7: the PUT body carries forwarders and nothing else', async () => {
        const puts = await loadFresh();
        const qrz = forwardingState.drafts.find((d) => d.name === 'qrz')!;
        qrz.enabled = false;
        await forwardingState.save();

        expect(Object.keys(puts[0] as object)).toEqual(['forwarders']);
    });

    // F8 — A FAILED RELOAD MARKS THE SECTION UNLOADED, not just errored.
    // Settings is mounted behind a router branch (App.svelte:100), so leaving
    // unmounts it while this module — a singleton — survives; returning
    // re-fires onMount → load(). Leaving `loaded` true there renders the
    // previous session's destinations as though current, and F6 above is
    // exactly why that is dangerous: the whole list rides every save, so a
    // stale one rewrites every destination at its stale enabled-state. Full
    // reasoning in email.svelte.test.ts (clean-room review dcb0316e69b9).
    it('F8: a failed reload after a successful one clears loaded', async () => {
        await loadFresh();
        expect(forwardingState.loaded).toBe(true);

        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('nope', { status: 503 })))
        );
        await forwardingState.load();

        expect(forwardingState.loaded).toBe(false);
        expect(forwardingState.error).not.toBe('');
    });

    // F8b — invalidation happens before the two reload requests settle, so a
    // slow daemon cannot leave the retained whole-list draft writable
    // (clean-room review 2c64c7aa P1).
    it('F8b: a pending reload immediately marks the section unloaded', async () => {
        await loadFresh();

        let releaseConfig!: (response: Response) => void;
        let releaseTypes!: (response: Response) => void;
        vi.stubGlobal(
            'fetch',
            vi.fn((url: RequestInfo | URL) => {
                const u = url instanceof URL ? url.href : typeof url === 'string' ? url : url.url;
                return new Promise<Response>((resolve) => {
                    if (u.includes('forwarder-types')) releaseTypes = resolve;
                    else releaseConfig = resolve;
                });
            })
        );
        const reload = forwardingState.load();

        expect(forwardingState.loading).toBe(true);
        expect(forwardingState.loaded).toBe(false);

        releaseConfig(
            new Response(JSON.stringify(CONFIG), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
        releaseTypes(
            new Response(JSON.stringify(TYPES), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
        await reload;
        expect(forwardingState.loaded).toBe(true);
    });

    // F8c — the state layer also refuses an unloaded whole-list write, even if
    // a future component regression exposes its Save control.
    it('F8c: save is refused while the section is not loaded', async () => {
        const puts = await loadFresh();
        forwardingState.drafts[0].enabled = !forwardingState.drafts[0].enabled;
        forwardingState.loaded = false;

        await forwardingState.save();

        expect(puts).toHaveLength(0);
    });
});
