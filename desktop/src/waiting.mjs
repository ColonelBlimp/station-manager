// The local "waiting for the daemon" page (ADR 0102, Decision; W-0027 A1).
//
// The page belongs to the shell, not the daemon: it is built here and loads nothing
// from the origin, so it works whatever is (or is not) listening there. The shell's
// main process probes the origin and loads the application once it answers. A failed
// load of the application later returns the window here.

import { onOrigin } from './hardening.mjs';

const PROBE_TIMEOUT_MS = 800;
const RETRY_MS = 1000;

export function waitingPageUrl(origin) {
    const html = `<!doctype html><html lang="en"><meta charset="utf-8">
<title>Station Manager</title>
<style>
 body{margin:0;height:100vh;display:flex;align-items:center;justify-content:center;
      font:15px system-ui,sans-serif;background:#f3f4f6;color:#101828}
 @media (prefers-color-scheme:dark){body{background:#101828;color:#e7e9ee}}
 .box{text-align:center;max-width:28rem;padding:2rem}
 h1{font-size:1.1rem;margin:0 0 .5rem} p{margin:.25rem 0;opacity:.75}
</style>
<div class="box" data-sm-waiting>
 <h1>Waiting for the Station Manager daemon</h1>
 <p>Station Manager will open as soon as the daemon answers.</p>
 <p><small>${origin.replace(/[<>&"]/g, '')}</small></p>
</div></html>`;
    return 'data:text/html;charset=utf-8,' + encodeURIComponent(html);
}

// An availability probe only: it says something answers at the origin, not that it is
// smd. It must never stand in for C1's authentication. Redirects are refused, so a
// replacement listener cannot send this main-process request (which the renderer's
// request filter does not see) to another origin (codex-038).
async function originAnswers(origin) {
    try {
        const res = await fetch(origin + '/', {
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
            if (await originAnswers(origin)) {
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
