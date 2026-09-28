// W-0019 slice 4 (ADR 0080): the Operate nav lists FT4 as a third mode beside
// Phone / CW and FT8 — the sidebar is where operating mode lives, so the
// third profile is a third item, not a selector inside the FT view.
import { afterEach, describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import OperateNav from './OperateNav.svelte';
import { setFtEnabled } from '../router.svelte';

describe('OperateNav', () => {
    it('offers Phone / CW, FT8 and FT4', () => {
        render(OperateNav);
        for (const label of ['Phone / CW', 'FT8', 'FT4']) {
            expect(screen.getAllByText(label).length, label).toBeGreaterThan(0);
        }
    });

    // Fresh-install ruling 2026-09-26: switch off → no FT8/FT4 links at all.
    it('lists only Phone / CW while FT8 and FT4 are turned off', () => {
        setFtEnabled(false);
        render(OperateNav);
        expect(screen.getAllByText('Phone / CW').length).toBeGreaterThan(0);
        expect(screen.queryByText('FT8')).toBeNull();
        expect(screen.queryByText('FT4')).toBeNull();
    });

    afterEach(() => setFtEnabled(true));
});
