/*
    Settings navigation guard — acceptance criteria.

    THE BEHAVIOUR THIS EXISTS FOR. Settings is rendered behind a router branch
    (App.svelte: `{:else if router.view === 'config'}`), so navigating away
    UNMOUNTS it while the state modules — module-level singletons — survive.
    The edits are therefore not lost when you leave. They are lost when you come
    BACK, because the remount's onMount → load() → #apply() overwrites the draft
    with the daemon's stored values. Nothing on screen marks the moment, so a
    half-filled SMTP block can disappear between two clicks with no warning.

    OPERATOR'S RULINGS (2026-08-04), which fix the outcomes below:
      · Keep discarding on return — the guard goes on the way OUT. Preserving
        drafts instead was rejected because the whole-block sections build a PUT
        from the draft, so a preserved-but-stale draft rewrites the whole block
        at stale values — the defect review dcb0316e69b9 filed as P1.
      · A blocking, cancellable window.confirm — matching the Restart daemon
        prompt already in Settings.svelte.
      · Tab close and reload warn too, via beforeunload.

    RIGS IS NOT EXEMPT, and the first version of this file wrongly said it was.
    #applyFetched re-baselines only PRISTINE drafts, so a dirty rig draft
    survives THAT step (review Rigs-editor #6) — and load() wipes drafts and
    baselines outright two statements later, undoing it. Reading the first half
    and stopping is how "Rigs survives the round trip" got written into a code
    comment, an ADR and two tests; the rig edits were silently lost on return
    exactly like everyone else's. R3b is the characterisation test that was
    missing: it runs the actual load() and asserts what is true at the END of
    the sequence. Caught in review, not by the reasoning that had been over this
    ground twice.

    A SAVE IN FLIGHT OUTRANKS THE PROMPT (R21). The dialog promises a discard;
    a PUT already on the wire cannot be recalled. It lands, the daemon persists
    the edits the operator was just told were thrown away, and #apply() writes
    them back into the form — the dialog having lied in the most expensive
    direction. So while any section is saving, leaving is refused outright and
    no promise is offered. Bounded, not a trap: these config writes use
    safeFetch's default 15 s timeout, so a wedged daemon cannot strand anyone.

    THE NEAREST CONFUSABLE STATES, which is where the defects live:
      · R1/R2 — "leaving with unsaved work" vs "leaving a form already saved".
        A guard that fires on a clean form is the failure mode that matters
        most: it trains the operator to dismiss it unread, after which the
        prompt protects nothing. R2 is the load-bearing half.
      · R5 — a pending password REMOVAL vs a field never touched. On screen the
        two are identical: both boxes are blank. Only the draft knows.
      · R6 — a save that was REFUSED vs one that succeeded. The draft is
        deliberately left as typed on failure (email.svelte.ts:140), so it is
        still unsaved work and must still be guarded.
      · R4 — unsaved edits on the rig you are LOOKING AT vs on one you edited
        earlier and navigated away from. rigsState.dirty answers only for the
        SELECTED rig (rigs.svelte.ts:97-101), so a guard keyed on it silently
        under-reports. R4 feeds a fixture where the two answers differ: rig 1
        edited, rig 2 selected — `dirty` is false there (pinned by
        rigs.svelte.test.ts:222) while real unsaved edits exist.
      · R21 — "your edits were discarded" vs "your edits were SAVED while the
        dialog said otherwise". Indistinguishable on screen; the difference is
        in the daemon.
    SETTINGS HAS THREE EXITS, NOT ONE, and they are easy to miscount because only
    the first is obvious:
      · The sidebar links — navigate(). R9/R10/R12.
      · Browser Back/forward — popstate, which never passes through navigate()
        (router.svelte.ts), so a guard installed only there lets Back straight
        past. It also fires AFTER the address bar has already moved, so refusing
        means putting the URL back: R11 asserts the URL, not just the view,
        because a view reading `config` under the previous entry's path is
        broken in its own way — a reload would land somewhere never chosen.
      · The sidebar's operating-mode buttons — setMode(). OperateNav lives in
        the always-visible sidebar (Sidebar.svelte:104), so Phone/FT8 leave
        Settings without touching navigate() at all. R13b.

    NOTE ON THE NEGATIVE RULES (R2, R12, R13, R14, R17, R18): they pass against
    the un-guarded code too — a guard that does nothing cannot over-fire. They
    are not evidence the feature works, they are the regression net for the
    false positives above. R3b is likewise not a guard test: it characterises
    rigs.svelte.ts and would pass with no guard at all. Its job is to hold down
    the FACT the design rests on, which is precisely what nothing did before.

    Reversion proofs, taken 2026-08-04 against each mechanism separately, each
    failing on its own rule's assertion: removing the navigate() guard reddened
    R9 alone; the popstate guard, R11; the setMode guard, R13b; rigsState
    .anyDirty, R4 and R16; e.preventDefault(), R15 and R16; the immediate
    discard, R19 and R20; re-exempting Rigs, R3/R4/R16/R20; dropping the
    in-flight-save refusal, R21.

    A NOTE ON FIXTURES. dirtyEmail() exists because two rules once passed with a
    hard-coded host that happened to equal the #pristine a previous test's save
    had written — the fixture made the guarded and unguarded paths agree. It now
    varies per call and asserts the state actually went dirty.
*/

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installSettingsGuards, leavePrompt, unsavedSections } from './unsaved';
import { stationState } from './station.svelte';
import { rigsState } from './rigs.svelte';
import { forwardingState } from './forwarding.svelte';
import { emailState } from './email.svelte';
import { enrichmentState } from './enrichment.svelte';
import { ft8SettingsState } from './ft8.svelte';
import { navigate, router, setMode } from '../router.svelte';

