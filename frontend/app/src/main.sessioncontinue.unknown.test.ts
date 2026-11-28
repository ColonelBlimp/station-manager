// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (the operator's ruling 1,
// 2026-11-27; codex-286 to codex-289): the real main.ts with only the
// transport stubbed, on the FT8 view. Continue fails, and the read that should
// say what it did fails too.
//
// The gate's rule for the recovery answer would take the workspace away here,
// and with it the FT8 view and its event stream, which is the app's presence
// for a run. The ruling: the workspace STAYS. So the test holds on to the FT8
// stream it found before the failure and requires the very same one, still
// open, at every step. The nearest confusable outcome on the other side is a
// workspace that stays because nothing was done at all: so the test also
// requires everything that acts on the session to be held (no control, the
// clock blank, the badge gone, the rows said to be as last read), a read sent
// BEFORE the failure not to give anything back, and "Try again" to send a read
// and no control.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { FakeEventSource } from './test/mainHarness';
import { session } from './lib/operate/session.svelte';
import { recovery } from './lib/recovery.svelte';
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
    stateReads,
    continues,
    news,
    deferred,
    boot,
    showPanel,
    openList,
    event,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const daemon = daemonWith(
    {
        [STATE]: ok(sessionState(S_OPEN, 4)),
        [ROWS]: ok(members(S_OPEN, 4, [member(1, 'G4OPEN')])),
        [CLOSED]: ok(closedList(4, [closedLine(S_CLOSED)])),
        // The FT8 view's claim is granted, so it opens its event stream: the
        // app's presence, which this test follows.
        'POST /v1/ft8/claim': ok({ mode: 'FT8' }),
    },
    true
);
const calls = (): string[] => session.qsos.map((q) => q.callsign);
const ft8Streams = (): FakeEventSource[] =>
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8'));
const workspace = (): Element | null => document.querySelector('.content-wrap');
const clock = (): string => screen.getByTestId('session-timer').textContent;
const badge = (): HTMLElement | null => screen.queryByTitle(/QSOs? this session/);
const settle = async (): Promise<void> => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
};

describe('main.ts, FT8 view: Continue fails and the read that follows fails too', () => {
    beforeAll(() => boot(daemon, 'ft8'), TEST_LIMIT);
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it(
        'the workspace and its FT8 stream stay; the session is said not to be known, and everything acting on it is held until a read sent since is drawn',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await vi.waitFor(() => expect(ft8Streams()).toHaveLength(1), WAIT);
            const stream = ft8Streams()[0];
            const sameStream = (): void => {
                expect(ft8Streams()).toEqual([stream]);
                expect(stream.readyState).not.toBe(2); // not closed
                expect(workspace()).not.toBeNull();
                expect(recovery.status).toBe('clear');
            };
            await showPanel();
            // Before anything goes wrong: the clock counts and the badge shows.
            await vi.waitFor(() => expect(clock()).toMatch(/^\d{2,}:\d{2}:\d{2}$/), WAIT);
            expect(badge()).not.toBeNull();
            const [line] = await openList();

            // The session is said to have changed: the page reads the state,
            // and that read is held. It was sent BEFORE the control.
            const earlier = deferred();
            daemon.routes[STATE] = [
                { status: 200, body: sessionState(S_OPEN, 4), hold: earlier.promise },
                { status: 503, body: { code: 'unavailable', message: 'The state is busy.' } },
            ];
            const r0 = stateReads(daemon);
            event('operating_session.changed', { archive_id: ARCHIVE, generation: 4 });
            await vi.waitFor(() => expect(stateReads(daemon)).toBe(r0 + 1), WAIT);

            // Continue fails; the read that finds out fails too.
            daemon.routes[CONTINUE] = {
                status: 503,
                body: { code: 'unavailable', message: 'The daemon is busy.' },
            };
            await fireEvent.click(line);
            const said = await screen.findByTestId('session-unknown', {}, WAIT);

            sameStream();
            expect(said.textContent.replace(/\s+/g, ' ').trim()).toMatch(
                /^The daemon is busy\. The session could not be read: The state is busy\. The contacts below are as last read\./
            );
            expect(calls()).toEqual(['G4OPEN']); // the rows stay
            expect(clock()).toBe(''); // the sitting on screen may have ended
            expect(badge()).toBeNull();
            expect(screen.getByRole('button', { name: 'Continue…' })).toBeDisabled();
            expect(session.behind).toBe(true); // Download and Send are held
            expect(continues(daemon)).toHaveLength(1);

            // The read sent before the failure answers, with the session as
            // it was. It says nothing of what the control did.
            earlier.release();
            await settle();
            await new Promise((r) => setTimeout(r, 50));
            sameStream();
            expect(screen.getByTestId('session-unknown')).toBeInTheDocument();
            expect(screen.getByRole('button', { name: 'Continue…' })).toBeDisabled();
            expect(clock()).toBe('');
            // Nothing is reading now, and Download and Send are still held.
            expect(session.behind).toBe(true);
            expect(session.hold).toBe('failed');

            // "Try again": the Continue HAD been applied. The state answers
            // with the session it opened; that session's rows are held.
            const rowsHeld = deferred();
            daemon.routes[STATE] = ok(
                sessionState(S_CLOSED, 6, {
                    session: {
                        id: S_CLOSED,
                        logbook_uuid: 'x',
                        opened_at: '2026-11-20T09:15:00Z',
                        sitting_started_at: new Date().toISOString(),
                        has_contacts: true,
                        contact_count: 1,
                        held_by_you: true,
                    },
                })
            );
            daemon.routes[ROWS] = {
                status: 200,
                body: members(S_CLOSED, 6, [member(2, 'K2EARLIER')]),
                hold: rowsHeld.promise,
            };
            daemon.routes[CLOSED] = ok(closedList(6, [closedLine(S_OPEN, { contact_count: 1 })]));
            const reads = stateReads(daemon);
            await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
            await vi.waitFor(() => expect(session.sessionId).toBe(S_CLOSED), WAIT);

            sameStream();
            expect(stateReads(daemon)).toBeGreaterThan(reads);
            expect(continues(daemon)).toHaveLength(1); // it read: it sent no control
            expect(news(daemon)).toEqual([]);
            expect(screen.queryByTestId('session-unknown')).toBeNull();
            // Which session is open is known; its contacts are not drawn yet,
            // so Download and Send stay held.
            expect(calls()).toEqual([]);
            expect(session.behind).toBe(true);
            expect(session.hold).toBe('reading');

            rowsHeld.release();
            await vi.waitFor(() => expect(calls()).toEqual(['K2EARLIER']), WAIT);
            await vi.waitFor(() => expect(session.behind).toBe(false), WAIT);
            sameStream();
            await vi.waitFor(() => expect(clock()).toMatch(/^00:00:\d{2}$/), WAIT);
            expect(badge()).not.toBeNull();
            await vi.waitFor(
                () => expect(screen.getByRole('button', { name: 'Continue…' })).toBeEnabled(),
                WAIT
            );
        },
        TEST_LIMIT
    );
});
