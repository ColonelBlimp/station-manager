// The rig card is shared across modes. The one mode-specific bit — the band-button
// behaviour — is injected via the pickBand prop (Phone/CW: selectBand; FT8 later: a
// watering-hole pick). This pins that seam so the two modes can diverge cleanly.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import RigPanel from './RigPanel.svelte';
import {
    rig,
    rigCaps,
    toggleTune,
    catLink,
    resetCatLink,
    setModeMappings,
    setRigCaps,
    setFtProfileLabel,
    setFt4Frequencies,
    setFt8Frequencies,
    setFt8Mode,
} from './rig.svelte';
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
                    return Promise.resolve({ status: 'accepted' });
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

// dogfood 2026-09-11 (codex): the live Mode select lists the rig's OWN literals
// and sends them as-is; its LABEL for a mapped literal carries the friendly
// name, which follows the last profile whose stream opened — so the option the
// rig is on reads "DATA-U · FT4" after FT4 opened, "DATA-U · FT8" otherwise, and its value
// is still the DATA-U that set_mode needs.
describe('live Mode select labels', () => {
    it('labels the data literal by the last opened profile and keeps the raw value', async () => {
        resetCatLink();
        setModeMappings({
            USB: { mode: 'SSB', submode: 'USB' },
            'DATA-U': { mode: 'FT8', submode: '' },
        });
        setRigCaps({
            ops: ['set_mode', 'set_band', 'set_freq'],
            tune: false,
            rigModes: ['USB', 'DATA-U'],
        });
        rig.cat = 'connected';
        catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
        setFtProfileLabel('FT4');
        render(RigPanel, { props: {} });

        const option = (label: string) => screen.getByRole('option', { name: label });
        expect(option('DATA-U · FT4')).toHaveProperty('value', 'DATA-U');
        expect(option('USB')).toHaveProperty('value', 'USB');

        setFtProfileLabel('');
        await vi.waitFor(() => expect(option('DATA-U · FT8')).toHaveProperty('value', 'DATA-U'));
        expect(screen.queryByRole('option', { name: 'DATA-U · FT4' })).toBeNull();
    });
});

// Operator ruling 2026-09-11: in an FT mode a band with no dial in that mode's
// table is greyed out with a tooltip — never offered to fail with a toast —
// while Phone/CW keeps every band (the rig's band stack recalls it).
describe('band buttons and the FT dial tables', () => {
    beforeEach(() => {
        rig.cat = 'connected';
        setFt8Frequencies({ '20m': 14_074_000, '40m': 7_074_000, '17m': 18_100_000 });
        setFt4Frequencies({ '20m': 14_080_000, '40m': 7_047_500, '80m': 3_576_000 });
    });

    it('FT4: greys out a band with no FT4 dial and says why; a band with one stays live', () => {
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
        const b17 = screen.getByRole('button', { name: '17m' });
        expect(b17).toBeDisabled();
        expect(b17).toHaveAttribute('title', 'No FT4 frequency configured for 17m');
        expect(screen.getByRole('button', { name: '20m' })).toBeEnabled();
        expect(screen.getByRole('button', { name: '80m' })).toBeEnabled();
    });

    it('FT8: gates on its own table', () => {
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT8', ftMode: 'ft8' } });
        expect(screen.getByRole('button', { name: '17m' })).toBeEnabled();
        const b80 = screen.getByRole('button', { name: '80m' });
        expect(b80).toBeDisabled();
        expect(b80).toHaveAttribute('title', 'No FT8 frequency configured for 80m');
    });

    it('a dial table that arrives after the panel mounted enables its bands (config lands after boot)', async () => {
        setFt4Frequencies({});
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
        expect(screen.getByRole('button', { name: '20m' })).toBeDisabled();

        setFt4Frequencies({ '20m': 14_080_000 });
        await vi.waitFor(() => expect(screen.getByRole('button', { name: '20m' })).toBeEnabled());
        expect(screen.getByRole('button', { name: '17m' })).toBeDisabled();
    });

    it('Phone/CW: every band stays selectable', () => {
        render(RigPanel, { props: {} });
        for (const band of ['80m', '17m', '20m']) {
            expect(screen.getByRole('button', { name: band })).toBeEnabled();
        }
    });
});

