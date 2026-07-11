// Guards the FT8 Session button → export/session dialog wiring: clicking it must
// flip operate.exportOpen so ExportDialog (mounted in Operate.svelte for both
// modes) renders. Also confirms it works with an EMPTY session (the panel is
// viewable even before any QSO is logged).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import Ft8Operate from './Ft8Operate.svelte';
import { operate, closeExport } from './state.svelte';
import { session } from './session.svelte';

beforeEach(() => {
    closeExport();
    session.qsos.length = 0;
});

describe('FT8 Operate Session button', () => {
    it('opens the session/export dialog even when the session is empty', async () => {
        render(Ft8Operate);
        expect(operate.exportOpen).toBe(false);

        const btn = screen.getByRole('button', { name: /Session/ });
        expect(btn).not.toBeDisabled();
        await fireEvent.click(btn);

        expect(operate.exportOpen).toBe(true);
    });
});
