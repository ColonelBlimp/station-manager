import { beforeEach, describe, expect, it, vi } from 'vitest';

/*
    SETTINGS → LOGBOOKS, FIRST SLICE — THE STORE (W-0021 dossier, operator ruling
    2026-09-29). It manages the ACTIVE archive's logbooks over the daemon's
    existing endpoints: creating one never activates an archive, makes it the
    default, or enables an upload the operator did not tick. Each write reports
    its own outcome — a refusal by name, a timeout as unknown — and re-reads.
*/

vi.mock('../api/logbooks', () => ({
    fetchLogbooks: vi.fn(),
    fetchLogbookCount: vi.fn(),
    createLogbook: vi.fn(),
    renameLogbook: vi.fn(),
    deleteLogbook: vi.fn(),
}));
vi.mock('../api/seams', () => ({ fetchStationContext: vi.fn() }));
vi.mock('../api/qso-archives', () => ({ fetchDaemonIdentity: vi.fn() }));
vi.mock('../api/archive-bindings', () => ({
    fetchArchiveBindings: vi.fn(),
    saveArchiveBindings: vi.fn(),
}));
vi.mock('./archives.svelte', () => ({
    archiveSwitchGate: vi.fn(() => null),
    archivesState: { list: [] },
}));
const bindingsReload = vi.hoisted(() => vi.fn());
vi.mock('./bindings.svelte', () => ({
    bindingsState: { requestReload: bindingsReload },
}));
vi.mock('../operate/station.svelte', () => ({ setStationInfo: vi.fn() }));

import {
    createLogbook,
    deleteLogbook,
    fetchLogbookCount,
    fetchLogbooks,
    renameLogbook,
} from '../api/logbooks';
import { fetchStationContext } from '../api/seams';
import { fetchDaemonIdentity } from '../api/qso-archives';
import { fetchArchiveBindings, saveArchiveBindings } from '../api/archive-bindings';
import { archiveSwitchGate } from './archives.svelte';
import { setStationInfo } from '../operate/station.svelte';
import { logbooksState, _resetLogbooksForTests } from './logbooks.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

const ARCHIVE = '01a0e79a-7b32-7cf5-a72c-57cf437a362d';

function smcloud(reason = '') {
    return {
        type: 'smcloud',
        display_name: 'SM Cloud',
        account: { configured: reason === '', label: '', fields_set: [], build_key: '' },
        state: 'off',
        reason,
        logbooks: [],
    };
}

function daemonHas(opts: {
    logbooks?: { id: number; name: string; callsign: string }[];
    counts?: Record<number, number | null>;
    destinations?: unknown[];
}) {
    const logbooks = opts.logbooks ?? [
        { id: 1, name: 'Default', callsign: '7Q5MLV' },
        { id: 2, name: 'Portable', callsign: '7Q5MLV/P' },
    ];
    const counts = opts.counts ?? { 1: 12, 2: 0 };
    vi.mocked(fetchLogbooks).mockResolvedValue({ kind: 'ok', logbooks });
    vi.mocked(fetchLogbookCount).mockImplementation((id: number) => {
        const n = counts[id];
        return Promise.resolve(
            n === null || n === undefined
                ? { kind: 'error', message: 'count failed' }
                : { kind: 'ok', count: n }
        );
    });
    vi.mocked(fetchStationContext).mockResolvedValue({
        configOk: true,
        stationCallsign: '7Q5MLV',
        logbookId: 1,
    } as never);
    vi.mocked(fetchDaemonIdentity).mockResolvedValue({ instance: 'i', archiveId: ARCHIVE });
    vi.mocked(fetchArchiveBindings).mockResolvedValue({
        kind: 'ok',
        bindings: {
            archive_id: ARCHIVE,
            archive_label: 'Home',
            restart_required: false,
            destinations: (opts.destinations ?? [smcloud()]) as never,
        },
    });
}

const toast = (level: string, re: RegExp): boolean =>
    toastsState.items.some((t) => t.level === level && re.test(t.message));

beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(archiveSwitchGate).mockReturnValue(null);
    _resetLogbooksForTests();
    resetToasts();
});

