// The Session panel is the QSO-list surface (this sitting's contacts) with its own
// Export… button. Shared by Phone/CW (a tile) and FT8 (a rail-toggled overlay);
// Export… opens the download/email dialog.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

const fetchQso = vi.fn();
const patchQso = vi.fn();
vi.mock('../api/qso-patch', () => ({
    fetchQso: (...a: unknown[]): unknown => fetchQso(...a),
    patchQso: (...a: unknown[]): unknown => patchQso(...a),
}));

import SessionPanel from './SessionPanel.svelte';
import {
    sessionStateDrawn,
    setSessionPorts,
    _resetSessionForTests,
    type SessionRowsReadLike,
} from './session.svelte';
import { addSessionRow } from '../../test/sessionRows';
import { TEST_ARCHIVE } from '../../test/archive';
import { sessionContactStored } from './session.svelte';
import {
    readSessionState,
    setRecoveryPorts,
    _resetRecoveryForTests,
    type SessionReadLike,
} from '../recovery.svelte';
import { noteDaemonDate, _resetDaemonClockForTests } from '../api/daemonClock.svelte';
import { flushSync } from 'svelte';
import { sessionEdit } from './sessionEdit.svelte';
import { operate, closeExport } from './state.svelte';

beforeEach(() => {
    _resetSessionForTests();
    closeExport();
    sessionEdit.close();
    sessionEdit.openError = null;
    fetchQso.mockReset();
});

describe('SessionPanel', () => {
    it('shows no rows with Export disabled', () => {
        render(SessionPanel);
        expect(screen.getByTestId('session-empty')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /Export/ })).toBeDisabled();
    });

    it('lists logged QSOs and Export opens the export dialog', async () => {
        addSessionRow({
            callsign: 'W1ABC',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: 'United States',
            comment: '',
        });
        render(SessionPanel);

        expect(screen.getByText('W1ABC')).toBeInTheDocument();
        const exportBtn = screen.getByRole('button', { name: /Export/ });
        expect(exportBtn).not.toBeDisabled();

        await fireEvent.click(exportBtn);
        expect(operate.exportOpen).toBe(true);
    });

    // The Map button was removed as redundant (operator, 2026-07-25): the sidebar's
    // always-visible Map link opens the identical tab. Coverage for the link itself
    // moved with it, to Sidebar.svelte.test.ts — it did not disappear.
    it('does not duplicate the sidebar Map link on the tile', () => {
        render(SessionPanel);
        expect(screen.queryByRole('link', { name: /Map/ })).toBeNull();
    });

    it('opens the in-place edit modal from a row callsign (no route change)', async () => {
        addSessionRow({
            uuid: 'u-9',
            callsign: 'ZS6DX',
            timeOn: '15:00:00',
            band: '40m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: 'South Africa',
            comment: '',
        });
        fetchQso.mockResolvedValue({
            kind: 'ok',
            qso: { id: 3, uuid: 'u-9', call: 'ZS6DX', band: '40m' },
        });
        render(SessionPanel);

        await fireEvent.click(screen.getByRole('button', { name: 'ZS6DX' }));
        expect(fetchQso).toHaveBeenCalledWith('u-9');
        // The shared EditQsoModal mounts in place, seeded with the row.
        expect(await screen.findByRole('dialog', { name: 'Edit QSO' })).toBeInTheDocument();
    });

    it('renders a uuid-less row as plain text (no edit affordance)', () => {
        addSessionRow({
            callsign: 'OLDROW',
            timeOn: '15:01:00',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '59',
            name: '',
            country: '',
            comment: '',
        });
        render(SessionPanel);
        expect(screen.getByText('OLDROW')).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: 'OLDROW' })).toBeNull();
    });

    it('surfaces a hydration failure as a panel error line', async () => {
        addSessionRow({
            uuid: 'u-10',
            callsign: 'G0FAIL',
            timeOn: '15:02:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });
        fetchQso.mockResolvedValue({ kind: 'error', message: 'Cannot reach the daemon.' });
        render(SessionPanel);

        await fireEvent.click(screen.getByRole('button', { name: 'G0FAIL' }));
        expect(await screen.findByTestId('session-edit-error')).toHaveTextContent('G0FAIL');
    });
});

