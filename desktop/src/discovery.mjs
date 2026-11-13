// Finding and validating smd's desktop identity (ADR 0103 points 1 and 3, with its 2026-11-11
// data_dir note; W-0027 C1d), and the operating credential beside it (ADR 0103 point 7; W-0027
// C2b). The credential's value is handed on inside an object that never prints it, and no
// refusal quotes the file.
//
// Read-only, in the main process, Linux-scoped. config.json is found by smd's own default rule
// and read for two fields only, data_dir and server.app_port; nothing else from it is kept,
// returned or logged, and no parse error is echoed (V8's messages quote the text). The data root
// is resolved once and only the resolved path is used afterwards. The shell validates the path
// and the certificate and never opens key.pem: the TLS handshake proves that the peer holds it.
// Every file opened goes through the boundary below, so tests can prove which files were read.
import fs from 'node:fs';
import path from 'node:path';
import { X509Certificate } from 'node:crypto';

export const DEFAULT_APP_PORT = 8443;
export const IDENTITY_DIR = 'desktop-identity';
const CERT = 'cert.pem';
export const CREDENTIAL_DIR = 'desktop-credential';
const CREDENTIAL_FILE = 'credential';
const CREDENTIAL_CHARS = 43; // 32 bytes in unpadded base64url, the whole file (smd's desktopid)

/** smd's default config.json location (cmd/smd/main.go defaultConfigPath): an explicit path, else
 *  SM_WORKING_DIR, else the XDG data directory. Never the executable's directory. Returns
 *  { ok: true, path } or { ok: false, reason }. Every source must give an absolute path: a relative
 *  one would be read against the shell's working directory, which a separately started daemon need
 *  not share (codex-066), so it is refused and --sm-config is asked for instead. */
export function configPath({ explicit, env = process.env } = {}) {
    const absolute = (p, what) =>
        path.isAbsolute(p)
            ? { ok: true, path: p }
            : {
                  ok: false,
                  reason: `${what} is not an absolute path; give --sm-config <absolute path>`,
              };
    if (explicit !== undefined && explicit !== null && explicit !== '') {
        return absolute(explicit, '--sm-config');
    }
    if (env.SM_WORKING_DIR) {
        const w = absolute(env.SM_WORKING_DIR, 'SM_WORKING_DIR');
        return w.ok ? { ok: true, path: path.join(w.path, 'config.json') } : w;
    }
    let base;
    if (env.XDG_DATA_HOME) {
        const x = absolute(env.XDG_DATA_HOME, 'XDG_DATA_HOME');
        if (!x.ok) return x;
        base = x.path;
    } else {
        if (!env.HOME)
            return { ok: false, reason: 'HOME is not set; give --sm-config <absolute path>' };
        const h = absolute(env.HOME, 'HOME');
        if (!h.ok) return h;
        base = path.join(h.path, '.local', 'share');
    }
    return { ok: true, path: path.join(base, 'station-manager', 'config.json') };
}

/** The file-system boundary. `opened` records every file the shell opens. */
export function recordingFs({ onLstat, onData } = {}) {
    const opened = [];
    const flags = fs.constants.O_RDONLY | fs.constants.O_NONBLOCK; // a FIFO never blocks the open
    return {
        opened,
        fs: {
            realpath: (p) => fs.realpathSync(p),
            lstat: (p) => {
                onLstat?.(p);
                return fs.lstatSync(p);
            },
            /** Open p (without following a final symlink unless `follow`), fstat it, and read
             *  it only when it is a regular file that `accept(st)` approves. */
            read: (p, { follow = false, accept = () => true } = {}) => {
                opened.push(p);
                const fd = fs.openSync(p, follow ? flags : flags | fs.constants.O_NOFOLLOW);
                try {
                    const st = fs.fstatSync(fd);
                    if (!st.isFile() || !accept(st)) return { st, data: null };
                    onData?.(p);
                    return { st, data: fs.readFileSync(fd) };
                } finally {
                    fs.closeSync(fd);
                }
            },
        },
    };
}

const waiting = (p, reason) => ({ ok: false, state: 'waiting', path: p, reason });
const refused = (p, reason) => ({ ok: false, state: 'refused', path: p, reason });
/** Marks a result as about the operating credential, so the waiting page can say so. */
const aboutCredential = (r) => (r.ok ? r : { ...r, subject: 'credential' });
const mode = (st) => (st.mode & 0o7777).toString(8).padStart(4, '0');

/**
 * Find smd's identity and operating credential. Returns { ok: true, configFile, dataRoot, port,
 * origin, certPath, fingerprint, credentialPath, credential } or { ok: false, state: 'waiting' |
 * 'refused', path, reason }. "waiting" is a state that smd's start resolves by itself; "refused"
 * needs the operator. Both must be valid before anything is returned: the shell never pins a
 * daemon it could not then present itself to. `credential.reveal()` gives the value.
 */
