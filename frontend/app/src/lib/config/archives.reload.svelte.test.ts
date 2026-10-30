import { describe, expect, it, vi } from 'vitest';

// W-0024 acceptance F6: every reload the archives store requests — the gate's
// "Reload now", a switch's or a default change's rebind — goes through the one
// reload seam (lib/utils/reload), which survives a refused earlier reload.
vi.mock('../utils/reload', () => ({ reloadPage: vi.fn() }));

import { reloadPage } from '../utils/reload';
import { reloadNow } from './archives.svelte';

describe('the archives store reloads through the shared seam', () => {
    it('Reload now calls lib/utils/reload, not location.reload directly', () => {
        reloadNow();
        expect(reloadPage).toHaveBeenCalledTimes(1);
    });
});
