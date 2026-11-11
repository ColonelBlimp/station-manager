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
    const state = { versionReady: true };
    const handshakes = { secure: 0, failed: 0 };
    const sockets = new Set();
    const marker = (url) => new URL(url, 'http://x').searchParams.get('m') ?? '';

    const handler = (req, res) => {
        requests.push({ method: req.method, url: req.url });
        const path = new URL(req.url, 'http://x').pathname;
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
        upgrades.push({ url: req.url });
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
        start: () => (server ? Promise.resolve() : listen()),
        stop: close,
    };
}
