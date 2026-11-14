// The Session panel is the QSO-list surface (this sitting's contacts) with its own
// Export… button. Shared by Phone/CW (a tile) and FT8 (a rail-toggled overlay);
// Export… opens the download/email dialog.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';

const fetchQso = vi.fn();
const patchQso = vi.fn();
vi.mock('../api/qso-patch', () => ({
    fetchQso: (...a: unknown[]): unknown => fetchQso(...a),
    patchQso: (...a: unknown[]): unknown => patchQso(...a),
}));

import SessionPanel from './SessionPanel.svelte';
import { addSessionQso, _resetSessionForTests } from './session.svelte';
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
    it('shows the empty state with Export disabled', () => {
        render(SessionPanel);
        expect(screen.getByText(/No QSOs logged/)).toBeInTheDocument();
        expect(screen.getByRole('button', { name: /Export/ })).toBeDisabled();
    });

    it('lists logged QSOs and Export opens the export dialog', async () => {
        addSessionQso({
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
        addSessionQso({
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
        addSessionQso({
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
        addSessionQso({
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
        addSessionQso(qso('W1ABC', 'FT8'));
        addSessionQso(qso('G0XYZ', 'SSB'));
        addSessionQso(qso('DL1ABC', 'CW'));
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
        addSessionQso(qso('W1ABC'));
        addSessionQso(qso('VK5GR'));
        addSessionQso(qso('G0XYZ'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: '5gr' } });

        expect(screen.getByText('VK5GR')).toBeInTheDocument();
        expect(screen.queryByText('W1ABC')).toBeNull();
        expect(screen.queryByText('G0XYZ')).toBeNull();
    });

    it('clearing the search restores every row', async () => {
        addSessionQso(qso('W1ABC'));
        addSessionQso(qso('VK5GR'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'VK' } });
        expect(screen.queryByText('W1ABC')).toBeNull();
        await fireEvent.input(box, { target: { value: '' } });

        expect(screen.getByText('W1ABC')).toBeInTheDocument();
        expect(screen.getByText('VK5GR')).toBeInTheDocument();
    });

    it('keeps the header count at the sitting total while filtering', async () => {
        addSessionQso(qso('W1ABC'));
        addSessionQso(qso('VK5GR'));
        render(SessionPanel);

        const box = screen.getByRole('searchbox', { name: /Search session callsigns/ });
        await fireEvent.input(box, { target: { value: 'VK' } });

        expect(screen.getByRole('heading', { name: /Session \(2\)/ })).toBeInTheDocument();
    });

    it('a no-match search reads differently from an empty session', async () => {
        addSessionQso(qso('W1ABC'));
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
        addSessionQso({
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
