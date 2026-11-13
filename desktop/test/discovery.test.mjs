// W-0027 C1d: finding and validating smd's desktop identity (ADR 0103 points 1 and 3, with the
// 2026-11-11 data_dir note). Synthetic installs in temporary directories only; never the
// station's working directory, and never a network request.
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { randomBytes, X509Certificate } from 'node:crypto';
import { after, test } from 'node:test';
import { configPath, discover, recordingFs } from '../src/discovery.mjs';
import { makeIdentity } from './identity.mjs';

// Every temporary directory a test makes, removed at the end (synthetic identities only).
const made = [];
const tmp = (prefix) => {
    const d = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
    made.push(d);
    return d;
};
after(() => {
    for (const d of made) {
        // Restore any mode a test removed so the tree can be deleted.
        fs.chmodSync(d, 0o700);
        fs.rmSync(d, { recursive: true, force: true });
    }
});

/** A synthetic operating credential: 43 base64url characters for 32 random bytes, as smd writes. */
const makeCredential = () => randomBytes(32).toString('base64url');

/** A synthetic install: <base>/sm/config.json, <dataRoot>/desktop-identity/{cert,key}.pem and
 *  <dataRoot>/desktop-credential/credential, laid out and permissioned as smd creates them. */
function install({ config = {}, dataRoot = null, identity = true, credential = true } = {}) {
    const base = tmp('sm-c1d-');
    const dir = path.join(base, 'sm');
    fs.mkdirSync(dir, { mode: 0o700 });
    const configFile = path.join(dir, 'config.json');
    fs.writeFileSync(configFile, JSON.stringify(config), { mode: 0o600 });
    const root = dataRoot ?? dir;
    fs.mkdirSync(root, { recursive: true, mode: 0o700 });
    let cert = null;
    if (identity) {
        const id = makeIdentity('c1d');
        const idDir = path.join(root, 'desktop-identity');
        fs.mkdirSync(idDir, { mode: 0o700 });
        fs.writeFileSync(path.join(idDir, 'key.pem'), id.key, { mode: 0o600 });
        fs.writeFileSync(path.join(idDir, 'cert.pem'), id.cert, { mode: 0o644 });
        cert = id.cert;
    }
    const credDir = path.join(root, 'desktop-credential');
    const credFile = path.join(credDir, 'credential');
    let cred = null;
    if (credential) {
        cred = makeCredential();
        fs.mkdirSync(credDir, { mode: 0o700 });
        fs.writeFileSync(credFile, cred, { mode: 0o600 });
    }
    return {
        base,
        dir,
        configFile,
        root,
        cert,
        idDir: path.join(root, 'desktop-identity'),
        cred,
        credDir,
        credFile,
    };
}

const fp = (pem) => new X509Certificate(pem).fingerprint256;

function run(inst, opts = {}) {
    const rec = recordingFs();
    const got = discover({ configFile: inst.configFile, ...opts }, rec.fs);
    return { got, opened: rec.opened };
}

function assertRefused(got, state, pathPart, reasonPart) {
    assert.equal(got.ok, false, `expected ${state}, got ok: ${JSON.stringify(got)}`);
    assert.equal(got.state, state, JSON.stringify(got));
    if (pathPart)
        assert.ok(got.path.includes(pathPart), `path ${got.path} should name ${pathPart}`);
    if (reasonPart) assert.match(got.reason, reasonPart);
}

test('config location follows smd: explicit, then SM_WORKING_DIR, then XDG', () => {
    const at = (o) => {
        const r = configPath(o);
        assert.equal(r.ok, true, JSON.stringify(r));
        return r.path;
    };
    assert.equal(at({ explicit: '/x/c.json', env: { SM_WORKING_DIR: 'relative' } }), '/x/c.json');
    assert.equal(at({ env: { SM_WORKING_DIR: '/w', HOME: '/h' } }), '/w/config.json');
    assert.equal(
        at({ env: { XDG_DATA_HOME: '/d', HOME: '/h' } }),
        '/d/station-manager/config.json'
    );
    assert.equal(at({ env: { HOME: '/h' } }), '/h/.local/share/station-manager/config.json');
});

