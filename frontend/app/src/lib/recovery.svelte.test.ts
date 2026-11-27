import { beforeEach, describe, expect, it } from 'vitest';
import {
    recovery,
    recoveryGate,
    readSessionState,
    sessionArchiveProven,
    answerRecovery,
    recoveryContactDeleted,
    recoveryContactStored,
    recoveryStreamLost,
    recoveryStreamOpened,
    setRecoveryPorts,
    _resetRecoveryForTests,
    type SessionStateLike,
    type SessionReadLike,
    type SessionAnswerLike,
} from './recovery.svelte';

// W-0029 part 4, criteria E: what the page shows for each state of the
// operating session, and how it gets from one to the next. The store never
// imports lib/api (ADR 0045): the read, the answer and the bound archive are
// injected, and here the test is the daemon.

const HOME = '01a16a00-0000-7000-8000-000000000001';
const OTHER = '01a16a00-0000-7000-8000-0000000000ff';
const S0 = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const S1 = '01a1f83c-da20-751f-80b7-ec9a3f1f93f4';

const question = (generation: number, id = S0, archiveId = HOME): SessionStateLike => ({
    archiveId,
    generation,
    state: 'recovery_pending',
    session: {
        id,
        openedAt: '2026-11-20T09:15:00Z',
        sittingStartedAt: '2026-11-20T09:15:00Z',
        contactCount: 3,
        heldByYou: false,
    },
});
const held = (generation: number, id = S0, archiveId = HOME): SessionStateLike => ({
    archiveId,
    generation,
    state: 'open',
    session: {
        id,
        openedAt: '2026-11-20T09:15:00Z',
        sittingStartedAt: '2026-11-20T09:15:00Z',
        contactCount: 3,
        heldByYou: true,
    },
});
const noSession = (
    generation: number,
    state: SessionStateLike['state'] = 'none'
): SessionStateLike => ({
    archiveId: HOME,
    generation,
    state,
    session: null,
});

// The daemon: reads answer from a queue (or wait to be released), answers are recorded.
let reads: Array<SessionReadLike | Promise<SessionReadLike>>;
let answers: Array<SessionAnswerLike | Promise<SessionAnswerLike>>;
let pressed: Array<[string, number, string]>;
let bound: string | null;
// Every state the gate handed on as drawn (W-0029 part 6): the Session follows these.
let handed: SessionStateLike[];
// Every archive the gate reported as another than the one proven.
let others: string[];
// How many reads of the state the gate has sent.
let readsSent: number;

const ok = (state: SessionStateLike): SessionReadLike => ({ kind: 'ok', state });
const failed: SessionReadLike = { kind: 'error', message: 'Cannot reach the daemon.' };

// Let a read the gate sent by itself be answered and drawn.
async function flushPromises(): Promise<void> {
    for (let i = 0; i < 10; i++) await Promise.resolve();
}

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => (resolve = r));
    return { promise, resolve };
}

beforeEach(() => {
    _resetRecoveryForTests();
    reads = [];
    answers = [];
    pressed = [];
    bound = HOME;
    handed = [];
    others = [];
    readsSent = 0;
    setRecoveryPorts({
        drawn: (state) => handed.push(state),
        otherArchive: (id) => others.push(id),
        read: () => {
            readsSent++;
            const next = reads.shift();
            if (next === undefined) throw new Error('the test queued no read');
            return Promise.resolve(next);
        },
        answer: (answer, generation, sessionId) => {
            pressed.push([answer, generation, sessionId]);
            const next = answers.shift();
            if (next === undefined) throw new Error('the test queued no answer');
            return Promise.resolve(next);
        },
        archive: () => bound,
    });
});

describe('E5: one rule draws every state', () => {
    it('before any read the page shows nothing of the workspace', () => {
        expect(recovery.status).toBe('loading');
        expect(recoveryGate()).toBe('loading');
    });

    it('recovery pending is the question, for the session and at the generation returned', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        expect(recoveryGate()).toBe('question');
        expect(recovery.session).toEqual({
            id: S0,
            openedAt: '2026-11-20T09:15:00Z',
            contactCount: 3,
        });
        expect(recovery.generation).toBe(4);
    });

    it.each([
        ['open', held(4)],
        ['setup pending', noSession(1, 'setup_pending')],
        ['archive switching', noSession(5, 'archive_switching')],
        ['none', noSession(5)],
    ])('%s shows what the page shows today: no question', async (_name, state) => {
        reads.push(ok(state));
        await readSessionState();
        expect(recoveryGate()).toBe(null);
        expect(recovery.status).toBe('clear');
        expect(recovery.session).toBe(null);
    });
});

describe('E2: the read gate at the load', () => {
    it('a read that fails does not show the workspace, and says why', async () => {
        reads.push(failed);
        await readSessionState();
        expect(recoveryGate()).toBe('unreadable');
        expect(recovery.error).toBe('Cannot reach the daemon.');
    });

    it('a retry that succeeds shows what its state calls for: the question', async () => {
        reads.push(failed, ok(question(4)));
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe('question');
        expect(recovery.error).toBe('');
    });

    it('a retry that succeeds shows what its state calls for: the workspace', async () => {
        reads.push(failed, ok(held(4)));
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe(null);
    });

    it('once the workspace has been shown, a later read that fails does not take it away', async () => {
        reads.push(ok(held(4)), failed);
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe(null);
    });

    it('a later read that succeeds and says recovery is pending is drawn like any other', async () => {
        reads.push(ok(held(4)), ok(question(5, S1)));
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe('question');
        expect(recovery.session?.id).toBe(S1);
    });

    it('a question that is showing is not taken away by a later read that fails', async () => {
        reads.push(ok(question(4)), failed);
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe('question');
    });
});

describe('E4 to E7: the answer', () => {
    beforeEach(async () => {
        reads.push(ok(question(4)));
        await readSessionState();
    });

    it('carries the generation and the session shown, and an applied answer shows the workspace', async () => {
        answers.push({ kind: 'applied', state: held(5) });
        await answerRecovery('continue');
        expect(pressed).toEqual([['continue', 4, S0]]);
        expect(recoveryGate()).toBe(null);
        expect(recovery.answering).toBe(false);
    });

    it('while an answer is in flight a second press sends nothing', async () => {
        const d = deferred<SessionAnswerLike>();
        answers.push(d.promise);
        const first = answerRecovery('new');
        expect(recovery.answering).toBe(true);
        await answerRecovery('continue');
        await answerRecovery('new');
        expect(pressed).toEqual([['new', 4, S0]]);
        d.resolve({ kind: 'applied', state: held(5, S1) });
        await first;
        expect(recovery.answering).toBe(false);
        expect(recoveryGate()).toBe(null);
    });

    it('stale, the first attempt had been applied: the workspace, and nothing is sent again', async () => {
        answers.push({ kind: 'stale', state: held(5) });
        await answerRecovery('continue');
        expect(recoveryGate()).toBe(null);
        expect(pressed).toHaveLength(1);
    });

    it('stale, a question still stands, for another session: the question at the new generation', async () => {
        answers.push({ kind: 'stale', state: question(6, S1) });
        await answerRecovery('continue');
        expect(recoveryGate()).toBe('question');
        expect(recovery.session?.id).toBe(S1);
        expect(recovery.generation).toBe(6);
        expect(pressed).toHaveLength(1);
        // The next press is the operator's, on the state now shown.
        answers.push({ kind: 'applied', state: held(7, S1) });
        await answerRecovery('continue');
        expect(pressed[1]).toEqual(['continue', 6, S1]);
    });

    it('stale, the default-epoch test closed the session (B7): no question, the workspace', async () => {
        answers.push({ kind: 'stale', state: noSession(5) });
        await answerRecovery('new');
        expect(recoveryGate()).toBe(null);
        expect(pressed).toHaveLength(1);
    });

    it('a failure: the page reads again, and with the question still standing says what went wrong', async () => {
        answers.push({
            kind: 'failed',
            message: 'the session could not be changed; nothing was changed',
        });
        reads.push(ok(question(4)));
        await answerRecovery('new');
        expect(recoveryGate()).toBe('question');
        expect(recovery.error).toBe('the session could not be changed; nothing was changed');
        expect(pressed).toHaveLength(1);
        // Pressed again, it carries the generation shown: the first press's, nothing having changed.
        answers.push({ kind: 'applied', state: held(5, S1) });
        await answerRecovery('new');
        expect(pressed[1]).toEqual(['new', 4, S0]);
        expect(recoveryGate()).toBe(null);
    });

    it('a failure whose answer had in fact been applied: the read finds the workspace', async () => {
        answers.push({ kind: 'failed', message: 'Cannot reach the daemon.' });
        reads.push(ok(held(5)));
        await answerRecovery('continue');
        expect(recoveryGate()).toBe(null);
    });

    it('a failure, and the read after it fails too: the session could not be read', async () => {
        answers.push({ kind: 'failed', message: 'Cannot reach the daemon.' });
        reads.push(failed);
        await answerRecovery('continue');
        expect(recoveryGate()).toBe('unreadable');
    });
});

