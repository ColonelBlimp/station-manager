import { beforeEach, describe, expect, it } from 'vitest';
import {
    recovery,
    recoveryGate,
    readSessionState,
    sessionArchiveProven,
    answerRecovery,
    sendSessionControl,
    readAfterUnknown,
    recoveryStreamLost,
    recoveryStreamOpened,
    setRecoveryPorts,
    _resetRecoveryForTests,
    type SessionStateLike,
    type SessionReadLike,
    type SessionAnswerLike,
} from './recovery.svelte';

// W-0029 part 6, slice 3: a control sent from the WORKSPACE (the Session
// panel's New and Continue) goes by the one path the recovery answer uses.
// What differs is what the page shows when the control's outcome is not known:
// the workspace stays (the operator's ruling 1, 2026-11-27).
//
// The nearest confusable outcomes: the workspace stays because the gate did
// nothing at all (so each test also says what may NOT be sent meanwhile); and
// the controls come back on a read that was sent before the failure.

const HOME = '01a16a00-0000-7000-8000-000000000001';
const S0 = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const S1 = '01a1f83c-da20-751f-80b7-ec9a3f1f93f4';
const S2 = '01a1f83c-da20-751f-80b7-ec9a3f1f93f5';

const open = (generation: number, id = S0): SessionStateLike => ({
    archiveId: HOME,
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
const question = (generation: number, id = S0): SessionStateLike => ({
    ...open(generation, id),
    state: 'recovery_pending',
});

let reads: Array<SessionReadLike | Promise<SessionReadLike>>;
let answers: Array<SessionAnswerLike | Promise<SessionAnswerLike>>;
let pressed: Array<[string, number, string]>;
let handed: SessionStateLike[];
let told: string[];
let readsSent: number;
// Every status the gate had at a moment the test looked: the workspace is
// showing only while it is "clear".
let statuses: string[];

const ok = (state: SessionStateLike): SessionReadLike => ({ kind: 'ok', state });
const failed = (message = 'Cannot reach the daemon.'): SessionReadLike => ({
    kind: 'error',
    message,
});
const STALE = 'The session changed before this could be done. Your choice was not applied.';

async function flushPromises(): Promise<void> {
    for (let i = 0; i < 10; i++) await Promise.resolve();
    statuses.push(recovery.status);
}

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => (resolve = r));
    return { promise, resolve };
}

// The workspace showing session S0 at generation 4.
async function workspace(): Promise<void> {
    reads.push(ok(open(4)));
    await sessionArchiveProven();
    expect(recoveryGate()).toBeNull();
    handed.length = 0;
    told.length = 0;
    readsSent = 0;
}

beforeEach(() => {
    _resetRecoveryForTests();
    reads = [];
    answers = [];
    pressed = [];
    handed = [];
    told = [];
    readsSent = 0;
    statuses = [];
    setRecoveryPorts({
        drawn: (state) => handed.push(state),
        control: (v) => told.push(v),
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
        archive: () => HOME,
    });
});

