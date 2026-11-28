// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria F6, F8; the
// operator's ruling 2 of 2026-11-27): the real main.ts with only the transport
// stubbed. The session moved on while the list was open, and the page had not
// heard: the choice is refused as stale.
//
// Nearest confusable outcomes: the page sends the choice again at the newer
// generation; or it redraws silently, so the control seems to have done
// nothing. So the test counts what was sent, and reads what the panel says.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import { continueList } from './lib/operate/continueList.svelte';
import {
    S_OPEN,
    S_CLOSED,
    S_NEW,
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
const STALE = 'The session changed before this could be done. Your choice was not applied.';

describe('main.ts: Continue chosen from a list the session has moved on from', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'is refused: sent once, the state returned is drawn, the panel says the choice was not applied, and the list is read again',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const [line] = await openList();

            // Another window pressed New: the daemon is at generation 5, with
            // another session open, and this page has not heard.
            daemon.routes[CONTINUE] = {
                status: 409,
                body: {
                    code: 'operating_session_stale',
                    message: 'the operating session has changed',
                    operating_session: sessionState(S_NEW, 5),
                },
            };
            daemon.routes[STATE] = ok(sessionState(S_NEW, 5));
            daemon.routes[ROWS] = ok(members(S_NEW, 5, [member(3, 'N3OTHER')]));
            daemon.routes[CLOSED] = ok(
                closedList(5, [closedLine(S_OPEN, { contact_count: 1 }), closedLine(S_CLOSED)])
            );
            const asked = listReads(daemon).length;
            await fireEvent.click(line);

            // The state the refusal carried is drawn, with its rows.
            await vi.waitFor(() => expect(calls()).toEqual(['N3OTHER']), WAIT);
            expect(session.sessionId).toBe(S_NEW);
            // The panel says so, in the ruled words.
            expect((await screen.findByTestId('session-control-error')).textContent.trim()).toBe(
                STALE
            );
            // Sent once, at the generation of the list; never again at the newer one.
            expect(continues(daemon)).toHaveLength(1);
            expect(JSON.parse(continues(daemon)[0].body ?? '')).toEqual({
                generation: 4,
                session_id: S_CLOSED,
            });
            expect(news(daemon)).toEqual([]);
            // The dialog is closed: a new choice is needed, from a list read again.
            expect(continueList.open).toBe(false);
            await vi.waitFor(
                () => expect(listReads(daemon).slice(asked)).toContain('limit=1'),
                WAIT
            );
            const lines = await openList();
            expect(lines.map((l) => l.textContent.trim())).toEqual([
                '2026-11-20 09:15 to 11:42 UTC · 1 contact',
                '2026-11-20 09:15 to 11:42 UTC · 37 contacts',
            ]);
            // A new choice, from the list read again, carries THAT list's generation.
            await fireEvent.click(lines[1]);
            await vi.waitFor(() => expect(continues(daemon)).toHaveLength(2), WAIT);
            expect(JSON.parse(continues(daemon)[1].body ?? '')).toEqual({
                generation: 5,
                session_id: S_CLOSED,
            });
        },
        TEST_LIMIT
    );
});
