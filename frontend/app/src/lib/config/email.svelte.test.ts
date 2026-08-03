import { afterEach, describe, expect, it, vi } from 'vitest';
import { emailState } from './email.svelte';

/*
    EMAIL SECTION — WHAT GOES ON THE WIRE.

    ACCEPTANCE CRITERIA (operator-checked 2026-08-03, before any mechanism):

        E2  When I change the host and save without touching the password, the
            stored password survives — and I can tell that apart from the save
            having wiped it.
        E3  When I type a new password and save, that value is what gets stored.
        E4  When I save Email while another client has just changed my transmit
            power or station identity, that change survives.
        E8  When I ask to remove the stored password and save, the daemon's copy
            is gone — and I can tell that apart from simply leaving the box
            blank, which keeps it.
        Q2  When I clear the port or timeout and save, it resolves to 587 / 30
            and what I see afterwards is what was stored.

    E3 IS NOT OPERATOR-OBSERVABLE AND THIS FILE IS WHERE IT LIVES. `password_set`
    reads true whether the daemon kept the old secret or took the new one, so
    nothing in the browser can tell those apart; the only human-level proof is a
    successful send. W3 therefore asserts the wire — and says so, rather than
    dressing a wire assertion up as a UI rule.

    THE THREE BLANK STATES, which are the whole point (same shape as forwarding,
    different mechanism — see W2/W3/W4):

        never touched   → no `password` key    → daemon keeps the stored secret
        typed           → `password: "…"`      → daemon replaces it
        explicitly reset→ `password_clear:true`→ daemon removes it

    Blank does NOT mean reset here, deliberately, and that is the operator's
    ruling: it is what an operator editing the host sends every single time. So
    removal got its own signal instead of overloading the empty string the way
    forwarder Clearable fields do.

    FIXTURE SHAPES DELIBERATELY CHOSEN:
      - W2 and W4 drive the SAME field through the same save path, once untouched
        and once reset. Asserting only that a reset sends the flag would pass
        against a payload that also sent it for an untouched box — the exact bug
        the feature exists to prevent.
      - W3 uses a value that is neither '' nor the stored state, so "replaced"
        cannot be confused with "kept" or "cleared".
      - W4b half-types a password BEFORE pressing remove. Without it, "the flag
        is sent" is satisfiable by an implementation that also sends the stale
        typed value, leaving the daemon to arbitrate an intent we should never
        have put on the wire.
      - W5 asserts on the KEYS of the PUT body, not on the smtp block. Asserting
        the smtp block is correct would stay green while the body also carried a
        stale logging_station — which is precisely the config SPA's bug.
      - W6 uses a stored port of 2525, not 587: with the default in the fixture,
        "sends the resolved default" and "echoes what it loaded" agree, and the
        rule proves nothing.
*/

afterEach(() => {
    vi.restoreAllMocks();
    emailState.loaded = false;
    emailState.loading = false;
    emailState.saving = false;
    emailState.error = '';
});

/** The daemon's masked view: a password IS stored, on a non-default port. */
const CONFIG = {
    logging_station: { station_callsign: 'M0ABC', my_gridsquare: 'IO91' },
    station: { tx_power: '100' },
    smtp: {
        enabled: true,
        host: 'smtp.example.org',
        port: 2525,
        username: 'tx@example.org',
        from: 'tx@example.org',
        default_recipient: 'qsl@example.org',
        starttls: true,
        timeout_sec: 15,
        password_set: true,
    },
};

/** Routes /v1/config and records PUT bodies. */
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
    await emailState.load();
    return puts;
}

function smtpOf(body: Record<string, unknown>): Record<string, unknown> {
    return body.smtp as Record<string, unknown>;
}

