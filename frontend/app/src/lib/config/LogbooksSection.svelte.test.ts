import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/svelte';
import { flushSync } from 'svelte';

/*
    SETTINGS → LOGBOOKS — WHAT THE OPERATOR SEES (W-0021 dossier, operator ruling
    2026-09-29). The active archive is named; each logbook shows its callsign,
    QSO count and a read-only Default tag. Adding says where live contacts go;
    SM Cloud is an explicit unticked option only where it can be turned on;
    Delete is disabled with its reason; nothing writes during an archive switch.
*/

vi.mock('../api/logbooks', () => ({
    fetchLogbooks: vi.fn(),
    fetchLogbookCount: vi.fn(),
    createLogbook: vi.fn(),
    renameLogbook: vi.fn(),
    deleteLogbook: vi.fn(),
}));
vi.mock('../api/seams', () => ({ fetchStationContext: vi.fn() }));
vi.mock('../api/qso-archives', () => ({
    fetchDaemonIdentity: vi.fn(),
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
}));
vi.mock('../api/archive-bindings', () => ({
    fetchArchiveBindings: vi.fn(),
    saveArchiveBindings: vi.fn(),
}));

import LogbooksSection from './LogbooksSection.svelte';
import {
    createLogbook,
    deleteLogbook,
    fetchLogbookCount,
    fetchLogbooks,
    renameLogbook,
} from '../api/logbooks';
import { fetchStationContext } from '../api/seams';
import { fetchDaemonIdentity } from '../api/qso-archives';
import { fetchArchiveBindings } from '../api/archive-bindings';
import { _resetLogbooksForTests } from './logbooks.svelte';
import { archivesState, _resetArchivesForTests } from './archives.svelte';
import { _resetForTests as resetToasts } from '../ui/toasts.svelte';

const flush = () => new Promise((r) => setTimeout(r, 0));

function daemonHas(smcloudReason: string | null = '', counts: Record<number, number> = {}) {
    vi.mocked(fetchLogbooks).mockResolvedValue({
        kind: 'ok',
        logbooks: [
            { id: 1, name: 'Default', callsign: '7Q5MLV' },
            { id: 2, name: 'Portable', callsign: '7Q5MLV/P' },
            { id: 3, name: 'Contest', callsign: '7Q5MLV' },
        ],
    });
    const n = { 1: 12, 2: 0, 3: 4, ...counts };
    vi.mocked(fetchLogbookCount).mockImplementation((id: number) =>
        Promise.resolve({ kind: 'ok', count: n[id as 1 | 2 | 3] })
    );
    vi.mocked(fetchStationContext).mockResolvedValue({
        configOk: true,
        stationCallsign: '7Q5MLV',
        logbookId: 1,
    } as never);
    vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i', archiveId: 'arch-a' });
    vi.mocked(fetchArchiveBindings).mockResolvedValue({
        kind: 'ok',
        bindings: {
            archive_id: 'arch-a',
            archive_label: 'Home',
            restart_required: false,
            destinations:
                smcloudReason === null
                    ? []
                    : ([
                          {
                              type: 'smcloud',
                              display_name: 'SM Cloud',
                              account: {
                                  configured: true,
                                  label: '',
                                  fields_set: [],
                                  build_key: '',
                              },
                              state: 'off',
                              reason: smcloudReason,
                              logbooks: [],
                          },
                      ] as never),
        },
    });
}

async function renderLoaded(props: { visible?: boolean } = {}) {
    const r = render(LogbooksSection, { props });
    await flush();
    await flush();
    flushSync();
    return r;
}

const cells = (id: number) =>
    within(screen.getByTestId(`logbook-row-${id}`))
        .getAllByRole('cell')
        .map((c) => c.textContent?.trim());

beforeEach(() => {
    vi.clearAllMocks();
    _resetLogbooksForTests();
    _resetArchivesForTests();
    resetToasts();
});
afterEach(() => vi.restoreAllMocks());

