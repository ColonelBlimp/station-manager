import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
    bindArchiveStores,
    fileUnderArchive,
    sessionActionBlock,
    setSessionActionGate,
    setStoresMismatchSink,
    _reloadArchiveStoresForTests,
} from './archiveStores.svelte';
import { fileSessionQso, session, type SessionQso } from './session.svelte';
import { ft8EngagedThisSession, ft8Link } from './ft8.svelte';
import { fileFt8Logged } from './ft8Logged';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import { TEST_ARCHIVE } from '../../test/archive';

/*
    Archive-scoped browser stores (ADR 0094 and its 2026-10-23 update; W-0021
    "alpha.4 hold: fix design", revision 3, slice 4). The Session list and the FT8
    engagement set are kept per archive within the tab, selected only by the archive
    the boot bracket PROVED, and every write carries the archive of the daemon that
    produced it (the FT8 connection's label, the submit response's archive_id):
    applied on a match, buffered before binding, refused with an immediate
    verification on a mismatch, dropped as a protocol fault when unlabelled.
    "Reload" below is _reloadArchiveStoresForTests + bindArchiveStores: a fresh page
    on the same tab (memory gone, sessionStorage kept), as an archive switch is.
*/

const HOME = '01a163a7-0f04-7f69-85e9-255664b58d11';
const CONTEST = '01a1640f-815a-75e4-bae2-b9ef8d86f8c3';

function row(uuid: string, callsign: string): Omit<SessionQso, 'id'> {
    return {
        uuid,
        callsign,
        timeOn: '12:00:00',
        band: '20m',
        mode: 'USB',
        rstSent: '59',
        rstRcvd: '59',
        name: '',
        country: '',
        comment: '',
    };
}
const engage = (call: string, archiveId: string | null): void =>
    ft8Link.onQso(
        { active: true, role: 'answerer', their_call: call, dial_freq_mhz: 14.074 },
        archiveId
    );
const calls = (): string[] => session.qsos.map((q) => q.callsign);
const reloadOn = (archiveId: string): void => {
    _reloadArchiveStoresForTests();
    bindArchiveStores(archiveId);
};
const RESET_NOTICE = /Session history was reset by this update/;
const notices = (): number => toastsState.items.filter((t) => RESET_NOTICE.test(t.message)).length;

let mismatches = 0;

beforeEach(() => {
    sessionStorage.clear();
    _reloadArchiveStoresForTests(); // a fresh page: nothing proven yet
    mismatches = 0;
    setStoresMismatchSink(() => mismatches++);
    resetToasts();
});

afterEach(() => {
    vi.restoreAllMocks();
    setStoresMismatchSink(() => {});
    setSessionActionGate(() => null);
    sessionStorage.clear();
    reloadOn(TEST_ARCHIVE); // leave the module as the test setup found it
});

describe('isolation: each archive keeps its own list and engagement within the tab', () => {
    it('Home → Contest → Home restores each, and a same-archive reload preserves both', () => {
        reloadOn(HOME);
        fileSessionQso(HOME, row('h-1', 'ZS4JAN'));
        engage('W1ABC', HOME);

        reloadOn(HOME); // same-archive reload
        expect(calls()).toEqual(['ZS4JAN']);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);

        reloadOn(CONTEST); // switch: Contest starts empty
        expect(calls()).toEqual([]);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);
        fileSessionQso(CONTEST, row('c-1', 'VK0YY'));

        reloadOn(HOME); // return: Home's list and engagement come back
        expect(calls()).toEqual(['ZS4JAN']);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);

        reloadOn(CONTEST);
        expect(calls()).toEqual(['VK0YY']);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);
    });
});

