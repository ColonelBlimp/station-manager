import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
    ARCHIVE_HEADER,
    DEFAULT_HEADER,
    setArchiveBindingGate,
    setArchiveMismatchSink,
    setBoundArchive,
    setBoundDefault,
    setDefaultMismatchSink,
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

// W-0024 slice 4, the client half of the default guard (ADR 0097 §4). A live
// request — the Phone / CW submit and every FT8 request that admits a new
// session or transmission — names the default logbook this tab proved at boot,
// beside its archive. With no proven default it is not sent. Other archive
// requests carry no default; stop and reduce controls carry neither. The
// daemon's default_logbook_mismatch reloads the tab once and is never retried.

const ARCHIVE = '01a163a7-0f04-7f69-85e9-255664b58d11';
const DEFAULT = '01a187bc-0173-7649-8ff3-71db8170c309';
const QSO = '01a168c1-2998-7db9-bab1-fc7b0ce4d302';
// The live submit's pair: the logbook it names and the default it asserts,
// both from the one station context.
const LIVE = { id: 1, uuid: DEFAULT };
const OTHER_DEFAULT = '01a187bc-0200-7000-8000-000000000002';

type Call = { url: string; archive: string | null; defaultLogbook: string | null };
let calls: Call[];

function stubFetch(status = 202, body: unknown = {}): void {
    calls = [];
    vi.stubGlobal(
        'fetch',
        vi.fn((input: string, init?: RequestInit) => {
            const h = new Headers(init?.headers);
            calls.push({
                url: input,
                archive: h.get(ARCHIVE_HEADER),
                defaultLogbook: h.get('X-SM-Default-Logbook'),
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

// FT8 has no ?logbook=: it asserts the tab's bound default (set from the same
// station context as the submit pair).
const liveFt8: [string, () => Promise<unknown>][] = [
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

const live: [string, () => Promise<unknown>][] = [
    ['submit', () => submitQso('<CALL:5>VK0YY<EOR>', LIVE)],
    ...liveFt8,
];

// Archive-guarded, but addressed to a QSO or a session: no default assertion.
const archiveOnly: [string, () => Promise<unknown>][] = [
    ['qso read for edit', () => fetchQso(QSO)],
    ['qso edit', () => patchQso(QSO, { comment: 'x' })],
    ['session export', () => exportSessionAdif([QSO])],
    ['session email', () => sendSessionEmail({ to: 'm@example.com', uuids: [QSO] })],
];

const exempt: [string, () => Promise<unknown>][] = [
    ['ft8 disarm', () => armFt8Tx(false)],
    ['ft8 abandon', () => abandonFt8Qso()],
    ['ft8 autowork stop', () => stopFt8AutoWork()],
    ['ft8 unbag', () => unbagFt8Answerer('K1ABC')],
    ['ft8 skip on', () => skipFt8Qso(true)],
    ['ft8 claim', () => claimFt8Profile('ft8')],
];

let defaultMismatches: number;
let archiveMismatches: number;

beforeEach(() => {
    defaultMismatches = 0;
    archiveMismatches = 0;
    setBoundArchive(ARCHIVE);
    setBoundDefault(DEFAULT);
    setArchiveBindingGate(() => false);
    setArchiveMismatchSink(() => {
        archiveMismatches++;
    });
    setDefaultMismatchSink(() => {
        defaultMismatches++;
    });
});

afterEach(() => {
    setBoundArchive(null);
    setBoundDefault(null);
    setArchiveBindingGate(() => false);
    setArchiveMismatchSink(() => {});
    setDefaultMismatchSink(() => {});
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

const settle = async (): Promise<void> => {
    await Promise.resolve();
    await new Promise((r) => setTimeout(r, 0));
};

describe('default guard: live requests', () => {
    it('names the header the daemon checks', () => {
        expect(DEFAULT_HEADER).toBe('X-SM-Default-Logbook');
    });

    it.each(live)('%s names the proven default beside the archive', async (_name, run) => {
        stubFetch();
        await run();
        expect(calls).toHaveLength(1);
        expect(calls[0].archive).toBe(ARCHIVE);
        expect(calls[0].defaultLogbook).toBe(DEFAULT);
    });

    it.each(liveFt8)('%s is not sent before a default is proven', async (_name, run) => {
        stubFetch();
        setBoundDefault(null);
        await run();
        expect(calls).toHaveLength(0);
    });

    it.each(live)(
        '%s: default_logbook_mismatch reloads once and is not retried',
        async (_name, run) => {
            stubFetch(409, { code: 'default_logbook_mismatch', message: 'nothing was done' });
            await run();
            await settle();
            expect(calls).toHaveLength(1);
            expect(defaultMismatches).toBe(1);
            expect(archiveMismatches).toBe(0);
        }
    );

    it('the mismatch is reported to the caller as a refusal in the daemon’s words', async () => {
        stubFetch(409, {
            code: 'default_logbook_mismatch',
            message: 'this page shows a different default logbook; nothing was logged',
        });
        const out = await submitQso('<CALL:5>VK0YY<EOR>', LIVE);
        expect(out).toEqual({
            kind: 'validation',
            code: 'default_logbook_mismatch',
            message: 'this page shows a different default logbook; nothing was logged',
        });
    });
});

describe('default guard: the submit pair', () => {
    // The daemon permits a submit naming a logbook other than the asserted
    // default (deliberate), so a header refreshed apart from ?logbook= would
    // turn a stale live submit into a "deliberate" one. The header is the
    // pair's own UUID, never a separately held binding.
    it('the submit asserts its own pair’s UUID, not the FT8 binding', async () => {
        stubFetch();
        setBoundDefault(OTHER_DEFAULT);
        await submitQso('<CALL:5>VK0YY<EOR>', LIVE);
        expect(calls).toHaveLength(1);
        expect(calls[0].url).toBe('/v1/qso?logbook=1');
        expect(calls[0].defaultLogbook).toBe(DEFAULT);
    });

    it('a pair with no UUID is not sent', async () => {
        stubFetch();
        await submitQso('<CALL:5>VK0YY<EOR>', { id: 1, uuid: '' });
        expect(calls).toHaveLength(0);
    });
});

describe('default guard: other requests', () => {
    it.each(archiveOnly)('%s names the archive and no default', async (_name, run) => {
        stubFetch();
        await run();
        expect(calls).toHaveLength(1);
        expect(calls[0].archive).toBe(ARCHIVE);
        expect(calls[0].defaultLogbook).toBeNull();
    });

    it.each(archiveOnly)('%s is still sent with no proven default', async (_name, run) => {
        stubFetch();
        setBoundDefault(null);
        await run();
        expect(calls).toHaveLength(1);
    });

    it.each(exempt)('%s carries neither header, with or without a default', async (_name, run) => {
        stubFetch();
        setBoundDefault(null);
        await run();
        expect(calls).toHaveLength(1);
        expect(calls[0].archive).toBeNull();
        expect(calls[0].defaultLogbook).toBeNull();
    });

    it.each([
        ['ft8_tx_not_armed', () => startFt8Cq(1500, 14.074)],
        ['default_change_pending', () => submitQso('<CALL:5>VK0YY<EOR>', LIVE)],
    ] as [string, () => Promise<unknown>][])('%s does not reload', async (code, run) => {
        stubFetch(409, { code, message: 'refused' });
        await run();
        await settle();
        expect(defaultMismatches).toBe(0);
    });
});
