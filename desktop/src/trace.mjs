// The start-up trace (W-0027 D1): one stderr line when the shell's state changes, so a launch
// from the desktop menu leaves an actionable record, and the production smoke check can follow
// the shell. Only bounded labels, paths, origins and reasons: never config contents, page
// contents or a data: URL. A diagnostic, not proof on its own (codex-075).
import { onOrigin } from './hardening.mjs';
import { isWaitingPage } from './waiting.mjs';

const PREFIX = 'station-manager-desktop: ';

const escaped = (text) =>
    String(text).replace(
        /[\x00-\x1f\x7f]/g, // eslint-disable-line no-control-regex -- matching them is the point
        (c) => `\\x${c.charCodeAt(0).toString(16).padStart(2, '0')}`
    );

/** One line: control characters (a newline in a path) are escaped, so nothing can forge one. */
export function traceLine(text) {
    return `${PREFIX}${escaped(text)}\n`;
}

export const trace = (text) => void process.stderr.write(traceLine(text));

/** A bounded label for a window's URL: the waiting page only for an exact document the shell
 *  generated (codex-077), never inferred from the data: scheme. */
export function pageLabel(url, origin) {
    if (isWaitingPage(url)) return 'waiting page';
    if (origin && onOrigin(url, origin)) return 'app';
    return 'other';
}

/** Trace a window's finished loads (from did-finish-load itself, with the pid of that same
 *  contents), its main-frame load failures, and a lost renderer. A load superseded or stopped
 *  (-3, the shell replacing its own page) is not a failure. */
export function traceWindow(wc, originOf, emit = trace) {
    wc.on('did-finish-load', () => {
        emit(`loaded ${pageLabel(wc.getURL(), originOf())} renderer ${wc.getOSProcessId()}`);
    });
    wc.on('did-fail-load', (_e, code, _desc, url, isMainFrame) => {
        if (!isMainFrame || code === -3) return;
        emit(`load failed ${pageLabel(url, originOf())} ${code}`);
    });
    wc.on('render-process-gone', (_e, details) => emit(`renderer gone ${details?.reason}`));
}

const LABEL_MAX = 120;

/**
 * A bounded label for an address in a diagnostic: its scheme, host and port. The user-info,
 * path, query and fragment, each of which can carry data, are left out. The host is kept,
 * because it is what says which address was refused; that is a choice about what the line
 * is for, not a promise that a host name can never be made to carry something. `base` is
 * the address a relative one came with. An address with no host is its scheme alone, and
 * what does not parse is a fixed label. Bounded in length and held to one line.
 */
export function addressLabel(url, base) {
    let u;
    try {
        u = base === undefined ? new URL(url) : new URL(url, base);
    } catch {
        return '[not an address]';
    }
    const label = u.host ? `${u.protocol}//${u.host}` : u.protocol;
    return escaped(label).slice(0, LABEL_MAX);
}

/** The line for a request the shell cancelled because it was not for the served origin. */
export const blockedRequestLine = (url) =>
    `shell: blocked a request outside the served origin: ${addressLabel(url)}`;

/** The line for a write the page attempted while the app was closing (W-0027 A3). The method
 *  only: the address of a write can carry data. */
export const refusedWriteLine = (method) =>
    `shell: refused a write from the page while closing: ${escaped(method).slice(0, 16)}`;

/** The line for a redirect the shell refused: the origin it came from and the one it named.
 *  `to` is the response's one Location, or '' when it had none or several. */
export const refusedRedirectLine = (from, to) =>
    `shell: refused a redirect off the served origin: ${addressLabel(from)} -> ` +
    (to === '' ? '[no single address]' : addressLabel(to, from));