describe('a control sent from the workspace', () => {
    it('carries the generation and the session it was given, and the state returned is drawn', async () => {
        await workspace();
        answers.push({ kind: 'applied', state: open(6, S1) });

        // The list it was chosen from was read at generation 4.
        const end = await sendSessionControl('continue', 4, S1);

        expect(end).toBe('applied');
        expect(pressed).toEqual([['continue', 4, S1]]);
        expect(handed.map((s) => [s.generation, s.session?.id])).toEqual([[6, S1]]);
        expect(recovery.generation).toBe(6);
        expect(recovery.answering).toBe(false);
        expect(recovery.controlError).toBe('');
        expect(recoveryGate()).toBeNull();
        expect(readsSent).toBe(0); // the page works out nothing: it drew what was returned
    });

    it('is not sent while the question, the loading screen or "could not be read" is showing', async () => {
        // Loading: nothing read yet.
        expect(await sendSessionControl('new', 0, '')).toBe('unsent');

        // The question.
        reads.push(ok(question(4)));
        await sessionArchiveProven();
        expect(recovery.status).toBe('question');
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');

        // Could not be read.
        _resetRecoveryForTests();
        setRecoveryPorts({
            read: () => Promise.resolve(failed()),
            answer: (a, g, s) => {
                pressed.push([a, g, s]);
                return Promise.resolve({ kind: 'failed', message: 'x' });
            },
            archive: () => HOME,
        });
        await sessionArchiveProven();
        expect(recovery.status).toBe('unreadable');
        expect(await sendSessionControl('new', 0, '')).toBe('unsent');

        expect(pressed).toEqual([]);
    });

    it('one control is out at a time for the whole page', async () => {
        await workspace();
        const first = deferred<SessionAnswerLike>();
        answers.push(first.promise);

        const sending = sendSessionControl('new', 4, '');
        await flushPromises();
        expect(recovery.answering).toBe(true);
        expect(await sendSessionControl('continue', 4, S1)).toBe('unsent');
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');
        expect(pressed).toEqual([['new', 4, '']]);

        first.resolve({ kind: 'applied', state: open(6, S1) });
        expect(await sending).toBe('applied');
        expect(recovery.answering).toBe(false);
    });

    it('the recovery answer and a control from the workspace share that one', async () => {
        reads.push(ok(question(4)));
        await sessionArchiveProven();
        const first = deferred<SessionAnswerLike>();
        answers.push(first.promise);
        const answering = answerRecovery('continue');
        await flushPromises();

        expect(await sendSessionControl('new', 4, '')).toBe('unsent');
        expect(pressed).toEqual([['continue', 4, S0]]);

        first.resolve({ kind: 'applied', state: open(5) });
        await answering;
    });

    it('a stale refusal draws the state returned, says the choice was not applied, and sends nothing again', async () => {
        await workspace();
        answers.push({ kind: 'stale', state: open(7, S2) });

        const end = await sendSessionControl('continue', 4, S1);
        await flushPromises();

        expect(end).toBe('stale');
        expect(recovery.controlError).toBe(STALE);
        expect(handed.map((s) => [s.generation, s.session?.id])).toEqual([[7, S2]]);
        expect(pressed).toEqual([['continue', 4, S1]]);
        expect(recovery.answering).toBe(false);
        expect(recoveryGate()).toBeNull();
    });

    it('a stale refusal that says recovery is pending puts the question in place of the workspace', async () => {
        await workspace();
        answers.push({ kind: 'stale', state: question(7, S2) });

        await sendSessionControl('new', 4, '');

        expect(recovery.status).toBe('question');
        expect(recovery.session?.id).toBe(S2);
    });
});

