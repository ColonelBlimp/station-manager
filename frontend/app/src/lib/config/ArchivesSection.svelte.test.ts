import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import ArchivesSection from './ArchivesSection.svelte';
import { activateQsoArchive, createQsoArchive, fetchQsoArchives } from '../api/qso-archives';
import { fetchDaemonInstance, waitForDaemonBack } from '../api/restart';
import { archivesState, loadArchives, _resetArchivesForTests } from './archives.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

/*
    ARCHIVES TAB — WHAT THE OPERATOR SEES (ADR 0071 AC 4, honest state).
    The state column is the daemon's word; Activate appears only where it can be
    acted on (never on the active archive, disabled while a candidate is pending);
    the create form sends one request key per attempt and keeps it on a refusal.
*/

const HOME = {
    id: 'a',
    label: 'Home',
    ownership: 'legacy',
    state: 'active',
    lastActivationError: '',
    lastActivationCode: '',
    sizeBytes: 2048,
    modifiedAt: '2026-09-23T10:00:00Z',
} as const;
const CONTEST = {
    id: 'b',
    label: 'Contest',
    ownership: 'managed',
    state: 'inactive',
    lastActivationError: 'the file at /x holds archive y',
    lastActivationCode: '',
    sizeBytes: null,
    modifiedAt: null,
} as const;

const flush = () => new Promise((r) => setTimeout(r, 0));

async function renderLoaded(archives: unknown[] = [HOME, CONTEST]) {
    vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'ok', archives } as never);
    render(ArchivesSection);
    await flush();
    flushSync();
}

beforeEach(() => {
    vi.mocked(fetchQsoArchives).mockReset();
    vi.mocked(createQsoArchive).mockReset();
    vi.mocked(activateQsoArchive).mockReset();
    vi.mocked(fetchDaemonInstance).mockReset();
    vi.mocked(waitForDaemonBack).mockReset();
    _resetArchivesForTests();
    resetToasts();
});
afterEach(() => vi.restoreAllMocks());

