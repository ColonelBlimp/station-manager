// The header's always-visible station identity: which logbook + rig this session
// logs to. Guards the config → setStationInfo → header render wiring (the gap the
// operator hit dogfooding — no way to see the active logbook/rig from the FT8 view).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
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
        const { fireEvent } = await import('@testing-library/svelte');
        // The chip is the only button in the header.
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

    it('from another view: click navigates to Operate and reveals — never toggles off', async () => {
        router.view = 'logbook';
        // Panel already visible (left open earlier) — arriving must keep it open.
        toggleTile('rig');
        render(Header);
        await clickChip();
        expect(router.view).toBe('operate');
        expect(isVisible('rig')).toBe(true);
    });
});