test('an ambiguous config location is refused, never resolved against the shell cwd (codex-066)', () => {
    for (const o of [
        { explicit: 'relative.json', env: { HOME: '/h' } },
        { env: { SM_WORKING_DIR: 'relative-work', HOME: '/h' } },
        { env: { XDG_DATA_HOME: 'relative-xdg', HOME: '/h' } },
        { env: { HOME: 'relative-home' } },
        { env: {} },
    ]) {
        const r = configPath(o);
        assert.equal(r.ok, false, `accepted ${JSON.stringify(o)}: ${JSON.stringify(r)}`);
        assert.match(r.reason, /absolute|--sm-config/);
    }
});

test('the default layout: identity beside config.json, port 8443', () => {
    const inst = install();
    const { got, opened } = run(inst);
    assert.equal(got.ok, true, JSON.stringify(got));
    assert.equal(got.port, 8443);
    assert.equal(got.origin, 'https://127.0.0.1:8443');
    assert.equal(got.fingerprint, fp(inst.cert));
    assert.ok(!opened.some((p) => p.endsWith('key.pem')), 'the shell opened key.pem');
});

test('an explicit app port, and 443 without its default port', () => {
    assert.equal(
        run(install({ config: { server: { app_port: 9443 } } })).got.origin,
        'https://127.0.0.1:9443'
    );
    assert.equal(
        run(install({ config: { server: { app_port: 443 } } })).got.origin,
        'https://127.0.0.1'
    );
    assert.equal(run(install({ config: { server: { app_port: 0 } } })).got.port, 8443);
});

test('an invalid app port is refused, never replaced by the default', () => {
    for (const bad of ['8443', -1, 70000, 1.5, null, true]) {
        const { got } = run(install({ config: { server: { app_port: bad } } }));
        assertRefused(got, 'refused', 'config.json', /server\.app_port/);
    }
    assertRefused(
        run(install({ config: { server: 'tcp' } })).got,
        'refused',
        'config.json',
        /server/
    );
});

test('data_dir: absolute elsewhere is used; relative is refused unless an explicit root is given', () => {
    const elsewhere = path.join(tmp('sm-c1d-data-'), 'data');
    const inst = install({ config: { data_dir: elsewhere }, dataRoot: elsewhere });
    const { got } = run(inst);
    assert.equal(got.ok, true, JSON.stringify(got));
    assert.equal(got.fingerprint, fp(inst.cert));

    const rel = install({ config: { data_dir: 'data' } });
    assertRefused(run(rel).got, 'refused', 'config.json', /relative/);
    const withRoot = run(rel, { explicitDataDir: rel.root });
    assert.equal(withRoot.got.ok, true, JSON.stringify(withRoot.got));
    assertRefused(run(rel, { explicitDataDir: 'data' }).got, 'refused', null, /absolute/);
});

test('missing or malformed config.json waits, and never echoes its contents', () => {
    const inst = install();
    fs.rmSync(inst.configFile);
    assertRefused(run(inst).got, 'waiting', 'config.json', /no config/i);

    // An unquoted value: V8's message quotes the text around it ("password":hunter2-se...), so
    // echoing the parser's message would leak it. A truncated document would not prove this.
    fs.writeFileSync(inst.configFile, '{"smtp":{"password":hunter2-secret}}', { mode: 0o600 });
    const { got } = run(inst);
    assertRefused(got, 'waiting', 'config.json', /not valid JSON/);
    assert.ok(
        !JSON.stringify(got).includes('hunter2'),
        'the config contents leaked into the result'
    );
});

test('no identity yet waits', () => {
    assertRefused(
        run(install({ identity: false })).got,
        'waiting',
        'desktop-identity',
        /not created/
    );
    const elsewhere = path.join(tmp('sm-c1d-data-'), 'data');
    const gone = install({ identity: false, config: { data_dir: elsewhere }, dataRoot: elsewhere });
    fs.rmSync(elsewhere, { recursive: true });
    assertRefused(run(gone).got, 'waiting', elsewhere, /does not exist/);
});

