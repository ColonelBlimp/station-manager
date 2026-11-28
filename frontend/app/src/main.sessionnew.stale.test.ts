// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria E3, F8; the
// operator's ruling 2 of 2026-11-27): the real main.ts with only the transport
// stubbed. Another window changed the session and this page had not heard:
// New is refused as stale.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import {
    S_OPEN,
    S_CLOSED,
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
    news,
    continues,
    boot,
    showPanel,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const daemon = daemonWith({
    [STATE]: ok(sessionState(S_OPEN, 4)),
    [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
    [CLOSED]: ok(closedList(4, [closedLine(S_CLOSED)])),
});
const calls = (): string[] => session.qsos.map((q) => q.callsign);
const STALE = 'The session changed before this could be done. Your choice was not applied.';

describe('main.ts: New pressed on a state the session has moved on from', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'is refused: sent once, nothing is opened by the page, the state returned is drawn, and the panel says the choice was not applied',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const press = await screen.findByRole('button', { name: 'New session' }, WAIT);

            // Another window continued the earlier session: generation 6.
            daemon.routes[NEW] = {
                status: 409,
                body: {
                    code: 'operating_session_stale',
                    message: 'the operating session has changed',
                    operating_session: sessionState(S_CLOSED, 6),
                },
            };
            daemon.routes[STATE] = ok(sessionState(S_CLOSED, 6));
            daemon.routes[ROWS] = ok(members(S_CLOSED, 6, [member(2, 'K2EARLIER')]));
            daemon.routes[CLOSED] = ok(closedList(6, [closedLine(S_OPEN, { contact_count: 1 })]));
            await fireEvent.click(press);

            await vi.waitFor(() => expect(calls()).toEqual(['K2EARLIER']), WAIT);
            expect(session.sessionId).toBe(S_CLOSED);
            expect((await screen.findByTestId('session-control-error')).textContent.trim()).toBe(
                STALE
            );
            // Sent once at the generation it was pressed on; never again at the newer one.
            expect(news(daemon)).toHaveLength(1);
            expect(JSON.parse(news(daemon)[0].body ?? '')).toEqual({ generation: 4 });
            expect(continues(daemon)).toEqual([]);
        },
        TEST_LIMIT
    );
});
