// Session hardening (ADR 0102, Requirements and Consequences; W-0027 A1).
//
// - Permissions are denied by default. Electron grants permission requests itself when
//   no handler is set, so both the request and the check handlers are installed; device
//   access (USB, serial, HID) is refused too. A permission the application needs is
//   granted here by name when a slice identifies it.
// - Renderers reach only the served origin. Every other request is cancelled and
//   reported; data: and blob: stay local to the page.

/** True when url belongs to the served origin (its http pages or its websocket). */
export function onOrigin(url, origin) {
    let u;
    try {
        u = new URL(url);
    } catch {
        return false;
    }
    const o = new URL(origin);
    if (u.host !== o.host) return false;
    return u.protocol === o.protocol || (o.protocol === 'http:' && u.protocol === 'ws:');
}

export function hardenSession(ses, { origin, onAllowed, onBlocked, onPermission }) {
    ses.setPermissionRequestHandler((_wc, permission, callback) => {
        onPermission?.({ kind: 'request', permission });
        callback(false);
    });
    ses.setPermissionCheckHandler((_wc, permission) => {
        onPermission?.({ kind: 'check', permission });
        return false;
    });
    ses.setDevicePermissionHandler(() => false);

    ses.webRequest.onBeforeRequest((details, callback) => {
        const url = details.url;
        if (onOrigin(url, origin) || url.startsWith('data:') || url.startsWith('blob:')) {
            onAllowed?.(details);
            callback({});
            return;
        }
        onBlocked?.(url);
        callback({ cancel: true });
    });
}
