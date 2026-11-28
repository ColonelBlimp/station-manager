// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria F1, F3; after
// codex-286 to codex-288): the real main.ts with only the transport stubbed.
// "Show more", a contact deleted while a page is out, and the stream
// reopening with the list on screen.
//
// A closed session's count changes with no generation moved, so the page can
// only learn of it by reading again. The nearest confusable outcome is a list
// that looks complete and holds a count from before the deletion. So the
// daemon's list here is a model the test changes, each page is answered from
// it as asked, and the test reads which pages the page asked for.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import {
    S_OPEN,
    STATE,
    ROWS,
    CLOSED,
    sessionState,
    member,
    members,
    closedLine,
    closedList,
    ok,
    daemonWith,
    listReads,
    deferred,
    boot,
    showPanel,
    openList,
    event,
    reconnect,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const id = (n: number): string => `0190${String(n).padStart(4, '0')}-0000-7000-8000-000000000000`;
// 45 closed sessions, the most recent first; each has 5 contacts.
const counts = new Map<string, number>();
const order = Array.from({ length: 45 }, (_, i) => id(45 - i));
for (const s of order) counts.set(s, 5);

// A page of the list, answered from the model as the daemon answers it.
function page(url: string): unknown {
    const q = new URLSearchParams(url.split('?')[1] ?? '');
    const limit = Number(q.get('limit') ?? '20');
    const after = q.get('after');
    const from = after === null ? 0 : order.indexOf(after) + 1;
    const sessions = order
        .slice(from, from + limit)
        .map((s) => closedLine(s, { contact_count: counts.get(s) }));
    return closedList(4, sessions, { more: from + limit < order.length });
}

const daemon = daemonWith({
    [STATE]: ok(sessionState(S_OPEN, 4)),
    [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
    [CLOSED]: ok(page),
});
const lines = (): string[] =>
    screen.getAllByRole('button', { name: /UTC ·/ }).map((b) => b.textContent.trim());
const withCount = (n: number): string => `2026-11-20 09:15 to 11:42 UTC · ${n} contacts`;

describe("main.ts: Continue's list is read again when what it shows may have changed", () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'a contact deleted while "Show more" is out, and the stream reopening: each is followed by reading the list from its start, as far as it is shown',
        async () => {
            await vi.waitFor(() => expect(session.qsos).toHaveLength(1), WAIT);
            await showPanel();
            await openList();
            expect(lines()).toHaveLength(20);
            const asked = listReads(daemon).length;

            // "Show more" is asked for and held. While it is out, a contact of
            // the newest closed session is deleted, and the page hears of it.
            const held = deferred();
            daemon.routes[CLOSED] = { status: 200, body: page, hold: held.promise };
            await fireEvent.click(screen.getByRole('button', { name: 'Show more' }));
            await vi.waitFor(() => expect(listReads(daemon)).toHaveLength(asked + 1), WAIT);
            counts.set(order[0], 4);
            event('qso.deleted', { qso_uuid: 'x', logbook_id: 1 });
            daemon.routes[CLOSED] = ok(page);
            held.release();

            // The page asked for follows the last line, in the list shown; and
            // then the whole of what is shown is read again from the start.
            await vi.waitFor(() => expect(lines()[0]).toBe(withCount(4)), WAIT);
            expect(listReads(daemon).slice(asked)).toEqual([
                `limit=20&after=${order[19]}&generation=4`,
                'limit=40',
            ]);
            expect(lines()).toHaveLength(40);
            expect(new Set(order.slice(0, 40)).size).toBe(40);

            // The stream drops and reopens: a deletion made meanwhile was
            // announced to nobody. The list on screen is read again.
            counts.set(order[39], 0);
            const before = listReads(daemon).length;
            await reconnect();
            await vi.waitFor(
                () => expect(lines()[39]).toBe('2026-11-20 09:15 to 11:42 UTC · 0 contacts'),
                WAIT
            );
            expect(listReads(daemon).slice(before)).toContain('limit=40');
            // A session with no contact left is still a session, listed once.
            expect(lines()).toHaveLength(40);
        },
        TEST_LIMIT
    );
});
