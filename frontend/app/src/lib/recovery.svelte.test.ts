import { beforeEach, describe, expect, it } from 'vitest';
import {
    recovery,
    recoveryGate,
    readSessionState,
    sessionArchiveProven,
    answerRecovery,
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

const ok = (state: SessionStateLike): SessionReadLike => ({ kind: 'ok', state });
const failed: SessionReadLike = { kind: 'error', message: 'Cannot reach the daemon.' };

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
    setRecoveryPorts({
        drawn: (state) => handed.push(state),
        otherArchive: (id) => others.push(id),
        read: () => {
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