// Each state back to not-dirty. Uses their OWN reset() — which restores the
// draft from the private #pristine — rather than a snapshot taken at import:
// any test that calls load() or save() rewrites #pristine, after which a stored
// snapshot no longer compares equal and the state reads dirty for the rest of
// the run.
function makeAllClean(): void {
    stationState.reset();
    forwardingState.reset();
    emailState.reset();
    enrichmentState.reset();
    ft8SettingsState.reset();
    rigsState.drafts = {};
    rigsState.baselines = {};
    rigsState.selectedId = null;
    emailState.saving = false;
}

/**
 * Make Email unmistakably dirty and PROVE the fixture did so.
 *
 * The value must be unique per call: a test that runs after one which load()ed
 * or save()d is comparing against a #pristine those rewrote, so a hard-coded
 * host can silently equal the pristine value and leave the state clean. Two
 * rules passed for that reason before this existed — the fixture made both the
 * guarded and unguarded paths agree.
 */
let hostSeq = 0;
function dirtyEmail(): string {
    const host = `smtp${++hostSeq}.example.net`;
    emailState.draft.host = host;
    expect(emailState.dirty).toBe(true);
    return host;
}

/**
 * Make FT8 unmistakably dirty and PROVE the fixture did so — same unique-value
 * discipline as dirtyEmail, and for the same reason.
 */
let ft8Seq = 0;
function dirtyFt8(): void {
    ft8SettingsState.draft.pskHost = `psk${++ft8Seq}.example.net`;
    expect(ft8SettingsState.dirty).toBe(true);
}

/** Serve GET /v1/rigs + /v1/hardware so rigsState.load() can complete. */
function mockRigsFetch(): void {
    const resp = (body: unknown) =>
        Promise.resolve(
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
    vi.stubGlobal(
        'fetch',
        vi.fn((url: string) =>
            url.includes('/v1/hardware')
                ? resp({ serial_ports: [], audio: { available: false } })
                : resp({
                      default_rig_id: 1,
                      rigs: [
                          { id: 1, model: 'ftdx10', port: '/dev/a' },
                          { id: 2, model: 'ic7300', port: '/dev/b' },
                      ],
                      catalogue: [],
                  })
        )
    );
}

/**
 * A PUT that hangs until release() is called — an in-flight save.
 *
 * The GET side still answers, so load() works normally; only the write is held
 * open. Nothing weaker reproduces the defect: the harm is what the daemon does
 * with a request already on the wire when the operator agrees to discard.
 */
function deferredSaveFetch(): { release: () => void } {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const smtp = { enabled: true, host: 'smtp.example.net', port: 587, password_set: false };
    const resp = (body: unknown) =>
        new Response(JSON.stringify(body), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
        });
    vi.stubGlobal(
        'fetch',
        vi.fn(async (_url: string, init?: RequestInit) => {
            if ((init?.method ?? 'GET') === 'PUT') {
                await gate;
                return resp({ smtp });
            }
            return resp({ smtp: { enabled: false, host: '', port: 0, password_set: false } });
        })
    );
    return { release };
}

/** Two rigs, rig 1 edited, rig 2 selected — the R4 fixture. */
function dirtyRigOffScreen(): void {
    rigsState.baselines = {
        1: { id: 1, model: 'ftdx10', port: '/dev/a' },
        2: { id: 2, model: 'ic7300', port: '/dev/b' },
    };
    rigsState.drafts = {
        1: { id: 1, model: 'ftdx10', port: '/dev/EDITED' },
        2: { id: 2, model: 'ic7300', port: '/dev/b' },
    };
    rigsState.rigs = [
        { id: 1, model: 'ftdx10', port: '/dev/a' },
        { id: 2, model: 'ic7300', port: '/dev/b' },
    ];
    rigsState.selectedId = 2; // looking at the PRISTINE rig
}

