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
    updateLogbook: vi.fn(),
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
    updateLogbook,
} from '../api/logbooks';
import { fetchStationContext } from '../api/seams';
import { fetchDaemonIdentity } from '../api/qso-archives';
import { fetchArchiveBindings, saveArchiveBindings } from '../api/archive-bindings';
import { archiveSwitchGate } from './archives.svelte';
import { setStationInfo } from '../operate/station.svelte';
import { logbooksState, _resetLogbooksForTests } from './logbooks.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

const ARCHIVE = '01a0e79a-7b32-7cf5-a72c-57cf437a362d';

function smcloud(reason = '', newLogbookReason = '', support: unknown = null) {
    return {
        type: 'smcloud',
        display_name: 'SM Cloud',
        account: { configured: reason === '', label: '', fields_set: [], build_key: '' },
        state: 'off',
        reason,
        new_logbook_reason: newLogbookReason || reason,
        support,
        logbooks: [],
    };
}

const CHECKING = 'SM Cloud server support is still being checked; try again shortly';
const smcloudChecking = () => smcloud('', CHECKING, { state: 'checking', message: '' });

function bindingsAnswer(destinations: unknown[]) {
    return {
        kind: 'ok' as const,
        bindings: {
            archive_id: ARCHIVE,
            archive_label: 'Home',
            restart_required: false,
            destinations: destinations as never,
        },
    };
}

