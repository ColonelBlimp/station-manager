// The local "waiting for the daemon" page (ADR 0102, Decision; W-0027 A1).
//
// The page belongs to the shell, not the daemon: it is built here and loads nothing
// from the origin, so it works whatever is (or is not) listening there. The shell's
// main process probes the origin and loads the application once it answers. A failed
// load of the application later returns the window here.

import { onOrigin } from './hardening.mjs';

const PROBE_TIMEOUT_MS = 800;
const RETRY_MS = 1000;

/** Escape text for the page's HTML: everything shown is local text, never markup. */
export function escapeHtml(text) {
    return String(text).replace(
        /[&<>"']/g,
        (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]
    );
}

// The waiting documents this shell generated, most recent last (W-0027 D1; codex-077): the trace
// names a page the waiting page only when it is one of these exact documents, never by its
// data: scheme. Bounded: a document is looked up when its load finishes, moments after it was
// made, so the latest few suffice.
const RECENT_WAITING = 32;
const recentWaiting = new Set();

function remember(url) {
    recentWaiting.delete(url);
    recentWaiting.add(url);
    if (recentWaiting.size > RECENT_WAITING)
        recentWaiting.delete(recentWaiting.values().next().value);
    return url;
}

/** True only for an exact waiting document this shell generated recently. */
export const isWaitingPage = (url) => recentWaiting.has(url);

/** The waiting page. `detail` (W-0027 C1d, operator ruling Q2) is the identity discovery's
 *  { state, path, reason } before the pin is fixed: "waiting" for what smd's first run
 *  resolves by itself, "refused" for what needs the operator. Only that bounded, escaped local
 *  text is shown: no config contents, and the page itself reads no file and loads nothing. */
export function waitingPageUrl(origin, detail = null) {
    const refused = detail?.state === 'refused';
    const title = refused
        ? 'Station Manager cannot trust the daemon’s identity'
        : 'Waiting for the Station Manager daemon';
    const lead = refused
        ? 'Nothing from the daemon will be loaded until this is put right.'
        : 'Station Manager will open as soon as the daemon answers.';
    // Operator ruling Q3 (a), W-0027 D1: while smd may simply not be running, say how to start
    // it. Not on a refusal: starting smd does not repair an identity it must not be trusted with.
    const start = refused
        ? ''
        : '<p data-sm-start>If the daemon is not running, start it with <code>smctl start</code>.</p>';
    const info = detail
        ? `<p data-sm-reason>${escapeHtml(detail.reason)}</p><p><small data-sm-path>${escapeHtml(detail.path)}</small></p>`
        : '';
    const html = `<!doctype html><html lang="en"><meta charset="utf-8">
<title>Station Manager</title>
<style>
 body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
      font:15px system-ui,sans-serif;background:#f3f4f6;color:#101828}
 @media (prefers-color-scheme:dark){body{background:#101828;color:#e7e9ee}}
 .box{text-align:center;max-width:32rem;padding:2rem}
 h1{font-size:1.1rem;margin:0 0 .5rem} p{margin:.25rem 0;opacity:.75} small{word-break:break-all}
</style>
<div class="box" data-sm-waiting data-sm-state="${escapeHtml(detail?.state ?? 'waiting')}">
 <h1>${escapeHtml(title)}</h1>
 <p>${escapeHtml(lead)}</p>
 ${info}
 ${start}
 <p><small>${escapeHtml(origin ?? '')}</small></p>
</div></html>`;
    return remember('data:text/html;charset=utf-8,' + encodeURIComponent(html));
}

// The availability probe. It goes through the window's own session (`ses.fetch`), so it
// passes the session's request filter and, over TLS, its certificate pin (ADR 0103 point
// 6): with a pinned https origin, a listener without smd's key never counts as up. Over
// plain http (A1's fixture) it says only that something answers. Redirects are refused, so
// a replacement listener cannot send the probe to another origin (codex-038).
async function originAnswers(ses, origin) {
    try {
        // ADR 0103 point 6: the pinned probe asks smd's version endpoint (codex-068).
        const res = await ses.fetch(origin + '/v1/version', {
            redirect: 'manual',
            signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
        });
        return res.status === 200;
    } catch {
        return false;
    }
}

/** Load url into win once origin answers, showing the waiting page meanwhile. One
 *  retry loop per window: a second call while one is waiting does not start another.
 *  `afterFailure` shows the waiting page and pauses before the first probe, so a load
 *  that keeps failing retries at the waiting pace instead of in a tight loop. */
export async function loadWhenUp(win, url, origin, { afterFailure = false } = {}) {
    if (win.isDestroyed() || win.__smWaiting) return;
    win.__smWaiting = true;
    try {
        if (afterFailure) {
            void win.loadURL(waitingPageUrl(origin)).catch(() => {});
            await new Promise((r) => setTimeout(r, RETRY_MS));
        }
        while (!win.isDestroyed()) {
            if (await originAnswers(win.webContents.session, origin)) {
                if (!win.isDestroyed()) void win.loadURL(url).catch(() => {});
                return;
            }
            if (win.isDestroyed()) return;
            if (!win.webContents.getURL().startsWith('data:')) {
                void win.loadURL(waitingPageUrl(origin)).catch(() => {});
            }
            await new Promise((r) => setTimeout(r, RETRY_MS));
        }
    } finally {
        win.__smWaiting = false;
    }
}

/** The application's failed or redirected main-frame loads return the window to waiting.
 *  A redirect away from the origin is stopped before it is followed, so no error page or
 *  other address is ever shown (the request filter would cancel it anyway). Retries after a
 *  failure pause first, so a listener that keeps failing loads gets the waiting pace. */
export function returnToWaitingOnFailure(win, appUrl, origin) {
    win.webContents.on('will-redirect', (e) => {
        if (!e.isMainFrame || onOrigin(e.url, origin)) return;
        e.preventDefault();
        void loadWhenUp(win, appUrl, origin, { afterFailure: true });
    });
    win.webContents.on('did-fail-load', (_e, code, _desc, failedUrl, isMainFrame) => {
        if (!isMainFrame || code === -3 /* superseded or stopped (incl. the guard above) */) return;
        if (failedUrl.startsWith(origin + '/')) {
            void loadWhenUp(win, appUrl, origin, { afterFailure: true });
        }
    });
}
