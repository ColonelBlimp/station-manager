// Launching an app window against smd's own listener (ADR 0103; W-0027 C1d).
//
// The order is the guarantee: the verifier (an empty pin, so it rejects everything) and the
// session hardening (no origin, so nothing is allowed on the network) are installed before the
// window exists; only then does discovery run, and the pin and origin are fixed together at the
// first valid identity (src/daemon.mjs). The interactive app and the real-smd acceptance
// (test/scenario-c1d.mjs) launch through this one function.
import { session } from 'electron';
import { connectWhenTrusted } from './daemon.mjs';
import { recordingFs } from './discovery.mjs';
import { hardenSession } from './hardening.mjs';
import { installPin, oncePin } from './pin.mjs';
import { createMainWindow } from './window.mjs';

/**
 * Launch one window. `partition` gives it its own session (its own verifier, pin and origin),
 * which only a test of a second, freshly started window needs. Returns the window and, for
 * tests, the pin, the target origin, every file the discovery opened, the verifier's decisions
 * and the requests the filter blocked.
 */
export function launchTrusted({
    env = process.env,
    args = {},
    offscreen = false,
    openExternal,
    partition,
    retryMs,
    onPermission,
}) {
    const ses = partition ? session.fromPartition(partition) : session.defaultSession;
    const pin = oncePin();
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
    const win = createMainWindow({
        origin: () => target.origin,
        offscreen,
        openExternal,
        partition,
    });
    void connectWhenTrusted({ win, pin, target, args, env, fsb: rec.fs, retryMs });
    return { win, pin, target, opened: rec.opened, decisions, blocked };
}
