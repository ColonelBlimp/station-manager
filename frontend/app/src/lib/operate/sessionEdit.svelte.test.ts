// The Session panel's in-place edit controller: hydrate-by-uuid on open,
// PATCH + session-row write-back on save. The api layer is mocked — the
// controller's job is state choreography, not HTTP.

import { describe, it, expect, vi, beforeEach } from 'vitest';

const fetchQso = vi.fn();
const patchQso = vi.fn();
vi.mock('../api/qso-patch', () => ({
    fetchQso: (...a: unknown[]): unknown => fetchQso(...a),
    patchQso: (...a: unknown[]): unknown => patchQso(...a),
}));

import { sessionEdit } from './sessionEdit.svelte';
import { session, addSessionQso, _resetSessionForTests, type SessionQso } from './session.svelte';

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
        addSessionQso(sq());
        fetchQso.mockResolvedValue({ kind: 'ok', qso: { id: 9, uuid: 'u-1', call: 'G4ABC' } });
        await sessionEdit.open(session.qsos[0]);
        expect(fetchQso).toHaveBeenCalledWith('u-1');
        expect(sessionEdit.row?.call).toBe('G4ABC');
        expect(sessionEdit.openError).toBeNull();
    });

    it('reports a hydration failure in the panel and never opens', async () => {
        addSessionQso(sq());
        fetchQso.mockResolvedValue({ kind: 'error', message: 'Cannot reach the daemon.' });
        await sessionEdit.open(session.qsos[0]);
        expect(sessionEdit.row).toBeNull();
        expect(sessionEdit.openError).toContain('G4ABC');
        expect(sessionEdit.openError).toContain('Cannot reach the daemon.');
    });

    it('is a no-op for rows without a uuid', async () => {
        addSessionQso(sq({ uuid: undefined }));
        await sessionEdit.open(session.qsos[0]);
        expect(fetchQso).not.toHaveBeenCalled();
        expect(sessionEdit.row).toBeNull();
    });
});

describe('sessionEdit.save', () => {
    async function openOn(row: Omit<SessionQso, 'id'>): Promise<void> {
        addSessionQso(row);
        fetchQso.mockResolvedValue({
            kind: 'ok',
            qso: { id: 9, uuid: row.uuid, call: row.callsign },
        });
        await sessionEdit.open(session.qsos[0]);
    }

    it('PATCHes, overlays the canonical row onto the session list, and closes', async () => {
        await openOn(sq());
        patchQso.mockResolvedValue({
            kind: 'ok',
            qso: {
                id: 9,
                uuid: 'u-1',
                call: 'G4ABC',
                time_on: '143205',
                band: '40m', // daemon re-derived from an edited freq
                mode: 'SSB',
                submode: 'USB', // display literal = submode when present
                rst_sent: '59',
                rst_rcvd: '57',
                name: 'Fred',
                country: 'England',
                comment: 'fixed',
            },
        });
        await sessionEdit.save({ name: 'Fred' });
        expect(patchQso).toHaveBeenCalledWith('u-1', { name: 'Fred' });
        expect(sessionEdit.row).toBeNull(); // closed
        const row = session.qsos[0];
        expect(row.name).toBe('Fred');
        expect(row.band).toBe('40m');
        expect(row.mode).toBe('USB');
        expect(row.timeOn).toBe('14:32:05');
        expect(row.comment).toBe('fixed');
    });

    it('keeps the modal open with the daemon message on failure', async () => {
        await openOn(sq());
        patchQso.mockResolvedValue({ kind: 'error', message: 'freq out of band' });
        await sessionEdit.save({ freq: '99.9' });
        expect(sessionEdit.row).not.toBeNull(); // still open for correction
        expect(sessionEdit.error).toBe('freq out of band');
        expect(session.qsos[0].callsign).toBe('G4ABC'); // untouched
    });

    it('drops a save that completes after close + reopen (no write onto the new row)', async () => {
        // The race: save row A, close, open row B while A's PATCH is still in
        // flight. A's completion must be discarded — the QSO is saved
        // daemon-side either way, but its write-back must NOT land on B, and
        // B's freshly opened modal must not be force-closed.
        addSessionQso(sq()); // A: u-1 G4ABC
        addSessionQso(sq({ uuid: 'u-2', callsign: 'K1XYZ', country: 'USA' })); // B
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
