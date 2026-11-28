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
    sessionStreamLost,
    sessionMayHaveChanged,
    sessionControlStands,
    _resetSessionForTests,
    type DrawnSessionState,
    type SessionRowLike,
    type SessionRowsReadLike,
} from './session.svelte';
import {
    _reloadArchiveStoresForTests,
    bindArchiveStores,
    setStoresMismatchSink,
} from './archiveStores.svelte';

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
    /** One for each read of the state asked for: call it when that read has
     *  finished, with true when it answered and its state was drawn. */
    readonly stateDone: ((drawn?: boolean) => void)[] = [];
    stateReads = 0;
    bound: string | null = ARCHIVE;

    connect(): void {
        setSessionPorts({
            readRows: () => new Promise<SessionRowsReadLike>((resolve) => this.sent.push(resolve)),
            readState: () => {
                this.stateReads++;
                return new Promise<boolean>((resolve) =>
                    this.stateDone.push((drawn) => resolve(drawn === true))
                );
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
        daemon.stateDone[0](true);
        await settle();

        expect(session.rows).toBe('loading');
        await daemon.answer(2, rows(S2, 5, [row('K2B')]));
        expect(calls()).toEqual(['K2B']);
        expect(session.rows).toBe('ok');
    });
});

// The rows on screen can be OLDER than something the page knows has happened: a
// contact this window just logged, an email it just sent, a change announced
// by an event, anything missed while the stream was down. Until a read TAKEN
// AFTER that has been drawn, the Session is behind, and says so: an export or a
// send from those rows would silently leave out the contact just logged (the
// clean-room review of 13d7c5b6), or mail again the contacts just mailed
// (codex-273). Rows on screen are kept; what is known is that they are not all.
describe('the rows are behind what the page knows', () => {
    it('is behind from a change until a read SENT after it is drawn', async () => {
        sessionStateDrawn(open(S1, 4));
        expect(session.behind).toBe(true); // nothing read yet
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(session.behind).toBe(false);

        refreshSessionRows(); // read 2 is out
        refreshSessionRows(); // K2B is logged while it is out
        expect(session.behind).toBe(true);

        await daemon.answer(2, rows(S1, 4, [row('G4A')])); // taken before K2B: proves nothing
        expect(session.behind).toBe(true);
        expect(calls()).toEqual(['G4A']);

        await daemon.answer(3, rows(S1, 4, [row('K2B'), row('G4A')]));
        expect(session.behind).toBe(false);
    });

    it('stays behind through a failed read, with its rows kept, until one succeeds', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionContactStored(S1);
        await daemon.answer(2, { kind: 'error', message: 'Cannot reach the daemon.' });
        expect(calls()).toEqual(['G4A']);
        expect(session.rows).toBe('ok');
        expect(session.behind).toBe(true); // the rows are there, and are not all

        refreshSessionRows(); // asked again, as opening the Export dialog asks
        await daemon.answer(3, rows(S1, 4, [row('K2B'), row('G4A')]));
        expect(session.behind).toBe(false);
    });

    it('is behind from a reopening of the stream until the rows are read again', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStreamReopened(); // what happened while it was down is not known
        expect(session.behind).toBe(true);
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[0](true);
        await settle();
        expect(session.behind).toBe(false);
    });

    it('is not behind with no session shown, and starts behind for a new one', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        refreshSessionRows();
        sessionStateDrawn({ archiveId: ARCHIVE, generation: 5, state: 'none', session: null });
        expect(session.behind).toBe(false);

        sessionStateDrawn(open(S2, 6));
        expect(session.behind).toBe(true);
        await daemon.answer(3, rows(S2, 6, []));
        expect(session.behind).toBe(false);
    });
});

