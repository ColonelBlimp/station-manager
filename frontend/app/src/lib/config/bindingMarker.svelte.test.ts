import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';

vi.mock('../api/qso-archives', () => ({
    fetchQsoArchives: vi.fn(),
    createQsoArchive: vi.fn(),
    activateQsoArchive: vi.fn(),
    fetchDaemonIdentity: vi.fn(),
}));
vi.mock('../api/restart', () => ({
    fetchDaemonInstance: vi.fn(),
    waitForDaemonBack: vi.fn(),
}));

import { fetchDaemonIdentity, type DaemonIdentity } from '../api/qso-archives';
import { setBoundDefault } from '../api/archive-guard';
import {
    archivesState,
    archiveSwitchGate,
    bootArchiveScoped,
    verifyAfterRigReconnect,
    verifyArchiveGeneration,
    _resetArchivesForTests,
    _setReloadForTests,
} from './archives.svelte';
import {
    defaultChangeGate,
    defaultChangeState,
    _resetDefaultChangeForTests,
} from './defaultChange.svelte';
import { computeBindingMarker, publishBindingMarker } from './bindingMarker.svelte';

/*
    THE READ-ONLY BINDING MARKER (ADR 0099 W1-2; reviewer contract 2026-11-05).

    Four attributes on the root element, so an acceptance driver can observe,
    passively and in every window, what this tab is bound to:
      data-sm-archive            the actual bound archive UUID
      data-sm-default            the established default logbook UUID
      data-sm-binding            verifying | verified | unresolved | unproven
      data-sm-verified-instance  the daemon instance accepted by the latest
                                 SUCCESSFUL binding verification
    Rules pinned here:
      M1  never 'verified' while a verification is pending or an existing gate
          blocks operations (the marker agrees with archiveSwitchGate and
          defaultChangeGate);
      M2  success is recorded only after the identity AND binding checks pass —
          an unreadable identity or a mismatch records nothing;
      M3  overlapping checks: an OLDER check completing later never overwrites
          the evidence of a NEWER one;
      M4  observational: publishing makes no requests, and no application code
          reads the attributes.
*/

const A = '01a1a6a9-39e5-755a-9ee5-befda572b95c';
const B = '01a1a6a9-39e5-755a-9ee5-000000000000';
const D1 = '01a1a6a9-f11a-73a9-9da8-ef0df1a3263e';
const D2 = '01a1a6a9-f11a-73a9-9da8-000000000002';
const id = (instance: string, archiveId = A, def = D1): DaemonIdentity => ({
    instance,
    archiveId,
    defaultLogbookUuid: def,
});

function deferred<T>() {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => (resolve = r));
    return { promise, resolve };
}

const reload = vi.fn();
const mockIdentity = vi.mocked(fetchDaemonIdentity);

async function bootAs(instance: string, def = D1): Promise<void> {
    mockIdentity
        .mockResolvedValueOnce(id(instance, A, def))
        .mockResolvedValueOnce(id(instance, A, def));
    await bootArchiveScoped(
        () => {
            setBoundDefault(def); // what applyStationContext does inside the bracket
            return Promise.resolve({ def });
        },
        (r) => r.def
    );
}

const gatesOpen = (): boolean => archiveSwitchGate() === null && defaultChangeGate() === null;

beforeEach(() => {
    _resetArchivesForTests();
    _resetDefaultChangeForTests();
    defaultChangeState.read = 'ok';
    setBoundDefault(null);
    reload.mockReset();
    _setReloadForTests(reload);
    mockIdentity.mockReset();
});
afterEach(() => {
    vi.unstubAllGlobals();
});

