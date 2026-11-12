// W-0027 C1d part 2: the shell's connection logic without Electron (codex-068). A fake window
// records every page loaded; its session's probe always fails, as with no listener or a
// mismatched one. Synthetic installs in temporary directories only.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { after, test } from 'node:test';
import { connectWhenTrusted, parseArgs } from '../src/daemon.mjs';
import { oncePin } from '../src/pin.mjs';
import { makeIdentity } from './identity.mjs';

const made = [];
after(() => {
    for (const d of made) fs.rmSync(d, { recursive: true, force: true });
});

function install() {
    const base = fs.mkdtempSync(path.join(os.tmpdir(), 'sm-c1d-daemon-'));
    made.push(base);
    const wd = path.join(base, 'sm');
    fs.mkdirSync(wd, { mode: 0o700 });
    fs.writeFileSync(path.join(wd, 'config.json'), JSON.stringify({ server: { app_port: 9443 } }), {
        mode: 0o600,
    });
    const idDir = path.join(wd, 'desktop-identity');
    const id = makeIdentity('daemon-test');
    const publish = (mode = 0o700) => {
        fs.mkdirSync(idDir, { mode });
        fs.chmodSync(idDir, mode);
        fs.writeFileSync(path.join(idDir, 'key.pem'), id.key, { mode: 0o600 });
        fs.writeFileSync(path.join(idDir, 'cert.pem'), id.cert, { mode: 0o644 });
    };
    return { env: { SM_WORKING_DIR: wd, HOME: '/h' }, idDir, publish };
}

/** A fake window. `step()` runs once per discovery round (before each sleep), so a test can
 *  change the install between rounds; the window closes after `rounds` probes. */
function fakeWindow({ rounds = 3 } = {}) {
    const loaded = [];
    let url = '';
    let probes = 0;
    const win = {
        isDestroyed: () => probes >= rounds,
        loadURL: (u) => {
            loaded.push(u);
            url = u;
            return Promise.resolve();
        },
        webContents: {
            getURL: () => url,
            on: () => {},
            session: {
                fetch: () => {
                    probes += 1;
                    return Promise.reject(new Error('refused'));
                },
            },
        },
    };
    return { win, loaded, page: () => decodeURIComponent(url) };
}

const lastPage = (loaded) => decodeURIComponent(loaded.at(-1) ?? '');

for (const [name, before] of [
    ['no identity -> valid, listener down', null],
    ['refused (0755) -> valid, listener down or mismatched', 0o755],
]) {
    test(`${name}: the obsolete local explanation is replaced once the pin is fixed`, async () => {
        const inst = install();
        if (before !== null) inst.publish(before);
        const { win, loaded } = fakeWindow({ rounds: 2 });
        const pin = oncePin();
        const target = { origin: null };
        let rounds = 0;
        const done = connectWhenTrusted({
            win,
            pin,
            target,
            env: inst.env,
            retryMs: 20,
            onDetail: () => {
                rounds += 1;
                if (rounds === 2) {
                    if (before === null) inst.publish();
                    else fs.chmodSync(inst.idDir, 0o700);
                }
            },
        });
        await done;
        assert.ok(pin.get(), 'the pin was never fixed');
        assert.ok(
            loaded.some((u) => decodeURIComponent(u).includes('data-sm-reason')),
            'the local explanation was never shown: the test proves nothing'
        );
        const last = lastPage(loaded);
        assert.ok(
            !last.includes('data-sm-reason'),
            `the obsolete explanation is still shown: ${last}`
        );
        assert.ok(last.includes('data-sm-waiting'), 'not the waiting page');
        assert.ok(last.includes('https://127.0.0.1:9443'), 'the pinned origin is not shown');
    });
}

