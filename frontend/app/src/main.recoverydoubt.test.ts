// W-0029 part 6 at the PRODUCTION BOUNDARY (Codex's review, codex-277): the
// real main.ts with only the transport stubbed, on a daemon whose archive
// holds a session left open with one contact.
//
//  1. A contact deleted while the question shows: the question's count is the
//     daemon's, and the deletion's event is what makes the page read it.
//  2. Two reads of the state out together, answered "recovery pending" and
//     "open" for the same session at the same generation. Which the daemon
//     served last is not known, so the page shows neither: no question, no
//     workspace, nothing that can act. It reads once more. That read fails:
//     the page says the session could not be read, and "Try again" reads it.
//  3. A contact deleted while that deciding read is out (codex-279): the read
//     was served before the deletion and brings the question back with the
//     old count. One read follows.
//  4. The rule behind 3 (codex-280): the question's count is good only if no
//     contact was stored into a session or deleted since the request that
//     supplied it was sent. A contact stored with the question showing; a
//     contact that joins the session shown while a read is out that brings
//     the question; a deletion before the held refusal of Continue arrives.
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/svelte';
import { installDaemon, shellStream, FakeEventSource, type Daemon } from './test/mainHarness';
import { _setReloadForTests, archivesState } from './lib/config/archives.svelte';
import { recovery } from './lib/recovery.svelte';

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const A = '01a187bc-0173-7649-8ff3-71db8170c309';
const REMNANT = '01a1f83c-da20-751f-80b7-ec9a3f1f93f3';
const STATE = 'GET /v1/operating-session';

// As the daemon answers (internal/api/operating_session.go, sessionResponse;
// codex-279). `has_contacts` stays true when the last contact is deleted: a
// deleted contact's membership is kept, and it is what makes recovery pending
// at all. And one caller can be told "open" and "recovery pending" for the
// same session with `held_by_you` false both times: the state follows which
// lifetime is admitted when the daemon is asked, not who holds the session.
const state = (name: 'recovery_pending' | 'open', contacts: number, generation = 4) => ({
    archive_id: ARCHIVE,
    generation,
    state: name,
    session: {
        id: REMNANT,
        logbook_uuid: A,
        opened_at: '2026-11-20T09:15:00Z',
        sitting_started_at: '2026-11-20T09:15:00Z',
        has_contacts: true,
        contact_count: contacts,
        held_by_you: false,
    },
});

const daemon: Daemon = {
    version: {
        daemon: 'test',
        instance: 'i1',
        archive: { id: ARCHIVE, label: 'Home', ownership: 'legacy', default_logbook_uuid: A },
    },
    config: {
        setup_complete: true,
        logging_station: { station_callsign: '7Q5MLV' },
        default_logbook: { id: 1, uuid: A, name: 'Home', callsign: '7Q5MLV' },
        bridge: { enabled: false },
        ft8_enabled: true,
        ft8_running: true,
    },
    routes: { [STATE]: { status: 200, body: state('recovery_pending', 1) } },
    sent: [],
};

const reads = (): number =>
    daemon.sent.filter((s) => s.method === 'GET' && s.url === '/v1/operating-session').length;
const posts = (): string[] => daemon.sent.filter((s) => s.method !== 'GET').map((s) => s.url);
const settle = async (): Promise<void> => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
};
const changed = (generation: number): void =>
    shellStream().emit(
        'operating_session.changed',
        JSON.stringify({ archive_id: ARCHIVE, generation })
    );
const stored = (n: number): void =>
    shellStream().emit(
        'qso.stored',
        JSON.stringify({
            qso_uuid: `01a1f83c-da20-751f-80b7-ec9a3f1f95${String(n).padStart(2, '0')}`,
            logbook_id: 1,
            operating_session_id: REMNANT,
        })
    );
const memberReads = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/operating-session/members')).length;
const ft8 = (): number =>
    daemon.sent.filter((s) => s.url.startsWith('/v1/ft8')).length +
    FakeEventSource.instances.filter((s) => s.url.includes('/v1/ft8')).length;
