// Launching an app window against smd's own listener (ADR 0103; W-0027 C1d).
//
// The order is the guarantee: the verifier (an empty pin, so it rejects everything), the
// session hardening (no origin, so nothing is allowed on the network) and the credential header
// (no credential, so none is sent; W-0027 C2b) are installed before the window exists; only then
// does discovery run, and the pin, the origin and the credential are fixed together at the first
// valid identity and credential (src/daemon.mjs). The interactive app and the real-smd acceptance
// (test/scenario-c1d.mjs) launch through this one function.
//
// The app lifetime (W-0027 C4a; src/admission.mjs) is made here, once per launch, and goes out
// with the credential. The app is admitted with it before the application is loaded, and again
// whenever smd's responses show a daemon process it has not been admitted to.
import { session } from 'electron';
import { admission, newLifetime, watchDaemon } from './admission.mjs';
import { closeFlow, isCloseRequest } from './closing.mjs';
import { installCredential, onceCredential, watchRefusal } from './credential.mjs';
import { connectWhenTrusted } from './daemon.mjs';
import { recordingFs } from './discovery.mjs';
import { hardenSession } from './hardening.mjs';
import { installPin, oncePin } from './pin.mjs';
import { blockedRequestLine, refusedRedirectLine, refusedWriteLine } from './trace.mjs';
import { credentialRefused, isShellProbe } from './waiting.mjs';
import { createMainWindow } from './window.mjs';

/**
 * Launch one window. `partition` gives it its own session (its own verifier, pin and origin),
 * which only a test of a second, freshly started window needs. `onCredentialRefused` is called
 * once if smd refuses the credential in a loaded workspace; `onCredentialRequest` sees, for
 * tests, each request and whether the credential went with it. `onRetired` is called once if
 * smd has admitted a later start of the app in place of this one. Returns the window and, for
 * tests, the pin, the credential holder, the target origin, every file the discovery opened,
 * the verifier's decisions, the requests the filter blocked, the app lifetime and its admission.
 *
 * It also returns `closing`, the window's close (W-0027 A3; src/closing.mjs), made of this
 * launch's own session, admission, request filter and window. `askUnconfirmed` shows the box
 * for a close smd did not confirm and `quit` ends the app. NOTHING HERE RUNS IT: the window's
 * own close is not wired to it until the question that goes before it is built (A3's second
 * slice; ADR 0102 ruling 6 asks before a run is stopped or an entry lost). Only the test
 * scenarios call `closing.close()`.
 */
export function launchTrusted({
    env = process.env,
    args = {},
    offscreen = false,
    openExternal,
    partition,
    retryMs,
    onPermission,
    onCredentialRefused,
    onCredentialRequest,
    onRetired,
    askUnconfirmed,
    quit,
}) {
    const ses = partition ? session.fromPartition(partition) : session.defaultSession;
    const pin = oncePin();
    const cred = onceCredential();
    const target = { origin: null };
    const lifetime = newLifetime();
    const rec = recordingFs();
    const decisions = [];
    const blocked = [];
    installPin(ses, { fingerprint: pin.get, onDecision: (d) => decisions.push(d) });
    const pageWrites = hardenSession(ses, {
        origin: () => target.origin,
        onBlocked: (url) => {
            blocked.push(url);
            console.error(blockedRequestLine(url));
        },
        onPermission,
        isShellRequest: isCloseRequest,
        onWriteRefused: (d) => console.error(refusedWriteLine(d.method)),
    });
    installCredential(ses, {
        origin: () => target.origin,
        credential: cred.get,
        lifetime: () => lifetime,
        onRequest: onCredentialRequest,
    });
    const admitted = admission({
        ses,
        origin: () => target.origin,
        onRetired: () => onRetired?.(),
    });
    watchDaemon(ses, {
        origin: () => target.origin,
        onMarker: admitted.sawMarker,
        onRedirectRefused: (from, to) => {
            blocked.push(to);
            console.error(refusedRedirectLine(from, to));
        },
    });
    watchRefusal(ses, {
        origin: () => target.origin,
        isProbe: isShellProbe,
        confirm: credentialRefused,
        onRefused: () => onCredentialRefused?.(),
    });
    const win = createMainWindow({
        origin: () => target.origin,
        offscreen,
        openExternal,
        partition,
    });
    const closing = closeFlow({
        ses,
        origin: () => target.origin,
        admission: admitted,
        window: {
            hide: () => !win.isDestroyed() && win.hide(),
            show: () => !win.isDestroyed() && win.show(),
            // destroy(), not close(): the page's veto of an unload has no say in a close the
            // operator has confirmed, and must not leave a window open whose lifetime is
            // retired (A3, section H).
            destroy: () => !win.isDestroyed() && win.destroy(),
        },
        pageWrites,
        ask: (state) => askUnconfirmed(state),
        quit: () => quit(),
    });
    void connectWhenTrusted({
        win,
        pin,
        cred,
        target,
        args,
        env,
        fsb: rec.fs,
        retryMs,
        admit: admitted.admitNow,
    });
    return {
        win,
        pin,
        cred,
        target,
        opened: rec.opened,
        decisions,
        blocked,
        lifetime,
        admission: admitted,
        closing,
    };
}
