// The Session panel's in-place edit controller: hydrate-by-uuid on open,
// PATCH + session-row write-back on save. The api layer is mocked — the
// controller's job is state choreography, not HTTP.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const fetchQso = vi.fn();
const patchQso = vi.fn();
vi.mock('../api/qso-patch', () => ({
    fetchQso: (...a: unknown[]): unknown => fetchQso(...a),
    patchQso: (...a: unknown[]): unknown => patchQso(...a),
}));

import { sessionEdit } from './sessionEdit.svelte';
import { setSessionActionGate } from './archiveStores.svelte';
import {
    session,
    setSessionPorts,
    sessionStateDrawn,
    _resetSessionForTests,
    type SessionQso,
    type SessionRowLike,
    type SessionRowsReadLike,
} from './session.svelte';
import { addSessionRow } from '../../test/sessionRows';
import { TEST_ARCHIVE } from '../../test/archive';

const sq = (over: Partial<SessionQso> = {}): Omit<SessionQso, 'id'> => ({
    uuid: 'u-1',
    callsign: 'G4ABC',
    timeOn: '14:30:00',
    band: '20m',
    mode: 'FT8',
    rstSent: '-10',
    rstRcvd: '-05',
    name: '',
    country: 'England',
    comment: '',
    ...over,
});

beforeEach(() => {
    _resetSessionForTests();
    sessionEdit.close();
    sessionEdit.openError = null;
    fetchQso.mockReset();
    patchQso.mockReset();
});

