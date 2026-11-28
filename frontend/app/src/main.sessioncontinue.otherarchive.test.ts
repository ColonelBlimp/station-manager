// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (codex-290, finding 2): the
// real main.ts with only the transport stubbed. Continue fails, and the read
// that should say what it did is answered for ANOTHER archive.
//
// That answer says nothing of which session is open in the archive the page
// is bound to. The page reports it (its archive check starts), and the check
// here finds the page's own archive again, which still says nothing of the
// session. The nearest confusable outcome is the one Codex found: the panel's
// controls come back as if the failed control had been settled. So the test
// requires them held after the archive check has run, and counts what was sent.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import { continueList } from './lib/operate/continueList.svelte';
import { recovery } from './lib/recovery.svelte';
import {
    OTHER_ARCHIVE,
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
    news,
    sentTo,
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
const identityReads = (): number => sentTo(daemon, 'GET', '/v1/version').length;

describe('main.ts: Continue fails, and the read that follows is answered for another archive', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'the session stays not known: no control can be sent until a state for this archive is drawn',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const [line] = await openList();

            daemon.routes[CONTINUE] = {
                status: 503,
                body: { code: 'unavailable', message: 'The daemon is busy.' },
            };
            // The read that finds out is answered by a daemon serving another archive.
            daemon.routes[STATE] = ok(sessionState(S_OPEN, 4, { archive_id: OTHER_ARCHIVE }));
            const identityBefore = identityReads();
            await fireEvent.click(line);

            const said = await screen.findByTestId('session-unknown', {}, WAIT);
            expect(said.textContent.replace(/\s+/g, ' ').trim()).toMatch(
                /^The daemon is busy\. The session could not be read\./
            );
            // The page's archive check started, and found its own archive again.
            await vi.waitFor(() => expect(identityReads()).toBeGreaterThan(identityBefore), WAIT);
            // That proves nothing of the session: the controls are still held.
            expect(screen.getByRole('button', { name: 'Continue…' })).toBeDisabled();
            expect(continueList.open).toBe(false);
            expect(session.control).toBe('unknown');
            expect(recovery.unknown).toBe(true);
            expect(continues(daemon)).toHaveLength(1);
            expect(news(daemon)).toEqual([]);
            expect(session.sessionId).toBe(S_OPEN);
            expect(calls()).toEqual(['G4OPEN']);

            // A state for THIS archive, read since: known again.
            daemon.routes[STATE] = ok(sessionState(S_OPEN, 4));
            await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
            await vi.waitFor(
                () => expect(screen.queryByTestId('session-unknown')).toBeNull(),
                WAIT
            );
            await vi.waitFor(
                () => expect(screen.getByRole('button', { name: 'Continue…' })).toBeEnabled(),
                WAIT
            );
            expect(continues(daemon)).toHaveLength(1);
        },
        TEST_LIMIT
    );
});