/**
 * Rig 1 edited AND selected — the R3 fixture.
 *
 * It has to be the SELECTED rig: with dirtyRigOffScreen(), rigsState.dirty is
 * already false, so "Rigs is absent from the leave set" would hold whether the
 * exemption existed or not, and R3 would prove nothing. Here both dirty and
 * anyDirty are true, so excluding Rigs is a decision the code has to make.
 */
function dirtyRigOnScreen(): void {
    dirtyRigOffScreen();
    rigsState.selectedId = 1;
}

describe('which sections have unsaved edits', () => {
    afterEach(makeAllClean);

    it('R1: names a section with unsaved edits', () => {
        dirtyEmail();
        expect(unsavedSections()).toEqual(['Email']);
    });

    it('R2: says nothing when every section is saved', () => {
        expect(unsavedSections()).toEqual([]);
    });

    it('R3: counts Rigs like everything else — its drafts do NOT survive a remount', () => {
        dirtyRigOnScreen();
        expect(rigsState.dirty).toBe(true);
        expect(unsavedSections()).toEqual(['Rigs']);
    });

    it('R3b: a successful rigs load() wipes every draft — the fact R3 rests on', () => {
        // The characterisation test that was missing, and whose absence let a
        // false exemption ship. #applyFetched carefully preserves DIRTY drafts
        // (rigs.svelte.ts, review Rigs-editor #6) and load() then discards them
        // outright two statements later. Reading only the first is how "Rigs
        // survives the round trip" got written down. Assert the END of the
        // sequence, not the step that looked decisive.
        mockRigsFetch();
        dirtyRigOnScreen();
        expect(rigsState.anyDirty).toBe(true);
        return rigsState.load().then(() => {
            expect(rigsState.anyDirty).toBe(false); // the remount's edit-eater
        });
    });

    it('R4: counts a rig edited but not selected — `dirty` alone under-reports', () => {
        dirtyRigOffScreen();
        // The fixture's whole point: the cheap answer disagrees with the true one.
        expect(rigsState.dirty).toBe(false);
        expect(unsavedSections()).toEqual(['Rigs']);
    });

    it('R5: counts a pending password removal, which looks identical on screen', () => {
        emailState.clearPassword();
        expect(emailState.draft.password).toBe(''); // nothing typed, nothing visible
        expect(unsavedSections()).toEqual(['Email']);
    });

    it('R6: still counts edits after a save the daemon refused', () => {
        dirtyEmail();
        emailState.saving = false; // the save returned; the draft is left as typed
        expect(unsavedSections()).toEqual(['Email']);
    });

    it('R7: lists every dirty section, in the order the tabs are shown', () => {
        // FT8 sits third on the strip, so this pins its POSITION and not merely
        // that it is counted — a section appended to the end of SECTIONS would
        // still be listed, just in an order that no longer reads as a walk
        // across the tabs.
        stationState.form = { station_callsign: '7Q5MLV' };
        dirtyFt8();
        dirtyEmail();
        enrichmentState.draft.countryTtlDays = '5';
        expect(unsavedSections()).toEqual(['Station', 'FT8', 'Email', 'Enrichment']);
    });
});

describe('the prompt names what is at stake', () => {
    it('R8: reads naturally for one, two and three sections', () => {
        expect(leavePrompt(['Email'])).toContain('Unsaved changes in Email will be discarded');
        expect(leavePrompt(['Email', 'Enrichment'])).toContain('Email and Enrichment');
        expect(leavePrompt(['Station', 'Email', 'Enrichment'])).toContain(
            'Station, Email and Enrichment'
        );
    });
});

