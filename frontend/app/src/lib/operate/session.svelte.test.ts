// The Session the page shows is the daemon's (W-0029 part 6, criteria A and B).
// The nearest confusable outcome is a panel that looks right because it still
// draws rows of its own. So every case here is judged on what the page has READ:
// the daemon's answers are the fixture, they are held and released by the test,
// and nothing but an answer may put a row on the screen or take one off.

import { describe, it, expect, beforeEach } from 'vitest';
import {
    session,
    sessionModeLiteral,
    sessionDisplayTime,
    setSessionPorts,
    sessionStateDrawn,
    refreshSessionRows,
    sessionContactStored,
    sessionStreamReopened,
    sessionEmailSent,
    _resetSessionForTests,
    type DrawnSessionState,
    type SessionRowLike,
    type SessionRowsReadLike,
} from './session.svelte';
import { _reloadArchiveStoresForTests, bindArchiveStores } from './archiveStores.svelte';

const ARCHIVE = '01a16a00-0000-7000-8000-000000000001';
const OTHER_ARCHIVE = '01a16a00-0000-7000-8000-000000000002';
const S1 = '01a1f83c-da20-7000-8000-0000000000a1';
const S2 = '01a1f83c-da20-7000-8000-0000000000a2';

function row(call: string, over: Partial<SessionRowLike> = {}): SessionRowLike {
    return {
        uuid: `01a21282-0000-7000-8000-${call.padStart(12, '0').toLowerCase()}`,
        call,
        timeOn: '143000',
        band: '20m',
        mode: 'SSB',
        rstSent: '59',
        rstRcvd: '57',
        name: '',
        country: '',
        comment: '',
        emailed: false,
        ...over,
    };
}

function rows(
    sessionId: string | null,
    generation: number,
    members: SessionRowLike[],
    archiveId = ARCHIVE
): SessionRowsReadLike {
    return { kind: 'ok', archiveId, generation, sessionId, members };
}

function open(
    sessionId: string,
    generation: number,
    over: Partial<DrawnSessionState> = {}
): DrawnSessionState {
    return {
        archiveId: ARCHIVE,
        generation,
        state: 'open',
        session: {
            id: sessionId,
            openedAt: '2026-11-25T09:15:00Z',
            sittingStartedAt: '2026-11-25T09:15:00Z',
        },
        ...over,
    };
}

// The daemon as the test holds it: each read of the rows waits until the test
// answers it, in whatever order the test chooses.
class Daemon {
    readonly sent: ((a: SessionRowsReadLike) => void)[] = [];
    /** One for each read of the state asked for: call it when that read has finished. */
    readonly stateDone: (() => void)[] = [];
    stateReads = 0;
    bound: string | null = ARCHIVE;

    connect(): void {
        setSessionPorts({
            readRows: () => new Promise<SessionRowsReadLike>((resolve) => this.sent.push(resolve)),
            readState: () => {
                this.stateReads++;
                return new Promise<void>((resolve) => this.stateDone.push(resolve));
            },
            archive: () => this.bound,
        });
    }

    /** Answer the n-th read sent (from 1) and let the page take the answer in. */
    async answer(n: number, a: SessionRowsReadLike): Promise<void> {
        const resolve = this.sent[n - 1];
        if (resolve === undefined) throw new Error(`read ${n} was never sent`);
        resolve(a);
        await settle();
    }
}

async function settle(): Promise<void> {
    for (let i = 0; i < 5; i++) await Promise.resolve();
}

const calls = (): string[] => session.qsos.map((q) => q.callsign);

let daemon: Daemon;

beforeEach(() => {
    _resetSessionForTests();
    daemon = new Daemon();
    daemon.connect();
});