function daemonHas(opts: {
    logbooks?: { id: number; name: string; callsign: string; description?: string }[];
    counts?: Record<number, number | null>;
    destinations?: unknown[];
}) {
    const logbooks = opts.logbooks ?? [
        { id: 1, name: 'Default', callsign: '7Q5MLV' },
        { id: 2, name: 'Portable', callsign: '7Q5MLV/P' },
    ];
    const counts = opts.counts ?? { 1: 12, 2: 0 };
    // fetchLogbooks returns the decoded shape: an omitted description is ''
    // (the decoder's rule, logbooks.test.ts).
    vi.mocked(fetchLogbooks).mockResolvedValue({
        kind: 'ok',
        logbooks: logbooks.map((l) => ({ ...l, description: l.description ?? '' })),
    });
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
    vi.mocked(fetchDaemonIdentity).mockResolvedValue({
        instance: 'i',
        archiveId: ARCHIVE,
        defaultLogbookUuid: '01a187bc-0173-7649-8ff3-71db8170c309',
    });
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
            { id: 1, name: 'Default', callsign: '7Q5MLV', description: '', count: 12 },
            { id: 2, name: 'Portable', callsign: '7Q5MLV/P', description: '', count: 0 },
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

    it('5F.0: SM Cloud is not offered when a NEW logbook could not have it turned on', async () => {
        daemonHas({ destinations: [smcloud('', 'only for the default logbook')] });
        await logbooksState.load();
        expect(logbooksState.smcloudAvailable).toBe(false);
    });

    it('5F.4 UI4: a new logbook follows the daemon’s answer, in a managed archive too', async () => {
        daemonHas({ destinations: [smcloud()] });
        vi.mocked(fetchArchiveBindings).mockResolvedValue({
            kind: 'ok',
            bindings: {
                archive_id: ARCHIVE,
                archive_label: 'Drill',
                restart_required: false,
                destinations: [smcloud()] as never,
            },
        });
        await logbooksState.load();
        expect(logbooksState.smcloudAvailable).toBe(true);
        for (const why of [
            'SM Cloud server support is still being checked; try again shortly',
            'the SM Cloud server does not support per-logbook backups; upgrade it',
        ]) {
            daemonHas({ destinations: [smcloud('', why)] });
            await logbooksState.load();
            expect(logbooksState.smcloudAvailable).toBe(false);
            expect(logbooksState.smcloudRefusal).toBe(why);
        }
    });

    it('W-0024 AC1: each row carries its stored description; none is empty', async () => {
        daemonHas({
            logbooks: [
                { id: 1, name: 'Default', callsign: '7Q5MLV', description: 'Home station log' },
                { id: 2, name: 'Portable', callsign: '7Q5MLV/P' },
            ],
        });
        await logbooksState.load();
        expect(logbooksState.rows.map((r) => r.description)).toEqual(['Home station log', '']);
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

    it('W-0024 AC1: sends a typed description, trimmed, and none when it is blank', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(createLogbook).mockResolvedValue({ kind: 'ok', id: 3 });
        await logbooksState.create({
            name: 'Contest',
            callsign: '7Q5MLV',
            smcloud: false,
            description: '  UK/EI DX 2026 ',
        });
        expect(createLogbook).toHaveBeenLastCalledWith({
            name: 'Contest',
            callsign: '7Q5MLV',
            description: 'UK/EI DX 2026',
        });

        await logbooksState.create({
            name: 'Field',
            callsign: '7Q5MLV',
            smcloud: false,
            description: '   ',
        });
        expect(createLogbook).toHaveBeenLastCalledWith({ name: 'Field', callsign: '7Q5MLV' });
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

    it('5F.0: an add with SM Cloud that a new logbook cannot have is refused whole — no logbook, no binding', async () => {
        daemonHas({ destinations: [smcloud('', 'only for the default logbook')] });
        await logbooksState.load();
        expect(
            await logbooksState.create({ name: 'Contest', callsign: '7Q5MLV', smcloud: true })
        ).toBe(false);
        expect(createLogbook).not.toHaveBeenCalled();
        expect(saveArchiveBindings).not.toHaveBeenCalled();
        expect(
            toast(
                'error',
                /SM Cloud can’t be turned on for a new logbook: only for the default logbook/
            )
        ).toBe(true);
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

describe('logbooksState.saveEdit', () => {
    const edit = (row: { id: number; name: string; description?: string }) =>
        logbooksState.startEdit({
            id: row.id,
            name: row.name,
            callsign: '7Q5MLV',
            description: row.description ?? '',
            count: 0,
        });

    it('renaming the default logbook also renames it in the header', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 1, name: 'Default' });
        logbooksState.editName = 'Home station';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(updateLogbook).toHaveBeenCalledWith(1, { name: 'Home station' });
        expect(setStationInfo).toHaveBeenCalledWith({ logbookName: 'Home station' });

        vi.mocked(setStationInfo).mockClear();
        edit({ id: 2, name: 'Portable' });
        logbooksState.editName = 'Field';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(setStationInfo).not.toHaveBeenCalled();
    });

    it('a refusal is named and the list re-read; a timeout is unknown; the draft stays', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({
            kind: 'error',
            code: 'duplicate_name',
            message: 'a logbook with that name already exists',
        });
        edit({ id: 2, name: 'Portable' });
        logbooksState.editName = 'Default';
        expect(await logbooksState.saveEdit()).toBe(false);
        expect(toast('error', /already exists/)).toBe(true);
        expect(fetchLogbooks).toHaveBeenCalledTimes(2);
        expect(logbooksState.editId).toBe(2);
        expect(logbooksState.editName).toBe('Default');

        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'error', message: 'x', timedOut: true });
        logbooksState.editName = 'Field';
        expect(await logbooksState.saveEdit()).toBe(false);
        expect(toast('warn', /outcome is unknown/)).toBe(true);
        expect(setStationInfo).not.toHaveBeenCalled();
        expect(logbooksState.editId).toBe(2);
        expect(logbooksState.editName).toBe('Field');
    });

    // ---- W-0024 AC1: descriptions ----

    it('a description-only change sends only the description, never the name', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 2, name: 'Portable' });
        logbooksState.editDescription = '  Holiday ops  ';
        expect(await logbooksState.saveEdit()).toBe(true);
        // Trimmed as the daemon trims; the name is absent, so a concurrent
        // rename elsewhere is not overwritten by this snapshot of it.
        expect(updateLogbook).toHaveBeenCalledWith(2, { description: 'Holiday ops' });
        expect(logbooksState.editId).toBe(0);
    });

    // The header names the Default logbook. A description-only save sent no
    // name, so the draft's name — possibly stale — must not reach the header.
    it('a description-only save on the Default logbook leaves the header’s name alone', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 1, name: 'Default' });
        logbooksState.editDescription = 'Home station log';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(updateLogbook).toHaveBeenCalledWith(1, { description: 'Home station log' });
        expect(setStationInfo).not.toHaveBeenCalled();
    });

    it('a name and description save on the Default logbook names the saved name in the header', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 1, name: 'Default' });
        logbooksState.editName = ' Home station ';
        logbooksState.editDescription = 'Home station log';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(setStationInfo).toHaveBeenCalledWith({ logbookName: 'Home station' });
    });

    it('a name-only change sends only the name, never the description', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 2, name: 'Portable', description: 'Holiday ops' });
        logbooksState.editName = 'Field';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(updateLogbook).toHaveBeenCalledWith(2, { name: 'Field' });
    });

    it('clearing a description sends an explicit empty description', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 2, name: 'Portable', description: 'Holiday ops' });
        logbooksState.editDescription = '   ';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(updateLogbook).toHaveBeenCalledWith(2, { description: '' });
    });

    it('both changed sends both', async () => {
        daemonHas({});
        await logbooksState.load();
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        edit({ id: 2, name: 'Portable' });
        logbooksState.editName = 'Field';
        logbooksState.editDescription = 'Portable ops';
        expect(await logbooksState.saveEdit()).toBe(true);
        expect(updateLogbook).toHaveBeenCalledWith(2, {
            name: 'Field',
            description: 'Portable ops',
        });
    });

    it('a refused or timed-out description save keeps the draft and the existing outcome handling', async () => {
        daemonHas({});
        await logbooksState.load();
        edit({ id: 2, name: 'Portable' });
        logbooksState.editDescription = 'Holiday ops';
        vi.mocked(updateLogbook).mockResolvedValue({
            kind: 'error',
            code: 'not_found',
            message: 'logbook not found',
        });
        expect(await logbooksState.saveEdit()).toBe(false);
        expect(updateLogbook).toHaveBeenCalledWith(2, { description: 'Holiday ops' });
        expect(toast('error', /logbook not found/)).toBe(true);
        expect(logbooksState.editDescription).toBe('Holiday ops');
        expect(logbooksState.dirty).toBe(true);

        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'error', message: 'x', timedOut: true });
        expect(await logbooksState.saveEdit()).toBe(false);
        expect(toast('warn', /outcome is unknown/)).toBe(true);
        expect(logbooksState.editDescription).toBe('Holiday ops');
        expect(logbooksState.dirty).toBe(true);
    });

    it('an unchanged edit, or one changed only by surrounding spaces, sends nothing', async () => {
        daemonHas({});
        await logbooksState.load();
        edit({ id: 2, name: 'Portable', description: 'Holiday ops' });
        logbooksState.editName = ' Portable ';
        logbooksState.editDescription = 'Holiday ops ';
        expect(await logbooksState.saveEdit()).toBe(false);
        expect(updateLogbook).not.toHaveBeenCalled();
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
        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        logbooksState.startEdit({
            id: 2,
            name: 'Portable',
            callsign: '7Q5MLV/P',
            description: '',
            count: 0,
        });
        logbooksState.editName = 'Field';
        await logbooksState.saveEdit();
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

    const portable = { id: 2, name: 'Portable', callsign: '7Q5MLV/P', description: '', count: 0 };
    const holdUpdate = (): ((v: unknown) => void) => {
        let answer: (v: unknown) => void = () => {};
        vi.mocked(updateLogbook).mockImplementation(
            () => new Promise((r) => (answer = r)) as never
        );
        return (v) => answer(v);
    };

    it('an edit keeps a newer draft, and closes the one it submitted', async () => {
        daemonHas({});
        await logbooksState.load();
        const answer = holdUpdate();
        logbooksState.startEdit(portable);
        logbooksState.editName = 'Field';
        const pending = logbooksState.saveEdit();
        logbooksState.editName = 'Field day';
        answer({ kind: 'ok' });
        await pending;
        expect(logbooksState.editId).toBe(2);
        expect(logbooksState.editName).toBe('Field day');

        vi.mocked(updateLogbook).mockResolvedValue({ kind: 'ok' });
        await logbooksState.saveEdit();
        expect(logbooksState.editId).toBe(0);
    });

    it('a kept draft measures against the name just saved', async () => {
        daemonHas({});
        await logbooksState.load();
        const answer = holdUpdate();
        logbooksState.startEdit(portable);
        logbooksState.editName = 'Field';
        const pending = logbooksState.saveEdit();
        logbooksState.editName = 'Portable'; // typed back while saving
        answer({ kind: 'ok' });
        await pending;
        expect(logbooksState.editId).toBe(2);
        expect(logbooksState.editFromName).toBe('Field');
        expect(logbooksState.dirty).toBe(true); // "Portable" is now a change
    });

    // W-0024 AC1: a successful save does not erase description edits made
    // while it was pending.
    it('a description typed while a name-only save is pending stays, and stays guarded', async () => {
        daemonHas({});
        await logbooksState.load();
        const answer = holdUpdate();
        logbooksState.startEdit(portable);
        logbooksState.editName = 'Field';
        const pending = logbooksState.saveEdit();
        logbooksState.editDescription = 'Portable ops';
        answer({ kind: 'ok' });
        await pending;
        expect(updateLogbook).toHaveBeenCalledWith(2, { name: 'Field' });
        expect(logbooksState.editId).toBe(2);
        expect(logbooksState.editDescription).toBe('Portable ops');
        expect(logbooksState.editFromName).toBe('Field');
        expect(logbooksState.editFromDescription).toBe('');
        expect(logbooksState.dirty).toBe(true);
    });

    it('a description changed again while its save is pending stays, measured against the saved one', async () => {
        daemonHas({});
        await logbooksState.load();
        const answer = holdUpdate();
        logbooksState.startEdit(portable);
        logbooksState.editDescription = 'Holiday';
        const pending = logbooksState.saveEdit();
        logbooksState.editDescription = 'Holiday ops';
        answer({ kind: 'ok' });
        await pending;
        expect(updateLogbook).toHaveBeenCalledWith(2, { description: 'Holiday' });
        expect(logbooksState.editId).toBe(2);
        expect(logbooksState.editDescription).toBe('Holiday ops');
        expect(logbooksState.editFromDescription).toBe('Holiday');
        expect(logbooksState.dirty).toBe(true);
    });

    it('the Add form’s description typed while a create is pending stays', async () => {
        daemonHas({});
        await logbooksState.load();
        let answer: (v: unknown) => void = () => {};
        vi.mocked(createLogbook).mockImplementation(
            () => new Promise((r) => (answer = r)) as never
        );
        logbooksState.addName = 'Contest';
        const pending = logbooksState.create({
            name: 'Contest',
            callsign: '7Q5MLV',
            smcloud: false,
            description: '',
        });
        logbooksState.addDescription = 'UK/EI DX';
        answer({ kind: 'ok', id: 3 });
        await pending;
        expect(logbooksState.addDescription).toBe('UK/EI DX');
        expect(logbooksState.dirty).toBe(true);
    });
});