const workspace = (): Element | null => document.querySelector('.content-wrap');
const asked = (): HTMLElement | null =>
    screen.queryByRole('heading', { name: 'The last session is still open' });
const text = (): string => document.body.textContent?.replace(/\s+/g, ' ') ?? '';

describe('main.ts: the recovery question, a deleted contact, and two answers in doubt', () => {
    beforeAll(async () => {
        installDaemon(daemon);
        _setReloadForTests(() => {});
        window.localStorage.setItem('sm-op-mode', 'ft8');
        window.history.replaceState({}, '', '/operate/ft8');
        document.body.innerHTML = '<div id="app"></div>';
        await import('./main');
        await vi.waitFor(() => expect(archivesState.verifying).toBe(false));
    });
    afterAll(() => {
        vi.unstubAllGlobals();
        window.localStorage.removeItem('sm-op-mode');
    });

    it('a contact deleted while the question shows: the question says the count the daemon holds', async () => {
        await vi.waitFor(() => expect(asked()).not.toBeNull());
        expect(text()).toContain('has 1 contact.');

        // The contact is deleted; the daemon announces it after the commit.
        const before = reads();
        daemon.routes[STATE] = { status: 200, body: state('recovery_pending', 0) };
        shellStream().emit(
            'qso.deleted',
            JSON.stringify({ qso_uuid: '01a1f83c-da20-751f-80b7-ec9a3f1f9401', logbook_id: 1 })
        );
        await vi.waitFor(() => expect(reads()).toBe(before + 1));
        await vi.waitFor(() => expect(text()).toContain('has 0 contacts.'));
        expect(asked()).not.toBeNull();
        expect(reads()).toBe(before + 1);
    });

    it('two answers in doubt that disagree: nothing is shown and nothing can act, and a failed deciding read says so', async () => {
        expect(recovery.status).toBe('question');
        const before = reads();
        let answerBoth!: () => void;
        let answerDeciding!: () => void;
        const both = new Promise<void>((r) => (answerBoth = r));
        daemon.routes[STATE] = [
            { status: 200, body: state('recovery_pending', 0), hold: both },
            // The same session at the same generation, as its holder would be told.
            { status: 200, body: state('open', 0), hold: both },
            {
                status: 503,
                body: { code: 'operating_session_unavailable', message: 'could not be read' },
                hold: new Promise<void>((r) => (answerDeciding = r)),
            },
            { status: 200, body: state('recovery_pending', 0) },
        ];
        // The stream reopens, and an event follows it: two reads out together.
        shellStream().emit('open');
        await vi.waitFor(() => expect(reads()).toBe(before + 1));
        shellStream().emit(
            'operating_session.changed',
            JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
        );
        await vi.waitFor(() => expect(reads()).toBe(before + 2));
        answerBoth();

        // The page reads once more, and while that read is out it shows
        // neither the question nor the workspace.
        await vi.waitFor(() => expect(reads()).toBe(before + 3));
        await vi.waitFor(() => expect(recovery.status).toBe('loading'));
        expect(asked()).toBeNull();
        expect(workspace()).toBeNull();
        expect(document.querySelector('nav')).toBeNull();
        // Nothing of the workspace exists to press: no export, no email, no
        // FT8 view (so no presence in FT8), and the Session read no contacts.
        expect(screen.queryAllByRole('button')).toEqual([]);
        expect(posts()).toEqual([]);
        expect(memberReads()).toBe(0);
        expect(ft8()).toBe(0);

        // The deciding read fails.
        answerDeciding();
        expect(
            await screen.findByRole('heading', { name: 'The session could not be read' })
        ).toBeInTheDocument();
        expect(workspace()).toBeNull();
        expect(posts()).toEqual([]);
        expect(memberReads()).toBe(0);
        expect(ft8()).toBe(0);
        expect(reads()).toBe(before + 3); // a read that fails is not repeated

        // "Try again" reads it: recovery is pending, and the question is asked.
        await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
        await vi.waitFor(() => expect(asked()).not.toBeNull());
        expect(reads()).toBe(before + 4);
        expect(workspace()).toBeNull();
        expect(posts()).toEqual([]);
    });

    it('a contact deleted while the deciding read is out: the question that comes back says the count the daemon holds', async () => {
        // A contact is logged with no app admitted: the question says one again.
        daemon.routes[STATE] = { status: 200, body: state('recovery_pending', 1) };
        let before = reads();
        shellStream().emit(
            'operating_session.changed',
            JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
        );
        await vi.waitFor(() => expect(reads()).toBe(before + 1));
        await vi.waitFor(() => expect(text()).toContain('has 1 contact.'));

        // Two reads in doubt that disagree, and the read that decides is held.
        before = reads();
        let answerBoth!: () => void;
        let answerDeciding!: () => void;
        const both = new Promise<void>((r) => (answerBoth = r));
        daemon.routes[STATE] = [
            { status: 200, body: state('recovery_pending', 1), hold: both },
            { status: 200, body: state('open', 1), hold: both },
            {
                // Served before the deletion: one contact.
                status: 200,
                body: state('recovery_pending', 1),
                hold: new Promise<void>((r) => (answerDeciding = r)),
            },
            { status: 200, body: state('recovery_pending', 0) },
        ];
        shellStream().emit('open');
        await vi.waitFor(() => expect(reads()).toBe(before + 1));
        shellStream().emit(
            'operating_session.changed',
            JSON.stringify({ archive_id: ARCHIVE, generation: 4 })
        );
        await vi.waitFor(() => expect(reads()).toBe(before + 2));
        answerBoth();
        await vi.waitFor(() => expect(reads()).toBe(before + 3));
        await vi.waitFor(() => expect(recovery.status).toBe('loading'));

        // The contact is deleted. Nothing is asked while the deciding read is out.
        shellStream().emit(
            'qso.deleted',
            JSON.stringify({ qso_uuid: '01a1f83c-da20-751f-80b7-ec9a3f1f9402', logbook_id: 1 })
        );
        for (let i = 0; i < 10; i++) await Promise.resolve();
        expect(reads()).toBe(before + 3);

        // The deciding read answers with what it was served: the question, one
        // contact. One read follows it, and the question says none.
        answerDeciding();
        await vi.waitFor(() => expect(reads()).toBe(before + 4));
        await vi.waitFor(() => expect(text()).toContain('has 0 contacts.'));
        expect(asked()).not.toBeNull();
        expect(workspace()).toBeNull();
        expect(reads()).toBe(before + 4);
        expect(posts()).toEqual([]);
    });

    it('a contact stored with the question showing: one read, and the question says the new count', async () => {
        expect(recovery.status).toBe('question');
        // First, a contact that joined NO session, stored while a read is out:
        // it changes no count, so the read that was out needs no other.
        let answerHeld!: () => void;
        daemon.routes[STATE] = {
            status: 200,
            body: state('recovery_pending', 7),
            hold: new Promise<void>((r) => (answerHeld = r)),
        };
        const held = reads();
        changed(4);
        await vi.waitFor(() => expect(reads()).toBe(held + 1));
        shellStream().emit(
            'qso.stored',
            JSON.stringify({ qso_uuid: '01a1f83c-da20-751f-80b7-ec9a3f1f9500', logbook_id: 1 })
        );
        answerHeld();
        await vi.waitFor(() => expect(text()).toContain('has 7 contacts.'));
        await settle();
        expect(reads()).toBe(held + 1);

        daemon.routes[STATE] = { status: 200, body: state('recovery_pending', 1) };
        const before = reads();
        stored(1);
        await vi.waitFor(() => expect(text()).toContain('has 1 contact.'));
        await settle();
        expect(reads()).toBe(before + 1); // the Session's read is enough: none follows it
    });

    it('a contact joins the session shown while a read is out that brings the question: one read follows', async () => {
        // The session is taken on: the workspace shows it.
        daemon.routes[STATE] = { status: 200, body: state('open', 1) };
        changed(4);
        await vi.waitFor(() => expect(workspace()).not.toBeNull());
        await vi.waitFor(() => expect(recovery.status).toBe('clear'));
        await settle();

        // The stream reopens. Its read of the state is served "recovery
        // pending, one contact" and is slow to return.
        const before = reads();
        let answerHeld!: () => void;
        daemon.routes[STATE] = [
            {
                status: 200,
                body: state('recovery_pending', 1),
                hold: new Promise<void>((r) => (answerHeld = r)),
            },
            { status: 200, body: state('recovery_pending', 2) },
        ];
        shellStream().emit('open');
        await vi.waitFor(() => expect(reads()).toBe(before + 1));

        // A second contact joins that session. No generation moves, and the
        // Session reads its rows, not the state.
        stored(2);
        await settle();
        expect(reads()).toBe(before + 1);

        answerHeld();
        await vi.waitFor(() => expect(asked()).not.toBeNull());
        await vi.waitFor(() => expect(reads()).toBe(before + 2));
        await vi.waitFor(() => expect(text()).toContain('has 2 contacts.'));
        await settle();
        expect(reads()).toBe(before + 2);
    });

    it('the refusal of Continue, held from before a deletion, brings the question back: one read follows, and Continue was sent once', async () => {
        expect(recovery.status).toBe('question');
        const CONTINUE = 'POST /v1/operating-session/continue';
        let answerContinue!: () => void;
        daemon.routes[CONTINUE] = {
            status: 409,
            body: {
                code: 'operating_session_stale',
                message: 'changed',
                // Taken before the deletion: recovery pending at 6, one contact.
                operating_session: state('recovery_pending', 1, 6),
            },
            hold: new Promise<void>((r) => (answerContinue = r)),
        };
        await fireEvent.click(screen.getByRole('button', { name: 'Continue this session' }));
        const continues = (): number =>
            posts().filter((u) => u === '/v1/operating-session/continue').length;
        expect(continues()).toBe(1);

        // Meanwhile the workspace comes to be shown at generation 5 with no
        // session open. (With one open, the Session would ask for the state
        // again at the deletion below, its last read of it having failed, and
        // that read would bring the count by itself.)
        daemon.routes[STATE] = {
            status: 200,
            body: { archive_id: ARCHIVE, generation: 5, state: 'none', session: null },
        };
        changed(5);
        await vi.waitFor(() => expect(workspace()).not.toBeNull());
        // The read for the next change fails: the workspace stays.
        let before = reads();
        daemon.routes[STATE] = { status: 503, body: { code: 'unavailable', message: 'no' } };
        changed(6);
        await vi.waitFor(() => expect(reads()).toBe(before + 1));
        await settle();
        expect(workspace()).not.toBeNull();

        // With only Continue out, the contact is deleted. Nothing shows a count.
        before = reads();
        daemon.routes[STATE] = { status: 200, body: state('recovery_pending', 0, 6) };
        shellStream().emit(
            'qso.deleted',
            JSON.stringify({ qso_uuid: '01a1f83c-da20-751f-80b7-ec9a3f1f9403', logbook_id: 1 })
        );
        await settle();
        const afterDeletion = reads();
        expect(afterDeletion).toBe(before); // the deletion asked nothing

        // The refusal arrives: the question at 6 with one contact. One read
        // of the state follows it, and the question says none.
        answerContinue();
        await vi.waitFor(() => expect(asked()).not.toBeNull());
        await vi.waitFor(() => expect(text()).toContain('has 0 contacts.'));
        await settle();
        expect(reads()).toBe(afterDeletion + 1);
        expect(continues()).toBe(1); // the answer is never sent again
    });
});