describe('the rows are the daemon’s (A)', () => {
    it('shows the open session’s contacts in the daemon’s order, in the panel’s shape', async () => {
        sessionStateDrawn(open(S1, 4));
        expect(session.rows).toBe('loading');
        expect(daemon.sent).toHaveLength(1);

        await daemon.answer(
            1,
            rows(S1, 4, [
                row('K2B', { mode: 'MFSK', submode: 'FT4', timeOn: '0915', emailed: true }),
                row('G4A', { name: 'Ann', country: 'England', comment: 'qrp' }),
            ])
        );

        expect(session.rows).toBe('ok');
        expect(calls()).toEqual(['K2B', 'G4A']);
        expect(session.qsos[0]).toMatchObject({ mode: 'FT4', timeOn: '09:15', emailed: true });
        expect(session.qsos[1]).toMatchObject({
            mode: 'SSB',
            timeOn: '14:30:00',
            rstSent: '59',
            rstRcvd: '57',
            name: 'Ann',
            country: 'England',
            comment: 'qrp',
            emailed: false,
        });
        expect(session.sessionId).toBe(S1);
        expect(session.openedAt).toBe('2026-11-25T09:15:00Z');
    });

    it('adds no row by itself: a change only asks, and the answer is what is drawn', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        // This window logged K2B, an FT8 completion arrived, a contact was
        // edited: each is only a reason to read.
        refreshSessionRows();
        sessionContactStored(S1);
        await settle();
        expect(calls()).toEqual(['G4A']);

        // The daemon says K2B did not join (a deliberate submit to another
        // logbook): it is stored, and it is not in the panel.
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(calls()).toEqual(['G4A']);
    });

    it('keeps one key for a contact from one read to the next', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        const key = session.qsos[0].id;

        refreshSessionRows();
        await daemon.answer(2, rows(S1, 4, [row('K2B'), row('G4A')]));

        expect(session.qsos[1].id).toBe(key);
        expect(session.qsos[0].id).not.toBe(key);
    });
});

