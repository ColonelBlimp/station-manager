// The A1 fixture harness (W-0027): a stand-in origin on an ephemeral loopback port.
//
// Until C1's authentication is implemented and validated, the shell executes no
// daemon-served UI (ADR 0102, Requirements). The harness is the only origin it loads:
// it never connects to smd, the rig or the network, and its port is chosen by the OS,
// never smd's.
import http from 'node:http';

const PAGE = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Station Manager — A1 fixture</title></head>
<body>
<main data-sm-fixture>
<h1>Station Manager desktop shell</h1>
<p>A1 fixture page: no daemon, no rig, no network.</p>
</main>
</body>
</html>
`;

/** Start the harness. `stop()` closes the listener and keeps its port, so `start()`
 *  brings the "daemon" back at the same origin (connection refused in between). */
export async function startHarness({ up = true } = {}) {
    const requests = [];
    const handler = (req, res) => {
        requests.push({ method: req.method, url: req.url });
        if (req.method === 'GET' && (req.url === '/' || req.url === '/index.html')) {
            res.writeHead(200, { 'content-type': 'text/html; charset=utf-8' });
            res.end(PAGE);
            return;
        }
        if (req.method === 'GET' && req.url === '/favicon.ico') {
            res.writeHead(204);
            res.end();
            return;
        }
        res.writeHead(404, { 'content-type': 'text/plain' });
        res.end('not a fixture route');
    };

    let server = null;
    let port = 0;
    const listen = () =>
        new Promise((resolve, reject) => {
            server = http.createServer(handler);
            server.once('error', reject);
            server.listen(port, '127.0.0.1', () => {
                port = server.address().port;
                resolve();
            });
        });
    const close = () =>
        new Promise((resolve) => {
            if (!server) return resolve();
            server.closeAllConnections();
            server.close(() => resolve());
            server = null;
        });

    // Reserve the port even when starting down, so the origin is known up front.
    await listen();
    if (!up) await close();

    return {
        get origin() {
            return `http://127.0.0.1:${port}`;
        },
        get up() {
            return server !== null;
        },
        requests,
        start: () => (server ? Promise.resolve() : listen()),
        stop: close,
    };
}