describe('E8: late results and other archives', () => {
    it('a read sent before an answer and arriving after it cannot put the old question back', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        const late = deferred<SessionReadLike>();
        reads.push(late.promise);
        const lateRead = readSessionState(); // sent now, answered later
        answers.push({ kind: 'applied', state: held(5) });
        await answerRecovery('continue');
        expect(recoveryGate()).toBe(null);
        late.resolve(ok(question(4)));
        await lateRead;
        expect(recoveryGate()).toBe(null);
    });

    it('a late read that failed does not take away what a later read drew', async () => {
        const late = deferred<SessionReadLike>();
        reads.push(late.promise, ok(question(4)));
        const lateRead = readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe('question');
        late.resolve(failed);
        await lateRead;
        expect(recoveryGate()).toBe('question');
    });

    it('a state for another archive is not drawn', async () => {
        reads.push(ok(held(4)), ok(question(9, S1, OTHER)));
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe(null);
    });

    it('generations are compared within one archive: after a rebind the new archive draws from its own', async () => {
        reads.push(ok(held(40)));
        await readSessionState();
        bound = OTHER;
        reads.push(ok(question(2, S1, OTHER)));
        await readSessionState();
        expect(recoveryGate()).toBe('question');
        expect(recovery.generation).toBe(2);
    });
});

// codex-226, finding 1. A state read before the boot has proven its archive is
// not a state for the archive the page will be bound to: it must not open the
// workspace, or ask a question, until the archive it belongs to is the one
// proven. A background read afterwards is not enough: the workspace is withheld
// while the matching state is unknown.
describe('the gate opens only for the archive the boot has proven', () => {
    it('a state read before any archive is proven is drawn by nothing: no workspace, no question', async () => {
        bound = null;
        reads.push(ok(held(4)));
        await readSessionState();
        expect(recoveryGate()).toBe('loading');
        reads.push(ok(question(4)));
        await readSessionState();
        expect(recoveryGate()).toBe('loading');
    });

    // The clean-room review of 2c362c3d: two reads overlap before the proof, the
    // older state arriving last. Whatever was read before the proof is never
    // what is drawn at it: the state is read then, so an obsolete question
    // cannot be drawn and left standing.
    it('at the proof the state is read afresh: an older answer that arrived last before it is not drawn', async () => {
        bound = null;
        reads.push(ok(held(5)), ok(question(4))); // the newer state first, the older one last
        await readSessionState();
        await readSessionState();
        expect(recoveryGate()).toBe('loading');
        bound = HOME;
        reads.push(ok(held(5))); // what the daemon holds now
        await sessionArchiveProven();
        expect(recoveryGate()).toBe(null);
        expect(reads).toHaveLength(0); // the proof's own read was made
    });

    // codex-228: the order the test below this one did not cover. A read sent
    // before the proof arrives after the proof but BEFORE the proof's own read
    // has answered. It was sent with nothing proven: it draws nothing, whatever
    // archive is bound by the time it returns, and it does not count as
    // progress that would make the fresh read's failure obsolete.
    it.each([
        ['fails', failed, 'unreadable'],
        ['succeeds', ok(held(5)), null],
    ] as const)(
        'a read sent before the proof, arriving after it and before the fresh read, draws nothing; the fresh read then %s',
        async (_name, fresh, gate) => {
            bound = null;
            const early = deferred<SessionReadLike>();
            reads.push(early.promise);
            const earlyRead = readSessionState(); // the stream's read, sent with nothing proven
            bound = HOME;
            const load = deferred<SessionReadLike>();
            reads.push(load.promise);
            const proven = sessionArchiveProven(); // the load's own read, sent now
            expect(recoveryGate()).toBe('loading');

            early.resolve(ok(question(4))); // the old question, for the archive now proven
            await earlyRead;
            expect(recoveryGate()).toBe('loading');
            expect(recovery.session).toBe(null);

            load.resolve(fresh);
            await proven;
            expect(recoveryGate()).toBe(gate);
        }
    );

    it('a read sent before the proof that fails after it does not hide what the fresh read then draws', async () => {
        bound = null;
        const early = deferred<SessionReadLike>();
        reads.push(early.promise);
        const earlyRead = readSessionState();
        bound = HOME;
        reads.push(ok(question(4)));
        await sessionArchiveProven();
        early.resolve(failed);
        await earlyRead;
        expect(recoveryGate()).toBe('question');
    });

    it('a read from before the proof that arrives after it cannot put an older question back', async () => {
        bound = null;
        const early = deferred<SessionReadLike>();
        reads.push(early.promise);
        const earlyRead = readSessionState();
        bound = HOME;
        reads.push(ok(held(5)));
        await sessionArchiveProven();
        expect(recoveryGate()).toBe(null);
        early.resolve(ok(question(4)));
        await earlyRead;
        expect(recoveryGate()).toBe(null);
    });

    it('another archive is proven: the workspace is withheld until ITS state is read, and then it is the question', async () => {
        bound = null;
        reads.push(ok(held(4, S0, HOME))); // the daemon served Home when the load's read was answered
        await readSessionState();
        bound = OTHER; // the boot then proved another archive
        const later = deferred<SessionReadLike>();
        reads.push(later.promise);
        const proven = sessionArchiveProven();
        expect(recoveryGate()).toBe('loading'); // not the workspace, while that read is out
        later.resolve(ok(question(2, S1, OTHER)));
        await proven;
        expect(recoveryGate()).toBe('question');
        expect(recovery.session?.id).toBe(S1);
    });

    it('another archive is proven and its state cannot be read: the session could not be read', async () => {
        bound = null;
        reads.push(ok(held(4, S0, HOME)));
        await readSessionState();
        bound = OTHER;
        reads.push(failed);
        await sessionArchiveProven();
        expect(recoveryGate()).toBe('unreadable');
    });

    it('nothing was read before the proof: the state is read then', async () => {
        reads.push(ok(question(4)));
        await sessionArchiveProven();
        expect(recoveryGate()).toBe('question');
    });
});

