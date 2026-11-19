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
    // app_admission.go): the fixture keeps one current lifetime, a generation and the
    // lifetimes it has replaced, and puts `instance`, its process marker, on what it serves.
    // `holdAdmit`, when set, is awaited before an admit is answered, so a test can place
    // another admission between a read and its admit.
    const state = {
        versionReady: true,
        requireCredential: null,
        forbid: null,
        admission: {
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
    // Every TCP connection accepted, whatever came of it: a test of "no contact at all" counts
    // these, since a connection can be made and dropped with no handshake and no request.
    const accepts = { count: 0 };
    // One record per accepted connection, in order, with what became of it: 'secure' (a
    // completed TLS handshake), 'failed' (a refused one), or null while neither has happened,
    // which is what an idle or plain connection stays at. A test reads the records made
    // during its own window, so a handshake that an earlier connection completes meanwhile
    // cannot stand in for a new connection that began none.
    //
    // A record is found through the raw socket under the TLS one (`_parent`, which Node sets
    // but does not document), not through the peer's port: a client that closes before any
    // handshake gets a TLS error on a socket that no longer has a port, and its record would
    // stay unsettled while the failed count rose (clean-room review of 92002312). If
    // `_parent` were ever missing, no record would settle and every reading would be 'none',
    // which fails a test rather than passing it.
    const connections = [];
    const byRawSocket = new WeakMap();
    const settle = (tlsSocket, outcome) => {
        const c = byRawSocket.get(tlsSocket?._parent);
        if (c && c.outcome === null) c.outcome = outcome;
    };
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
                daemon_instance: a.instance,
                generation: a.generation,
                current: lifetime !== '' && lifetime === a.current,
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
        // As smd: this process's marker on everything it serves.
        const isAdmission =
            path === '/v1/app/lifetime' && (req.method === 'GET' || req.method === 'POST');
        let instance = state.admission.instance;
        if (!isAdmission && state.admission.oneOffMarker !== null) {
            instance = state.admission.oneOffMarker;
            state.admission.oneOffMarker = null;
        }
        res.setHeader('X-Sm-Daemon-Instance', instance);
        res.setHeader('X-Sm-Admission-Generation', String(state.admission.generation));
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
            // A redirect to wherever `to` says: a test's way off the origin. `status` is the
            // redirect's status (302 unless given), `hdr` the header's spelling, and `to2` a
            // second Location header, for a response that names two addresses.
            const q = new URL(req.url, 'http://x').searchParams;
            const name = q.get('hdr') ?? 'location';
            const headers = [name, String(q.get('to'))];
            if (q.has('to2')) headers.push(name, String(q.get('to2')));
            res.writeHead(Number(q.get('status') ?? 302), headers);
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
                `X-Sm-Daemon-Instance: ${state.admission.instance}\r\n` +
                `X-Sm-Admission-Generation: ${state.admission.generation}\r\n` +
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
                accepts.count += 1;
                const record = { outcome: null };
                connections.push(record);
                byRawSocket.set(s, record);
                sockets.add(s);
                s.on('close', () => sockets.delete(s));
            });
            if (tls) {
                server.on('secureConnection', (t) => {
                    handshakes.secure += 1;
                    settle(t, 'secure');
                });
                server.on('tlsClientError', (_err, t) => {
                    handshakes.failed += 1;
                    settle(t, 'failed');
                });
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
        accepts,
        connections,
        replaceLifetime,
        start: () => (server ? Promise.resolve() : listen()),
        stop: close,
    };
}
