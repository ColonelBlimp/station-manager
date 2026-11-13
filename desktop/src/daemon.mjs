// Connecting the shell to smd's own listener (ADR 0103 point 3; W-0027 C1d).
//
// Until the first valid identity, the shell knows no origin and no pin: the verifier rejects
// everything, the request filter allows nothing on the network, and the window shows the local
// waiting page with the discovery's state, path and reason (operator ruling Q2). At the first
// valid read, the pin, the origin and the operating credential (W-0027 C2b) are fixed together,
// once, for the app's lifetime; neither file is read again, and a later TLS failure or refusal
// never re-pins or re-reads. From then on the pinned probe loads the application when smd
// answers.
import { onceCredential } from './credential.mjs';
import { configPath, discover, recordingFs } from './discovery.mjs';
import { trace as stderrTrace } from './trace.mjs';
import { loadWhenUp, returnToWaitingOnFailure, waitingPageUrl } from './waiting.mjs';

const RETRY_MS = 1000;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** The shell's own options: --sm-config <path> and --sm-data-dir <path> (or --x=path). A named
 *  option with a missing or empty value, or followed by another option, is an error, never
 *  "no override" (codex-068): it must not silently select the default install. */
export function parseArgs(argv) {
    const out = {};
    const names = { '--sm-config': 'config', '--sm-data-dir': 'dataDir' };
    for (let i = 0; i < argv.length; i++) {
        const [flag, inline] = argv[i].split(/=(.*)/s, 2);
        if (!(flag in names)) continue;
        const value = inline !== undefined ? inline : argv[++i];
        if (value === undefined || value === '' || value.startsWith('--')) {
            return { error: `${flag} needs a value: --sm-config <path> or --sm-config=<path>` };
        }
        out[names[flag]] = value;
    }
    return out;
}

/**
 * Discover smd's identity and credential and, at the first valid read, fix the pin, the origin and
 * the credential and load the application. `pin` is a oncePin() and `cred` a onceCredential();
 * `target.origin` is read by the session's request filter, its credential header and the window's
 * navigation guards. Resolves with the discovery result once fixed, or undefined if the window
 * closes first.
 */
export async function connectWhenTrusted({
    win,
    pin,
    cred = onceCredential(),
    target,
    args = {},
    env = process.env,
    fsb = recordingFs().fs,
    retryMs = RETRY_MS,
    onDetail,
    trace = stderrTrace,
}) {
    let shown = '';
    if (args.error) {
        // A malformed option is the operator's to correct: shown locally, with no discovery
        // and no network, until the app is started again.
        trace(`refused: ${args.error}`);
        void win
            .loadURL(waitingPageUrl(null, { state: 'refused', path: '', reason: args.error }))
            .catch(() => {});
        while (!win.isDestroyed()) await sleep(retryMs);
        return undefined;
    }
    const where = configPath({ explicit: args.config, env });
    while (!win.isDestroyed()) {
        const r = where.ok
            ? discover({ configFile: where.path, explicitDataDir: args.dataDir }, fsb)
            : { ok: false, state: 'refused', path: args.config ?? '', reason: where.reason };
        if (r.ok) {
            // One synchronous step, so no request can see one fixed without the others.
            pin.set(r.fingerprint);
            cred.set(r.credential.reveal());
            target.origin = r.origin;
            trace(`pinned ${r.origin}`);
            const appUrl = r.origin + '/';
            // The local explanation is now obsolete: show the ordinary waiting page for the
            // pinned daemon, once, before the probe (which keeps an existing local page).
            void win.loadURL(waitingPageUrl(r.origin)).catch(() => {});
            returnToWaitingOnFailure(win, appUrl, r.origin);
            await loadWhenUp(win, appUrl, r.origin);
            return r;
        }
        onDetail?.(r);
        const key = `${r.state}\n${r.path}\n${r.reason}`;
        if (key !== shown) {
            shown = key;
            trace(`${r.state}: ${r.reason} ${r.path}`);
            void win.loadURL(waitingPageUrl(null, r)).catch(() => {});
        }
        await sleep(retryMs);
    }
    return undefined;
}