describe('SessionPanel header count', () => {
    const qso = (callsign: string, mode: string) => ({
        callsign,
        timeOn: '14:30:00',
        band: '20m',
        mode,
        rstSent: '',
        rstRcvd: '',
        name: '',
        country: '',
        comment: '',
    });

    it('shows (0) on an empty session', () => {
        render(SessionPanel);
        expect(screen.getByRole('heading', { name: /Session \(0\)/ })).toBeInTheDocument();
    });

    // The tally spans BOTH modes — session.qsos is fed by the Phone/CW submit sink
    // and the FT8 ft8-logged SSE alike, so the header must not read as per-mode.
    it('counts FT8 and Phone/CW contacts together', () => {
        addSessionRow(qso('W1ABC', 'FT8'));
        addSessionRow(qso('G0XYZ', 'SSB'));
        addSessionRow(qso('DL1ABC', 'CW'));
        render(SessionPanel);
        expect(screen.getByRole('heading', { name: /Session \(3\)/ })).toBeInTheDocument();
    });
});

/*
    CALLSIGN SEARCH — dogfood 2026-08-07 ("add a simple search as you type
    field to the session panel for callsigns"). A long FT8 sitting runs to
    dozens of rows; finding whether/when a call was worked means scrolling.
    Rules:
    - substring match, case-insensitive (subsumes prefix; "5gr" finds VK5GR —
      the fixture differentiates a prefix-only or case-sensitive cut);
    - the filter is a VIEW: the header count stays the sitting's total;
    - the no-match state must be tellable from the nearest confusable state,
      an EMPTY SESSION — different message, and only while a filter is typed.
*/
describe('SessionPanel callsign search', () => {
    const qso = (callsign: string) => ({
        callsign,
        timeOn: '14:30:00',
        band: '20m',
        mode: 'FT8',
        rstSent: '',
        rstRcvd: '',
        name: '',
        country: '',
        comment: '',
    });

    it('narrows rows to callsigns containing the typed text, case-insensitive', async () => {
        addSessionRow(qso('W1ABC'));
        addSessionRow(qso('VK5GR'));
        addSessionRow(qso('G0XYZ'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: '5gr' } });

        expect(screen.getByText('VK5GR')).toBeInTheDocument();
        expect(screen.queryByText('W1ABC')).toBeNull();
        expect(screen.queryByText('G0XYZ')).toBeNull();
    });

    it('clearing the search restores every row', async () => {
        addSessionRow(qso('W1ABC'));
        addSessionRow(qso('VK5GR'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'VK' } });
        expect(screen.queryByText('W1ABC')).toBeNull();
        await fireEvent.input(box, { target: { value: '' } });

        expect(screen.getByText('W1ABC')).toBeInTheDocument();
        expect(screen.getByText('VK5GR')).toBeInTheDocument();
    });

    it('keeps the header count at the sitting total while filtering', async () => {
        addSessionRow(qso('W1ABC'));
        addSessionRow(qso('VK5GR'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'VK' } });

        expect(screen.getByRole('heading', { name: /Session \(2\)/ })).toBeInTheDocument();
    });

    it('a no-match search reads differently from an empty session', async () => {
        addSessionRow(qso('W1ABC'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'ZZZZ' } });

        expect(screen.getByText(/No callsigns match/)).toBeInTheDocument();
        expect(screen.queryByText(/No QSOs logged/)).toBeNull();
    });
});

