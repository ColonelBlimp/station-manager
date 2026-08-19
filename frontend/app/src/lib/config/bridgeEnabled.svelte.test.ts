import { afterEach, describe, expect, it, vi } from 'vitest';
import { bridgeEnabledState } from './bridgeEnabled.svelte';

afterEach(() => {
    vi.restoreAllMocks();
    // Reset the module singleton between cases.
    bridgeEnabledState.enabled = false;
    bridgeEnabledState.loaded = false;
    bridgeEnabledState.loading = false;
    bridgeEnabledState.saving = false;
    bridgeEnabledState.error = '';
    bridgeEnabledState.restartPending = false;
});

function mockDaemon(getEnabled: boolean, putStatus = 200, putBody: unknown = {}) {
    const ok = (b: unknown, status: number) =>
        new Response(JSON.stringify(b), {
            status,
            headers: { 'Content-Type': 'application/json' },
        });
    const spy = vi.fn((_url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
        if (init?.method === 'PUT') return Promise.resolve(ok(putBody, putStatus));
        return Promise.resolve(ok({ bridge_enabled: getEnabled }, 200));
    });
    vi.stubGlobal('fetch', spy);
    return spy;
}

describe('bridgeEnabledState (CAT master switch)', () => {
    it('load reads bridge_enabled from the config', async () => {
        mockDaemon(true);
        await bridgeEnabledState.load();
        expect(bridgeEnabledState.loaded).toBe(true);
        expect(bridgeEnabledState.enabled).toBe(true);
    });

    it('setEnabled PUTs ONLY bridge_enabled and marks a restart pending', async () => {
        const spy = mockDaemon(false);
        await bridgeEnabledState.load(); // enabled = false
        await bridgeEnabledState.setEnabled(true);

        const put = spy.mock.calls.find((c) => c[1]?.method === 'PUT');
        expect(put, 'a PUT was issued').toBeTruthy();
        const sent = JSON.parse((put![1] as RequestInit).body as string) as Record<string, unknown>;
        expect(sent).toEqual({ bridge_enabled: true }); // presence-aware: no other config touched
        expect(bridgeEnabledState.enabled).toBe(true);
        expect(bridgeEnabledState.restartPending).toBe(true);
    });

    it('a refused enable (400: active rig has no port/driver) reverts the toggle', async () => {
        mockDaemon(false, 400, { message: 'active rig has no port/driver' });
        await bridgeEnabledState.load(); // enabled = false
        await bridgeEnabledState.setEnabled(true);

        expect(bridgeEnabledState.enabled).toBe(false); // reverted — the daemon refused
        expect(bridgeEnabledState.restartPending).toBe(false);
        expect(bridgeEnabledState.error).toContain('port/driver');
    });

    it('setEnabled is a no-op when the value is unchanged (no PUT)', async () => {
        const spy = mockDaemon(true);
        await bridgeEnabledState.load(); // enabled = true
        const before = spy.mock.calls.length;
        await bridgeEnabledState.setEnabled(true); // same value
        expect(spy.mock.calls.length).toBe(before); // no PUT issued
    });

    it('a timed-out toggle re-reads the daemon instead of blindly reverting', async () => {
        let gets = 0;
        vi.stubGlobal(
            'fetch',
            vi.fn((_url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
                if (init?.method === 'PUT') {
                    const e = new Error('timed out');
                    e.name = 'TimeoutError';
                    return Promise.reject(e);
                }
                gets += 1;
                // load GET → false; the reconcile GET → true (the change DID land).
                const enabled = gets >= 2;
                return Promise.resolve(
                    new Response(JSON.stringify({ bridge_enabled: enabled }), {
                        status: 200,
                        headers: { 'Content-Type': 'application/json' },
                    })
                );
            })
        );
        await bridgeEnabledState.load(); // enabled = false
        await bridgeEnabledState.setEnabled(true); // PUT times out ⇒ reconcile re-reads → true

        expect(bridgeEnabledState.enabled).toBe(true); // adopted the re-read, NOT reverted to false
        expect(bridgeEnabledState.restartPending).toBe(true); // it changed ⇒ restart owed
    });

    it('a failed CAT-state load records the error and stays unloaded (control shows a retry)', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(() => Promise.resolve(new Response('{}', { status: 503 })))
        );
        await bridgeEnabledState.load();
        expect(bridgeEnabledState.loaded).toBe(false);
        expect(bridgeEnabledState.error).not.toBe('');
    });
});