test('local refusals name the path; nothing is pinned', () => {
    const cases = [
        [
            'identity directory 0755',
            (i) => fs.chmodSync(i.idDir, 0o755),
            'desktop-identity',
            /0700/,
        ],
        [
            'cert.pem group-writable',
            (i) => fs.chmodSync(path.join(i.idDir, 'cert.pem'), 0o664),
            'cert.pem',
            /writable/,
        ],
        [
            'cert.pem a symlink',
            (i) => {
                const real = path.join(i.root, 'elsewhere.pem');
                fs.renameSync(path.join(i.idDir, 'cert.pem'), real);
                fs.symlinkSync(real, path.join(i.idDir, 'cert.pem'));
            },
            'cert.pem',
            /symlink|regular/,
        ],
        [
            'identity directory a symlink',
            (i) => {
                const real = path.join(i.root, 'real-identity');
                fs.renameSync(i.idDir, real);
                fs.symlinkSync(real, i.idDir);
            },
            'desktop-identity',
            /symlink/,
        ],
        [
            'cert.pem not a regular file',
            (i) => {
                fs.rmSync(path.join(i.idDir, 'cert.pem'));
                fs.mkdirSync(path.join(i.idDir, 'cert.pem'));
            },
            'cert.pem',
            /regular/,
        ],
        [
            'cert.pem malformed',
            (i) => fs.writeFileSync(path.join(i.idDir, 'cert.pem'), 'not a certificate'),
            'cert.pem',
            /certificate/,
        ],
        [
            'cert.pem missing',
            (i) => fs.rmSync(path.join(i.idDir, 'cert.pem')),
            'cert.pem',
            /missing/,
        ],
        [
            'config.json group-writable',
            (i) => fs.chmodSync(i.configFile, 0o620),
            'config.json',
            /writable/,
        ],
    ];
    for (const [name, spoil, pathPart, reason] of cases) {
        const inst = install();
        spoil(inst);
        const { got, opened } = run(inst);
        assert.equal(got.fingerprint, undefined, `${name}: a fingerprint was produced`);
        assertRefused(got, 'refused', pathPart, reason);
        assert.ok(!opened.some((p) => p.endsWith('key.pem')), `${name}: the shell opened key.pem`);
    }
});

test('foreign owners are refused (owner lookup seam)', () => {
    for (const target of ['desktop-identity', 'cert.pem']) {
        const inst = install();
        const foreign = (p, st) => (p.endsWith(target) ? st.uid + 1 : st.uid);
        const rec = recordingFs();
        const got = discover({ configFile: inst.configFile, ownerOf: foreign }, rec.fs);
        assertRefused(got, 'refused', target, /owned by/);
    }
});

test('an ancestor another user could replace is refused; a sticky one is accepted', () => {
    const open = install({ dataRoot: null });
    const parent = path.dirname(open.dir);
    fs.chmodSync(parent, 0o777);
    assertRefused(run(open).got, 'refused', parent, /writable/);
    fs.chmodSync(parent, 0o1777);
    assert.equal(
        run(open).got.ok,
        true,
        'a sticky world-writable ancestor (like /tmp) must be accepted'
    );
    fs.chmodSync(parent, 0o700);
});

test('a root alias swapped after resolution: the resolved identity is read, the substitute never', () => {
    const genuine = install();
    const substitute = install();
    const aliasParent = tmp('sm-c1d-alias-');
    const alias = path.join(aliasParent, 'root');
    fs.symlinkSync(genuine.root, alias);
    // data_dir names the alias; it is swapped to the substitute after it was resolved, the
    // moment the identity directory is inspected.
    fs.writeFileSync(genuine.configFile, JSON.stringify({ data_dir: alias }), { mode: 0o600 });
    const rec = recordingFs({
        onLstat: (p) => {
            if (p.endsWith('desktop-identity') && fs.readlinkSync(alias) === genuine.root) {
                fs.unlinkSync(alias);
                fs.symlinkSync(substitute.root, alias);
            }
        },
    });
    const got = discover({ configFile: genuine.configFile }, rec.fs);
    assert.equal(
        fs.readlinkSync(alias),
        substitute.root,
        'the swap did not happen: the test proves nothing'
    );
    assert.equal(got.ok, true, JSON.stringify(got));
    assert.equal(got.fingerprint, fp(genuine.cert));
    assert.ok(!rec.opened.some((p) => p.startsWith(substitute.root)), 'the substitute was read');
});

