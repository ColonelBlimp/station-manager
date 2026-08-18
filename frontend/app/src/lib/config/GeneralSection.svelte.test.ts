import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import GeneralSection from './GeneralSection.svelte';
import { generalState } from './general.svelte';

/*
    GENERAL SECTION — WHAT THE OPERATOR SEES.

    general.svelte.test.ts pins what goes ON THE WIRE (the map round-trip, sparse
    band colours, the restore-knob default). These rules pin the browser half: the
    operating toggle, a band-colour row, the read-only About panel, and that an edit
    lights up Save.
*/

afterEach(() => {
    vi.restoreAllMocks();
    generalState.loaded = false;
    generalState.loading = false;
    generalState.saving = false;
    generalState.error = '';
    generalState.buildInfo = null;
    generalState.buildError = '';
    generalState.form = { restoreRigOnModeSwitch: true, bandColors: {} };
});

function mockDaemon(
    config: unknown,
    version: unknown = { daemon: '2.0-test', go: 'go1.24.0', schema: { version: 6, dirty: false } }
): void {
    vi.stubGlobal(
        'fetch',
        vi.fn((url: RequestInfo | URL) => {
            const u = typeof url === 'string' ? url : url instanceof URL ? url.href : url.url;
            const body = u.includes('/v1/version') ? version : config;
            return Promise.resolve(
                new Response(JSON.stringify(body), {
                    status: 200,
                    headers: { 'Content-Type': 'application/json' },
                })
            );
        })
    );
}

describe('GeneralSection', () => {
    it('renders the operating toggle, a band-colour row, and the About panel after load', async () => {
        mockDaemon({ restore_rig_on_mode_switch: true, map: { band_colors: {} } });
        render(GeneralSection);

        expect(
            await screen.findByText(/Restore the rig when switching operating mode/)
        ).toBeTruthy();
        expect(screen.getByLabelText('20m arc colour')).toBeTruthy();
        // About panel — version + Go runtime from /v1/version.
        expect(await screen.findByText('2.0-test')).toBeTruthy();
        expect(screen.getByText('go1.24.0')).toBeTruthy();
    });

    it('toggling the knob marks it dirty and enables Save', async () => {
        mockDaemon({ restore_rig_on_mode_switch: true, map: { band_colors: {} } });
        render(GeneralSection);

        const save = await screen.findByRole<HTMLButtonElement>('button', { name: 'Save' });
        expect(save.disabled).toBe(true); // clean after load

        await fireEvent.click(screen.getByRole('checkbox'));
        expect(generalState.dirty).toBe(true);
        expect(save.disabled).toBe(false);
    });
});