describe('logbooksState.load', () => {
    it('lists the active archive’s logbooks with counts, the default, and the archive label', async () => {
        daemonHas({});
        await logbooksState.load();
        expect(logbooksState.loaded).toBe(true);
        expect(logbooksState.archiveLabel).toBe('Home');
        expect(logbooksState.defaultId).toBe(1);
        expect(logbooksState.stationCallsign).toBe('7Q5MLV');
        expect(logbooksState.rows).toEqual([
            { id: 1, name: 'Default', callsign: '7Q5MLV', count: 12 },
            { id: 2, name: 'Portable', callsign: '7Q5MLV/P', count: 0 },
        ]);
    });

    it('offers SM Cloud only when this archive can turn it on', async () => {
        daemonHas({});
        await logbooksState.load();
        expect(logbooksState.smcloudAvailable).toBe(true);

        daemonHas({ destinations: [smcloud('no station account')] });
        await logbooksState.load();
        expect(logbooksState.smcloudAvailable).toBe(false);

        daemonHas({ destinations: [] });
        await logbooksState.load();
        expect(logbooksState.smcloudAvailable).toBe(false);
    });

    it('a failed logbook read is an error, not an empty list', async () => {
        daemonHas({});
        vi.mocked(fetchLogbooks).mockResolvedValue({ kind: 'error', message: 'no daemon' });
        await logbooksState.load();
        expect(logbooksState.loaded).toBe(false);
        expect(logbooksState.error).toContain('no daemon');
    });
});

describe('logbooksState.load — overlapping reads', () => {
    it('an older read that answers last never replaces a newer one', async () => {
        daemonHas({});
        let releaseOld: (v: unknown) => void = () => {};
        vi.mocked(fetchLogbooks).mockImplementationOnce(
            () => new Promise((r) => (releaseOld = r)) as never
        );
        const older = logbooksState.load(); // e.g. the tab opening
        const newer = logbooksState.load(); // e.g. the re-read after a delete
        await newer;
        expect(logbooksState.rows.map((r) => r.name)).toEqual(['Default', 'Portable']);
        releaseOld({ kind: 'ok', logbooks: [{ id: 9, name: 'Stale', callsign: '7Q5MLV' }] });
        await older;
        expect(logbooksState.rows.map((r) => r.name)).toEqual(['Default', 'Portable']);
    });
});

describe('logbooksState.deleteBlock', () => {
    it('names why Delete is unavailable: the default, QSOs held, or an unknown count', async () => {
        daemonHas({
            logbooks: [
                { id: 1, name: 'Default', callsign: '7Q5MLV' },
                { id: 2, name: 'Portable', callsign: '7Q5MLV' },
                { id: 3, name: 'Busy', callsign: '7Q5MLV' },
                { id: 4, name: 'Unknown', callsign: '7Q5MLV' },
            ],
            counts: { 1: 0, 2: 0, 3: 1, 4: null },
        });
        await logbooksState.load();
        const block = (id: number) =>
            logbooksState.deleteBlock(logbooksState.rows.find((r) => r.id === id)!);
        expect(block(1)).toBe('The Default logbook can’t be deleted');
        expect(block(2)).toBeNull();
        expect(block(3)).toBe('Holds 1 QSO — only an empty logbook can be deleted');
        expect(block(4)).toBe('Its QSO count could not be read');
    });
});