// codex-226, finding 2. A read that fails may make the page unreadable only
// while the uncertainty it was sent to resolve is still current. Anything
// authoritative drawn since, a later read or a later answer, supersedes it.
describe('an obsolete failure does not take away what was drawn after it was sent', () => {
    beforeEach(async () => {
        reads.push(ok(question(4)));
        await readSessionState();
    });

    it('while the page is finding out what a failed answer did, no answer can be pressed', async () => {
        answers.push({ kind: 'failed', message: 'Cannot reach the daemon.' });
        const reconciling = deferred<SessionReadLike>();
        reads.push(reconciling.promise);
        const first = answerRecovery('continue');
        await Promise.resolve();
        await Promise.resolve();
        expect(recovery.answering).toBe(true);
        await answerRecovery('new');
        expect(pressed).toHaveLength(1);
        reconciling.resolve(ok(question(4)));
        await first;
        expect(recovery.answering).toBe(false);
        expect(recoveryGate()).toBe('question');
    });

    it('the read after a failed answer fails late, after a later read drew the workspace: the workspace stays', async () => {
        answers.push({ kind: 'failed', message: 'Cannot reach the daemon.' });
        const reconciling = deferred<SessionReadLike>();
        reads.push(reconciling.promise, ok(held(5)));
        const first = answerRecovery('continue');
        await Promise.resolve();
        await Promise.resolve();
        await readSessionState(); // an event's read: the answer had been applied
        expect(recoveryGate()).toBe(null);
        reconciling.resolve(failed);
        await first;
        expect(recoveryGate()).toBe(null);
        expect(recovery.status).toBe('clear');
    });
});

// The Session the workspace shows follows the gate's one rule (W-0029 part 6,
// B2): it is told each state the gate draws, and no state the gate refuses. So
// the panel can never be of another archive's session, of one read before the
// archive was proven, or of a state older than the one on screen.
describe('each state drawn is handed on, and no state refused is', () => {
    it('hands on a read, an applied answer and a stale refusal, in the order drawn', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        answers.push({ kind: 'applied', state: held(5) });
        await answerRecovery('continue');
        reads.push(ok(noSession(6)));
        await readSessionState();

        expect(handed.map((s) => [s.state, s.generation])).toEqual([
            ['recovery_pending', 4],
            ['open', 5],
            ['none', 6],
        ]);
        expect(handed[1].session?.sittingStartedAt).toBe('2026-11-20T09:15:00Z');
    });

    it('hands on nothing it does not draw', async () => {
        reads.push(ok(held(5)));
        await readSessionState();
        handed = [];

        reads.push(ok(held(4))); // older than the one shown
        await readSessionState();
        reads.push(ok(held(9, S1, OTHER))); // another archive's
        await readSessionState();
        reads.push(failed);
        await readSessionState();
        bound = null; // no archive proven when the read is sent
        reads.push(ok(held(7)));
        await readSessionState();

        expect(handed).toEqual([]);
    });
});

// Codex's review (codex-275). The Session holds export and email until a state
// read SENT AFTER the event has been drawn; so the read says whether it was. And
// a state answered for another archive than the one proven is positive word
// that the daemon now serves another archive: it starts the page's archive check,
// which nothing else may do when the event stream first opens on that daemon.
describe('what a read of the state reports', () => {
    it('says whether its answer was drawn', async () => {
        reads.push(ok(held(5)));
        expect(await readSessionState()).toBe(true);

        reads.push(failed);
        expect(await readSessionState()).toBe(false);

        reads.push(ok(held(4))); // older than the one shown: not drawn
        expect(await readSessionState()).toBe(false);

        reads.push(ok(held(9, S1, OTHER))); // another archive's: not drawn
        expect(await readSessionState()).toBe(false);
    });

    it('reports another archive, when its own was proven as the read was sent', async () => {
        reads.push(ok(held(5, S0, OTHER)));
        await readSessionState();
        expect(others).toEqual([OTHER]);

        reads.push(ok(held(6)));
        await readSessionState();
        expect(others).toEqual([OTHER]); // its own archive reports nothing
    });

    it('reports nothing for a read sent before any archive was proven', async () => {
        bound = null;
        reads.push(ok(held(5, S0, OTHER)));
        await readSessionState();
        expect(others).toEqual([]);
        expect(handed).toEqual([]);
    });
});

