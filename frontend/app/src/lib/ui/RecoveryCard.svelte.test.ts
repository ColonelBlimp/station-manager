// The recovery question, and the screen for a session that could not be read
// (W-0029 part 4, E). The read and the answer are injected, so these run
// without a daemon. The words are the operator's ruling of 2026-11-23.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import RecoveryCard from './RecoveryCard.svelte';
import {
    recovery,
    setRecoveryPorts,
    readSessionState,
    sendSessionControl,
    _resetRecoveryForTests,
    type SessionStateLike,
    type SessionAnswerLike,
    type SessionReadLike,
} from '../recovery.svelte';

const HOME = '01a16a00-0000-7000-8000-000000000001';
const S0 = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';

const question = (contactCount: number): SessionStateLike => ({
    archiveId: HOME,
    generation: 4,
    state: 'recovery_pending',
    session: {
        id: S0,
        openedAt: '2026-11-20T09:15:00Z',
        sittingStartedAt: '2026-11-20T09:15:00Z',
        contactCount,
        heldByYou: false,
    },
});
const held: SessionStateLike = {
    archiveId: HOME,
    generation: 5,
    state: 'open',
    session: {
        id: S0,
        openedAt: '2026-11-20T09:15:00Z',
        sittingStartedAt: '2026-11-20T09:15:00Z',
        contactCount: 3,
        heldByYou: true,
    },
};

let nextRead: SessionReadLike;
let answer: ReturnType<
    typeof vi.fn<(a: string, g: number, s: string) => Promise<SessionAnswerLike>>
>;

async function show(read: SessionReadLike): Promise<void> {
    nextRead = read;
    await readSessionState();
    render(RecoveryCard);
}

beforeEach(() => {
    _resetRecoveryForTests();
    answer = vi.fn();
    setRecoveryPorts({ read: () => Promise.resolve(nextRead), answer, archive: () => HOME });
});

describe('RecoveryCard — the question', () => {
    it('names the session in the ruled words, and offers Continue and New and nothing else', async () => {
        await show({ kind: 'ok', state: question(3) });
        expect(
            screen.getByRole('heading', { name: 'The last session is still open' })
        ).toBeInTheDocument();
        // Said of the session as it is now, and of nothing in its past (operator,
        // 2026-11-24). "Station Manager did not close normally last time" is false for a
        // session left open by a contact logged while no app was running; "was never
        // closed" is false for one that was closed, continued, and then left open.
        expect(
            screen.getByText('The session that started 2026-11-20 09:15 UTC has 3 contacts.')
        ).toBeInTheDocument();
        expect(screen.queryByText(/did not close normally/)).toBeNull();
        expect(screen.queryByText(/never closed/)).toBeNull();
        expect(
            screen.getByText(
                'If you start a new one, the earlier session is closed and kept, with its contacts.'
            )
        ).toBeInTheDocument();
        expect(screen.getAllByRole('button').map((b) => b.textContent?.trim())).toEqual([
            'Continue this session',
            'Start a new session',
        ]);
        expect(screen.queryByRole('link')).toBeNull();
    });

    it.each([
        [1, 'has 1 contact.'],
        [0, 'has 0 contacts.'],
    ])('a session with %i: "%s"', async (n, tail) => {
        await show({ kind: 'ok', state: question(n) });
        expect(screen.getByText(new RegExp(tail.replace('.', '\\.')))).toBeInTheDocument();
    });

    it('Continue sends the answer, and while it is in flight both buttons are disabled', async () => {
        let settle!: (v: SessionAnswerLike) => void;
        answer.mockReturnValue(new Promise<SessionAnswerLike>((r) => (settle = r)));
        await show({ kind: 'ok', state: question(3) });
        const cont = screen.getByRole('button', { name: 'Continue this session' });
        const fresh = screen.getByRole('button', { name: 'Start a new session' });

        await fireEvent.click(cont);
        expect(answer).toHaveBeenCalledWith('continue', 4, S0);
        expect(cont).toBeDisabled();
        expect(fresh).toBeDisabled();
        await fireEvent.click(fresh);
        expect(answer).toHaveBeenCalledTimes(1);

        settle({ kind: 'applied', state: held });
        await vi.waitFor(() => expect(recovery.status).toBe('clear'));
    });

    it('New sends the other answer', async () => {
        answer.mockResolvedValue({ kind: 'applied', state: held });
        await show({ kind: 'ok', state: question(3) });
        await fireEvent.click(screen.getByRole('button', { name: 'Start a new session' }));
        expect(answer).toHaveBeenCalledWith('new', 4, S0);
    });

    it('an answer that failed, with the question still standing: it says what went wrong and can be pressed again', async () => {
        answer.mockResolvedValue({
            kind: 'failed',
            message: 'the session could not be changed; nothing was changed',
        });
        await show({ kind: 'ok', state: question(3) });
        await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
        expect(await screen.findByRole('alert')).toHaveTextContent(
            'the session could not be changed; nothing was changed'
        );
        expect(screen.getByRole('button', { name: 'Continue this session' })).not.toBeDisabled();
    });
});

describe('RecoveryCard — the session could not be read', () => {
    it('says so, gives the reason, and offers only to try again', async () => {
        await show({ kind: 'error', message: 'Cannot reach the daemon.' });
        expect(
            screen.getByRole('heading', { name: 'The session could not be read' })
        ).toBeInTheDocument();
        expect(screen.getByRole('alert')).toHaveTextContent('Cannot reach the daemon.');
        expect(screen.getAllByRole('button').map((b) => b.textContent?.trim())).toEqual([
            'Try again',
        ]);
    });

    it('Try again reads once more and shows what that read calls for', async () => {
        await show({ kind: 'error', message: 'Cannot reach the daemon.' });
        nextRead = { kind: 'ok', state: question(3) };
        await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
        expect(
            await screen.findByRole('heading', { name: 'The last session is still open' })
        ).toBeInTheDocument();
    });
});

// W-0029 part 6, slice 3 (codex-290): a control pressed in the workspace can be
// refused as stale with a state that says recovery is pending. The operator is
// then looking at THIS card, not the panel, and must still be told the choice
// was not applied.
describe('RecoveryCard after a control from the workspace was refused as stale', () => {
    const STALE = 'The session changed before this could be done. Your choice was not applied.';

    it('the question says the choice was not applied, and still says so after the page reads again', async () => {
        nextRead = { kind: 'ok', state: held };
        await readSessionState();
        expect(recovery.status).toBe('clear');
        const pending = { ...question(3), generation: 7 };
        answer.mockResolvedValue({ kind: 'stale', state: pending });

        await sendSessionControl('new', held.generation, '');
        render(RecoveryCard);

        expect(
            screen.getByRole('heading', { name: 'The last session is still open' })
        ).toBeInTheDocument();
        expect(screen.getByRole('alert').textContent.trim()).toBe(STALE);

        // A read the page makes for itself (the count, the stream reopening).
        nextRead = { kind: 'ok', state: pending };
        await readSessionState();
        expect(screen.getByRole('alert').textContent.trim()).toBe(STALE);

        // Answering the question is a new choice: the sentence goes.
        answer.mockReturnValue(new Promise(() => {}));
        await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
        expect(screen.queryByRole('alert')).toBeNull();
    });
});
