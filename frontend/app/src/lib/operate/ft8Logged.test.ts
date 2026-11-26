// An FT8 contact is confirmed once, whichever word arrives first (W-0029 part 6,
// A8). Two words reach the page by two routes: the daemon's rows, which are the
// Session, and the FT8 stream's ft8-logged frame, which is the toast and the
// worked mark. Before part 6 a frame was judged a repeat by finding its UUID in
// the window's list; with the daemon's rows there first, that would lose the
// toast and the mark. So the cases here put the row on screen BEFORE the frame.

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fileFt8Logged, _resetFt8LoggedForTests } from './ft8Logged';
import { session, _resetSessionForTests } from './session.svelte';
import { addSessionRow } from '../../test/sessionRows';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import { TEST_ARCHIVE } from '../../test/archive';

const UUID = '01a21282-f185-7377-a3eb-cf669a088916';
const frame = { uuid: UUID, callsign: 'JA8EPO', band: '20m', mode: 'MFSK', submode: 'FT4' };
const logged = (): number =>
    toastsState.items.filter((t) => /QSO logged — JA8EPO \(20m\)/.test(t.message)).length;

beforeEach(() => {
    _resetSessionForTests();
    _resetFt8LoggedForTests();
    resetToasts();
    vi.restoreAllMocks();
});

describe('ft8-logged', () => {
    it('toasts and marks when the daemon’s row is already on screen', () => {
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        addSessionRow({
            uuid: UUID,
            callsign: 'JA8EPO',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT4',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });

        fileFt8Logged(frame, TEST_ARCHIVE);

        expect(logged()).toBe(1);
        expect(marked).toHaveBeenCalledTimes(1);
        expect(marked).toHaveBeenCalledWith('JA8EPO', '20m', 'FT4');
        expect(session.qsos).toHaveLength(1); // the frame made no second row
    });

    it('makes no row when the frame is first: the row is the daemon’s to give', () => {
        fileFt8Logged(frame, TEST_ARCHIVE);
        expect(logged()).toBe(1);
        expect(session.qsos).toEqual([]);
    });

    it('delivered twice, in either order with the row: one toast and one mark', () => {
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        fileFt8Logged(frame, TEST_ARCHIVE);
        addSessionRow({
            uuid: UUID,
            callsign: 'JA8EPO',
            timeOn: '14:30:00',
            band: '20m',
            mode: 'FT4',
            rstSent: '',
            rstRcvd: '',
            name: '',
            country: '',
            comment: '',
        });
        fileFt8Logged(frame, TEST_ARCHIVE);

        expect(logged()).toBe(1);
        expect(marked).toHaveBeenCalledTimes(1);
        expect(session.qsos).toHaveLength(1);
    });
});
