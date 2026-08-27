// Build identity wire (W-0004 AC1/AC2): /v1/version carries `env: "dev"|"release"`,
// which the shell needs to mark a development daemon. fetchBuildInfo must surface it —
// and, per AC2 (a release daemon is never falsely labelled DEV), treat anything that
// is not exactly "dev" as release, so a missing or malformed env can only ever
// under-claim DEV, never fabricate it. Availability stays gated on daemon+go: a
// version string is never invented (AC3), but a valid version is never withheld just
// because env is odd.

import { describe, it, expect, afterEach, vi } from 'vitest';
import { fetchBuildInfo } from './general';

function stubVersion(body: unknown, status = 200): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

afterEach(() => vi.restoreAllMocks());

describe('fetchBuildInfo env', () => {
    it('surfaces env "dev"', async () => {
        stubVersion({ daemon: 'v2.1.0-3-gabc123', go: 'go1.24.0', env: 'dev' });
        const r = await fetchBuildInfo();
        expect(r.kind).toBe('ok');
        if (r.kind === 'ok') expect(r.info.env).toBe('dev');
    });

    it('surfaces env "release"', async () => {
        stubVersion({ daemon: 'v2.1.0', go: 'go1.24.0', env: 'release' });
        const r = await fetchBuildInfo();
        expect(r.kind).toBe('ok');
        if (r.kind === 'ok') expect(r.info.env).toBe('release');
    });

    it('never fabricates DEV: absent or unknown env resolves to release (AC2)', async () => {
        stubVersion({ daemon: 'v2.1.0', go: 'go1.24.0' }); // env omitted
        const missing = await fetchBuildInfo();
        expect(missing.kind).toBe('ok');
        if (missing.kind === 'ok') expect(missing.info.env).toBe('release');

        stubVersion({ daemon: 'v2.1.0', go: 'go1.24.0', env: 'staging' }); // unexpected
        const odd = await fetchBuildInfo();
        expect(odd.kind).toBe('ok');
        if (odd.kind === 'ok') expect(odd.info.env).toBe('release');
    });

    it('still reports unavailable when the version string itself is absent (AC3)', async () => {
        stubVersion({ go: 'go1.24.0', env: 'dev' }); // no daemon
        const r = await fetchBuildInfo();
        expect(r.kind).toBe('error');
    });
});
