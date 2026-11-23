import { afterEach, describe, expect, it, vi } from 'vitest';
import { ARCHIVE_HEADER, setBoundArchive } from './archive-guard';
import { TEST_ARCHIVE } from '../../test/archive';
import { answerOperatingSession, fetchOperatingSession } from './operating-session';

// W-0029 part 4, second slice: the session's read and its two controls as the
// page sends them. The read is never drawn as an empty session when it fails;
// a control's stale refusal carries the state to redraw to.

const SESSION = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';

const pending = {
    archive_id: TEST_ARCHIVE,
    generation: 4,
    state: 'recovery_pending',
    session: {
        id: SESSION,
        logbook_uuid: '01a187bc-0173-7649-8ff3-71db8170c309',
        opened_at: '2026-11-20T09:15:00Z',
        sitting_started_at: '2026-11-20T09:15:00Z',
        has_contacts: true,
        contact_count: 3,
        held_by_you: false,
    },
};

type Sent = { url: string; method: string; body: string | null; archive: string | null };
let sent: Sent[] = [];

function stub(status: number, body: unknown, raw = false): void {
    sent = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            sent.push({
                url: input,
                method: (init?.method ?? 'GET').toUpperCase(),
                body: typeof init?.body === 'string' ? init.body : null,
                archive: new Headers(init?.headers).get(ARCHIVE_HEADER),
            });
            return Promise.resolve(
                new Response(raw ? String(body) : JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
}

afterEach(() => {
    vi.unstubAllGlobals();
    setBoundArchive(null);
});

describe('fetchOperatingSession', () => {
    it('reads the state, the session and its contact count', async () => {
        stub(200, pending);
        const out = await fetchOperatingSession();
        expect(out).toEqual({
            kind: 'ok',
            state: {
                archiveId: TEST_ARCHIVE,
                generation: 4,
                state: 'recovery_pending',
                session: {
                    id: SESSION,
                    openedAt: '2026-11-20T09:15:00Z',
                    contactCount: 3,
                    heldByYou: false,
                },
            },
        });
        expect(sent).toEqual([
            { url: '/v1/operating-session', method: 'GET', body: null, archive: null },
        ]);
    });

    it('reads a state with no session', async () => {
        stub(200, { archive_id: TEST_ARCHIVE, generation: 7, state: 'none', session: null });
        expect(await fetchOperatingSession()).toEqual({
            kind: 'ok',
            state: { archiveId: TEST_ARCHIVE, generation: 7, state: 'none', session: null },
        });
    });

    // A failed read is an error, whatever the failure: it is never an empty session.
    it.each([
        [
            'the daemon says it cannot read the record',
            503,
            { code: 'operating_session_unavailable', message: 'x' },
            false,
        ],
        ['a daemon with no such route', 404, { code: 'not_found' }, false],
        ['a body that is not JSON', 200, 'not json', true],
        ['a state the page does not know', 200, { ...pending, state: 'paused' }, false],
        ['no generation', 200, { ...pending, generation: undefined }, false],
        ['no archive', 200, { ...pending, archive_id: '' }, false],
        [
            'a session with no count',
            200,
            { ...pending, session: { ...pending.session, contact_count: undefined } },
            false,
        ],
        [
            'a session with no start',
            200,
            { ...pending, session: { ...pending.session, opened_at: 'yesterday' } },
            false,
        ],
        ['recovery pending with no session', 200, { ...pending, session: null }, false],
    ])('%s is an error', async (_name, status, body, raw) => {
        stub(status, body, raw);
        expect((await fetchOperatingSession()).kind).toBe('error');
    });

    it('a daemon that cannot be reached is an error', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('failed to fetch')))
        );
        expect((await fetchOperatingSession()).kind).toBe('error');
    });
});

describe('answerOperatingSession', () => {
    const applied = {
        ...pending,
        generation: 5,
        state: 'open',
        session: { ...pending.session, held_by_you: true },
    };

    it('Continue names the generation shown and the session, for the bound archive', async () => {
        setBoundArchive(TEST_ARCHIVE);
        stub(200, applied);
        const out = await answerOperatingSession('continue', 4, SESSION);
        expect(out.kind).toBe('applied');
        expect(out.kind === 'applied' && out.state.session?.heldByYou).toBe(true);
        expect(sent).toEqual([
            {
                url: '/v1/operating-session/continue',
                method: 'POST',
                body: JSON.stringify({ generation: 4, session_id: SESSION }),
                archive: TEST_ARCHIVE,
            },
        ]);
    });

    it('New names the generation shown and no session', async () => {
        setBoundArchive(TEST_ARCHIVE);
        stub(200, {
            ...applied,
            session: {
                ...applied.session,
                id: '01a1f83c-da20-751f-80b7-ec9a3f1f93f4',
                contact_count: 0,
                has_contacts: false,
            },
        });
        const out = await answerOperatingSession('new', 4, SESSION);
        expect(out.kind).toBe('applied');
        expect(sent[0]).toEqual({
            url: '/v1/operating-session/new',
            method: 'POST',
            body: JSON.stringify({ generation: 4 }),
            archive: TEST_ARCHIVE,
        });
    });

    it('a stale refusal carries the state to redraw to', async () => {
        setBoundArchive(TEST_ARCHIVE);
        stub(409, {
            code: 'operating_session_stale',
            message: 'changed',
            operating_session: applied,
        });
        const out = await answerOperatingSession('new', 4, SESSION);
        expect(out).toMatchObject({ kind: 'stale', state: { generation: 5, state: 'open' } });
    });

    it('a stale refusal with no usable state is a failure, not a state', async () => {
        setBoundArchive(TEST_ARCHIVE);
        stub(409, { code: 'operating_session_stale', message: 'changed' });
        expect((await answerOperatingSession('new', 4, SESSION)).kind).toBe('failed');
    });

    it("another refusal is given in the daemon's words", async () => {
        setBoundArchive(TEST_ARCHIVE);
        stub(409, {
            code: 'operating_session_switching',
            message: 'The archive is being switched. Nothing was changed.',
        });
        expect(await answerOperatingSession('continue', 4, SESSION)).toEqual({
            kind: 'failed',
            message: 'The archive is being switched. Nothing was changed.',
        });
    });

    it.each([
        [
            'a 503',
            503,
            {
                code: 'operating_session_unavailable',
                message: 'the session could not be changed; nothing was changed',
            },
        ],
        ['a 200 that is not a state', 200, { ok: true }],
    ])('%s is a failure', async (_name, status, body) => {
        setBoundArchive(TEST_ARCHIVE);
        stub(status, body);
        expect((await answerOperatingSession('new', 4, SESSION)).kind).toBe('failed');
    });

    it('with no archive proven, nothing is sent', async () => {
        setBoundArchive(null);
        stub(200, applied);
        const out = await answerOperatingSession('new', 4, SESSION);
        expect(out.kind).toBe('failed');
        expect(sent).toEqual([]);
    });
});