describe('logbooksState.create', () => {
    it('creates the logbook and enables nothing it was not asked to', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 3 });
        const ok = await logbooksState.create({
            name: 'Contest',
            callsign: '7Q5MLV',
            smcloud: false,
        });
        expect(ok).toBe(true);
        expect(createLogbook).toHaveBeenCalledWith({ name: 'Contest', callsign: '7Q5MLV' });
        expect(saveArchiveBindings).not.toHaveBeenCalled();
        expect(fetchLogbooks).toHaveBeenCalledTimes(2); // re-read
        expect(toast('info', /Logbook “Contest” added/)).toBe(true);
    });

    it('ticked, enables only the new logbook’s SM Cloud binding, and says it applies after a restart', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 3 });
        vi.mocked(saveArchiveBindings).mockResolvedValue({ kind: 'ok', bindings: {} as never });
        expect(
            await logbooksState.create({ name: 'Contest', callsign: '7Q5MLV', smcloud: true })
        ).toBe(true);
        expect(saveArchiveBindings).toHaveBeenCalledWith(ARCHIVE, {
            destinations: [{ type: 'smcloud', logbooks: [{ logbook_id: 3, enabled: true }] }],
        });
        expect(toast('info', /SM Cloud uploads start after a restart/)).toBe(true);
    });

    it('if SM Cloud cannot be turned on, the logbook stays and the message says so', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 3 });
        vi.mocked(saveArchiveBindings).mockResolvedValue({ kind: 'error', message: 'refused' });
        expect(
            await logbooksState.create({ name: 'Contest', callsign: '7Q5MLV', smcloud: true })
        ).toBe(true);
        expect(toast('warn', /“Contest” was added, but SM Cloud was not turned on: refused/)).toBe(
            true
        );
    });

    it('a refusal keeps the form and names the reason', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({
            kind: 'error',
            code: 'duplicate_name',
            message: 'a logbook with that name already exists',
        });
        expect(
            await logbooksState.create({ name: 'Default', callsign: '7Q5MLV', smcloud: true })
        ).toBe(false);
        expect(saveArchiveBindings).not.toHaveBeenCalled();
        expect(toast('error', /already exists/)).toBe(true);
    });

    it('a timed-out create is an unknown outcome, re-read, never a failure', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({
            kind: 'error',
            message: 'Cannot reach the daemon.',
            timedOut: true,
        });
        expect(
            await logbooksState.create({ name: 'Contest', callsign: '7Q5MLV', smcloud: true })
        ).toBe(false);
        expect(saveArchiveBindings).not.toHaveBeenCalled();
        expect(toast('warn', /outcome is unknown/)).toBe(true);
        expect(toastsState.items.some((t) => t.level === 'error')).toBe(false);
        expect(fetchLogbooks).toHaveBeenCalledTimes(2);
    });

    it('is refused while an archive switch is in flight or unresolved', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(archiveSwitchGate).mockReturnValue('An archive switch is in progress.');
        expect(
            await logbooksState.create({ name: 'Contest', callsign: '7Q5MLV', smcloud: false })
        ).toBe(false);
        expect(createLogbook).not.toHaveBeenCalled();
        expect(toast('error', /archive switch is in progress/)).toBe(true);
    });
});

describe('logbooksState.rename', () => {
    it('renaming the default logbook also renames it in the header', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(renameLogbook).mockResolvedValue({ kind: 'ok' });
        expect(await logbooksState.rename(1, 'Home station')).toBe(true);
        expect(renameLogbook).toHaveBeenCalledWith(1, 'Home station');
        expect(setStationInfo).toHaveBeenCalledWith({ logbookName: 'Home station' });

        vi.mocked(setStationInfo).mockClear();
        expect(await logbooksState.rename(2, 'Field')).toBe(true);
        expect(setStationInfo).not.toHaveBeenCalled();
    });

    it('a refusal is named and the list re-read; a timeout is unknown', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(renameLogbook).mockResolvedValue({
            kind: 'error',
            code: 'duplicate_name',
            message: 'a logbook with that name already exists',
        });
        expect(await logbooksState.rename(2, 'Default')).toBe(false);
        expect(toast('error', /already exists/)).toBe(true);
        expect(fetchLogbooks).toHaveBeenCalledTimes(2);

        vi.mocked(renameLogbook).mockResolvedValue({ kind: 'error', message: 'x', timedOut: true });
        expect(await logbooksState.rename(2, 'Field')).toBe(false);
        expect(toast('warn', /outcome is unknown/)).toBe(true);
        expect(setStationInfo).not.toHaveBeenCalled();
    });
});

describe('logbooksState.remove', () => {
    it('deletes and re-reads; a refusal (the state changed since render) is shown and re-read', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(deleteLogbook).mockResolvedValue({ kind: 'ok' });
        await logbooksState.remove(2);
        expect(deleteLogbook).toHaveBeenCalledWith(2);
        expect(toast('info', /“Portable” deleted/)).toBe(true);

        vi.mocked(deleteLogbook).mockResolvedValue({
            kind: 'error',
            code: 'has_qsos',
            message: 'logbook holds QSOs',
        });
        await logbooksState.remove(2);
        expect(toast('error', /holds QSOs/)).toBe(true);
        expect(fetchLogbooks).toHaveBeenCalledTimes(3);
    });

    it('is refused while an archive switch is unresolved', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(archiveSwitchGate).mockReturnValue('An archive switch is unresolved.');
        await logbooksState.remove(2);
        expect(deleteLogbook).not.toHaveBeenCalled();
    });
});

