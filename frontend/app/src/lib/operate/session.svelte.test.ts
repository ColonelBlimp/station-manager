// The session list is the operator's "this sitting" glance + the export/email
// source. It's in-memory but MUST survive a page reload (a dogfood redeploy
// reloads /app/) — otherwise the whole session's contacts vanish. These pin the
// sessionStorage persistence.

import { describe, it, expect, beforeEach } from 'vitest';
import { session, addSessionQso, _resetSessionForTests, type SessionQso } from './session.svelte';

beforeEach(() => {
    _resetSessionForTests();
});

function qso(over: Partial<Omit<SessionQso, 'id'>> = {}): Omit<SessionQso, 'id'> {
    return {
        callsign: 'W1ABC',
        timeOn: '14:30:00',
        band: '20m',
        mode: 'FT8',
        rstSent: '',
        rstRcvd: '',
        name: '',
        country: 'United States',
        comment: '',
        ...over,
    };
}

describe('session log', () => {
    it('adds newest-first with unique ids', () => {
        addSessionQso(qso({ callsign: 'A' }));
        addSessionQso(qso({ callsign: 'B' }));
        expect(session.qsos.map((q) => q.callsign)).toEqual(['B', 'A']);
        expect(session.qsos[0].id).not.toBe(session.qsos[1].id);
    });

    it('persists to sessionStorage so a reload keeps the session', () => {
        addSessionQso(qso({ callsign: 'G3XYZ' }));
        const raw = sessionStorage.getItem('sm.session.qsos');
        expect(raw).not.toBeNull();
        const stored = JSON.parse(raw!) as { qsos: SessionQso[] };
        expect(stored.qsos[0].callsign).toBe('G3XYZ');
    });

    it('reset clears both the state and the persisted copy', () => {
        addSessionQso(qso());
        _resetSessionForTests();
        expect(session.qsos).toHaveLength(0);
        expect(sessionStorage.getItem('sm.session.qsos')).toBeNull();
    });
});