// Codex's review (codex-276). The gate refused an OLDER generation and accepted
// either of two answers at the SAME one. But a session's state changes without
// its generation changing: a contact joins and the count goes up; and for one
// session at one generation the daemon says "open" or "recovery pending"
// depending on which app is admitted when it is asked. So of two reads that
// overlap, the answer of the one sent LATER is kept, whichever arrives first.
// It is not known to be the newer (the reviews of ff5e855c and 5f48eaa0): the
// daemon may have served the two in either order. So after any such overlap
// the page reads once more, with no read out, and ends on that answer.
describe('two answers at the same generation are ordered by when they were asked', () => {
    const counted = (count: number): SessionStateLike => ({
        archiveId: HOME,
        generation: 4,
        state: 'recovery_pending',
        session: {
            id: S0,
            openedAt: '2026-11-20T09:15:00Z',
            sittingStartedAt: '2026-11-20T09:15:00Z',
            contactCount: count,
            heldByYou: false,
        },
    });

    it('a count read later is not put back by an earlier read answered last', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();

        const older = deferred<SessionReadLike>();
        reads.push(older.promise); // asked after the second contact
        reads.push(ok(counted(3))); // asked after the third
        const first = readSessionState();
        await readSessionState();
        expect(recovery.session?.contactCount).toBe(3);

        // The earlier read answers last. It is not drawn; and because the page
        // cannot know which of the two the daemon answered first, it asks once
        // more. That read was sent after both, so its answer is the truth: here,
        // that there are still three.
        reads.push(ok(counted(3)));
        older.resolve(ok(counted(2)));
        expect(await first).toBe(false); // not drawn
        await flushPromises();
        expect(reads).toHaveLength(0); // the reconciling read was sent
        expect(recovery.session?.contactCount).toBe(3);
    });

    // The clean-room review of ff5e855c: the order two reads were SENT in is
    // not the order the daemon answered them in. The read sent first can be
    // the one the daemon served last, and so the newer in fact.
    it('when the earlier-sent read was in fact the later one, the page ends on what the daemon holds', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();

        const sentFirst = deferred<SessionReadLike>(); // waits at the daemon
        reads.push(sentFirst.promise);
        reads.push(ok(counted(3))); // sent second, served first: three contacts
        const first = readSessionState();
        await readSessionState();
        expect(recovery.session?.contactCount).toBe(3);

        // A contact is deleted; no generation moves. Then the first read is
        // served: two. By its number it looks older, and it is not drawn...
        reads.push(ok(counted(2))); // ...but the page asks again, and is told: two.
        sentFirst.resolve(ok(counted(2)));
        await first;
        await flushPromises();

        expect(reads).toHaveLength(0);
        expect(recovery.session?.contactCount).toBe(2);
    });

    // The clean-room review of 5f48eaa0: a read that is already out when an
    // answer is set aside was SENT after the state shown, but the daemon may
    // have served it before the answer set aside. It settles nothing. Only a
    // read sent after every answer has arrived is known to be the newest.
    it('a later read already on its way does not settle it: the page reads once more when it lands', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();

        const sentFirst = deferred<SessionReadLike>();
        const sentThird = deferred<SessionReadLike>();
        reads.push(sentFirst.promise);
        reads.push(ok(counted(3)));
        reads.push(sentThird.promise);
        const first = readSessionState(); // stalls at the daemon
        await readSessionState(); // the second: three contacts, drawn
        const third = readSessionState(); // served at once: three; its answer is slow

        expect(readsSent).toBe(4);
        // A contact is deleted. Then the first read is served: two.
        sentFirst.resolve(ok(counted(2))); // set aside by its number
        await first;
        await flushPromises();
        expect(readsSent).toBe(4); // nothing is sent while a read is still out

        reads.push(ok(counted(2))); // the read sent once all have landed
        sentThird.resolve(ok(counted(3))); // drawn: it was sent after the one shown
        await third;
        await flushPromises();

        expect(readsSent).toBe(5);
        expect(recovery.session?.contactCount).toBe(2); // what the daemon holds
        await flushPromises();
        expect(readsSent).toBe(5); // and that read, alone when out, is the last
    });

    // The same doubt the other way round: the later-sent read is DRAWN over
    // the earlier one, by its number, though the daemon served it first.
    it('a later-sent read drawn over one it overlapped may be the older: the page reads once more', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();

        const sentFirst = deferred<SessionReadLike>();
        const sentSecond = deferred<SessionReadLike>();
        reads.push(sentFirst.promise);
        reads.push(sentSecond.promise);
        const first = readSessionState();
        const second = readSessionState(); // served first: three contacts
        const sent = readsSent;

        // A contact is deleted, then the FIRST is served (two) and arrives first.
        sentFirst.resolve(ok(counted(2)));
        expect(await first).toBe(true);
        await flushPromises();
        expect(readsSent).toBe(sent); // the second is still out

        reads.push(ok(counted(2)));
        sentSecond.resolve(ok(counted(3))); // drawn by its number: the older in fact
        expect(await second).toBe(true);
        await flushPromises();

        expect(readsSent).toBe(sent + 1);
        expect(recovery.session?.contactCount).toBe(2);
    });

    it('two reads that did not overlap are in order: nothing more is read', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();
        reads.push(ok(counted(2)));
        await readSessionState();
        reads.push(ok(counted(3)));
        await readSessionState();
        await flushPromises();
        expect(readsSent).toBe(3);
        expect(recovery.session?.contactCount).toBe(3);
    });

    // The read the page sends for itself is a read like any other. If another
    // overlaps it, one more follows the pair, and then it stops: the reads
    // never chase one another.
    it('the extra read is itself overlapped by another: one more after both, and then no more', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        b.resolve(ok(counted(3)));
        await second;
        const extra = deferred<SessionReadLike>();
        reads.push(extra.promise);
        a.resolve(ok(counted(3))); // set aside; all have landed: the extra read goes out
        await first;
        await flushPromises();
        expect(readsSent).toBe(3);

        const event = deferred<SessionReadLike>();
        reads.push(event.promise);
        const fromEvent = readSessionState(); // something happened while it was out
        expect(readsSent).toBe(4);
        event.resolve(ok(counted(3)));
        await fromEvent;
        await flushPromises();
        expect(readsSent).toBe(4); // the extra read is still out: nothing yet

        reads.push(ok(counted(3)));
        extra.resolve(ok(counted(3))); // set aside: the event's read was drawn while it was out
        await flushPromises();
        expect(readsSent).toBe(5); // one more, sent with nothing out
        await flushPromises();
        expect(readsSent).toBe(5); // and no more
        expect(reads).toHaveLength(0);
    });

    it('a stale refusal set aside for its number is followed by a read, like a read is', async () => {
        reads.push(ok(question(4)));
        await readSessionState();

        const refusal = deferred<SessionAnswerLike>();
        answers.push(refusal.promise);
        const pressedNow = answerRecovery('continue'); // sent first, answered last
        reads.push(ok(question(4)));
        await readSessionState(); // sent second: still the question, drawn
        const sent = readsSent;

        // The refusal carries the daemon's state as it was served: open. By its
        // number it is not drawn; which of the two was served last is not known.
        reads.push(ok(held(4)));
        refusal.resolve({ kind: 'stale', state: held(4) });
        await pressedNow;
        await flushPromises();

        expect(readsSent).toBe(sent + 1);
        expect(recovery.status).toBe('clear'); // what the daemon holds
        expect(pressed).toHaveLength(1); // the answer itself is never sent again
    });

    it('the question is not dismissed by an earlier read that said open', async () => {
        const older = deferred<SessionReadLike>();
        reads.push(older.promise); // asked first, before this app was admitted
        reads.push(ok(question(4))); // asked second
        const first = readSessionState();
        await readSessionState();
        expect(recovery.status).toBe('question');

        reads.push(ok(question(4))); // the reconciling read: still the question
        older.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(recovery.status).toBe('question');
        // Never the workspace, even for a moment: "open" was handed on to nobody.
        expect(handed.map((s) => s.state)).toEqual(['recovery_pending', 'recovery_pending']);
    });

    it('an answer of an OLDER generation is simply not drawn: nothing is asked again', async () => {
        const sentFirst = deferred<SessionReadLike>();
        reads.push(sentFirst.promise);
        reads.push(ok(held(5)));
        const first = readSessionState();
        await readSessionState(); // sent second: generation 5, drawn
        const sent = readsSent;

        // Sent first, answered last, and a generation behind: plainly older,
        // whichever the daemon served first. There is nothing to reconcile.
        sentFirst.resolve(ok(question(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(readsSent).toBe(sent);
        expect(recovery.status).toBe('clear');
    });

    it('a higher generation is drawn whenever it was asked for', async () => {
        const older = deferred<SessionReadLike>();
        reads.push(older.promise);
        reads.push(ok(question(4)));
        const first = readSessionState();
        await readSessionState();

        older.resolve(ok(held(5))); // asked earlier, and of a later generation
        expect(await first).toBe(true);
        expect(recovery.status).toBe('clear');
        expect(recovery.generation).toBe(5);
    });

    it('the state an answer returns is not put back by a read asked before the answer', async () => {
        reads.push(ok(question(4)));
        await readSessionState();

        const older = deferred<SessionReadLike>();
        reads.push(older.promise);
        const early = readSessionState(); // asked before the operator pressed
        answers.push({ kind: 'stale', state: held(4) }); // the refusal carries the current state
        await answerRecovery('continue');
        expect(recovery.status).toBe('clear');

        reads.push(ok(held(4))); // the reconciling read: open, as the answer said
        older.resolve(ok(question(4)));
        await early;
        expect(recovery.status).toBe('clear'); // the question is not put back, even for a moment
        await flushPromises();
        expect(recovery.status).toBe('clear');
    });
});

// Codex's review (codex-277). Two answers of one generation whose order is not
// known can DISAGREE on whether the workspace may be shown: the same session
// reads "recovery pending" or "open" by which app holds it when the daemon is
// asked. The page then shows neither the question nor the workspace until a
// read sent after both has answered; and if that read fails, it says the
// session could not be read. While it waits, the workspace cannot act.
describe('answers in doubt that disagree on whether the workspace may show', () => {
    const states = (): string[] => handed.map((s) => s.state);

    it('pending, then open from a read that overlapped it: neither is shown until a read decides', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        a.resolve(ok(question(4)));
        expect(await first).toBe(true);
        expect(recovery.status).toBe('question');

        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        b.resolve(ok(held(4))); // sent later; which the daemon served last is not known
        expect(await second).toBe(false); // not drawn
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // not the workspace, and not the question
        expect(recovery.session).toBeNull();
        expect(states()).toEqual(['recovery_pending']); // "open" is handed on to nobody
        expect(readsSent).toBe(3);

        deciding.resolve(ok(question(4)));
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(states()).toEqual(['recovery_pending', 'recovery_pending']);
        expect(readsSent).toBe(3);
    });

    it('the deciding read fails: the session could not be read, and "Try again" reads it', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        a.resolve(ok(question(4)));
        await first;
        reads.push(failed); // the deciding read
        b.resolve(ok(held(4)));
        await second;
        await flushPromises();

        expect(recovery.status).toBe('unreadable');
        expect(recovery.error).toBe('Cannot reach the daemon.');
        expect(states()).toEqual(['recovery_pending']); // the workspace was never opened
        expect(readsSent).toBe(3); // a read that fails is not repeated

        reads.push(ok(held(4))); // "Try again"
        expect(await readSessionState()).toBe(true);
        expect(recovery.status).toBe('clear');
    });

    it('open, then pending from a read that overlapped it: the workspace is withheld, and no question is asked yet', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        a.resolve(ok(held(4)));
        await first;
        expect(recovery.status).toBe('clear');

        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        b.resolve(ok(question(4)));
        expect(await second).toBe(false);
        await flushPromises();
        expect(recoveryGate()).toBe('loading');
        expect(states()).toEqual(['open']);

        deciding.resolve(ok(held(4))); // the daemon: open
        await flushPromises();
        expect(recovery.status).toBe('clear');
    });

    it('the earlier-sent answer, set aside, said open while the question shows: the same', async () => {
        const older = deferred<SessionReadLike>();
        reads.push(older.promise);
        reads.push(ok(question(4)));
        const first = readSessionState();
        await readSessionState();
        expect(recovery.status).toBe('question');

        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        older.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(recoveryGate()).toBe('loading');

        deciding.resolve(ok(held(4))); // it was the newer in fact
        await flushPromises();
        expect(recovery.status).toBe('clear');
        expect(states()).toEqual(['recovery_pending', 'open']);
    });

    it('answers in doubt that AGREE are shown as they come: nothing is withheld', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        a.resolve(ok(held(4)));
        await first;
        reads.push(ok(held(4)));
        b.resolve(ok(held(4)));
        expect(await second).toBe(true);
        expect(recovery.status).toBe('clear'); // at once, with the read after it still to come
        await flushPromises();
        expect(recovery.status).toBe('clear');
        expect(readsSent).toBe(3);
    });

    it('once withheld, a further answer in doubt shows nothing, whatever it says', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        const c = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise, c.promise);
        const first = readSessionState();
        const second = readSessionState();
        const third = readSessionState();
        a.resolve(ok(question(4)));
        await first;
        b.resolve(ok(held(4)));
        await second;
        expect(recoveryGate()).toBe('loading');

        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        c.resolve(ok(held(4))); // agrees with the last one, and is in doubt as it was
        expect(await third).toBe(false);
        await flushPromises();
        expect(recoveryGate()).toBe('loading');
        expect(states()).toEqual(['recovery_pending']);
        expect(readsSent).toBe(4); // the deciding read went out when the last one landed

        deciding.resolve(ok(held(4)));
        await flushPromises();
        expect(recovery.status).toBe('clear');
    });

    // The deciding read is sent when no READ is out. An answer to the question
    // can still be out, and land while the deciding read is: then that read is
    // in doubt too, though the answer was set aside and drew nothing.
    it('an answer set aside while the deciding read is out leaves that read in doubt: one more decides', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        const refusal = deferred<SessionAnswerLike>();
        answers.push(refusal.promise);
        const pressedNow = answerRecovery('continue'); // out for the whole of what follows

        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        a.resolve(ok(question(4)));
        await first;
        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        b.resolve(ok(held(4)));
        await second;
        await flushPromises();
        expect(recoveryGate()).toBe('loading');
        const sent = readsSent;

        // The refusal lands while the deciding read is out, and is set aside
        // by its number. Which of the two the daemon served last is not known.
        refusal.resolve({ kind: 'stale', state: held(4) });
        await pressedNow;
        const second2 = deferred<SessionReadLike>();
        reads.push(second2.promise);
        deciding.resolve(ok(question(4)));
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // still not shown
        expect(readsSent).toBe(sent + 1);

        second2.resolve(ok(held(4)));
        await flushPromises();
        expect(recovery.status).toBe('clear');
    });
});

