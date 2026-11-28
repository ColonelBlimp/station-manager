import { beforeEach, describe, expect, it } from 'vitest';
import {
    continueList,
    continueChoosable,
    continueStateDrawn,
    continueStreamReopened,
    continueContactDeleted,
    openContinue,
    closeContinue,
    showMoreContinue,
    retryContinue,
    chooseContinue,
    setContinuePorts,
    _resetContinueForTests,
    type ClosedListReadLike,
    type ClosedSessionLike,
} from './continueList.svelte';

// W-0029 part 6, slice 3: Continue's list (criteria F). The module never
// imports lib/api (ADR 0045): the read, the bound archive, the read of the
// state and the control are injected, and here the test is the daemon.
//
// The nearest confusable outcomes: a list that cannot be read shown as "nothing
// to continue"; pages of two different lists shown as one; a count from before
// a deletion; and a choice sent at whatever generation the page then holds
// instead of the generation of the list the operator chose from.

const HOME = '01a16a00-0000-7000-8000-000000000001';
const OTHER = '01a16a00-0000-7000-8000-0000000000ff';
const LOGBOOK = '01a187bc-0173-7649-8ff3-71db8170c309';
const id = (n: number): string => `0190${String(n).padStart(4, '0')}-0000-7000-8000-000000000000`;

const closed = (n: number, contactCount = 1): ClosedSessionLike => ({
    id: id(n),
    openedAt: '2026-11-20T09:15:00Z',
    latestSittingStartedAt: '2026-11-20T09:15:00Z',
    latestSittingEndedAt: '2026-11-20T11:42:00Z',
    anEndUnconfirmed: false,
    contactCount,
});

// The daemon: the closed sessions it holds, newest first, and its generation.
let held: ClosedSessionLike[];
let generation: number;
let archive: string;
let bound: string | null;
type Asked = { limit: number; after?: { sessionId: string; generation: number } };
let asked: Asked[];
// Answers the test gives in place of the daemon's own, in order; a promise holds one.
let scripted: Array<ClosedListReadLike | Promise<ClosedListReadLike> | undefined>;
let stateReads: number;
let sent: Array<[number, string]>;
let sendEnds: Array<'applied' | 'stale' | 'failed' | 'unsent'>;
let others: string[];

function answer(q: Asked): ClosedListReadLike {
    let from = 0;
    if (q.after !== undefined) {
        const at = held.findIndex((s) => s.id === q.after?.sessionId);
        if (q.after.generation !== generation || at < 0) return { kind: 'stale' };
        from = at + 1;
    }
    const page = held.slice(from, from + q.limit);
    return {
        kind: 'ok',
        archiveId: archive,
        generation,
        logbookUuid: LOGBOOK,
        sessions: page.map((s) => ({ ...s })),
        more: from + q.limit < held.length,
    };
}

async function flush(): Promise<void> {
    for (let i = 0; i < 20; i++) await Promise.resolve();
}

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void } {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => (resolve = r));
    return { promise, resolve };
}

const drawn = (g: number, state: 'open' | 'none' | 'recovery_pending' = 'open', a = HOME): void =>
    continueStateDrawn({ archiveId: a, generation: g, state });

beforeEach(() => {
    _resetContinueForTests();
    held = [closed(3), closed(2), closed(1)];
    generation = 4;
    archive = HOME;
    bound = HOME;
    asked = [];
    scripted = [];
    stateReads = 0;
    sent = [];
    sendEnds = [];
    others = [];
    setContinuePorts({
        read: (limit, after) => {
            const q = { limit, after };
            asked.push(q);
            const next = scripted.shift();
            return Promise.resolve(next ?? answer(q));
        },
        archive: () => bound,
        readState: () => {
            stateReads++;
        },
        send: (g, sessionId) => {
            sent.push([g, sessionId]);
            return Promise.resolve(sendEnds.shift() ?? 'applied');
        },
        otherArchive: (a) => others.push(a),
    });
});