describe('a control from the workspace fails', () => {
    it('the page says what went wrong and reads; nothing more is sent until that read is drawn', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        const finding = deferred<SessionReadLike>();
        reads.push(finding.promise);

        const sending = sendSessionControl('new', 4, '');
        await flushPromises();

        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(readsSent).toBe(1);
        expect(recovery.answering).toBe(true);
        expect(recoveryGate()).toBeNull(); // the workspace is still showing
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');

        // The New had been applied: the read shows the new session.
        finding.resolve(ok(open(6, S1)));
        expect(await sending).toBe('failed');

        expect(handed.map((s) => [s.generation, s.session?.id])).toEqual([[6, S1]]);
        expect(recovery.answering).toBe(false);
        expect(recovery.unknown).toBe(false);
        expect(recovery.controlError).toBe('The daemon is busy.'); // the page still says so
        expect(pressed).toEqual([['new', 4, '']]); // and sent nothing again by itself
        expect(readsSent).toBe(1);
    });

    it('and the read fails too: the workspace stays, the session is not known, and no control can be sent', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed('Cannot reach the daemon.'));

        expect(await sendSessionControl('continue', 4, S1)).toBe('failed');
        await flushPromises();

        expect(recovery.status).toBe('clear');
        expect(recoveryGate()).toBeNull();
        expect(recovery.unknown).toBe(true);
        // What the control came to stays said; why the session could not be
        // read is said beside it (codex-291).
        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(recovery.unreadError).toBe('Cannot reach the daemon.');
        expect(handed).toEqual([]);
        // Nothing can be sent on a session the page does not know.
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');
        expect(await sendSessionControl('continue', 4, S1)).toBe('unsent');
        expect(pressed).toEqual([['continue', 4, S1]]);
        expect(readsSent).toBe(1); // and nothing reads in a loop
    });

    it('"Try again" sends one read and no control; drawn, the session is known again', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed());
        await sendSessionControl('new', 4, '');
        expect(recovery.unknown).toBe(true);

        reads.push(ok(open(6, S1)));
        expect(await readAfterUnknown()).toBe(true);

        expect(readsSent).toBe(2);
        expect(pressed).toEqual([['new', 4, '']]);
        expect(recovery.unknown).toBe(false);
        expect(handed.map((s) => [s.generation, s.session?.id])).toEqual([[6, S1]]);
        expect(recoveryGate()).toBeNull();
        // The control had been applied after all: there is nothing left to say.
        expect(recovery.controlError).toBe('');
        // And a control can be sent again.
        answers.push({ kind: 'applied', state: open(8, S2) });
        expect(await sendSessionControl('new', 6, '')).toBe('applied');
    });

    it('"Try again" that finds the same session goes on saying what went wrong with the control', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed('Cannot reach the daemon.'));
        await sendSessionControl('new', 4, '');

        reads.push(ok(open(4)));
        await readAfterUnknown();

        expect(recovery.unknown).toBe(false);
        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(recovery.unreadError).toBe('');
    });

    it('"Try again" that fails leaves the session not known, with what went wrong now', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed('Cannot reach the daemon.'));
        await sendSessionControl('new', 4, '');

        reads.push(failed('The daemon answered 503.'));
        expect(await readAfterUnknown()).toBe(false);

        expect(recovery.unknown).toBe(true);
        expect(recovery.status).toBe('clear');
        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(recovery.unreadError).toBe('The daemon answered 503.');
        expect(pressed).toEqual([['new', 4, '']]);
    });

    it('"Try again" that says recovery is pending puts the question in place of the workspace', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed());
        await sendSessionControl('new', 4, '');

        reads.push(ok(question(6, S1)));
        await readAfterUnknown();

        expect(recovery.status).toBe('question');
        expect(recovery.unknown).toBe(false);
    });

    it('"Try again" does nothing when the session is known', async () => {
        await workspace();
        expect(await readAfterUnknown()).toBe(false);
        expect(readsSent).toBe(0);
    });

    it('a read sent BEFORE the failure does not make the session known, answered or failed', async () => {
        await workspace();
        // A read is out (an event asked for it) when the control is sent.
        const earlier = deferred<SessionReadLike>();
        reads.push(earlier.promise);
        const reading = readSessionState();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed());
        await sendSessionControl('new', 4, '');
        expect(recovery.unknown).toBe(true);

        // It answers now, with the session as it was before the control.
        earlier.resolve(ok(open(4)));
        await reading;
        await flushPromises();

        expect(recovery.unknown).toBe(true);
        expect(handed).toEqual([]);
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');
        expect(pressed).toEqual([['new', 4, '']]);
    });

    it('a read sent AFTER the failure and drawn makes the session known, whoever asked for it', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed());
        await sendSessionControl('new', 4, '');
        expect(recovery.unknown).toBe(true);

        // An event asks for the state to be read.
        reads.push(ok(open(6, S1)));
        await readSessionState();

        expect(recovery.unknown).toBe(false);
        expect(handed.map((s) => s.generation)).toEqual([6]);
    });

    it('an ordinary read that fails while the session is not known changes nothing', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(failed('Cannot reach the daemon.'));
        await sendSessionControl('new', 4, '');

        reads.push(failed('Something else.'));
        await readSessionState();

        expect(recovery.unknown).toBe(true);
        expect(recovery.status).toBe('clear');
        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(recovery.unreadError).toBe('Cannot reach the daemon.');
    });

    it('the connection changes while the page finds out: the workspace stays and the session is not known', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        const finding = deferred<SessionReadLike>();
        reads.push(finding.promise);
        const sending = sendSessionControl('new', 4, '');
        await flushPromises();

        // The event stream is lost; the read from before answers.
        recoveryStreamLost();
        finding.resolve(ok(open(6, S1)));
        await sending;
        await flushPromises();

        expect(handed).toEqual([]); // nothing from before the loss is drawn
        expect(recovery.status).toBe('clear');
        expect(recovery.unknown).toBe(true);
        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');
        expect(statuses.every((s) => s === 'clear')).toBe(true);

        // The stream opens and its read is drawn: known again.
        recoveryStreamOpened();
        reads.push(ok(open(6, S1)));
        await readSessionState();
        expect(recovery.unknown).toBe(false);
        expect(handed.map((s) => s.generation)).toEqual([6]);
    });
});

