// The start-up trace (W-0027 D1): one stderr line when the shell's state changes, so a launch
// from the desktop menu leaves an actionable record, and the production smoke check can follow
// the shell. Only bounded labels, paths, origins and reasons: never config contents, page
// contents or a data: URL. A diagnostic, not proof on its own (codex-075).
import { onOrigin } from './hardening.mjs';
import { isWaitingPage } from './waiting.mjs';

const PREFIX = 'station-manager-desktop: ';

/** One line: control characters (a newline in a path) are escaped, so nothing can forge one. */
export function traceLine(text) {
    const safe = String(text).replace(
        /[\x00-\x1f\x7f]/g, // eslint-disable-line no-control-regex -- matching them is the point
        (c) => `\\x${c.charCodeAt(0).toString(16).padStart(2, '0')}`
    );
    return `${PREFIX}${safe}\n`;
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