test('an ancestor inspection error is a result, and a later healthy probe succeeds (codex-066)', () => {
    const inst = install();
    const failing = path.dirname(inst.dir);
    for (const code of ['ENOENT', 'EACCES', 'EIO']) {
        let fail = true;
        const rec = recordingFs();
        const base = rec.fs;
        const fsb = {
            ...base,
            lstat: (p) => {
                if (fail && p === failing) {
                    const e = new Error(`synthetic ${code} with secret detail`);
                    e.code = code;
                    throw e;
                }
                return base.lstat(p);
            },
        };
        const got = discover({ configFile: inst.configFile }, fsb);
        assert.equal(got.ok, false, `${code}: ${JSON.stringify(got)}`);
        assert.equal(got.path, failing);
        assert.equal(got.state, code === 'ENOENT' ? 'waiting' : 'refused', code);
        assert.ok(!JSON.stringify(got).includes('secret detail'), `${code}: the raw error leaked`);
        fail = false;
        assert.equal(
            discover({ configFile: inst.configFile }, fsb).ok,
            true,
            `${code}: no recovery`
        );
    }
});

test('a foreign owner of config.json is refused', () => {
    const inst = install();
    const foreign = (p, st) => (p === inst.configFile ? st.uid + 1 : st.uid);
    const got = discover({ configFile: inst.configFile, ownerOf: foreign }, recordingFs().fs);
    assertRefused(got, 'refused', 'config.json', /owned by/);
});

test('config.json is checked before its contents are read', () => {
    const inst = install();
    fs.chmodSync(inst.configFile, 0o620);
    const reads = [];
    const rec = recordingFs({ onData: (p) => reads.push(p) });
    const got = discover({ configFile: inst.configFile }, rec.fs);
    assertRefused(got, 'refused', 'config.json', /writable/);
    assert.deepEqual(reads, [], 'a refused config.json was read');
});

test('a FIFO in place of cert.pem is refused without blocking (child process, deadline)', () => {
    const inst = install();
    const cert = path.join(inst.idDir, 'cert.pem');
    fs.rmSync(cert);
    execFileSync('mkfifo', ['-m', '0644', cert]);
    const probe = `import { discover } from ${JSON.stringify(new URL('../src/discovery.mjs', import.meta.url).href)};
        process.stdout.write(JSON.stringify(discover({ configFile: ${JSON.stringify(inst.configFile)} })));`;
    const r = spawnSync(process.execPath, ['--input-type=module', '-e', probe], {
        timeout: 5000,
        encoding: 'utf8',
    });
    assert.notEqual(r.signal, 'SIGTERM', 'discover blocked on the FIFO (killed at the deadline)');
    const got = JSON.parse(r.stdout);
    assertRefused(got, 'refused', 'cert.pem', /regular/);
});

// --- W-0027 C2b: the operating credential, beside the identity -------------------------

test('the credential is found beside the identity, and is never printed with the result', () => {
    const inst = install();
    const { got, opened } = run(inst);
    assert.equal(got.ok, true, JSON.stringify(got));
    assert.equal(got.credential.reveal(), inst.cred);
    assert.equal(got.credentialPath, inst.credFile);
    for (const text of [JSON.stringify(got), String(got.credential), `${got.credential}`]) {
        assert.ok(!text.includes(inst.cred), 'the result prints the credential');
    }
    assert.equal(opened.filter((p) => p === inst.credFile).length, 1, 'read once');
});

test('no credential yet waits, after a valid identity, and nothing is pinned', () => {
    const { got, opened } = run(install({ credential: false }));
    assertRefused(got, 'waiting', 'desktop-credential', /not created/);
    assert.equal(got.fingerprint, undefined, 'a fingerprint was produced without a credential');
    assert.ok(!opened.some((p) => p.endsWith('key.pem')));
});

