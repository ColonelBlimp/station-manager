import { afterEach, describe, expect, it, vi } from 'vitest';
import { ft8SettingsState, setFt8PrefsSaved, type Ft8DisplayPrefs } from './ft8.svelte';
import { toastsState } from '../ui/toasts.svelte';

/*
    FT8 SETTINGS — WHAT GOES ON THE WIRE, AND WHAT REACHES THE LIVE VIEW.

    ACCEPTANCE CRITERIA (drafted before any mechanism, operator-checked
    2026-08-05 — the FT8 tab port that lets the standalone config SPA retire):

        A1  When I open Settings → FT8, it shows the values the daemon actually
            holds, and I can tell "no ft8 block yet, showing defaults" apart
            from "the load failed" — the second shows an error, not defaults.
        A2  When I change Row cap / Feed mode / Float CQ / Hide hashed and save,
            the FT8 view's Band Activity honours it WITHOUT a page reload, and I
            can tell that apart from "saved but not applied", which is what
            happens today (only F5 shows the change).
        A3  When I change the master switch, PSK Reporter or the decode log and
            save, the section tells me a restart is needed — and I can tell that
            apart from a display-pref change, which takes effect at once.
        A5  When a save is rejected, my typed values stay and it says why; I can
            tell that apart from success, which re-shows the daemon's RESOLVED
            values — a row cap of 5 comes back as 10.

    (A4 — the leave prompt naming FT8 — is pinned in unsaved.test.ts, where the
    other sections' prompt rules already live.)

    THE COLOUR RULE IS THE ONE THAT ISN'T IN THE CRITERIA, and it is here
    because it is invisible until it has already cost something. The operator
    ruled on 2026-08-05 that this shell renders NO colour pickers: the app's
    Band Activity uses a theme-aware palette, and a single operator-picked hex
    cannot serve both themes. But `ft8_display` is a WHOLE-BLOCK replace
    daemon-side, so "we don't render it" must not become "we don't send it" —
    that would silently erase a hand-set config.json colour the first time the
    operator saved an unrelated row cap. W1 is the guard.

    FIXTURE SHAPES DELIBERATELY CHOSEN — each one is a case where a wrong
    implementation and a right one would otherwise agree:

      - W1 loads NON-DEFAULT colours (#ff0000 …). With the daemon's own
        defaults in the fixture, "round-trips what it loaded" and "invents the
        defaults" produce identical payloads and the rule proves nothing.
      - W2 asserts the KEYS of the PUT body, not the ft8 blocks inside it. A
        body that is correct about FT8 while also carrying a stale
        logging_station passes any assertion aimed at the blocks — and that
        stale echo is precisely the config SPA's live bug.
      - W3 stores a row cap of 5 and has the daemon answer 10 (its clamp
        floor). If the request and the response agreed, "re-applies the
        daemon's answer" and "keeps what I typed" would be the same assertion.
      - W4 drives restartRequired in BOTH directions from the same loaded
        state. Asserting only that a PSK edit sets it would pass against an
        implementation that returns true for every edit — which is the whole
        distinction A3 asks the operator to be able to make.
      - W10 asserts the value PUSHED to the live view is the daemon's 10, not
        the typed 5. Pushing the draft is the obvious implementation and it is
        wrong: the feed would cap at a number the daemon never stored.
      - W7 loads a STORED port of 2525 before blanking it. Blanking a field
        that was already 0 leaves the payload identical either way.

    WHAT W10 CAN AND CANNOT PIN — found by its reversion proof, not by reading
    it. Pushing `buildPayload()` instead of the response is NOT a defect and
    W10 stays green against it, because #apply has already rebuilt the draft
    FROM that response by then; the two expressions are the same values. The
    failure W10 does catch is the ORDER: push before #apply and the typed 5
    goes to the live view. So the rule is about when the push happens, and a
    proof that only swaps the expression proves nothing.
*/

afterEach(() => {
    vi.restoreAllMocks();
    ft8SettingsState.loaded = false;
    ft8SettingsState.loading = false;
    ft8SettingsState.saving = false;
    ft8SettingsState.error = '';
    setFt8PrefsSaved(() => {});
    toastsState.items = [];
});

/** The message of the most recent toast, or '' if none was raised. */
function lastToast(): string {
    return toastsState.items.at(-1)?.message ?? '';
}

