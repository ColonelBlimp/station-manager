/**
 * Country-code → flag-emoji helper.
 *
 * A flag emoji is the two ISO-3166-1 alpha-2 letters expressed as Unicode
 * regional-indicator symbols (U+1F1E6…U+1F1FF), so a valid two-letter code maps
 * to a flag with zero dependencies and no glyph table. The daemon's enrichment
 * country source-of-truth is hamnut (ADR 0017), whose `countryCode` populates
 * `Country.ccode` as an alpha-2 code — the value this consumes.
 *
 * Guarded on purpose: anything that isn't exactly two ASCII letters (empty,
 * a DXCC number from a non-hamnut source, junk) returns '' so the caller simply
 * renders no flag rather than a broken glyph — the "enrichment degrades, never
 * breaks" posture extended to display.
 */
export function ccodeToFlag(ccode: string | null | undefined): string {
    if (!ccode) return '';
    const cc = ccode.trim().toUpperCase();
    if (!/^[A-Z]{2}$/.test(cc)) return '';
    const ri = 0x1f1e6; // 🇦 — regional indicator 'A'
    const a = 'A'.charCodeAt(0);
    return String.fromCodePoint(ri + (cc.charCodeAt(0) - a), ri + (cc.charCodeAt(1) - a));
}
