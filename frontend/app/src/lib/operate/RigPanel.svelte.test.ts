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