describe('provenance after binding', () => {
    // The nearest confusable failure: the FT8 stream reconnects to a daemon now
    // serving Contest BEFORE the always-on stream's verification has reloaded this
    // Home tab. Its frames must file NOTHING — not under Home, not under Contest —
    // and must start verification at once, synchronously.
    it('a reconnect from another archive files nothing anywhere and verifies at once', () => {
        reloadOn(HOME);
        fileSessionQso(HOME, row('h-1', 'ZS4JAN'));
        const homeBefore = sessionStorage.getItem(`sm.session.qsos.${HOME}`);
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');

        engage('K1XYZ', CONTEST);
        fileFt8Logged({ uuid: 'c-9', callsign: 'VK0YY', band: '20m' }, CONTEST);
        fileSessionQso(CONTEST, row('c-10', 'VK9XX'));

        expect(mismatches).toBeGreaterThanOrEqual(1);
        expect(calls()).toEqual(['ZS4JAN']);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);
        expect(sessionStorage.getItem(`sm.session.qsos.${HOME}`)).toBe(homeBefore);
        expect(sessionStorage.getItem(`sm.session.qsos.${CONTEST}`)).toBeNull();
        expect(sessionStorage.getItem(`sm.ft8.engaged.${CONTEST}`)).toBeNull();
        expect(marked).not.toHaveBeenCalled(); // not "worked this session" here either

        reloadOn(HOME);
        expect(calls()).toEqual(['ZS4JAN']);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);
    });

    // UUIDs can exist in both archives (`smd import` preserves them), so a frame
    // from Contest naming a UUID Home's list already shows is NOT a repeat delivery:
    // provenance is decided first — verification starts, nothing is written or
    // marked, and the "it's logged" toast still shows.
    it('a foreign frame reusing a UUID this list shows still verifies, writes nothing, toasts', () => {
        reloadOn(HOME);
        fileSessionQso(HOME, row('u-shared', 'ZS4JAN'));
        const homeBefore = sessionStorage.getItem(`sm.session.qsos.${HOME}`);
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');

        fileFt8Logged({ uuid: 'u-shared', callsign: 'VK0YY', band: '20m' }, CONTEST);

        expect(mismatches).toBe(1);
        expect(calls()).toEqual(['ZS4JAN']);
        expect(sessionStorage.getItem(`sm.session.qsos.${HOME}`)).toBe(homeBefore);
        expect(sessionStorage.getItem(`sm.session.qsos.${CONTEST}`)).toBeNull();
        expect(marked).not.toHaveBeenCalled();
        expect(toastsState.items.some((t) => /QSO logged — VK0YY/.test(t.message))).toBe(true);
    });

    // The genuine repeat: the same archive delivers a row it already filed — no
    // second row, mark or toast, and nothing to verify.
    it('a repeat delivery from the same archive adds no row, mark or toast', () => {
        reloadOn(HOME);
        fileFt8Logged({ uuid: 'h-7', callsign: 'JA8EPO', band: '20m' }, HOME);
        resetToasts();
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        fileFt8Logged({ uuid: 'h-7', callsign: 'JA8EPO', band: '20m' }, HOME);
        expect(calls()).toEqual(['JA8EPO']);
        expect(marked).not.toHaveBeenCalled();
        expect(toastsState.items).toHaveLength(0);
        expect(mismatches).toBe(0);
    });

    it('a matching frame is applied without verification', () => {
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        reloadOn(HOME);
        fileFt8Logged({ uuid: 'h-2', callsign: 'JA8EPO', band: '20m' }, HOME);
        engage('W1ABC', HOME);
        expect(calls()).toEqual(['JA8EPO']);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);
        expect(marked).toHaveBeenCalledWith('JA8EPO', '20m', 'FT8');
        expect(mismatches).toBe(0);
    });

    it('an unlabelled frame is a protocol fault: dropped, no verification', () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        reloadOn(HOME);
        fileSessionQso(null, row('x-1', 'NOBODY'));
        fileFt8Logged({ uuid: 'x-2', callsign: 'NOBODY2', band: '20m' }, null);
        engage('W1ABC', null);
        expect(calls()).toEqual([]);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);
        expect(mismatches).toBe(0);
    });
});