// Codex's review of the correction (codex-274): three more ways the rows could be
// treated as current when the page already knew they might not be.
describe('behind as soon as the page knows the rows may be another session’s, or missed', () => {
    it('is behind at once when the session may have changed, and a read already out does not clear it', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        refreshSessionRows(); // read 2 is out
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(session.behind).toBe(false);

        refreshSessionRows(); // read 3 is out when the change is announced
        sessionMayHaveChanged(); // operating_session.changed: the state is being read
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('reading');
        expect(daemon.stateReads).toBe(1);

        await daemon.answer(3, rows(S1, 4, [row('G4A')])); // taken before the change: proves nothing
        expect(session.behind).toBe(true);

        sessionStateDrawn(open(S2, 5)); // the state read drew the new session
        daemon.stateDone[0](true);
        await settle();
        expect(session.behind).toBe(true); // its rows are not read yet
        const last = daemon.sent.length;
        await daemon.answer(last, rows(S2, 5, [row('K2B')]));
        expect(session.behind).toBe(false);
        expect(session.hold).toBe('');
        expect(calls()).toEqual(['K2B']);
    });

    it('when the state turns out unchanged, the rows are read again before they are trusted', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionMayHaveChanged();
        sessionStateDrawn(open(S1, 4)); // the same session at the same generation
        daemon.stateDone[0](true);
        await settle();
        expect(session.behind).toBe(true);
        expect(daemon.sent).toHaveLength(2); // a read was sent after the announcement
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(session.behind).toBe(false);
    });

    it('when the state cannot be read, it stays behind and says nothing is being read', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionMayHaveChanged();
        expect(session.hold).toBe('reading');
        daemon.stateDone[0](); // the state read finished and drew nothing
        await settle();

        expect(session.behind).toBe(true);
        expect(session.hold).toBe('failed');
        expect(calls()).toEqual(['G4A']); // the rows stay on screen

        refreshSessionRows(); // the operator asks again: the state is checked again too
        expect(session.hold).toBe('reading');
        expect(daemon.stateReads).toBe(2);
        await daemon.answer(2, rows(S1, 4, [row('G4A')])); // the rows: still this session
        expect(session.behind).toBe(true); // rows alone do not say the workspace may act
        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[1](true); // and the state: still open, for this app
        await settle();
        expect(session.behind).toBe(false);
    });

    it('a stored contact of a session not shown puts the rows behind at once', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        sessionContactStored(S2);
        expect(session.behind).toBe(true);
        expect(daemon.stateReads).toBe(1);
    });

    it('is behind from the loss of the event stream until the read after it reopens', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStreamLost(); // changes can no longer be announced
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('stream');

        refreshSessionRows(); // a read made during the outage proves nothing about what follows it
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('stream');

        sessionStreamReopened();
        expect(session.hold).toBe('reading');
        const last = daemon.sent.length;
        await daemon.answer(last, rows(S1, 4, [row('K2B'), row('G4A')]));
        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[0](true);
        await settle();
        expect(session.behind).toBe(false);
        expect(calls()).toEqual(['K2B', 'G4A']);
    });

    it('says a read is on its way, then that it failed', async () => {
        sessionStateDrawn(open(S1, 4));
        expect(session.hold).toBe('reading');
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(session.hold).toBe('');

        refreshSessionRows();
        expect(session.hold).toBe('reading');
        await daemon.answer(2, { kind: 'error', message: 'Cannot reach the daemon.' });
        expect(session.hold).toBe('failed');
    });

    it('rows that name another archive start the page’s archive check', async () => {
        let checks = 0;
        setStoresMismatchSink(() => checks++);
        try {
            sessionStateDrawn(open(S1, 4));
            await daemon.answer(1, rows(S1, 4, [row('G4A')], OTHER_ARCHIVE));
            expect(checks).toBe(1);
            expect(calls()).toEqual([]);

            // Rows of the page's own archive start nothing.
            refreshSessionRows();
            await daemon.answer(2, rows(S1, 4, [row('G4A')]));
            expect(checks).toBe(1);
        } finally {
            setStoresMismatchSink(() => {});
        }
    });
});

// Codex's third reading of the hold (codex-275).
describe('what a matching list of contacts does not settle', () => {
    // The same session at the same generation can be "open" for one caller and
    // "recovery pending" for another. The rows say who was in the session; only
    // the state says whether this workspace may act on it.
    it('matching rows do not release the hold while the state check is out', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStreamReopened(); // the state and the rows are both read again
        expect(daemon.stateReads).toBe(1);
        await daemon.answer(2, rows(S1, 4, [row('G4A')])); // the rows come first, and match
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('reading');

        // The state: recovery pending. The workspace is replaced by the question.
        sessionStateDrawn({
            archiveId: ARCHIVE,
            generation: 4,
            state: 'recovery_pending',
            session: { id: S1, openedAt: '2026-11-25T09:15:00Z', sittingStartedAt: null },
        });
        daemon.stateDone[0](true);
        await settle();
        expect(calls()).toEqual([]);
        expect(session.behind).toBe(false); // there is nothing here to export
    });

    it('the same after the session is said to have changed', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionMayHaveChanged();
        refreshSessionRows(); // a contact event follows; its rows are sent after the announcement
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        expect(session.behind).toBe(true);

        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[0](true);
        await settle();
        expect(session.behind).toBe(false);
    });

    it('a state check that fails at a reopening is said, and asking again checks the state again', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStreamReopened();
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        daemon.stateDone[0](false);
        await settle();
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('failed');

        refreshSessionRows(); // "Read again"
        expect(daemon.stateReads).toBe(2);
        await daemon.answer(3, rows(S1, 4, [row('G4A')]));
        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[1](true);
        await settle();
        expect(session.behind).toBe(false);
    });
});

