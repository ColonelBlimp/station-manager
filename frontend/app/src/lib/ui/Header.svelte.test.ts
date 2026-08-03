// The header's always-visible station identity: which logbook + rig this session
// logs to. Guards the config → setStationInfo → header render wiring (the gap the
// operator hit dogfooding — no way to see the active logbook/rig from the FT8 view).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Header from './Header.svelte';
import { setStationInfo, setLogbookCount, _resetStationForTests } from '../operate/station.svelte';
import { rig } from '../operate/rig.svelte';
import { router } from '../router.svelte';
import { isVisible, toggleTile } from '../operate/layout.svelte';

beforeEach(() => {
    _resetStationForTests();
    router.mode = 'phone';
    rig.cat = 'off';
});

describe('Header station identity', () => {
    it('shows the configured logbook + rig names once injected', () => {
        setStationInfo({ logbookName: 'Malawi 2026', rigName: 'FTdx10' });
        render(Header);
        expect(screen.getByText('Malawi 2026')).toBeInTheDocument();
        expect(screen.getByText('FTdx10')).toBeInTheDocument();
    });

    it('shows the logbook QSO count and live-updates it as QSOs are logged', () => {
        setStationInfo({ logbookName: 'Malawi 2026', rigName: 'FTdx10' });
        setLogbookCount(1234);
        render(Header);
        // Thousands-grouped beside the name.
        expect(screen.getByText('(1,234)')).toBeInTheDocument();

        // A logged QSO bumps the count — the header must reflect it without a reload.
        setLogbookCount(1235);
        flushSync();
        expect(screen.getByText('(1,235)')).toBeInTheDocument();
        expect(screen.queryByText('(1,234)')).toBeNull();
    });

    it('shows placeholders before config resolves, then updates reactively', () => {
        render(Header);
        expect(screen.getByText('—')).toBeInTheDocument();
        expect(screen.getByText('not set')).toBeInTheDocument();

        // The async /v1/config fetch resolves after the header has mounted — the
        // $state-backed store must re-render it, not leave the placeholders.
        setStationInfo({ logbookName: 'Field Day', rigName: 'IC-7300' });
        flushSync();
        expect(screen.getByText('Field Day')).toBeInTheDocument();
        expect(screen.getByText('IC-7300')).toBeInTheDocument();
    });
});

// The CAT chip must agree with the Rig panel's pill: in FT8 the manual/confirm
// states are meaningless (FT8 can't run without CAT — the panel's requiresCat
// lockout), so the chip shows "CAT required" there instead of manual/confirm.
describe('Header CAT chip vs FT8', () => {
    it('Phone/CW keeps the manual/confirm labels', () => {
        rig.cat = 'off';
        render(Header);
        expect(screen.getByText(/manual|confirm/)).toBeInTheDocument();
        expect(screen.queryByText('CAT required')).toBeNull();
    });

    it('FT8 with CAT away shows "CAT required" (matches the Rig panel)', () => {
        router.mode = 'ft8';
        rig.cat = 'off';
        render(Header);
        expect(screen.getByText('CAT required')).toBeInTheDocument();
        expect(screen.getByTitle('FT8 needs a live CAT connection')).toBeInTheDocument();
    });

    it('FT8 with CAT live shows the normal CAT label', () => {
        router.mode = 'ft8';
        rig.cat = 'connected';
        render(Header);
        expect(screen.getByText('CAT')).toBeInTheDocument();
        expect(screen.queryByText('CAT required')).toBeNull();
    });
});

// The CAT chip toggles the Rig Control panel (dogfood 2026-07-18): a second
// click on Operate dismisses what the first revealed; from any other view it
// navigates to Operate and REVEALS (never a blind toggle-off the operator
// can't see).
describe('Header CAT chip → Rig Control panel', () => {
    async function clickChip(): Promise<void> {
        // On Operate the chip is the only button in the header.
        await fireEvent.click(screen.getByRole('button'));
        flushSync();
    }

    beforeEach(() => {
        // Hidden is the default layout state for the rig tile.
        if (isVisible('rig')) toggleTile('rig');
    });

    it('on Operate: click reveals, second click dismisses', async () => {
        router.view = 'operate';
        render(Header);
        expect(isVisible('rig')).toBe(false);
        await clickChip();
        expect(isVisible('rig')).toBe(true);
        await clickChip();
        expect(isVisible('rig')).toBe(false);
    });

    /*
        OFF OPERATE THE CHIP IS A READOUT, NOT A CONTROL (operator, 2026-08-03).

        It used to navigate to Operate and reveal the panel, and H0 below is the
        test that pinned it — deliberate behaviour, deliberately tested. The
        operator ruled it out after hitting it from Settings: the chip sits in
        the header of every view, so a glance at the frequency was one stray
        click away from leaving the page.

        The cost is not the navigation itself, it is what leaving Settings does.
        There is NO unsaved-changes guard anywhere (router.svelte.ts:93 —
        navigate() just switches the view), and Settings is mounted behind
        {#if router.view === 'config'}, so leaving unmounts it. The draft
        survives (module singleton) but returning re-fires onMount → load() →
        #apply(), which overwrites it. Unsaved config edits are therefore
        discarded, and discarded on RETURN rather than on leaving, so there is
        no moment at which the operator could be warned.

        NARROW BY INSTRUCTION: the sidebar links still navigate away from dirty
        Settings and still discard edits the same way. That is the same defect
        through a different door, and it is knowingly left open here — the
        operator chose the narrow fix over a navigation guard.

        H2 is the half that is easy to skip. A chip that keeps a button's
        semantics and hover affordance while doing nothing is worse than one
        that navigates: the operator clicks, nothing happens, and there is no
        way to tell that from a broken control. Same rule as the Email
        section's Remove button (U2) — do not offer an action you won't take.
    */

    // H1 — off Operate, the chip changes NOTHING. Both halves are asserted: a
    // fix that only stopped the navigation would still pop the panel open
    // underneath, which is a layout change the operator never asked for and
    // cannot see from here.
    it('from another view: clicking the chip neither navigates nor touches the panel', async () => {
        router.view = 'logbook';
        render(Header);

        const chip = screen.getByTitle('Waiting for confirmation');
        await fireEvent.click(chip);
        flushSync();

        expect(router.view).toBe('logbook');
        expect(isVisible('rig')).toBe(false);
    });

    // H1b — and the same with the panel already open: "does nothing" has to mean
    // nothing in both directions, not just "doesn't reveal".
    it('from another view: an open panel is left open, not toggled', async () => {
        router.view = 'logbook';
        toggleTile('rig');
        render(Header);

        await fireEvent.click(screen.getByTitle('Waiting for confirmation'));
        flushSync();

        expect(router.view).toBe('logbook');
        expect(isVisible('rig')).toBe(true);
    });

    // H2 — off Operate it must not present itself as a control at all.
    it('from another view: the chip is not a button', () => {
        router.view = 'logbook';
        render(Header);
        expect(screen.queryByRole('button')).toBeNull();
    });

    // H2b — and on Operate it still is one, so H2 cannot be satisfied by
    // removing the control everywhere.
    it('on Operate: the chip is still a button', () => {
        router.view = 'operate';
        render(Header);
        expect(screen.getByRole('button')).toBeInTheDocument();
    });
});