/**
 * The daemon's view: FT8 on, a hand-set colour scheme, a clamp-worthy row cap,
 * PSK on a non-default port, decode log off. `logging_station` / `station` are
 * present so a payload that echoes them has something to echo (W2).
 */
const CONFIG = {
    logging_station: { station_callsign: 'M0ABC', my_gridsquare: 'IO91' },
    station: { tx_power: '100' },
    ft8_enabled: true,
    ft8_display: {
        history_max: 250,
        feed_mode: 'single',
        cq_to_top: true,
        hide_hashed_calls: false,
        highlight_unworked: '#ff0000',
        highlight_worked: '#00ff00',
        highlight_calling: '#0000ff',
    },
    psk_reporter: { enabled: true, host: 'report.example.org', port: 2525 },
    ft8_decode_log: { enabled: false, path: '' },
};

type Body = Record<string, unknown>;

/** GET answers `get`; PUT answers `put` (default: the GET body, echoed). */
function mockDaemon(get: Body = CONFIG, put?: Body, putStatus = 200): ReturnType<typeof vi.fn> {
    const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
        const isPut = init?.method === 'PUT';
        const body = isPut ? (put ?? get) : get;
        return Promise.resolve(
            new Response(JSON.stringify(body), {
                status: isPut ? putStatus : 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
    });
    vi.stubGlobal('fetch', fetchMock);
    return fetchMock;
}

/** The JSON body of the first PUT the mock saw. */
function putBody(fetchMock: ReturnType<typeof vi.fn>): Body {
    const call = fetchMock.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'PUT'
    );
    expect(call, 'expected a PUT to /v1/config').toBeDefined();
    // `body` is BodyInit|null to TypeScript, but saveFt8Settings always sends a
    // JSON string — asserting that is what makes the parse meaningful.
    return JSON.parse((call![1] as RequestInit).body as string) as Body;
}

describe('ft8 settings — load', () => {
    it('S1: shows the values the daemon holds', async () => {
        mockDaemon();
        await ft8SettingsState.load();

        expect(ft8SettingsState.loaded).toBe(true);
        expect(ft8SettingsState.draft.enabled).toBe(true);
        expect(ft8SettingsState.draft.historyMax).toBe('250');
        expect(ft8SettingsState.draft.feedMode).toBe('single');
        expect(ft8SettingsState.draft.cqToTop).toBe(true);
        expect(ft8SettingsState.draft.hideHashedCalls).toBe(false);
        expect(ft8SettingsState.draft.pskEnabled).toBe(true);
        expect(ft8SettingsState.draft.pskHost).toBe('report.example.org');
        expect(ft8SettingsState.draft.pskPort).toBe('2525');
        expect(ft8SettingsState.draft.decodeLogEnabled).toBe(false);
        expect(ft8SettingsState.dirty).toBe(false);
    });

    it('S2: a failed load is NOT loaded — defaults must not read as current (A1)', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('', { status: 500 })))
        );
        await ft8SettingsState.load();

        expect(ft8SettingsState.loaded).toBe(false);
        expect(ft8SettingsState.error).not.toBe('');
    });

    it('S3: an absent ft8 block loads as blanks, not as an error (A1)', async () => {
        mockDaemon({ logging_station: {} });
        await ft8SettingsState.load();

        expect(ft8SettingsState.loaded).toBe(true);
        expect(ft8SettingsState.error).toBe('');
        expect(ft8SettingsState.draft.enabled).toBe(false);
        // 0 daemon-side means "use the default", and the box shows that as
        // blank — not as a literal 0, which would read as a chosen value.
        expect(ft8SettingsState.draft.historyMax).toBe('');
        expect(ft8SettingsState.draft.feedMode).toBe('accumulate');
    });

    it('S4: an edit makes it dirty; reset returns to the loaded values', async () => {
        mockDaemon();
        await ft8SettingsState.load();

        ft8SettingsState.draft.cqToTop = false;
        expect(ft8SettingsState.dirty).toBe(true);

        ft8SettingsState.reset();
        expect(ft8SettingsState.draft.cqToTop).toBe(true);
        expect(ft8SettingsState.dirty).toBe(false);
    });
});

