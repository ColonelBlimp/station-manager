// The fixture harness (W-0027): a stand-in origin on an ephemeral loopback port.
//
// Until C1's authentication is implemented and validated, the shell executes no
// daemon-served UI (ADR 0102, Requirements). The harness is the only origin it loads:
// it never connects to smd, the rig or the network, and its port is chosen by the OS,
// never smd's.
//
// A1 serves plain HTTP. The C1 proof (ADR 0103) serves the same routes over TLS with a
// synthetic identity, and runs a genuine and an impostor harness at the same address in
// turn. Every route answers with the harness's tag, so a test can tell which listener
// served it; requests, WebSocket upgrades and TLS handshakes are counted.
import crypto from 'node:crypto';
import http from 'node:http';
import https from 'node:https';

const page = (tag) => `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Station Manager — fixture</title></head>
<body>
<main data-sm-fixture data-tag="${tag}">
<h1>Station Manager desktop shell</h1>
<p>Fixture page: no daemon, no rig, no network.</p>
</main>
<script>window.__smFixtureScript = ${JSON.stringify(tag)};</script>
</body>
</html>
`;

const WS_GUID = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';

function wsFrame(text) {
    const payload = Buffer.from(text);
    if (payload.length > 125) throw new Error('fixture frame too long');
    return Buffer.concat([Buffer.from([0x81, payload.length]), payload]);
}

/** Start the harness. `stop()` closes the listener and every connection and keeps its
 *  port, so `start()` brings the "daemon" back at the same origin (connection refused
 *  in between). With `tls: { key, cert }` it serves HTTPS and WSS. `port` reuses a
 *  known port (another harness's), so two can take turns at one address. */
