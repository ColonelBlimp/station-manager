// The main window (ADR 0102 rulings 2 and 3; W-0027 A1).
//
// Renderers are sandboxed and context-isolated, with no Node and no webview. A page
// cannot create windows (the shell owns them) or navigate away from the served origin.
// An external address goes to the default browser, and only when it is https.
import { fileURLToPath } from 'node:url';
import { BrowserWindow, nativeImage } from 'electron';
import { onOrigin } from './hardening.mjs';

export const MAIN_MIN = { width: 1024, height: 640 };
// The window icon (title bar and window switcher under X11/XWayland): the SPA's
// logo.svg rendered to 256x256. Launcher and native-Wayland icons come with the
// package (W-0027 D1).
export const ICON_PATH = fileURLToPath(new URL('../assets/icon.png', import.meta.url));

/** Hand an external address to the default browser: https only, never file: or other
 *  scheme handlers (ADR 0102, Consequences). */
export function externalOpener(open) {
    return (url) => {
        let u;
        try {
            u = new URL(url);
        } catch {
            return;
        }
        if (u.protocol === 'https:') open(u.href);
    };
}

/** The main window's construction options (exported so the A1 check reads exactly what
 *  createMainWindow uses). */
export function mainWindowOptions({ offscreen }) {
    return {
        width: 1400,
        height: 900,
        minWidth: MAIN_MIN.width,
        minHeight: MAIN_MIN.height,
        title: 'Station Manager',
        show: !offscreen,
        webPreferences: {
            sandbox: true,
            contextIsolation: true,
            nodeIntegration: false,
            webviewTag: false,
            spellcheck: false,
            offscreen,
        },
        icon: nativeImage.createFromPath(ICON_PATH),
    };
}

export function createMainWindow({ origin, offscreen, openExternal }) {
    const win = new BrowserWindow(mainWindowOptions({ offscreen }));
    win.webContents.on('will-navigate', (e, url) => {
        if (onOrigin(url, origin)) return;
        e.preventDefault();
        openExternal(url);
    });
    win.webContents.on('will-attach-webview', (e) => e.preventDefault());
    win.webContents.setWindowOpenHandler(({ url }) => {
        if (!onOrigin(url, origin)) openExternal(url);
        return { action: 'deny' };
    });
    return win;
}
