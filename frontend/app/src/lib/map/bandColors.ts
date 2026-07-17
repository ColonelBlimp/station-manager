/**
 * Band → arc colour for the contacts map. Spectrum-ordered mnemonic: the
 * longest wavelength (160m) sits at the red end and the palette walks the
 * rainbow toward violet/pink as the bands shorten — so adjacent bands are
 * adjacent hues and the common HF pairs (40/20/15/10) stay far apart.
 * Mid-lightness picks (Tailwind 500-step) keep every colour legible on
 * both basemaps (white light-mode ocean, near-black dark-mode night).
 *
 * Operator overrides (config `map.band_colors`, slice 2 of the map-colour
 * work) layer over these defaults per band; unknown bands fall back to a
 * neutral gray so a QSO never disappears for want of a palette entry.
 */

export const FALLBACK_BAND_COLOR = '#6b7280'; // gray-500

export const DEFAULT_BAND_COLORS: Record<string, string> = {
    '160m': '#ef4444', // red
    '80m': '#f97316', // orange
    '60m': '#f59e0b', // amber
    '40m': '#eab308', // yellow
    '30m': '#84cc16', // lime
    '20m': '#22c55e', // green
    '17m': '#14b8a6', // teal
    '15m': '#06b6d4', // cyan
    '12m': '#3b82f6', // blue
    '10m': '#6366f1', // indigo
    '6m': '#8b5cf6', // violet
    '4m': '#a855f7', // purple
    '2m': '#d946ef', // fuchsia
    '70cm': '#ec4899', // pink
};

/** Canonical ADIF band token: trimmed, lowercase ("20M " → "20m"). */
export function normalizeBand(band?: string): string {
    return (band ?? '').trim().toLowerCase();
}

/** Arc colour for a band: operator override → default palette → gray. */
export function bandColor(band?: string, overrides?: Record<string, string>): string {
    const key = normalizeBand(band);
    return overrides?.[key] ?? DEFAULT_BAND_COLORS[key] ?? FALLBACK_BAND_COLOR;
}

/** Legend sort rank: palette (wavelength) order first, unknown bands last. */
export function bandRank(band?: string): number {
    const i = Object.keys(DEFAULT_BAND_COLORS).indexOf(normalizeBand(band));
    return i === -1 ? Number.MAX_SAFE_INTEGER : i;
}
