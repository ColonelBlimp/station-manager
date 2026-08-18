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
    submitState,
    dismissDuplicate,
    rstDefaultFor,
    DEFAULT_RST_VOICE,
    DEFAULT_RST_CW,
    type QsoDraft,
} from './qso.svelte';
import { rig, confirmRig } from './rig.svelte';
import { commentHistory } from './commentHistory.svelte';
import { toastsState, _resetForTests as resetToasts } from '../ui/toasts.svelte';

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
    resetToasts();
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

    it('the tone digit is CW-only: 599 passes on CW, fails on USB and RTTY', () => {
        draft.rstSent = '599';
        rig.mode = 'CW';
        expect(draftProblems().rstSent).toBe(false);
        rig.mode = 'USB';
        expect(draftProblems().rstSent).toBe(true);
        rig.mode = 'RTTY';
        expect(draftProblems().rstSent).toBe(true);
    });

    it('CW also accepts a tone-less RS report', () => {
        rig.mode = 'CW';
        draft.rstSent = '59';
        expect(draftProblems().rstSent).toBe(false);
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

describe('submit outcomes: toasts for messages, card-local for the duplicate action', () => {
    it('success pushes an info toast, clears the draft, and returns true (refocus signal)', async () => {
        fillDraft();
        setSubmit(() => Promise.resolve({ ok: true }));
        expect(await logDraft()).toBe(true);
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0]).toMatchObject({ level: 'info', message: 'Logged DL3YA' });
        expect(draft.callsign).toBe(''); // next QSO starts clean
    });

    it('records the comment in the recent-comments history on the shared path — a forced "Log anyway" records it too', async () => {
        fillDraft();
        draft.comment = 'Tnx QSO 73';
        commentHistory.clear();
        setSubmit(() => Promise.resolve({ ok: true }));
        // force=true is the DuplicateDialog "Log anyway" path; it bypasses the
        // card's log handler, so recording must live in logDraft (review c4f7474d P2).
        expect(await logDraft(true)).toBe(true);
        expect(commentHistory.items).toContain('Tnx QSO 73');
    });

    it('a non-duplicate refusal is an error toast; the draft is preserved', async () => {
        fillDraft();
        setSubmit(() => Promise.resolve({ ok: false, message: 'daemon says no' }));
        expect(await logDraft()).toBe(false); // no refocus on refusal
        expect(toastsState.items).toHaveLength(1);
        expect(toastsState.items[0]).toMatchObject({ level: 'error', message: 'daemon says no' });
        expect(submitState.duplicate).toBe(false);
        expect(submitState.error).toBe('');
        expect(draft.callsign).toBe('DL3YA'); // nothing typed is lost
    });

    it('a duplicate refusal stays card-local (it carries the Log-anyway action) — no toast', async () => {
        fillDraft();
        setSubmit(() => Promise.resolve({ ok: false, message: 'Duplicate.', duplicate: true }));
        await logDraft();
        expect(toastsState.items).toHaveLength(0);
        expect(submitState.duplicate).toBe(true);
        expect(submitState.error).toBe('Duplicate.');
        expect(draft.callsign).toBe('DL3YA');
    });

    it('dismissDuplicate drops the refusal and keeps the draft', async () => {
        fillDraft();
        setSubmit(() => Promise.resolve({ ok: false, message: 'Duplicate.', duplicate: true }));
        await logDraft();
        dismissDuplicate();
        expect(submitState.duplicate).toBe(false);
        expect(submitState.error).toBe('');
        expect(draft.callsign).toBe('DL3YA'); // draft survives the cancel
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

describe('RX_PWR validation', () => {
    it('accepts empty (optional) and a bare integer / decimal', () => {
        for (const v of ['', '100', '5', '2.5', '.5', '100.', '0.0000001', '0']) {
            draft.rxPwr = v;
            expect(draftProblems().rxPwr, `rxPwr ${JSON.stringify(v)}`).toBe(false);
        }
    });

    it('flags malformed values (never transforms them into a different number)', () => {
        for (const v of ['2,5', '1e3', '-5', '100W', 'abc', '.']) {
            draft.rxPwr = v;
            expect(draftProblems().rxPwr, `rxPwr ${JSON.stringify(v)}`).toBe(true);
        }
    });

    it('a malformed RX power blocks logging until fixed', () => {
        fillDraft();
        expect(canLog()).toBe(true);
        draft.rxPwr = '2,5';
        expect(canLog()).toBe(false);
        draft.rxPwr = '2.5';
        expect(canLog()).toBe(true);
    });
});