export async function startHarness({ up = true, tls = null, tag = 'fixture', port = 0 } = {}) {
    const requests = [];
    const upgrades = [];
    // `requireCredential`, when set, makes the fixture answer as smd does with its
    // operating-client check on (W-0027 C2): anything without exactly that one credential
    // header gets 403 desktop_app_required.
    // `forbid`, when set to a code, answers every request 403 with that code instead.
    // `admission` answers the app's admission as smd does (W-0027 C4a; internal/api
    // app_admission.go): with `required` false, as with the check off, there is nothing to be
    // admitted to; with it true the fixture keeps one current lifetime, a generation and the
    // lifetimes it has replaced, and puts `instance`, its process marker, on what it serves.
    // `holdAdmit`, when set, is awaited before an admit is answered, so a test can place
    // another admission between a read and its admit.
    const state = {
        versionReady: true,
        requireCredential: null,
        forbid: null,
        admission: {
            required: false,
            instance: `fixture-${tag}-1`,
            generation: 0,
            current: null,
            retired: new Set(),
            holdAdmit: null,
            // failReads: answer that many reads with 500. oneOffMarker: put this marker,
            // once, on the next response that is not an admission's, as a late response from
            // an earlier process would carry.
            failReads: 0,
            oneOffMarker: null,
        },
    };
    const handshakes = { secure: 0, failed: 0 };
    const sockets = new Set();
    const marker = (url) => new URL(url, 'http://x').searchParams.get('m') ?? '';
    // Every value of the operating-credential header on a request, in any letter case, so a
    // test can tell none, one and two apart (Node's own view joins repeats).
    const credentials = (req) => {
        const out = [];
        for (let i = 0; i < req.rawHeaders.length; i += 2) {
            if (req.rawHeaders[i].toLowerCase() === 'x-sm-operating-credential')
                out.push(req.rawHeaders[i + 1]);
        }
        return out;
    };
    const admitted = (cred) =>
        state.requireCredential === null ||
        (cred.length === 1 && cred[0] === state.requireCredential);
    // Every value of the app-lifetime header on a request, as for the credential.
    const lifetimes = (req) => {
        const out = [];
        for (let i = 0; i < req.rawHeaders.length; i += 2) {
            if (req.rawHeaders[i].toLowerCase() === 'x-sm-app-lifetime')
                out.push(req.rawHeaders[i + 1]);
        }
        return out;
    };
    /** Replace the current lifetime, as an admission does (also for a test, directly). */
    const replaceLifetime = (lifetime) => {
        const a = state.admission;
        if (a.current) a.retired.add(a.current);
        a.current = lifetime;
        a.generation += 1;
    };
    const json = (res, status, body) => {
        res.writeHead(status, { 'content-type': 'application/json' });
        res.end(JSON.stringify(body));
    };
    const admission = async (req, res, lifetime) => {
        const a = state.admission;
        if (req.method === 'GET') {
            if (a.failReads > 0) {
                a.failReads -= 1;
                json(res, 500, { code: 'fixture_failure' });
                return;
            }
            json(res, 200, {
                required: a.required,
                daemon_instance: a.instance,
                generation: a.required ? a.generation : 0,
                current: a.required && lifetime !== '' && lifetime === a.current,
            });
            return;
        }
        let raw = '';
        for await (const chunk of req) raw += chunk;
        if (a.holdAdmit) await a.holdAdmit();
        let body = null;
        try {
            body = JSON.parse(raw);
        } catch {
            // answered below
        }
        if (!a.required) return json(res, 200, { required: false, daemon_instance: a.instance });
        if (lifetime === '') return json(res, 400, { code: 'app_lifetime_required' });
        if (lifetime === a.current) return json(res, 200, { current: true });
        if (a.retired.has(lifetime)) return json(res, 409, { code: 'app_lifetime_retired' });
        if (body?.daemon_instance !== a.instance || body?.generation !== a.generation)
            return json(res, 409, { code: 'app_admission_stale' });
        replaceLifetime(lifetime);
        return json(res, 200, { current: true, generation: a.generation });
    };

    const handler = (req, res) => {
        const cred = credentials(req);
        const lifetime = lifetimes(req);
        requests.push({ method: req.method, url: req.url, cred, lifetime });
        const path = new URL(req.url, 'http://x').pathname;
        if (!admitted(cred)) {
            res.writeHead(403, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ code: 'desktop_app_required', message: 'x', op: 'fixture' }));
            return;
        }
        if (state.forbid) {
            // A 403 of another kind, for every request: smd's own code for something else.
            res.writeHead(403, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ code: state.forbid, message: 'x', op: 'fixture' }));
            return;
        }
        // As smd with its check on: this process's marker on everything it serves.
        const isAdmission =
            path === '/v1/app/lifetime' && (req.method === 'GET' || req.method === 'POST');
        if (state.admission.required) {
            let instance = state.admission.instance;
            if (!isAdmission && state.admission.oneOffMarker !== null) {
                instance = state.admission.oneOffMarker;
                state.admission.oneOffMarker = null;
            }
            res.setHeader('X-Sm-Daemon-Instance', instance);
            res.setHeader('X-Sm-Admission-Generation', String(state.admission.generation));
        }
        if (isAdmission) {
            void admission(req, res, lifetime.length === 1 ? lifetime[0] : '');
            return;
        }
        if (req.method === 'GET' && path === '/asset.js') {
            res.writeHead(200, { 'content-type': 'text/javascript' });
            res.end(`window.__smAsset = ${JSON.stringify(marker(req.url))};`);
            return;
        }
        if (req.method === 'GET' && path === '/redirect') {
            // A redirect to wherever `to` says: a test's way off the origin.
            res.writeHead(302, { location: new URL(req.url, 'http://x').searchParams.get('to') });
            res.end();
            return;
        }
        if (req.method === 'GET' && (path === '/' || path === '/index.html')) {
            res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
            res.end(page(tag));
            return;
        }
        if (req.method === 'GET' && path === '/v1/version') {
            // The shell's availability probe (ADR 0103 point 6), answered like smd's endpoint.
            // While `versionReady` is false it answers 503 although / answers 200, so a test
            // can tell a /v1/version probe from a root probe (codex-070).
            if (!state.versionReady) {
                res.writeHead(503, { 'content-type': 'text/plain' });
                res.end('not ready');
                return;
            }
            res.writeHead(200, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ daemon: `fixture-${tag}` }));
            return;
        }
        if (req.method === 'GET' && path === '/favicon.ico') {
            res.writeHead(204);
            res.end();
            return;
        }
        if (req.method === 'GET' && path === '/api/conflict') {
            // A refusal of some other kind, as smd answers a command it cannot take now.
            res.writeHead(409, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ code: 'ft8_tx_not_armed', message: 'x', op: 'fixture' }));
            return;
        }
        if (req.method === 'GET' && path === '/api/echo') {
            res.writeHead(200, { 'content-type': 'application/json' });
            res.end(JSON.stringify({ tag, m: marker(req.url) }));
            return;
        }
        if (req.method === 'GET' && path === '/events') {
            res.writeHead(200, {
                'content-type': 'text/event-stream',
                'cache-control': 'no-store',
            });
            res.write(`data: ${tag}:${marker(req.url)}\n\n`);
            return; // left open, as a stream is
        }
        if (req.method === 'GET' && path === '/download') {
            res.writeHead(200, {
                'content-type': 'text/plain',
                'content-disposition': `attachment; filename="fixture-${marker(req.url)}.txt"`,
            });
            res.end(`${tag}:${marker(req.url)}\n`);
            return;
        }
        res.writeHead(404, { 'content-type': 'text/plain' });
        res.end('not a fixture route');
    };

    const onUpgrade = (req, socket) => {
        const cred = credentials(req);
        upgrades.push({ url: req.url, cred, lifetime: lifetimes(req) });
        if (!admitted(cred)) {
            socket.end('HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n');
            return;
        }
        const key = req.headers['sec-websocket-key'];
        if (!key || new URL(req.url, 'http://x').pathname !== '/ws') {
            socket.destroy();
            return;
        }
        const accept = crypto
            .createHash('sha1')
            .update(key + WS_GUID)
            .digest('base64');
        socket.write(
            'HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n' +
                (state.admission.required
                    ? `X-Sm-Daemon-Instance: ${state.admission.instance}\r\n` +
                      `X-Sm-Admission-Generation: ${state.admission.generation}\r\n`
                    : '') +
                `Sec-WebSocket-Accept: ${accept}\r\n\r\n`
        );
        socket.write(wsFrame(`${tag}:${marker(req.url)}`));
    };

    let server = null;
    const listen = () =>
        new Promise((resolve, reject) => {
            server = tls
                ? https.createServer({ key: tls.key, cert: tls.cert }, handler)
                : http.createServer(handler);
            server.on('upgrade', onUpgrade);
            server.on('connection', (s) => {
                sockets.add(s);
                s.on('close', () => sockets.delete(s));
            });
            if (tls) {
                server.on('secureConnection', () => (handshakes.secure += 1));
                server.on('tlsClientError', () => (handshakes.failed += 1));
            }
            server.once('error', reject);
            server.listen(port, '127.0.0.1', () => {
                port = server.address().port;
                resolve();
            });
        });
    const close = () =>
        new Promise((resolve) => {
            if (!server) return resolve();
            for (const s of sockets) s.destroy();
            sockets.clear();
            server.close(() => resolve());
            server = null;
        });

    // Reserve the port even when starting down, so the origin is known up front.
    await listen();
    if (!up) await close();

    return {
        get origin() {
            return `${tls ? 'https' : 'http'}://127.0.0.1:${port}`;
        },
        get port() {
            return port;
        },
        get up() {
            return server !== null;
        },
        tag,
        state,
        requests,
        upgrades,
        handshakes,
        replaceLifetime,
        start: () => (server ? Promise.resolve() : listen()),
        stop: close,
    };
}