describe('the boot buffer', () => {
    it('releases only records tagged with the proven archive, merging hydrated UUIDs', () => {
        // Persisted earlier this tab: Home already holds h-1.
        reloadOn(HOME);
        fileSessionQso(HOME, row('h-1', 'ZS4JAN'));
        _reloadArchiveStoresForTests(); // the next page load, still unproven

        fileSessionQso(HOME, row('h-1', 'ZS4JAN')); // the same row again (a repeat)
        fileSessionQso(HOME, row('h-2', 'JA8EPO'));
        fileSessionQso(CONTEST, row('c-1', 'VK0YY'));
        engage('W1ABC', HOME);
        engage('K1XYZ', CONTEST);
        expect(calls()).toEqual([]); // nothing applied before proof

        bindArchiveStores(HOME);
        expect(calls()).toEqual(['JA8EPO', 'ZS4JAN']);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);

        reloadOn(CONTEST); // the discarded Contest records were never filed
        expect(calls()).toEqual([]);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);
    });

    it('an unlabelled record is never buffered', () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        fileSessionQso(null, row('x-1', 'NOBODY'));
        engage('W1ABC', null);
        bindArchiveStores(HOME);
        expect(calls()).toEqual([]);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);
    });

    it('binding is set once: a second proof cannot rebind the page', () => {
        bindArchiveStores(HOME);
        bindArchiveStores(CONTEST);
        fileSessionQso(CONTEST, row('c-1', 'VK0YY'));
        expect(calls()).toEqual([]);
        expect(mismatches).toBe(1);
    });

    it('a buffered record cannot be released by a later proof of its archive on another page', () => {
        fileSessionQso(CONTEST, row('c-1', 'VK0YY'));
        bindArchiveStores(HOME); // this page proved Home: the Contest record is discarded
        reloadOn(CONTEST);
        expect(calls()).toEqual([]);
    });

    it('fileUnderArchive applies nothing before proof, then exactly once', () => {
        let applied = 0;
        fileUnderArchive(HOME, 'probe', () => applied++);
        expect(applied).toBe(0);
        bindArchiveStores(HOME);
        expect(applied).toBe(1);
        bindArchiveStores(HOME);
        expect(applied).toBe(1);
    });
});

describe('legacy unscoped stores (R2, R4)', () => {
    const legacyRow = JSON.stringify({ qsos: [{ ...row('l-1', 'OLDCALL'), id: 1 }] });
    function seedLegacy(): void {
        sessionStorage.setItem('sm.session.qsos', legacyRow);
        sessionStorage.setItem('sm.ft8.engaged', JSON.stringify(['W1ABC|20M']));
    }

    it('after proof: removes both unscoped keys, keeps scoped stores, adopts nothing, notices once', () => {
        reloadOn(HOME);
        fileSessionQso(HOME, row('h-1', 'ZS4JAN'));
        _reloadArchiveStoresForTests();
        seedLegacy();

        bindArchiveStores(HOME);
        expect(sessionStorage.getItem('sm.session.qsos')).toBeNull();
        expect(sessionStorage.getItem('sm.ft8.engaged')).toBeNull();
        expect(calls()).toEqual(['ZS4JAN']); // the scoped store survives
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false); // legacy not adopted
        expect(notices()).toBe(1);

        reloadOn(HOME);
        expect(notices()).toBe(1); // nothing left to reset: no repeat
    });

    it('before proof (a failed boot) nothing is removed and no notice is shown', () => {
        seedLegacy();
        // The boot bracket never proves an archive: bindArchiveStores is not called.
        expect(sessionStorage.getItem('sm.session.qsos')).toBe(legacyRow);
        expect(sessionStorage.getItem('sm.ft8.engaged')).not.toBeNull();
        expect(calls()).toEqual([]);
        expect(notices()).toBe(0);
    });

    it('a remove that fails leaves the key, so the next proven boot resets and notices again', () => {
        seedLegacy();
        vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
            throw new Error('storage unavailable');
        });
        bindArchiveStores(HOME);
        expect(notices()).toBe(1);
        expect(sessionStorage.getItem('sm.session.qsos')).toBe(legacyRow);
        expect(calls()).toEqual([]); // never hydrated from the legacy key

        reloadOn(HOME);
        expect(notices()).toBe(2); // the accepted limitation: it repeats
    });
});

describe('Session actions while the archive is unproven', () => {
    it('blocked before binding, while the gate reports a check, and free otherwise', () => {
        expect(sessionActionBlock()).toMatch(/confirmed its archive/);
        bindArchiveStores(HOME);
        expect(sessionActionBlock()).toBeNull();
        setSessionActionGate(() => 'Waiting until this tab has confirmed its archive.');
        expect(sessionActionBlock()).toMatch(/confirmed its archive/);
    });
});