describe('emailState wire behaviour', () => {
    // W1 — THE SECRET NEVER ARRIVES. The daemon reports only that one is stored.
    it('W1: loads password_set without any value, and starts with an empty box', async () => {
        await loadFresh();
        expect(emailState.draft.passwordSet).toBe(true);
        expect(emailState.draft.password).toBe('');
    });

    // W2 — E2. An untouched password box must not put `password` on the wire at
    // all: a "" would be indistinguishable from a deliberate value to any future
    // reader of the payload, and invites exactly the overload we rejected.
    it('W2: saving an unrelated edit sends no password field', async () => {
        const puts = await loadFresh();
        emailState.draft.host = 'new.example.org';
        await emailState.save();

        expect(puts).toHaveLength(1);
        const smtp = smtpOf(puts[0]);
        expect(smtp.host).toBe('new.example.org');
        expect('password' in smtp).toBe(false);
        expect('password_clear' in smtp).toBe(false);
    });

    // W3 — E3. WIRE-ONLY by necessity; see the header.
    it('W3: a typed password rides, and nothing asks for a clear', async () => {
        const puts = await loadFresh();
        emailState.draft.password = 'typed-pw';
        await emailState.save();

        const smtp = smtpOf(puts[0]);
        expect(smtp.password).toBe('typed-pw');
        expect('password_clear' in smtp).toBe(false);
    });

    // W4 — E8. The distinct signal reaches the daemon.
    it('W4: an explicit remove sends password_clear and no password', async () => {
        const puts = await loadFresh();
        emailState.clearPassword();
        await emailState.save();

        const smtp = smtpOf(puts[0]);
        expect(smtp.password_clear).toBe(true);
        expect('password' in smtp).toBe(false);
    });

    // W4b — a half-typed value is DISCARDED by the remove, so the two intents
    // can never both be on the wire. The daemon has its own rule for that pair
    // (clear wins), but it exists for foreign clients — ours must not need it.
    it('W4b: removing discards a half-typed password rather than sending both', async () => {
        const puts = await loadFresh();
        emailState.draft.password = 'half-typed';
        emailState.clearPassword();
        await emailState.save();

        const smtp = smtpOf(puts[0]);
        expect(smtp.password_clear).toBe(true);
        expect('password' in smtp).toBe(false);
    });

    // W4c — and the reverse: retyping after a remove is a replace, not a remove.
    // Without this, "last intent wins" is only half-proved.
    it('W4c: typing after a remove cancels the remove', async () => {
        const puts = await loadFresh();
        emailState.clearPassword();
        emailState.setPassword('changed-mind');
        await emailState.save();

        const smtp = smtpOf(puts[0]);
        expect(smtp.password).toBe('changed-mind');
        expect('password_clear' in smtp).toBe(false);
    });

    // W5 — E4. THE CLOBBER RULE. The config SPA's saveEmail echoes
    // logging_station and station (config.svelte.ts:1035); carrying that across
    // would let an Email save revert a concurrent identity or power change made
    // between our GET and our PUT. Assert the body's KEYS.
    it('W5: the PUT carries only smtp — no station blocks to clobber', async () => {
        const puts = await loadFresh();
        emailState.draft.host = 'new.example.org';
        await emailState.save();

        expect(Object.keys(puts[0])).toEqual(['smtp']);
    });

    // W6 — Q2. A cleared number sends 0, which the daemon resolves to its
    // default; the form then shows what was actually stored. The stored port
    // here is 2525, so "sent the default" and "echoed the load" differ.
    it('W6: a blank port sends 0 and the form adopts the daemon resolved value', async () => {
        const resolved = { ...CONFIG, smtp: { ...CONFIG.smtp, port: 587, timeout_sec: 30 } };
        const puts = await loadFresh(resolved);
        emailState.draft.port = '';
        emailState.draft.timeoutSec = '';
        await emailState.save();

        const smtp = smtpOf(puts[0]);
        expect(smtp.port).toBe(0);
        expect(smtp.timeout_sec).toBe(0);
        // The daemon's answer, not the blank we sent.
        expect(emailState.draft.port).toBe('587');
        expect(emailState.draft.timeoutSec).toBe('30');
    });

    // W7 — a pending remove is by itself an unsaved change. Without this, an
    // operator could press Remove, see no "unsaved changes", and navigate away
    // believing the password was gone.
    it('W7: a pending remove alone marks the form dirty', async () => {
        await loadFresh();
        expect(emailState.dirty).toBe(false);
        emailState.clearPassword();
        expect(emailState.dirty).toBe(true);
    });

    // W7b — and so is a typed password on its own.
    it('W7b: a typed password alone marks the form dirty', async () => {
        await loadFresh();
        emailState.setPassword('x');
        expect(emailState.dirty).toBe(true);
    });

    // W8 — E7. Cancel drops BOTH transient intents, not just the visible fields.
    // A reset that restored the text boxes while leaving passwordCleared set
    // would arm a deletion the operator believes they cancelled.
    it('W8: cancel discards a typed password and a pending remove', async () => {
        await loadFresh();
        emailState.draft.host = 'edited.example.org';
        emailState.setPassword('typed');
        emailState.clearPassword();
        emailState.reset();

        expect(emailState.draft.host).toBe('smtp.example.org');
        expect(emailState.draft.password).toBe('');
        expect(emailState.draft.passwordCleared).toBe(false);
        expect(emailState.dirty).toBe(false);
    });

    /*
        A FAILED RELOAD MUST NOT LEAVE STALE SETTINGS LOOKING CURRENT.
        (clean-room review dcb0316e69b9, P1 — verified reachable before fixing.)

        Criterion:

            When I come back to Settings and the daemon can't be reached, I see
            that the refresh failed and can retry — and I can tell that apart
            from looking at settings that are actually current. I cannot save a
            stale copy over the daemon's state.

        REACHABILITY, checked rather than assumed: App.svelte:100 renders
        <Settings /> behind {#if router.view === 'config'}, so leaving the view
        UNMOUNTS it while these state modules — module-level singletons —
        survive. Returning re-fires onMount → load(). If that request fails,
        `loaded` was left true, the component's error branch
        ({#if !loaded && error}) never fired, and the form rendered the previous
        session's values with nothing to say so.

        WHY IT IS A P1 AND NOT COSMETIC: the PUT replaces the smtp block WHOLE.
        Editing one field on a stale form therefore writes every OTHER field
        back at its stale value — silently reverting anything changed in the
        meantime, including a password removal that had already been applied.

        R2 IS THE LAST LINE AND IS NOT MERELY REDUNDANT. It also closes a
        window R1 cannot: between mount and onMount's load, the state is
        loaded=false / loading=false / error='', which falls through the
        component's {:else} into the FORM — bound to the blank placeholder
        block. Without R2 a save in that window would write blanks.
    */

    // R1 — a failed reload after a successful one marks the section unloaded,
    // so the component shows its error card instead of stale values. The first
    // load must SUCCEED here: asserting against a first-load failure would pass
    // trivially, since `loaded` was never true to begin with.
    it('R1: a failed reload clears loaded, so the failure is visible', async () => {
        await loadFresh();
        expect(emailState.loaded).toBe(true);
        expect(emailState.draft.host).toBe('smtp.example.org');

        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('nope', { status: 503 })))
        );
        await emailState.load();

        expect(emailState.loaded).toBe(false);
        expect(emailState.error).not.toBe('');
    });

    // R2 — and the state module refuses the write outright, so no component bug
    // can put a stale or blank whole-block PUT on the wire.
    it('R2: save is refused while the section is not loaded', async () => {
        const puts = await loadFresh();
        emailState.draft.host = 'stale.example.org';
        emailState.loaded = false;

        await emailState.save();

        expect(puts).toHaveLength(0);
    });

    // W9 — E5. A refused save keeps what the operator typed, so they can fix the
    // one bad field instead of re-entering the form. Asserting only that an
    // error is reported would pass against a save that reset the draft.
    it('W9: a rejected save preserves the operator’s entries', async () => {
        const puts = await loadFresh(
            { code: 'invalid_smtp', message: 'smtp.host is required' },
            400
        );
        emailState.draft.host = '';
        emailState.setPassword('kept-pw');
        await emailState.save();

        expect(puts).toHaveLength(1);
        expect(emailState.draft.host).toBe('');
        expect(emailState.draft.password).toBe('kept-pw');
        expect(emailState.dirty).toBe(true);
    });
});
