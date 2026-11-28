// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria F, D3): the real
// main.ts with only the transport stubbed. The operator opens Continue's list,
// chooses a session, and the panel becomes that session's.
//
// The nearest confusable outcome is a panel that looks right because the page
// changed itself. So everything the page shows afterwards is something only
// the daemon's ANSWERS hold: the state the control returned (another session,
// at a later generation, with a sitting that started later) and the rows the
// daemon then answers for it. And the choice is judged on what was SENT: once,
// with the generation of the list and the archive the page is bound to.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import { continueList } from './lib/operate/continueList.svelte';
import {
    ARCHIVE,
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
    news,
    stateReads,
    boot,
    showPanel,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const SENTENCE = 'Contacts stay in the session they were logged in.';

const daemon = daemonWith({
    [STATE]: ok(sessionState(S_OPEN, 4)),
    [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
    [CLOSED]: ok(closedList(4, [closedLine(S_CLOSED)])),
});

const calls = (): string[] => session.qsos.map((q) => q.callsign);

describe('main.ts: Continue from the list', () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => {
        vi.unstubAllGlobals();
    });

    it(
        'the chosen session is sent once at the generation of the list, and the panel becomes what the daemon answers',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);

            // The control is offered, with the ruled sentence beside it.
            await showPanel();
            const open = await screen.findByRole('button', { name: 'Continue…' }, WAIT);
            expect(screen.getByText(SENTENCE)).toBeInTheDocument();
            // Whether there is anything to continue was asked, and no lines were read for it.
            expect(listReads(daemon).every((q) => q === 'limit=1')).toBe(true);
            const asked = listReads(daemon).length;

            // The list is read when the dialog opens.
            await fireEvent.click(open);
            const line = await screen.findByRole(
                'button',
                { name: '2026-11-20 09:15 to 11:42 UTC · 37 contacts' },
                WAIT
            );
            expect(listReads(daemon).slice(asked)).toEqual(['limit=20']);

            // The daemon applies it: the closed session is open again at a
            // later generation, in a sitting that starts now, with its own
            // contact; and the session just left is in the list.
            daemon.routes[CONTINUE] = ok(
                sessionState(S_CLOSED, 6, {
                    session: {
                        id: S_CLOSED,
                        logbook_uuid: 'x',
                        opened_at: '2026-11-20T09:15:00Z',
                        sitting_started_at: new Date().toISOString(),
                        has_contacts: true,
                        contact_count: 37,
                        held_by_you: true,
                    },
                })
            );
            daemon.routes[STATE] = daemon.routes[CONTINUE];
            daemon.routes[ROWS] = ok(members(S_CLOSED, 6, [member(2, 'K2EARLIER')]));
            daemon.routes[CLOSED] = ok(closedList(6, [closedLine(S_OPEN, { contact_count: 1 })]));
            const readsBefore = stateReads(daemon);
            await fireEvent.click(line);

            await vi.waitFor(() => expect(calls()).toEqual(['K2EARLIER']), WAIT);
            expect(session.sessionId).toBe(S_CLOSED);

            // What was sent: once, the line's session, the LIST's generation,
            // for the archive the page is bound to. And no New.
            expect(continues(daemon)).toHaveLength(1);
            expect(JSON.parse(continues(daemon)[0].body ?? '')).toEqual({
                generation: 4,
                session_id: S_CLOSED,
            });
            expect(continues(daemon)[0].archive).toBe(ARCHIVE);
            expect(news(daemon)).toEqual([]);
            // The page drew the state the control returned: it read nothing to find out.
            expect(stateReads(daemon)).toBe(readsBefore);

            // D3: the clock starts again with the new sitting.
            const clock = await screen.findByTestId('session-timer');
            await vi.waitFor(() => expect(clock.textContent).toMatch(/^00:00:\d{2}$/), WAIT);

            // The dialog is closed, and nothing went wrong to say.
            expect(continueList.open).toBe(false);
            expect(screen.queryByTestId('session-control-error')).toBeNull();

            // The session just left is offered: the list was asked again for
            // the new generation, and opening it shows that session.
            await vi.waitFor(() => expect(listReads(daemon).at(-1)).toBe('limit=1'), WAIT);
            await fireEvent.click(await screen.findByRole('button', { name: 'Continue…' }, WAIT));
            expect(
                await screen.findByRole(
                    'button',
                    { name: '2026-11-20 09:15 to 11:42 UTC · 1 contact' },
                    WAIT
                )
            ).toBeEnabled();
        },
        TEST_LIMIT
    );
});
