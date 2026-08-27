import { afterEach, describe, expect, it, vi } from 'vitest';
import { generalState } from './general.svelte';

afterEach(() => {
    vi.restoreAllMocks();
});

// URL-aware fetch stub: /v1/version → versionBody, everything else (/v1/config) → configBody.
// General's load() pulls both endpoints (config + About), so a single-body stub isn't enough.
function mockDaemon(
    config: unknown,
    version: unknown = { daemon: 'dev', go: 'go1.24.0' }
): ReturnType<typeof vi.fn> {
    const spy = vi.fn((url: RequestInfo | URL, _init?: RequestInit): Promise<Response> => {
        const u = typeof url === 'string' ? url : url instanceof URL ? url.href : url.url;
        const body = u.includes('/v1/version') ? version : config;
        return Promise.resolve(
            new Response(JSON.stringify(body), {
                status: 200,
                headers: { 'Content-Type': 'application/json' },
            })
        );
    });
    vi.stubGlobal('fetch', spy);
    return spy;
}

function configBody(over: Record<string, unknown> = {}) {
    return {
        restore_rig_on_mode_switch: true,
        map: { band_colors: { '20m': '#111111' } },
        ...over,
    };
}

function putCall(spy: ReturnType<typeof vi.fn>): { url: string; body: Record<string, unknown> } {
    const call = spy.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'PUT');
    if (!call) throw new Error('no PUT was issued');
    const body = JSON.parse((call[1] as RequestInit).body as string) as Record<string, unknown>;
    return { url: String(call[0]), body };
}