// Codex's review (codex-277). The question says how many contacts the session
// has. A contact deleted while it shows changes that count and nothing else
// reads the state again: no session is shown, so the rows are not read.
describe('a contact deleted while the question shows', () => {
    const counted = (count: number): SessionStateLike => ({
        archiveId: HOME,
        generation: 4,
        state: 'recovery_pending',
        session: {
            id: S0,
            openedAt: '2026-11-20T09:15:00Z',
            sittingStartedAt: '2026-11-20T09:15:00Z',
            contactCount: count,
            heldByYou: false,
        },
    });

    it('reads the state, and the question shows the count the daemon holds', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();
        expect(recovery.session?.contactCount).toBe(1);

        reads.push(ok(counted(0)));
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(2);
        expect(recovery.status).toBe('question');
        expect(recovery.session?.contactCount).toBe(0);
    });

    it('asks nothing while the workspace shows: the Session reads its own rows', async () => {
        reads.push(ok(held(4)));
        await readSessionState();
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(1);
    });

    it('asks nothing before any state has been read', async () => {
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(0);
    });

    // Codex's review (codex-279). A deletion can arrive while the question is
    // NOT showing and a read of the state is out: that read's answer may have
    // been taken before the deletion. It is remembered, and if the question is
    // showing when the reads have landed, the state is read once more.
    it('arriving while the deciding read is out, with nothing shown: remembered, and one read follows the question', async () => {
        const a = deferred<SessionReadLike>();
        const b = deferred<SessionReadLike>();
        reads.push(a.promise, b.promise);
        const first = readSessionState();
        const second = readSessionState();
        a.resolve(ok(counted(1)));
        await first;
        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        b.resolve(ok(held(4)));
        await second;
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // in doubt: nothing shown
        expect(readsSent).toBe(3);

        // The deciding read was served with one contact. Then the contact is deleted.
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(3); // nothing is asked while that read is out

        reads.push(ok(counted(0)));
        deciding.resolve(ok(counted(1)));
        await flushPromises();
        expect(readsSent).toBe(4); // one read, sent after the deletion
        expect(recovery.status).toBe('question');
        expect(recovery.session?.contactCount).toBe(0);
        await flushPromises();
        expect(readsSent).toBe(4); // and no more
    });

    it("arriving while the load's read is out: the same", async () => {
        const load = deferred<SessionReadLike>();
        reads.push(load.promise);
        const first = readSessionState();
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(1);

        reads.push(ok(counted(0)));
        load.resolve(ok(counted(1)));
        await first;
        await flushPromises();
        expect(readsSent).toBe(2);
        expect(recovery.session?.contactCount).toBe(0);
    });

    it('arriving with the workspace showing and a read out that brings the question: the same', async () => {
        reads.push(ok(held(4)));
        await readSessionState();
        const out = deferred<SessionReadLike>();
        reads.push(out.promise);
        const second = readSessionState(); // the session was said to have changed
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(2);

        reads.push(ok({ ...counted(0), generation: 5 }));
        out.resolve(ok({ ...counted(1), generation: 5 })); // recovery is pending now
        await second;
        await flushPromises();
        expect(readsSent).toBe(3);
        expect(recovery.status).toBe('question');
        expect(recovery.session?.contactCount).toBe(0);
    });

    it('remembered, but the read that was out brings the workspace: nothing more is read', async () => {
        const load = deferred<SessionReadLike>();
        reads.push(load.promise);
        const first = readSessionState();
        recoveryContactDeleted();
        load.resolve(ok(held(4)));
        await first;
        await flushPromises();
        expect(readsSent).toBe(1);
        expect(recovery.status).toBe('clear');

        // And it is not kept for later: a question read after this is current.
        reads.push(ok({ ...counted(2), generation: 5 }));
        await readSessionState();
        await flushPromises();
        expect(readsSent).toBe(2);
    });

    it('remembered, and the read that was out fails: nothing is asked again, and "Try again" is enough', async () => {
        const load = deferred<SessionReadLike>();
        reads.push(load.promise);
        const first = readSessionState();
        recoveryContactDeleted();
        load.resolve(failed);
        await first;
        await flushPromises();
        expect(readsSent).toBe(1); // a daemon that cannot be read is not asked again
        expect(recovery.status).toBe('unreadable');

        reads.push(ok(counted(0))); // "Try again": sent after the deletion
        await readSessionState();
        await flushPromises();
        expect(readsSent).toBe(2);
        expect(recovery.session?.contactCount).toBe(0);
    });

    it('arriving with no read out and no question showing: nothing to remember', async () => {
        reads.push(failed);
        await readSessionState();
        expect(recovery.status).toBe('unreadable');
        recoveryContactDeleted();

        reads.push(ok(counted(0))); // "Try again" is sent after the deletion
        await readSessionState();
        await flushPromises();
        expect(readsSent).toBe(2);
    });
});