/*
  The Session list never scrolls sideways (operator, dogfood 2026-11-13).

  Measured in the desktop app's Chromium (Electron 44), not here: jsdom lays
  nothing out. Once the list is long enough to scroll down, the vertical
  scrollbar takes 12px of the list's 630px (the w-2xl card less its padding and
  border). The table has a fixed layout and a width of 100%, so it is wider than
  that space only when the columns that state a width add up to more than it.
  When all eight stated one they added up to 628px against the 618px left, and
  the list scrolled sideways by 10px.

  So what this pins is the arrangement that cannot overflow: Name and Country
  state no width and share whatever is left, and the six that do state one leave
  them room. The nearest wrong fix hides the sideways overflow instead, which
  cuts the Country column off at the panel's edge.
*/
describe('SessionPanel list width', () => {
    const LIST_WIDTH_WITH_SCROLLBAR = 618; // px, measured: 672 card - 40 padding - 2 border - 12 bar
    const SPACING = 4; // px per Tailwind width unit (w-20 = 80px)

    const header = (name: string): HTMLElement => screen.getByRole('columnheader', { name });
    const statedWidth = (el: Element): number | null => {
        const m = /(?:^|\s)w-(\d+)(?:\s|$)/.exec(el.className);
        return m ? Number(m[1]) * SPACING : null;
    };

    beforeEach(() => {
        addSessionRow({
            callsign: 'W1ABC',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '-15',
            rstRcvd: '-18',
            name: 'Alexandr Gazizovich Verylongname',
            country: 'Bosnia-Herzegovina',
            comment: '',
        });
        render(SessionPanel);
    });

    it('lays the table out by its header widths, as wide as the list', () => {
        const table = screen.getByRole('table');
        expect(table).toHaveClass('table-fixed', 'w-full');
    });

    it('lets Name and Country share the space the other columns leave', () => {
        expect(statedWidth(header('Name'))).toBeNull();
        expect(statedWidth(header('Country'))).toBeNull();
    });

    it('keeps the stated column widths within the list beside its scrollbar', () => {
        const stated = screen
            .getAllByRole('columnheader')
            .map(statedWidth)
            .filter((w): w is number => w !== null);
        expect(stated).toHaveLength(6);
        const total = stated.reduce((a, b) => a + b, 0);
        // Name and Country get what is left; 100px each still reads.
        expect(total).toBeLessThanOrEqual(LIST_WIDTH_WITH_SCROLLBAR - 200);
    });

    it('cuts a long name or country short with an ellipsis rather than widening the table', () => {
        for (const text of ['Alexandr Gazizovich Verylongname', 'Bosnia-Herzegovina']) {
            const cell = screen.getByText(text);
            expect(cell).toHaveClass('overflow-hidden', 'text-nowrap', 'text-ellipsis');
            expect(statedWidth(cell)).toBeNull();
        }
    });

    it('does not hide sideways overflow, which would cut the last column off', () => {
        const list = screen.getByRole('table').parentElement as HTMLElement;
        expect(list.className).not.toMatch(/overflow-(x-)?(hidden|clip)/);
    });
});

/*
  A long callsign stays in its own column (operator, 2026-11-16: "widen to 136 px
  with the ellipsis backstop").

  Measured in Electron 44: at the list's 14px medium weight, 13-character
  callsigns drew between 113px and 133px (VP8/SP9ABC/MM, KH0/WA6MWM/MM) and ran
  over the Band column, because the column was 108px and its cell neither
  wrapped nor clipped. The column is now 136px. Anything wider still is cut
  short with an ellipsis, in the cell and in the edit button inside it, and the
  whole callsign is in the tooltip. jsdom lays nothing out, so these pin that
  arrangement; the widths were measured separately.
*/
describe('SessionPanel long callsigns', () => {
    const LONG = 'KH0/WA6MWM/MM';
    const row = (uuid?: string) => ({
        uuid,
        callsign: LONG,
        timeOn: '14:30:00',
        band: '160m',
        mode: 'FT8',
        rstSent: '-15',
        rstRcvd: '-18',
        name: '',
        country: '',
        comment: '',
    });
    const CLIPPED = ['overflow-hidden', 'text-nowrap', 'text-ellipsis'];

    it('gives the Callsign column 136px', () => {
        addSessionRow(row('u-1'));
        render(SessionPanel);
        expect(screen.getByRole('columnheader', { name: 'Callsign' })).toHaveClass('w-34');
    });

    it('cuts an editable callsign short inside its cell, never over the next column', () => {
        addSessionRow(row('u-1'));
        render(SessionPanel);
        const button = screen.getByRole('button', { name: LONG });
        // The button is the box that overflows, so it is the one that must be
        // held to the cell's width and clipped.
        expect(button).toHaveClass('max-w-full', ...CLIPPED);
        expect(button.closest('td')).toHaveClass(...CLIPPED);
    });

    it('cuts a plain-text callsign short too', () => {
        addSessionRow(row(undefined));
        render(SessionPanel);
        expect(screen.getByText(LONG).closest('td')).toHaveClass(...CLIPPED);
    });

    it('keeps the whole callsign readable in the tooltip, with the edit hint', () => {
        addSessionRow(row('u-1'));
        render(SessionPanel);
        const title = screen.getByRole('button', { name: LONG }).getAttribute('title') ?? '';
        expect(title.startsWith(LONG)).toBe(true);
        expect(title).toMatch(/edit this QSO/i);
    });

    it('keeps the whole callsign readable in the tooltip of a plain-text row', () => {
        addSessionRow(row(undefined));
        render(SessionPanel);
        expect(screen.getByText(LONG).closest('td')).toHaveAttribute('title', LONG);
    });
});