describe('the read that finds out is answered, and is not drawn (codex-290)', () => {
    const OTHER = '01a16a00-0000-7000-8000-0000000000ff';

    it('answered for another archive: the session stays not known, with "Try again", and no control can be sent', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(ok({ ...open(4), archiveId: OTHER }));

        expect(await sendSessionControl('continue', 4, S1)).toBe('failed');
        await flushPromises();

        // Which session is open in THIS archive is not known from that answer.
        expect(handed).toEqual([]);
        expect(recovery.status).toBe('clear');
        expect(recovery.unknown).toBe(true);
        expect(recovery.controlError).toBe('The daemon is busy.');
        expect(told.at(-1)).toBe('unknown');
        expect(await sendSessionControl('new', 4, '')).toBe('unsent');
        expect(pressed).toEqual([['continue', 4, S1]]);

        // A state for this archive, sent since, is what makes it known.
        reads.push(ok(open(4)));
        expect(await readAfterUnknown()).toBe(true);
        expect(recovery.unknown).toBe(false);
    });

    it('the same for the answer to the QUESTION: the old question does not come back with its choices', async () => {
        reads.push(ok(question(4)));
        await sessionArchiveProven();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        reads.push(ok({ ...question(4), archiveId: OTHER }));

        await answerRecovery('continue');
        await flushPromises();

        expect(recovery.status).toBe('unreadable');
        expect(recovery.error).toBe('The daemon is busy.');
        expect(pressed).toEqual([['continue', 4, S0]]);
    });
});

