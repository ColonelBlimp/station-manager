import { afterEach, describe, expect, it, vi } from 'vitest';
import {
    activateQsoArchive,
    createQsoArchive,
    fetchDaemonIdentity,
    fetchQsoArchives,
    toArchive,
} from './qso-archives';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockJSON(status: number, body: unknown) {
    const fn = vi.fn((_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
        Promise.resolve(
            new Response(status === 204 ? null : JSON.stringify(body), {
                status,
                headers: { 'Content-Type': 'application/json' },
            })
        )
    );
    vi.stubGlobal('fetch', fn);
    return fn;
}

const HOME = {
    id: 'a',
    label: 'Home',
    ownership: 'legacy',
    state: 'active',
    size_bytes: 12,
    modified_at: '2026-09-23T10:00:00Z',
};
const CONTEST = {
    id: 'b',
    label: 'Contest',
    ownership: 'managed',
    state: 'inactive',
    last_activation_error: 'x',
};

describe('fetchQsoArchives', () => {
    it('decodes the catalogue and keeps only well-formed rows', async () => {
        mockJSON(200, {
            archives: [
                HOME,
                CONTEST,
                { id: 'c', label: 'Odd', ownership: 'weird', state: 'active' },
            ],
        });
        const out = await fetchQsoArchives();
        expect(out.kind).toBe('ok');
        if (out.kind !== 'ok') return;
        expect(out.archives.map((a) => a.id)).toEqual(['a', 'b']);
        expect(out.archives[0]).toMatchObject({
            state: 'active',
            sizeBytes: 12,
            modifiedAt: '2026-09-23T10:00:00Z',
            lastActivationError: '',
        });
        expect(out.archives[1]).toMatchObject({
            state: 'inactive',
            sizeBytes: null,
            modifiedAt: null,
            lastActivationError: 'x',
        });
    });

    it('reports the daemon error on a non-2xx', async () => {
        mockJSON(503, { code: 'archives_unavailable', message: 'no manager' });
        const out = await fetchQsoArchives();
        expect(out.kind).toBe('error');
        if (out.kind === 'error') expect(out.message).toContain('no manager');
    });
});

describe('createQsoArchive', () => {
    it('sends the wire shape and returns the created archive', async () => {
        const fn = mockJSON(201, { archive: CONTEST, reused: false });
        const out = await createQsoArchive({
            requestKey: 'k1',
            label: 'Contest',
            logbookName: 'Contest',
            logbookCallsign: 'G4ABC',
        });
        expect(out).toMatchObject({ kind: 'ok', reused: false, archive: { id: 'b' } });
        const init = fn.mock.calls[0][1] as RequestInit;
        expect(JSON.parse(init.body as string) as unknown).toEqual({
            request_key: 'k1',
            label: 'Contest',
            logbook_name: 'Contest',
            logbook_callsign: 'G4ABC',
        });
    });

    it('a 200 is the reused archive', async () => {
        mockJSON(200, { archive: CONTEST, reused: true });
        const out = await createQsoArchive({
            requestKey: 'k1',
            label: 'x',
            logbookName: 'x',
            logbookCallsign: 'G4ABC',
        });
        expect(out).toMatchObject({ kind: 'ok', reused: true });
    });

    it('keeps the daemon code and message on a refusal', async () => {
        mockJSON(400, { code: 'missing_required_field', message: 'label is required' });
        const out = await createQsoArchive({
            requestKey: 'k1',
            label: '',
            logbookName: 'x',
            logbookCallsign: 'G4ABC',
        });
        expect(out).toEqual({
            kind: 'refused',
            code: 'missing_required_field',
            message: 'label is required',
        });
    });

    it('marks a fired timeout as the ambiguous write', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() =>
                Promise.reject(Object.assign(new Error('timed out'), { name: 'TimeoutError' }))
            )
        );
        const out = await createQsoArchive({
            requestKey: 'k1',
            label: 'x',
            logbookName: 'x',
            logbookCallsign: 'G4ABC',
        });
        expect(out.kind).toBe('network');
        if (out.kind === 'network') expect(out.timedOut).toBe(true);
    });

    it('a non-timeout transport failure is still `network` (unconfirmed), never `error`', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('connection reset')))
        );
        const out = await createQsoArchive({
            requestKey: 'k1',
            label: 'x',
            logbookName: 'x',
            logbookCallsign: 'G4ABC',
        });
        expect(out.kind).toBe('network');
        if (out.kind === 'network') expect(out.timedOut).not.toBe(true);
    });
});

describe('activateQsoArchive', () => {
    it('a 202 carries the durability and never claims active', async () => {
        const fn = mockJSON(202, { id: 'b', durability: 'uncertain' });
        const out = await activateQsoArchive('b');
        expect(out).toEqual({ kind: 'accepted', id: 'b', durability: 'uncertain' });
        expect(fn.mock.calls[0][0]).toBe('/v1/qso-archives/b/activate');
    });

    it('a transport failure of any kind is `network`: the write is unconfirmed', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('connection reset')))
        );
        const out = await activateQsoArchive('b');
        expect(out.kind).toBe('network');
    });

    it('an uncoded daemon answer is a definite `error`', async () => {
        mockJSON(500, { oops: true });
        const out = await activateQsoArchive('b');
        expect(out.kind).toBe('error');
    });

    it('a 409 keeps the daemon reason', async () => {
        mockJSON(409, { code: 'tx_busy', message: 'FT8 is busy: ft8: transmit is armed' });
        const out = await activateQsoArchive('b');
        expect(out).toEqual({
            kind: 'refused',
            code: 'tx_busy',
            message: 'FT8 is busy: ft8: transmit is armed',
        });
    });
});

describe('fetchDaemonIdentity', () => {
    it('reads the instance and the served archive', async () => {
        mockJSON(200, {
            daemon: 'dev',
            instance: 'i1',
            archive: { id: 'a', label: 'Home', ownership: 'legacy' },
        });
        expect(await fetchDaemonIdentity()).toEqual({ instance: 'i1', archiveId: 'a' });
    });
    it('a daemon without an adopted archive reports an empty archive id', async () => {
        mockJSON(200, { daemon: 'dev', instance: 'i1' });
        expect(await fetchDaemonIdentity()).toEqual({ instance: 'i1', archiveId: '' });
    });
    it('is null when unreachable or malformed', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.reject(new TypeError('down')))
        );
        expect(await fetchDaemonIdentity()).toBeNull();
        mockJSON(200, { daemon: 'dev' });
        expect(await fetchDaemonIdentity()).toBeNull();
    });
});

describe('toArchive', () => {
    it('refuses a row without a known state or ownership', () => {
        expect(toArchive({ id: 'a', label: 'x', ownership: 'managed', state: 'later' })).toBeNull();
        expect(toArchive({ id: 'a', label: 'x', ownership: 'cloud', state: 'active' })).toBeNull();
    });
});