// Codex's review (codex-280). The rule behind the deletion cases, stated once:
// the question's count is good only if no contact joined a session or was
// deleted, that the page has heard of, since the REQUEST that supplied it was
// sent. That request may be a read or an answer to the question; the change may
// be a deletion or a contact stored. When the question is drawn from an older
// request, one read follows.
describe("the question's count, and contacts stored or deleted since its request was sent", () => {
    const counted = (count: number, generation = 4): SessionStateLike => ({
        archiveId: HOME,
        generation,
        state: 'recovery_pending',
        session: {
            id: S0,
            openedAt: '2026-11-20T09:15:00Z',
            sittingStartedAt: '2026-11-20T09:15:00Z',
            contactCount: count,
            heldByYou: false,
        },
    });

    it('a contact joins the session shown while a read is out that brings the question: one read follows', async () => {
        reads.push(ok(held(4)));
        await readSessionState();
        expect(recovery.status).toBe('clear');
        const out = deferred<SessionReadLike>();
        reads.push(out.promise);
        const second = readSessionState(); // served: recovery pending, one contact

        recoveryContactStored(); // a second contact joins; no generation moves
        await flushPromises();
        expect(readsSent).toBe(2); // the gate asks nothing for it by itself

        reads.push(ok(counted(2)));
        out.resolve(ok(counted(1)));
        await second;
        await flushPromises();
        expect(readsSent).toBe(3);
        expect(recovery.status).toBe('question');
        expect(recovery.session?.contactCount).toBe(2);
        await flushPromises();
        expect(readsSent).toBe(3);
    });

    it('the question comes back from the refusal of an answer sent before a deletion: one read follows, and the answer is sent once', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();
        const refusal = deferred<SessionAnswerLike>();
        answers.push(refusal.promise);
        const pressedNow = answerRecovery('continue'); // at generation 4; held at the daemon

        reads.push(ok(held(5))); // another start took the session on: the workspace
        await readSessionState();
        expect(recovery.status).toBe('clear');
        reads.push(failed); // the read for a later change fails: the workspace stays
        await readSessionState();
        expect(recovery.status).toBe('clear');

        // With only the answer out, a contact is deleted. Nothing shows a count.
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(3);

        // The refusal was taken before the deletion: the question, one contact.
        reads.push(ok(counted(0, 6)));
        refusal.resolve({ kind: 'stale', state: counted(1, 6) });
        await pressedNow;
        await flushPromises();
        expect(readsSent).toBe(4);
        expect(recovery.status).toBe('question');
        expect(recovery.session?.contactCount).toBe(0);
        expect(pressed).toHaveLength(1);
        await flushPromises();
        expect(readsSent).toBe(4);
    });

    it('a contact stored with the question showing: the gate asks nothing itself, and a read sent after it is current', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();
        recoveryContactStored(); // the Session reads the state for a session it does not show
        await flushPromises();
        expect(readsSent).toBe(1);

        reads.push(ok(counted(2)));
        await readSessionState(); // that read: sent after the contact was heard of
        await flushPromises();
        expect(recovery.session?.contactCount).toBe(2);
        expect(readsSent).toBe(2); // nothing follows it
    });

    it('a deletion with the question showing and a read out: one read once it has landed, not two', async () => {
        reads.push(ok(counted(2)));
        await readSessionState();
        const out = deferred<SessionReadLike>();
        reads.push(out.promise);
        const second = readSessionState();
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(2); // it waits for the read that is out

        reads.push(ok(counted(1)));
        out.resolve(ok(counted(2))); // taken before the deletion
        await second;
        await flushPromises();
        expect(readsSent).toBe(3);
        expect(recovery.session?.contactCount).toBe(1);
        await flushPromises();
        expect(readsSent).toBe(3);
    });

    it('the read that follows fails: the question and its choices stay, and nothing is asked again', async () => {
        reads.push(ok(counted(1)));
        await readSessionState();
        reads.push(failed);
        recoveryContactDeleted();
        await flushPromises();
        expect(readsSent).toBe(2);
        expect(recovery.status).toBe('question');
        expect(recovery.session?.contactCount).toBe(1);
        await flushPromises();
        expect(readsSent).toBe(2);
    });

    // The operator's ruling, 2026-11-27 (codex-280's last point, option a):
    // when the read the page owed for the count fails, the question says so on
    // the line it already has for what went wrong, with the read's own
    // message. No new words; both choices stay; the next read drawn clears it.
    describe('the read owed for the count fails', () => {
        it("the question says what went wrong, with the read's own message, and both choices stay", async () => {
            reads.push(ok(counted(1)));
            await readSessionState();
            expect(recovery.error).toBe('');
            reads.push(failed);
            recoveryContactDeleted();
            await flushPromises();
            expect(recovery.error).toBe('Cannot reach the daemon.');
            expect(recovery.status).toBe('question'); // still the question
            expect(recovery.session?.contactCount).toBe(1); // the last count received
            expect(recovery.answering).toBe(false); // Continue and New can be pressed
            expect(readsSent).toBe(2); // and the read is not repeated
        });

        it('the next read that is drawn clears it', async () => {
            reads.push(ok(counted(1)));
            await readSessionState();
            reads.push(failed);
            recoveryContactDeleted();
            await flushPromises();
            expect(recovery.error).toBe('Cannot reach the daemon.');

            reads.push(ok(counted(0)));
            expect(await readSessionState()).toBe(true);
            expect(recovery.error).toBe('');
            expect(recovery.session?.contactCount).toBe(0);
        });

        it('when the question comes from a request older than a change, and its follow-up fails: the same', async () => {
            const out = deferred<SessionReadLike>();
            reads.push(out.promise);
            const first = readSessionState();
            recoveryContactDeleted(); // heard while the load's read is out
            reads.push(failed);
            out.resolve(ok(counted(1)));
            await first;
            await flushPromises();
            expect(recovery.status).toBe('question');
            expect(recovery.error).toBe('Cannot reach the daemon.');
            expect(readsSent).toBe(2);
        });

        it('a read that fails with the question showing and NO count owed says nothing, as ever', async () => {
            reads.push(ok(counted(1)));
            await readSessionState();
            reads.push(failed);
            expect(await readSessionState()).toBe(false);
            expect(recovery.status).toBe('question');
            expect(recovery.error).toBe('');
        });

        it('the read that settles answers in doubt is not a read for the count: its failure says nothing', async () => {
            const a = deferred<SessionReadLike>();
            const b = deferred<SessionReadLike>();
            reads.push(a.promise, b.promise);
            const first = readSessionState();
            const second = readSessionState();
            a.resolve(ok(counted(1)));
            await first;
            reads.push(failed); // the read for their order
            b.resolve(ok(counted(1)));
            await second;
            await flushPromises();
            expect(readsSent).toBe(3);
            expect(recovery.status).toBe('question');
            expect(recovery.error).toBe('');
        });

        it('its failure after a newer state has been drawn says nothing: the count shown is not the one it was for', async () => {
            reads.push(ok(counted(2)));
            await readSessionState();
            const owed = deferred<SessionReadLike>();
            reads.push(owed.promise);
            recoveryContactDeleted(); // the owed read goes out, and stalls
            await flushPromises();
            expect(readsSent).toBe(2);

            const event = deferred<SessionReadLike>();
            reads.push(event.promise);
            const fromEvent = readSessionState(); // sent after the deletion: current
            reads.push(ok(counted(1))); // the two overlapped: one more settles their order
            event.resolve(ok(counted(1)));
            await fromEvent;
            expect(recovery.session?.contactCount).toBe(1);

            owed.resolve(failed);
            await flushPromises();
            expect(recovery.error).toBe('');
            expect(recovery.session?.contactCount).toBe(1);
        });
    });
});

