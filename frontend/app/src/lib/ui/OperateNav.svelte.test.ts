// W-0019 slice 4 (ADR 0080): the Operate nav lists FT4 as a third mode beside
// Phone / CW and FT8 — the sidebar is where operating mode lives, so the
// third profile is a third item, not a selector inside the FT view.
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import OperateNav from './OperateNav.svelte';

describe('OperateNav', () => {
    it('offers Phone / CW, FT8 and FT4', () => {
        render(OperateNav);
        for (const label of ['Phone / CW', 'FT8', 'FT4']) {
            expect(screen.getAllByText(label).length, label).toBeGreaterThan(0);
        }
    });
});
