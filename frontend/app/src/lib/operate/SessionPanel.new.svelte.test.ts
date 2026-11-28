// The panel's "New session" (W-0029 part 6, slice 3; criteria E; the
// operator's ruling 2 of 2026-11-25: New does not ask before it acts). The
// control is sent by the recovery gate, which is real here: what the test
// stubs is the daemon. The nearest confusable outcomes: a New sent at a
// generation the page made up; a second New sent by a second press; and the
// control offered where the daemon would only refuse it.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import SessionPanel from './SessionPanel.svelte';
import {
    session,
    sessionControlStands,
    sessionStateDrawn,
    setSessionPorts,
    _resetSessionForTests,
} from './session.svelte';
import {
    continueList,
    continueStateDrawn,
    setContinuePorts,
    _resetContinueForTests,
} from './continueList.svelte';
import {
    recovery,
    sessionArchiveProven,
    setRecoveryPorts,
    _resetRecoveryForTests,
    type SessionAnswerLike,
    type SessionStateLike,
} from '../recovery.svelte';
import { TEST_ARCHIVE } from '../../test/archive';

const SENTENCE = 'Contacts stay in the session they were logged in.';
const S1 = '01a1f83c-da20-7000-8000-0000000000a1';
const S2 = '01a1f83c-da20-7000-8000-0000000000a2';

const state = (
    name: SessionStateLike['state'],
    generation: number,
    id: string | null = S1
): SessionStateLike => ({
    archiveId: TEST_ARCHIVE,
    generation,
    state: name,
    session:
        id === null
            ? null
            : {
                  id,
                  openedAt: '2026-11-25T09:15:00Z',
                  sittingStartedAt: '2026-11-25T09:15:00Z',
                  contactCount: 0,
                  heldByYou: true,
              },
});

let pressed: Array<[string, number, string]>;
let answers: Array<SessionAnswerLike | Promise<SessionAnswerLike>>;

// The page as main.ts wires it: the gate draws, and the Session and the list follow.
async function showing(first: SessionStateLike): Promise<void> {
    setRecoveryPorts({
        read: () => Promise.resolve({ kind: 'ok', state: first }),
        answer: (answer, generation, sessionId) => {
            pressed.push([answer, generation, sessionId]);
            const next = answers.shift();
            if (next === undefined) throw new Error('the test queued no answer');
            return Promise.resolve(next);
        },
        archive: () => TEST_ARCHIVE,
        drawn: (s) => {
            sessionStateDrawn(s);
            continueStateDrawn(s);
        },
        control: sessionControlStands,
    });
    await sessionArchiveProven();
    flushSync();
}
const newButton = (): HTMLElement | null => screen.queryByRole('button', { name: 'New session' });
const settle = async (): Promise<void> => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    flushSync();
};

beforeEach(() => {
    _resetRecoveryForTests();
    _resetSessionForTests();
    _resetContinueForTests();
    pressed = [];
    answers = [];
    setSessionPorts({
        readRows: () => new Promise(() => {}),
        readState: () => Promise.resolve(true),
        archive: () => TEST_ARCHIVE,
    });
    setContinuePorts({
        read: () => new Promise(() => {}), // whether there is anything to continue is never known here
        archive: () => TEST_ARCHIVE,
        readState: () => {},
        send: () => Promise.resolve('applied'),
    });
});

describe('SessionPanel: New session', () => {
    it.each([
        ['open', S1],
        ['none', null],
    ] as const)(
        'is offered in "%s", with the sentence beside it, whatever is known of the list',
        async (name, id) => {
            render(SessionPanel);
            await showing(state(name, 4, id));

            expect(continueList.available).toBe('unread');
            expect(newButton()).toBeEnabled();
            expect(screen.getByText(SENTENCE)).toBeInTheDocument();
            // Continue is not offered until the list says there is something to continue.
            expect(screen.queryByRole('button', { name: 'Continue…' })).toBeNull();
        }
    );

    it('is not offered while the archive is being switched', async () => {
        render(SessionPanel);
        await showing(state('archive_switching', 4, null));
        expect(newButton()).toBeNull();
        expect(screen.queryByText(SENTENCE)).toBeNull();
    });

    it('is not offered before any state is drawn', () => {
        render(SessionPanel);
        expect(newButton()).toBeNull();
    });

    it('one press sends New once, at the generation of the state shown, and asks nothing first', async () => {
        render(SessionPanel);
        await showing(state('open', 4));
        let release!: (v: SessionAnswerLike) => void;
        answers.push(new Promise<SessionAnswerLike>((r) => (release = r)));

        await fireEvent.click(newButton()!);
        await settle();

        // Sent at once: no dialog stands between the press and the control.
        expect(pressed).toEqual([['new', 4, '']]);
        expect(screen.queryByRole('dialog')).toBeNull();
        // While it is out it cannot be pressed again, and a press sends nothing.
        expect(newButton()).toBeDisabled();
        await fireEvent.click(newButton()!);
        expect(pressed).toHaveLength(1);

        release({ kind: 'applied', state: state('open', 5, S2) });
        await settle();
        expect(session.sessionId).toBe(S2);
        expect(newButton()).toBeEnabled();
        expect(pressed).toHaveLength(1);
        expect(recovery.controlError).toBe('');
    });

    it.each(['out', 'unknown'] as const)(
        'cannot be pressed while a control is "%s"',
        async (standing) => {
            render(SessionPanel);
            await showing(state('open', 4));
            sessionControlStands(standing);
            flushSync();
            expect(newButton()).toBeDisabled();
        }
    );

    it('with no session open it opens one', async () => {
        render(SessionPanel);
        await showing(state('none', 4, null));
        answers.push({ kind: 'applied', state: state('open', 5, S2) });

        await fireEvent.click(newButton()!);
        await settle();

        expect(pressed).toEqual([['new', 4, '']]);
        expect(session.sessionId).toBe(S2);
    });
});