// Codex's review (codex-281). When the event stream opens or reopens, the page
// reads the state afresh: what the daemon says of a session depends on which
// app is admitted, and that can change across a connection. A request sent
// BEFORE the opening may have been answered before it too. Its answer is not
// drawn, whenever it arrives: it must not open the workspace, or dismiss the
// question, while the fresh read is still out.
describe('a request sent before the event stream opened draws nothing after it', () => {
    const states = (): string[] => handed.map((s) => s.state);

    it('with nothing shown: an earlier "open" does not show the workspace; the fresh read decides (pending)', async () => {
        const early = deferred<SessionReadLike>();
        const fresh = deferred<SessionReadLike>();
        reads.push(early.promise, fresh.promise);
        const first = readSessionState(); // served: open
        recoveryStreamOpened(); // the stream drops and its replacement opens...
        const second = readSessionState(); // ...and the page reads afresh

        early.resolve(ok(held(4)));
        expect(await first).toBe(false); // not drawn
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // the workspace stays withheld
        expect(states()).toEqual([]); // and the Session is handed nothing

        fresh.resolve(ok(question(4)));
        expect(await second).toBe(true);
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(states()).toEqual(['recovery_pending']);
        expect(readsSent).toBe(2); // the earlier answer left nothing to settle
    });

    it('with nothing shown: the fresh read says open, and the workspace is shown then', async () => {
        const early = deferred<SessionReadLike>();
        const fresh = deferred<SessionReadLike>();
        reads.push(early.promise, fresh.promise);
        const first = readSessionState();
        recoveryStreamOpened();
        const second = readSessionState();
        early.resolve(ok(held(4)));
        await first;
        expect(recoveryGate()).toBe('loading');
        fresh.resolve(ok(held(4)));
        expect(await second).toBe(true);
        expect(recovery.status).toBe('clear');
        await flushPromises();
        expect(readsSent).toBe(2);
    });

    it('with the question showing: an earlier "open" does not dismiss it', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        const early = deferred<SessionReadLike>();
        const fresh = deferred<SessionReadLike>();
        reads.push(early.promise, fresh.promise);
        const first = readSessionState();
        recoveryStreamOpened();
        const second = readSessionState();

        early.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(states()).toEqual(['recovery_pending']);

        fresh.resolve(ok(question(4)));
        await second;
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(readsSent).toBe(3);
    });

    it('the fresh read fails with nothing shown: the session could not be read, and the earlier answer changes nothing', async () => {
        const early = deferred<SessionReadLike>();
        reads.push(early.promise, failed);
        const first = readSessionState();
        recoveryStreamOpened();
        await readSessionState();
        expect(recovery.status).toBe('unreadable');

        early.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(recovery.status).toBe('unreadable');
        expect(states()).toEqual([]);

        reads.push(ok(held(4))); // "Try again": sent on this connection
        expect(await readSessionState()).toBe(true);
        expect(recovery.status).toBe('clear');
    });

    it('a read that fails after the opening, sent before it, says nothing either', async () => {
        const early = deferred<SessionReadLike>();
        const fresh = deferred<SessionReadLike>();
        reads.push(early.promise, fresh.promise);
        const first = readSessionState();
        recoveryStreamOpened();
        const second = readSessionState();
        early.resolve(failed);
        await first;
        expect(recoveryGate()).toBe('loading'); // not "could not be read": the fresh read is out
        fresh.resolve(ok(held(4)));
        await second;
        expect(recovery.status).toBe('clear');
    });

    it('the refusal of an answer sent before the opening is not drawn; nothing is shown until a read sent after it decides', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        const refusal = deferred<SessionAnswerLike>();
        answers.push(refusal.promise);
        const pressedNow = answerRecovery('continue');
        recoveryStreamOpened();
        const fresh = deferred<SessionReadLike>();
        reads.push(fresh.promise);
        const second = readSessionState(); // the fresh read, still out
        const sent = readsSent;

        // The refusal carries "open", as the daemon said before the connection
        // changed. By its number it would be kept, and nothing else has arrived.
        refusal.resolve({ kind: 'stale', state: held(4) });
        await pressedNow;
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // not the workspace; and what the answer did is not known
        expect(states()).toEqual(['recovery_pending']);
        expect(recovery.answering).toBe(false);
        expect(readsSent).toBe(sent); // nothing is asked while the fresh read is out

        // The fresh read was sent before the refusal arrived: the daemon may
        // have served it before the answer. It decides nothing; one read does.
        reads.push(ok(question(4)));
        fresh.resolve(ok(question(4)));
        expect(await second).toBe(false);
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(readsSent).toBe(sent + 1);
        expect(pressed).toHaveLength(1);
        await flushPromises();
        expect(readsSent).toBe(sent + 1);
    });

    it('a request sent after the opening is drawn as ever', async () => {
        recoveryStreamOpened();
        reads.push(ok(held(4)));
        expect(await readSessionState()).toBe(true);
        expect(recovery.status).toBe('clear');
    });
});

