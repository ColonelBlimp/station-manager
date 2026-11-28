// W-0029 part 6, slice 3 at the PRODUCTION BOUNDARY (criterion F6, after
// codex-286): the real main.ts with only the transport stubbed. A write takes
// its archive from what the page is bound to WHEN IT IS SENT. So a line chosen
// from archive A's list must send nothing once the page is bound to B, even
// though the two archives' generations can be equal.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { session } from './lib/operate/session.svelte';
import { continueList } from './lib/operate/continueList.svelte';
import { setBoundArchive } from './lib/api/archive-guard';
import {
    OTHER_ARCHIVE,
    S_OPEN,
    S_CLOSED,
    STATE,
    ROWS,
    CLOSED,
    sessionState,
    member,
    members,
    closedLine,
    closedList,
    ok,
    daemonWith,
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

describe("main.ts: a line chosen from another archive's list", () => {
    beforeAll(() => boot(daemon), TEST_LIMIT);
    afterAll(() => vi.unstubAllGlobals());

    it(
        'sends nothing once the page is bound to another archive than the list is of',
        async () => {
            await vi.waitFor(() => expect(calls()).toEqual(['G4OPEN']), WAIT);
            await showPanel();
            const [line] = await openList();
            expect(continueList.open).toBe(true);

            // The page comes to be bound to another archive while the list is on screen.
            setBoundArchive(OTHER_ARCHIVE);
            await fireEvent.click(line);
            for (let i = 0; i < 20; i++) await Promise.resolve();

            expect(continues(daemon)).toEqual([]);
            expect(news(daemon)).toEqual([]);
            expect(session.sessionId).toBe(S_OPEN);

            // The control half: bound to the list's archive again, the same line is sent.
            setBoundArchive('01a163a7-0f04-7f69-85e9-255664b58d11');
            await fireEvent.click(screen.getAllByRole('button', { name: /UTC ·/ })[0]);
            await vi.waitFor(() => expect(continues(daemon)).toHaveLength(1), WAIT);
        },
        TEST_LIMIT
    );
});
