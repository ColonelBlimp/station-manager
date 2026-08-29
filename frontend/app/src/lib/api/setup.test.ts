import { afterEach, describe, expect, it, vi } from 'vitest';
import { completeSetup } from './setup';
import { toasts } from '../ui/toasts.svelte';

afterEach(() => {
    vi.restoreAllMocks();
});

function mockJSON(status: number, body: unknown) {
    const spy = vi.fn((_url: RequestInfo | URL, _init?: RequestInit): Promise<Response> =>
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

// A first-run setup PUT can succeed (setup_complete flips) while the daemon could
// not confirm the write survives a crash. Setup is APPLIED, not failed — so
// completeSetup must still resolve `ok` AND surface the same durability caveat.
// setup_complete matters especially across a reboot, so an unconfirmed write must
// not be dropped silently (PT-6). There is no success toast here to suppress.
describe('completeSetup durability caveat', () => {
    it('surfaces the caveat but still completes when the write is durability-unconfirmed', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        mockJSON(200, { setup_complete: true, durability: 'unconfirmed' });
        const out = await completeSetup('7Q5MLV');
        expect(out.kind, 'setup is applied, not failed').toBe('ok');
        expect(warn, 'the durability caveat is shown').toHaveBeenCalledOnce();
        expect(String(warn.mock.calls[0][0])).toMatch(/survive a crash/i);
    });

    it('shows no caveat on an ordinary durable setup write', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        mockJSON(200, { setup_complete: true });
        const out = await completeSetup('7Q5MLV');
        expect(out.kind).toBe('ok');
        expect(warn, 'no caveat on a durable write').not.toHaveBeenCalled();
    });
});
