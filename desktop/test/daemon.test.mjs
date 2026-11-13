// W-0027 C1d part 2: the shell's connection logic without Electron (codex-068). A fake window
// records every page loaded; its session's probe always fails, as with no listener or a
// mismatched one. Synthetic installs in temporary directories only.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { after, test } from 'node:test';
import { randomBytes } from 'node:crypto';
import { onceCredential } from '../src/credential.mjs';
import { connectWhenTrusted, parseArgs } from '../src/daemon.mjs';
import { oncePin } from '../src/pin.mjs';
import { makeIdentity } from './identity.mjs';

const made = [];
after(() => {
    for (const d of made) fs.rmSync(d, { recursive: true, force: true });
});

function install({ credential = true } = {}) {
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
    // The operating credential, as smd makes it at every start (W-0027 C2a). Tests of the
    // credential's own states pass `credential: false` and publish it themselves.
    const credDir = path.join(wd, 'desktop-credential');
    const credFile = path.join(credDir, 'credential');
    const value = randomBytes(32).toString('base64url');
    const publishCredential = (fileMode = 0o600) => {
        fs.mkdirSync(credDir, { mode: 0o700 });
        fs.writeFileSync(credFile, value, { mode: fileMode });
        fs.chmodSync(credFile, fileMode);
    };
    if (credential) publishCredential();
    return {
        env: { SM_WORKING_DIR: wd, HOME: '/h' },
        idDir,
        publish,
        credDir,
        credFile,
        value,
        publishCredential,
    };
}

/** A fake window. `step()` runs once per discovery round (before each sleep), so a test can
 *  change the install between rounds; the window closes after `rounds` probes. */
function fakeWindow({ rounds = 3, answer = null } = {}) {
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
                    // `answer(n)` gives the nth probe's response; without it, no listener.
                    const res = answer?.(probes);
                    return res ? Promise.resolve(res) : Promise.reject(new Error('refused'));
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

// --- W-0027 C2b: the operating credential is fixed together with the pin and the origin ---

test('no credential yet: the shell waits and fixes nothing; then all three are fixed together', async () => {
    const inst = install({ credential: false });
    inst.publish();
    const { win, loaded } = fakeWindow({ rounds: 2 });
    const pin = oncePin();
    const cred = onceCredential();
    const target = { origin: null };
    const seen = [];
    let rounds = 0;
    await connectWhenTrusted({
        win,
        pin,
        cred,
        target,
        env: inst.env,
        retryMs: 20,
        trace: () => {},
        onDetail: (r) => {
            rounds += 1;
            // While the credential is missing, nothing at all is fixed.
            seen.push({ state: r.state, pin: pin.get(), cred: cred.get(), origin: target.origin });
            if (rounds === 2) inst.publishCredential();
        },
    });
    assert.ok(seen.length >= 2, 'the shell never waited for the credential');
    for (const s of seen) {
        assert.deepEqual(s, { state: 'waiting', pin: null, cred: null, origin: null });
    }
    assert.ok(
        loaded.some((u) => decodeURIComponent(u).includes('operating credential')),
        'the waiting page never said what it was waiting for'
    );
    assert.ok(pin.get(), 'the pin was never fixed');
    assert.equal(cred.get(), inst.value, 'the credential fixed is not the published one');
    assert.equal(target.origin, 'https://127.0.0.1:9443');
    for (const u of loaded) {
        assert.ok(!decodeURIComponent(u).includes(inst.value), 'a page shows the credential');
    }
});

test('an unsafe credential is refused on the waiting page, by path, and nothing is fixed', async () => {
    const inst = install({ credential: false });
    inst.publish();
    inst.publishCredential(0o644);
    const { win, loaded } = fakeWindow({ rounds: 1 });
    const pin = oncePin();
    const cred = onceCredential();
    const target = { origin: null };
    const lines = [];
    let rounds = 0;
    const done = connectWhenTrusted({
        win,
        pin,
        cred,
        target,
        env: inst.env,
        retryMs: 20,
        trace: (l) => lines.push(l),
        onDetail: () => {
            rounds += 1;
            if (rounds === 3) win.isDestroyed = () => true; // the operator closes the window
        },
    });
    await done;
    assert.equal(pin.get(), null);
    assert.equal(cred.get(), null);
    assert.equal(target.origin, null);
    const last = lastPage(loaded);
    assert.ok(last.includes('data-sm-state="refused"'), last);
    assert.ok(last.includes('0600') && last.includes('desktop-credential'), last);
    assert.ok(last.includes('operating credential'), 'the page does not say what is refused');
    assert.ok(!last.includes('identity'), 'a credential refusal is described as an identity one');
    for (const text of [...loaded.map((u) => decodeURIComponent(u)), ...lines]) {
        assert.ok(!text.includes(inst.value), 'the credential is shown or traced');
    }
});

const json = (status, body) =>
    new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });

test('the daemon refuses the credential: the waiting page says so, and it loads once accepted', async () => {
    const inst = install();
    inst.publish();
    const { win, loaded } = fakeWindow({
        rounds: 4,
        answer: (n) =>
            n < 3
                ? json(403, { code: 'desktop_app_required', message: 'x', op: 'y' })
                : json(200, { daemon: 'fixture' }),
    });
    await connectWhenTrusted({
        win,
        pin: oncePin(),
        cred: onceCredential(),
        target: { origin: null },
        env: inst.env,
        retryMs: 20,
        trace: () => {},
    });
    const pages = loaded.map((u) => decodeURIComponent(u));
    const refusal = pages.filter((p) => p.includes('data-sm-state="unaccepted"'));
    assert.equal(refusal.length, 1, 'the refusal page is shown once, not on every probe');
    assert.ok(refusal[0].includes('smctl restart'), refusal[0]);
    assert.ok(!refusal[0].includes(inst.value));
    assert.equal(loaded.at(-1), 'https://127.0.0.1:9443/', 'the application was not loaded');
});

test('another 403 is not the credential refusal: the shell keeps the ordinary waiting page', async () => {
    const inst = install();
    inst.publish();
    const { win, loaded } = fakeWindow({
        rounds: 2,
        answer: () => json(403, { code: 'cross_origin', message: 'x', op: 'y' }),
    });
    await connectWhenTrusted({
        win,
        pin: oncePin(),
        cred: onceCredential(),
        target: { origin: null },
        env: inst.env,
        retryMs: 20,
        trace: () => {},
    });
    const pages = loaded.map((u) => decodeURIComponent(u));
    assert.ok(
        !pages.some((p) => p.includes('unaccepted')),
        'a foreign 403 was read as the refusal'
    );
    assert.ok(!loaded.includes('https://127.0.0.1:9443/'), 'the application was loaded on a 403');
});
