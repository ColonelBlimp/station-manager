// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria F7, F8): the
// real main.ts with only the transport stubbed. The session chosen can no
// longer be continued (another logbook is now the default), and the daemon
// says so.
//
// Nearest confusable outcome: the panel goes on offering a list that is no
// longer the one the daemon holds. So the test counts the reads of the list
// after the refusal, and what the page then offers.
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
    listReads,
    continues,
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

describe('main.ts: Continue for a session that can no longer be continued', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        "is refused in the daemon's words, the current session stays, and the list is asked for again",
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const [line] = await openList();

            daemon.routes[CONTINUE] = {
                status: 409,
                body: {
                    code: 'operating_session_ineligible',
                    message: 'That session belongs to another logbook than the default.',
                },
            };
            // The list is now of the other logbook, which has no closed session.
            daemon.routes[CLOSED] = ok(closedList(4, []));
            const asked = listReads(daemon).length;
            await fireEvent.click(line);

            expect((await screen.findByTestId('session-control-error')).textContent.trim()).toBe(
                'That session belongs to another logbook than the default.'
            );
            // The session open before is still the one shown, with its rows.
            expect(session.sessionId).toBe(S_OPEN);
            expect(calls()).toEqual(['G4OPEN']);
            expect(continues(daemon)).toHaveLength(1);
            expect(continueList.open).toBe(false);
            // Whether there is anything to continue is asked again, though no
            // generation moved; and there is not, so the control is not offered.
            await vi.waitFor(
                () => expect(listReads(daemon).slice(asked)).toEqual(['limit=1']),
                WAIT
            );
            await vi.waitFor(
                () => expect(screen.queryByRole('button', { name: 'Continue…' })).toBeNull(),
                WAIT
            );
        },
        TEST_LIMIT
    );
});
