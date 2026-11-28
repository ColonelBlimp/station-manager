// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria E3, F7): the
// real main.ts with only the transport stubbed. Continue's answer is lost, and
// the daemon HAD applied it.
//
// Nearest confusable outcomes: the page sends it again (and so continues, or
// opens, a session twice); or the page shows the session it thinks it asked
// for instead of reading what the daemon holds.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import { continueList } from './lib/operate/continueList.svelte';
import {
    S_OPEN,
    S_CLOSED,
    STATE,
    ROWS,
    CLOSED,
    CONTINUE,
    sessionState,
    member,
    members,
    closedLine,
    closedList,
    ok,
    daemonWith,
    stateReads,
    continues,
    news,
    boot,
    showPanel,
    openList,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const daemon = daemonWith({
    [STATE]: ok(sessionState(S_OPEN, 4)),
    [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
    [CLOSED]: ok(closedList(4, [closedLine(S_CLOSED)])),
});
const calls = (): string[] => session.qsos.map((q) => q.callsign);

describe("main.ts: Continue's answer is lost, and the daemon had applied it", () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'the page says what went wrong, reads the state, shows the session the daemon holds, and sends nothing again',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const [line] = await openList();

            daemon.routes[CONTINUE] = {
                status: 503,
                body: { code: 'unavailable', message: 'The daemon is busy.' },
            };
            // What the daemon holds: the Continue was applied.
            daemon.routes[STATE] = ok(sessionState(S_CLOSED, 6));
            daemon.routes[ROWS] = ok(members(S_CLOSED, 6, [member(2, 'K2EARLIER')]));
            daemon.routes[CLOSED] = ok(closedList(6, [closedLine(S_OPEN, { contact_count: 1 })]));
            const reads = stateReads(daemon);
            await fireEvent.click(line);

            await vi.waitFor(() => expect(calls()).toEqual(['K2EARLIER']), WAIT);
            expect(session.sessionId).toBe(S_CLOSED);
            expect((await screen.findByTestId('session-control-error')).textContent.trim()).toBe(
                'The daemon is busy.'
            );
            expect(stateReads(daemon)).toBeGreaterThan(reads); // it read to find out
            expect(continues(daemon)).toHaveLength(1);
            expect(news(daemon)).toEqual([]);
            expect(continueList.open).toBe(false);
            // The session is known: nothing is held, and nothing asks to try again.
            await vi.waitFor(() => expect(session.behind).toBe(false), WAIT);
            expect(session.control).toBe('');
            expect(screen.queryByTestId('session-unknown')).toBeNull();
            expect(screen.getByRole('button', { name: 'Continue…' })).toBeEnabled();
        },
        TEST_LIMIT
    );
});