// codex-292: a read sent while the control was out answers, and the control's
// failure arrives in the same turn. The read is drawn (it was valid when it was
// drawn: no failure was known), the failure is then learnt and its own read
// sent, and only THEN does the first read's handling come to its end, holding a
// "drawn". That draw says nothing of a failure learnt after it: only a read
// SENT after the failure may give the choices back.
describe('a read drawn just before the failure was learnt does not settle it (codex-292)', () => {
    it.each([
        ['the read answers, then the control fails', true],
        ['the control fails, then the read answers', false],
    ])(
        'from the workspace: a second press sends nothing while the read that finds out is out (%s)',
        async (_n, readFirst) => {
            await workspace();
            const control = deferred<SessionAnswerLike>();
            answers.push(control.promise);
            const sending = sendSessionControl('continue', 4, S1);
            await flushPromises();
            // A read is asked for while the control is out (an event).
            const earlier = deferred<SessionReadLike>();
            reads.push(earlier.promise);
            const reading = readSessionState();
            // The read that finds out, which stays unanswered.
            const finding = deferred<SessionReadLike>();
            reads.push(finding.promise);

            // Both bodies arrive in the same turn.
            const fail = (): void =>
                control.resolve({ kind: 'failed', message: 'The daemon is busy.' });
            if (readFirst) {
                earlier.resolve(ok(open(4)));
                fail();
            } else {
                fail();
                earlier.resolve(ok(open(4)));
            }
            await reading;
            await flushPromises();

            expect(readsSent).toBe(2); // the earlier read, and the one that finds out
            expect(recovery.answering).toBe(true);
            expect(recovery.unknown).toBe(false);
            expect(await sendSessionControl('continue', 4, S1)).toBe('unsent');
            expect(await sendSessionControl('new', 4, '')).toBe('unsent');
            expect(pressed).toEqual([['continue', 4, S1]]);

            // A state for this archive, sent after the failure: the controls come back.
            finding.resolve(ok(open(4)));
            expect(await sending).toBe('failed');
            expect(recovery.answering).toBe(false);
            answers.push({ kind: 'applied', state: open(6, S1) });
            expect(await sendSessionControl('continue', 4, S1)).toBe('applied');
            expect(pressed).toHaveLength(2);
        }
    );

    it.each([
        ['the read answers, then the answer fails', true],
        ['the answer fails, then the read answers', false],
    ])(
        'from the question: both choices stay disabled, and a second press sends nothing (%s)',
        async (_n, readFirst) => {
            reads.push(ok(question(4)));
            await sessionArchiveProven();
            const control = deferred<SessionAnswerLike>();
            answers.push(control.promise);
            const answering = answerRecovery('continue');
            await flushPromises();
            const earlier = deferred<SessionReadLike>();
            reads.push(earlier.promise);
            const reading = readSessionState();
            const finding = deferred<SessionReadLike>();
            reads.push(finding.promise);

            const fail = (): void =>
                control.resolve({ kind: 'failed', message: 'The daemon is busy.' });
            if (readFirst) {
                earlier.resolve(ok(question(4)));
                fail();
            } else {
                fail();
                earlier.resolve(ok(question(4)));
            }
            await reading;
            await flushPromises();

            expect(recovery.status).toBe('question');
            expect(recovery.answering).toBe(true);
            await answerRecovery('continue');
            await answerRecovery('new');
            expect(pressed).toEqual([['continue', 4, S0]]);

            finding.resolve(ok(question(4)));
            await answering;
            expect(recovery.answering).toBe(false);
        }
    );
});

