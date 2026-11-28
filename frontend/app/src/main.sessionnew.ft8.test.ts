// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criteria C2, E4, E5): the
// real main.ts with only the transport stubbed, on the FT8 view, starting with
// NO session open.
//
// E5: New does not stop FT8 or disarm TX, and a run crosses it. What the page
// can do wrong there is to send FT8 something, or to let go of its FT8 event
// stream (the app's presence for a run). So the test holds on to the stream it
// found and counts the requests under /v1/ft8 across the press.
// E4: a refusal the daemon gives is shown in its words.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { FakeEventSource } from './test/mainHarness';
import { session } from './lib/operate/session.svelte';
import {
    S_NEW,
    STATE,
    ROWS,
    CLOSED,
    NEW,
    sessionState,
    members,
    closedList,
    ok,
    daemonWith,
    news,
    boot,
    showPanel,
    WAIT,
    TEST_LIMIT,
} from './test/sessionControls';

const daemon = daemonWith(
    {
        // No session is open: a start step failed, and no contact has opened one.
        [STATE]: ok(sessionState(null, 4)),
        [ROWS]: ok(members(null, 4, [])),
        [CLOSED]: ok(closedList(4, [])),
        'POST /v1/ft8/claim': ok({ mode: 'FT8' }),
    },
    true
);
const ft8Streams = (): FakeEventSource[] =>
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8'));
const ft8Requests = (): number => daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length;

describe('main.ts, FT8 view: New with no session open, and a New the daemon refuses', () => {
    beforeAll(() => boot(daemon, 'ft8'), TEST_LIMIT);
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it(
        'New opens a session and touches nothing of FT8; a refusal is said in the words the daemon gave',
        async () => {
            await vi.waitFor(() => expect(session.state).toBe('none'), WAIT);
            await vi.waitFor(() => expect(ft8Streams()).toHaveLength(1), WAIT);
            const stream = ft8Streams()[0];
            await showPanel();
            expect(screen.getByTestId('session-empty').textContent.trim()).toBe(
                'No session is open.'
            );
            const press = await screen.findByRole('button', { name: 'New session' }, WAIT);

            daemon.routes[NEW] = ok(sessionState(S_NEW, 5));
            daemon.routes[STATE] = ok(sessionState(S_NEW, 5));
            daemon.routes[ROWS] = ok(members(S_NEW, 5, []));
            daemon.routes[CLOSED] = ok(closedList(5, []));
            const ft8Before = ft8Requests();
            await fireEvent.click(press);

            await vi.waitFor(() => expect(session.sessionId).toBe(S_NEW), WAIT);
            await vi.waitFor(() => expect(session.behind).toBe(false), WAIT);
            expect(JSON.parse(news(daemon)[0].body ?? '')).toEqual({ generation: 4 });
            // E5: nothing was sent to FT8, and its stream is the same one, still open.
            expect(ft8Requests()).toBe(ft8Before);
            expect(ft8Streams()).toEqual([stream]);
            expect(stream.readyState).not.toBe(2);
            expect(document.querySelector('.content-wrap')).not.toBeNull();

            // E4: the daemon refuses the next one (the archive is being switched).
            daemon.routes[NEW] = {
                status: 409,
                body: {
                    code: 'operating_session_archive_switching',
                    message: 'The archive is being switched; no session can be started.',
                },
            };
            await fireEvent.click(screen.getByRole('button', { name: 'New session' }));
            expect((await screen.findByTestId('session-control-error')).textContent.trim()).toBe(
                'The archive is being switched; no session can be started.'
            );
            expect(news(daemon)).toHaveLength(2);
            expect(session.sessionId).toBe(S_NEW);
            expect(ft8Streams()).toEqual([stream]);
        },
        TEST_LIMIT
    );
});