export function discover(
    { configFile, explicitDataDir, uid = process.geteuid(), ownerOf = (_p, st) => st.uid },
    fsb = recordingFs().fs
) {
    if (!configFile)
        return refused('', 'no config.json location: an explicit path must be absolute');

    const cfg = readConfig(configFile, uid, ownerOf, fsb);
    if (!cfg.ok) return cfg;
    const port = appPort(configFile, cfg.doc);
    if (!port.ok) return port;
    const root = dataRoot(configFile, cfg.doc, explicitDataDir);
    if (!root.ok) return root;

    let resolved;
    try {
        resolved = fsb.realpath(root.dir);
    } catch (e) {
        if (e.code === 'ENOENT') return waiting(root.dir, 'the data directory does not exist yet');
        return refused(root.dir, `the data directory cannot be resolved (${e.code ?? 'error'})`);
    }
    const anc = checkAncestors(resolved, uid, ownerOf, fsb);
    if (!anc.ok) return anc;

    const idDir = path.join(resolved, IDENTITY_DIR);
    const dir = checkIdentityDir(idDir, uid, ownerOf, fsb);
    if (!dir.ok) return dir;
    const certPath = path.join(idDir, CERT);
    const cert = readCertificate(certPath, uid, ownerOf, fsb);
    if (!cert.ok) return cert;

    const credDir = path.join(resolved, CREDENTIAL_DIR);
    const cdir = checkPrivateDir(credDir, uid, ownerOf, fsb, {
        absent: 'smd has not created its operating credential yet (it does at each start)',
    });
    if (!cdir.ok) return aboutCredential(cdir);
    const credentialPath = path.join(credDir, CREDENTIAL_FILE);
    const cred = readCredential(credentialPath, uid, ownerOf, fsb);
    if (!cred.ok) return aboutCredential(cred);

    return {
        ok: true,
        configFile,
        dataRoot: resolved,
        port: port.port,
        origin: new URL(`https://127.0.0.1:${port.port}`).origin,
        certPath,
        fingerprint: cert.fingerprint,
        credentialPath,
        credential: sealed(cred.value),
    };
}

/** A value that travels without being printed: JSON, string conversion and inspection all show
 *  a placeholder. Only reveal() gives it. */
function sealed(value) {
    const shown = '[operating credential]';
    return Object.freeze({
        reveal: () => value,
        toString: () => shown,
        toJSON: () => shown,
        [Symbol.for('nodejs.util.inspect.custom')]: () => shown,
    });
}

function readConfig(configFile, uid, ownerOf, fsb) {
    let r;
    try {
        // Owner and mode are checked on the open descriptor before any byte is read.
        r = fsb.read(configFile, {
            follow: true,
            accept: (st) => ownerOf(configFile, st) === uid && !(st.mode & 0o022),
        });
    } catch (e) {
        if (e.code === 'ENOENT')
            return waiting(configFile, 'no config.json here yet: smd has not run');
        return waiting(configFile, `config.json cannot be read (${e.code ?? 'error'})`);
    }
    if (!r.st.isFile()) return refused(configFile, 'config.json is not a regular file');
    const owner = ownerOf(configFile, r.st);
    if (owner !== uid)
        return refused(configFile, `config.json is owned by uid ${owner}, not ${uid}`);
    if (r.st.mode & 0o022)
        return refused(
            configFile,
            `config.json is writable by group or others (mode ${mode(r.st)})`
        );
    let doc;
    try {
        doc = JSON.parse(r.data.toString('utf8'));
    } catch {
        return waiting(configFile, 'config.json is not valid JSON'); // never the parser's message
    }
    if (doc === null || typeof doc !== 'object' || Array.isArray(doc)) {
        return refused(configFile, 'config.json is not a JSON object');
    }
    return { ok: true, doc };
}

function appPort(configFile, doc) {
    const server = doc.server;
    if (server === undefined) return { ok: true, port: DEFAULT_APP_PORT };
    if (server === null || typeof server !== 'object' || Array.isArray(server)) {
        return refused(configFile, 'server in config.json is not an object');
    }
    const p = server.app_port;
    if (p === undefined || p === 0) return { ok: true, port: DEFAULT_APP_PORT };
    if (!Number.isInteger(p) || p < 1 || p > 65535) {
        return refused(
            configFile,
            'server.app_port in config.json must be an integer from 1 to 65535'
        );
    }
    return { ok: true, port: p };
}

function dataRoot(configFile, doc, explicitDataDir) {
    if (explicitDataDir !== undefined && explicitDataDir !== null && explicitDataDir !== '') {
        if (!path.isAbsolute(explicitDataDir))
            return refused(explicitDataDir, 'the explicit data root must be an absolute path');
        return { ok: true, dir: explicitDataDir };
    }
    const d = doc.data_dir;
    if (d === undefined || d === '') return { ok: true, dir: path.dirname(configFile) };
    if (typeof d !== 'string')
        return refused(configFile, 'data_dir in config.json is not a string');
    if (!path.isAbsolute(d)) {
        return refused(
            configFile,
            "data_dir in config.json is relative, so it depends on the daemon's working directory; give the absolute data root explicitly"
        );
    }
    return { ok: true, dir: d };
}

