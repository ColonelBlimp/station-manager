import { describe, it, expect } from 'vitest';
import { decodeQsoEvent } from './log-events';

// AW-1 alpha.2: the qso.* event decode is additive — it prefers the canonical qso_uuid but
// still accepts a legacy qso_id-only event, and rejects one carrying neither identifier.
// logbook_id must stay a number (the contacts map keys on it). alpha.3 removes the
// qso_id-only fallback.
describe('decodeQsoEvent (AW-1 alpha.2 additive decode)', () => {
    it('accepts a uuid-only payload (qso_uuid preferred, no qso_id)', () => {
        const p = decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1', logbook_id: 3 }));
        expect(p).not.toBeNull();
        expect(p?.qso_uuid).toBe('u-1');
    });

    it('tolerates a legacy qso_id-only payload during alpha.2', () => {
        const p = decodeQsoEvent(JSON.stringify({ qso_id: 7, logbook_id: 3 }));
        expect(p).not.toBeNull();
        expect(p?.qso_id).toBe(7);
    });

    it('accepts a payload carrying both ids', () => {
        const p = decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1', qso_id: 7, logbook_id: 3 }));
        expect(p).not.toBeNull();
    });

    it('rejects a payload with neither identifier', () => {
        expect(decodeQsoEvent(JSON.stringify({ logbook_id: 3 }))).toBeNull();
    });

    it('rejects an empty qso_uuid as an identifier', () => {
        expect(decodeQsoEvent(JSON.stringify({ qso_uuid: '', logbook_id: 3 }))).toBeNull();
    });

    it('rejects a non-numeric or absent logbook_id (the map keys on it)', () => {
        expect(decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1', logbook_id: '3' }))).toBeNull();
        expect(decodeQsoEvent(JSON.stringify({ qso_uuid: 'u-1' }))).toBeNull();
    });

    it('returns null on invalid JSON', () => {
        expect(decodeQsoEvent('{not json')).toBeNull();
    });

    it('returns null for valid JSON that is not a payload object', () => {
        expect(decodeQsoEvent('null')).toBeNull();
    });
});
