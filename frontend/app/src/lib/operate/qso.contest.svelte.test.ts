// W-0023 weekend slice: the contest exchange on the Phone / CW draft.
//   - Committing the callsign (startQso) allocates the sent serial once per
//     draft; a re-Tab keeps it; a failed allocation retries with the same draft
//     key, so a lost response cannot consume a second number.
//   - Contest mode needs a Sent # to log; the exchange travels with CONTEST_ID.
//   - Completion is captured once, at the first Log press, and kept across
//     retries.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { flushSync } from 'svelte';
import {
    draft,
    clearDraft,
    canLog,
    draftProblems,
    logDraft,
    setSubmit,
    setSerialAllocator,
    startQso,
    contestBound,
    type SubmittedQso,
    type SubmitResult,
} from './qso.svelte';
import { contest } from './contest.svelte';
import { rig, confirmRig } from './rig.svelte';
import { _resetForTests as resetToasts, toastsState } from '../ui/toasts.svelte';

type Alloc = (
    contestId: string,
    start: string,
    key: string
) => Promise<{ ok: true; serial: number } | { ok: false; message: string }>;

let alloc: ReturnType<typeof vi.fn<Alloc>>;
let next = 42;

beforeEach(() => {
    rig.mode = 'USB';
    rig.cat = 'off';
    confirmRig();
    flushSync();
    clearDraft();
    resetToasts();
    contest.on = true;
    contest.id = 'UKEIDXSSB';
    contest.start = '2026-10-31';
    next = 42;
    alloc = vi.fn<Alloc>(() => Promise.resolve({ ok: true, serial: next++ }));
    setSerialAllocator(alloc);
});

afterEach(() => {
    clearDraft();
    contest.on = false;
    setSerialAllocator(null);
    vi.useRealTimers();
});

const settle = () => new Promise((r) => setTimeout(r, 0));

describe('serial allocation', () => {
    it('allocates once when the callsign is committed and keeps it on a re-Tab', async () => {
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(draft.stx).toBe('42');
        startQso(); // typo-fix re-Tab
        await settle();
        expect(alloc).toHaveBeenCalledTimes(1);
        expect(alloc.mock.calls[0].slice(0, 2)).toEqual(['UKEIDXSSB', '20261031']);
    });

    it('a failed allocation leaves Sent # empty, says so, and retries with the same draft key', async () => {
        alloc.mockResolvedValueOnce({ ok: false, message: 'daemon down' });
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(draft.stx).toBe('');
        expect(toastsState.items.some((t) => t.message.includes('Sent #'))).toBe(true);
        startQso(); // re-Tab retries
        await settle();
        expect(draft.stx).toBe('42');
        expect(alloc.mock.calls[1][2]).toBe(alloc.mock.calls[0][2]);
    });

    it('a new QSO gets a new draft key', async () => {
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        clearDraft();
        draft.callsign = 'EI2XYZ';
        startQso();
        await settle();
        expect(alloc.mock.calls[1][2]).not.toBe(alloc.mock.calls[0][2]);
        expect(draft.stx).toBe('43');
    });

    it('an allocation answering after the draft was cleared never lands in the next draft', async () => {
        let release: (v: { ok: true; serial: number }) => void = () => {};
        alloc.mockImplementationOnce(() => new Promise((r) => (release = r)));
        draft.callsign = 'G4ABC';
        startQso();
        clearDraft(); // abandoned while the request was out: its number stays consumed
        release({ ok: true, serial: 42 });
        await settle();
        expect(draft.stx).toBe('');
    });

    it('Clear while an allocation is out never stalls the next contact', async () => {
        let release: (v: { ok: true; serial: number }) => void = () => {};
        alloc.mockImplementationOnce(() => new Promise((r) => (release = r)));
        draft.callsign = 'G4ABC';
        startQso();
        clearDraft();
        draft.callsign = 'EI2XYZ';
        startQso(); // the next contact allocates at once, not after the old reply
        await settle();
        expect(alloc).toHaveBeenCalledTimes(2);
        expect(draft.stx).toBe('42');
        release({ ok: true, serial: 99 }); // the abandoned draft's reply: a gap
        await settle();
        expect(draft.stx).toBe('42');
    });

    it("the contest a serial was allocated under stays the QSO's contest", async () => {
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        contest.id = 'UKEIDXCW'; // changed mid-draft (the card prevents it; the store holds anyway)
        contest.start = '2027-04-24';
        startQso(); // re-Tab
        await settle();
        expect(alloc).toHaveBeenCalledTimes(1);
        let sent: SubmittedQso | null = null;
        setSubmit((q) => {
            sent = q;
            return Promise.resolve({ ok: true });
        });
        expect(await logDraft()).toBe(true);
        expect(sent).toMatchObject({ contestId: 'UKEIDXSSB', contestStart: '20261031', stx: '42' });
    });

    it('without a valid start date nothing is allocated, and it says why', async () => {
        for (const start of ['', '31/10/2026', '2026-02-30']) {
            clearDraft();
            resetToasts();
            contest.start = start;
            draft.callsign = 'G4ABC';
            startQso();
            await settle();
            expect(alloc).not.toHaveBeenCalled();
            expect(toastsState.items.some((t) => t.message.includes('start date'))).toBe(true);
        }
    });

    it('a bad start date is corrected mid-QSO: re-Tab then allocates and the contact logs', async () => {
        const sent: SubmittedQso[] = [];
        setSubmit((q) => {
            sent.push(q);
            return Promise.resolve({ ok: true });
        });
        contest.start = '2026-10-3';
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(contestBound()).toBe(false); // nothing allocated: the date stays editable
        contest.start = '2026-10-31';
        startQso(); // re-Tab
        await settle();
        expect(draft.stx).toBe('42');
        expect(contestBound()).toBe(true);
        expect(await logDraft()).toBe(true);
        expect(sent[0]).toMatchObject({ contestStart: '20261031', stx: '42' });
    });

    it('a typed Sent # cannot log without a valid start date', async () => {
        alloc.mockResolvedValue({ ok: false, message: 'daemon down' });
        contest.start = '';
        draft.callsign = 'G4ABC';
        draft.dateOn = '2026-10-31';
        draft.timeOn = '12:01:30';
        draft.stx = '17'; // typed by hand
        expect(canLog()).toBe(false);
        contest.start = '2026-10-31';
        expect(canLog()).toBe(true);
        let sent: SubmittedQso | null = null;
        setSubmit((q) => {
            sent = q;
            return Promise.resolve({ ok: true });
        });
        expect(await logDraft()).toBe(true);
        expect(sent).toMatchObject({ contestStart: '20261031', stx: '17' });
    });

    it('an empty or malformed contest ID allocates nothing and cannot log, until corrected', async () => {
        for (const id of ['', '  ', 'UK EI', 'UKEI.DX', 'X'.repeat(41)]) {
            clearDraft();
            resetToasts();
            contest.id = id;
            draft.callsign = 'G4ABC';
            startQso();
            await settle();
            expect(alloc).not.toHaveBeenCalled();
            expect(contestBound()).toBe(false);
            expect(toastsState.items.some((t) => t.message.includes('contest ID'))).toBe(true);
            draft.stx = '17'; // typed by hand
            expect(canLog()).toBe(false);
        }
        contest.id = 'ukeidxssb'; // corrected (case is normalised)
        expect(canLog()).toBe(true);
        let sent: SubmittedQso | null = null;
        setSubmit((q) => {
            sent = q;
            return Promise.resolve({ ok: true });
        });
        expect(await logDraft()).toBe(true);
        expect(sent).toMatchObject({ contestId: 'UKEIDXSSB', contestStart: '20261031', stx: '17' });
    });

    it('outside contest mode nothing is allocated', async () => {
        contest.on = false;
        draft.callsign = 'G4ABC';
        startQso();
        await settle();
        expect(alloc).not.toHaveBeenCalled();
    });
});

