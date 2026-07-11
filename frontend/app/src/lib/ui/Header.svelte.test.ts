// The header's always-visible station identity: which logbook + rig this session
// logs to. Guards the config → setStationInfo → header render wiring (the gap the
// operator hit dogfooding — no way to see the active logbook/rig from the FT8 view).

import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Header from './Header.svelte';
import { setStationInfo, _resetStationForTests } from '../operate/station.svelte';

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
