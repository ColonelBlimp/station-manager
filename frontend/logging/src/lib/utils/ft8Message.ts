/**
 * FT8 message parsing helpers for the Band Activity feed.
 *
 * Only CQ messages are parsed today — a CQ line carries exactly one callsign
 * (the calling station), so there is no "which of the two calls do I flag?"
 * ambiguity that a worked-someone-else exchange (`K1ABC W2XYZ -10`) has. That
 * single, unambiguous case is the first enrichment slice (flag + worked-before
 * highlight); reply/report messages are a later extension.
 */

// A 4-character Maidenhead grid (FN42, IO91). FT8 CQ messages end with the
// caller's grid, which — like a callsign — contains digits, so it must be
// excluded explicitly when scanning for the call.
const GRID4 = /^[A-R]{2}[0-9]{2}$/;

// A callsign token: letters, digits and '/' (compound calls like G3ABC/P),
// at least three characters, with at least one letter AND one digit. The
// digit requirement is what separates a call from CQ modifiers (DX, EU, POTA,
// TEST); the grid check below removes the one digit-bearing non-call token.
const CALL = /^[A-Z0-9/]{3,}$/;

function looksLikeCall(t: string): boolean {
    if (!CALL.test(t)) return false;
    if (!/[0-9]/.test(t)) return false; // a callsign has a digit
    if (!/[A-Z]/.test(t)) return false; // …and a letter (rejects "030" directed-CQ tokens)
    if (GRID4.test(t)) return false; // a grid is not a call
    return true;
}

/**
 * parseCqCall returns the calling station's callsign from a CQ decode, or null
 * if the line is not a CQ (or carries no recognisable callsign).
 *
 * Handles the standard shapes — `CQ <call> <grid>`, `CQ DX <call> <grid>`,
 * directional `CQ EU <call>`, contest/activity `CQ POTA <call>`, and directed
 * `CQ 030 <call>` — by skipping every leading token that isn't a callsign and
 * taking the first one that is. A line whose call has no digit (rare special
 * events) simply returns null and renders undecorated — degrade, never break.
 */
export function parseCqCall(text: string): string | null {
    const toks = text.trim().toUpperCase().split(/\s+/);
    if (toks.length < 2 || toks[0] !== 'CQ') return null;
    for (let i = 1; i < toks.length; i++) {
        if (looksLikeCall(toks[i])) return toks[i];
    }
    return null;
}

/**
 * isCqFd reports whether a CQ decode is an ARRL Field Day call — `CQ FD <call> <grid>`
 * (the `FD` modifier appears before the callsign). Used to route the answer to the FD
 * exchange (class/section) instead of the standard grid/report one. Mirrors the
 * daemon's parseMessage FD detection so browser and daemon agree.
 */
export function isCqFd(text: string): boolean {
    const toks = text.trim().toUpperCase().split(/\s+/);
    if (toks.length < 2 || toks[0] !== 'CQ') return false;
    for (let i = 1; i < toks.length; i++) {
        if (toks[i] === 'FD') return true;
        if (looksLikeCall(toks[i])) return false; // reached the call without an FD modifier
    }
    return false;
}

/**
 * parseCq returns the calling station's callsign AND grid from a CQ decode, or
 * null if the line is not an answerable CQ. The grid is the 4-char Maidenhead
 * token immediately after the call when present (`CQ K1ABC FN42` → FN42), else
 * '' (`CQ EU G3XYZ`). Used to initiate an answer-a-CQ exchange (the grid is
 * carried for the logged QSO; an absent grid is fine).
 */
export function parseCq(text: string): { call: string; grid: string } | null {
    const toks = text.trim().toUpperCase().split(/\s+/);
    if (toks.length < 2 || toks[0] !== 'CQ') return null;
    for (let i = 1; i < toks.length; i++) {
        if (looksLikeCall(toks[i])) {
            const grid = i + 1 < toks.length && GRID4.test(toks[i + 1]) ? toks[i + 1] : '';
            return { call: toks[i], grid };
        }
    }
    return null;
}

/**
 * parseDirectedToMe returns the calling station's callsign AND grid from a decode
 * that is a station CALLING US — the opening of a contact, `<myCall> <theirCall>
 * <grid>` (e.g. `7Q5MLV PA3KUS JO21`) — or null otherwise. This is the pile-up
 * signal: stations answering us / tail-ending while we're on frequency. Used to
 * make those lines clickable to work the caller (ADR 0033 "work a caller").
 *
 * It matches ONLY the grid-bearing opening, which is unambiguous — deliberately NOT
 * the mid-exchange replies addressed to us (`<myCall> <theirCall> R-12` / `RR73` /
 * `73`), which are part of an in-progress QSO, not a fresh caller to pick up. The
 * caller's grid is carried for the logged QSO. myCall is compared case-insensitively;
 * a blank myCall (no station callsign configured) never matches.
 */
export function parseDirectedToMe(
    text: string,
    myCall: string
): { call: string; grid: string } | null {
    const me = myCall.trim().toUpperCase();
    if (me === '') return null;
    const toks = text.trim().toUpperCase().split(/\s+/);
    // <me> <them> <grid> — exactly the opening shape; the third token must be a grid.
    if (toks.length < 3) return null;
    if (toks[0] !== me) return null;
    if (!looksLikeCall(toks[1])) return null;
    // RR73 satisfies GRID4 (R,R ∈ A–R; 7,3 ∈ 0–9) but is a roger token, not a grid —
    // the well-known FT8 Maidenhead collision. Exclude it so a roger addressed to us
    // ("<me> <them> RR73", an in-progress QSO) isn't misread as a fresh caller. RRR /
    // 73 / R-report don't match GRID4, so RR73 is the only token that needs excluding.
    if (toks[2] === 'RR73' || !GRID4.test(toks[2])) return null;
    return { call: toks[1], grid: toks[2] };
}

const FD_CLASS = /^[0-9]{1,2}[A-F]$/;
const FD_SECTION = /^[A-Z]{2,4}$/;

/**
 * parseDirectedToMeFd returns a station CALLING US with an ARRL Field Day exchange —
 * `<myCall> <theirCall> <class> <section>` (e.g. `7Q5MLV K7IOC 1D WWA`) — or null. The
 * FD twin of parseDirectedToMe: a Field Day caller sends their class+section instead of
 * a grid, so we work them with the FD exchange. `grid` is '' (an FD call carries no
 * grid). The class/section shapes are loose guards (the daemon owns the canonical
 * section list); the `<call> <2-digit+A–F> <2–4 letters>` shape is FD-specific enough
 * that ordinary traffic won't match.
 */
export function parseDirectedToMeFd(
    text: string,
    myCall: string
): { call: string; grid: string; class: string; section: string } | null {
    const me = myCall.trim().toUpperCase();
    if (me === '') return null;
    const toks = text.trim().toUpperCase().split(/\s+/);
    if (toks.length < 4) return null;
    if (toks[0] !== me) return null;
    if (!looksLikeCall(toks[1])) return null;
    if (!FD_CLASS.test(toks[2]) || !FD_SECTION.test(toks[3])) return null;
    return { call: toks[1], grid: '', class: toks[2], section: toks[3] };
}
