/*
    The rig reading a saved draft carries (ADR 0085, operator ruling 2026-09-30, S2).

      R1  With no connection loss seen, the reading is the rig's state at save
          time, labelled "when saved".
      R2  At the first loss of the rig connection the last reading is kept, with
          its capture time; later reports (the replacement daemon's) and repeated
          loss callbacks do not overwrite it. Apart from saving a reading taken
          after the reconnect as if it were the contact's.
      R3  A verified same-archive recovery retires it, so an unrelated later
          switch cannot reuse it.
      R5  Retirement is tied to the loss it verified: a check that began
          before a later loss cannot retire the reading held for that loss
          (review 2026-09-30: a late identity answer replaced 14.255 MHz with
          7.074 MHz).
      R4  Missing values stay unknown: an unparseable frequency is null and an
          empty mode stays empty, never a default.
*/
import { beforeEach, describe, expect, it } from 'vitest';
import { rig } from './rig.svelte';
import {
    noteRigDrop,
    retireRigSnapshot,
    rigDropEpoch,
    rigReadingForSave,
    rigSnapshotHeld,
    _resetRigSnapshotForTests,
} from './rigSnapshot.svelte';

const T0 = Date.parse('2026-09-30T12:00:00Z');
const T1 = Date.parse('2026-09-30T12:05:00Z');
const T2 = Date.parse('2026-09-30T12:06:00Z');

beforeEach(() => {
    _resetRigSnapshotForTests();
    rig.freq = '14.255.000';
    rig.band = '20m';
    rig.mode = 'USB';
});

describe('rig reading for a saved draft', () => {
    it('R1 no loss seen: the reading when saved', () => {
        expect(rigReadingForSave(T1)).toEqual({
            freqHz: 14_255_000,
            band: '20m',
            mode: 'USB',
            adifMode: 'SSB',
            subMode: 'USB',
            basis: 'when-saved',
            capturedAt: new Date(T1).toISOString(),
        });
    });

    it('R2 the reading before the first loss is kept through reconnect reports and repeated losses', () => {
        noteRigDrop(T0);
        rig.freq = '7.074.000';
        rig.band = '40m';
        rig.mode = 'CW';
        noteRigDrop(T1);
        const r = rigReadingForSave(T2);
        expect(r).toMatchObject({ freqHz: 14_255_000, band: '20m', mode: 'USB' });
        expect(r.basis).toBe('before-drop');
        expect(r.capturedAt).toBe(new Date(T0).toISOString());
    });

    it('R3 a retired snapshot is not reused', () => {
        noteRigDrop(T0);
        retireRigSnapshot(rigDropEpoch());
        rig.freq = '7.074.000';
        rig.band = '40m';
        rig.mode = 'CW';
        expect(rigReadingForSave(T2)).toMatchObject({
            freqHz: 7_074_000,
            band: '40m',
            basis: 'when-saved',
        });
    });

    it('R4 missing values stay unknown', () => {
        rig.freq = '';
        rig.mode = '';
        rig.band = '';
        expect(rigReadingForSave(T1)).toMatchObject({
            freqHz: null,
            band: '',
            mode: '',
            adifMode: '',
            subMode: '',
        });
    });

    it('R5 a check that began before a later loss cannot retire its reading', () => {
        const before = rigDropEpoch(); // a check starts with no loss seen
        noteRigDrop(T0); // the loss happens while it waits
        retireRigSnapshot(before); // its (older) answer arrives
        expect(rigSnapshotHeld()).toBe(true);
        expect(rigReadingForSave(T2).basis).toBe('before-drop');
        const during = rigDropEpoch();
        noteRigDrop(T1); // a further loss while a later check waits
        retireRigSnapshot(during);
        expect(rigSnapshotHeld()).toBe(true);
        retireRigSnapshot(rigDropEpoch());
        expect(rigSnapshotHeld()).toBe(false);
    });
});
