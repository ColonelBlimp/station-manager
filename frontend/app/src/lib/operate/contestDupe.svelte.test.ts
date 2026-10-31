// W-0023 weekend slice (dogfood 2026-10-31: 7Q7EB logged twice on 20m with no
// warning). In contest mode, committing the callsign asks whether the station
// was already worked on this band in this contest occurrence; a hit is a
// warning, never a refusal (ruling A: duplicates stay in the log).

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import { draft, clearDraft, startQso, setSerialAllocator, canLog } from './qso.svelte';
import { contest } from './contest.svelte';
import { contestDupeState, contestDupeShown, setContestDupeCheck } from './contestDupe.svelte';
import { rig, confirmRig } from './rig.svelte';

type Check = (
    call: string,
    band: string,
    contestId: string,
    start: string
) => Promise<boolean | null>;
let check: ReturnType<typeof vi.fn<Check>>;
const settle = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
    rig.mode = 'USB';
    rig.band = '20m';
    rig.cat = 'off';
    confirmRig();
    flushSync();
    clearDraft();
    contest.on = true;
    contest.id = 'UKEIDXSSB';
    contest.start = '2026-10-31';
    setSerialAllocator(() => Promise.resolve({ ok: true, serial: 5 }));
    check = vi.fn<Check>(() => Promise.resolve(true));
    setContestDupeCheck(check);
});
afterEach(() => {
    clearDraft();
    contest.on = false;
    setSerialAllocator(null);
    setContestDupeCheck(null);
});

describe('contest dupe warning', () => {
    it('asks about this call, band and occurrence when the callsign is committed', async () => {
        draft.callsign = 'g4abc';
        startQso();
        await settle();
        expect(check).toHaveBeenCalledWith('G4ABC', '20m', 'UKEIDXSSB', '20261031');
        expect(contestDupeShown()).toBe(true);
    });

    it('warns but never blocks logging', async () => {
        draft.callsign = 'G4ABC';
        draft.dateOn = '2026-10-31';
        draft.timeOn = '12:01:00';
        startQso();
        await settle();
        expect(contestDupeShown()).toBe(true);
        expect(canLog()).toBe(true);
    });

    it('no warning when not a dupe, when the call or band has changed, or after Clear', async () => {
        check.mockResolvedValueOnce(false);
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(contestDupeShown()).toBe(false);

        draft.callsign = 'G4XYZ';
        startQso(); // re-Tab after a correction asks again
        await settle();
        expect(check).toHaveBeenLastCalledWith('G4XYZ', '20m', 'UKEIDXSSB', '20261031');
        expect(contestDupeShown()).toBe(true);
        draft.callsign = 'G4XY'; // edited since the answer
        expect(contestDupeShown()).toBe(false);
        draft.callsign = 'G4XYZ';
        rig.band = '40m'; // band moved since the answer
        expect(contestDupeShown()).toBe(false);
        rig.band = '20m';
        clearDraft();
        expect(contestDupeState.call).toBe('');
        expect(contestDupeShown()).toBe(false);
    });

    it('an older answer arriving last never overwrites the newer one', async () => {
        let release: (v: boolean) => void = () => {};
        check.mockImplementationOnce(() => new Promise((r) => (release = r)));
        check.mockResolvedValueOnce(false);
        draft.callsign = 'G4ABC';
        startQso();
        clearDraft(); // e.g. the first attempt was abandoned
        draft.callsign = 'G4ABC'; // the same station, asked again
        startQso();
        await settle(); // the new answer: not a dupe
        release(true); // the OLD answer arrives last
        await settle();
        expect(contestDupeShown()).toBe(false);
    });

    it('a failed check shows nothing (never a false alarm)', async () => {
        check.mockResolvedValueOnce(null);
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(contestDupeShown()).toBe(false);
    });

    it('outside contest mode nothing is asked', async () => {
        contest.on = false;
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(check).not.toHaveBeenCalled();
    });
});