describe('a state question left open is asked again when a session comes to be shown', () => {
    // The gate can draw a state the Session did not ask for: the load's own
    // read, "Try again", the answer to the recovery question. If a state check
    // had failed before that, it is not settled by such a draw (it may be of a
    // read sent earlier); the Session asks, so the hold does not wait on the
    // operator finding "Read again".
    it('asks for the state itself, and is released when that answers', async () => {
        sessionStateDrawn({ archiveId: ARCHIVE, generation: 3, state: 'none', session: null });
        sessionStreamReopened();
        daemon.stateDone[0](false); // the check at the opening failed
        await settle();
        expect(daemon.stateReads).toBe(1);

        sessionStateDrawn(open(S1, 4)); // drawn by the gate's own "Try again"
        expect(daemon.stateReads).toBe(2);
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('reading');

        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[1](true);
        await settle();
        expect(session.behind).toBe(false);
        expect(daemon.stateReads).toBe(2); // and it does not go on asking
    });
});

// The clean-room review of 9b8c6260. At a start the event stream can open
// before the archive is proven. The state read made at that opening is then
// refused by the gate (sent before the proof); meanwhile the load's own read
// draws the session. If the refused read is still out at that moment, nothing
// asked for the state again, and when it ended the buttons stayed held as
// "failed" with every read in fact answered.
describe('a state read that is out when a session is drawn, and then settles nothing', () => {
    it('is followed by one more, which releases the hold', async () => {
        _resetSessionForTests({ streamOpen: false });
        daemon = new Daemon();
        daemon.connect();

        sessionStreamReopened(); // the stream opens before the archive is proven
        expect(daemon.stateReads).toBe(1);

        sessionStateDrawn(open(S1, 4)); // the load's own read, after the proof, draws the session
        expect(daemon.stateReads).toBe(1); // the first is still out: nothing more is asked yet
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(session.hold).toBe('reading');

        daemon.stateDone[0](false); // refused: it was sent before the proof
        await settle();
        expect(daemon.stateReads).toBe(2); // asked again, now that it can be answered
        expect(session.hold).toBe('reading');

        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[1](true);
        await settle();
        expect(session.behind).toBe(false);
        expect(session.hold).toBe('');
        expect(daemon.stateReads).toBe(2); // and no more after that
    });

    it('a state check that simply fails is not asked again by itself', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));

        sessionStreamReopened();
        await daemon.answer(2, rows(S1, 4, [row('G4A')]));
        daemon.stateDone[0](false); // the daemon could not be read
        await settle();
        await settle();

        expect(daemon.stateReads).toBe(1); // no retry in a loop against a daemon that is down
        expect(session.hold).toBe('failed');
    });
});

describe('before the event stream has opened for the first time', () => {
    it('rows read by then are shown, and are behind until the stream is open and they are read again', async () => {
        _resetSessionForTests({ streamOpen: false }); // a page that has just loaded
        daemon = new Daemon();
        daemon.connect();

        sessionStateDrawn(open(S1, 4)); // the load's own read of the state
        await daemon.answer(1, rows(S1, 4, [row('G4A')]));
        expect(calls()).toEqual(['G4A']); // the panel is populated
        expect(session.behind).toBe(true); // and nothing can yet announce a change
        expect(session.hold).toBe('stream');

        sessionStreamReopened(); // the first opening
        await daemon.answer(2, rows(S1, 4, [row('K2B'), row('G4A')]));
        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[0](true);
        await settle();
        expect(session.behind).toBe(false);
        expect(calls()).toEqual(['K2B', 'G4A']);
    });
});

