// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria E1 to E3, D3;
// the operator's ruling 2 of 2026-11-25): the real main.ts with only the
// transport stubbed. The operator presses "New session".
//
// The nearest confusable outcome is a panel that looks right because the page
// emptied itself. So what the page shows afterwards is what only the daemon's
// ANSWERS hold: another session, at a later generation, whose sitting started
// later, with the rows the daemon answers for it; and the session just closed
// is offered because the daemon's list says so. The press is judged on what
// was SENT: once, at the generation of the state shown, with no question first.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import {
    ARCHIVE,
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
    // Nothing to continue yet: New is offered all the same.
    [CLOSED]: ok(closedList(4, [])),
});
const calls = (): string[] => session.qsos.map((q) => q.callsign);

describe('main.ts: New session from the panel', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'is sent once at the generation shown, with no question; the panel becomes the new session the daemon answers, and the one closed is offered',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const press = await screen.findByRole('button', { name: 'New session' }, WAIT);
            expect(screen.queryByRole('button', { name: 'Continue…' })).toBeNull();

            const sitting = new Date().toISOString();
            daemon.routes[NEW] = ok(
                sessionState(S_NEW, 5, {
                    session: {
                        id: S_NEW,
                        logbook_uuid: 'x',
                        opened_at: sitting,
                        sitting_started_at: sitting,
                        has_contacts: false,
                        contact_count: 0,
                        held_by_you: true,
                    },
                })
            );
            daemon.routes[STATE] = daemon.routes[NEW];
            daemon.routes[ROWS] = ok(members(S_NEW, 5, []));
            daemon.routes[CLOSED] = ok(closedList(5, [closedLine(S_OPEN, { contact_count: 1 })]));
            const reads = stateReads(daemon);
            await fireEvent.click(press);

            await vi.waitFor(() => expect(session.sessionId).toBe(S_NEW), WAIT);
            await vi.waitFor(() => expect(session.rows).toBe('ok'), WAIT);

            // What was sent: once, the generation of the state shown and
            // nothing else, for the bound archive. No Continue.
            expect(news(daemon)).toHaveLength(1);
            expect(JSON.parse(news(daemon)[0].body ?? '')).toEqual({ generation: 4 });
            expect(news(daemon)[0].archive).toBe(ARCHIVE);
            expect(continues(daemon)).toEqual([]);
            // The page drew the state the control returned: it read nothing to find out.
            expect(stateReads(daemon)).toBe(reads);

            // E1: the panel is empty, the count 0, the clock restarted, the start now.
            expect(calls()).toEqual([]);
            expect(screen.getByTestId('session-empty').textContent.trim()).toBe(
                'No QSOs logged this session.'
            );
            expect(document.body.textContent).not.toContain('G4OPEN');
            expect(document.body.textContent.replace(/\s+/g, ' ')).toContain('Session (0)');
            await vi.waitFor(
                () =>
                    expect(screen.getByTestId('session-timer').textContent).toMatch(
                        /^00:00:\d{2}$/
                    ),
                WAIT
            );
            expect(screen.getByTestId('session-started').textContent).toBe(
                `Started ${sitting.slice(11, 16)} UTC`
            );
            expect(screen.queryByTestId('session-control-error')).toBeNull();

            // E2: the session closed had a contact, so it is kept and offered.
            const lines = await openList();
            expect(lines.map((l) => l.textContent.trim())).toEqual([
                '2026-11-20 09:15 to 11:42 UTC · 1 contact',
            ]);
        },
        TEST_LIMIT
    );
});