describe('whether Continue is offered', () => {
    it('is asked when a state is drawn for the workspace, and is not offered until it is known', async () => {
        expect(continueList.available).toBe('unread');

        drawn(4);
        expect(continueList.available).toBe('unread');
        await flush();

        expect(asked).toEqual([{ limit: 1, after: undefined }]);
        expect(continueList.available).toBe('some');
    });

    it('is "none" when the daemon holds no closed session', async () => {
        held = [];
        drawn(4);
        await flush();
        expect(continueList.available).toBe('none');
    });

    it('a list that cannot be read is unreadable, never "none"', async () => {
        scripted.push({ kind: 'error', message: 'Cannot reach the daemon.' });
        drawn(4);
        await flush();
        expect(continueList.available).toBe('unreadable');
    });

    it('is not asked again for the same generation, and is for another', async () => {
        drawn(4);
        await flush();
        drawn(4);
        await flush();
        expect(asked).toHaveLength(1);

        generation = 6;
        held = [];
        drawn(6);
        await flush();
        expect(asked).toHaveLength(2);
        expect(continueList.available).toBe('none');
    });

    it('is asked with no session open as well', async () => {
        drawn(4, 'none');
        await flush();
        expect(continueList.available).toBe('some');
    });

    it('is not asked, and nothing is offered, in a state where the controls are not shown', async () => {
        drawn(4);
        await flush();
        drawn(5, 'recovery_pending');
        await flush();
        expect(asked).toHaveLength(1);
        expect(continueList.available).toBe('unread');
    });

    it('a state drawn while the read is out is followed by one more read', async () => {
        const first = deferred<ClosedListReadLike>();
        scripted.push(first.promise);
        drawn(4);
        await flush();
        generation = 6;
        held = [];
        drawn(6);
        await flush();
        expect(asked).toHaveLength(1);

        first.resolve({
            kind: 'ok',
            archiveId: HOME,
            generation: 4,
            logbookUuid: LOGBOOK,
            sessions: [closed(1)],
            more: false,
        });
        await flush();

        expect(asked).toHaveLength(2);
        expect(continueList.available).toBe('none');
    });

    it('an answer for another archive than the page is bound to is not drawn, and is reported', async () => {
        archive = OTHER;
        drawn(4);
        await flush();
        expect(continueList.available).toBe('unread');
        expect(others).toEqual([OTHER]);
    });

    it('an answer that comes back after the page was bound to another archive is not drawn', async () => {
        const first = deferred<ClosedListReadLike>();
        scripted.push(first.promise);
        drawn(4);
        await flush();
        bound = OTHER;
        first.resolve(answer({ limit: 1 }));
        await flush();
        expect(continueList.available).toBe('unread');
    });

    it('a list at a newer generation than the state shown asks for the state to be read, once', async () => {
        generation = 6;
        drawn(4);
        await flush();
        expect(stateReads).toBe(1);
        expect(continueList.available).toBe('some');

        // The state is drawn at that generation: nothing more is read.
        drawn(6);
        await flush();
        expect(asked).toHaveLength(1);
        expect(stateReads).toBe(1);
    });

    it('is asked again when the event stream reopens', async () => {
        drawn(4);
        await flush();
        held = [];
        continueStreamReopened();
        await flush();
        expect(asked).toHaveLength(2);
        expect(continueList.available).toBe('none');
    });
});