describe('never an older, a foreign or an overwritten list (B)', () => {
    it('B2: draws no rows of another archive, and none when the page’s archive is unproven', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')], OTHER_ARCHIVE));
        expect(calls()).toEqual([]);

        // Sent with no archive proven: not drawn, even answered for the right one.
        daemon.bound = null;
        refreshSessionRows();
        daemon.bound = ARCHIVE;
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(calls()).toEqual([]);

        // Bound when sent, and no longer when the answer comes.
        refreshSessionRows();
        daemon.bound = OTHER_ARCHIVE;
        await daemon.answer(3, rows(S1, 4, [row('G4A')]));
        expect(calls()).toEqual([]);
    });

    it('B2: drops rows of a session or generation it does not show, and reads the state', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        refreshSessionRows();
        await daemon.answer(2, rows(S2, 5, [row('K2B')]));
        expect(calls()).toEqual(['G4A']);
        expect(daemon.stateReads).toBe(1);

        // The same session at another generation is not the one shown either.
        refreshSessionRows();
        await daemon.answer(3, rows(S1, 6, [row('K2B')]));
        expect(calls()).toEqual(['G4A']);
        expect(daemon.stateReads).toBe(2);

        // No session open on the daemon's side.
        refreshSessionRows();
        await daemon.answer(4, rows(null, 7, []));
        expect(calls()).toEqual(['G4A']);
        expect(daemon.stateReads).toBe(3);
    });

    // The operator's ruling of 2026-11-26: a contact that joins while a read is
    // out may wait for that read. So what is on screen is stated at every step:
    // the wait is real, it changes nothing already shown, and it ends.
    it('B3: a contact that joins while a read is out waits for it, and is not lost to it', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        refreshSessionRows(); // read 2 is out, from before K2B joined
        sessionContactStored(S1); // K2B joins
        await settle();
        expect(daemon.sent).toHaveLength(2); // nothing more is sent while it is out
        expect(calls()).toEqual(['G4A']); // K2B is not on screen yet; G4A is untouched

        await daemon.answer(2, rows(S1, 4, [row('G4A')])); // the older snapshot, released
        expect(calls()).toEqual(['G4A']);
        expect(daemon.sent).toHaveLength(3); // one more read, for what arrived meanwhile

        await daemon.answer(3, rows(S1, 4, [row('K2B'), row('G4A')]));
        expect(calls()).toEqual(['K2B', 'G4A']);
        expect(daemon.sent).toHaveLength(3);
    });

    it('B3: the wait is bounded: a read given up at its limit is followed by the next', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        refreshSessionRows(); // read 2 hangs
        sessionContactStored(S1); // K2B joins behind it
        // The read ends at its own limit, as the page's fetch ends every request.
        await daemon.answer(2, { kind: 'error', message: 'The request timed out.' });
        expect(calls()).toEqual(['G4A']); // nothing drawn is taken away
        expect(session.rows).toBe('ok');
        expect(daemon.sent).toHaveLength(3); // and the read for K2B goes now

        await daemon.answer(3, rows(S1, 4, [row('K2B'), row('G4A')]));
        expect(calls()).toEqual(['K2B', 'G4A']);
    });

    it('B3: one further read answers every change that arrived while a read was out', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, []));

        refreshSessionRows();
        for (let i = 0; i < 200; i++) refreshSessionRows(); // a send stamps 200 contacts
        expect(daemon.sent).toHaveLength(2);
        await daemon.answer(2, rows(S1, 4, []));
        expect(daemon.sent).toHaveLength(3);
        await daemon.answer(3, rows(S1, 4, [row('G4A')]));
        expect(daemon.sent).toHaveLength(3);
        expect(calls()).toEqual(['G4A']);
    });

    it('B3: a read held across a break in the stream never puts back a contact deleted meanwhile', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('K2B'), row('G4A')]));

        refreshSessionRows(); // read 2, held; it saw K2B
        // The stream breaks; K2B is deleted and nobody is told; the stream reopens.
        sessionStreamReopened();
        expect(daemon.sent).toHaveLength(3); // read at once, not behind the held one
        await daemon.answer(3, rows(S1, 4, [row('G4A')]));
        expect(calls()).toEqual(['G4A']);

        await daemon.answer(2, rows(S1, 4, [row('K2B'), row('G4A')])); // the earlier connection's, last
        expect(calls()).toEqual(['G4A']);
        expect(daemon.sent).toHaveLength(3);
    });

    it('B4: a deleted contact does not return, and the later of two edits stays', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A', { name: 'first' })]));

        // Edited twice while one read is out: the answers come in the order sent.
        refreshSessionRows(); // after the first edit
        refreshSessionRows(); // after the second
        expect(session.qsos[0].name).toBe('first'); // neither edit is on screen yet
        await daemon.answer(2, rows(S1, 4, [row('G4A', { name: 'second' })]));
        expect(session.qsos[0].name).toBe('second');
        await daemon.answer(3, rows(S1, 4, [row('G4A', { name: 'third' })]));
        expect(session.qsos[0].name).toBe('third');

        // Deleted while a read that still holds the row is out.
        refreshSessionRows();
        refreshSessionRows();
        await daemon.answer(4, rows(S1, 4, [row('G4A', { name: 'third' })]));
        expect(calls()).toEqual(['G4A']); // the read that was out still held it
        await daemon.answer(5, rows(S1, 4, []));
        expect(calls()).toEqual([]);
        expect(session.rows).toBe('ok');
        expect(daemon.sent).toHaveLength(5); // and nothing brings it back
    });

    it('B5: after the session changes no row of the one before stays, even for a moment', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        refreshSessionRows(); // read 2 of S1, out when New arrives
        sessionStateDrawn(open(S2, 5));
        expect(calls()).toEqual([]);
        expect(session.rows).toBe('loading');
        expect(session.sessionId).toBe(S2);

        await daemon.answer(2, rows(S1, 4, [row('K2B'), row('G4A')])); // the old session's, late
        expect(calls()).toEqual([]);
        expect(daemon.stateReads).toBe(0); // given up, not mistaken for a newer state

        await daemon.answer(3, rows(S2, 5, []));
        expect(calls()).toEqual([]);
        expect(session.rows).toBe('ok');
    });

    it('B2: a session brought back at a new generation has its rows read again, not kept', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStateDrawn(open(S1, 8)); // New, then Continue
        expect(calls()).toEqual([]);
        expect(daemon.sent).toHaveLength(2);
        await daemon.answer(2, rows(S1, 8, [row('G4A')]));
        expect(calls()).toEqual(['G4A']);

        // The same state drawn again (a reread at a reopening) keeps the rows.
        sessionStateDrawn(open(S1, 8));
        expect(calls()).toEqual(['G4A']);
        expect(daemon.sent).toHaveLength(2);
    });

    it('B6: when the stream reopens the rows are read again', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStreamReopened();
        expect(daemon.sent).toHaveLength(2);
        await daemon.answer(2, rows(S1, 4, [row('K2B'), row('G4A')]));
        expect(calls()).toEqual(['K2B', 'G4A']);
    });

    it('B7: a read that fails takes nothing away, and is never shown as an empty session', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, { kind: 'error', message: 'Cannot reach the daemon.' });
        expect(session.rows).toBe('failed');
        expect(session.error).toBe('Cannot reach the daemon.');

        refreshSessionRows(); // tried again at the next change
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(session.rows).toBe('ok');
        expect(session.error).toBe('');

        refreshSessionRows();
        await daemon.answer(3, { kind: 'error', message: 'Cannot reach the daemon.' });
        expect(calls()).toEqual(['G4A']);
        expect(session.rows).toBe('ok');
    });

    it('B8: removes what an earlier build left in the tab’s storage, and shows none of it', () => {
        sessionStorage.setItem(
            `sm.session.qsos.${ARCHIVE}`,
            JSON.stringify({ qsos: [{ id: 1, uuid: 'x', callsign: 'OLD1' }] })
        );
        sessionStorage.setItem(`sm.session.qsos.${OTHER_ARCHIVE}`, '{"qsos":[]}');
        sessionStorage.setItem('sm.session.startedAt', '1');
        sessionStorage.setItem('sm.ft8.engaged.' + ARCHIVE, '["kept"]');

        _reloadArchiveStoresForTests();
        bindArchiveStores(ARCHIVE);

        expect(sessionStorage.getItem(`sm.session.qsos.${ARCHIVE}`)).toBeNull();
        expect(sessionStorage.getItem(`sm.session.qsos.${OTHER_ARCHIVE}`)).toBeNull();
        expect(sessionStorage.getItem('sm.session.startedAt')).toBeNull();
        expect(sessionStorage.getItem('sm.ft8.engaged.' + ARCHIVE)).toBe('["kept"]');
        expect(calls()).toEqual([]);
    });

    it('B10: the list built from events has the order a reload shows', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, []));

        // A is stored and the read for it takes its snapshot; B is stored while
        // that read is out. No further event comes, and there is no reload.
        sessionContactStored(S1);
        sessionContactStored(S1);
        await daemon.answer(2, rows(S1, 4, [row('A1A')]));
        await daemon.answer(3, rows(S1, 4, [row('B2B'), row('A1A')]));

        expect(calls()).toEqual(['B2B', 'A1A']); // B above A, as the daemon orders them
    });
});

