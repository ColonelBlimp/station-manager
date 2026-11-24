// The local "waiting for the daemon" page (ADR 0102, Decision; W-0027 A1).
//
// The page belongs to the shell, not the daemon: it is built here and loads nothing
// from the origin, so it works whatever is (or is not) listening there. The shell's
// main process probes the origin and loads the application once it answers. A failed
// load of the application later returns the window here.

import { randomBytes } from 'node:crypto';
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
 *  { state, path, reason } before the pin is fixed: "waiting" for what smd's start resolves by
 *  itself, "refused" for what needs the operator (`subject: 'credential'` when it is the
 *  operating credential's file, W-0027 C2b). After the pin, { state: 'unaccepted' } says the
 *  daemon answered but refused this app's credential. Only that bounded, escaped local text is
 *  shown: no config contents and never the credential, and the page itself reads no file and
 *  loads nothing. */
export function waitingPageUrl(origin, detail = null) {
    const refused = detail?.state === 'refused';
    const unaccepted = detail?.state === 'unaccepted';
    const retired = detail?.state === 'retired';
    let title = 'Waiting for the Station Manager daemon';
    let lead = 'Station Manager will open as soon as the daemon answers.';
    if (refused) {
        title =
            detail.subject === 'credential'
                ? 'Station Manager cannot use its operating credential'
                : 'Station Manager cannot trust the daemon’s identity';
        lead = 'Nothing from the daemon will be loaded until this is put right.';
    } else if (unaccepted) {
        title = 'The daemon did not accept this app';
        lead =
            'Its operating credential is not the one this app holds. Close Station Manager and open it again.';
    } else if (retired) {
        // W-0027 C4a: the daemon has admitted a later start of the app, and this one stays out.
        title = 'Another Station Manager has taken over';
        lead =
            'The daemon is being operated from a later start of Station Manager. Close this one.';
    }
    // Operator ruling Q3 (a), W-0027 D1: while smd may simply not be running, say how to start
    // it. Not on a refusal: starting smd does not repair an identity it must not be trusted with.
    // Not accepted: smd is running; if reopening the app does not help, restarting smd makes it
    // read its credential again.
    let start =
        '<p data-sm-start>If the daemon is not running, start it with <code>smctl start</code>.</p>';
    if (refused || retired) start = '';
    if (unaccepted)
        start =
            '<p data-sm-restart>If it still does not connect, restart the daemon with <code>smctl restart</code>.</p>';
    const info =
        detail && !unaccepted && !retired
            ? `<p data-sm-reason>${escapeHtml(detail.reason)}</p><p><small data-sm-path>${escapeHtml(detail.path)}</small></p>`
            : '';
    // The scrollbar rules are the app's (frontend/app/src/styles/app.css), copied because this
    // page loads nothing; the two colours are the theme's muted grey, light and dark. They are
    // held to the app's by frontend/app/src/styles/scrollbars.test.ts. Never set scrollbar-width
    // or scrollbar-color here; with either, Chromium ignores these rules and draws its own bar.
    const html = `<!doctype html><html lang="en"><meta charset="utf-8">
<title>Station Manager</title>
<style>
 body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
      font:15px system-ui,sans-serif;background:#f3f4f6;color:#101828}
 @media (prefers-color-scheme:dark){body{background:#101828;color:#e7e9ee}}
 .box{text-align:center;max-width:32rem;padding:2rem}
 h1{font-size:1.1rem;margin:0 0 .5rem} p{margin:.25rem 0;opacity:.75} small{word-break:break-all}
 ::-webkit-scrollbar{width:12px;height:12px}
 ::-webkit-scrollbar-track,::-webkit-scrollbar-corner{background:transparent}
 ::-webkit-scrollbar-thumb{border:3px solid transparent;border-radius:9999px;
      background-color:color-mix(in oklab,oklch(55.1% 0.027 264.364) 50%,transparent);background-clip:content-box}
 ::-webkit-scrollbar-thumb:hover{background-color:color-mix(in oklab,oklch(55.1% 0.027 264.364) 75%,transparent)}
 ::-webkit-scrollbar-thumb:active{background-color:oklch(55.1% 0.027 264.364)}
 @media (prefers-color-scheme:dark){
  ::-webkit-scrollbar-thumb{border:3px solid transparent;border-radius:9999px;
      background-color:color-mix(in oklab,#99a1af 50%,transparent);background-clip:content-box}
  ::-webkit-scrollbar-thumb:hover{background-color:color-mix(in oklab,#99a1af 75%,transparent)}
  ::-webkit-scrollbar-thumb:active{background-color:#99a1af}}
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
// The mark on every probe the shell itself sends (codex-112): a value made in this process at
// start and known to no page. The refusal watcher recognises the shell's own requests by it
// (isShellProbe) instead of by who Electron reports as the sender, which a shared worker's
// request does not name either. smd ignores the parameter.
const PROBE_PARAM = 'sm-probe';
const PROBE_MARK = randomBytes(16).toString('hex');

/** The address the shell probes: smd's version endpoint (ADR 0103 point 6; codex-068), marked. */
export const probeUrl = (origin) => `${origin}/v1/version?${PROBE_PARAM}=${PROBE_MARK}`;

/** True only for a request this shell's main process made as a probe. */
export function isShellProbe(url) {
    try {
        const u = new URL(url);
        return u.pathname === '/v1/version' && u.searchParams.get(PROBE_PARAM) === PROBE_MARK;
    } catch {
        return false;
    }
}

async function originAnswers(ses, origin) {
    try {
        const res = await ses.fetch(probeUrl(origin), {
            redirect: 'manual',
            signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
        });
        if (res.status === 200) return 'up';
        if (res.status === 403 && (await refusalCode(res)) === CREDENTIAL_REFUSED)
            return 'unaccepted';
        return 'down';
    } catch {
        return 'down';
    }
}

/** smd's refusal code for a client without its operating credential (internal/api). */
export const CREDENTIAL_REFUSED = 'desktop_app_required';

/** The `code` of smd's JSON error envelope, or '' for anything else. Never throws. */
async function refusalCode(res) {
    try {
        const body = await res.json();
        return typeof body?.code === 'string' ? body.code : '';
    } catch {
        return '';
    }
}

/** True when smd, asked through the session, refuses this app's credential (W-0027 C2b). The
 *  main process asks, so the answer does not depend on what a page reports. */
export async function credentialRefused(ses, origin) {
    return (await originAnswers(ses, origin)) === 'unaccepted';
}

/** Load url into win once origin answers, showing the waiting page meanwhile. One
 *  retry loop per window: a second call while one is waiting does not start another.
 *  `afterFailure` shows the waiting page and pauses before the first probe, so a load
 *  that keeps failing retries at the waiting pace instead of in a tight loop. While smd
 *  answers but refuses the credential, the page says so, once, and the loop goes on: the
 *  credential is fixed for this app's lifetime, but the daemon may be restarted.
 *
 *  `admit` (W-0027 C4a) is the app's admission: it runs once smd answers and before the
 *  application is loaded, so smd has stopped whatever an earlier start left running before
 *  this one shows its workspace. Retired ends the loop on a page that says so; a failed
 *  admission is tried again at the waiting pace. */
export async function loadWhenUp(win, url, origin, { afterFailure = false, admit } = {}) {
    if (win.isDestroyed() || win.__smWaiting) return;
    win.__smWaiting = true;
    let unacceptedShown = false;
    try {
        if (afterFailure) {
            void win.loadURL(waitingPageUrl(origin)).catch(() => {});
            await new Promise((r) => setTimeout(r, RETRY_MS));
        }
        while (!win.isDestroyed()) {
            const answer = await originAnswers(win.webContents.session, origin);
            if (answer === 'up') {
                // The window may have gone while the probe was out. An answer that comes
                // back to no window starts nothing, an admission least of all (codex-239).
                if (win.isDestroyed()) return;
                const admitted = admit ? (await admit()).state : 'admitted';
                if (win.isDestroyed()) return;
                if (admitted === 'retired') {
                    void win.loadURL(waitingPageUrl(origin, { state: 'retired' })).catch(() => {});
                    return;
                }
                if (admitted !== 'failed') {
                    void win.loadURL(url).catch(() => {});
                    return;
                }
                // Not admitted and not refused for good: wait and ask again, as for a daemon
                // that is not up.
                await new Promise((r) => setTimeout(r, RETRY_MS));
                continue;
            }
            if (win.isDestroyed()) return;
            if (answer === 'unaccepted') {
                if (!unacceptedShown) {
                    unacceptedShown = true;
                    void win
                        .loadURL(waitingPageUrl(origin, { state: 'unaccepted' }))
                        .catch(() => {});
                }
            } else if (unacceptedShown || !win.webContents.getURL().startsWith('data:')) {
                unacceptedShown = false;
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
export function returnToWaitingOnFailure(win, appUrl, origin, { admit } = {}) {
    win.webContents.on('will-redirect', (e) => {
        if (!e.isMainFrame || onOrigin(e.url, origin)) return;
        e.preventDefault();
        void loadWhenUp(win, appUrl, origin, { afterFailure: true, admit });
    });
    win.webContents.on('did-fail-load', (_e, code, _desc, failedUrl, isMainFrame) => {
        if (!isMainFrame || code === -3 /* superseded or stopped (incl. the guard above) */) return;
        if (failedUrl.startsWith(origin + '/')) {
            void loadWhenUp(win, appUrl, origin, { afterFailure: true, admit });
        }
    });
}