describe('a failed control is never left with nothing reading and nothing said (codex-291)', () => {
    const OTHER = '01a16a00-0000-7000-8000-0000000000ff';

    // The control failed; its read is out; the event stream drops and reopens.
    // The reopening's read is answered for ANOTHER archive, and the control's
    // own read is dropped for its old connection. Neither is drawn, and neither
    // ends inside the control's own handling.
    it.each([
        ["the reopening's read answers first", true],
        ['the read from before answers first', false],
    ])(
        'from the workspace: the session is not known, and "Try again" is there (%s)',
        async (_n, reopenFirst) => {
            await workspace();
            answers.push({ kind: 'failed', message: 'The daemon is busy.' });
            const finding = deferred<SessionReadLike>();
            reads.push(finding.promise);
            const sending = sendSessionControl('new', 4, '');
            await flushPromises();

            recoveryStreamLost();
            recoveryStreamOpened();
            const reopening = deferred<SessionReadLike>();
            reads.push(reopening.promise);
            const reading = readSessionState();
            const foreign = ok({ ...open(4), archiveId: OTHER });
            if (reopenFirst) {
                reopening.resolve(foreign);
                await reading;
                finding.resolve(ok(open(4)));
            } else {
                finding.resolve(ok(open(4)));
                await sending;
                reopening.resolve(foreign);
            }
            await sending;
            await reading;
            await flushPromises();

            expect(handed).toEqual([]);
            expect(recovery.status).toBe('clear');
            expect(recovery.answering).toBe(false);
            expect(recovery.unknown).toBe(true);
            expect(told.at(-1)).toBe('unknown');
            expect(await sendSessionControl('new', 4, '')).toBe('unsent');

            // A state for this archive, sent since: known again.
            reads.push(ok(open(4)));
            expect(await readAfterUnknown()).toBe(true);
            expect(recovery.unknown).toBe(false);
        }
    );

    it.each([
        ["the reopening's read answers first", true],
        ['the read from before answers first', false],
    ])(
        'from the question: the session could not be read, with "Try again" (%s)',
        async (_n, reopenFirst) => {
            reads.push(ok(question(4)));
            await sessionArchiveProven();
            answers.push({ kind: 'failed', message: 'The daemon is busy.' });
            const finding = deferred<SessionReadLike>();
            reads.push(finding.promise);
            const answering = answerRecovery('continue');
            await flushPromises();

            recoveryStreamLost();
            recoveryStreamOpened();
            const reopening = deferred<SessionReadLike>();
            reads.push(reopening.promise);
            const reading = readSessionState();
            const foreign = ok({ ...question(4), archiveId: OTHER });
            if (reopenFirst) {
                reopening.resolve(foreign);
                await reading;
                finding.resolve(ok(question(4)));
            } else {
                finding.resolve(ok(question(4)));
                await answering;
                reopening.resolve(foreign);
            }
            await answering;
            await reading;
            await flushPromises();

            expect(recovery.status).toBe('unreadable');
            expect(recovery.answering).toBe(false);
            expect(pressed).toEqual([['continue', 4, S0]]);
        }
    );

    it("a state for this archive drawn by the reopening's read still settles it", async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        const finding = deferred<SessionReadLike>();
        reads.push(finding.promise);
        const sending = sendSessionControl('new', 4, '');
        await flushPromises();
        recoveryStreamLost();
        recoveryStreamOpened();
        reads.push(ok(open(6, S1)));
        await readSessionState();
        finding.resolve(ok(open(4)));
        await sending;

        expect(recovery.unknown).toBe(false);
        expect(recovery.answering).toBe(false);
        expect(handed.map((s) => s.generation)).toEqual([6]);
    });
});

describe('a stale refusal is said wherever the operator ends up (codex-290)', () => {
    it('with the question put in place of the workspace, the ruled sentence is still there to show', async () => {
        await workspace();
        answers.push({ kind: 'stale', state: question(7, S2) });

        await sendSessionControl('new', 4, '');
        // The reads the page makes for itself afterwards do not take it away.
        reads.push(ok(question(7, S2)));
        await readSessionState();

        expect(recovery.status).toBe('question');
        expect(recovery.controlError).toBe(STALE);
    });

    it('answering the question clears it', async () => {
        await workspace();
        answers.push({ kind: 'stale', state: question(7, S2) });
        await sendSessionControl('new', 4, '');

        const out = deferred<SessionAnswerLike>();
        answers.push(out.promise);
        const answering = answerRecovery('continue');
        await flushPromises();
        expect(recovery.controlError).toBe('');
        out.resolve({ kind: 'applied', state: open(8, S2) });
        await answering;
    });

    it('refused as stale AFTER the connection changed: the choice was declined, and the page says that', async () => {
        await workspace();
        const first = deferred<SessionAnswerLike>();
        answers.push(first.promise);
        const sending = sendSessionControl('continue', 4, S1);
        await flushPromises();
        recoveryStreamLost();
        recoveryStreamOpened();
        reads.push(ok(open(7, S2)));

        first.resolve({ kind: 'stale', state: open(7, S2) });
        expect(await sending).toBe('failed');

        // Its state is not drawn (the connection changed); the read's is.
        expect(handed.map((s) => [s.generation, s.session?.id])).toEqual([[7, S2]]);
        expect(recovery.controlError).toBe(STALE);
        expect(statuses.every((s) => s === 'clear')).toBe(true);
    });

    it('and the read after it FAILS: the refusal is still said, with why the session could not be read beside it (codex-291)', async () => {
        await workspace();
        const first = deferred<SessionAnswerLike>();
        answers.push(first.promise);
        const sending = sendSessionControl('continue', 4, S1);
        await flushPromises();
        recoveryStreamLost();
        recoveryStreamOpened();
        reads.push(failed('Cannot reach the daemon.'));

        first.resolve({ kind: 'stale', state: open(7, S2) });
        await sending;

        expect(recovery.unknown).toBe(true);
        expect(recovery.controlError).toBe(STALE);
        expect(recovery.unreadError).toBe('Cannot reach the daemon.');
    });
});

