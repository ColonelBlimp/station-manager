// Launching an app window against smd's own listener (ADR 0103; W-0027 C1d).
//
// The order is the guarantee: the verifier (an empty pin, so it rejects everything), the
// session hardening (no origin, so nothing is allowed on the network) and the credential header
// (no credential, so none is sent; W-0027 C2b) are installed before the window exists; only then
// does discovery run, and the pin, the origin and the credential are fixed together at the first
// valid identity and credential (src/daemon.mjs). The interactive app and the real-smd acceptance
// (test/scenario-c1d.mjs) launch through this one function.
import { session } from 'electron';
import { installCredential, onceCredential, watchRefusal } from './credential.mjs';
import { connectWhenTrusted } from './daemon.mjs';
import { recordingFs } from './discovery.mjs';
import { hardenSession } from './hardening.mjs';
import { installPin, oncePin } from './pin.mjs';
import { credentialRefused, isShellProbe } from './waiting.mjs';
import { createMainWindow } from './window.mjs';

/**
 * Launch one window. `partition` gives it its own session (its own verifier, pin and origin),
 * which only a test of a second, freshly started window needs. `onCredentialRefused` is called
 * once if smd refuses the credential in a loaded workspace; `onCredentialRequest` sees, for
 * tests, each request and whether the credential went with it. Returns the window and, for
 * tests, the pin, the credential holder, the target origin, every file the discovery opened,
 * the verifier's decisions and the requests the filter blocked.
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
}) {
    const ses = partition ? session.fromPartition(partition) : session.defaultSession;
    const pin = oncePin();
    const cred = onceCredential();
    const target = { origin: null };
    const rec = recordingFs();
    const decisions = [];
    const blocked = [];
    installPin(ses, { fingerprint: pin.get, onDecision: (d) => decisions.push(d) });
    hardenSession(ses, {
        origin: () => target.origin,
        onBlocked: (url) => {
            blocked.push(url);
            console.error(`shell: blocked a request outside the served origin: ${url}`);
        },
        onPermission,
    });
    installCredential(ses, {
        origin: () => target.origin,
        credential: cred.get,
        onRequest: onCredentialRequest,
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
    void connectWhenTrusted({ win, pin, cred, target, args, env, fsb: rec.fs, retryMs });
    return { win, pin, cred, target, opened: rec.opened, decisions, blocked };
}
