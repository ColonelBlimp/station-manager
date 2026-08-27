// Shell build-identity store (W-0004 AC1/AC2/AC3). One always-loaded source of the
// running daemon's build + env for the Sidebar footer and the tab title — distinct
// from Settings' lazy About panel. Fetched once at boot, then once per SSE
// reconnection TRANSITION (an open that follows a drop), never on a schedule and
// never on the first connect (the boot fetch already covered that). An unreachable
// or malformed /v1/version resolves to an honest unavailable state; a version is
// never fabricated.

import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest';
import {
    buildIdentity,
    loadBuildIdentity,
    noteStreamError,
    noteStreamReopen,
    isDevDaemon,
    resetBuildIdentityForTests,
} from './buildIdentity.svelte';

function stubVersion(body: unknown, status = 200): ReturnType<typeof vi.fn> {
    const spy = vi.fn(() =>
        Promise.resolve(
            new Response(JSON.stringify(body), {
                status,
                headers: { 'Content-Type': 'application/json' },
            })
        )
    );
    vi.stubGlobal('fetch', spy);
    return spy;
}

beforeEach(() => resetBuildIdentityForTests());
afterEach(() => vi.restoreAllMocks());

describe('build identity store', () => {
    it('loads the daemon build and marks a development daemon', async () => {
        stubVersion({ daemon: 'v2.1.0-3-gdeadbee', go: 'go1.24.0', env: 'dev' });
        await loadBuildIdentity();
        expect(buildIdentity.status).toBe('ready');
        expect(buildIdentity.info?.daemon).toBe('v2.1.0-3-gdeadbee');
        expect(isDevDaemon()).toBe(true);
    });

    it('a release daemon is ready but not marked dev', async () => {
        stubVersion({ daemon: 'v2.1.0', go: 'go1.24.0', env: 'release' });
        await loadBuildIdentity();
        expect(buildIdentity.status).toBe('ready');
        expect(isDevDaemon()).toBe(false);
    });

    it('an unreachable/malformed version is unavailable, never a fabricated build (AC3)', async () => {
        stubVersion({ go: 'go1.24.0', env: 'dev' }); // no daemon → malformed
        await loadBuildIdentity();
        expect(buildIdentity.status).toBe('unavailable');
        expect(buildIdentity.info).toBeNull();
        expect(isDevDaemon()).toBe(false); // unknown identity must not claim DEV
    });

    it('a stale in-flight load never overwrites a newer result (ordering guard)', async () => {
        // The boot fetch and a reconnection fetch overlap. The OLDER (boot) request
        // resolves LAST with an outdated build; it must not clobber the newer result.
        const resolvers: Array<(r: Response) => void> = [];
        const json = (body: unknown) =>
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            });
        vi.stubGlobal(
            'fetch',
            vi.fn(() => new Promise<Response>((resolve) => resolvers.push(resolve)))
        );

        const boot = loadBuildIdentity(); // gen 1 — old build, will resolve last
        const reconnect = loadBuildIdentity(); // gen 2 — the current build

        resolvers[1](json({ daemon: 'v-new', go: 'go1.24.0', env: 'release' }));
        await reconnect;
        expect(buildIdentity.info?.daemon).toBe('v-new');

        resolvers[0](json({ daemon: 'v-old', go: 'go1.24.0', env: 'release' }));
        await boot;
        expect(buildIdentity.info?.daemon).toBe('v-new'); // stale boot result dropped
    });

    it('re-fetches once on a reconnection (error → reopen), not on the first open', async () => {
        const spy = stubVersion({ daemon: 'v1', go: 'go1.24.0', env: 'release' });

        // The first (boot) open has no preceding error — the boot fetch owns it.
        noteStreamReopen();
        await Promise.resolve();
        expect(spy).not.toHaveBeenCalled();

        // A drop then a reconnect is one transition → exactly one re-fetch.
        noteStreamError();
        noteStreamReopen();
        await vi.waitFor(() => expect(spy).toHaveBeenCalledTimes(1));

        // A further reopen with no new error is not a transition → no re-fetch.
        noteStreamReopen();
        await Promise.resolve();
        expect(spy).toHaveBeenCalledTimes(1);
    });
});