// What the panel says when it has no rows depends on WHY (W-0029 part 6, B7 and
// C). "No QSOs logged this session" is one answer among five, and is said only
// when the daemon has answered that the open session has no contact.
describe('SessionPanel states', () => {
    const SESSION = '01a1f83c-da20-7000-8000-0000000000a1';
    const flush = () => new Promise((r) => setTimeout(r, 0));

    // An open session whose rows read is answered by `answer` (never, when null).
    function openSession(
        answer: SessionRowsReadLike | null,
        openedAt = '2026-11-20T09:15:00Z'
    ): void {
        setSessionPorts({
            readRows: () =>
                answer === null
                    ? new Promise<SessionRowsReadLike>(() => {})
                    : Promise.resolve(answer),
            readState: () => Promise.resolve(true),
            archive: () => TEST_ARCHIVE,
        });
        sessionStateDrawn({
            archiveId: TEST_ARCHIVE,
            generation: 4,
            state: 'open',
            session: { id: SESSION, openedAt, sittingStartedAt: openedAt },
        });
    }
    const rowsOf = (calls: string[]): SessionRowsReadLike => ({
        kind: 'ok',
        archiveId: TEST_ARCHIVE,
        generation: 4,
        sessionId: SESSION,
        members: calls.map((call, i) => ({
            uuid: `01a21282-0000-7000-8000-00000000000${i}`,
            call,
            timeOn: '143000',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '59',
            name: '',
            country: '',
            comment: '',
            emailed: false,
        })),
    });
    const empty = (): string => screen.getByTestId('session-empty').textContent.trim();

    it('before any state is drawn it does not claim an empty session', () => {
        render(SessionPanel);
        expect(empty()).toBe('No session is open.');
    });

    it('says no session is open, with the count at 0 (C2)', () => {
        sessionStateDrawn({ archiveId: TEST_ARCHIVE, generation: 3, state: 'none', session: null });
        render(SessionPanel);
        expect(empty()).toBe('No session is open.');
        expect(screen.getByRole('heading', { level: 3 })).toHaveTextContent('Session (0)');
        expect(screen.queryByTestId('session-started')).not.toBeInTheDocument();
    });

    it('says the archive is being switched (C5)', () => {
        sessionStateDrawn({
            archiveId: TEST_ARCHIVE,
            generation: 3,
            state: 'archive_switching',
            session: null,
        });
        render(SessionPanel);
        expect(empty()).toBe('The archive is being switched.');
    });

    it('says it is reading while the rows are out', () => {
        openSession(null);
        render(SessionPanel);
        expect(empty()).toBe('Reading the session…');
    });

    it('says the rows could not be read, never that the session is empty (B7)', async () => {
        openSession({ kind: 'error', message: 'Cannot reach the daemon.' });
        render(SessionPanel);
        await flush();
        expect(empty()).toBe("The session's contacts could not be read. Cannot reach the daemon.");
        expect(screen.queryByText(/No QSOs logged/)).not.toBeInTheDocument();
    });

    it('says no QSOs are logged only when the daemon answers an empty session', async () => {
        openSession(rowsOf([]));
        render(SessionPanel);
        await flush();
        expect(empty()).toBe('No QSOs logged this session.');
    });

    it('lists the daemon’s rows and says when the session started (ruling 1)', async () => {
        openSession(rowsOf(['K2B', 'G4A']));
        render(SessionPanel);
        await flush();
        expect(screen.queryByTestId('session-empty')).not.toBeInTheDocument();
        expect(screen.getByRole('heading', { level: 3 })).toHaveTextContent('Session (2)');
        expect(screen.getByText('K2B')).toBeInTheDocument();
        // Not today's start: the date goes with the time.
        expect(screen.getByTestId('session-started')).toHaveTextContent(
            'Started 2026-11-20 09:15 UTC'
        );
    });
});