describe('LogbooksSection', () => {
    it('names the active archive and lists its logbooks, tagging the Default one', async () => {
        daemonHas();
        await renderLoaded();
        expect(screen.getByTestId('logbooks-archive')).toHaveTextContent('Home');
        expect(cells(1).slice(0, 3)).toEqual(['DefaultDefault', '7Q5MLV', '12 QSOs']);
        expect(cells(2).slice(0, 3)).toEqual(['Portable', '7Q5MLV/P', '0 QSOs']);
        expect(screen.getByTestId('logbook-default-1')).toHaveTextContent('Default');
        expect(screen.queryByTestId('logbook-default-2')).toBeNull();
        expect(screen.queryByTestId('logbook-default-3')).toBeNull();
    });

    it('reads the list again each time the tab opens', async () => {
        daemonHas();
        const { rerender } = await renderLoaded({ visible: true });
        const reads = vi.mocked(fetchLogbooks).mock.calls.length;
        await rerender({ visible: false });
        await flush();
        expect(vi.mocked(fetchLogbooks).mock.calls.length).toBe(reads);
        await rerender({ visible: true });
        await flush();
        expect(vi.mocked(fetchLogbooks).mock.calls.length).toBe(reads + 1);
    });

    it('Delete is disabled with its reason for the default and a logbook holding QSOs', async () => {
        daemonHas();
        await renderLoaded();
        const del = (name: string) => screen.getByRole('button', { name: `Delete ${name}` });
        expect(del('Default')).toBeDisabled();
        expect(del('Default')).toHaveAttribute('title', 'The Default logbook can’t be deleted');
        expect(del('Contest')).toBeDisabled();
        expect(del('Contest')).toHaveAttribute(
            'title',
            'Holds 4 QSOs — only an empty logbook can be deleted'
        );
        expect(del('Portable')).toBeEnabled();
    });

    it('Delete asks first; declined sends nothing, confirmed deletes', async () => {
        daemonHas();
        await renderLoaded();
        const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
        await fireEvent.click(screen.getByRole('button', { name: 'Delete Portable' }));
        expect(confirm).toHaveBeenCalledWith(expect.stringContaining('Portable'));
        expect(deleteLogbook).not.toHaveBeenCalled();
        confirm.mockReturnValue(true);
        vi.mocked(deleteLogbook).mockResolvedValue({ kind: 'ok' });
        await fireEvent.click(screen.getByRole('button', { name: 'Delete Portable' }));
        await flush();
        expect(deleteLogbook).toHaveBeenCalledWith(2);
    });

    it('Rename edits the name in place; Cancel sends nothing', async () => {
        daemonHas();
        await renderLoaded();
        await fireEvent.click(screen.getByRole('button', { name: 'Rename Portable' }));
        flushSync();
        const input = screen.getByRole('textbox', { name: 'New name for Portable' });
        expect(input).toHaveValue('Portable');
        await fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
        flushSync();
        expect(renameLogbook).not.toHaveBeenCalled();
        expect(screen.queryByRole('textbox', { name: 'New name for Portable' })).toBeNull();

        await fireEvent.click(screen.getByRole('button', { name: 'Rename Portable' }));
        flushSync();
        vi.mocked(renameLogbook).mockResolvedValue({ kind: 'ok' });
        await fireEvent.input(screen.getByRole('textbox', { name: 'New name for Portable' }), {
            target: { value: 'Field day' },
        });
        await fireEvent.click(screen.getByRole('button', { name: 'Save name' }));
        await flush();
        expect(renameLogbook).toHaveBeenCalledWith(2, 'Field day');
    });

    it('the add form prefills the station callsign and says where live contacts go', async () => {
        daemonHas();
        await renderLoaded();
        const call = screen.getByLabelText('Callsign');
        expect(call).toHaveValue('7Q5MLV');
        const note = screen.getByTestId('logbook-live-note');
        expect(note).toHaveTextContent('Live contacts keep going to the Default logbook.');
        expect(note).not.toHaveTextContent('different callsign');

        await fireEvent.input(call, { target: { value: '7q8ac' } });
        flushSync();
        expect(call).toHaveValue('7Q8AC');
        expect(screen.getByTestId('logbook-live-note')).toHaveTextContent(
            'A logbook with a different callsign can’t receive live contacts yet.'
        );
    });

    it('offers SM Cloud unticked only where this archive can turn it on', async () => {
        daemonHas('');
        const { unmount } = await renderLoaded();
        const box = screen.getByRole('checkbox', { name: /SM Cloud/ });
        expect(box).not.toBeChecked();
        unmount();

        _resetLogbooksForTests();
        daemonHas('station account is incomplete');
        await renderLoaded();
        expect(screen.queryByRole('checkbox', { name: /SM Cloud/ })).toBeNull();
    });

    it('adds a logbook with what was typed and clears the form', async () => {
        daemonHas();
        await renderLoaded();
        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 4 });
        const name = screen.getByLabelText('Name');
        await fireEvent.input(name, { target: { value: 'Portable 2' } });
        await fireEvent.click(screen.getByRole('button', { name: 'Add logbook' }));
        await flush();
        await flush();
        flushSync();
        expect(createLogbook).toHaveBeenCalledWith({ name: 'Portable 2', callsign: '7Q5MLV' });
        expect(name).toHaveValue('');
    });

    it('holds the submit for a missing name or a malformed callsign', async () => {
        daemonHas();
        await renderLoaded();
        const add = screen.getByRole('button', { name: 'Add logbook' });
        expect(add).toBeDisabled(); // no name yet
        await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'X' } });
        flushSync();
        expect(add).toBeEnabled();
        await fireEvent.input(screen.getByLabelText('Callsign'), { target: { value: 'Q' } });
        flushSync();
        expect(add).toBeDisabled();
    });

    // Operator ruling 2026-09-29: switching tabs inside Settings keeps the
    // drafts, unprompted — including across the tab's own re-read.
    it('keeps a typed Add form and an open rename across a tab switch', async () => {
        daemonHas();
        const { rerender } = await renderLoaded({ visible: true });
        await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Contest 2' } });
        await fireEvent.input(screen.getByLabelText('Callsign'), { target: { value: '7q8ac' } });
        await fireEvent.click(screen.getByRole('button', { name: 'Rename Portable' }));
        flushSync();
        await fireEvent.input(screen.getByRole('textbox', { name: 'New name for Portable' }), {
            target: { value: 'Field day' },
        });
        await rerender({ visible: false });
        await rerender({ visible: true });
        await flush();
        await flush();
        flushSync();
        expect(screen.getByLabelText('Name')).toHaveValue('Contest 2');
        expect(screen.getByLabelText('Callsign')).toHaveValue('7Q8AC');
        expect(screen.getByRole('textbox', { name: 'New name for Portable' })).toHaveValue(
            'Field day'
        );
    });

    it('writes nothing while an archive switch is unresolved', async () => {
        daemonHas();
        await renderLoaded();
        archivesState.switchUnresolved = true;
        flushSync();
        expect(screen.getByRole('button', { name: 'Delete Portable' })).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Rename Portable' })).toBeDisabled();
        await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'X' } });
        flushSync();
        expect(screen.getByRole('button', { name: 'Add logbook' })).toBeDisabled();
        expect(screen.getByTestId('logbooks-gate')).toBeInTheDocument();
    });
});
