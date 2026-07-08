// Integration tests for the mode-aware report validation path — real rig
// state + real draft + real validators/mode table, observed through the
// injected submit seam. This path exists for manual FT8/FT4 entry, which the
// operator essentially never drives by hand (FT8 QSOs arrive via the FT8
// surface), so these tests are the only thing exercising it routinely.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import {
    draft,
    clearDraft,
    draftProblems,
    canLog,
    logDraft,
    setSubmit,
    rstDefaultFor,
    DEFAULT_RST_VOICE,
    DEFAULT_RST_CW,
    type QsoDraft,
} from './qso.svelte';
import { rig, confirmRig } from './rig.svelte';

// A loggable draft, minus the report fields under test. Times are set
// directly (not via startQso) so no interval ticks over the values.
function fillDraft(): void {
    draft.callsign = 'DL3YA';
    draft.dateOn = '2026-07-07';
    draft.timeOn = '14:30:00';
}

beforeEach(() => {
    rig.mode = 'USB';
    rig.cat = 'off';
    confirmRig(); // satisfy the ADR 0044 gate — logDraft refuses unconfirmed CAT-off
    flushSync(); // settle the RST default-fill effect before the case starts
    clearDraft(); // also resets rstSent/rstRcvd to the mode default
});

describe('report validation follows rig mode', () => {
    it('voice mode accepts RST digits and rejects a signed SNR', () => {
        rig.mode = 'USB';
        draft.rstSent = '59';
        draft.rstRcvd = '-12';
        const p = draftProblems();
        expect(p.rstSent).toBe(false);
        expect(p.rstRcvd).toBe(true);
    });

    it('FT8 accepts signed dB SNR reports', () => {
        rig.mode = 'FT8';
        for (const report of ['-12', '+04', '0', '15']) {
            draft.rstSent = report;
            expect(draftProblems().rstSent, `report ${report}`).toBe(false);
        }
    });

    it('FT8 rejects a three-digit RST', () => {
        rig.mode = 'FT8';
        draft.rstSent = '599';
        expect(draftProblems().rstSent).toBe(true);
    });

    it('the same draft flips validity when the mode flips — no re-entry needed', () => {
        draft.rstSent = '-12';
        rig.mode = 'USB';
        expect(draftProblems().rstSent).toBe(true);
        rig.mode = 'FT8';
        expect(draftProblems().rstSent).toBe(false);
        rig.mode = 'FT4';
        expect(draftProblems().rstSent).toBe(false);
        rig.mode = 'CW';
        expect(draftProblems().rstSent).toBe(true);
    });

    it('non-WSJT-X digital modes still use RST (RTTY, PSK31)', () => {
        draft.rstSent = '-12';
        rig.mode = 'RTTY';
        expect(draftProblems().rstSent).toBe(true);
        rig.mode = 'PSK31';
        expect(draftProblems().rstSent).toBe(true);
    });
});

describe('RST defaults follow the rig mode (default-fill effect)', () => {
    it('rstDefaultFor: CW gets the tone digit, everything else R/S', () => {
        expect(rstDefaultFor('CW')).toBe(DEFAULT_RST_CW);
        expect(rstDefaultFor('USB')).toBe(DEFAULT_RST_VOICE);
        expect(rstDefaultFor('FT8')).toBe(DEFAULT_RST_VOICE); // '59' also passes the SNR pattern
    });

    it('refills both fields when the mode crosses the CW ↔ voice boundary', () => {
        rig.mode = 'CW';
        flushSync();
        expect(draft.rstSent).toBe('599');
        expect(draft.rstRcvd).toBe('599');
        rig.mode = 'USB';
        flushSync();
        expect(draft.rstSent).toBe('59');
        expect(draft.rstRcvd).toBe('59');
    });

    it('a same-family mode change never touches an operator-typed report', () => {
        draft.rstSent = '57';
        rig.mode = 'LSB'; // default stays '59' → memoized derived → no re-fire
        flushSync();
        expect(draft.rstSent).toBe('57');
        rig.mode = 'FT8'; // still '59' — USB/LSB/FT8 share the default
        flushSync();
        expect(draft.rstSent).toBe('57');
    });

    it('the boundary flip deliberately clobbers a typed value (59 is meaningless on CW)', () => {
        draft.rstSent = '47';
        rig.mode = 'CW';
        flushSync();
        expect(draft.rstSent).toBe('599');
    });

    it('clearDraft resets to the CURRENT mode default, not a hardcoded 59', () => {
        rig.mode = 'CW';
        flushSync();
        draft.rstSent = '579';
        clearDraft();
        expect(draft.rstSent).toBe('599');
        expect(draft.rstRcvd).toBe('599');
    });
});

describe('logging a manual FT8 QSO end-to-end (state modules + submit seam)', () => {
    it('submits with SNR reports intact when the mode is FT8', async () => {
        rig.mode = 'FT8';
        fillDraft();
        draft.rstSent = '-12';
        draft.rstRcvd = '+04';
        expect(canLog()).toBe(true);

        const submitted: QsoDraft[] = [];
        setSubmit((q) => {
            submitted.push(q);
            return Promise.resolve({ ok: true });
        });
        await logDraft();

        expect(submitted).toHaveLength(1);
        expect(submitted[0].rstSent).toBe('-12');
        expect(submitted[0].rstRcvd).toBe('+04');
    });

    it('blocks the same SNR reports in a voice mode — submit is never called', async () => {
        rig.mode = 'USB';
        fillDraft();
        draft.rstSent = '-12';
        draft.rstRcvd = '+04';
        expect(canLog()).toBe(false);

        const submit = vi.fn(() => Promise.resolve({ ok: true as const }));
        setSubmit(submit);
        await logDraft();

        expect(submit).not.toHaveBeenCalled();
    });
});