describe('binding marker', () => {
    it('before any proof: unproven, nothing bound, no instance', () => {
        expect(computeBindingMarker()).toEqual({
            archive: '',
            default: '',
            binding: 'unproven',
            verifiedInstance: '',
        });
    });

    it('boot: verifying while the bracket runs, then verified with the actual binding and the bracket instance', async () => {
        const first = deferred<DaemonIdentity | null>();
        mockIdentity.mockReturnValueOnce(first.promise).mockResolvedValueOnce(id('i1'));
        const booted = bootArchiveScoped(
            () => {
                setBoundDefault(D1);
                return Promise.resolve({ def: D1 });
            },
            (r) => r.def
        );
        await Promise.resolve();
        expect(computeBindingMarker().binding).toBe('verifying');
        expect(computeBindingMarker().verifiedInstance).toBe('');
        first.resolve(id('i1'));
        await booted;
        expect(computeBindingMarker()).toEqual({
            archive: A,
            default: D1,
            binding: 'verified',
            verifiedInstance: 'i1',
        });
        expect(gatesOpen()).toBe(true);
    });

    it('first-run: the default appears when setup establishes the context', async () => {
        await bootAs('i1', '');
        setBoundDefault(null); // no default until setup saves
        expect(computeBindingMarker().default).toBe('');
        setBoundDefault(D1); // setSetupSave → applyStationContext
        const m = computeBindingMarker();
        expect(m.default).toBe(D1);
        expect(m.archive).toBe(A);
    });

    it('a successful reconnect check records the NEW instance; while pending the marker is verifying and the gate agrees', async () => {
        await bootAs('i1');
        const read = deferred<DaemonIdentity | null>();
        mockIdentity.mockReturnValueOnce(read.promise);
        const check = verifyArchiveGeneration();
        await Promise.resolve();
        expect(computeBindingMarker().binding).toBe('verifying');
        expect(archiveSwitchGate()).not.toBeNull(); // M1: the marker and the gate agree
        read.resolve(id('i2'));
        await check;
        expect(computeBindingMarker()).toEqual({
            archive: A,
            default: D1,
            binding: 'verified',
            verifiedInstance: 'i2',
        });
        expect(gatesOpen()).toBe(true);
    });

    it('the rig-stream reconnect check records the same way', async () => {
        await bootAs('i1');
        mockIdentity.mockResolvedValueOnce(id('i2'));
        await verifyAfterRigReconnect();
        expect(computeBindingMarker().verifiedInstance).toBe('i2');
        expect(computeBindingMarker().binding).toBe('verified');
    });

    it('M2: an unreadable identity records nothing — unresolved, the old instance kept', async () => {
        await bootAs('i1');
        mockIdentity.mockResolvedValue(null);
        await verifyArchiveGeneration();
        expect(computeBindingMarker().binding).toBe('unresolved');
        expect(computeBindingMarker().verifiedInstance).toBe('i1');
        expect(archiveSwitchGate()).not.toBeNull();
    });

    it('M2: an archive mismatch records nothing (the page reloads)', async () => {
        await bootAs('i1');
        mockIdentity.mockResolvedValueOnce(id('i2', B));
        await verifyArchiveGeneration();
        expect(reload).toHaveBeenCalledOnce();
        expect(computeBindingMarker().binding).toBe('unresolved');
        expect(computeBindingMarker().verifiedInstance).toBe('i1');
    });

    it('M2: a default mismatch records nothing (the page reloads)', async () => {
        await bootAs('i1');
        mockIdentity.mockResolvedValueOnce(id('i2', A, D2));
        await verifyArchiveGeneration();
        expect(reload).toHaveBeenCalledOnce();
        expect(computeBindingMarker().binding).toBe('unresolved');
        expect(computeBindingMarker().verifiedInstance).toBe('i1');
    });

    it('M3: overlapping checks completing out of order — the newer check’s evidence stands', async () => {
        await bootAs('i1');
        const older = deferred<DaemonIdentity | null>();
        const newer = deferred<DaemonIdentity | null>();
        mockIdentity.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise);
        const a = verifyArchiveGeneration(); // started first
        const b = verifyAfterRigReconnect(); // started second
        await Promise.resolve();
        newer.resolve(id('i3'));
        await b;
        expect(computeBindingMarker().verifiedInstance).toBe('i3');
        expect(computeBindingMarker().binding).toBe('verifying'); // the older check is still pending
        older.resolve(id('i2')); // a stale answer from before
        await a;
        expect(computeBindingMarker().verifiedInstance).toBe('i3');
        expect(computeBindingMarker().binding).toBe('verified');
    });

    it('M1: a default-change gate blocks verified', async () => {
        await bootAs('i1');
        defaultChangeState.requesting = true;
        expect(defaultChangeGate()).not.toBeNull();
        expect(computeBindingMarker().binding).toBe('verifying');
        defaultChangeState.requesting = false;
        defaultChangeState.unproven = true;
        defaultChangeState.read = 'unread';
        expect(computeBindingMarker().binding).toBe('verifying');
        defaultChangeState.read = 'unreadable';
        expect(computeBindingMarker().binding).toBe('unproven');
        defaultChangeState.unproven = false;
        defaultChangeState.read = 'ok';
        expect(computeBindingMarker().binding).toBe('verified');
    });

    it('M1: an activation in progress blocks verified', async () => {
        await bootAs('i1');
        archivesState.activating = true;
        expect(computeBindingMarker().binding).toBe('verifying');
        expect(archiveSwitchGate()).not.toBeNull();
    });

    it('M4: publishing writes the four attributes, follows changes, and makes no requests', async () => {
        const fetchSpy = vi.fn();
        vi.stubGlobal('fetch', fetchSpy);
        const el = document.createElement('div');
        const cleanup = $effect.root(() => {
            $effect(() => publishBindingMarker(el));
        });
        flushSync();
        expect(el.getAttribute('data-sm-binding')).toBe('unproven');
        await bootAs('i1');
        flushSync();
        expect(el.getAttribute('data-sm-archive')).toBe(A);
        expect(el.getAttribute('data-sm-default')).toBe(D1);
        expect(el.getAttribute('data-sm-binding')).toBe('verified');
        expect(el.getAttribute('data-sm-verified-instance')).toBe('i1');
        mockIdentity.mockResolvedValueOnce(id('i2'));
        await verifyArchiveGeneration();
        flushSync();
        expect(el.getAttribute('data-sm-verified-instance')).toBe('i2');
        expect(fetchSpy).not.toHaveBeenCalled();
        cleanup();
    });

    it('M4: no application code reads the marker attributes', () => {
        const sources = import.meta.glob('/src/**/*.{ts,svelte}', {
            query: '?raw',
            import: 'default',
            eager: true,
        });
        const readers = Object.entries(sources)
            .filter(([path]) => !/\.test\.|\/test\/|bindingMarker\.svelte\.ts$/.test(path))
            .filter(([, text]) =>
                /data-sm-(archive|default|binding|verified-instance)|dataset\.sm(Archive|Default|Binding|VerifiedInstance)/.test(
                    text
                )
            )
            .map(([path]) => path);
        expect(readers).toEqual([]);
    });
});
