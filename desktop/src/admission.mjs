// The app lifetime and its admission (ADR 0102 ruling 6; ADR 0100 §3; W-0027 C4a).
//
// At each start the main process makes one random identity, the app lifetime. It is a label,
// not a secret; the credential is what authenticates. It lives here, in the main process, so a
// renderer that reloads or crashes keeps it and a restart of the app makes a new one. It is
// sent on every request to the pinned origin, beside the credential (credential.mjs).
//
// Before the workspace is loaded the shell is admitted: it reads smd's process marker and
// admission generation, and admits against what it read. smd then stops whatever an earlier
// start of the app left running, so a new start never finds TX armed or a run in progress.
//   - stale (another admission landed between the read and the admit): read again, admit again.
//     That is how the running app gets past an admission a dead app had already sent. Each
//     repeat follows a definite answer; there is no timer.
//   - retired (this lifetime has been replaced): the attempt ends, and is never made again.
//   - anything else: failed; the caller shows the waiting page and tries again at its pace.
//
// Each smd process is admitted to afresh. The shell keeps its lifetime across a daemon restart
// and learns of the new process from the marker smd puts on its responses, read when the
// headers arrive (a stream may never end). A different marker is a reason to ask, not proof: a
// late response from the old process can arrive after the new one has been admitted to. So the
// shell then reads, and the read decides.
//
// Beside the marker rides the admission generation. If another start of the app replaces this
// one, the daemon is the same process and its marker is the same; the generation is what
// changes. The shell asks then too, and smd's answer is that it is retired (codex-129,
// finding 2). It never becomes a fresh attempt to take the station back: a replaced lifetime
// is refused for good.
import { randomBytes } from 'node:crypto';
import { onOrigin, redirectTarget, redirectsOffOrigin } from './hardening.mjs';

export const LIFETIME_HEADER = 'X-SM-App-Lifetime';
export const MARKER_HEADER = 'x-sm-daemon-instance';
export const GENERATION_HEADER = 'x-sm-admission-generation';
export const ADMISSION_PATH = '/v1/app/lifetime';

const TIMEOUT_MS = 5000;

/** A new app lifetime: 16 random bytes, base64url. */
export const newLifetime = () => randomBytes(16).toString('base64url');

/** True for the shell's own admission requests, whose responses must start nothing. */
export function isAdmissionRequest(url) {
    try {
        return new URL(url).pathname === ADMISSION_PATH;
    } catch {
        return false;
    }
}

async function body(res) {
    try {
        return await res.json();
    } catch {
        return null;
    }
}

/**
 * Admit this app's lifetime to the smd at origin, through the window's session, so the request
 * carries the credential and the lifetime and is held to the pin. Resolves with
 * { state, instance, generation }: state is 'admitted', 'retired' or 'failed'; instance is smd's process marker when it answered, and generation the
 * admission generation this lifetime is current at. Never throws.
 */
export async function admit(ses, origin) {
    const url = origin + ADMISSION_PATH;
    const opts = () => ({ redirect: 'manual', signal: AbortSignal.timeout(TIMEOUT_MS) });
    try {
        for (;;) {
            const read = await ses.fetch(url, opts());
            if (read.status !== 200) return { state: 'failed' };
            const st = await body(read);
            if (typeof st?.daemon_instance !== 'string') return { state: 'failed' };
            const instance = st.daemon_instance;
            if (!Number.isSafeInteger(st.generation)) return { state: 'failed' };
            if (st.current === true)
                return { state: 'admitted', instance, generation: st.generation };

            const res = await ses.fetch(url, {
                ...opts(),
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ daemon_instance: instance, generation: st.generation }),
            });
            if (res.status === 200) {
                // The generation after the admit: the one read, plus the replacement just made.
                const now = (await body(res))?.generation;
                return {
                    state: 'admitted',
                    instance,
                    generation: Number.isSafeInteger(now) ? now : st.generation + 1,
                };
            }
            const code = res.status === 409 ? (await body(res))?.code : '';
            if (code === 'app_lifetime_retired') return { state: 'retired', instance };
            if (code !== 'app_admission_stale') return { state: 'failed' };
        }
    } catch {
        return { state: 'failed' };
    }
}

/**
 * The shell's admission, held for the app's lifetime. `admitNow()` admits and remembers the
 * process and generation it was admitted at; calls made while one is in progress join it. Once
 * retired it stays retired and asks no more. While a close is under way it asks nothing
 * (beginClosing). `sawMarker(marker, generation)` is told what each response carried. `onRetired` is called once, and only when the retirement is learnt in a
 * loaded workspace (through a response): at start the waiting page says it instead.
 */