describe('a control from the workspace is answered after the connection changed', () => {
    it('the workspace is never taken away: its answer is not drawn, and the page reads', async () => {
        await workspace();
        const first = deferred<SessionAnswerLike>();
        answers.push(first.promise);
        const sending = sendSessionControl('new', 4, '');
        await flushPromises();

        // The stream drops and reopens while the control is out. What the
        // daemon says of a session depends on which app is admitted, and that
        // can change across a connection.
        recoveryStreamLost();
        recoveryStreamOpened();
        const finding = deferred<SessionReadLike>();
        reads.push(finding.promise);
        first.resolve({ kind: 'applied', state: open(6, S1) });
        await flushPromises();

        expect(handed).toEqual([]);
        expect(recovery.status).toBe('clear');
        expect(recovery.answering).toBe(true);
        expect(recovery.controlError).toBe(
            'The connection to the daemon changed while this was being done.'
        );
        expect(readsSent).toBe(1);

        finding.resolve(ok(open(6, S1)));
        expect(await sending).toBe('failed');
        expect(handed.map((s) => [s.generation, s.session?.id])).toEqual([[6, S1]]);
        expect(recovery.answering).toBe(false);
        expect(statuses.every((s) => s === 'clear')).toBe(true);
        expect(pressed).toEqual([['new', 4, '']]);
    });
});

describe('what the gate says of a control', () => {
    it('"out" while it is on its way, and nothing once its state is drawn', async () => {
        await workspace();
        const first = deferred<SessionAnswerLike>();
        answers.push(first.promise);
        const sending = sendSessionControl('new', 4, '');
        await flushPromises();
        expect(told.at(-1)).toBe('out');

        first.resolve({ kind: 'applied', state: open(6, S1) });
        await sending;
        expect(told.at(-1)).toBe('');
    });

    it('"out" while the page finds out what a failed control did, "unknown" when it cannot, and nothing once a read is drawn', async () => {
        await workspace();
        answers.push({ kind: 'failed', message: 'The daemon is busy.' });
        const finding = deferred<SessionReadLike>();
        reads.push(finding.promise);
        const sending = sendSessionControl('new', 4, '');
        await flushPromises();
        expect(told.at(-1)).toBe('out');

        finding.resolve(failed());
        await sending;
        expect(told.at(-1)).toBe('unknown');

        reads.push(ok(open(4)));
        await readAfterUnknown();
        expect(told.at(-1)).toBe('');
    });
});

describe('what went wrong with a control is said until something newer replaces it', () => {
    it('the next control clears it', async () => {
        await workspace();
        answers.push({ kind: 'stale', state: open(7, S2) });
        await sendSessionControl('continue', 4, S1);
        expect(recovery.controlError).toBe(STALE);

        const next = deferred<SessionAnswerLike>();
        answers.push(next.promise);
        const sending = sendSessionControl('new', 7, '');
        await flushPromises();
        expect(recovery.controlError).toBe('');
        next.resolve({ kind: 'applied', state: open(8, S1) });
        await sending;
    });

    it('a state of another generation drawn later clears it; one of the same generation does not', async () => {
        await workspace();
        answers.push({ kind: 'stale', state: open(7, S2) });
        await sendSessionControl('continue', 4, S1);

        reads.push(ok(open(7, S2)));
        await readSessionState();
        expect(recovery.controlError).toBe(STALE);

        reads.push(ok(open(8, S1)));
        await readSessionState();
        expect(recovery.controlError).toBe('');
    });
});
