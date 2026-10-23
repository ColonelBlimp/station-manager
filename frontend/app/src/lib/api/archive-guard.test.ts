import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
    ARCHIVE_HEADER,
    setArchiveBindingGate,
    setArchiveMismatchSink,
    setBoundArchive,
} from './archive-guard';
import { submitQso } from './qso';
import { fetchQso, patchQso } from './qso-patch';
import { exportSessionAdif } from './session-export';
import { sendSessionEmail } from './session-email';
import { armFt8Tx } from './ft8tx';
import { claimFt8Profile } from './ft8claim';
import {
    abandonFt8Qso,
    bagFt8Answerer,
    nextFt8Answerer,
    pickFt8CqAnswerer,
    resumeFt8Drain,
    skipFt8Qso,
    startFt8Cq,
    startFt8Qso,
    startFt8WorkCaller,
    stopFt8AutoWork,
    unbagFt8Answerer,
} from './ft8qso';

// The client half of the archive request guard (ADR 0094 update; W-0021
// "alpha.4 hold: fix design", revision 3). A request that acts on the archive or
// admits transmission names the tab's proven archive; with no proven binding it
// is not sent at all. Stop and reduce controls are never held back and never
// carry the header. Only the daemon's archive_mismatch starts a rebind, and the
// refused request is never retried against the replacement archive.

const BOUND = '01a163a7-0f04-7f69-85e9-255664b58d11';
const QSO = '01a168c1-2998-7db9-bab1-fc7b0ce4d302';

type Call = { url: string; header: string | null };
let calls: Call[];

function stubFetch(status = 202, body: unknown = {}): void {
    calls = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            calls.push({
                url: input,
                header: new Headers(init?.headers).get(ARCHIVE_HEADER),
            });
            return Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
}

const guarded: [string, () => Promise<unknown>][] = [
    ['submit', () => submitQso('<CALL:5>VK0YY<EOR>', 1)],
    ['qso read for edit', () => fetchQso(QSO)],
    ['qso edit', () => patchQso(QSO, { comment: 'x' })],
    ['session export', () => exportSessionAdif([QSO])],
    ['session email', () => sendSessionEmail({ to: 'm@example.com', uuids: [QSO] })],
    ['ft8 arm', () => armFt8Tx(true)],
    ['ft8 answer', () => startFt8Qso('K1ABC', 'FN42', '2026-10-23T12:00:00Z', 1500, 14.074)],
    [
        'ft8 work',
        () => startFt8WorkCaller('K1ABC', 'FN42', -5, '2026-10-23T12:00:00Z', 1500, 14.074),
    ],
    ['ft8 call cq', () => startFt8Cq(1500, 14.074)],
    ['ft8 pick', () => pickFt8CqAnswerer('K1ABC')],
    ['ft8 bag', () => bagFt8Answerer('K1ABC')],
    ['ft8 resume', () => resumeFt8Drain()],
    ['ft8 next', () => nextFt8Answerer()],
    ['ft8 skip off', () => skipFt8Qso(false)],
];

const exempt: [string, () => Promise<unknown>][] = [
    ['ft8 disarm', () => armFt8Tx(false)],
    ['ft8 abandon', () => abandonFt8Qso()],
    ['ft8 autowork stop', () => stopFt8AutoWork()],
    ['ft8 unbag', () => unbagFt8Answerer('K1ABC')],
    ['ft8 skip on', () => skipFt8Qso(true)],
    ['ft8 claim', () => claimFt8Profile('ft8')],
];

let mismatches: number;

beforeEach(() => {
    mismatches = 0;
    setBoundArchive(BOUND);
    setArchiveBindingGate(() => false);
    setArchiveMismatchSink(() => {
        mismatches++;
    });
});

afterEach(() => {
    setBoundArchive(null);
    setArchiveBindingGate(() => false);
    setArchiveMismatchSink(() => {});
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

describe('archive guard: guarded requests', () => {
    it.each(guarded)('%s names the bound archive', async (_name, run) => {
        stubFetch();
        await run();
        expect(calls).toHaveLength(1);
        expect(calls[0].header).toBe(BOUND);
    });

    it.each(guarded)('%s is not sent before the archive is proven', async (_name, run) => {
        stubFetch();
        setBoundArchive(null);
        await run();
        expect(calls).toHaveLength(0);
    });

    it.each(guarded)('%s is not sent while the binding is being re-checked', async (_name, run) => {
        stubFetch();
        setArchiveBindingGate(() => true);
        await run();
        expect(calls).toHaveLength(0);
    });

    it.each(guarded)(
        '%s: archive_mismatch starts one rebind and is not retried',
        async (_name, run) => {
            stubFetch(409, { code: 'archive_mismatch', message: 'nothing was done' });
            await run();
            await Promise.resolve(); // the mismatch check reads a cloned body
            await new Promise((r) => setTimeout(r, 0));
            expect(calls).toHaveLength(1);
            expect(mismatches).toBe(1);
        }
    );
});

describe('archive guard: other refusals keep their behaviour', () => {
    it.each([
        ['edit_conflict', () => patchQso(QSO, { comment: 'x' })],
        ['duplicate_key', () => patchQso(QSO, { comment: 'x' })],
        ['ft8_tx_not_armed', () => startFt8Cq(1500, 14.074)],
        ['ft8_no_cq_pick_run', () => pickFt8CqAnswerer('K1ABC')],
    ] as [string, () => Promise<unknown>][])('%s does not rebind', async (code, run) => {
        stubFetch(409, { code, message: 'refused' });
        await run();
        await new Promise((r) => setTimeout(r, 0));
        expect(mismatches).toBe(0);
    });
});

describe('archive guard: stop and reduce controls', () => {
    it.each(exempt)('%s carries no header', async (_name, run) => {
        stubFetch();
        await run();
        expect(calls).toHaveLength(1);
        expect(calls[0].header).toBeNull();
    });

    it.each(exempt)('%s is sent even with no proven archive', async (_name, run) => {
        stubFetch();
        setBoundArchive(null);
        setArchiveBindingGate(() => true);
        await run();
        expect(calls).toHaveLength(1);
    });
});