export function admission({ ses, origin, admitFn = admit, onRetired }) {
    let instance = null; // the smd process this lifetime was last admitted to
    let generation = null; // ...and the admission generation it is current at, as a string
    let retired = false;
    let told = false;
    let pending = null;
    let asked = false; // an admission was asked for at least once (set before any is sent)
    let closing = false; // the barrier of a confirmed close (beginClosing)
    let owedRetired = false; // a retirement learnt in a loaded workspace while closing
    // Everything responses carried while an admission was in progress. Joining that work must
    // not lose it (codex-129, finding 1): it is asked about when the work settles. All of it is
    // kept, not the last: the order responses arrive in is not the order of smd's processes,
    // so a late response from an older one must not hide a newer one seen before it
    // (codex-130). Only smd's own answer settles what was seen.
    const seenMeanwhile = new Map();

    const differs = (seen) =>
        seen.marker !== instance ||
        (seen.generation !== undefined && generation !== null && seen.generation !== generation);
    const settle = (r) => {
        if (r.state === 'retired') retired = true;
        if (r.state === 'admitted') {
            instance = r.instance;
            generation = Number.isSafeInteger(r.generation) ? String(r.generation) : null;
        }
        return r;
    };
    const admitNow = () => {
        if (retired) return Promise.resolve({ state: 'retired', instance });
        // A closing app launches nothing. The caller is told what it is told when smd cannot
        // be asked: the waiting page's loop waits and comes back.
        if (closing) return Promise.resolve({ state: 'failed' });
        asked = true;
        pending ??= Promise.resolve()
            .then(() => admitFn(ses, origin()))
            .then(settle, () => ({ state: 'failed' }))
            .finally(() => {
                pending = null;
                // Closing: the markers are kept for a cancelled close (endClosing), and
                // start nothing now.
                if (closing) return;
                const seen = [...seenMeanwhile.values()];
                seenMeanwhile.clear();
                if (!retired && instance !== null && seen.some(differs)) ask();
            });
        return pending;
    };
    /** Ask smd, from a loaded workspace: its answer decides, and a retirement is said once. */
    const ask = () => {
        void admitNow().then((r) => {
            if (r.state !== 'retired' || told) return;
            // Not while closing: the close knows (isRetired), and the operator is told only
            // if the close is cancelled and the window comes back.
            if (closing) {
                owedRetired = true;
                return;
            }
            told = true;
            onRetired?.();
        });
    };
    return {
        admitNow,
        isRetired: () => retired,
        /** Whether an admission was ever asked for. It turns true when one is asked for,
         *  before anything is sent, so it can say "asked" of a request that never left; it
         *  never says "not asked" of one that did. A lifetime never asked for is one smd has
         *  never heard of: it holds no session and can have armed nothing. */
        everAsked: () => asked,
        isClosing: () => closing,
        /**
         * The barrier (W-0027 A3, section D): from the moment a close is confirmed, this
         * lifetime is never admitted again. No new admission is launched, by a marker, by
         * the waiting page's loop or by the loop above; markers are kept and start nothing.
         * Resolves when admission work already issued has settled here. That is not proof
         * smd will not still apply it (a request given up on may land later): what makes a
         * late admission safe is smd's own order, not this wait.
         */
        beginClosing() {
            closing = true;
            return (pending ?? Promise.resolve()).then(() => undefined);
        },
        /** A close that was cancelled before any exit report was sent: ordinary admission
         *  resumes. What was seen while closing is asked about now, and a retirement learnt
         *  meanwhile is said, once. */
        endClosing() {
            if (!closing) return;
            closing = false;
            if (retired) {
                if (owedRetired && !told) {
                    told = true;
                    onRetired?.();
                }
                return;
            }
            const seen = [...seenMeanwhile.values()];
            seenMeanwhile.clear();
            if (instance !== null && seen.some(differs)) ask();
        },
        /** What a response carried. A marker or a generation other than the one admitted at:
         *  ask smd, and let its answer decide. */
        sawMarker(marker, seenGeneration) {
            if (retired || instance === null || typeof marker !== 'string' || marker === '') return;
            const seen = {
                marker,
                generation: typeof seenGeneration === 'string' ? seenGeneration : undefined,
            };
            if (pending || closing) {
                seenMeanwhile.set(`${seen.marker}\n${seen.generation ?? ''}`, seen);
                return;
            }
            if (differs(seen)) ask();
        },
    };
}

/**
 * Watch smd's responses. Electron keeps one listener per webRequest event: this is the only
 * owner of onHeadersReceived, so both of its jobs live here, and each response is answered
 * exactly once.
 *
 * The markers: read from responses of the pinned origin, never from the admission's own
 * requests, so admitting starts no further asking.
 *
 * Redirects: one that would leave the pinned origin is cancelled here, before it is followed
 * (hardening.mjs, redirectsOffOrigin), and `onRedirectRefused(from, to)` is told. That is the
 * one response this changes; every other is passed as it came. The marker on a refused
 * redirect is still read: it came from smd.
 */
export function watchDaemon(ses, { origin, onMarker, onRedirectRefused }) {
    ses.webRequest.onHeadersReceived((details, callback) => {
        const o = origin();
        if (redirectsOffOrigin(details, o)) {
            callback({ cancel: true });
            onRedirectRefused?.(details.url, redirectTarget(details));
        } else {
            callback({});
        }
        if (!o || !onOrigin(details.url, o) || isAdmissionRequest(details.url)) return;
        let marker;
        let generation;
        for (const [k, v] of Object.entries(details.responseHeaders ?? {})) {
            const value = Array.isArray(v) ? v[0] : v;
            if (k.toLowerCase() === MARKER_HEADER) marker = value;
            if (k.toLowerCase() === GENERATION_HEADER) generation = value;
        }
        if (marker !== undefined) onMarker(marker, generation);
    });
}