describe('generalState', () => {
    it('load fills the form from /v1/config and starts not-dirty', async () => {
        mockDaemon(configBody());
        await generalState.load();
        expect(generalState.loaded).toBe(true);
        expect(generalState.form.restoreRigOnModeSwitch).toBe(true);
        expect(generalState.form.bandColors).toEqual({ '20m': '#111111' });
        expect(generalState.dirty).toBe(false);
    });

    it('restore_rig_on_mode_switch defaults ON when absent, OFF only when explicit false', async () => {
        mockDaemon({ map: {} }); // key absent
        await generalState.load();
        expect(generalState.form.restoreRigOnModeSwitch).toBe(true);

        mockDaemon({ restore_rig_on_mode_switch: false, map: {} });
        await generalState.load();
        expect(generalState.form.restoreRigOnModeSwitch).toBe(false);
    });

    it('toggling the knob makes it dirty; reset reverts to the loaded value', async () => {
        mockDaemon(configBody());
        await generalState.load();
        generalState.form.restoreRigOnModeSwitch = false;
        expect(generalState.dirty).toBe(true);
        generalState.reset();
        expect(generalState.form.restoreRigOnModeSwitch).toBe(true);
        expect(generalState.dirty).toBe(false);
    });

    it('setBandColor sets a lowercased override; a colour equal to the default clears it (sparse)', async () => {
        mockDaemon(configBody({ map: { band_colors: {} } }));
        await generalState.load();
        generalState.setBandColor('40m', '#ABCDEF', '#eab308');
        expect(generalState.form.bandColors['40m']).toBe('#abcdef');
        expect(generalState.dirty).toBe(true);
        // Back to the default ⇒ no override, block stays sparse.
        generalState.setBandColor('40m', '#EAB308', '#eab308');
        expect('40m' in generalState.form.bandColors).toBe(false);
    });

    it('save PUTs restore + the WHOLE map (round-tripping other map fields), and no other block', async () => {
        const spy = mockDaemon(
            configBody({ map: { band_colors: { '20m': '#111111' }, center: 'JJ00' } })
        );
        await generalState.load();
        generalState.form.restoreRigOnModeSwitch = false;
        generalState.setBandColor('20m', '#222222', '#22c55e');
        await generalState.save();

        const { url, body } = putCall(spy);
        expect(url).toContain('/v1/config');
        expect(body.restore_rig_on_mode_switch).toBe(false);
        expect((body.map as Record<string, unknown>).band_colors).toEqual({ '20m': '#222222' });
        // Other map fields survive — the edit must never zero them.
        expect((body.map as Record<string, unknown>).center).toBe('JJ00');
        // Only restore + map are sent; untouched blocks are omitted (left alone by the daemon).
        expect(body.logging_station).toBeUndefined();
        expect(body.station).toBeUndefined();
    });

    it('a band-colour key reorder is NOT dirty (canonical compare)', async () => {
        mockDaemon(configBody({ map: { band_colors: { '20m': '#111111', '40m': '#222222' } } }));
        await generalState.load();
        // Rebuild the map in a different key order, same contents.
        generalState.form.bandColors = { '40m': '#222222', '20m': '#111111' };
        expect(generalState.dirty).toBe(false);
    });

    it('loadBuildInfo populates version/go/schema from /v1/version', async () => {
        generalState.buildInfo = null;
        mockDaemon(configBody(), {
            daemon: '2.0-x',
            go: 'go1.24.0',
            schema: { version: 6, dirty: false },
        });
        await generalState.loadBuildInfo();
        expect(generalState.buildInfo).toEqual({
            daemon: '2.0-x',
            go: 'go1.24.0',
            // env defaults to release when the daemon omits it (W-0004 AC2 fail-safe).
            env: 'release',
            schema: { version: 6, dirty: false },
        });
    });

    it('an edit made WHILE the PUT is in flight is preserved, not overwritten by the response', async () => {
        // Hold the PUT response until an in-flight edit has been made. The daemon
        // echoes exactly what was sent (no normalisation), so a save must adopt only
        // the BASELINE from the echo and keep the live form — otherwise the newer
        // edit is silently lost and the form marked pristine (review 16cb3ea3 P1).
        let releasePut!: () => void;
        const gate = new Promise<void>((r) => (releasePut = r));
        const ok = (b: string) =>
            new Response(b, { status: 200, headers: { 'Content-Type': 'application/json' } });
        const spy = vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            if (init?.method === 'PUT') return gate.then(() => ok((init.body as string) ?? '{}'));
            return Promise.resolve(ok(JSON.stringify(configBody())));
        });
        vi.stubGlobal('fetch', spy);

        await generalState.load();
        generalState.form.restoreRigOnModeSwitch = false; // dirty ⇒ save proceeds
        const saving = generalState.save();
        // Operator toggles a DIFFERENT field after clicking Save, before the response.
        generalState.setBandColor('80m', '#123456', '#dc2626');
        releasePut();
        await saving;

        expect(generalState.form.bandColors['80m']).toBe('#123456'); // the edit survived
        expect(generalState.dirty).toBe(true); // still unsaved work
    });

    it('a timed-out save re-reads, keeps the operator edit, and does NOT revert a concurrent change to another band', async () => {
        // GET #1 loads; the PUT times out; GET #2 (the reconcile re-read) shows a
        // SECOND writer has added 40m and moved the opaque map centre, while the
        // operator's own 20m edit is not (yet) stored.
        let gets = 0;
        const spy = vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            if (init?.method === 'PUT') {
                const e = new Error('timed out');
                e.name = 'TimeoutError';
                return Promise.reject(e);
            }
            gets += 1;
            const body =
                gets === 1
                    ? configBody({ map: { band_colors: { '20m': '#111111' }, center: 'JJ00' } })
                    : configBody({
                          map: {
                              band_colors: { '20m': '#111111', '40m': '#0000ff' },
                              center: 'KK11',
                          },
                      });
            return Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        });
        vi.stubGlobal('fetch', spy);

        await generalState.load();
        generalState.setBandColor('20m', '#222222', '#22c55e'); // operator changes 20m
        await generalState.save(); // PUT times out ⇒ reconcile

        expect(generalState.form.bandColors['20m']).toBe('#222222'); // operator edit kept
        expect(generalState.form.bandColors['40m']).toBe('#0000ff'); // concurrent add adopted
        expect(generalState.dirty).toBe(true); // 20m still differs from stored

        // The resend carries the FRESH map centre (KK11) and BOTH bands — the
        // whole-block PUT reverts neither the concurrent 40m nor the moved centre.
        const spy2 = mockDaemon(configBody());
        await generalState.save();
        const { body } = putCall(spy2);
        const map = body.map as Record<string, unknown>;
        expect(map.center).toBe('KK11');
        expect(map.band_colors).toEqual({ '20m': '#222222', '40m': '#0000ff' });
    });

    it('a timed-out save whose re-read also fails keeps the typed form', async () => {
        let gets = 0;
        const spy = vi.fn((url: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
            if (init?.method === 'PUT') {
                const e = new Error('timed out');
                e.name = 'TimeoutError';
                return Promise.reject(e);
            }
            gets += 1;
            if (gets >= 2) return Promise.reject(new Error('network down')); // reconcile GET fails
            return Promise.resolve(
                new Response(JSON.stringify(configBody()), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        });
        vi.stubGlobal('fetch', spy);

        await generalState.load();
        generalState.setBandColor('30m', '#654321', '#f97316');
        await generalState.save(); // PUT times out, re-read fails

        expect(generalState.form.bandColors['30m']).toBe('#654321'); // nothing discarded
        expect(generalState.dirty).toBe(true);
    });
});