test('an unsafe or malformed credential is refused, names the path, and is never quoted', () => {
    const good = 'A'.repeat(43);
    const cases = [
        ['directory 0755', (i) => fs.chmodSync(i.credDir, 0o755), 'desktop-credential', /0700/],
        ['directory 0710', (i) => fs.chmodSync(i.credDir, 0o710), 'desktop-credential', /0700/],
        ['file 0640', (i) => fs.chmodSync(i.credFile, 0o640), 'credential', /0600/],
        ['file 0604', (i) => fs.chmodSync(i.credFile, 0o604), 'credential', /0600/],
        ['file 0700', (i) => fs.chmodSync(i.credFile, 0o700), 'credential', /0600/],
        ['file missing', (i) => fs.rmSync(i.credFile), 'credential', /missing/],
        [
            'file a symlink',
            (i) => {
                const real = path.join(i.root, 'elsewhere');
                fs.renameSync(i.credFile, real);
                fs.symlinkSync(real, i.credFile);
            },
            'credential',
            /symlink/,
        ],
        [
            'directory a symlink',
            (i) => {
                const real = path.join(i.root, 'real-credential');
                fs.renameSync(i.credDir, real);
                fs.symlinkSync(real, i.credDir);
            },
            'desktop-credential',
            /symlink/,
        ],
        [
            'directory is a file',
            (i) => {
                fs.rmSync(i.credDir, { recursive: true });
                fs.writeFileSync(i.credDir, 'x', { mode: 0o600 });
            },
            'desktop-credential',
            /directory/,
        ],
        [
            'file not a regular file',
            (i) => {
                fs.rmSync(i.credFile);
                fs.mkdirSync(i.credFile, { mode: 0o700 });
            },
            'credential',
            /regular/,
        ],
        ['empty', (i) => fs.writeFileSync(i.credFile, ''), 'credential', /43/],
        ['one short', (i) => fs.writeFileSync(i.credFile, good.slice(1)), 'credential', /43/],
        ['one long', (i) => fs.writeFileSync(i.credFile, good + 'A'), 'credential', /43/],
        [
            'a trailing newline',
            (i) => fs.writeFileSync(i.credFile, good + '\n'),
            'credential',
            /43/,
        ],
        ['a space', (i) => fs.writeFileSync(i.credFile, ' ' + good.slice(1)), 'credential', /43/],
        [
            "standard '+'",
            (i) => fs.writeFileSync(i.credFile, '+' + good.slice(1)),
            'credential',
            /43/,
        ],
        [
            "standard '/'",
            (i) => fs.writeFileSync(i.credFile, '/' + good.slice(1)),
            'credential',
            /43/,
        ],
        ['padding', (i) => fs.writeFileSync(i.credFile, good.slice(1) + '='), 'credential', /43/],
        [
            'non-canonical final bits',
            (i) => fs.writeFileSync(i.credFile, good.slice(1) + 'B'),
            'credential',
            /43/,
        ],
        [
            'a line break inside',
            (i) => fs.writeFileSync(i.credFile, good.slice(0, 21) + '\n' + good.slice(0, 21)),
            'credential',
            /43/,
        ],
    ];
    for (const [name, spoil, pathPart, reason] of cases) {
        const inst = install();
        spoil(inst);
        const { got } = run(inst);
        assertRefused(got, 'refused', pathPart, reason);
        assert.equal(got.fingerprint, undefined, `${name}: a fingerprint was produced`);
        assert.equal(got.credential, undefined, `${name}: a credential was produced`);
        const text = JSON.stringify(got);
        assert.ok(!text.includes(inst.cred), `${name}: the refusal quotes the credential`);
        let present = '';
        try {
            present = fs.readFileSync(inst.credFile, 'utf8').trim();
        } catch {
            /* not a readable file in this case */
        }
        if (present.length >= 8)
            assert.ok(!text.includes(present), `${name}: the refusal quotes the file`);
    }
});

test('a credential or its directory owned by another user is refused', () => {
    for (const foreign of ['desktop-credential', 'credential']) {
        const inst = install();
        const rec = recordingFs();
        const got = discover(
            {
                configFile: inst.configFile,
                ownerOf: (p, st) => (path.basename(p) === foreign ? st.uid + 4242 : st.uid),
            },
            rec.fs
        );
        assertRefused(got, 'refused', foreign, /owned by/);
    }
});

test('the credential file is checked on the open descriptor before any byte is read', () => {
    const inst = install();
    fs.chmodSync(inst.credFile, 0o644);
    const read = [];
    const rec = recordingFs({ onData: (p) => read.push(p) });
    const got = discover({ configFile: inst.configFile }, rec.fs);
    assertRefused(got, 'refused', 'credential', /0600/);
    assert.ok(!read.includes(inst.credFile), 'an unsafe credential file was read');
});
