// The operating credential the shell presents (ADR 0102 ruling 1; ADR 0103 point 7; W-0027 C2b).
//
// smd's operating-client check serves only a request that arrives on its pinned listener and
// carries this install's credential. The shell's main process adds it, as one request header,
// to requests for the pinned origin and nothing else. It is added here, after the page has made
// its request, so page script never holds it; and it is sent only inside a connection the pin
// has already authenticated, so it never reaches an impostor and never leaves the machine.
//
// The value is fixed once, together with the pin and the origin, for the app's lifetime. A
// replaced credential is adopted when the app is started again, as a replaced identity is.
import { onOrigin } from './hardening.mjs';

export const CREDENTIAL_HEADER = 'X-SM-Operating-Credential';
const LOWER = CREDENTIAL_HEADER.toLowerCase();

/** A credential set exactly once. Before it is set, `get()` returns null, so nothing is sent; a
 *  second `set` throws. It never prints its value. */
export function onceCredential() {
    let value = null;
    const shown = '[operating credential]';
    return {
        get: () => value,
        set(v) {
            if (value !== null)
                throw new Error('the credential is already fixed for this app lifetime');
            if (typeof v !== 'string' || v === '') throw new Error('an empty credential');
            value = v;
        },
        toString: () => shown,
        toJSON: () => shown,
    };
}

/** True when url is the pinned origin itself or its WebSocket partner, over TLS. A plain-HTTP
 *  origin never receives the credential, whatever it is. */
function receives(url, origin) {
    if (!origin || !origin.startsWith('https://')) return false;
    return onOrigin(url, origin); // https or wss on exactly that host and port; never a downgrade
}

/** The headers a request goes out with. Any header of the credential's name already there, in
 *  any letter case, is removed, so a page can neither supply one nor make the shell send two;
 *  then the credential is added if, and only if, the request is for the pinned origin. Pure:
 *  `headers` is not modified. */
export function credentialHeaders(headers, url, origin, value) {
    const out = {};
    for (const [k, v] of Object.entries(headers ?? {})) {
        if (k.toLowerCase() !== LOWER) out[k] = v;
    }
    if (typeof value === 'string' && value !== '' && receives(url, origin))
        out[CREDENTIAL_HEADER] = value;
    return out;
}

/** Install the header on a session before its first request. `origin` and `credential` are read
 *  at each request (null until fixed). Electron keeps one listener per webRequest event, so this
 *  is the only owner of onBeforeSendHeaders; the request filter in hardening.mjs is a different
 *  event. `onRequest` sees, for tests, each request's URL and whether the credential went. */
export function installCredential(ses, { origin, credential, onRequest }) {
    ses.webRequest.onBeforeSendHeaders((details, callback) => {
        const requestHeaders = credentialHeaders(
            details.requestHeaders,
            details.url,
            origin(),
            credential()
        );
        onRequest?.({ url: details.url, sent: CREDENTIAL_HEADER in requestHeaders });
        callback({ requestHeaders });
    });
}

/** Tell the operator, once, when smd refuses the credential in a loaded workspace (W-0027 C2b):
 *  after a deliberate replacement, say. Nothing is navigated, so what the operator has typed
 *  stays where it is; the app adopts the new credential when it is started again.
 *
 *  A sign of refusal on the pinned origin only prompts a question: the main process then asks
 *  smd itself (`confirm`), so a page can neither fake nor hide the refusal, and another cause is
 *  not mistaken for it. There are two signs (codex-112, measured on Electron 44):
 *    - a request that completes with 403, whoever sent it: a shared worker's request names no
 *      window, so the sender is not asked about;
 *    - a WebSocket that fails: a refused upgrade is reported as an error with no status at all.
 *      The daemon being down looks the same, and smd's answer (none) tells them apart.
 *  The shell's own probes are recognised by their mark (`isProbe`) and never prompt: not the
 *  question itself, so it cannot ask for ever, and not the waiting page's, which shows this
 *  state on its own and makes no other request.
 *
 *  Electron keeps one listener per webRequest event: this is the only owner of onCompleted and
 *  of onErrorOccurred. */
export function watchRefusal(ses, { origin, isProbe, confirm, onRefused }) {
    let asking = false;
    let told = false;
    const ask = (url) => {
        if (told || asking || isProbe(url) || !receives(url, origin())) return;
        asking = true;
        // Asked at once; a confirm that throws is a failed question, like one that rejects.
        new Promise((resolve) => resolve(confirm(ses, origin())))
            .then((refused) => {
                if (refused && !told) {
                    told = true;
                    onRefused();
                }
            })
            .catch(() => {})
            .finally(() => {
                asking = false;
            });
    };
    ses.webRequest.onCompleted((details) => {
        if (details.statusCode === 403) ask(details.url);
    });
    ses.webRequest.onErrorOccurred((details) => {
        if (details.resourceType === 'webSocket') ask(details.url);
    });
}
