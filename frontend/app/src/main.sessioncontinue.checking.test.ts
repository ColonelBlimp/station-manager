// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (codex-297): the real
// main.ts with only the transport stubbed. While the page is checking which
// archive the daemon serves, a guarded request is refused before it is sent.
// A line of Continue's list pressed then would close the dialog and send
// nothing. So no line is pressable while the check is out, the dialog stays,
// and the lines come back by themselves when the archive is confirmed.
//
// The check here is the one a reconnect starts; its identity read is held, so
// the test is looking at the page DURING the check and not after it.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { archivesState } from './lib/config/archives.svelte';
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
    continues,
    deferred,
    boot,
    showPanel,
    openList,
    reconnect,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const daemon = daemonWith({
    [STATE]: ok(sessionState(S_OPEN, 4)),
    [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
    [CLOSED]: ok(closedList(4, [closedLine(S_CLOSED)])),
});
const calls = (): string[] => session.qsos.map((q) => q.callsign);
const line = (): HTMLElement => screen.getAllByRole('button', { name: /UTC ·/ })[0];

describe('main.ts: Continue while the page checks its archive', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'no line can be pressed during the check, the dialog stays, and the lines come back when the archive is confirmed',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            await openList();
            await vi.waitFor(() => expect(line()).toBeEnabled(), WAIT);

            // The event stream reconnects: the page checks which archive the
            // daemon serves, and the identity read is held.
            const identity = deferred();
            daemon.versionHold = identity.promise;
            await reconnect();
            await vi.waitFor(() => expect(archivesState.verifying).toBe(true), WAIT);
            // The reconnect also reads the list again, and a line cannot be
            // pressed while that is out. Wait until it is over, so that what
            // holds the line now is the check and nothing else: the list is
            // read, it is the list, and no control is out.
            await vi.waitFor(() => expect(continueList.reading).toBe(false), WAIT);
            await vi.waitFor(() => expect(continueList.held).toBe(''), WAIT);
            expect(session.control).toBe('');
            expect(archivesState.verifying).toBe(true);

            await vi.waitFor(() => expect(line()).toBeDisabled(), WAIT);
            await fireEvent.click(line());
            expect(continueList.open).toBe(true);
            expect(continues(daemon)).toEqual([]);
            expect(session.control).toBe('');

            // The check confirms the page's own archive.
            daemon.versionHold = undefined;
            identity.release();
            await vi.waitFor(() => expect(archivesState.verifying).toBe(false), WAIT);
            await vi.waitFor(() => expect(line()).toBeEnabled(), WAIT);

            daemon.routes[CONTINUE] = ok(sessionState(S_CLOSED, 6));
            daemon.routes[STATE] = ok(sessionState(S_CLOSED, 6));
            daemon.routes[ROWS] = ok(members(S_CLOSED, 6, [member(2, 'K2EARLIER')]));
            await fireEvent.click(line());
            await vi.waitFor(() => expect(continues(daemon)).toHaveLength(1), WAIT);
            expect(JSON.parse(continues(daemon)[0].body ?? '')).toEqual({
                generation: 4,
                session_id: S_CLOSED,
            });
        },
        TEST_LIMIT
    );
});