describe('the states (C) and the first contact', () => {
    it('with no session open there are no rows and none are read', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        for (const state of ['none', 'archive_switching'] as const) {
            sessionStateDrawn({ archiveId: ARCHIVE, generation: 5, state, session: null });
            expect(session.state).toBe(state);
            expect(calls()).toEqual([]);
            expect(session.rows).toBe('none');
            expect(session.sessionId).toBeNull();
            expect(session.sittingStartedAt).toBeNull();
            refreshSessionRows();
            sessionStreamReopened();
        }
        expect(daemon.sent).toHaveLength(1);
    });

    it('the contact that opens a session is found by reading the state, not added to another', async () => {
        sessionStateDrawn({ archiveId: ARCHIVE, generation: 3, state: 'none', session: null });

        sessionContactStored(S1); // announced before the session's own change
        expect(daemon.stateReads).toBe(1);
        expect(daemon.sent).toHaveLength(0);

        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(calls()).toEqual(['G4A']);

        // A contact of a session the page does not show reads the state too.
        sessionContactStored(S2);
        expect(daemon.stateReads).toBe(2);
        expect(daemon.sent).toHaveLength(1);

        // A contact that joined no session (an import, another logbook) asks nothing.
        sessionContactStored(undefined);
        sessionContactStored('');
        expect(daemon.stateReads).toBe(2);
        expect(daemon.sent).toHaveLength(1);
    });
});