describe('ArchivesSection', () => {
    it('lists each archive with the daemon state, and Activate only off the active one', async () => {
        await renderLoaded();
        expect(screen.getByTestId('state-a')).toHaveTextContent('Active');
        expect(screen.getByTestId('state-b')).toHaveTextContent('Inactive');
        // A failed activation is a ⚠ after the label (inbox 2026-09-24/26): the
        // reason is out of the row's flow, so it cannot push the columns. It is a
        // BUTTON, so a keyboard reaches it (Codex P2 on 23508984), described by a
        // tooltip that hover and focus both reveal.
        const glyph = screen.getByRole('button', { name: 'Last activation failed' });
        const tip = document.getElementById(glyph.getAttribute('aria-describedby')!)!;
        expect(tip.getAttribute('role')).toBe('tooltip');
        expect(tip.textContent).toMatch(/^Last activation failed: .*holds archive y/);
        expect(tip.className).toMatch(/\bhidden\b/);
        expect(tip.className).toMatch(/peer-focus:block/);
        expect(tip.className).toMatch(/peer-hover:block/);
        expect(tip.className).toMatch(/\babsolute\b/);
        expect(screen.getAllByRole('button', { name: 'Last activation failed' })).toHaveLength(1);
        expect(screen.queryByRole('button', { name: 'Activate Home' })).toBeNull();
        expect(screen.getByRole('button', { name: 'Activate Contest' })).toBeEnabled();
        expect(screen.getByText('2 KB')).toBeInTheDocument();
    });

    it('a failed re-read shows the retained list as stale, with Retry, and disables Activate', async () => {
        await renderLoaded();
        vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'error', message: 'daemon away' });
        await loadArchives();
        flushSync();
        expect(screen.getByTestId('stale')).toHaveTextContent('daemon away');
        expect(screen.getByTestId('state-a')).toHaveTextContent('Active');
        expect(screen.getByRole('button', { name: 'Activate Contest' })).toBeDisabled();
        vi.mocked(fetchQsoArchives).mockResolvedValue({ kind: 'ok', archives: [HOME, CONTEST] });
        await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        await flush();
        flushSync();
        expect(screen.queryByTestId('stale')).toBeNull();
        expect(screen.getByRole('button', { name: 'Activate Contest' })).toBeEnabled();
    });

    it('a pending candidate is named and every Activate is disabled', async () => {
        await renderLoaded([
            { ...HOME },
            { ...CONTEST, state: 'pending', lastActivationError: '' },
            { ...CONTEST, id: 'c', label: 'Third' },
        ]);
        expect(screen.getByRole('status')).toHaveTextContent('“Contest” is pending');
        expect(screen.getByTestId('state-b')).toHaveTextContent('Pending restart');
        expect(screen.getByRole('button', { name: 'Activate Third' })).toBeDisabled();
    });

    it('Activate asks first; a declined confirmation sends nothing', async () => {
        await renderLoaded();
        vi.spyOn(window, 'confirm').mockReturnValue(false);
        await fireEvent.click(screen.getByRole('button', { name: 'Activate Contest' }));
        await flush();
        expect(window.confirm).toHaveBeenCalledWith(
            expect.stringContaining('Switch to the archive “Contest”')
        );
        expect(activateQsoArchive).not.toHaveBeenCalled();
    });

    it('Activate confirmed: the request goes out and the restart is awaited', async () => {
        await renderLoaded();
        vi.spyOn(window, 'confirm').mockReturnValue(true);
        vi.mocked(fetchDaemonInstance).mockResolvedValue('i1');
        vi.mocked(activateQsoArchive).mockResolvedValue({
            kind: 'accepted',
            id: 'b',
            durability: 'durable',
        });
        vi.mocked(waitForDaemonBack).mockResolvedValue(true);
        await fireEvent.click(screen.getByRole('button', { name: 'Activate Contest' }));
        await flush();
        await flush();
        expect(activateQsoArchive).toHaveBeenCalledWith('b');
        expect(waitForDaemonBack).toHaveBeenCalledWith('i1');
    });

    it('the create form sends the fields with one request key and keeps it on a refusal', async () => {
        await renderLoaded();
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'invalid_field_value',
            message: 'no',
        });
        await fireEvent.input(screen.getByLabelText('Label'), { target: { value: 'Contest 2' } });
        await fireEvent.input(screen.getByLabelText('First logbook'), {
            target: { value: 'Contest' },
        });
        await fireEvent.input(screen.getByLabelText('Logbook callsign'), {
            target: { value: 'g4abc' },
        });
        // Uppercase as typed (drill 1, 2026-09-24): the field shows what the daemon stores.
        expect(screen.getByLabelText('Logbook callsign')).toHaveValue('G4ABC');
        const submit = screen.getByRole('button', { name: 'Create archive' });
        expect(submit).toBeEnabled();
        await fireEvent.click(submit);
        await flush();
        const first = vi.mocked(createQsoArchive).mock.calls[0][0];
        expect(first).toMatchObject({
            label: 'Contest 2',
            logbookName: 'Contest',
            logbookCallsign: 'G4ABC',
        });
        expect(first.requestKey).not.toBe('');
        // Refused: the form and its key survive for the retry.
        expect(screen.getByLabelText('Label')).toHaveValue('Contest 2');
        await fireEvent.click(screen.getByRole('button', { name: 'Create archive' }));
        await flush();
        expect(vi.mocked(createQsoArchive).mock.calls[1][0].requestKey).toBe(first.requestKey);
        expect(archivesState.creating).toBe(false);
    });

    // Inbox 2026-09-26: the tab explains by ⓘ link, not by paragraph. The list
    // links the QSO Archives chapter; the create form links "Creating an
    // archive", which carries what the form used to say — including that a
    // new archive uploads nowhere (ADR 0082 part 9, moved to the manual).
    it('each section links its manual section instead of explaining', async () => {
        await renderLoaded();
        const list = screen.getByRole('link', { name: 'How archives work' });
        expect(list.getAttribute('href')).toBe('/manual/#qso-archives');
        const create = screen.getByRole('link', { name: 'How creating an archive works' });
        expect(create.getAttribute('href')).toBe('/manual/#creating-an-archive');
        for (const l of [list, create]) {
            expect(l.getAttribute('target')).toBe('_blank');
            expect(l.closest('h2')).toBeNull();
        }
        expect(document.body.textContent).not.toMatch(/Each archive is a separate/);
        expect(document.body.textContent).not.toMatch(/Creates an empty archive/);
    });

    it('a created archive clears the form', async () => {
        await renderLoaded();
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'ok',
            archive: { ...CONTEST, id: 'n', label: 'New' },
            reused: false,
        });
        await fireEvent.input(screen.getByLabelText('Label'), { target: { value: 'New' } });
        await fireEvent.input(screen.getByLabelText('First logbook'), { target: { value: 'L' } });
        await fireEvent.input(screen.getByLabelText('Logbook callsign'), {
            target: { value: 'G4ABC' },
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Create archive' }));
        await flush();
        flushSync();
        expect(screen.getByLabelText('Label')).toHaveValue('');
    });
});