describe('ft8 settings — the save payload', () => {
    it('W1: round-trips hand-set highlight colours this shell never renders', async () => {
        const fetchMock = mockDaemon();
        await ft8SettingsState.load();

        ft8SettingsState.draft.cqToTop = false; // an unrelated edit
        await ft8SettingsState.save();

        const display = putBody(fetchMock).ft8_display as Record<string, unknown>;
        expect(display.highlight_unworked).toBe('#ff0000');
        expect(display.highlight_worked).toBe('#00ff00');
        expect(display.highlight_calling).toBe('#0000ff');
    });

    it('W2: sends ONLY the four FT8 keys — never station identity (A5 safety)', async () => {
        const fetchMock = mockDaemon();
        await ft8SettingsState.load();

        ft8SettingsState.draft.hideHashedCalls = true;
        await ft8SettingsState.save();

        expect(Object.keys(putBody(fetchMock)).sort()).toEqual([
            'ft8_decode_log',
            'ft8_display',
            'ft8_enabled',
            'psk_reporter',
        ]);
    });

    it('W7: a blanked port and row cap go out as 0 — the daemon resolves them', async () => {
        const fetchMock = mockDaemon();
        await ft8SettingsState.load();

        ft8SettingsState.draft.pskPort = '';
        ft8SettingsState.draft.historyMax = '';
        await ft8SettingsState.save();

        const body = putBody(fetchMock);
        expect((body.psk_reporter as Record<string, unknown>).port).toBe(0);
        expect((body.ft8_display as Record<string, unknown>).history_max).toBe(0);
    });

    it('W8: refuses to save before a successful load — a whole-block write from an empty draft would erase everything', async () => {
        const fetchMock = mockDaemon();
        ft8SettingsState.loaded = false;
        ft8SettingsState.draft.pskHost = 'typed-while-unloaded';

        await ft8SettingsState.save();

        expect(fetchMock.mock.calls.some((c) => (c[1] as RequestInit)?.method === 'PUT')).toBe(
            false
        );
    });
});

describe('ft8 settings — the save response', () => {
    it('W3: re-shows the daemon RESOLVED values, not what I typed (A5)', async () => {
        const resolved = {
            ...CONFIG,
            ft8_display: { ...CONFIG.ft8_display, history_max: 10 },
        };
        mockDaemon(CONFIG, resolved);
        await ft8SettingsState.load();

        ft8SettingsState.draft.historyMax = '5'; // below the daemon's clamp floor
        await ft8SettingsState.save();

        expect(ft8SettingsState.draft.historyMax).toBe('10');
        expect(ft8SettingsState.dirty).toBe(false);
    });

    it('W4: a rejected save keeps my typed values and says why (A5)', async () => {
        mockDaemon(CONFIG, { code: 'invalid_config', message: 'feed_mode is invalid' }, 400);
        await ft8SettingsState.load();

        ft8SettingsState.draft.pskHost = 'typed.example.org';
        await ft8SettingsState.save();

        expect(ft8SettingsState.draft.pskHost).toBe('typed.example.org');
        expect(ft8SettingsState.dirty).toBe(true);
    });
});

describe('ft8 settings — restart required (A3)', () => {
    it('W5: a display-pref edit does NOT ask for a restart', async () => {
        mockDaemon();
        await ft8SettingsState.load();

        ft8SettingsState.draft.historyMax = '500';
        ft8SettingsState.draft.feedMode = 'accumulate';
        ft8SettingsState.draft.cqToTop = false;
        ft8SettingsState.draft.hideHashedCalls = true;

        expect(ft8SettingsState.dirty).toBe(true);
        expect(ft8SettingsState.restartRequired).toBe(false);
    });

    it('W6: the master switch, PSK Reporter and the decode log each DO', async () => {
        mockDaemon();

        await ft8SettingsState.load();
        ft8SettingsState.draft.enabled = false;
        expect(ft8SettingsState.restartRequired).toBe(true);

        ft8SettingsState.reset();
        ft8SettingsState.draft.pskHost = 'other.example.org';
        expect(ft8SettingsState.restartRequired).toBe(true);

        ft8SettingsState.reset();
        ft8SettingsState.draft.decodeLogEnabled = true;
        expect(ft8SettingsState.restartRequired).toBe(true);
    });
});

