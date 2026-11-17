// What the desktop RPM build (scripts/desktop-rpm.sh) relies on in the pinned downloader,
// @electron/get, when it is given a cache that outlives the build: an archive already in the
// cache is checked against the pinned SHA-256 and used without a download; one that does not
// match is downloaded again and replaced; another version is another file. These are the
// library's behaviours, not ours, so they are measured here against the version the lockfile
// pins. Nothing is fetched: the network is a stand-in that counts its calls.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';

import { downloadArtifact } from '@electron/get';

const sha256 = (bytes) => createHash('sha256').update(bytes).digest('hex');
const name = (version) => `electron-v${version}-linux-x64.zip`;

const PINNED = Buffer.from('the pinned archive');
const OTHER = Buffer.from('the next version, pinned too');
const checksums = { [name('44.9.3')]: sha256(PINNED), [name('44.9.4')]: sha256(OTHER) };

// build is one run of the downloader as install.js makes it, over the cache at `cacheRoot`.
// `served` is what the stand-in network hands out; `calls` collects the URLs it was asked for.
function build(cacheRoot, calls, { version = '44.9.3', served = PINNED } = {}) {
    return downloadArtifact({
        version,
        artifactName: 'electron',
        platform: 'linux',
        arch: 'x64',
        cacheRoot,
        checksums,
        downloader: {
            download: async (url, to) => {
                calls.push(url);
                writeFileSync(to, served);
            },
        },
    });
}

function cache(t) {
    const dir = mkdtempSync(join(tmpdir(), 'sm-runtime-cache-test-'));
    t.after(() => rmSync(dir, { recursive: true, force: true })); // exactly the directory this test made
    return dir;
}

test('an archive in the cache with the pinned SHA-256 is used without a download', async (t) => {
    const root = cache(t);
    const calls = [];
    const first = await build(root, calls);
    assert.equal(calls.length, 1, 'an empty cache did not download');
    assert.ok(first.startsWith(root), 'the downloaded archive was not kept in the cache');

    const second = await build(root, calls);
    assert.equal(calls.length, 1, 'a second build downloaded the archive again');
    assert.equal(second, first);
    assert.deepEqual(readFileSync(second), PINNED);
});

test('a cached archive that is not the pinned one is downloaded again and replaced', async (t) => {
    const root = cache(t);
    const calls = [];
    const kept = await build(root, calls);
    writeFileSync(kept, 'changed after it was cached');

    const again = await build(root, calls);
    assert.equal(calls.length, 2, 'an archive that does not match the pin was used from the cache');
    assert.equal(again, kept);
    assert.deepEqual(
        readFileSync(again),
        PINNED,
        'the cache still holds the archive that did not match'
    );
});

test('a download that is not the pinned archive is refused and not kept', async (t) => {
    const root = cache(t);
    const calls = [];
    await assert.rejects(build(root, calls, { served: Buffer.from('not what was pinned') }));
    assert.equal(calls.length, 1);

    // Nothing was left behind for a later build to find: it downloads, and this time is served the pin.
    await build(root, calls);
    assert.equal(calls.length, 2, 'a refused download was kept in the cache');
});

test('another version is another archive: it is downloaded, and the first stays', async (t) => {
    const root = cache(t);
    const calls = [];
    const old = await build(root, calls);
    const next = await build(root, calls, { version: '44.9.4', served: OTHER });
    assert.equal(calls.length, 2, 'a new pinned version was not downloaded');
    assert.notEqual(next, old);
    assert.deepEqual(readFileSync(next), OTHER);
    assert.deepEqual(readFileSync(old), PINNED);

    await build(root, calls);
    await build(root, calls, { version: '44.9.4', served: OTHER });
    assert.equal(calls.length, 2, 'with both cached, one was downloaded again');
});