describe('Settings → Forwarding stays in step', () => {
    // Review 412cca37 P2: whether Forwarding can re-read now (no unsaved edits)
    // or must owe it until they are discarded is the bindings store's call
    // (bindings.svelte.test.ts B17–B20); every logbook write asks.
    it('asks Forwarding to re-read its rows after every write', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 3 });
        await logbooksState.create({ name: 'Contest', callsign: '7Q5MLV', smcloud: false });
        expect(bindingsReload).toHaveBeenCalledTimes(1);
        vi.mocked(renameLogbook).mockResolvedValue({ kind: 'ok' });
        await logbooksState.rename(2, 'Field');
        expect(bindingsReload).toHaveBeenCalledTimes(2);
        vi.mocked(deleteLogbook).mockResolvedValue({ kind: 'ok' });
        await logbooksState.remove(2);
        expect(bindingsReload).toHaveBeenCalledTimes(3);
    });
});

// Review 412cca37 P2: the editors stay usable while a write is on the wire, so
// a success clears only the draft it submitted — a newer one typed meanwhile is
// kept, and stays guarded as unsaved.
describe('a draft typed while a write is pending', () => {
    it('create keeps a newer Add draft, and clears the one it submitted', async () => {
        daemonHas({});
        await logbooksState.load();
        let answer: (v: unknown) => void = () => {};
        vi.mocked(createLogbook).mockImplementation(
            () => new Promise((r) => (answer = r)) as never
        );
        logbooksState.addName = 'First';
        const pending = logbooksState.create({ name: 'First', callsign: '7Q5MLV', smcloud: false });
        logbooksState.addName = 'Second';
        answer({ kind: 'ok', id: 3 });
        await pending;
        expect(logbooksState.addName).toBe('Second');
        expect(logbooksState.dirty).toBe(true);

        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 4 });
        await logbooksState.create({ name: 'Second', callsign: '7Q5MLV', smcloud: false });
        expect(logbooksState.addName).toBe('');
        expect(logbooksState.dirty).toBe(false);
    });

    it('rename keeps a newer rename draft, and closes the one it submitted', async () => {
        daemonHas({});
        await logbooksState.load();
        let answer: (v: unknown) => void = () => {};
        vi.mocked(renameLogbook).mockImplementation(
            () => new Promise((r) => (answer = r)) as never
        );
        logbooksState.startRename({ id: 2, name: 'Portable', callsign: '7Q5MLV/P', count: 0 });
        logbooksState.renameName = 'Field';
        const pending = logbooksState.rename(2, 'Field');
        logbooksState.renameName = 'Field day';
        answer({ kind: 'ok' });
        await pending;
        expect(logbooksState.renameId).toBe(2);
        expect(logbooksState.renameName).toBe('Field day');

        vi.mocked(renameLogbook).mockResolvedValue({ kind: 'ok' });
        await logbooksState.rename(2, 'Field day');
        expect(logbooksState.renameId).toBe(0);
    });

    it('a kept rename draft measures against the name just saved', async () => {
        daemonHas({});
        await logbooksState.load();
        let answer: (v: unknown) => void = () => {};
        vi.mocked(renameLogbook).mockImplementation(
            () => new Promise((r) => (answer = r)) as never
        );
        logbooksState.startRename({ id: 2, name: 'Portable', callsign: '7Q5MLV/P', count: 0 });
        logbooksState.renameName = 'Field';
        const pending = logbooksState.rename(2, 'Field');
        logbooksState.renameName = 'Portable'; // typed back while saving
        answer({ kind: 'ok' });
        await pending;
        expect(logbooksState.renameId).toBe(2);
        expect(logbooksState.renameFrom).toBe('Field');
        expect(logbooksState.dirty).toBe(true); // "Portable" is now a change
    });
});