describe('what a row shows', () => {
    it('shows the submode when the record has one', () => {
        expect(sessionModeLiteral('MFSK', 'FT4')).toBe('FT4');
        expect(sessionModeLiteral('SSB', 'USB')).toBe('USB');
        expect(sessionModeLiteral('FT8', '')).toBe('FT8');
        expect(sessionModeLiteral('CW')).toBe('CW');
        expect(sessionModeLiteral()).toBe('');
    });

    it('shows ADIF times as the list does', () => {
        expect(sessionDisplayTime('143005')).toBe('14:30:05');
        expect(sessionDisplayTime('1430')).toBe('14:30');
        expect(sessionDisplayTime('')).toBe('');
        expect(sessionDisplayTime('14:30')).toBe('14:30');
    });
});

// Found by Codex's review of the cutover (codex-273). The page waits for the
// next event after a read fails; while it waits it must not say something it
// does not know.
describe('what is said while the page waits after a failed read (B7)', () => {
    it('an empty session whose next read fails is not still said to be empty', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [])); // the daemon answered: no contact
        expect(session.rows).toBe('ok');

        sessionContactStored(S1); // a contact joined; the page knows the list has changed
        await daemon.answer(2, { kind: 'error', message: 'Cannot reach the daemon.' });

        expect(calls()).toEqual([]);
        expect(session.rows).toBe('failed'); // not 'ok', which the panel reads as "No QSOs logged"
        expect(session.error).toBe('Cannot reach the daemon.');
    });

    it('rows already on screen stay through a failed read, and are not called unread', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        refreshSessionRows();
        await daemon.answer(2, { kind: 'error', message: 'Cannot reach the daemon.' });
        expect(calls()).toEqual(['G4A']);
        expect(session.rows).toBe('ok');
    });

    it('rows of a later generation, when the state cannot then be read, are not left as "reading"', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 5, [row('K2B')])); // the daemon has moved on
        expect(daemon.stateReads).toBe(1);
        expect(session.rows).toBe('loading'); // the state is being read: nothing is claimed yet

        daemon.stateDone[0](); // that read has finished, and drew nothing: it failed
        await settle();

        expect(calls()).toEqual([]);
        expect(session.rows).toBe('failed');
        expect(session.error).not.toBe('');
    });

    it('when that state read draws the new session, its rows are read and nothing is called failed', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S2, 5, [row('K2B')]));
        sessionStateDrawn(open(S2, 5)); // the state read drew it
        daemon.stateDone[0]();
        await settle();

        expect(session.rows).toBe('loading');
        await daemon.answer(2, rows(S2, 5, [row('K2B')]));
        expect(calls()).toEqual(['K2B']);
        expect(session.rows).toBe('ok');
    });
});

// The "already emailed" mark is the contact's own record, read from the daemon
// (ruling 6). Between a send and the read that shows its stamps, the rows on
// screen still say "not emailed": a send from them would mail the same contacts
// again (codex-273). The Session says when that is so.
describe('after an email send, until a read taken after it has answered', () => {
    it('is pending until a read SENT after the send is drawn', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(session.emailPending).toBe(false);

        refreshSessionRows(); // read 2 is out, from before the send
        sessionEmailSent();
        expect(session.emailPending).toBe(true);

        await daemon.answer(2, rows(S1, 4, [row('G4A')])); // taken before the send: proves nothing
        expect(session.emailPending).toBe(true);
        expect(daemon.sent).toHaveLength(3);

        await daemon.answer(3, rows(S1, 4, [row('G4A', { emailed: true })]));
        expect(session.emailPending).toBe(false);
        expect(session.qsos[0].emailed).toBe(true);
    });

    it('stays pending through a failed read, and clears at the next that succeeds', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionEmailSent();
        await daemon.answer(2, { kind: 'error', message: 'Cannot reach the daemon.' });
        expect(session.emailPending).toBe(true);

        refreshSessionRows(); // asked again, as reopening the dialog asks
        await daemon.answer(3, rows(S1, 4, [row('G4A', { emailed: true })]));
        expect(session.emailPending).toBe(false);
    });

    it('is not pending for a session that is no longer shown', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        sessionEmailSent();
        sessionStateDrawn({ archiveId: ARCHIVE, generation: 5, state: 'none', session: null });
        expect(session.emailPending).toBe(false);
    });
});
