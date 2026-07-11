// The header's always-visible station identity: which logbook + rig this session
// logs to. Guards the config → setStationInfo → header render wiring (the gap the
// operator hit dogfooding — no way to see the active logbook/rig from the FT8 view).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Header from './Header.svelte';
import { setStationInfo, setLogbookCount, _resetStationForTests } from '../operate/station.svelte';

beforeEach(() => {
    _resetStationForTests();
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