describe('the dialog', () => {
    beforeEach(async () => {
        drawn(4);
        await flush();
        asked.length = 0;
    });

    it('opens on the twenty most recent, and shows nothing until they are read', async () => {
        held = Array.from({ length: 25 }, (_, i) => closed(25 - i));
        const first = deferred<ClosedListReadLike>();
        scripted.push(first.promise);

        openContinue();
        await flush();
        expect(continueList.open).toBe(true);
        expect(continueList.reading).toBe(true);
        expect(continueList.lines).toEqual([]);
        expect(asked).toEqual([{ limit: 20, after: undefined }]);

        first.resolve(answer({ limit: 20 }));
        await flush();
        expect(continueList.reading).toBe(false);
        expect(continueList.lines.map((l) => l.id)).toEqual(held.slice(0, 20).map((s) => s.id));
        expect(continueList.more).toBe(true);
        expect(continueChoosable()).toBe(true);
    });

    it('"Show more" asks for what follows the last line, in the list it is of, and adds each session once', async () => {
        held = Array.from({ length: 45 }, (_, i) => closed(45 - i));
        openContinue();
        await flush();

        showMoreContinue();
        await flush();
        expect(asked[1]).toEqual({ limit: 20, after: { sessionId: held[19].id, generation: 4 } });
        expect(continueList.lines).toHaveLength(40);
        expect(continueList.more).toBe(true);

        showMoreContinue();
        await flush();
        expect(continueList.lines.map((l) => l.id)).toEqual(held.map((s) => s.id));
        expect(new Set(continueList.lines.map((l) => l.id)).size).toBe(45);
        expect(continueList.more).toBe(false);

        // With no more there is nothing to ask for.
        showMoreContinue();
        await flush();
        expect(asked).toHaveLength(3);
    });

    it('a further page refused as stale reads the list again from its start; two lists are never joined', async () => {
        held = Array.from({ length: 45 }, (_, i) => closed(45 - i));
        openContinue();
        await flush();

        // A session is closed meanwhile: another list, at another generation.
        generation = 6;
        held = [closed(99), ...held];
        showMoreContinue();
        await flush();

        // The page it asked for was refused, and the list was read from its
        // start, as far as the operator had asked to see.
        expect(asked[1]).toEqual({ limit: 20, after: { sessionId: id(26), generation: 4 } });
        expect(asked[2]).toEqual({ limit: 40, after: undefined });
        expect(continueList.lines.map((l) => l.id)).toEqual(held.slice(0, 40).map((s) => s.id));
        expect(continueList.error).toBe('');
        // It is of a newer generation than the state shown: the state is read,
        // and nothing can be chosen until it is drawn.
        expect(stateReads).toBe(1);
        expect(continueChoosable()).toBe(false);
        drawn(6);
        await flush();
        expect(continueChoosable()).toBe(true);
        expect(asked.filter((q) => q.limit !== 1)).toHaveLength(3);
    });

    it('the first read fails: the dialog says so, with no lines, and "Try again" reads it', async () => {
        scripted.push({ kind: 'error', message: 'Cannot reach the daemon.' });
        openContinue();
        await flush();

        expect(continueList.lines).toEqual([]);
        expect(continueList.error).toBe('Cannot reach the daemon.');
        expect(continueList.reading).toBe(false);

        retryContinue();
        await flush();
        expect(continueList.error).toBe('');
        expect(continueList.lines).toHaveLength(3);
    });

    it('opens afresh each time: how far "Show more" had gone is not kept', async () => {
        held = Array.from({ length: 45 }, (_, i) => closed(45 - i));
        openContinue();
        await flush();
        showMoreContinue();
        await flush();
        expect(continueList.lines).toHaveLength(40);

        closeContinue();
        expect(continueList.open).toBe(false);
        expect(continueList.lines).toEqual([]);

        const again = deferred<ClosedListReadLike>();
        scripted.push(again.promise);
        openContinue();
        await flush();
        expect(continueList.lines).toEqual([]); // nothing from the opening before
        expect(asked.at(-1)).toEqual({ limit: 20, after: undefined });
        again.resolve(answer({ limit: 20 }));
        await flush();
        expect(continueList.lines).toHaveLength(20);
    });

    it('an answer to a read sent for an earlier opening supplies no lines and no further pages', async () => {
        held = Array.from({ length: 45 }, (_, i) => closed(45 - i, 5));
        openContinue();
        await flush();
        const late = deferred<ClosedListReadLike>();
        scripted.push(late.promise);
        showMoreContinue();
        await flush();
        const lateAnswer = answer({ limit: 20, after: { sessionId: held[19].id, generation: 4 } });

        // Closed; a contact of the newest session is deleted; opened again, at
        // the same archive and generation.
        closeContinue();
        held[0] = { ...held[0], contactCount: 4 };
        continueContactDeleted();
        openContinue();
        await flush();
        expect(continueList.lines).toHaveLength(20);
        expect(continueList.lines[0].contactCount).toBe(4);

        late.resolve(lateAnswer);
        await flush();
        expect(continueList.lines).toHaveLength(20);
        expect(continueList.lines[0].contactCount).toBe(4);
        expect(continueList.more).toBe(true);
    });

    it('a contact deleted while the list is shown: it is read again as far as it was shown', async () => {
        held = Array.from({ length: 45 }, (_, i) => closed(45 - i, 5));
        openContinue();
        await flush();
        showMoreContinue();
        await flush();
        asked.length = 0;

        held[30] = { ...held[30], contactCount: 0 };
        continueContactDeleted();
        await flush();

        expect(asked).toEqual([{ limit: 40, after: undefined }]);
        expect(continueList.lines).toHaveLength(40);
        expect(continueList.lines[30].contactCount).toBe(0);
        expect(continueList.more).toBe(true);
    });

    it('more lines than one read can return are read again page by page, and replaced only when all are read', async () => {
        held = Array.from({ length: 1001 }, (_, i) => closed(1001 - i, 1));
        openContinue();
        await flush();
        for (let i = 0; i < 50; i++) {
            showMoreContinue();
            await flush();
        }
        expect(continueList.lines).toHaveLength(1001);
        expect(continueList.more).toBe(false);
        asked.length = 0;

        // The only contact of the last session is deleted. It is still a session.
        held[1000] = { ...held[1000], contactCount: 0 };
        const second = deferred<ClosedListReadLike>();
        scripted.push(undefined, second.promise);
        continueContactDeleted();
        await flush();

        expect(asked).toEqual([
            { limit: 1000, after: undefined },
            { limit: 1, after: { sessionId: held[999].id, generation: 4 } },
        ]);
        // The first page has answered and the second has not: the lines on
        // screen are still the ones from before, all of them.
        expect(continueList.lines).toHaveLength(1001);
        expect(continueList.lines[1000].contactCount).toBe(1);

        second.resolve(answer(asked[1]));
        await flush();
        expect(continueList.lines).toHaveLength(1001);
        expect(continueList.lines.filter((l) => l.id === held[1000].id)).toHaveLength(1);
        expect(continueList.lines[1000].contactCount).toBe(0);
        expect(continueList.error).toBe('');
    });

    it('reading again stops when the daemon says there are no more, even if the list is shorter now', async () => {
        held = Array.from({ length: 45 }, (_, i) => closed(45 - i));
        openContinue();
        await flush();
        showMoreContinue();
        await flush();
        asked.length = 0;

        held = held.slice(0, 12);
        continueContactDeleted();
        await flush();

        expect(asked).toEqual([{ limit: 40, after: undefined }]);
        expect(continueList.lines).toHaveLength(12);
        expect(continueList.more).toBe(false);
    });

    it('a page of that reading fails: the lines shown before stay, and the dialog says they may be out of date', async () => {
        held = Array.from({ length: 1001 }, (_, i) => closed(1001 - i, 1));
        openContinue();
        await flush();
        for (let i = 0; i < 50; i++) {
            showMoreContinue();
            await flush();
        }
        asked.length = 0;

        scripted.push(undefined, { kind: 'error', message: 'Cannot reach the daemon.' });
        held[0] = { ...held[0], contactCount: 0 };
        continueContactDeleted();
        await flush();

        expect(continueList.lines).toHaveLength(1001);
        expect(continueList.lines[0].contactCount).toBe(1); // as before: not half of each
        expect(continueList.error).toBe('Cannot reach the daemon.');

        retryContinue();
        await flush();
        expect(continueList.error).toBe('');
        expect(continueList.lines).toHaveLength(1001);
        expect(continueList.lines[0].contactCount).toBe(0);
    });

    it('"Show more" after a reading again that failed does not bless the lines it left (codex-290)', async () => {
        held = Array.from({ length: 21 }, (_, i) => closed(21 - i, 1));
        openContinue();
        await flush();
        expect(continueList.lines).toHaveLength(20);
        // The only contact of the newest session is deleted; reading again fails.
        held[0] = { ...held[0], contactCount: 0 };
        scripted.push({ kind: 'error', message: 'Cannot reach the daemon.' });
        continueContactDeleted();
        await flush();
        expect(continueList.lines[0].contactCount).toBe(1);
        expect(continueList.error).toBe('Cannot reach the daemon.');
        asked.length = 0;

        showMoreContinue();
        await flush();

        // It did not append to lines known to be out of date and call them
        // good: the whole of what is shown was read again, and one page more.
        expect(asked).toEqual([{ limit: 40, after: undefined }]);
        expect(continueList.lines).toHaveLength(21);
        expect(continueList.lines[0].contactCount).toBe(0);
        expect(continueList.error).toBe('');
    });

    it('a list that goes on changing: the reading starts again three times, then says so; "Try again" reads it', async () => {
        held = Array.from({ length: 1001 }, (_, i) => closed(1001 - i, 1));
        openContinue();
        await flush();
        for (let i = 0; i < 50; i++) {
            showMoreContinue();
            await flush();
        }
        asked.length = 0;

        // Every second page is refused as stale: the first reading and three more.
        for (let i = 0; i < 4; i++) scripted.push(undefined, { kind: 'stale' });
        continueContactDeleted();
        await flush();

        expect(asked.map((q) => q.limit)).toEqual([1000, 1, 1000, 1, 1000, 1, 1000, 1]);
        expect(continueList.lines).toHaveLength(1001); // the lines shown before stay
        expect(continueList.error).toBe('The list kept changing while it was being read.');
        expect(continueList.reading).toBe(false);

        held[0] = { ...held[0], contactCount: 0 };
        retryContinue();
        await flush();
        expect(continueList.error).toBe('');
        expect(continueList.lines[0].contactCount).toBe(0);
    });

    it('a page of that reading refused as stale starts it again', async () => {
        held = Array.from({ length: 1001 }, (_, i) => closed(1001 - i, 1));
        openContinue();
        await flush();
        for (let i = 0; i < 50; i++) {
            showMoreContinue();
            await flush();
        }
        asked.length = 0;

        scripted.push(undefined, { kind: 'stale' });
        continueContactDeleted();
        await flush();

        expect(asked.map((q) => q.limit)).toEqual([1000, 1, 1000, 1]);
        expect(continueList.lines).toHaveLength(1001);
        expect(continueList.error).toBe('');
    });

    it('a contact deleted while a read is out is followed by one more reading', async () => {
        held = [closed(3, 5), closed(2, 5), closed(1, 5)];
        const first = deferred<ClosedListReadLike>();
        scripted.push(first.promise);
        openContinue();
        await flush();
        const before = answer({ limit: 20 });

        held[1] = { ...held[1], contactCount: 4 };
        continueContactDeleted();
        continueContactDeleted();
        await flush();
        expect(asked).toHaveLength(1);

        first.resolve(before);
        await flush();
        expect(asked).toHaveLength(2);
        expect(continueList.lines[1].contactCount).toBe(4);
    });

    it('a contact deleted with the dialog closed reads nothing', async () => {
        continueContactDeleted();
        await flush();
        expect(asked).toEqual([]);
    });

    it('a state of another generation drawn while it is open: the list is read again', async () => {
        openContinue();
        await flush();
        expect(continueChoosable()).toBe(true);
        asked.length = 0;

        generation = 6;
        held = [closed(9), ...held];
        drawn(6);
        expect(continueChoosable()).toBe(false); // the lines shown are of the list before
        await flush();

        expect(asked.filter((q) => q.limit === 20)).toEqual([{ limit: 20, after: undefined }]);
        expect(continueList.lines.map((l) => l.id)).toEqual(held.map((s) => s.id));
        expect(continueChoosable()).toBe(true);
    });

    it('an answer for another archive is not drawn: the dialog says the list could not be read', async () => {
        archive = OTHER;
        openContinue();
        await flush();
        expect(continueList.lines).toEqual([]);
        expect(continueList.error).not.toBe('');
        expect(others).toEqual([OTHER]);
    });
});