describe('leaving Settings', () => {
    let uninstall: () => void;

    beforeEach(() => {
        makeAllClean();
        uninstall = installSettingsGuards();
        navigate('config');
    });

    afterEach(() => {
        uninstall();
        makeAllClean();
        vi.restoreAllMocks();
    });

    it('R9: a cancelled confirm keeps me on Settings with my edits', () => {
        const host = dirtyEmail();
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        navigate('logbook');
        expect(confirm).toHaveBeenCalledOnce();
        expect(router.view).toBe('config');
        expect(window.location.pathname).toMatch(/\/config$/);
        expect(emailState.draft.host).toBe(host);
    });

    it('R10: confirming leaves', () => {
        dirtyEmail();
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        navigate('logbook');
        expect(router.view).toBe('logbook');
        expect(window.location.pathname).toMatch(/\/logbook$/);
    });

    it('R11: browser Back is guarded too, and the address bar is put back', () => {
        dirtyEmail();
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        // Back: the URL has ALREADY changed by the time popstate fires.
        window.history.pushState({}, '', '/logbook');
        window.dispatchEvent(new PopStateEvent('popstate'));
        expect(confirm).toHaveBeenCalledOnce();
        expect(router.view).toBe('config');
        expect(window.location.pathname).toMatch(/\/config$/);
    });

    it('R12: a saved Settings navigates straight through, unprompted', () => {
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        navigate('logbook');
        expect(confirm).not.toHaveBeenCalled();
        expect(router.view).toBe('logbook');
    });

    it('R13: leaving a view that is NOT Settings is never guarded', () => {
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        navigate('logbook'); // clean, goes through
        dirtyEmail(); // dirty, but we are not in Settings
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        navigate('map');
        expect(confirm).not.toHaveBeenCalled();
        expect(router.view).toBe('map');
    });

    it('R19: confirming the discard actually discards it, there and then', () => {
        // Otherwise the app keeps holding edits it has just told the operator
        // are gone — and beforeunload warns about the same ones a second time,
        // which is the false alarm R2 exists to prevent, arriving by the back
        // door. The remount's load() would clear them on RETURN, but "on
        // return" is not when the promise was made.
        dirtyEmail();
        enrichmentState.draft.countryTtlDays = '5';
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        navigate('logbook');
        expect(unsavedSections()).toEqual([]); // nothing left to warn about
    });

    it('R20: a confirmed leave discards Rigs too', () => {
        dirtyRigOnScreen();
        dirtyEmail();
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        navigate('logbook');
        expect(rigsState.anyDirty).toBe(false);
        expect(unsavedSections()).toEqual([]);
    });

    it('R21: refuses to leave while a save is in flight, and discards nothing', async () => {
        // The dialog promises a DISCARD, but a PUT already on the wire cannot be
        // recalled: it lands, the daemon persists the very edits the operator
        // was told were thrown away, and #apply() then writes them back into the
        // form. So the honest answer while saving is not to offer the choice.
        // Bounded, not a trap — these writes use safeFetch's default 15 s timeout.
        const { release } = deferredSaveFetch();
        await emailState.load();
        emailState.draft.host = 'smtp.example.net';
        const saved = emailState.save(); // in flight, deliberately not awaited
        expect(emailState.saving).toBe(true);

        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
        navigate('logbook');
        expect(confirm).not.toHaveBeenCalled(); // never offered a promise it can't keep
        expect(router.view).toBe('config');
        expect(emailState.draft.host).toBe('smtp.example.net'); // nothing discarded

        release();
        await saved;
        expect(emailState.saving).toBe(false);
    });

    it('R22: leaving works normally once that save completes', async () => {
        const { release } = deferredSaveFetch();
        await emailState.load();
        emailState.draft.host = 'smtp.example.net';
        const saved = emailState.save();
        release();
        await saved;
        // The save succeeded, so there is nothing unsaved left to guard.
        vi.spyOn(window, 'confirm').mockReturnValue(false);
        navigate('logbook');
        expect(router.view).toBe('logbook');
    });

    it('R13b: the sidebar operating-mode buttons are an exit too', () => {
        // OperateNav sits in the always-visible sidebar (Sidebar.svelte:104), so
        // its Phone/FT8 buttons leave Settings via setMode — never touching
        // navigate(). A guard installed only in navigate() leaves this door open.
        dirtyEmail();
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        setMode('ft8');
        expect(confirm).toHaveBeenCalledOnce();
        expect(router.view).toBe('config');
    });

    it('R14: moving between Settings tabs is not leaving', () => {
        dirtyEmail();
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        navigate('config');
        expect(confirm).not.toHaveBeenCalled();
        expect(router.view).toBe('config');
    });
});

describe('closing the tab', () => {
    let uninstall: () => void;

    beforeEach(() => {
        makeAllClean();
        uninstall = installSettingsGuards();
    });

    afterEach(() => {
        uninstall();
        makeAllClean();
    });

    const unload = (): boolean =>
        !window.dispatchEvent(new Event('beforeunload', { cancelable: true }));

    it('R15: warns when a section has unsaved edits', () => {
        dirtyEmail();
        expect(unload()).toBe(true);
    });

    it('R16: warns for Rigs too — module state dies with the page', () => {
        dirtyRigOffScreen();
        expect(unload()).toBe(true);
    });

    it('R17: does not warn when everything is saved', () => {
        expect(unload()).toBe(false);
    });

    it('R18: stops warning once uninstalled', () => {
        dirtyEmail();
        uninstall();
        expect(unload()).toBe(false);
        uninstall = () => {}; // afterEach must not double-uninstall
    });
});