// The same, with the recovery gate reading the state as main.ts wires it
// (codex-273): the page waits for the next event after a read fails, and while it
// waits the panel must not say something it does not know.
describe('SessionPanel through the recovery gate, after a read has failed', () => {
    const SESSION = '01a1f83c-da20-7000-8000-0000000000a1';
    const flush = () => new Promise((r) => setTimeout(r, 0));
    const empty = (): string => screen.getByTestId('session-empty').textContent.trim();
    const state = (generation: number): SessionReadLike => ({
        kind: 'ok',
        state: {
            archiveId: TEST_ARCHIVE,
            generation,
            state: 'open',
            session: {
                id: SESSION,
                openedAt: '2026-11-25T09:15:00Z',
                sittingStartedAt: '2026-11-25T09:15:00Z',
                contactCount: 0,
                heldByYou: true,
            },
        },
    });
    const rowsAt = (generation: number): SessionRowsReadLike => ({
        kind: 'ok',
        archiveId: TEST_ARCHIVE,
        generation,
        sessionId: SESSION,
        members: [],
    });
    const failed = { kind: 'error', message: 'Cannot reach the daemon.' } as const;

    // The daemon: each read takes the next answer queued for it.
    let states: SessionReadLike[];
    let rows: SessionRowsReadLike[];
    beforeEach(() => {
        _resetRecoveryForTests();
        states = [];
        rows = [];
        setRecoveryPorts({
            read: () => Promise.resolve(states.shift() ?? failed),
            answer: () => Promise.resolve({ kind: 'failed', message: 'not used' }),
            archive: () => TEST_ARCHIVE,
            drawn: sessionStateDrawn,
        });
        setSessionPorts({
            readRows: () => Promise.resolve(rows.shift() ?? failed),
            readState: readSessionState,
            archive: () => TEST_ARCHIVE,
        });
    });

    it('an empty session whose next read fails is not still said to be empty', async () => {
        states.push(state(4));
        rows.push(rowsAt(4)); // the daemon answers: no contact
        await readSessionState();
        render(SessionPanel);
        await flush();
        expect(empty()).toBe('No QSOs logged this session.');

        sessionContactStored(SESSION); // a contact joined; the read for it fails
        await flush();
        flushSync();

        expect(empty()).toBe("The session's contacts could not be read. Cannot reach the daemon.");
    });

    it('rows of a later generation, with the state then unreadable, are not left as "Reading"', async () => {
        states.push(state(4)); // then the state cannot be read again
        rows.push(rowsAt(5)); // the daemon has moved on since the state was read
        await readSessionState();
        render(SessionPanel);
        await flush();
        await flush();
        flushSync();

        expect(empty()).toMatch(/^The session's contacts could not be read\./);
        expect(screen.queryByText(/Reading the session/)).not.toBeInTheDocument();
    });
});

// "Started 09:15 UTC" is true only while 09:15 is today's (ruling 1). A panel
// left open across midnight UTC must come to say the date (codex-273), by the
// daemon's clock.
describe('SessionPanel start line across midnight UTC', () => {
    const SESSION = '01a1f83c-da20-7000-8000-0000000000a1';
    afterEach(() => {
        vi.useRealTimers();
        _resetDaemonClockForTests();
    });

    it('gains its date when the start becomes yesterday’s', () => {
        vi.useFakeTimers();
        vi.setSystemTime(Date.parse('2026-11-25T23:59:58Z'));
        _resetDaemonClockForTests();
        setSessionPorts({
            readRows: () => new Promise(() => {}),
            readState: () => Promise.resolve(true),
            archive: () => TEST_ARCHIVE,
        });
        sessionStateDrawn({
            archiveId: TEST_ARCHIVE,
            generation: 4,
            state: 'open',
            session: {
                id: SESSION,
                openedAt: '2026-11-25T09:15:00Z',
                sittingStartedAt: '2026-11-25T09:15:00Z',
            },
        });
        render(SessionPanel);
        expect(screen.getByTestId('session-started')).toHaveTextContent('Started 09:15 UTC');

        vi.advanceTimersByTime(4000); // now 00:00:02 on the 26th
        flushSync();
        expect(screen.getByTestId('session-started')).toHaveTextContent(
            'Started 2026-11-25 09:15 UTC'
        );
    });

    it('judges today by the daemon’s clock, not the window’s', () => {
        vi.useFakeTimers();
        vi.setSystemTime(Date.parse('2026-11-26T00:03:00Z')); // the window is five minutes fast
        noteDaemonDate(new Date(Date.parse('2026-11-25T23:58:00Z')).toUTCString());
        setSessionPorts({
            readRows: () => new Promise(() => {}),
            readState: () => Promise.resolve(true),
            archive: () => TEST_ARCHIVE,
        });
        sessionStateDrawn({
            archiveId: TEST_ARCHIVE,
            generation: 4,
            state: 'open',
            session: {
                id: SESSION,
                openedAt: '2026-11-25T09:15:00Z',
                sittingStartedAt: '2026-11-25T09:15:00Z',
            },
        });
        render(SessionPanel);
        expect(screen.getByTestId('session-started')).toHaveTextContent('Started 09:15 UTC');
    });
});

// W-0029 part 6, slice 3: the panel's controls, and what it says of a control
// that was not applied (criteria E, F; the operator's rulings of 2026-11-27).
describe('SessionPanel controls', () => {
    const SENTENCE = 'Contacts stay in the session they were logged in.';
    const S1 = '01a1f83c-da20-7000-8000-0000000000a1';
    let listReads: number;
    let stateReads: number;

    beforeEach(async () => {
        _resetRecoveryForTests();
        const list = await import('./continueList.svelte');
        list._resetContinueForTests();
        listReads = 0;
        stateReads = 0;
        list.setContinuePorts({
            read: () => {
                listReads++;
                return new Promise(() => {});
            },
            archive: () => TEST_ARCHIVE,
            readState: () => {},
            send: () => Promise.resolve('applied'),
        });
        setSessionPorts({
            readRows: () => new Promise(() => {}),
            readState: () => Promise.resolve(true),
            archive: () => TEST_ARCHIVE,
        });
    });

    async function shown(
        state: 'open' | 'none' | 'archive_switching',
        available: 'unread' | 'some' | 'none' | 'unreadable'
    ): Promise<void> {
        const list = await import('./continueList.svelte');
        const drawn = {
            archiveId: TEST_ARCHIVE,
            generation: 4,
            state,
            session:
                state === 'open'
                    ? {
                          id: S1,
                          openedAt: '2026-11-25T09:15:00Z',
                          sittingStartedAt: '2026-11-25T09:15:00Z',
                      }
                    : null,
        };
        sessionStateDrawn(drawn);
        list.continueStateDrawn(drawn);
        list.continueList.available = available;
        flushSync();
    }
    const continueButton = (): HTMLElement | null =>
        screen.queryByRole('button', { name: 'Continue…' });

    it.each([
        ['open', 'some'],
        ['none', 'some'],
        ['open', 'unreadable'],
    ] as const)(
        'offers Continue, with the sentence beside it, in "%s" when the list is "%s"',
        async (state, available) => {
            render(SessionPanel);
            await shown(state, available);
            expect(continueButton()).toBeEnabled();
            expect(screen.getByText(SENTENCE)).toBeInTheDocument();
        }
    );

    it.each([
        ['open', 'none'],
        ['open', 'unread'],
        ['archive_switching', 'some'],
    ] as const)('does not offer it in "%s" when the list is "%s"', async (state, available) => {
        render(SessionPanel);
        await shown(state, available);
        expect(continueButton()).toBeNull();
        expect(screen.queryByText(SENTENCE)).toBeNull();
    });

    it('pressing it opens the list, which is read then', async () => {
        const list = await import('./continueList.svelte');
        render(SessionPanel);
        await shown('open', 'some');
        const before = listReads;

        await fireEvent.click(continueButton()!);
        flushSync();

        expect(list.continueList.open).toBe(true);
        expect(listReads).toBe(before + 1);
        expect(screen.getByRole('dialog', { name: 'Continue an earlier session' })).toBeVisible();
    });

    it.each(['out', 'unknown'] as const)(
        'it cannot be pressed while a control is "%s"',
        async (standing) => {
            const { sessionControlStands } = await import('./session.svelte');
            render(SessionPanel);
            await shown('open', 'some');
            sessionControlStands(standing);
            flushSync();
            expect(continueButton()).toBeDisabled();
        }
    );

    it('says what a control that was not applied came to, in the words it was given', async () => {
        const { recovery } = await import('../recovery.svelte');
        render(SessionPanel);
        await shown('open', 'some');
        expect(screen.queryByRole('alert')).toBeNull();

        recovery.controlError =
            'The session changed before this could be done. Your choice was not applied.';
        flushSync();

        expect(screen.getByRole('alert').textContent.trim()).toBe(
            'The session changed before this could be done. Your choice was not applied.'
        );
        expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
    });

    it('when the outcome is not known: says so, that the rows are as last read, and "Try again" reads the state once', async () => {
        const { recovery } = await import('../recovery.svelte');
        const { sessionControlStands } = await import('./session.svelte');
        setRecoveryPorts({
            read: () => {
                stateReads++;
                return new Promise(() => {});
            },
            answer: () => Promise.reject(new Error('no control may be sent')),
            archive: () => TEST_ARCHIVE,
        });
        render(SessionPanel);
        await shown('open', 'some');
        addSessionRow({
            callsign: 'W1ABC',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });
        recovery.status = 'clear';
        recovery.unknown = true;
        recovery.controlError = 'Cannot reach the daemon.';
        sessionControlStands('unknown');
        flushSync();

        expect(screen.getByRole('alert').textContent.replace(/\s+/g, ' ').trim()).toMatch(
            /^Cannot reach the daemon\. The session could not be read\. The contacts below are as last read\./
        );
        expect(screen.getByText('W1ABC')).toBeInTheDocument(); // the rows stay

        await fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
        expect(stateReads).toBe(1);
        // One read at a time: it cannot be pressed again while that one is out.
        expect(screen.getByRole('button', { name: 'Try again' })).toBeDisabled();
    });

    // codex-291: a refusal that is certain (the choice was declined as stale)
    // is not replaced by the failure of the read that followed it.
    it('says what the control came to AND why the session could not be read, when both are so', async () => {
        const { recovery } = await import('../recovery.svelte');
        const { sessionControlStands } = await import('./session.svelte');
        render(SessionPanel);
        await shown('open', 'some');
        addSessionRow({
            callsign: 'W1ABC',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT8',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });
        recovery.controlError =
            'The session changed before this could be done. Your choice was not applied.';
        recovery.unreadError = 'Cannot reach the daemon.';
        sessionControlStands('unknown');
        flushSync();

        expect(screen.getByRole('alert').textContent.replace(/\s+/g, ' ').trim()).toMatch(
            /^The session changed before this could be done\. Your choice was not applied\. The session could not be read: Cannot reach the daemon\. The contacts below are as last read\./
        );
    });

    // codex-291: the count in the heading is as last read too, and must say so
    // whether or not there are rows under it ("Session (0)" is a claim).
    it.each([0, 1])(
        'marks the count as last read while the outcome is not known (%i rows), and not otherwise',
        async (rows) => {
            const { sessionControlStands } = await import('./session.svelte');
            render(SessionPanel);
            await shown('open', 'some');
            if (rows === 1) {
                addSessionRow({
                    callsign: 'W1ABC',
                    timeOn: '14:30:00',
                    band: '20m',
                    mode: 'FT8',
                    rstSent: '',
                    rstRcvd: '',
                    name: '',
                    country: '',
                    comment: '',
                });
            }
            flushSync();
            expect(screen.queryByTestId('session-last-read')).toBeNull();

            sessionControlStands('unknown');
            flushSync();
            expect(screen.getByTestId('session-last-read').textContent.trim()).toBe('last read');

            sessionControlStands('out');
            flushSync();
            expect(screen.queryByTestId('session-last-read')).toBeNull();
        }
    );

    it('when the outcome is not known and no rows are shown, the panel does not say the session is empty', async () => {
        const { recovery } = await import('../recovery.svelte');
        const { sessionControlStands } = await import('./session.svelte');
        render(SessionPanel);
        await shown('none', 'some');
        recovery.controlError = 'Cannot reach the daemon.';
        sessionControlStands('unknown');
        flushSync();

        expect(screen.getByTestId('session-empty').textContent.trim()).toBe(
            'The session could not be read.'
        );
        expect(screen.getByRole('alert').textContent).not.toMatch(/contacts below/);
    });
});
