// Fresh-install ruling 2026-09-26: where FT8/FT4 would have opened with the
// switch off, Phone / CW shows a short note linking to the setting — never the
// raw 'no such API route'.
import { afterEach, describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import FtOffNotice from './FtOffNotice.svelte';
import { ftFeature, router, setMode, takeSettingsTab } from '../router.svelte';

afterEach(() => {
    ftFeature.offNotice = false;
    takeSettingsTab();
    setMode('phone');
});

describe('FtOffNotice', () => {
    it('shows nothing unless a fallback happened', () => {
        render(FtOffNotice);
        expect(screen.queryByRole('status')).toBeNull();
    });

    it('says FT8 and FT4 are off and links to their settings', async () => {
        ftFeature.offNotice = true;
        render(FtOffNotice);
        const note = screen.getByRole('status');
        expect(note).toHaveTextContent('FT8 and FT4 are turned off');
        expect(note).not.toHaveTextContent(/API route/i);
        await fireEvent.click(screen.getByRole('link', { name: 'Settings → FT8 / FT4' }));
        expect(router.view).toBe('config');
        expect(takeSettingsTab()).toBe('ft8');
    });

    it('can be dismissed', async () => {
        ftFeature.offNotice = true;
        render(FtOffNotice);
        await fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
        flushSync();
        expect(screen.queryByRole('status')).toBeNull();
        expect(ftFeature.offNotice).toBe(false);
    });
});
