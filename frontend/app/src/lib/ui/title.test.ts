import { describe, it, expect } from 'vitest';
import { computeTitle } from './title';

describe('computeTitle', () => {
    it('release form: view · product, no marker', () => {
        expect(computeTitle('Operate', false)).toBe('Operate · Station Manager');
        expect(computeTitle('Logbook', false)).toBe('Logbook · Station Manager');
    });

    it('dev form: a DEV prefix ahead of the view (survives right-truncation)', () => {
        expect(computeTitle('Operate', true)).toBe('DEV · Operate · Station Manager');
    });

    it('the full-window Map view follows the same rule', () => {
        expect(computeTitle('Contacts Map', true)).toBe('DEV · Contacts Map · Station Manager');
        expect(computeTitle('Contacts Map', false)).toBe('Contacts Map · Station Manager');
    });

    it('an unknown identity (isDev=false) is the neutral release form, never DEV', () => {
        expect(computeTitle('Settings', false)).toBe('Settings · Station Manager');
    });
});