describe('a choice', () => {
    beforeEach(async () => {
        drawn(4);
        await flush();
        openContinue();
        await flush();
        asked.length = 0;
    });

    it("is sent once, with the line's session and the LIST's generation, and the dialog closes", async () => {
        await chooseContinue(id(2));

        expect(sent).toEqual([[4, id(2)]]);
        expect(continueList.open).toBe(false);
        expect(continueList.lines).toEqual([]);
    });

    it('is not sent for a session that is not a line of the list shown', async () => {
        await chooseContinue(id(77));
        expect(sent).toEqual([]);
        expect(continueList.open).toBe(true);
    });

    it('is not sent once the page is bound to another archive than the list is of', async () => {
        bound = OTHER;
        await chooseContinue(id(2));
        expect(sent).toEqual([]);
    });

    it('is not sent while the list is of another generation than the state shown', async () => {
        // The session moved on; the list on screen is of generation 4, and
        // the read made for the new state FAILS. Nothing is being read, so
        // the generation alone is what keeps the line from being chosen.
        scripted.push(
            { kind: 'error', message: 'Cannot reach the daemon.' },
            { kind: 'error', message: 'Cannot reach the daemon.' }
        );
        drawn(6);
        await flush();
        expect(continueList.reading).toBe(false);
        expect(continueList.lines).toHaveLength(3); // the lines stay
        expect(continueList.held).toBe('moved');

        await chooseContinue(id(2));

        expect(sent).toEqual([]);
        expect(continueList.open).toBe(true);
    });

    it('is not sent while the list is being read again', async () => {
        const again = deferred<ClosedListReadLike>();
        scripted.push(again.promise);
        continueContactDeleted();
        await flush();
        expect(continueChoosable()).toBe(false);
        await chooseContinue(id(2));
        expect(sent).toEqual([]);
        again.resolve(answer({ limit: 20 }));
        await flush();
        expect(continueChoosable()).toBe(true);
    });

    it.each(['stale', 'failed'] as const)(
        'that ends "%s" is followed by asking again whether there is anything to continue',
        async (end) => {
            sendEnds.push(end);
            await chooseContinue(id(2));
            await flush();
            expect(asked).toEqual([{ limit: 1, after: undefined }]);
        }
    );

    it('that is applied asks nothing by itself: the state drawn is what asks', async () => {
        await chooseContinue(id(2));
        await flush();
        expect(asked).toEqual([]);
    });
});