/*
    W-0021 5F.4 commit 4, operator review: a new logbook's SM Cloud answer
    while the server's support is still being checked.

      LB1 checking, then a re-read finds it supported: SM Cloud is offered, and
          the add form's drafts stay.
      LB2 checking, then error: the refusal names the error; no longer checking.
      LB3 a re-read answering after a newer load cannot roll it back; only one
          re-read is on the wire at a time.
      LB4 a re-read cannot start while a load is in flight: otherwise it shares
          the load's generation, and its late "checking" would overwrite the
          load's newer answer (operator review).
*/
describe('logbooksState.refreshSmcloud (5F.4 review)', () => {
    it('LB1: checking → supported offers SM Cloud and keeps the drafts', async () => {
        daemonHas({ destinations: [smcloudChecking()] });
        await logbooksState.load();
        expect(logbooksState.smcloudChecking).toBe(true);
        expect(logbooksState.smcloudAvailable).toBe(false);
        expect(logbooksState.smcloudRefusal).toBe(CHECKING);
        logbooksState.addName = 'Portable 2026';
        vi.mocked(fetchArchiveBindings).mockResolvedValue(
            bindingsAnswer([smcloud('', '', { state: 'supported', message: '' })])
        );
        expect(await logbooksState.refreshSmcloud()).toBe(true);
        expect(logbooksState.smcloudChecking).toBe(false);
        expect(logbooksState.smcloudAvailable).toBe(true);
        expect(logbooksState.addName).toBe('Portable 2026');
    });

    it('LB2: checking → error names the error', async () => {
        daemonHas({ destinations: [smcloudChecking()] });
        await logbooksState.load();
        const why = 'SM Cloud server support could not be checked; the daemon log has the cause';
        vi.mocked(fetchArchiveBindings).mockResolvedValue(
            bindingsAnswer([smcloud('', why, { state: 'error', message: '' })])
        );
        await logbooksState.refreshSmcloud();
        expect(logbooksState.smcloudChecking).toBe(false);
        expect(logbooksState.smcloudAvailable).toBe(false);
        expect(logbooksState.smcloudRefusal).toBe(why);
    });

    it('LB3: a late re-read cannot roll back a newer load; one on the wire', async () => {
        daemonHas({ destinations: [smcloudChecking()] });
        await logbooksState.load();
        let release: (v: unknown) => void = () => {};
        vi.mocked(fetchArchiveBindings).mockReturnValueOnce(
            new Promise((resolve) => (release = resolve)) as never
        );
        const late = logbooksState.refreshSmcloud();
        expect(await logbooksState.refreshSmcloud()).toBe(false);
        daemonHas({ destinations: [smcloud('', '', { state: 'supported', message: '' })] });
        await logbooksState.load();
        expect(logbooksState.smcloudAvailable).toBe(true);
        release(bindingsAnswer([smcloudChecking()]));
        expect(await late).toBe(false);
        expect(logbooksState.smcloudAvailable).toBe(true);
        expect(logbooksState.smcloudChecking).toBe(false);
    });

    it('LB4: a re-read cannot start during a load and overwrite its answer', async () => {
        daemonHas({ destinations: [smcloudChecking()] });
        await logbooksState.load();
        expect(logbooksState.smcloudChecking).toBe(true);
        let releaseList: (v: unknown) => void = () => {};
        vi.mocked(fetchLogbooks).mockReturnValueOnce(
            new Promise((resolve) => (releaseList = resolve)) as never
        );
        let releasePoll: (v: unknown) => void = () => {};
        let phase: 'poll' | 'load' = 'poll';
        vi.mocked(fetchArchiveBindings).mockImplementation(() =>
            phase === 'poll'
                ? (new Promise((resolve) => (releasePoll = resolve)) as never)
                : Promise.resolve(
                      bindingsAnswer([smcloud('', '', { state: 'supported', message: '' })])
                  )
        );
        const loading = logbooksState.load();
        const poll = logbooksState.refreshSmcloud();
        phase = 'load';
        releaseList({
            kind: 'ok',
            logbooks: [
                { id: 1, name: 'Default', callsign: '7Q5MLV' },
                { id: 2, name: 'Portable', callsign: '7Q5MLV/P' },
            ],
        });
        await loading;
        expect(logbooksState.smcloudAvailable).toBe(true);
        releasePoll(bindingsAnswer([smcloudChecking()]));
        await poll;
        expect(logbooksState.smcloudAvailable).toBe(true);
        expect(logbooksState.smcloudChecking).toBe(false);
    });
});
