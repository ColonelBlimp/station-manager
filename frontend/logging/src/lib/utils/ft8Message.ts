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
