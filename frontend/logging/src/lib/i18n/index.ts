/**
 * SPA-side i18n: locale-aware operator-facing string rendering.
 *
 * Single-locale (English) at the moment; structured so adding
 * Tumbuka / Chichewa later is a matter of dropping a new
 * `<locale>.ts` catalogue and adding it to the registry below.
 * No external dependency — the SPA's string surface is small and
 * doesn't need plurals or locale-aware date/number formatting, so
 * a minimal in-house substitute earns its keep over pulling in
 * `svelte-i18n` / `i18next`.
 *
 * **Render contract:**
 *   t('bridge.error.unknown_driver', { driver: 'yaesu-foo' })
 *     → 'CAT driver "yaesu-foo" is not recognised — check bridge.cat.driver in config.json'
 *
 * **Fallback chain** (resolved per render call):
 *   1. current locale's catalogue
 *   2. English baseline (always the master — every key must exist)
 *   3. `[missing: <key>]` sentinel so dev-time gaps are visible
 *
 * **Placeholder format:** `{name}` substituted from the details map.
 * Names that the details map doesn't supply leave the placeholder
 * intact (e.g. `{driver}`) so a missing detail is also visible at
 * runtime rather than collapsing silently.
 *
 * **Locale persistence:** the catalogue selector lives in
 * `configState.station` (planned — wire it up when Tumbuka /
 * Chichewa ship). For now `currentLocale` is hardcoded to 'en'.
 */

import { en } from './en';

type Catalogue = Record<string, string>;

// Registered catalogues. Add new languages by importing them and
// adding to this map; the render helper will pick them up
// automatically (subject to `currentLocale` being changed).
const catalogues: Record<string, Catalogue> = { en };

let currentLocale = 'en';

/**
 * Switch the active locale. No-op (with console warning) when the
 * requested locale has no catalogue registered — guards against typos
 * and against config drift between SPA + persisted operator settings.
 */
export function setLocale(locale: string): void {
    if (catalogues[locale]) {
        currentLocale = locale;
    } else {
        console.warn(`[i18n] unknown locale "${locale}"; staying on "${currentLocale}"`);
    }
}

/** Active locale code (test/inspection only). */
export function getLocale(): string {
    return currentLocale;
}

/**
 * Render a localized string for the given key, substituting any
 * `{placeholder}` tokens from the details map.
 *
 * Resolution: current locale → English baseline → `[missing: key]`.
 */
export function t(key: string, details?: Record<string, string>): string {
    const template =
        catalogues[currentLocale]?.[key] ?? catalogues.en[key] ?? `[missing: ${key}]`;
    if (!details) return template;
    // `\w+` is [A-Za-z0-9_], so hyphens and other punctuation in
    // placeholder names won't match — `{client-id}` would render
    // literally instead of substituting. All current daemon
    // `details` keys are snake_case (`{driver}`, `{error}`,
    // `{port}`), so this is fine today; widen the character class
    // if a future event ever ships hyphenated keys.
    return template.replace(/{(\w+)}/g, (_match, name: string) =>
        details[name] !== undefined ? details[name] : `{${name}}`,
    );
}