test('parseArgs keeps both valid forms', () => {
    assert.deepEqual(parseArgs(['electron', '.', '--sm-config', '/a/c.json', '--sm-data-dir=/d']), {
        config: '/a/c.json',
        dataDir: '/d',
    });
    assert.deepEqual(parseArgs(['--sm-config=/x=y.json']), { config: '/x=y.json' });
    assert.deepEqual(parseArgs(['--unrelated']), {});
});

test('parseArgs refuses a named option without a value (codex-068)', () => {
    for (const argv of [
        ['--sm-config'],
        ['--sm-config='],
        ['--sm-config', '--sm-data-dir', '/d'],
        ['--sm-data-dir'],
        ['--sm-data-dir='],
        ['--sm-data-dir', '--sm-config=/c.json'],
    ]) {
        const r = parseArgs(argv);
        assert.ok(r.error, `accepted ${JSON.stringify(argv)}: ${JSON.stringify(r)}`);
        assert.match(r.error, /--sm-(config|data-dir) needs a value/);
    }
});

test('a malformed option is shown locally, with no discovery and no network', async () => {
    const inst = install();
    inst.publish();
    const { win, loaded } = fakeWindow({ rounds: 1 });
    let probed = false;
    win.webContents.session.fetch = () => {
        probed = true;
        return Promise.reject(new Error('x'));
    };
    const pin = oncePin();
    let closed = false;
    win.isDestroyed = () => closed;
    const done = connectWhenTrusted({
        win,
        pin,
        target: { origin: null },
        args: parseArgs(['--sm-config']),
        env: inst.env, // a valid install exists: it must NOT be selected
        retryMs: 20,
    });
    await new Promise((r) => setTimeout(r, 150));
    closed = true;
    await done;
    assert.equal(pin.get(), null, 'a pin was fixed from the default install');
    assert.equal(probed, false, 'the network was used');
    assert.match(lastPage(loaded), /--sm-config needs a value/);
});

// W-0027 D1: the start-up trace from discovery: one line per state change, then the pin.
test('discovery traces each state change once, then the pin; no config contents', async () => {
    const inst = install();
    fs.writeFileSync(
        path.join(inst.env.SM_WORKING_DIR, 'config.json'),
        JSON.stringify({ server: { app_port: 9443 }, secret_marker: 'TOKEN-abc123' }),
        { mode: 0o600 }
    );
    const { win } = fakeWindow({ rounds: 1 });
    const lines = [];
    let rounds = 0;
    await connectWhenTrusted({
        win,
        pin: oncePin(),
        target: { origin: null },
        env: inst.env,
        retryMs: 20,
        trace: (l) => lines.push(l),
        onDetail: () => {
            rounds += 1;
            if (rounds === 3) inst.publish(0o755); // waiting -> refused
            if (rounds === 5) fs.chmodSync(inst.idDir, 0o700); // refused -> valid
        },
    });
    assert.deepEqual(
        lines.map((l) => l.split(':')[0]),
        ['waiting', 'refused', 'pinned https'],
        lines.join(' | ')
    );
    assert.equal(lines[0], `waiting: smd has not created its identity yet ${inst.idDir}`);
    assert.ok(lines[1].includes(inst.idDir) && lines[1].includes('0700'), lines[1]);
    assert.equal(lines[2], 'pinned https://127.0.0.1:9443');
    assert.ok(!lines.join('\n').includes('TOKEN-abc123'), 'config contents reached the trace');
});

test('a malformed option is traced as refused', async () => {
    const { win } = fakeWindow({ rounds: 1 });
    let closed = false;
    win.isDestroyed = () => closed;
    const lines = [];
    const done = connectWhenTrusted({
        win,
        pin: oncePin(),
        target: { origin: null },
        args: parseArgs(['--sm-config']),
        retryMs: 20,
        trace: (l) => lines.push(l),
    });
    await new Promise((r) => setTimeout(r, 80));
    closed = true;
    await done;
    assert.deepEqual(lines, [
        'refused: --sm-config needs a value: --sm-config <path> or --sm-config=<path>',
    ]);
});
