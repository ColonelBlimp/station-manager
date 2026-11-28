// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criterion E3): the real
// main.ts with only the transport stubbed. New's answer is lost, and the
// daemon HAD applied it.
//
// The nearest confusable outcome is a second New sent by the page, which
// would close the session just opened and open a third. So the test counts
// the News sent, and requires the session shown to be the one the daemon
// holds, read after the failure.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import {
    S_OPEN,
    S_NEW,
    STATE,
    ROWS,
    CLOSED,
    NEW,
    sessionState,
    member,
    members,
    closedLine,
    closedList,
    ok,
    daemonWith,
    stateReads,
    news,
    boot,
    showPanel,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const daemon = daemonWith({
    [STATE]: ok(sessionState(S_OPEN, 4)),
    [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
    [CLOSED]: ok(closedList(4, [])),
});
const calls = (): string[] => session.qsos.map((q) => q.callsign);

describe("main.ts: New's answer is lost, and the daemon had applied it", () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'the page says what went wrong, reads, shows the new session, and no second New is sent',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const press = await screen.findByRole('button', { name: 'New session' }, WAIT);

            daemon.routes[NEW] = {
                status: 503,
                body: { code: 'unavailable', message: 'The daemon is busy.' },
            };
            daemon.routes[STATE] = ok(sessionState(S_NEW, 5));
            daemon.routes[ROWS] = ok(members(S_NEW, 5, []));
            daemon.routes[CLOSED] = ok(closedList(5, [closedLine(S_OPEN, { contact_count: 1 })]));
            const reads = stateReads(daemon);
            await fireEvent.click(press);

            await vi.waitFor(() => expect(session.sessionId).toBe(S_NEW), WAIT);
            await vi.waitFor(() => expect(session.behind).toBe(false), WAIT);
            expect(calls()).toEqual([]);
            expect((await screen.findByTestId('session-control-error')).textContent.trim()).toBe(
                'The daemon is busy.'
            );
            expect(stateReads(daemon)).toBeGreaterThan(reads); // it read to find out
            expect(news(daemon)).toHaveLength(1);
            expect(screen.queryByTestId('session-unknown')).toBeNull();
            expect(screen.getByRole('button', { name: 'New session' })).toBeEnabled();

            // The operator presses again, on purpose: THAT one is sent, at the
            // generation now shown.
            daemon.routes[NEW] = ok(sessionState(S_NEW, 5));
            await fireEvent.click(screen.getByRole('button', { name: 'New session' }));
            await vi.waitFor(() => expect(news(daemon)).toHaveLength(2), WAIT);
            expect(JSON.parse(news(daemon)[1].body ?? '')).toEqual({ generation: 5 });
        },
        TEST_LIMIT
    );
});
