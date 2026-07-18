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
});