// W-0012 slice "Rig Control mode control in the FT views" (operator rulings
// 2026-09-12): in the FT8/FT4 views the mode is owned by the profile — the
// bridge asserts the per-rig ft8_mode literal before every keyed rung — so a
// live selector there can only fight it. Mode becomes a READOUT in those views
// whenever a data literal is configured; Phone/CW is untouched (AC5).
describe('FT views: Mode is a readout owned by the profile', () => {
    const liveFt4 = () => {
        resetCatLink();
        setModeMappings({
            USB: { mode: 'SSB', submode: 'USB' },
            'DATA-U': { mode: 'FT8', submode: '' },
        });
        setRigCaps({
            ops: ['set_mode', 'set_band', 'set_freq'],
            tune: false,
            rigModes: ['USB', 'DATA-U'],
        });
        setFt4Frequencies({ '20m': 14_080_000, '40m': 7_047_500 });
        setFt8Mode('DATA-U');
        rig.cat = 'connected';
    };

    it('AC1: the rig on the configured data literal — the readout names the literal AND the open profile, in full, with nothing to open', () => {
        liveFt4();
        catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
        setFtProfileLabel(''); // the confusable: a readout naming the mapping's FT8 while FT4 is open
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });

        expect(screen.getByRole('status', { name: 'Mode' })).toHaveTextContent('DATA-U · FT4');
        expect(screen.queryByRole('combobox')).toBeNull();
    });

    it('AC2: the rig on another literal — the readout shows it, says it is not the profile mode, and the current band button still re-asserts', async () => {
        liveFt4();
        catLink.onRigState({ vfoA: 14_080_000, mode: 'USB' });
        const picked: string[] = [];
        render(RigPanel, {
            props: {
                requiresCat: true,
                modeLabel: 'FT4',
                ftMode: 'ft4',
                pickBand: (band: string) => {
                    picked.push(band);
                    return Promise.resolve({ status: 'accepted' });
                },
            },
        });

        const readout = screen.getByRole('status', { name: 'Mode' });
        expect(readout).toHaveTextContent('USB');
        expect(readout).toHaveTextContent(/not the FT4 data mode/);
        expect(readout).toHaveTextContent('DATA-U');
        expect(screen.queryByRole('combobox')).toBeNull();

        // The band already selected is not a no-op: its button is live and the
        // pick re-asserts dial + data mode (ft8SelectBand always writes).
        const current = screen.getByRole('button', { name: '20m' });
        expect(current).toBeEnabled();
        await fireEvent.click(current);
        expect(picked).toEqual(['20m']);
    });

    // Inbox 2026-09-13 named the daemon's tune carrier in the readout; the
    // 2026-09-14 ruling replaced that: nothing in the Mode field changes during
    // a tune — nothing added, nothing taken away — because the two-line note
    // reflowed the row and moved the Tune button out from under the operator's
    // finger. The daemon names the pre-tune literal on the tune-state push and
    // the readout holds it while the rig reports the carrier's RTTY-U. The
    // carrier survives only in the tooltip. The rig-state and tune-state pushes
    // are separate events with no ordering guarantee, so the hold must not
    // depend on which lands first.
    describe('AC2b: the Mode readout is identical before, during and after a tune', () => {
        const snapshot = () => {
            const readout = screen.getByRole('status', { name: 'Mode' });
            return { text: readout.textContent, cls: readout.className };
        };

        it('on the data mode: held through the carrier, whichever push lands first', () => {
            liveFt4();
            catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
            render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
            const before = snapshot();
            expect(before.text).toContain('DATA-U · FT4');

            // Tune-state first, then the rig's mode push.
            catLink.onTuneState({ active: true, restore_mode: 'DATA-U' });
            flushSync();
            expect(snapshot()).toEqual(before);
            catLink.onRigState({ mode: 'RTTY-U' });
            flushSync();
            expect(snapshot()).toEqual(before);
            const readout = screen.getByRole('status', { name: 'Mode' });
            expect(readout).not.toHaveTextContent(/RTTY/);
            expect(readout).not.toHaveTextContent(/tune carrier/);
            expect(readout.title).toMatch(/tune carrier/i); // the fact lives in the tooltip only

            // Stop: the daemon publishes inactive right after WRITING the restore;
            // the rig reports the restored mode a moment later. Rendered between
            // the two, the field is still identical (codex 2544b3da P2).
            catLink.onTuneState({ active: false });
            flushSync();
            expect(snapshot()).toEqual(before);
            catLink.onRigState({ mode: 'DATA-U' });
            flushSync();
            expect(snapshot()).toEqual(before);
        });

        it('after the stop, the first mode report ends the hold — a restore that did not take shows honestly', () => {
            liveFt4();
            catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
            render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
            catLink.onTuneState({ active: true, restore_mode: 'DATA-U' });
            catLink.onRigState({ mode: 'RTTY-U' });
            catLink.onTuneState({ active: false });
            catLink.onRigState({ mode: 'RTTY-U' }); // the rig answers still in RTTY
            flushSync();
            const readout = screen.getByRole('status', { name: 'Mode' });
            expect(readout).toHaveTextContent('RTTY-U');
            expect(readout).toHaveTextContent(/not the FT4 data mode/);
        });

        // The start side has no SPA-side cover for the other order: the daemon
        // publishes tune-state straight after the tune-on write returns, before
        // the rig can answer over serial and the read loop can publish its mode
        // (bridge/tune.go StartTune → publishTuneState; a reading of the code, not
        // a measurement). This case pins only that the END state is held whichever
        // order lands.
        it('rig mode push before the tune-state push: the settled state is held', () => {
            liveFt4();
            catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
            render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
            const before = snapshot();

            catLink.onRigState({ mode: 'RTTY-U' }); // the carrier's push lands first
            catLink.onTuneState({ active: true, restore_mode: 'DATA-U' });
            flushSync();
            expect(snapshot()).toEqual(before);
        });

        it('off the data mode before the tune: the mismatch note is held too, unchanged', () => {
            liveFt4();
            catLink.onRigState({ vfoA: 14_080_000, mode: 'USB' });
            render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
            const before = snapshot();
            expect(before.text).toMatch(/not the FT4 data mode/);
            expect(before.cls).toContain('border-amber-500');

            catLink.onTuneState({ active: true, restore_mode: 'USB' });
            catLink.onRigState({ mode: 'RTTY-U' });
            flushSync();
            expect(snapshot()).toEqual(before); // nothing added, nothing taken away
        });

        it('a tab opened mid-tune shows the restore mode, not the carrier', () => {
            liveFt4();
            catLink.onRigState({ vfoA: 14_080_000, mode: 'RTTY-U' });
            catLink.onTuneState({ active: true, restore_mode: 'DATA-U' }); // hub replay
            render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });

            expect(screen.getByRole('status', { name: 'Mode' })).toHaveTextContent('DATA-U · FT4');
        });

        it('the live selector (Phone/CW, or ft8_mode "") holds the restore mode as its value', () => {
            liveFt4();
            setFt8Mode('');
            catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
            render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
            expect(screen.getByRole('combobox')).toHaveValue('DATA-U');

            catLink.onTuneState({ active: true, restore_mode: 'DATA-U' });
            catLink.onRigState({ mode: 'RTTY-U' });
            flushSync();
            expect(screen.getByRole('combobox')).toHaveValue('DATA-U');
        });
    });

    it('AC3: ft8_mode configured as "" (leave the mode alone) — the FT view keeps the live selector', () => {
        liveFt4();
        setFt8Mode('');
        catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });

        expect(screen.getByRole('combobox')).toHaveValue('DATA-U');
        expect(screen.queryByRole('status', { name: 'Mode' })).toBeNull();
    });

    it('AC4: CAT off in an FT view — the readout names the profile and the manual mode is untouched', () => {
        liveFt4();
        rig.cat = 'off';
        rig.mode = 'USB'; // the operator's manual pick from Phone/CW
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });

        const readout = screen.getByRole('status', { name: 'Mode' });
        expect(readout).toHaveTextContent('FT4');
        expect(screen.queryByRole('combobox')).toBeNull();
        expect(rig.mode).toBe('USB');
    });

    it('AC5: Phone/CW keeps its live selector under the same rig state', () => {
        liveFt4();
        catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
        setFtProfileLabel('FT4'); // a label left over from an FT4 session must not leak a readout
        render(RigPanel, { props: {} });

        expect(screen.getByRole('combobox')).toHaveValue('DATA-U');
        expect(screen.queryByRole('status', { name: 'Mode' })).toBeNull();
    });

    it('a data literal that arrives after the panel mounted turns the selector into the readout (config lands after boot)', async () => {
        liveFt4();
        setFt8Mode('');
        catLink.onRigState({ vfoA: 14_080_000, mode: 'DATA-U' });
        render(RigPanel, { props: { requiresCat: true, modeLabel: 'FT4', ftMode: 'ft4' } });
        expect(screen.getByRole('combobox')).toBeInTheDocument();

        setFt8Mode('DATA-U');
        await vi.waitFor(() =>
            expect(screen.getByRole('status', { name: 'Mode' })).toHaveTextContent('DATA-U · FT4')
        );
        expect(screen.queryByRole('combobox')).toBeNull();
    });
});
