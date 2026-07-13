// The right util rail shows live count pills: the pile-up depth on the Pile-up
// button (covered by the pile-up suites) and the session QSO count on the Session
// button. This guards the Session badge's reactive wiring to session.qsos.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import UtilRail from './UtilRail.svelte';
import { router } from '../router.svelte';
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
    router.mode = 'phone';
});

// The Worked panel is Phone/CW-only — FT8's Band Activity already carries
// worked-before context, so the rail button disables there instead of
// silently flipping the Phone/CW tile board's visibility state.
describe('UtilRail Worked button per mode', () => {
    it('is enabled in Phone/CW', () => {
        render(UtilRail);
        flushSync();
        expect(screen.getByRole('button', { name: /Worked/ })).not.toBeDisabled();
    });

    it('is disabled in FT8 with an explanatory title', () => {
        router.mode = 'ft8';
        render(UtilRail);
        flushSync();
        const btn = screen.getByRole('button', { name: /Worked/ });
        expect(btn).toBeDisabled();
        expect(btn).toHaveAttribute('title', 'Worked — not available in FT8');
        expect(btn).toHaveAttribute('data-active', 'false');
    });
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
