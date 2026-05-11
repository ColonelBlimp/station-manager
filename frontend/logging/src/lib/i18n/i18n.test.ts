import { describe, it, expect, afterEach, vi } from 'vitest';
import { t, setLocale, getLocale } from './index';

describe('i18n render helper', () => {
    afterEach(() => {
        // Reset to English baseline between tests so cross-test
        // state doesn't bleed.
        setLocale('en');
    });

    it('renders a known key from the English catalogue', () => {
        const out = t('bridge.disconnected.rig_no_data');
        expect(out).toContain('rig');
        // Anchor on a stable substring rather than the exact wording
        // so the operator can retune `en.ts` without breaking tests.
        expect(out.startsWith('[missing:')).toBe(false);
    });

    it('substitutes named placeholders from the details map', () => {
        const out = t('bridge.error.unknown_driver', { driver: 'yaesu-foo' });
        expect(out).toContain('yaesu-foo');
        expect(out).not.toContain('{driver}');
    });

    it('leaves unsupplied placeholders intact (visible at runtime)', () => {
        const out = t('bridge.error.serial_open_failed', { port: '/dev/ttyUSB0' });
        // error not supplied, port is — port substitutes, error stays
        // as the literal placeholder so the gap is visible.
        expect(out).toContain('/dev/ttyUSB0');
        expect(out).toContain('{error}');
    });

    it('returns a [missing: key] sentinel for unknown keys', () => {
        const out = t('this.does.not.exist');
        expect(out).toBe('[missing: this.does.not.exist]');
    });

    it('setLocale switches the active locale when registered', () => {
        setLocale('en'); // baseline, registered
        expect(getLocale()).toBe('en');
    });

    it('setLocale stays put + warns when the locale is not registered', () => {
        const before = getLocale();
        const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
        setLocale('zz-not-real');
        expect(getLocale()).toBe(before);
        expect(warnSpy).toHaveBeenCalled();
        warnSpy.mockRestore();
    });
});

// Note for future: when Tumbuka / Chichewa catalogues land, add
// per-locale tests pinning that a partial catalogue falls back to
// the English baseline for missing keys. The fallback chain is
// already implemented in t(); the test just makes sure it stays
// implemented.
