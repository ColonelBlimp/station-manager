import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// Counts the times a caller asks for the Session's rows to be read again. The
// Session's rows are the daemon's (W-0029 part 6): a frame makes no row, it asks.
const asks = vi.hoisted(() => ({ n: 0 }));
vi.mock('./session.svelte', async (importOriginal) => {
    const real = await importOriginal<typeof import('./session.svelte')>();
    return {
        ...real,
        refreshSessionRows: () => {
            asks.n++;
            real.refreshSessionRows();
        },
    };
});

import {
    bindArchiveStores,
    fileUnderArchive,
    sessionActionBlock,
    setSessionActionGate,
    setStoresMismatchSink,
    _reloadArchiveStoresForTests,
} from './archiveStores.svelte';
import { session } from './session.svelte';
import { ft8EngagedThisSession, ft8Link } from './ft8.svelte';
import { fileFt8Logged, _resetFt8LoggedForTests } from './ft8Logged';
import { ft8EnrichState } from './ft8Enrich.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';
import { TEST_ARCHIVE } from '../../test/archive';

/*
    Archive-scoped browser stores (ADR 0094 and its 2026-10-23 update; W-0021
    "alpha.4 hold: fix design", revision 3, slice 4). The FT8 engagement set is
    kept per archive within the tab, selected only by the archive the boot bracket
    PROVED, and every write carries the archive of the daemon that produced it
    (the FT8 connection's label): applied on a match, buffered before binding,
    refused with an immediate verification on a mismatch, dropped as a protocol
    fault when unlabelled. "Reload" below is _reloadArchiveStoresForTests +
    bindArchiveStores: a fresh page on the same tab (memory gone, sessionStorage
    kept), as an archive switch is.

    The Session's rows are no longer one of these stores (W-0029 part 6): they
    are read from the daemon and kept nowhere in the browser. What still goes
    through the scheme for a logged contact is the worked mark and the request
    to read the rows, and those are what the cases below watch. `filed` stands
    for any write a caller files under an archive.
*/

const HOME = '01a163a7-0f04-7f69-85e9-255664b58d11';
const CONTEST = '01a1640f-815a-75e4-bae2-b9ef8d86f8c3';

const engage = (call: string, archiveId: string | null): void =>
    ft8Link.onQso(
        { active: true, role: 'answerer', their_call: call, dial_freq_mhz: 14.074 },
        archiveId
    );
let filed: string[] = [];
const file = (archiveId: string | null, what: string): void =>
    fileUnderArchive(archiveId, what, () => filed.push(what));
const reloadOn = (archiveId: string): void => {
    _reloadArchiveStoresForTests();
    bindArchiveStores(archiveId);
};
const RESET_NOTICE = /Session history was reset by this update/;
const notices = (): number => toastsState.items.filter((t) => RESET_NOTICE.test(t.message)).length;
const toasted = (re: RegExp): boolean => toastsState.items.some((t) => re.test(t.message));

let mismatches = 0;

beforeEach(() => {
    sessionStorage.clear();
    _reloadArchiveStoresForTests(); // a fresh page: nothing proven yet
    _resetFt8LoggedForTests();
    mismatches = 0;
    filed = [];
    asks.n = 0;
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

describe('isolation: each archive keeps its own engagement within the tab', () => {
    it('Home → Contest → Home restores it, and a same-archive reload preserves it', () => {
        reloadOn(HOME);
        engage('W1ABC', HOME);

        reloadOn(HOME); // same-archive reload
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);

        reloadOn(CONTEST); // switch: Contest starts empty
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);

        reloadOn(HOME); // return: Home's engagement comes back
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);

        reloadOn(CONTEST);
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
        file(HOME, 'home write');
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');

        engage('K1XYZ', CONTEST);
        fileFt8Logged({ uuid: 'c-9', callsign: 'VK0YY', band: '20m' }, CONTEST);
        file(CONTEST, 'contest write');

        expect(mismatches).toBeGreaterThanOrEqual(1);
        expect(filed).toEqual(['home write']);
        expect(asks.n).toBe(0); // another archive's contact asks nothing of this Session
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);
        expect(sessionStorage.getItem(`sm.ft8.engaged.${CONTEST}`)).toBeNull();
        expect(marked).not.toHaveBeenCalled(); // not "worked this session" here either

        reloadOn(HOME);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);
    });

    // UUIDs can exist in both archives (`smd import` preserves them), so a frame
    // from Contest naming a UUID this page has already confirmed for Home is NOT a
    // repeat delivery: provenance is decided first — verification starts, nothing
    // is asked or marked, and the "it's logged" toast still shows.
    it('a foreign frame reusing a UUID this page confirmed still verifies, marks nothing, toasts', () => {
        reloadOn(HOME);
        fileFt8Logged({ uuid: 'u-shared', callsign: 'ZS4JAN', band: '20m' }, HOME);
        resetToasts();
        asks.n = 0;
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');

        fileFt8Logged({ uuid: 'u-shared', callsign: 'VK0YY', band: '20m' }, CONTEST);

        expect(mismatches).toBe(1);
        expect(asks.n).toBe(0);
        expect(marked).not.toHaveBeenCalled();
        expect(toasted(/QSO logged — VK0YY/)).toBe(true);
    });

    // The genuine repeat: the same archive delivers a frame it already confirmed —
    // no second request, mark or toast, and nothing to verify.
    it('a repeat delivery from the same archive asks, marks and toasts once', () => {
        reloadOn(HOME);
        fileFt8Logged({ uuid: 'h-7', callsign: 'JA8EPO', band: '20m' }, HOME);
        expect(asks.n).toBe(1);
        resetToasts();
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        fileFt8Logged({ uuid: 'h-7', callsign: 'JA8EPO', band: '20m' }, HOME);
        expect(asks.n).toBe(1);
        expect(marked).not.toHaveBeenCalled();
        expect(toastsState.items).toHaveLength(0);
        expect(mismatches).toBe(0);
    });

    it('a matching frame is applied without verification, and makes no row itself', () => {
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        reloadOn(HOME);
        fileFt8Logged({ uuid: 'h-2', callsign: 'JA8EPO', band: '20m' }, HOME);
        engage('W1ABC', HOME);
        expect(asks.n).toBe(1);
        expect(session.qsos).toEqual([]); // the row is the daemon's to give
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);
        expect(marked).toHaveBeenCalledWith('JA8EPO', '20m', 'FT8');
        expect(toasted(/QSO logged — JA8EPO \(20m\)/)).toBe(true);
        expect(mismatches).toBe(0);
    });

    it('an unlabelled frame is a protocol fault: dropped, no verification', () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        reloadOn(HOME);
        file(null, 'unlabelled write');
        fileFt8Logged({ uuid: 'x-2', callsign: 'NOBODY2', band: '20m' }, null);
        engage('W1ABC', null);
        expect(filed).toEqual([]);
        expect(asks.n).toBe(0);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);
        expect(mismatches).toBe(0);
    });
});