// Codex's review (codex-276). Rows that answer for another session ask for the
// state; a state drawn unchanged asks for the rows. If the daemon's two answers
// go on disagreeing, each asks for the other for ever. A stable daemon
// converges at once; one that does not is said to be unreadable, with the
// actions held, and is not polled.
describe('rows and state that go on disagreeing', () => {
    it('are asked once more each, and then the page stops and says so', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 5, [row('K2B')])); // the rows say generation 5
        expect(daemon.stateReads).toBe(1);

        sessionStateDrawn(open(S1, 4)); // the state says 4, again
        daemon.stateDone[0](true);
        await settle();
        expect(daemon.sent).toHaveLength(2); // the rows are read once more

        await daemon.answer(2, rows(S1, 5, [row('K2B')])); // and still say 5
        await settle();
        expect(daemon.stateReads).toBe(1); // the state is not asked again
        expect(daemon.sent).toHaveLength(2); // nor the rows
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('failed');
        expect(session.rows).toBe('failed');
        expect(calls()).toEqual([]);
    });

    it('asking again starts over, and can succeed', async () => {
        sessionStateDrawn(open(S1, 4));
        await daemon.answer(1, rows(S1, 5, []));
        sessionStateDrawn(open(S1, 4));
        daemon.stateDone[0](true);
        await settle();
        await daemon.answer(2, rows(S1, 5, []));
        await settle();
        expect(session.hold).toBe('failed');

        refreshSessionRows(); // "Read again"
        expect(daemon.sent).toHaveLength(3);
        await daemon.answer(3, rows(S1, 5, [row('G4A')])); // the rows still say 5
        expect(daemon.stateReads).toBe(2); // so the state is asked again: it starts over
        expect(session.hold).toBe('reading');

        sessionStateDrawn(open(S1, 5)); // and this time the state has moved on to 5
        daemon.stateDone[1](true);
        await settle();
        const last = daemon.sent.length;
        await daemon.answer(last, rows(S1, 5, [row('G4A')]));
        expect(calls()).toEqual(['G4A']);
        expect(session.behind).toBe(false);
    });
});

// W-0029 part 6, slice 3: a control sent from this window (New, Continue) whose
// outcome the page has not drawn. The rows on screen may then be of a session
// that is no longer the open one, so everything that acts on them is held, as
// it is for any other way of being behind (the operator's rulings 1 and 5,
// 2026-11-27). The recovery gate says where the control stands.
describe('a control whose outcome is not drawn holds what acts on the rows', () => {
    async function shownWithRows(d: Daemon): Promise<void> {
        sessionStateDrawn(open(S1, 4));
        await d.answer(1, rows(S1, 4, [row('K1ABC')]));
        expect(session.behind).toBe(false);
    }

    it('while it is out the Session is behind, as being read', async () => {
        const d = new Daemon();
        d.connect();
        await shownWithRows(d);

        sessionControlStands('out');

        expect(session.control).toBe('out');
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('reading');
        expect(session.qsos.map((q) => q.callsign)).toEqual(['K1ABC']); // the rows stay
    });

    it('when the page could not find out it is behind, as failed, and the rows stay', async () => {
        const d = new Daemon();
        d.connect();
        await shownWithRows(d);

        sessionControlStands('out');
        sessionControlStands('unknown');

        expect(session.control).toBe('unknown');
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('failed');
        expect(session.qsos.map((q) => q.callsign)).toEqual(['K1ABC']);
    });

    it('once the control is settled with the same session shown, nothing is held', async () => {
        const d = new Daemon();
        d.connect();
        await shownWithRows(d);
        sessionControlStands('out');

        sessionControlStands('');

        expect(session.control).toBe('');
        expect(session.behind).toBe(false);
        expect(session.hold).toBe('');
        expect(d.sent).toHaveLength(1); // and nothing was read for it
    });

    it("settled by a state of another session: held until that session's rows are drawn", async () => {
        const d = new Daemon();
        d.connect();
        await shownWithRows(d);
        sessionControlStands('out');

        // New was applied: the gate draws the new session, then says the control is settled.
        sessionStateDrawn(open(S2, 6));
        sessionControlStands('');
        expect(session.qsos).toEqual([]);
        expect(session.behind).toBe(true);
        expect(session.hold).toBe('reading');

        await d.answer(2, rows(S2, 6, []));
        expect(session.behind).toBe(false);
    });

    it('"Read again" while the outcome is not known asks for the state as well as the rows', async () => {
        const d = new Daemon();
        d.connect();
        await shownWithRows(d);
        sessionControlStands('unknown');
        expect(d.stateReads).toBe(0);

        refreshSessionRows();
        await settle();

        expect(d.stateReads).toBe(1);
        expect(d.sent).toHaveLength(2);
        // One read of the state at a time: asking again while it is out sends no second.
        refreshSessionRows();
        await settle();
        expect(d.stateReads).toBe(1);
    });

    it('with no session shown the standing is still stated, and nothing is "behind"', () => {
        const d = new Daemon();
        d.connect();
        sessionStateDrawn({ archiveId: ARCHIVE, generation: 4, state: 'none', session: null });

        sessionControlStands('unknown');

        expect(session.control).toBe('unknown');
        expect(session.behind).toBe(false); // there are no rows to be behind
    });
});