/** Every directory from / down to the resolved root: owned by the user or root, and not writable
 *  by group or others unless sticky (as smd's checkAncestors). */
function checkAncestors(resolved, uid, ownerOf, fsb) {
    const parts = resolved.split(path.sep).filter(Boolean);
    let p = path.sep;
    for (let i = 0; i <= parts.length; i++) {
        if (i > 0) p = path.join(p, parts[i - 1]);
        let st;
        try {
            st = fsb.lstat(p);
        } catch (e) {
            // A component that vanished since resolution is transient; anything else needs the
            // operator. Only the errno is reported, never the raw error (codex-066).
            if (e.code === 'ENOENT') return waiting(p, 'disappeared while being checked');
            return refused(p, `cannot be inspected (${e.code ?? 'error'})`);
        }
        if (st.isSymbolicLink()) return refused(p, 'is a symlink after resolution'); // never expected
        const owner = ownerOf(p, st);
        if (owner !== uid && owner !== 0)
            return refused(p, `is owned by uid ${owner}, which could replace what is below it`);
        if (st.mode & 0o022 && !(st.mode & 0o1000)) {
            return refused(
                p,
                `is writable by another user (mode ${mode(st)}), who could replace what is below it`
            );
        }
    }
    return { ok: true };
}

function checkIdentityDir(idDir, uid, ownerOf, fsb) {
    return checkPrivateDir(idDir, uid, ownerOf, fsb, {
        absent: 'smd has not created its identity yet',
    });
}

/** A directory smd publishes as one unit: the user's own, mode 0700, not a symlink. Absent, it
 *  is something smd's start resolves, so the shell waits. */
function checkPrivateDir(dir, uid, ownerOf, fsb, { absent }) {
    let st;
    try {
        st = fsb.lstat(dir);
    } catch (e) {
        if (e.code === 'ENOENT') return waiting(dir, absent);
        return refused(dir, `cannot be inspected (${e.code ?? 'error'})`);
    }
    if (st.isSymbolicLink()) return refused(dir, 'is a symlink');
    if (!st.isDirectory()) return refused(dir, 'is not a directory');
    const owner = ownerOf(dir, st);
    if (owner !== uid) return refused(dir, `is owned by uid ${owner}, not ${uid}`);
    if ((st.mode & 0o7777) !== 0o700) return refused(dir, `must be mode 0700 (is ${mode(st)})`);
    return { ok: true };
}

/** The credential file: the user's own, mode exactly 0600, a regular file opened without
 *  following a symlink, holding exactly the canonical text of 32 bytes. Owner and mode are
 *  checked on the open descriptor before any byte is read, and no reason quotes the content. */
function readCredential(file, uid, ownerOf, fsb) {
    let r;
    try {
        r = fsb.read(file, {
            accept: (st) => ownerOf(file, st) === uid && (st.mode & 0o7777) === 0o600,
        });
    } catch (e) {
        if (e.code === 'ENOENT') return refused(file, 'is missing from a published credential');
        if (e.code === 'ELOOP') return refused(file, 'is a symlink');
        return refused(file, `cannot be read (${e.code ?? 'error'})`);
    }
    if (!r.st.isFile()) return refused(file, 'is not a regular file');
    const owner = ownerOf(file, r.st);
    if (owner !== uid) return refused(file, `is owned by uid ${owner}, not ${uid}`);
    if ((r.st.mode & 0o7777) !== 0o600)
        return refused(file, `must be mode 0600 (is ${mode(r.st)})`);
    const text = r.data.toString('latin1');
    // One secret has one text. The length is checked on the file's own characters, and
    // decoding then re-encoding must give the same text back: that refuses anything outside the
    // base64url alphabet (the decoder skips or translates it) and a final character whose
    // unused bits are set.
    const canonical =
        text.length === CREDENTIAL_CHARS &&
        Buffer.from(text, 'base64url').toString('base64url') === text;
    if (!canonical)
        return refused(
            file,
            `is not a valid credential: it must hold exactly ${CREDENTIAL_CHARS} base64url characters`
        );
    return { ok: true, value: text };
}

function readCertificate(certPath, uid, ownerOf, fsb) {
    let r;
    try {
        r = fsb.read(certPath, {
            accept: (st) => ownerOf(certPath, st) === uid && !(st.mode & 0o022),
        });
    } catch (e) {
        if (e.code === 'ENOENT') return refused(certPath, 'is missing from a published identity');
        if (e.code === 'ELOOP') return refused(certPath, 'is a symlink');
        return refused(certPath, `cannot be read (${e.code ?? 'error'})`);
    }
    if (!r.st.isFile()) return refused(certPath, 'is not a regular file');
    const owner = ownerOf(certPath, r.st);
    if (owner !== uid) return refused(certPath, `is owned by uid ${owner}, not ${uid}`);
    if (r.st.mode & 0o022)
        return refused(certPath, `is writable by group or others (mode ${mode(r.st)})`);
    try {
        return { ok: true, fingerprint: new X509Certificate(r.data).fingerprint256 };
    } catch {
        return refused(certPath, 'is not a valid certificate');
    }
}
