// The right util rail shows live count pills: the pile-up depth on the Pile-up
// button (covered by the pile-up suites) and the session QSO count on the Session
// button. This guards the Session badge's reactive wiring to session.qsos.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import UtilRail from './UtilRail.svelte';
import { session, type SessionQso } from './session.svelte';

function sessionQso(call: string): SessionQso {
    return {
        id: 1,
        callsign: call,
        timeOn: '14:30:00',
        band: '20m',
        mode: 'FT8',
        rstSent: '-12',
        rstRcvd: '-08',
        name: '',
        country: '',
        comment: '',
    };
}

beforeEach(() => {
    session.qsos.length = 0;
});

describe('UtilRail session count badge', () => {
    it('shows no badge when the session is empty', () => {
        render(UtilRail);
        flushSync();
        expect(screen.queryByTitle(/QSOs? this session/)).toBeNull();
    });

    it('shows the live QSO count, singular/plural aware', () => {
        render(UtilRail);
        session.qsos.push(sessionQso('K1ABC'));
        flushSync();
        const badge = screen.getByTitle('1 QSO this session');
        expect(badge).toHaveTextContent('1');

        session.qsos.push(sessionQso('9A4ZM'));
        flushSync();
        expect(screen.getByTitle('2 QSOs this session')).toHaveTextContent('2');
    });
});