// Codex's review (codex-282), first finding. The gate heard the stream OPEN
// but not its loss, so an answer from before the loss was still drawn while
// the replacement was connecting. Nothing sent before the stream was lost is
// drawn after it either.
describe('a request sent before the event stream was lost draws nothing after it', () => {
    const states = (): string[] => handed.map((s) => s.state);

    it('with the question showing: an earlier "open" arriving during the outage does not dismiss it', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        const early = deferred<SessionReadLike>();
        reads.push(early.promise);
        const first = readSessionState(); // served: open
        recoveryStreamLost(); // the socket drops; its replacement is connecting

        early.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(states()).toEqual(['recovery_pending']);
        expect(readsSent).toBe(2); // nothing is asked for it: the opening will read

        recoveryStreamOpened();
        reads.push(ok(question(4)));
        expect(await readSessionState()).toBe(true);
        expect(recovery.status).toBe('question');
    });

    it('with nothing shown: it shows nothing, and with no read left out the page says the session could not be read', async () => {
        const early = deferred<SessionReadLike>();
        reads.push(early.promise);
        const first = readSessionState();
        recoveryStreamLost();
        early.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(states()).toEqual([]); // the workspace is not opened
        expect(recovery.status).toBe('unreadable'); // and the operator has "Try again"
        expect(readsSent).toBe(1);

        recoveryStreamOpened(); // the replacement opens, and the page reads afresh
        reads.push(ok(held(4)));
        expect(await readSessionState()).toBe(true);
        expect(recovery.status).toBe('clear');
    });

    it('with nothing shown and another read still out: it waits for that read', async () => {
        const early = deferred<SessionReadLike>();
        const during = deferred<SessionReadLike>();
        reads.push(early.promise, during.promise);
        const first = readSessionState();
        recoveryStreamLost();
        const second = readSessionState(); // sent during the outage
        early.resolve(ok(held(4)));
        await first;
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // not "could not be read": a read is out
        during.resolve(ok(question(4)));
        expect(await second).toBe(true); // sent after the loss: drawn
        expect(recovery.status).toBe('question');
    });

    it('after an OPENING whose own read has already answered for another archive, a late earlier answer leaves the page as it is', async () => {
        const early = deferred<SessionReadLike>();
        reads.push(early.promise);
        const first = readSessionState();
        recoveryStreamOpened();
        reads.push(ok(held(4, S0, OTHER))); // the opening's read: the daemon serves another archive
        expect(await readSessionState()).toBe(false);
        expect(others).toEqual([OTHER]); // the page's archive check has been started

        early.resolve(ok(held(4)));
        expect(await first).toBe(false);
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // not "could not be read": the check is deciding
    });

    it('a read sent during the outage is not drawn once the stream has opened', async () => {
        reads.push(ok(question(4)));
        await readSessionState();
        recoveryStreamLost();
        const during = deferred<SessionReadLike>();
        reads.push(during.promise);
        const first = readSessionState();
        recoveryStreamOpened();
        const fresh = deferred<SessionReadLike>();
        reads.push(fresh.promise);
        const second = readSessionState();
        during.resolve(ok(held(4)));
        expect(await first).toBe(false);
        expect(recovery.status).toBe('question');
        fresh.resolve(ok(question(4)));
        await second;
        expect(recovery.status).toBe('question');
        expect(states()).toEqual(['recovery_pending', 'recovery_pending']);
    });
});

// Codex's review (codex-282), second finding. An answer to the question that
// was sent before the connection changed is not drawn; but what it DID is then
// not known, and a read already out when it arrived may have been served
// before the daemon applied it. That read must not open the workspace. Nothing
// is shown until a read sent after the answer arrived has answered.
describe('after an answer from before the connection changed, a read already out decides nothing', () => {
    const states = (): string[] => handed.map((s) => s.state);
    const at5 = (s: SessionStateLike): SessionStateLike => ({ ...s, generation: 5 });

    async function continuePressedThenReconnected(): Promise<{
        refusal: { resolve: (v: SessionAnswerLike) => void };
        reconnect: { resolve: (v: SessionReadLike) => void };
        pressedNow: Promise<void>;
    }> {
        reads.push(ok(question(4)));
        await readSessionState();
        const refusal = deferred<SessionAnswerLike>();
        answers.push(refusal.promise);
        const pressedNow = answerRecovery('continue'); // at generation 4
        recoveryStreamLost();
        recoveryStreamOpened();
        const reconnect = deferred<SessionReadLike>();
        reads.push(reconnect.promise);
        void readSessionState(); // the reconnect's read, held
        return { refusal, reconnect, pressedNow };
    }

    it('the reconnect read says open: the workspace is withheld until a later read decides (pending)', async () => {
        const { refusal, reconnect, pressedNow } = await continuePressedThenReconnected();
        refusal.resolve({ kind: 'stale', state: at5(question(4)) }); // recovery pending at 5
        await pressedNow;
        await flushPromises();
        expect(recoveryGate()).toBe('loading');
        const sent = readsSent;

        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        reconnect.resolve(ok(at5(held(4)))); // open at 5, served who knows when
        await flushPromises();
        expect(recoveryGate()).toBe('loading'); // never the workspace
        expect(states()).toEqual(['recovery_pending']);
        expect(readsSent).toBe(sent + 1); // the read that decides, sent now

        deciding.resolve(ok(at5(question(4))));
        await flushPromises();
        expect(recovery.status).toBe('question');
        expect(recovery.generation).toBe(5);
        expect(pressed).toHaveLength(1);
    });

    it('the deciding read fails: the session could not be read, and "Try again" reads it', async () => {
        const { refusal, reconnect, pressedNow } = await continuePressedThenReconnected();
        refusal.resolve({ kind: 'stale', state: at5(question(4)) });
        await pressedNow;
        reads.push(failed);
        reconnect.resolve(ok(at5(held(4))));
        await flushPromises();
        expect(recovery.status).toBe('unreadable');
        expect(states()).toEqual(['recovery_pending']); // the workspace was never opened
        const sent = readsSent;
        await flushPromises();
        expect(readsSent).toBe(sent); // not repeated

        reads.push(ok(at5(held(4))));
        expect(await readSessionState()).toBe(true);
        expect(recovery.status).toBe('clear');
        expect(pressed).toHaveLength(1);
    });

    it('an applied answer from before the connection changed: shown by the read that follows, not before', async () => {
        const { refusal, reconnect, pressedNow } = await continuePressedThenReconnected();
        reconnect.resolve(ok(question(4))); // served before the answer was applied
        await flushPromises();
        expect(recovery.status).toBe('question');
        const sent = readsSent;

        const deciding = deferred<SessionReadLike>();
        reads.push(deciding.promise);
        refusal.resolve({ kind: 'applied', state: at5(held(4)) });
        await pressedNow;
        await flushPromises();
        expect(readsSent).toBe(sent + 1); // the read that decides
        expect(recoveryGate()).toBe('loading'); // neither the old question nor the workspace
        expect(states()).toEqual(['recovery_pending', 'recovery_pending']); // nothing from the answer itself

        deciding.resolve(ok(at5(held(4))));
        await flushPromises();
        expect(recovery.status).toBe('clear');
        expect(recovery.generation).toBe(5);
    });
});
