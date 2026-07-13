// The rig card is shared across modes. The one mode-specific bit — the band-button
// behaviour — is injected via the pickBand prop (Phone/CW: selectBand; FT8 later: a
// watering-hole pick). This pins that seam so the two modes can diverge cleanly.

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import RigPanel from './RigPanel.svelte';
import { rig } from './rig.svelte';

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
