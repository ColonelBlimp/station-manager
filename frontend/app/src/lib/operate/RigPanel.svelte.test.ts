// The rig card is shared across modes. The one mode-specific bit — the band-button
// behaviour — is injected via the pickBand prop (Phone/CW: selectBand; FT8 later: a
// watering-hole pick). This pins that seam so the two modes can diverge cleanly.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import RigPanel from './RigPanel.svelte';
import { rig, rigCaps, toggleTune } from './rig.svelte';
import { toasts } from '../ui/toasts.svelte';

// Mock only toggleTune so the Tune button's outcome handling can be driven
// directly; everything else in rig.svelte (incl. the `rig` store) stays real.
vi.mock('./rig.svelte', async (importOriginal) => {
    const actual = await importOriginal<typeof import('./rig.svelte')>();
    return { ...actual, toggleTune: vi.fn() };
});

beforeEach(() => {
    rig.cat = 'off'; // manual mode — the band grid + freq field render
    rig.band = '20m';
});

describe('RigPanel shared card', () => {
    it('band buttons call the injected pickBand', async () => {
        const picked: string[] = [];
        render(RigPanel, {
            props: {
                pickBand: (band: string) => {
                    picked.push(band);
                    return Promise.resolve({ ok: true, message: '' });
                },
            },
        });

        await fireEvent.click(screen.getByRole('button', { name: '40m' }));
        expect(picked).toEqual(['40m']);
    });
});

// FT8 cannot operate without CAT (capture is gated daemon-side on the rig being
// connected), so the FT8 host passes requiresCat: with the rig away the card must
// disable everything and say why — a Confirm button there would promise an unblock
// (manual logging) that FT8 can't honour.
describe('requiresCat (FT8 host)', () => {
    it('CAT off — disables the controls, explains, and offers no Confirm', () => {
        rig.cat = 'off';
        render(RigPanel, { props: { requiresCat: true } });

        expect(screen.getByRole('button', { name: '40m' })).toBeDisabled();
        expect(screen.getByRole('combobox')).toBeDisabled();
        expect(screen.getByLabelText('Frequency (MHz)')).toBeDisabled();
        expect(screen.getByText('CAT required')).toBeInTheDocument();
        expect(screen.getByText(/FT8 needs a live CAT connection/)).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /Confirm/i })).toBeNull();
    });

    it('CAT lost — same lockout, keeps the lost pill, no go-manual Confirm', () => {
        rig.cat = 'lost';
        render(RigPanel, { props: { requiresCat: true } });

        expect(screen.getByRole('button', { name: '40m' })).toBeDisabled();
        expect(screen.getByText('CAT link lost')).toBeInTheDocument();
        expect(screen.getByText(/FT8 needs a live CAT connection/)).toBeInTheDocument();
        expect(screen.queryByRole('button', { name: /confirm/i })).toBeNull();
    });

    it('CAT connected — the live card is unaffected by requiresCat', () => {
        rig.cat = 'connected';
        render(RigPanel, { props: { requiresCat: true } });

        expect(screen.getByText('CAT connected')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: '40m' })).not.toBeDisabled();
        expect(screen.queryByText(/FT8 needs a live CAT connection/)).toBeNull();
    });

    it('without requiresCat the manual confirm flow is unchanged', () => {
        rig.cat = 'off';
        rig.confirmedBand = ''; // unconfirmed for this band
        render(RigPanel, { props: {} });

        expect(screen.getByText('Manual — confirm to log')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
        expect(screen.getByRole('button', { name: '40m' })).not.toBeDisabled();
    });
});

// F-04 confirm-by-push: the Tune button maps the widened tune outcome three ways
// for the operator — silent on a success (accepted/observed/alreadySatisfied/
// superseded), a WARN on unknown, and an ERROR on a definite failure. The old
// `if (!r.ok) toasts.error` collapses all of these into a single error toast.
describe('Tune button — confirm-by-push outcomes (F-04)', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        rig.cat = 'connected'; // Tune needs a live CAT connection
        rig.tuneActive = false;
        rigCaps.tune = true; // the Tune button renders only when the rig exposes tune
    });

    it('warns (not errors) on an unknown outcome', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        vi.mocked(toggleTune).mockResolvedValue({ status: 'unknown', message: 'outcome unknown' });

        render(RigPanel, { props: {} });
        await fireEvent.click(screen.getByRole('button', { name: 'Tune' }));

        expect(warn).toHaveBeenCalledOnce();
        expect(error).not.toHaveBeenCalled();
    });

    it('is silent on an accepted outcome', async () => {
        const warn = vi.spyOn(toasts, 'warn').mockImplementation(() => 0);
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        const info = vi.spyOn(toasts, 'info').mockImplementation(() => 0);
        vi.mocked(toggleTune).mockResolvedValue({ status: 'accepted' });

        render(RigPanel, { props: {} });
        await fireEvent.click(screen.getByRole('button', { name: 'Tune' }));

        expect(warn).not.toHaveBeenCalled();
        expect(error).not.toHaveBeenCalled();
        expect(info).not.toHaveBeenCalled();
    });

    it('errors on a definite refusal', async () => {
        const error = vi.spyOn(toasts, 'error').mockImplementation(() => 0);
        vi.mocked(toggleTune).mockResolvedValue({
            status: 'failed',
            kind: 'refused',
            message: 'rig not connected',
        });

        render(RigPanel, { props: {} });
        await fireEvent.click(screen.getByRole('button', { name: 'Tune' }));

        expect(error).toHaveBeenCalled();
    });
});
