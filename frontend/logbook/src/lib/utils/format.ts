/* Small display formatters for the logbook table. ADIF-native values come off the
   wire as strings (date YYYYMMDD, time HHMM[SS], freq in MHz). Kept pure + tiny. */

/** "20260625" → "2026-06-25"; passes through anything not 8 digits. */
export function formatQsoDate(d: string | undefined): string {
    if (!d || d.length !== 8) return d ?? '';
    return `${d.slice(0, 4)}-${d.slice(4, 6)}-${d.slice(6, 8)}`;
}

/** "0448" / "044812" → "04:48"; passes through anything shorter than 4 digits. */
export function formatTime(t: string | undefined): string {
    if (!t || t.length < 4) return t ?? '';
    return `${t.slice(0, 2)}:${t.slice(2, 4)}`;
}

/** freq is already ADIF MHz (e.g. "14.074") — show as-is, trimmed. */
export function formatFreq(freq: string | undefined): string {
    return (freq ?? '').trim();
}

/** MODE, with SUBMODE appended in parens when it adds detail (e.g. "MFSK (FT4)"). */
export function formatMode(mode: string | undefined, submode: string | undefined): string {
    const m = (mode ?? '').trim();
    const s = (submode ?? '').trim();
    if (!s || s === m) return m;
    return `${m} (${s})`;
}