describe('sessionEdit.open', () => {
    it('hydrates the full row by uuid and opens the modal', async () => {
        addSessionRow(sq());
        fetchQso.mockResolvedValue({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC' } });
        await sessionEdit.open(session.qsos[0]);
        expect(fetchQso).toHaveBeenCalledWith('u-1');
        expect(sessionEdit.row?.call).toBe('G4ABC');
        expect(sessionEdit.openError).toBeNull();
    });

    it('reports a hydration failure in the panel and never opens', async () => {
        addSessionRow(sq());
        fetchQso.mockResolvedValue({ kind: 'error', message: 'Cannot reach the daemon.' });
        await sessionEdit.open(session.qsos[0]);
        expect(sessionEdit.row).toBeNull();
        expect(sessionEdit.openError).toContain('G4ABC');
        expect(sessionEdit.openError).toContain('Cannot reach the daemon.');
    });

    it('is a no-op for rows without a uuid', async () => {
        addSessionRow(sq({ uuid: undefined }));
        await sessionEdit.open(session.qsos[0]);
        expect(fetchQso).not.toHaveBeenCalled();
        expect(sessionEdit.row).toBeNull();
    });
});

describe('sessionEdit.save', () => {
    async function openOn(row: Omit<SessionQso, 'id'>): Promise<void> {
        addSessionRow(row);
        fetchQso.mockResolvedValue({
            kind: 'ok',
            qso: { id: 9, uuid: row.uuid, call: row.callsign },
        });
        await sessionEdit.open(session.qsos[0]);
    }

    // The daemon as the Session reads it: an open session whose rows are
    // whatever `held` is when a read is answered.
    const SESSION = '01a1f83c-da20-7000-8000-0000000000a1';
    function daemonRow(over: Partial<SessionRowLike> = {}): SessionRowLike {
        return {
            uuid: 'u-1',
            call: 'G4ABC',
            timeOn: '143000',
            band: '20m',
            mode: 'SSB',
            rstSent: '59',
            rstRcvd: '59',
            name: '',
            country: 'England',
            comment: '',
            emailed: false,
            ...over,
        };
    }
    async function sessionOf(held: { rows: SessionRowLike[] }): Promise<{ reads: () => number }> {
        let reads = 0;
        setSessionPorts({
            readRows: (): Promise<SessionRowsReadLike> => {
                reads++;
                return Promise.resolve({
                    kind: 'ok',
                    archiveId: TEST_ARCHIVE,
                    generation: 4,
                    sessionId: SESSION,
                    members: held.rows,
                });
            },
            readState: () => Promise.resolve(true),
            archive: () => TEST_ARCHIVE,
        });
        sessionStateDrawn({
            archiveId: TEST_ARCHIVE,
            generation: 4,
            state: 'open',
            session: { id: SESSION, openedAt: '2026-11-25T09:15:00Z', sittingStartedAt: null },
        });
        await Promise.resolve();
        await Promise.resolve();
        return { reads: () => reads };
    }
    const settle = async (): Promise<void> => {
        for (let i = 0; i < 5; i++) await Promise.resolve();
    };

    it('PATCHes, reads the rows again, and closes: the row shows what the DAEMON holds (A6)', async () => {
        const held = { rows: [daemonRow()] };
        const daemon = await sessionOf(held);
        fetchQso.mockResolvedValue({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC' } });
        await sessionEdit.open(session.qsos[0]);
        const before = daemon.reads();
        // The PATCH's own answer and the daemon's row differ on purpose: what
        // the panel shows must be the read, not the answer written in.
        held.rows = [
            daemonRow({ name: 'Fred', band: '40m', mode: 'SSB', submode: 'USB', timeOn: '143205' }),
        ];
        patchQso.mockResolvedValue({
            kind: 'ok',
            qso: { id: 9, uuid: 'u-1', call: 'G4ABC', name: 'NOT-WRITTEN-IN', band: '80m' },
        });

        await sessionEdit.save({ name: 'Fred' });
        await settle();

        expect(patchQso).toHaveBeenCalledWith('u-1', { name: 'Fred' });
        expect(sessionEdit.row).toBeNull(); // closed
        expect(daemon.reads()).toBe(before + 1);
        expect(session.qsos[0]).toMatchObject({
            name: 'Fred',
            band: '40m',
            mode: 'USB',
            timeOn: '14:32:05',
        });
    });

    it('writes nothing into the panel of a session that has changed meanwhile (B5)', async () => {
        const held = { rows: [daemonRow()] };
        await sessionOf(held);
        fetchQso.mockResolvedValue({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC' } });
        await sessionEdit.open(session.qsos[0]);
        let release: (v: unknown) => void = () => undefined;
        patchQso.mockImplementation(() => new Promise((res) => (release = res)));
        const inflight = sessionEdit.save({ name: 'Fred' });

        // New: another session is shown, empty, before the edit's answer returns.
        const NEXT = '01a1f83c-da20-7000-8000-0000000000a2';
        held.rows = [];
        setSessionPorts({
            readRows: () =>
                Promise.resolve({
                    kind: 'ok',
                    archiveId: TEST_ARCHIVE,
                    generation: 5,
                    sessionId: NEXT,
                    members: [],
                }),
            readState: () => Promise.resolve(true),
            archive: () => TEST_ARCHIVE,
        });
        sessionStateDrawn({
            archiveId: TEST_ARCHIVE,
            generation: 5,
            state: 'open',
            session: { id: NEXT, openedAt: '2026-11-25T10:00:00Z', sittingStartedAt: null },
        });
        await settle();
        expect(session.qsos).toEqual([]);

        release({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC', name: 'Fred' } });
        await inflight;
        await settle();

        expect(session.qsos).toEqual([]); // the edited contact is not this session's
        expect(session.sessionId).toBe(NEXT);
    });

    it('keeps the modal open with the daemon message on failure', async () => {
        await openOn(sq());
        patchQso.mockResolvedValue({ kind: 'error', message: 'freq out of band' });
        await sessionEdit.save({ freq: '99.9' });
        expect(sessionEdit.row).not.toBeNull(); // still open for correction
        expect(sessionEdit.error).toBe('freq out of band');
        expect(session.qsos[0].callsign).toBe('G4ABC'); // untouched
    });

    it('a save that completes after close + reopen leaves the new row and its modal alone', async () => {
        // The race: save row A, close, open row B while A's PATCH is still in
        // flight. The QSO is saved daemon-side either way; A's answer must not
        // change B's row, and B's freshly opened modal must not be force-closed.
        addSessionRow(sq()); // A: u-1 G4ABC
        addSessionRow(sq({ uuid: 'u-2', callsign: 'K1XYZ', country: 'USA' })); // B
        const rowA = session.qsos.find((q) => q.uuid === 'u-1');
        const rowB = session.qsos.find((q) => q.uuid === 'u-2');
        if (!rowA || !rowB) throw new Error('setup: session rows missing');
        fetchQso.mockImplementation((uuid: unknown) =>
            Promise.resolve({
                kind: 'ok',
                qso: { id: 1, uuid, call: uuid === 'u-1' ? 'G4ABC' : 'K1XYZ' },
            })
        );
        await sessionEdit.open(rowA);
        let release: (v: unknown) => void = () => undefined;
        patchQso.mockImplementation(() => new Promise((res) => (release = res)));
        const inflight = sessionEdit.save({ name: 'Fred' });
        sessionEdit.close();
        await sessionEdit.open(rowB);
        release({
            kind: 'ok',
            qso: { id: 1, uuid: 'u-1', call: 'G4ABC', name: 'Fred', country: 'England' },
        });
        await inflight;
        expect(sessionEdit.row?.uuid).toBe('u-2'); // B's modal survives
        const b = session.qsos.find((q) => q.uuid === 'u-2');
        expect(b?.callsign).toBe('K1XYZ'); // A's data never landed on B
        expect(b?.name).toBe('');
        expect(b?.country).toBe('USA');
    });

    it('ignores a repeated save while one is in flight', async () => {
        await openOn(sq());
        let release: (v: unknown) => void = () => undefined;
        patchQso.mockImplementation(() => new Promise((res) => (release = res)));
        const first = sessionEdit.save({ name: 'Fred' });
        void sessionEdit.save({ name: 'Fred' }); // Ctrl+Enter repeat — must not double-PATCH
        release({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC', name: 'Fred' } });
        await first;
        expect(patchQso).toHaveBeenCalledTimes(1);
    });
});

// While the tab's archive is unproven (before binding, while a check is pending,
// or a switch is unresolved) Session edit sends nothing (ADR 0094 update): the row
// may belong to an archive the daemon no longer serves.
describe('sessionEdit while the archive is unproven', () => {
    const WAIT = 'Waiting until this tab has confirmed its archive.';
    afterEach(() => setSessionActionGate(() => null));

    it('open is refused with the reason and fetches nothing', async () => {
        addSessionRow(sq());
        setSessionActionGate(() => WAIT);
        await sessionEdit.open(session.qsos[0]);
        expect(fetchQso).not.toHaveBeenCalled();
        expect(sessionEdit.row).toBeNull();
        expect(sessionEdit.openError).toBe(WAIT);
    });

    it('save is refused with the reason and sends no PATCH', async () => {
        addSessionRow(sq());
        fetchQso.mockResolvedValue({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC' } });
        await sessionEdit.open(session.qsos[0]);
        setSessionActionGate(() => WAIT); // a reconnect check began with the modal open
        await sessionEdit.save({ comment: 'x' });
        expect(patchQso).not.toHaveBeenCalled();
        expect(sessionEdit.error).toBe(WAIT);
        expect(sessionEdit.row).not.toBeNull(); // the edit is kept for when it clears
    });
});
