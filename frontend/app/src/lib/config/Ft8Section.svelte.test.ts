import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import Ft8Section from './Ft8Section.svelte';
import { ft8SettingsState } from './ft8.svelte';

/*
    FT8 SETTINGS — WHAT THE OPERATOR SEES.

    ft8.svelte.test.ts pins what goes on the wire and what reaches the live
    view. These rules pin the half of the acceptance criteria that is only
    observable in the browser:

        A1  I can tell "no ft8 block yet, showing defaults" apart from "the
            load failed".
        A3  I can tell an edit that needs a daemon restart apart from one that
            takes effect at once.

    U2 IS THE LOAD-BEARING ONE. The four blocks share one Save button, so
    nothing on screen distinguishes a row-cap change (live) from a PSK Reporter
    change (restart-only) except this notice. A section that showed it for every
    edit would be as uninformative as one that never showed it — and the
    always-on version is the easy implementation, which is why U2 drives it in
    BOTH directions from the same loaded state rather than only asserting the
    notice can appear.

    U3 PINS AN ABSENCE, which is unusual enough to justify. The config SPA's FT8
    tab has three CQ highlight-colour pickers; this shell deliberately has none
    (operator's ruling 2026-08-05 — Band Activity uses a theme-aware palette,
    and nothing in this app has ever read those values). Without U3 the natural
    "finish the port" instinct re-adds them, and three controls that change
    nothing on screen are indistinguishable from three that are broken. The
    values still ride the save — that half is W1's job, in the other file.

    U4 IS NOT COSMETIC. Every block here is a whole-block write, so a form that
    rendered blanks after a failed load would be one Save press away from
    erasing the operator's FT8 configuration. It must show an error instead.
*/

afterEach(() => {
    vi.restoreAllMocks();
    ft8SettingsState.loaded = false;
    ft8SettingsState.loading = false;
    ft8SettingsState.saving = false;
    ft8SettingsState.error = '';
});

const CONFIG = {
    ft8_enabled: true,
    ft8_display: {
        history_max: 250,
        feed_mode: 'single',
        cq_to_top: true,
        hide_hashed_calls: false,
        highlight_unworked: '#ff0000',
        highlight_worked: '#00ff00',
        highlight_calling: '#0000ff',
    },
    psk_reporter: { enabled: true, host: 'report.example.org', port: 2525 },
    ft8_decode_log: { enabled: true, path: 'log/ft8-custom.txt' },
};

function mockDaemon(body: unknown = CONFIG, status = 200): void {
    vi.stubGlobal(
        'fetch',
        vi.fn(() =>
            Promise.resolve(
                new Response(JSON.stringify(body), {
                    status,
                    headers: { 'Content-Type': 'application/json' },
                })
            )
        )
    );
}

/** Render and wait for the mounted load() to settle. */
async function renderLoaded(): Promise<void> {
    render(Ft8Section);
    await waitFor(() => expect(ft8SettingsState.loaded).toBe(true));
}

describe('Ft8Section', () => {
    it('U1: shows the values the daemon holds (A1)', async () => {
        mockDaemon();
        await renderLoaded();

        expect(screen.getByLabelText<HTMLInputElement>('Enable FT8').checked).toBe(true);
        expect(screen.getByLabelText<HTMLInputElement>('Row cap').value).toBe('250');
        expect(screen.getByLabelText<HTMLSelectElement>('Feed mode').value).toBe('single');
        expect(screen.getByLabelText<HTMLInputElement>('PSK Reporter host').value).toBe(
            'report.example.org'
        );
        expect(screen.getByLabelText<HTMLInputElement>('PSK Reporter port').value).toBe('2525');
        expect(screen.getByLabelText<HTMLInputElement>('Decode log file path').value).toBe(
            'log/ft8-custom.txt'
        );
    });

    it('U2: the restart notice marks restart-only edits, and only those (A3)', async () => {
        mockDaemon();
        await renderLoaded();

        // A display pref — applied to the running view on save.
        await fireEvent.input(screen.getByLabelText('Row cap'), { target: { value: '500' } });
        expect(ft8SettingsState.dirty).toBe(true);
        expect(screen.queryByText(/restart/i)).toBeNull();

        // A startup-only block — the daemon reads it when it binds.
        await fireEvent.input(screen.getByLabelText('PSK Reporter host'), {
            target: { value: 'other.example.org' },
        });
        expect(screen.getByText(/restart/i)).toBeTruthy();
    });

    it('U3: renders no colour pickers — a control this app cannot honour', async () => {
        mockDaemon();
        const { container } = render(Ft8Section);
        await waitFor(() => expect(ft8SettingsState.loaded).toBe(true));

        expect(container.querySelectorAll('input[type="color"]')).toHaveLength(0);
    });

    it('U4: a failed load shows an error, never a form of blanks (A1)', async () => {
        mockDaemon('', 500);
        render(Ft8Section);

        await waitFor(() => expect(ft8SettingsState.error).not.toBe(''));
        expect(screen.queryByLabelText('Row cap')).toBeNull();
        expect(screen.getByText(/couldn’t load/i)).toBeTruthy();
    });

    it('U5: Save and Cancel are inert until something is edited', async () => {
        mockDaemon();
        await renderLoaded();

        expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Save' }).disabled).toBe(true);
        await fireEvent.click(screen.getByLabelText('Float CQ decodes to the top'));
        expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Save' }).disabled).toBe(
            false
        );
    });
});