describe('ft8 settings — what the save CONFIRMATION says (A3)', () => {
    /*
        The in-page banner is gated on `dirty`, so it disappears the instant a
        save lands — at which point the confirmation is the only thing left that
        can say a restart is still owed. Reading `restartRequired` to decide
        that is a trap: #apply has already rebaselined the draft, so it is false
        by then whatever was saved. Both directions, because a confirmation that
        always mentions a restart carries no more information than one that
        never does.
    */
    it('W12: a saved restart-only change says so; a display-pref change does not', async () => {
        mockDaemon();
        await ft8SettingsState.load();
        ft8SettingsState.draft.pskHost = 'other.example.org';
        await ft8SettingsState.save();
        expect(lastToast()).toMatch(/restart/i);

        toastsState.items = [];
        await ft8SettingsState.load();
        ft8SettingsState.draft.cqToTop = false;
        await ft8SettingsState.save();
        expect(lastToast()).not.toMatch(/restart/i);
    });
});

describe('ft8 settings — an ambiguous write (clean-room review 569b2236 P2)', () => {
    /*
        A2/A5 both assume the save had an OUTCOME. A timed-out PUT has none: the
        request reached the daemon and the response did not come back, so the
        write may or may not have committed. `safeFetch` flags exactly this case
        (`timedOut`), and reporting it as "Save failed" states as fact the one
        thing we do not know — then invites a retry that resends all four blocks
        as whole-block replaces.

        The reconcile is a GET, and it is what turns the unknown back into
        something the operator can act on. It re-baselines the PRISTINE only and
        deliberately leaves the DRAFT alone: their typed values are never
        discarded on our guess, and `dirty` then means precisely "what I have
        differs from what the daemon holds" — true in both branches, and the
        answer to "did it land?" without asking anyone.

        FIXTURE: the two cases differ only in what the reconcile GET reports, so
        a single flow proves both. Asserting one alone would pass against an
        implementation that hard-coded either verdict.
    */
    function mockTimedOutPut(reconcile: Body): ReturnType<typeof vi.fn> {
        let gets = 0;
        const fetchMock = vi.fn((_url: string, init?: RequestInit) => {
            if (init?.method === 'PUT') {
                // What AbortSignal.timeout() raises — safeFetch keys on the name.
                const e = new Error('timed out');
                e.name = 'TimeoutError';
                return Promise.reject(e);
            }
            gets++;
            return Promise.resolve(
                new Response(JSON.stringify(gets === 1 ? CONFIG : reconcile), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        });
        vi.stubGlobal('fetch', fetchMock);
        return fetchMock;
    }

    const LANDED = { ...CONFIG, ft8_display: { ...CONFIG.ft8_display, feed_mode: 'accumulate' } };

    it('W13: a timed-out save is never reported as failed', async () => {
        mockTimedOutPut(LANDED);
        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate';
        await ft8SettingsState.save();

        expect(lastToast()).not.toMatch(/failed/i);
        expect(lastToast()).toMatch(/timed out/i);
    });

    it('W14: when the write DID land, the form settles clean and the view is updated', async () => {
        mockTimedOutPut(LANDED);
        const seen: Partial<Ft8DisplayPrefs>[] = [];
        setFt8PrefsSaved((p) => seen.push(p));

        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate';
        await ft8SettingsState.save();

        expect(ft8SettingsState.draft.feedMode).toBe('accumulate'); // never discarded
        expect(ft8SettingsState.dirty).toBe(false); // matches the daemon → it landed
        expect(seen.at(-1)?.feedMode).toBe('accumulate');
    });

    it('W15: when it did NOT land, the edit survives and still reads as unsaved', async () => {
        mockTimedOutPut(CONFIG); // the daemon still holds the pre-save block
        const seen: Partial<Ft8DisplayPrefs>[] = [];
        setFt8PrefsSaved((p) => seen.push(p));

        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate';
        await ft8SettingsState.save();

        expect(ft8SettingsState.draft.feedMode).toBe('accumulate'); // still typed
        expect(ft8SettingsState.dirty).toBe(true); // differs from the daemon
        // The live view mirrors what is STORED, not what was attempted.
        expect(seen.at(-1)?.feedMode).toBe('single');
    });

    it('W17: a value the daemon NORMALISES is not reported as missing', async () => {
        /*
            Clean-room review e41425f1 P2. The verdict cannot be "does the
            draft equal what is stored?", because the daemon normalises on the
            way in — a row cap of 5 is stored as 10, a host is trimmed. Every
            such save would then be reported as not landed, with the form left
            dirty and the operator told to retry a write that had succeeded.

            The sound question is whether the daemon's state MOVED from what we
            held before the write. Nobody else is writing, so it moved because
            of us — and the response is then authoritative, exactly as on the
            unambiguous success path.
        */
        mockTimedOutPut({ ...CONFIG, ft8_display: { ...CONFIG.ft8_display, history_max: 10 } });
        await ft8SettingsState.load();
        ft8SettingsState.draft.historyMax = '5'; // below the daemon's clamp floor
        await ft8SettingsState.save();

        expect(lastToast()).not.toMatch(/does NOT have|press Save again/i);
        expect(ft8SettingsState.draft.historyMax).toBe('10'); // what was stored
        expect(ft8SettingsState.dirty).toBe(false);
    });

    it('W18: another client’s change is not proof that MY save landed', async () => {
        /*
            Clean-room review f5ff2df5 P2. "The stored block differs from my
            baseline" is not the same claim as "my write landed" — the config
            SPA is still served at /config/ until its General tab is ported, so
            a second writer genuinely exists. Treating any movement as success
            would replace the operator's draft with the other client's values
            and announce it as theirs.

            So the question narrows to the FIELDS THIS SAVE CHANGED: did they
            move? That needs no knowledge of the daemon's normalisation — only
            whether what we asked to change is now different from what was
            there. Someone else editing a DIFFERENT field no longer counts.

            FIXTURE: the reconcile GET moves pskHost (someone else) while
            leaving feed_mode — the field this save edited — untouched. With a
            fixture that moved the edited field too, "landed" and "someone
            else wrote" would be indistinguishable and the rule would prove
            nothing.
        */
        mockTimedOutPut({
            ...CONFIG,
            psk_reporter: { ...CONFIG.psk_reporter, host: 'someone-else.example.org' },
        });
        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate';
        await ft8SettingsState.save();

        expect(ft8SettingsState.draft.feedMode).toBe('accumulate'); // never discarded
        expect(ft8SettingsState.dirty).toBe(true);
        expect(lastToast()).not.toMatch(/does have your/i);
    });

    it('W19: a PARTIAL move is not this save’s signature either', async () => {
        /*
            Added because W18's reversion proof did not fail when `every` was
            swapped for `some` — the code drew a distinction no rule pinned.

            A partial move has TWO readings and neither can be ruled out from
            here: another client moved a field we edited while our write
            failed, OR our write landed and the daemon normalised one edit back
            to the value already stored. Adopting the response would discard
            the operator's other edit under the first reading, so it does not.

            WHAT THIS COMMENT USED TO SAY WAS WRONG, and the correction is the
            point: it claimed a partial move MUST mean someone else moved it,
            reasoning from PUT atomicity. Atomicity rules out a half-applied
            write; it does not rule out a fully applied write whose effect on
            one field is invisible. Clean-room review bcfbd8ea P2 caught it,
            and W20 now pins the consequence — the verdict stays conservative,
            but the message must not assert the changes were not stored.
        */
        mockTimedOutPut({
            ...CONFIG,
            ft8_display: { ...CONFIG.ft8_display, feed_mode: 'accumulate' },
        });
        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate'; // moved in the response
        ft8SettingsState.draft.pskHost = 'mine.example.org'; // did NOT move
        await ft8SettingsState.save();

        expect(ft8SettingsState.draft.pskHost).toBe('mine.example.org');
        expect(ft8SettingsState.dirty).toBe(true);
        expect(lastToast()).not.toMatch(/does have your/i);
    });

    it('W20: a partial move is reported as UNCONFIRMED, never as "not stored"', async () => {
        /*
            Clean-room review bcfbd8ea P2. W19 keeps the conservative verdict —
            a partial move must not adopt the response — but the MESSAGE that
            went with it asserted the daemon did not have the changes, and that
            is not something a partial move establishes.

            The daemon normalises: an edit that resolves back to the value
            already stored never moves. Here the row cap is already AT the clamp
            floor, so typing 5 stores 10 again — no movement — while the feed
            mode moves. That is a fully successful atomic write showing as a
            partial move, and telling the operator it did not land is false.

            FIXTURE: the row cap must already be 10. At any other starting value
            the clamp WOULD move it, both fields would move, and the case this
            rule exists for could not arise.
        */
        const atFloor = { ...CONFIG, ft8_display: { ...CONFIG.ft8_display, history_max: 10 } };
        mockTimedOutPut({
            ...atFloor,
            ft8_display: { ...atFloor.ft8_display, feed_mode: 'accumulate' },
        });
        vi.stubGlobal(
            'fetch',
            vi.fn((_url: string, init?: RequestInit) => {
                if (init?.method === 'PUT') {
                    const e = new Error('timed out');
                    e.name = 'TimeoutError';
                    return Promise.reject(e);
                }
                return Promise.resolve(
                    new Response(
                        JSON.stringify(
                            ft8SettingsState.loaded
                                ? {
                                      ...atFloor,
                                      ft8_display: {
                                          ...atFloor.ft8_display,
                                          feed_mode: 'accumulate',
                                      },
                                  }
                                : atFloor
                        ),
                        { status: 200, headers: { 'Content-Type': 'application/json' } }
                    )
                );
            })
        );

        await ft8SettingsState.load();
        ft8SettingsState.draft.historyMax = '5'; // clamps back to the 10 already stored
        ft8SettingsState.draft.feedMode = 'accumulate'; // moves
        await ft8SettingsState.save();

        expect(lastToast()).not.toMatch(/does not appear|press Save again/i);
        // Still conservative: nothing typed is discarded (W19's half).
        expect(ft8SettingsState.draft.historyMax).toBe('5');
        expect(ft8SettingsState.dirty).toBe(true);
    });

    it('W16: a genuine connection failure is still a failure, not an unknown', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn((_url: string, init?: RequestInit) => {
                if (init?.method === 'PUT') return Promise.reject(new TypeError('refused'));
                return Promise.resolve(
                    new Response(JSON.stringify(CONFIG), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate';
        await ft8SettingsState.save();

        expect(lastToast()).toMatch(/failed/i);
        expect(ft8SettingsState.dirty).toBe(true);
    });
});

describe('ft8 settings — live apply (A2)', () => {
    it('W9: a saved display pref reaches the FT8 view without a reload', async () => {
        // The PUT response is spelled out rather than echoed from the GET: the
        // daemon stores what it accepted, and a mock that answered with the
        // PRE-edit block would be asserting the view gets stale values. It is
        // still not an echo of the REQUEST — that would make "pushes the draft"
        // and "pushes the response" the same implementation, which is the
        // distinction W10 exists to keep.
        mockDaemon(CONFIG, {
            ...CONFIG,
            ft8_display: { ...CONFIG.ft8_display, feed_mode: 'accumulate', cq_to_top: false },
        });
        const seen: Partial<Ft8DisplayPrefs>[] = [];
        setFt8PrefsSaved((p) => seen.push(p));

        await ft8SettingsState.load();
        ft8SettingsState.draft.feedMode = 'accumulate';
        ft8SettingsState.draft.cqToTop = false;
        await ft8SettingsState.save();

        expect(seen).toHaveLength(1);
        expect(seen[0].feedMode).toBe('accumulate');
        expect(seen[0].cqToTop).toBe(false);
    });

    it('W10: it pushes the daemon RESOLVED row cap, not the typed one', async () => {
        // Typed 5, stored 10 (the clamp floor). Pushing the draft would cap the
        // live feed at a number the daemon never accepted.
        mockDaemon(CONFIG, { ...CONFIG, ft8_display: { ...CONFIG.ft8_display, history_max: 10 } });
        const seen: Partial<Ft8DisplayPrefs>[] = [];
        setFt8PrefsSaved((p) => seen.push(p));

        await ft8SettingsState.load();
        ft8SettingsState.draft.historyMax = '5';
        await ft8SettingsState.save();

        expect(seen).toHaveLength(1);
        expect(seen[0].historyMax).toBe(10);
    });

    it('W11: a rejected save pushes nothing — the view must not show what was refused', async () => {
        mockDaemon(CONFIG, { message: 'nope' }, 400);
        const seen: Partial<Ft8DisplayPrefs>[] = [];
        setFt8PrefsSaved((p) => seen.push(p));

        await ft8SettingsState.load();
        ft8SettingsState.draft.cqToTop = false;
        await ft8SettingsState.save();

        expect(seen).toHaveLength(0);
    });
});