describe('logging a contest QSO', () => {
    let submitted: SubmittedQso[];
    let results: SubmitResult[];
    beforeEach(() => {
        submitted = [];
        results = [];
        setSubmit((q) => {
            submitted.push(q);
            return Promise.resolve(results.shift() ?? { ok: true });
        });
    });

    function fill(): void {
        draft.callsign = 'G4ABC';
        draft.dateOn = '2026-10-31';
        draft.timeOn = '12:01:30';
    }

    it('needs a Sent # in contest mode, and flags a non-numeric exchange', () => {
        fill();
        expect(canLog()).toBe(false);
        draft.stx = '42';
        expect(canLog()).toBe(true);
        draft.srx = '3a';
        expect(draftProblems().srx).toBe(true);
        expect(canLog()).toBe(false);
        draft.srx = '36';
        draft.srxString = 'O1';
        expect(draftProblems().srxString).toBe(true);
        draft.srxString = 'ox';
        expect(draftProblems().srxString).toBe(false);
    });

    it('submits CONTEST_ID with the exchange', async () => {
        fill();
        Object.assign(draft, { stx: '42', srx: '36', srxString: 'ox' });
        expect(await logDraft()).toBe(true);
        expect(submitted[0]).toMatchObject({
            contestId: 'UKEIDXSSB',
            contestStart: '20261031',
            stx: '42',
            srx: '36',
            srxString: 'OX',
        });
    });

    it('outside contest mode sends no contest fields', async () => {
        contest.on = false;
        fill();
        Object.assign(draft, { stx: '42', srx: '36', srxString: 'OX' });
        expect(await logDraft()).toBe(true);
        expect(submitted[0]).toMatchObject({
            contestId: '',
            contestStart: '',
            stx: '',
            srx: '',
            srxString: '',
        });
    });

    it('takes completion at the Log press itself, not the last clock tick', async () => {
        vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] });
        vi.setSystemTime(new Date('2026-10-31T12:01:30Z'));
        draft.callsign = 'G4ABC';
        startQso();
        draft.stx = '42';
        vi.advanceTimersByTime(29_000); // last tick 12:01:59
        vi.setSystemTime(new Date('2026-10-31T12:02:00.600Z')); // pressed before the next tick
        expect(await logDraft()).toBe(true);
        expect(submitted[0].timeOff).toBe('12:02:00');
    });

    it('keeps an end time the operator typed', async () => {
        fill();
        draft.stx = '42';
        draft.dateOff = '2026-10-31';
        draft.timeOff = '12:03:00';
        expect(await logDraft()).toBe(true);
        expect(submitted[0].timeOff).toBe('12:03:00');
    });

    it('captures completion at the first Log press and keeps it across a retry', async () => {
        vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'Date'] });
        vi.setSystemTime(new Date('2026-10-31T12:01:30Z'));
        draft.callsign = 'G4ABC';
        startQso();
        draft.stx = '42';
        vi.setSystemTime(new Date('2026-10-31T12:02:10Z'));
        vi.advanceTimersByTime(1000);
        results.push({ ok: false, message: 'daemon busy' });
        expect(await logDraft()).toBe(false);
        const first = submitted[0].timeOff;
        vi.setSystemTime(new Date('2026-10-31T12:05:00Z'));
        vi.advanceTimersByTime(5000);
        expect(await logDraft()).toBe(true);
        expect(submitted[1].timeOff).toBe(first);
        expect(first).toMatch(/^12:02:1[01]$/);
    });
});
