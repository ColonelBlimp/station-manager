// Session hardening (ADR 0102, Requirements and Consequences; W-0027 A1).
//
// - Permissions are denied by default. Electron grants permission requests itself when
//   no handler is set, so both the request and the check handlers are installed; device
//   access (USB, serial, HID) is refused too. A permission the application needs is
//   granted here by name when a slice identifies it.
// - Renderers reach only the served origin. Every other request is cancelled and
//   reported; data: and blob: stay local to the page.
// - While the app is closing, the page starts nothing new (W-0027 A3, B5): its writes on the
//   served origin are refused, each one, from the moment the close is confirmed until the
//   window goes or the close is cancelled. Its reads go on. The origin rule is not loosened.

/** True when url belongs to the served origin: its own scheme or its WebSocket partner. */
export function onOrigin(url, origin) {
    if (!origin) return false; // no origin fixed yet: nothing is on it (W-0027 C1d)
    let u;
    try {
        u = new URL(url);
    } catch {
        return false;
    }
    const o = new URL(origin);
    if (u.host !== o.host) return false;
    // The origin's own scheme, or its WebSocket partner: http with ws, https with wss
    // (ADR 0103 point 5). Never a downgrade (https origin, ws or http request).
    const ws = { 'http:': 'ws:', 'https:': 'wss:' }[o.protocol];
    return u.protocol === o.protocol || u.protocol === ws;
}

/** No spellchecker: Electron downloads its Hunspell dictionaries from Google's CDN (found
 *  in the C1 proof's network log: redirector.gvt1.com/.../en-gb-10-1.bdic), a connection off
 *  the machine. Nothing leaves the machine (ADR 0102). */
export function disableSpellchecker(ses) {
    ses.setSpellCheckerEnabled(false);
    ses.setSpellCheckerLanguages([]);
}

// What a request may do without changing anything.
const READS = new Set(['GET', 'HEAD']);

/** `origin` is the served origin, or a function read on each request (W-0027 C1d: fixed once,
 *  together with the pin; null until then).
 *
 *  Returns the switch for a close, `{ refuse, allow }`. While refused, a request on the served
 *  origin that is not a read is cancelled and `onWriteRefused(details)` told, unless it is the
 *  shell's own: `isShellRequest(url)` recognises those by a mark only the main process knows.
 *  Who Electron names as the sender is not asked: a worker's request names no window, and
 *  would pass for the shell's (codex-112). It is decided per request, so it covers what the
 *  page had deferred and its own continuations, not only the operator's hands. Refusing a new
 *  request is no proof that one sent earlier was cancelled. */
export function hardenSession(
    ses,
    { origin, onAllowed, onBlocked, onPermission, onWriteRefused, isShellRequest }
) {
    const originOf = typeof origin === 'function' ? origin : () => origin;
    let writesRefused = false;
    ses.setPermissionRequestHandler((_wc, permission, callback) => {
        onPermission?.({ kind: 'request', permission });
        callback(false);
    });
    ses.setPermissionCheckHandler((_wc, permission) => {
        onPermission?.({ kind: 'check', permission });
        return false;
    });
    ses.setDevicePermissionHandler(() => false);

    disableSpellchecker(ses);

    ses.webRequest.onBeforeRequest((details, callback) => {
        const url = details.url;
        const local = url.startsWith('data:') || url.startsWith('blob:');
        if (!local && !onOrigin(url, originOf())) {
            onBlocked?.(url);
            callback({ cancel: true });
            return;
        }
        if (writesRefused && !local && !READS.has(details.method) && !isShellRequest?.(url)) {
            onWriteRefused?.(details);
            callback({ cancel: true });
            return;
        }
        onAllowed?.(details);
        callback({});
    });
    return {
        refuse: () => {
            writesRefused = true;
        },
        allow: () => {
            writesRefused = false;
        },
    };
}

// The statuses on which a redirect is followed; a Location on any other response is not one.
const REDIRECT_STATUS = new Set([301, 302, 303, 307, 308]);

/**
 * True when a response is a redirect that would leave the served origin, so that it is
 * refused at the response and never followed (operator ruling 2026-11-19: an address other
 * than smd's gets no contact at all). onBeforeRequest above would cancel the redirected
 * request, but by then the connection to its target could already be made: one TLS handshake
 * with no request on it, about one time in three at a fixture (W-0027, C2b).
 *
 * `details` is what webRequest.onHeadersReceived gives: the request's url, the status and the
 * response headers (names in any case, each value a string or a list). The Location is
 * resolved against the request's own address, as the redirect would be. What cannot be read
 * as one address is refused: a Location that does not parse, or two that differ.
 */
export function redirectsOffOrigin(details, origin) {
    if (!origin || !REDIRECT_STATUS.has(details?.statusCode)) return false;
    const targets = new Set();
    for (const [name, value] of Object.entries(details.responseHeaders ?? {})) {
        if (name.toLowerCase() !== 'location') continue;
        for (const v of Array.isArray(value) ? value : [value]) targets.add(String(v));
    }
    if (targets.size === 0) return false;
    if (targets.size > 1) return true;
    const [location] = targets;
    let to;
    try {
        to = new URL(location, details.url);
    } catch {
        return true;
    }
    // The origin's own scheme only: a redirect is not a WebSocket upgrade.
    return !(to.protocol === new URL(origin).protocol && onOrigin(to.href, origin));
}

/** The one Location a response carries, for a report; '' when it has none or several. */
export function redirectTarget(details) {
    const found = [];
    for (const [name, value] of Object.entries(details?.responseHeaders ?? {})) {
        if (name.toLowerCase() !== 'location') continue;
        for (const v of Array.isArray(value) ? value : [value]) found.push(String(v));
    }
    return new Set(found).size === 1 ? found[0] : '';
}