describe('the boot buffer', () => {
    it('releases only records tagged with the proven archive', () => {
        file(HOME, 'home 1');
        file(HOME, 'home 2');
        file(CONTEST, 'contest 1');
        fileFt8Logged({ uuid: 'h-1', callsign: 'ZS4JAN', band: '20m' }, HOME);
        fileFt8Logged({ uuid: 'c-1', callsign: 'VK0YY', band: '20m' }, CONTEST);
        engage('W1ABC', HOME);
        engage('K1XYZ', CONTEST);
        const marked = vi.spyOn(ft8EnrichState, 'markWorked');
        expect(filed).toEqual([]); // nothing applied before proof
        expect(asks.n).toBe(0);

        bindArchiveStores(HOME);
        expect(filed).toEqual(['home 1', 'home 2']);
        expect(asks.n).toBe(1); // Home's contact, and not Contest's
        expect(marked).toHaveBeenCalledTimes(1);
        expect(marked).toHaveBeenCalledWith('ZS4JAN', '20m', 'FT8');
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(true);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);

        reloadOn(CONTEST); // the discarded Contest records were never filed
        expect(filed).toEqual(['home 1', 'home 2']);
        expect(ft8EngagedThisSession('K1XYZ', '20m')).toBe(false);
    });

    it('an unlabelled record is never buffered', () => {
        vi.spyOn(console, 'warn').mockImplementation(() => {});
        file(null, 'unlabelled write');
        engage('W1ABC', null);
        bindArchiveStores(HOME);
        expect(filed).toEqual([]);
        expect(ft8EngagedThisSession('W1ABC', '20m')).toBe(false);
    });

    it('binding is set once: a second proof cannot rebind the page', () => {
        bindArchiveStores(HOME);
        bindArchiveStores(CONTEST);
        file(CONTEST, 'contest write');
        expect(filed).toEqual([]);
        expect(mismatches).toBe(1);
    });

    it('a buffered record cannot be released by a later proof of its archive on another page', () => {
        file(CONTEST, 'contest write');
        bindArchiveStores(HOME); // this page proved Home: the Contest record is discarded
        reloadOn(CONTEST);
        expect(filed).toEqual([]);
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
    const legacyRow = JSON.stringify({ qsos: [{ id: 1, uuid: 'l-1', callsign: 'OLDCALL' }] });
    function seedLegacy(): void {
        sessionStorage.setItem('sm.session.qsos', legacyRow);
        sessionStorage.setItem('sm.ft8.engaged', JSON.stringify(['W1ABC|20M']));
    }

    it('after proof: removes both unscoped keys, keeps scoped stores, adopts nothing, notices once', () => {
        reloadOn(HOME);
        engage('K9KEPT', HOME);
        _reloadArchiveStoresForTests();
        seedLegacy();

        bindArchiveStores(HOME);
        expect(sessionStorage.getItem('sm.session.qsos')).toBeNull();
        expect(sessionStorage.getItem('sm.ft8.engaged')).toBeNull();
        expect(ft8EngagedThisSession('K9KEPT', '20m')).toBe(true); // the scoped store survives
        expect(session.qsos).toEqual([]); // no row is ever taken from storage
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
        expect(session.qsos).toEqual([]);
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
        expect(session.qsos).toEqual([]); // never hydrated from the legacy key

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
