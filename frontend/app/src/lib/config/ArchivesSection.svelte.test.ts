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
        expect(screen.getByTestId('activation-error')).toHaveTextContent('holds archive y');
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

    it('a malformed callsign marks the field and holds the submit; a valid one releases it', async () => {
        await renderLoaded();
        await fireEvent.input(screen.getByLabelText('Label'), { target: { value: 'Drill' } });
        await fireEvent.input(screen.getByLabelText('First logbook'), {
            target: { value: 'Drill' },
        });
        const field = screen.getByLabelText('Logbook callsign');
        await fireEvent.input(field, { target: { value: 'g' } });
        expect(screen.getByTestId('callsign-error')).toBeInTheDocument();
        expect(field).toHaveAttribute('aria-invalid', 'true');
        expect(screen.getByRole('button', { name: 'Create archive' })).toBeDisabled();
        await fireEvent.input(field, { target: { value: 'abcdef' } }); // letters only: no digit
        expect(screen.getByRole('button', { name: 'Create archive' })).toBeDisabled();
        await fireEvent.input(field, { target: { value: '7q5mlv' } });
        expect(screen.queryByTestId('callsign-error')).toBeNull();
        expect(screen.getByRole('button', { name: 'Create archive' })).toBeEnabled();
        vi.mocked(createQsoArchive).mockResolvedValue({
            kind: 'refused',
            code: 'x',
            message: 'no',
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Create archive' }));
        await flush();
        expect(createQsoArchive).toHaveBeenCalledTimes(1);
        expect(vi.mocked(createQsoArchive).mock.calls[0][0].logbookCallsign).toBe('7Q5MLV');
    });

    // ADR 0082: a new archive's logbooks start unbound — SM Cloud included —
    // and bindings are edited only on the active archive. Said before the
    // operator creates one, so an archive that uploads nowhere is expected.
    it('the create form says a new archive starts with every destination off', async () => {
        await renderLoaded();
        expect(screen.getByTestId('new-archive-destinations')).toHaveTextContent(
            /every destination starts off.*Forwarding tab once it is active/
        );
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
